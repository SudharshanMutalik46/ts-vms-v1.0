#include "TSVmsPlaybackEngine.h"

#include <gst/gst.h>
#include <gst/video/videooverlay.h>
#include <gst/video/video.h>

#include <string>
#include <mutex>
#include <atomic>
#include <memory>
#include <vector>

namespace {

std::once_flag g_gstInitFlag;

std::wstring Utf8ToWide(const char* utf8)
{
    if (!utf8) return L"";
    int len = MultiByteToWideChar(CP_UTF8, 0, utf8, -1, nullptr, 0);
    if (len <= 0) return L"";
    std::wstring out(len - 1, L'\0');
    MultiByteToWideChar(CP_UTF8, 0, utf8, -1, out.data(), len);
    return out;
}

std::string WideToUtf8(const wchar_t* wide)
{
    if (!wide) return {};
    int len = WideCharToMultiByte(CP_UTF8, 0, wide, -1, nullptr, 0, nullptr, nullptr);
    if (len <= 0) return {};
    std::string out(len - 1, '\0');
    WideCharToMultiByte(CP_UTF8, 0, wide, -1, out.data(), len, nullptr, nullptr);
    return out;
}

std::string FilePathToUri(const wchar_t* path)
{
    std::string utf8 = WideToUtf8(path);
    if (utf8.rfind("file://", 0) == 0 || utf8.rfind("rtsp://", 0) == 0 || utf8.rfind("http://", 0) == 0 || utf8.rfind("https://", 0) == 0)
        return utf8;
    gchar* uri = gst_filename_to_uri(utf8.c_str(), nullptr);
    if (!uri)
        return utf8;
    std::string result(uri);
    g_free(uri);
    return result;
}

class PlaybackEngine {
public:
    PlaybackEngine() = default;
    ~PlaybackEngine() { Cleanup(); }

    int NormalizeDegrees(int degrees)
    {
        int d = degrees % 360;
        if (d < 0) d += 360;
        return d;
    }

    int RotationToFlipMethod(int degrees)
    {
        switch (NormalizeDegrees(degrees))
        {
        case 90:  return 1; // clockwise
        case 180: return 2; // rotate-180
        case 270: return 3; // counterclockwise
        default:  return 0; // identity
        }
    }

    int SetRotationDegrees(int degrees)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        _rotationDegrees = NormalizeDegrees(degrees);

        if (_videoFlip)
        {
            g_object_set(
                G_OBJECT(_videoFlip),
                "method",
                RotationToFlipMethod(_rotationDegrees),
                nullptr);
        }

        return 1;
    }

    int GetRotationDegrees() const
    {
        return _rotationDegrees;
    }

    double GetRate()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        return _currentRate;
    }

    int Initialize(HWND hwnd)
    {
        std::call_once(g_gstInitFlag, []() {
            int argc = 0;
            char** argv = nullptr;
            gst_init(&argc, &argv);
        });
        std::lock_guard<std::mutex> lock(_mutex);
        _hwnd = hwnd;
        _overlayHwnd.store(reinterpret_cast<guintptr>(hwnd), std::memory_order_release);
        EnsurePipelineLocked();
        return _pipeline ? 1 : 0;
    }

    int SetWindowHandle(HWND hwnd)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        _hwnd = hwnd;
        _overlayHwnd.store(reinterpret_cast<guintptr>(hwnd), std::memory_order_release);

        if (!_pipeline || hwnd == nullptr)
            return 1;
        GstElement* sink = nullptr;
        g_object_get(G_OBJECT(_pipeline), "video-sink", &sink, nullptr);

        if (sink)
        {
            if (GST_IS_VIDEO_OVERLAY(sink))
            {
                gst_video_overlay_set_window_handle(
                    GST_VIDEO_OVERLAY(sink),
                    reinterpret_cast<guintptr>(hwnd));
                gst_video_overlay_handle_events(GST_VIDEO_OVERLAY(sink), TRUE);
                gst_video_overlay_expose(GST_VIDEO_OVERLAY(sink));
            }

            gst_object_unref(sink);
        }

        return 1;
    }

    int SetMediaPath(const wchar_t* path)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        EnsurePipelineLocked();
        if (!_pipeline)
        {
            SetErrorLocked(L"Pipeline not initialized");
            return 0;
        }

        {
            std::lock_guard<std::mutex> playlistLock(_playlistMutex);
            _playlistPaths.clear();
            
            // --- FIX: Reset all trackers ---
            _currentPlaylistIndex.store(-1, std::memory_order_release);
            _queuedPlaylistIndex.store(-1, std::memory_order_release);
            _lastPos = 0.0;
        }

        std::string uri = FilePathToUri(path);

        gst_element_set_state(_pipeline, GST_STATE_NULL);
        g_object_set(G_OBJECT(_pipeline), "uri", uri.c_str(), nullptr);

        _currentRate = 1.0;
        _lastPath = path ? path : L"";
        _mediaLoaded = !_lastPath.empty();
        _eosReached.store(false, std::memory_order_release);
        return _mediaLoaded ? 1 : 0;
    }

    int SetPlaylist(const wchar_t* const* paths, int count, int startIndex)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        EnsurePipelineLocked();

        if (!_pipeline)
        {
            SetErrorLocked(L"Pipeline not initialized");
            return 0;
        }

        std::vector<std::wstring> items;
        if (paths && count > 0)
        {
            items.reserve(count);
            for (int i = 0; i < count; ++i)
            {
                if (paths[i] && paths[i][0] != L'\0')
                    items.emplace_back(paths[i]);
            }
        }

        if (items.empty())
        {
            SetErrorLocked(L"Playlist is empty");
            return 0;
        }

        if (startIndex < 0 || startIndex >= static_cast<int>(items.size()))
            startIndex = 0;
            
        std::wstring firstPath;
        {
            std::lock_guard<std::mutex> playlistLock(_playlistMutex);
            _playlistPaths = std::move(items);
            
            // --- FIX: Reset all trackers ---
            _currentPlaylistIndex.store(startIndex, std::memory_order_release);
            _queuedPlaylistIndex.store(startIndex, std::memory_order_release);
            _lastPos = 0.0;
            
            firstPath = _playlistPaths[startIndex];
        }

        std::string uri = FilePathToUri(firstPath.c_str());

        gst_element_set_state(_pipeline, GST_STATE_NULL);
        g_object_set(G_OBJECT(_pipeline), "uri", uri.c_str(), nullptr);

        _currentRate = 1.0;
        _lastPath = firstPath;
        _mediaLoaded = true;
        _eosReached.store(false, std::memory_order_release);

        return 1;
    }

    static void AboutToFinish(GstElement* playbin, gpointer userData)
    {
        auto* self = static_cast<PlaybackEngine*>(userData);
        if (!self)
            return;

        std::wstring nextPath;
        int nextIndex = -1;
        {
            std::lock_guard<std::mutex> playlistLock(self->_playlistMutex);

            // --- FIX: Increment _queuedPlaylistIndex, NOT _currentPlaylistIndex ---
            int currentQueued = self->_queuedPlaylistIndex.load(std::memory_order_acquire);
            nextIndex = currentQueued + 1;

            if (nextIndex < 0 || nextIndex >= static_cast<int>(self->_playlistPaths.size()))
                return;
                
            nextPath = self->_playlistPaths[nextIndex];
            self->_queuedPlaylistIndex.store(nextIndex, std::memory_order_release);
        }

        std::string nextUri = FilePathToUri(nextPath.c_str());
        g_object_set(G_OBJECT(playbin), "uri", nextUri.c_str(), nullptr);

        self->_lastPath = nextPath;
        self->_eosReached.store(false, std::memory_order_release);
    }

    // --- FIX: Gapless Transition Wrap-Around Detection ---
    void CheckForGaplessTransitionLocked(double currentPos)
    {
        int current = _currentPlaylistIndex.load(std::memory_order_acquire);
        int queued = _queuedPlaylistIndex.load(std::memory_order_acquire);

        if (queued > current)
        {
            // Detect file switch: 
            // We removed the "< 3.0" check because your segments are very short.
            // Now, we ONLY transition when the position physically drops back to zero,
            // or drops backwards by more than half a second.
            if ((_lastPos - currentPos > 0.5) || (currentPos < 0.2))
            {
                _currentPlaylistIndex.store(queued, std::memory_order_release);
            }
        }
        _lastPos = currentPos;
    }

    int GetPlaylistIndex() 
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (_pipeline)
        {
            gint64 pos = 0;
            if (gst_element_query_position(_pipeline, GST_FORMAT_TIME, &pos))
            {
                CheckForGaplessTransitionLocked(static_cast<double>(pos) / GST_SECOND);
            }
        }
        return _currentPlaylistIndex.load(std::memory_order_acquire);
    }

    double GetPositionSeconds()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline) return 0.0;
        
        gint64 pos = 0;
        if (!gst_element_query_position(_pipeline, GST_FORMAT_TIME, &pos))
            return 0.0;
            
        double currentPos = static_cast<double>(pos) / GST_SECOND;
        CheckForGaplessTransitionLocked(currentPos);
        
        return currentPos;
    }

    int Play()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline || !_mediaLoaded)
        {
            SetErrorLocked(L"Load a recorded segment first");
            return 0;
        }

        _eosReached.store(false, std::memory_order_release);
        auto result = gst_element_set_state(_pipeline, GST_STATE_PLAYING);
        if (result == GST_STATE_CHANGE_FAILURE)
        {
            SetErrorLocked(L"Failed to start playback");
            return 0;
        }

        return 1;
    }

    int Pause()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline || !_mediaLoaded)
        {
            SetErrorLocked(L"Load a recorded segment first");
            return 0;
        }

        auto result = gst_element_set_state(_pipeline, GST_STATE_PAUSED);
        if (result == GST_STATE_CHANGE_FAILURE)
        {
            SetErrorLocked(L"Failed to pause playback");
            return 0;
        }

        return 1;
    }

    int Stop()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline) return 0;

        _eosReached.store(false, std::memory_order_release);

        gst_element_set_state(_pipeline, GST_STATE_NULL);

        GstState st, pending;
        gst_element_get_state(_pipeline, &st, &pending, 500 * GST_MSECOND);
        return 1;
    }

    int SeekSeconds(double seconds)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline || !_mediaLoaded)
        {
            SetErrorLocked(L"Load a recorded segment first");
            return 0;
        }

        if (seconds < 0.0)
            seconds = 0.0;
            
        GstState state = GST_STATE_NULL;
        GstState pending = GST_STATE_VOID_PENDING;
        gst_element_get_state(_pipeline, &state, &pending, 200 * GST_MSECOND);
        if (state < GST_STATE_PAUSED)
        {
            auto change = gst_element_set_state(_pipeline, GST_STATE_PAUSED);
            if (change == GST_STATE_CHANGE_FAILURE)
            {
                SetErrorLocked(L"Failed to prepare playback for seek");
                return 0;
            }

            gst_element_get_state(_pipeline, &state, &pending, 1000 * GST_MSECOND);
        }

        gint64 dur = 0;
        if (gst_element_query_duration(_pipeline, GST_FORMAT_TIME, &dur) && dur > 0)
        {
            double durSec = static_cast<double>(dur) / GST_SECOND;
            if (durSec > 0.25 && seconds > durSec - 0.25)
                seconds = durSec - 0.25;
        }

        gint64 pos = static_cast<gint64>(seconds * GST_SECOND);
        
        // --- FIX: A Seek flushes the gapless queue. Reset trackers. ---
        _queuedPlaylistIndex.store(_currentPlaylistIndex.load(std::memory_order_acquire), std::memory_order_release);
        _lastPos = static_cast<double>(pos) / GST_SECOND;

        GstSeekFlags seekFlags =
            static_cast<GstSeekFlags>(GST_SEEK_FLAG_FLUSH | GST_SEEK_FLAG_KEY_UNIT);
            
        if (std::abs(_currentRate) <= 2.0)
        {
            seekFlags = static_cast<GstSeekFlags>(seekFlags | GST_SEEK_FLAG_ACCURATE);
        }

        gboolean ok = gst_element_seek(
            _pipeline,
            _currentRate,
            GST_FORMAT_TIME,
            seekFlags,
            GST_SEEK_TYPE_SET, pos,
            GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);
            
        if (!ok)
        {
            ok = gst_element_seek(
                _pipeline,
                _currentRate,
                GST_FORMAT_TIME,
                GST_SEEK_FLAG_FLUSH,
                GST_SEEK_TYPE_SET, pos,
                GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);
        }

        if (!ok)
        {
            SetErrorLocked(L"Seek failed");
            return 0;
        }

        _eosReached.store(false, std::memory_order_release);
        return 1;
    }
    
    bool TrySeekRateLocked(double rate, gint64 pos)
    {
        // --- FIX: A Seek flushes the gapless queue. Reset trackers. ---
        _queuedPlaylistIndex.store(_currentPlaylistIndex.load(std::memory_order_acquire), std::memory_order_release);
        _lastPos = static_cast<double>(pos) / GST_SECOND;

        bool isNearStart = (pos <= 500 * GST_MSECOND);

        GstSeekFlags flags = isNearStart
            ? static_cast<GstSeekFlags>(GST_SEEK_FLAG_KEY_UNIT)
            : static_cast<GstSeekFlags>(GST_SEEK_FLAG_FLUSH | GST_SEEK_FLAG_KEY_UNIT);
            
        if (rate == 1.0)
        {
            if (isNearStart) return true;

            gboolean ok = gst_element_seek(
                _pipeline,
                1.0,
                GST_FORMAT_TIME,
                flags,
                GST_SEEK_TYPE_SET, pos,
                GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);
                
            if (!ok)
            {
                ok = gst_element_seek_simple(
                    _pipeline,
                    GST_FORMAT_TIME,
                    flags,
                    pos);
            }

            return ok == TRUE;
        }

        if (!isNearStart && std::abs(rate) <= 2.0)
        {
            flags = static_cast<GstSeekFlags>(flags | GST_SEEK_FLAG_ACCURATE);
        }
        else if (std::abs(rate) > 2.0)
        {
#ifdef GST_SEEK_FLAG_TRICKMODE
            flags = static_cast<GstSeekFlags>(flags | GST_SEEK_FLAG_TRICKMODE);
#endif
#ifdef GST_SEEK_FLAG_TRICKMODE_KEY_UNITS
            flags = static_cast<GstSeekFlags>(flags | GST_SEEK_FLAG_TRICKMODE_KEY_UNITS);
#endif
#ifdef GST_SEEK_FLAG_TRICKMODE_NO_AUDIO
            flags = static_cast<GstSeekFlags>(flags | GST_SEEK_FLAG_TRICKMODE_NO_AUDIO);
#endif
        }

        gboolean ok = gst_element_seek(
            _pipeline,
            rate,
            GST_FORMAT_TIME,
            flags,
            GST_SEEK_TYPE_SET, pos,
            GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);
            
        if (!ok)
        {
            ok = gst_element_seek(
                _pipeline,
                rate,
                GST_FORMAT_TIME,
                static_cast<GstSeekFlags>(GST_SEEK_FLAG_KEY_UNIT),
                GST_SEEK_TYPE_SET, pos,
                GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);
        }

        return ok == TRUE;
    }
    
    int SetRate(double rate)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline || !_mediaLoaded)
        {
            SetErrorLocked(L"Load a recorded segment first");
            return 0;
        }

        if (rate == 0.0)
        {
            SetErrorLocked(L"Playback rate cannot be zero");
            return 0;
        }

        if (rate == 1.0 && _currentRate == 1.0)
            return 1;
            
        GstState state = GST_STATE_NULL;
        GstState pending = GST_STATE_VOID_PENDING;
        gst_element_get_state(_pipeline, &state, &pending, 200 * GST_MSECOND);
        
        if (state < GST_STATE_PAUSED)
        {
            auto change = gst_element_set_state(_pipeline, GST_STATE_PAUSED);
            if (change == GST_STATE_CHANGE_FAILURE)
            {
                SetErrorLocked(L"Failed to prepare playback for rate change");
                return 0;
            }
            gst_element_get_state(_pipeline, &state, &pending, 2000 * GST_MSECOND);
        }

        gint64 pos = 0;
        if (!gst_element_query_position(_pipeline, GST_FORMAT_TIME, &pos))
            pos = 0;
        double appliedRate = rate;

        if (!TrySeekRateLocked(rate, pos))
        {
            if (rate > 2.0)
            {
                gst_element_get_state(_pipeline, &state, &pending, 200 * GST_MSECOND);
                if (TrySeekRateLocked(2.0, pos))
                {
                    appliedRate = 2.0;
                }
                else
                {
                    SetErrorLocked(L"Requested high-speed playback is not supported for the current stream");
                    return 0;
                }
            }
            else if (rate != 1.0 && TrySeekRateLocked(1.0, pos))
            {
                appliedRate = 1.0;
            }
            else
            {
                SetErrorLocked(L"Requested playback speed is not supported for the current stream");
                return 0;
            }
        }

        _currentRate = appliedRate;
        _eosReached.store(false, std::memory_order_release);
        return 1;
    }

    int StepFrame(int frames)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline || !_mediaLoaded)
        {
            SetErrorLocked(L"Load a recorded segment first");
            return 0;
        }

        if (frames == 0)
            frames = 1;
            
        GstState state = GST_STATE_NULL;
        GstState pending = GST_STATE_VOID_PENDING;
        gst_element_get_state(_pipeline, &state, &pending, 200 * GST_MSECOND);
        
        if (state != GST_STATE_PAUSED)
        {
            gst_element_set_state(_pipeline, GST_STATE_PAUSED);
            gst_element_get_state(_pipeline, &state, &pending, 500 * GST_MSECOND);
        }

        if (frames > 0)
        {
            GstEvent* evt = gst_event_new_step(
                GST_FORMAT_BUFFERS,
                static_cast<guint64>(frames),
                1.0,
                TRUE,
                FALSE);
                
            gboolean ok = gst_element_send_event(_pipeline, evt);
            if (!ok)
            {
                SetErrorLocked(L"Frame step failed");
                return 0;
            }

            return 1;
        }

        gint64 pos = 0;
        if (!gst_element_query_position(_pipeline, GST_FORMAT_TIME, &pos))
        {
            SetErrorLocked(L"Backward frame step failed");
            return 0;
        }

        const double assumedFrameSeconds = 1.0 / 25.0;
        gint64 delta = static_cast<gint64>(std::llabs(frames) * assumedFrameSeconds * GST_SECOND);
        gint64 target = pos > delta ? pos - delta : 0;
        
        gboolean ok = gst_element_seek(
            _pipeline,
            1.0,
            GST_FORMAT_TIME,
            static_cast<GstSeekFlags>(GST_SEEK_FLAG_FLUSH | GST_SEEK_FLAG_ACCURATE),
            GST_SEEK_TYPE_SET, target,
            GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);
            
        if (!ok)
        {
            SetErrorLocked(L"Backward frame step failed");
            return 0;
        }

        return 1;
    }

    double GetDurationSeconds()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline) return 0.0;
        gint64 dur = 0;
        if (!gst_element_query_duration(_pipeline, GST_FORMAT_TIME, &dur))
            return 0.0;
        return static_cast<double>(dur) / GST_SECOND;
    }

    int GetState()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline) return 0;
        GstState state = GST_STATE_NULL;
        GstState pending = GST_STATE_VOID_PENDING;
        gst_element_get_state(_pipeline, &state, &pending, 50 * GST_MSECOND);
        switch (state)
        {
            case GST_STATE_PLAYING: return 2;
            case GST_STATE_PAUSED:  return 1;
            case GST_STATE_READY:   return 0;
            default:                return 0;
        }
    }

    int HasReachedEos() const
    {
        return _eosReached.load(std::memory_order_acquire) ? 1 : 0;
    }

    const wchar_t* GetLastError()
    {
        std::lock_guard<std::mutex> lock(_errorMutex);
        return _lastError.c_str();
    }

private:
    GstElement* BuildVideoFilterBin()
    {
        GstElement* bin = gst_bin_new("tsvms_video_filter_bin");
        if (!bin) return nullptr;

        GstElement* d3d11dl = gst_element_factory_make("d3d11download", "tsvms_d3d11dl");
        GstElement* convert = gst_element_factory_make("videoconvert", "tsvms_convert");
        _videoFlip = gst_element_factory_make("videoflip", "tsvms_video_flip");

        if (!convert || !_videoFlip)
        {
            if (d3d11dl) gst_object_unref(d3d11dl);
            if (convert) gst_object_unref(convert);
            if (_videoFlip) { gst_object_unref(_videoFlip); _videoFlip = nullptr; }
            gst_object_unref(bin);
            return nullptr;
        }

        g_object_set(G_OBJECT(_videoFlip), "method", RotationToFlipMethod(_rotationDegrees), nullptr);
        
        if (d3d11dl)
        {
            gst_bin_add_many(GST_BIN(bin), d3d11dl, convert, _videoFlip, nullptr);
            if (!gst_element_link_many(d3d11dl, convert, _videoFlip, nullptr))
            {
                gst_bin_remove_many(GST_BIN(bin), d3d11dl, convert, _videoFlip, nullptr);
                gst_object_unref(d3d11dl);
                d3d11dl = nullptr;
    
                gst_bin_add_many(GST_BIN(bin), convert, _videoFlip, nullptr);
                gst_element_link(convert, _videoFlip);
            }
        }
        else
        {
            gst_bin_add_many(GST_BIN(bin), convert, _videoFlip, nullptr);
            gst_element_link(convert, _videoFlip);
        }

        GstElement* firstElem = d3d11dl ? d3d11dl : convert;
        GstPad* sinkPad = gst_element_get_static_pad(firstElem, "sink");
        gst_element_add_pad(bin, gst_ghost_pad_new("sink", sinkPad));
        gst_object_unref(sinkPad);

        GstPad* srcPad = gst_element_get_static_pad(_videoFlip, "src");
        gst_element_add_pad(bin, gst_ghost_pad_new("src", srcPad));
        gst_object_unref(srcPad);
        
        return bin;
    }

    void EnsurePipelineLocked()
    {
        if (_pipeline)
            return;
            
        _pipeline = gst_element_factory_make("playbin", "tsvms-playbin");
        if (!_pipeline)
        {
            SetErrorLocked(L"Failed to create playbin");
            return;
        }

        GstElement* filterBin = BuildVideoFilterBin();
        if (filterBin)
        {
            g_object_set(G_OBJECT(_pipeline), "video-filter", filterBin, nullptr);
        }

        GstElement* videoSink = gst_element_factory_make("d3d11videosink", "video-sink");
        if (!videoSink)
            videoSink = gst_element_factory_make("glimagesink", "video-sink");
        if (!videoSink)
            videoSink = gst_element_factory_make("autovideosink", "video-sink");
        if (!videoSink)
            videoSink = gst_element_factory_make("d3dvideosink", "video-sink");
            
        if (videoSink)
        {
            if (g_object_class_find_property(G_OBJECT_GET_CLASS(videoSink), "force-aspect-ratio"))
            {
                g_object_set(G_OBJECT(videoSink), "force-aspect-ratio", FALSE, nullptr);
            }

            if (g_object_class_find_property(G_OBJECT_GET_CLASS(videoSink), "sync"))
            {
                g_object_set(G_OBJECT(videoSink), "sync", TRUE, nullptr);
            }

            g_object_set(G_OBJECT(_pipeline), "video-sink", videoSink, nullptr);
        }

        GstBus* bus = gst_element_get_bus(_pipeline);
        gst_bus_set_sync_handler(bus, &PlaybackEngine::BusSyncHandler, this, nullptr);
        gst_object_unref(bus);
        
        g_signal_connect(_pipeline, "about-to-finish", G_CALLBACK(&PlaybackEngine::AboutToFinish), this);
    }

    void Cleanup()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (_pipeline)
        {
            gst_element_set_state(_pipeline, GST_STATE_NULL);
            gst_object_unref(_pipeline);
            _pipeline = nullptr;
        }
        _videoFlip = nullptr;
    }

    static GstBusSyncReply BusSyncHandler(GstBus*, GstMessage* message, gpointer userData)
    {
        auto* self = static_cast<PlaybackEngine*>(userData);
        if (!self) return GST_BUS_PASS;

        if (gst_is_video_overlay_prepare_window_handle_message(message))
        {
            auto hwndValue = self->_overlayHwnd.load(std::memory_order_acquire);
            if (hwndValue != 0)
            {
                GstVideoOverlay* overlay = GST_VIDEO_OVERLAY(GST_MESSAGE_SRC(message));
                gst_video_overlay_set_window_handle(overlay, hwndValue);
                gst_video_overlay_handle_events(overlay, TRUE);
                gst_video_overlay_expose(overlay);
            }
            return GST_BUS_DROP;
        }

        if (GST_MESSAGE_TYPE(message) == GST_MESSAGE_EOS)
        {
            self->_eosReached.store(true, std::memory_order_release);
            return GST_BUS_PASS;
        }

        if (GST_MESSAGE_TYPE(message) == GST_MESSAGE_ERROR)
        {
            GError* err = nullptr;
            gchar* dbg = nullptr;
            gst_message_parse_error(message, &err, &dbg);
            if (err)
            {
                std::lock_guard<std::mutex> lock(self->_errorMutex);
                self->_lastError = Utf8ToWide(err->message);
                g_error_free(err);
            }
            if (dbg) g_free(dbg);
        }

        return GST_BUS_PASS;
    }

    void SetErrorLocked(const std::wstring& message)
    {
        std::lock_guard<std::mutex> lock(_errorMutex);
        _lastError = message;
    }

private:
    std::mutex _mutex;
    std::mutex _errorMutex;
    std::mutex _playlistMutex;
    GstElement* _pipeline = nullptr;
    HWND _hwnd = nullptr;
    std::atomic<guintptr> _overlayHwnd { 0 };
    double _currentRate = 1.0;
    std::wstring _lastError;
    std::wstring _lastPath;
    bool _mediaLoaded = false;
    GstElement* _videoFlip = nullptr;
    int _rotationDegrees = 0;
    std::atomic<bool> _eosReached { false };
    std::vector<std::wstring> _playlistPaths;

    // --- NEW GAPLESS TRACKING VARIABLES ---
    std::atomic<int> _currentPlaylistIndex { -1 };
    std::atomic<int> _queuedPlaylistIndex { -1 }; 
    double _lastPos = 0.0;
};

} // namespace

extern "C" {

TSVMS_PLAYBACK_API void* tsplay_create()
{
    return new PlaybackEngine();
}

TSVMS_PLAYBACK_API void tsplay_destroy(void* engine)
{
    delete static_cast<PlaybackEngine*>(engine);
}

TSVMS_PLAYBACK_API int tsplay_initialize(void* engine, HWND hwnd)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->Initialize(hwnd);
}

TSVMS_PLAYBACK_API int tsplay_set_window_handle(void* engine, HWND hwnd)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SetWindowHandle(hwnd);
}

TSVMS_PLAYBACK_API int tsplay_set_media_path(void* engine, const wchar_t* path)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SetMediaPath(path);
}

TSVMS_PLAYBACK_API int tsplay_play(void* engine)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->Play();
}

TSVMS_PLAYBACK_API int tsplay_pause(void* engine)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->Pause();
}

TSVMS_PLAYBACK_API int tsplay_stop(void* engine)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->Stop();
}

TSVMS_PLAYBACK_API int tsplay_seek_seconds(void* engine, double seconds)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SeekSeconds(seconds);
}

TSVMS_PLAYBACK_API int tsplay_set_rate(void* engine, double rate)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SetRate(rate);
}

TSVMS_PLAYBACK_API int tsplay_step_frame(void* engine, int frames)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->StepFrame(frames);
}

TSVMS_PLAYBACK_API int tsplay_set_playlist(void* engine, const wchar_t* const* paths, int count, int startIndex)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SetPlaylist(paths, count, startIndex);
}

TSVMS_PLAYBACK_API int tsplay_get_playlist_index(void* engine)
{
    if (!engine) return -1;
    return static_cast<PlaybackEngine*>(engine)->GetPlaylistIndex();
}

TSVMS_PLAYBACK_API double tsplay_get_position_seconds(void* engine)
{
    if (!engine) return 0.0;
    return static_cast<PlaybackEngine*>(engine)->GetPositionSeconds();
}

TSVMS_PLAYBACK_API double tsplay_get_duration_seconds(void* engine)
{
    if (!engine) return 0.0;
    return static_cast<PlaybackEngine*>(engine)->GetDurationSeconds();
}

TSVMS_PLAYBACK_API int tsplay_get_state(void* engine)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->GetState();
}

TSVMS_PLAYBACK_API int tsplay_has_reached_eos(void* engine)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->HasReachedEos();
}

TSVMS_PLAYBACK_API const wchar_t* tsplay_get_last_error(void* engine)
{
    if (!engine) return L"Invalid engine";
    return static_cast<PlaybackEngine*>(engine)->GetLastError();
}

TSVMS_PLAYBACK_API int TSPlayback_SetRotationDegrees(void* engine, int degrees)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SetRotationDegrees(degrees);
}

TSVMS_PLAYBACK_API int TSPlayback_GetRotationDegrees(void* engine)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->GetRotationDegrees();
}

TSVMS_PLAYBACK_API double tsplay_get_rate(void* engine)
{
    if (!engine) return 1.0;
    return static_cast<PlaybackEngine*>(engine)->GetRate();
}

}
