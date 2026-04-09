#include "TSVmsPlaybackEngine.h"

#include <gst/gst.h>
#include <gst/video/videooverlay.h>
#include <gst/video/video.h>
#include <gst/video/video-frame.h>
#include <gst/tag/tag.h>

#include <string>
#include <mutex>
#include <atomic>
#include <memory>
#include <vector>
#include <algorithm>
#include <cctype>
#include <cmath>

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

bool GetClientSize(HWND hwnd, int& width, int& height)
{
    width = 0;
    height = 0;

    if (hwnd == nullptr)
        return false;

    RECT rc{};
    if (!GetClientRect(hwnd, &rc))
        return false;

    width = rc.right - rc.left;
    height = rc.bottom - rc.top;
    return width > 0 && height > 0;
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
        _manualRotationOverride = true;

        if (_videoFlip)
        {
            g_object_set(
                G_OBJECT(_videoFlip),
                "method",
                RotationToFlipMethod(_rotationDegrees),
                nullptr);
        }
        ApplyCropToFillLocked();

        return 1;
    }

    int SetLastSampleEnabled(bool enabled)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        _lastSampleEnabled = enabled;
        if (_videoSink && g_object_class_find_property(G_OBJECT_GET_CLASS(_videoSink), "enable-last-sample"))
        {
            g_object_set(G_OBJECT(_videoSink), "enable-last-sample", enabled ? TRUE : FALSE, nullptr);
        }
        return 1;
    }

    int ForceExpose()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_videoSink)
            return 0;

        if (GST_IS_VIDEO_OVERLAY(_videoSink))
        {
            auto hwndValue = _overlayHwnd.load(std::memory_order_acquire);
            if (hwndValue != 0)
            {
                gst_video_overlay_set_window_handle(GST_VIDEO_OVERLAY(_videoSink), hwndValue);
                gst_video_overlay_handle_events(GST_VIDEO_OVERLAY(_videoSink), TRUE);
                if (_clientWidth > 0 && _clientHeight > 0)
                {
                    gst_video_overlay_set_render_rectangle(
                        GST_VIDEO_OVERLAY(_videoSink),
                        0,
                        0,
                        _clientWidth,
                        _clientHeight);
                }
                gst_video_overlay_expose(GST_VIDEO_OVERLAY(_videoSink));
                return 1;
            }
        }
        return 0;
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

    static GstPadProbeReturn OnVideoCapsProbe(GstPad* /*pad*/, GstPadProbeInfo* info, gpointer userData)
    {
        auto* self = static_cast<PlaybackEngine*>(userData);
        if (!self || !info || !(info->type & GST_PAD_PROBE_TYPE_EVENT_DOWNSTREAM))
            return GST_PAD_PROBE_OK;

        GstEvent* ev = GST_PAD_PROBE_INFO_EVENT(info);
        if (!ev || GST_EVENT_TYPE(ev) != GST_EVENT_CAPS)
            return GST_PAD_PROBE_OK;

        GstCaps* caps = nullptr;
        gst_event_parse_caps(ev, &caps);
        if (!caps)
            return GST_PAD_PROBE_OK;

        GstVideoInfo vinfo;
        if (!gst_video_info_from_caps(&vinfo, caps))
            return GST_PAD_PROBE_OK;

        std::lock_guard<std::mutex> lock(self->_mutex);
        self->_sourceWidth = GST_VIDEO_INFO_WIDTH(&vinfo);
        self->_sourceHeight = GST_VIDEO_INFO_HEIGHT(&vinfo);
        self->ApplyAutoRotationLocked();
        self->ApplyCropToFillLocked();
        return GST_PAD_PROBE_OK;
    }

    int Initialize(HWND hwnd)
    {
        std::call_once(g_gstInitFlag, []() {
            int argc = 0;
            char** argv = nullptr;
            gst_init(&argc, &argv);

            // Disable d3dvideosink globally (causing black screen on this system).
            if (GstRegistry* reg = gst_registry_get())
            {
                if (GstPluginFeature* feat = gst_registry_find_feature(reg, "d3dvideosink", GST_TYPE_ELEMENT_FACTORY))
                {
                    gst_plugin_feature_set_rank(feat, GST_RANK_NONE);
                    gst_registry_remove_feature(reg, feat);
                    gst_object_unref(feat);
                }
            }
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
        UpdateClientSizeLocked(hwnd);
        ApplyWindowCapsLocked();
        ApplyRenderRectangleUnlocked();

        if (!_pipeline || hwnd == nullptr)
            return 1;
        GstElement* sink = nullptr;
        g_object_get(G_OBJECT(_pipeline), "video-sink", &sink, nullptr);

        if (sink)
        {
            if (GST_IS_VIDEO_OVERLAY(sink))
            {
                auto* overlay = GST_VIDEO_OVERLAY(sink);
                gst_video_overlay_set_window_handle(
                    overlay,
                    reinterpret_cast<guintptr>(hwnd));
                gst_video_overlay_handle_events(overlay, TRUE);

                int width = 0;
                int height = 0;
                if (GetClientSize(hwnd, width, height))
                {
                    gst_video_overlay_set_render_rectangle(overlay, 0, 0, width, height);
                }

                gst_video_overlay_expose(overlay);
            }

        }

        return 1;
    }

    int SetWindowSize(int width, int height)
    {
        std::lock_guard<std::mutex> lock(_mutex);
        _clientWidth = width;
        _clientHeight = height;
        ApplyWindowCapsLocked();

        if (_videoCrop)
        {
            g_object_set(
                G_OBJECT(_videoCrop),
                "left", 0,
                "right", 0,
                "top", 0,
                "bottom", 0,
                nullptr);
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

        // Reset source dimensions for the newly loaded item, but DO NOT wipe a
        // user-selected/manual rotation. Otherwise playback returns to 0° every
        // time a new segment is loaded.
        _sourceWidth = 0;
        _sourceHeight = 0;

        if (_videoFlip)
        {
            g_object_set(
                G_OBJECT(_videoFlip),
                "method",
                RotationToFlipMethod(_rotationDegrees),
                nullptr);
        }

        std::string uri = FilePathToUri(path);

        gst_element_set_state(_pipeline, GST_STATE_NULL);
        g_object_set(G_OBJECT(_pipeline), "uri", uri.c_str(), nullptr);

        _currentRate = 1.0;
        _lastPath = path ? path : L"";
        _mediaLoaded = !_lastPath.empty();
        _eosReached.store(false, std::memory_order_release);
        ApplyWindowCapsLocked();
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

        // Reset source dimensions for the newly loaded item, but preserve the
        // active manual rotation across playlist segment changes.
        _sourceWidth = 0;
        _sourceHeight = 0;

        if (_videoFlip)
        {
            g_object_set(
                G_OBJECT(_videoFlip),
                "method",
                RotationToFlipMethod(_rotationDegrees),
                nullptr);
        }

        std::string uri = FilePathToUri(firstPath.c_str());

        gst_element_set_state(_pipeline, GST_STATE_NULL);
        g_object_set(G_OBJECT(_pipeline), "uri", uri.c_str(), nullptr);

        _currentRate = 1.0;
        _lastPath = firstPath;
        _mediaLoaded = true;
        _eosReached.store(false, std::memory_order_release);
        ApplyWindowCapsLocked();

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

    void CheckForGaplessTransitionLocked(double currentPos)
    {
        int current = _currentPlaylistIndex.load(std::memory_order_acquire);
        int queued = _queuedPlaylistIndex.load(std::memory_order_acquire);

        if (queued > current)
        {
            const bool switched =
                ((_lastPos - currentPos) > 0.5) ||
                (currentPos < 0.2);

            if (switched)
            {
                _currentPlaylistIndex.store(queued, std::memory_order_release);

                // Re-apply the active playback rate to the newly transitioned segment.
                // Gapless URI switching can leave the next file effectively running at 1x
                // even though _currentRate still remembers the previous requested rate.
                if (_pipeline && _mediaLoaded && std::abs(_currentRate - 1.0) > 0.001)
                {
                    gint64 pos = static_cast<gint64>(std::max(0.0, currentPos) * GST_SECOND);

                    if (!TrySeekRateLocked(_currentRate, pos))
                    {
                        // Non-fatal: keep playback alive, but surface a useful diagnostic.
                        SetErrorLocked(L"Failed to reapply playback rate after segment transition");
                    }
                }
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

        // Explicitly clear the window surface to black to prevent old frames from sticking
        if (_hwnd)
        {
            HDC hdc = GetDC(_hwnd);
            if (hdc)
            {
                RECT rect;
                GetClientRect(_hwnd, &rect);
                FillRect(hdc, &rect, (HBRUSH)GetStockObject(BLACK_BRUSH));
                ReleaseDC(_hwnd, hdc);
            }
        }

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

        if (rate < 0.25)
            rate = 0.25;
        if (rate > 4.0)
            rate = 4.0;

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

        // Do NOT early-return just because the requested rate matches _currentRate.
        // The actual stream may have transitioned to a new segment and silently fallen
        // back to normal playback, so we must reassert the rate onto the pipeline.
        if (!TrySeekRateLocked(rate, pos))
        {
            SetErrorLocked(L"Requested playback rate is not supported for the current stream");
            return 0;
        }

        _currentRate = rate;
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
    void ResetDetectedContentCropLocked()
    {
        _contentCropDetected = false;
        _contentCropLeft = 0;
        _contentCropRight = 0;
        _contentCropTop = 0;
        _contentCropBottom = 0;
    }

    static GstPadProbeReturn OnContentDetectProbe(GstPad* pad, GstPadProbeInfo* info, gpointer userData)
    {
        auto* self = static_cast<PlaybackEngine*>(userData);
        if (!self || !info || !(info->type & GST_PAD_PROBE_TYPE_BUFFER))
            return GST_PAD_PROBE_OK;

        GstBuffer* buffer = GST_PAD_PROBE_INFO_BUFFER(info);
        if (!buffer)
            return GST_PAD_PROBE_OK;

        std::lock_guard<std::mutex> lock(self->_mutex);

        if (self->_contentCropDetected)
            return GST_PAD_PROBE_REMOVE;

        GstCaps* caps = gst_pad_get_current_caps(pad);
        if (!caps)
            return GST_PAD_PROBE_OK;

        GstVideoInfo vinfo;
        if (!gst_video_info_from_caps(&vinfo, caps))
        {
            gst_caps_unref(caps);
            return GST_PAD_PROBE_OK;
        }
        gst_caps_unref(caps);

        GstVideoFrame frame;
        if (!gst_video_frame_map(&frame, &vinfo, buffer, GST_MAP_READ))
            return GST_PAD_PROBE_OK;

        const int width = GST_VIDEO_INFO_WIDTH(&vinfo);
        const int height = GST_VIDEO_INFO_HEIGHT(&vinfo);
        const int stride = GST_VIDEO_FRAME_PLANE_STRIDE(&frame, 0);
        const guint8* data = static_cast<const guint8*>(GST_VIDEO_FRAME_PLANE_DATA(&frame, 0));

        if (!data || width <= 0 || height <= 0)
        {
            gst_video_frame_unmap(&frame);
            return GST_PAD_PROBE_OK;
        }

        auto isDarkPixel = [&](int x, int y) -> bool
        {
            const guint8* px = data + y * stride + x * 4; // BGRA
            const int b = px[0];
            const int g = px[1];
            const int r = px[2];
            return r < 20 && g < 20 && b < 20;
        };

        auto columnMostlyBlack = [&](int x) -> bool
        {
            int dark = 0;
            int total = 0;
            for (int y = height / 20; y < height - (height / 20); y += 4)
            {
                ++total;
                if (isDarkPixel(x, y))
                    ++dark;
            }
            return total > 0 && (dark * 100 / total) >= 98;
        };

        auto rowMostlyBlack = [&](int y) -> bool
        {
            int dark = 0;
            int total = 0;
            for (int x = width / 20; x < width - (width / 20); x += 4)
            {
                ++total;
                if (isDarkPixel(x, y))
                    ++dark;
            }
            return total > 0 && (dark * 100 / total) >= 98;
        };

        int left = 0;
        int right = 0;
        int top = 0;
        int bottom = 0;

        const int maxSideScan = width / 3;
        const int maxTopBottomScan = height / 3;

        while (left < maxSideScan && columnMostlyBlack(left))
            ++left;

        while (right < maxSideScan && columnMostlyBlack(width - 1 - right))
            ++right;

        while (top < maxTopBottomScan && rowMostlyBlack(top))
            ++top;

        while (bottom < maxTopBottomScan && rowMostlyBlack(height - 1 - bottom))
            ++bottom;

        self->_contentCropLeft = left;
        self->_contentCropRight = right;
        self->_contentCropTop = top;
        self->_contentCropBottom = bottom;
        self->_contentCropDetected = true;

        gst_video_frame_unmap(&frame);

        self->ApplyCropToFillLocked();
        return GST_PAD_PROBE_REMOVE;
    }

    GstElement* BuildVideoFilterBin()
    {
        GstElement* bin = gst_bin_new("tsvms_video_sink_bin");
        if (!bin) return nullptr;

        GstElement* convert = gst_element_factory_make("videoconvert", "tsvms_convert");
        _videoFlip = gst_element_factory_make("videoflip", "tsvms_video_flip");
        _analysisCapsFilter = gst_element_factory_make("capsfilter", "tsvms_analysis_caps");
        _videoCrop = gst_element_factory_make("videocrop", "tsvms_video_crop");
        _videoScale = gst_element_factory_make("videoscale", "tsvms_video_scale");
        _scaleCapsFilter = gst_element_factory_make("capsfilter", "tsvms_scale_caps");

        GstElement* videoSink = gst_element_factory_make("d3d11videosink", "video-sink");
        if (!videoSink)
            videoSink = gst_element_factory_make("glimagesink", "video-sink");
        if (!videoSink)
            videoSink = gst_element_factory_make("fakesink", "video-sink");

        if (!convert || !_videoFlip || !_analysisCapsFilter || !_videoCrop || !_videoScale || !_scaleCapsFilter || !videoSink)
        {
            if (convert) gst_object_unref(convert);
            if (_videoFlip) { gst_object_unref(_videoFlip); _videoFlip = nullptr; }
            if (_analysisCapsFilter) { gst_object_unref(_analysisCapsFilter); _analysisCapsFilter = nullptr; }
            if (_videoCrop) { gst_object_unref(_videoCrop); _videoCrop = nullptr; }
            if (_videoScale) { gst_object_unref(_videoScale); _videoScale = nullptr; }
            if (_scaleCapsFilter) { gst_object_unref(_scaleCapsFilter); _scaleCapsFilter = nullptr; }
            if (videoSink) gst_object_unref(videoSink);
            gst_object_unref(bin);
            return nullptr;
        }

        GstCaps* analysisCaps = gst_caps_new_simple(
            "video/x-raw",
            "format", G_TYPE_STRING, "BGRA",
            nullptr);
        if (analysisCaps)
        {
            g_object_set(G_OBJECT(_analysisCapsFilter), "caps", analysisCaps, nullptr);
            gst_caps_unref(analysisCaps);
        }

        g_object_set(G_OBJECT(_videoFlip), "method", RotationToFlipMethod(_rotationDegrees), nullptr);

        _videoSink = videoSink;
        gst_object_ref(_videoSink);
        ApplySinkDisplayModeLocked(_videoSink);

        gst_bin_add_many(GST_BIN(bin),
            convert,
            _videoFlip,
            _analysisCapsFilter,
            _videoCrop,
            _videoScale,
            _scaleCapsFilter,
            videoSink,
            nullptr);

        gst_element_link_many(
            convert,
            _videoFlip,
            _analysisCapsFilter,
            _videoCrop,
            _videoScale,
            _scaleCapsFilter,
            videoSink,
            nullptr);

        GstPad* sinkPad = gst_element_get_static_pad(convert, "sink");
        gst_element_add_pad(bin, gst_ghost_pad_new("sink", sinkPad));
        gst_object_unref(sinkPad);

        GstPad* analysisPad = gst_element_get_static_pad(_analysisCapsFilter, "src");
        if (analysisPad)
        {
            gst_pad_add_probe(
                analysisPad,
                GST_PAD_PROBE_TYPE_EVENT_DOWNSTREAM,
                &PlaybackEngine::OnVideoCapsProbe,
                this,
                nullptr);

            gst_pad_add_probe(
                analysisPad,
                GST_PAD_PROBE_TYPE_BUFFER,
                &PlaybackEngine::OnContentDetectProbe,
                this,
                nullptr);

            gst_object_unref(analysisPad);
        }

        ApplyWindowCapsLocked();
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

        GstElement* videoSinkBin = BuildVideoFilterBin();
        if (!videoSinkBin)
        {
            // Fallback: force a direct sink to avoid playbin selecting d3dvideosink.
            GstElement* fallbackSink = gst_element_factory_make("d3d11videosink", "video-sink");
            if (!fallbackSink)
                fallbackSink = gst_element_factory_make("glimagesink", "video-sink");
            if (!fallbackSink)
                fallbackSink = gst_element_factory_make("fakesink", "video-sink");

            if (fallbackSink)
            {
                g_object_set(G_OBJECT(_pipeline), "video-sink", fallbackSink, nullptr);
                _videoSink = fallbackSink;
                gst_object_ref(_videoSink);
            }
        }
        else
        {
            g_object_set(G_OBJECT(_pipeline), "video-sink", videoSinkBin, nullptr);
        }
        ApplyWindowCapsLocked();

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
            // --- FIX: Stop pipeline and unset bus sync handler BEFORE destruction ---
            gst_element_set_state(_pipeline, GST_STATE_NULL);

            GstBus* bus = gst_element_get_bus(_pipeline);
            if (bus)
            {
                gst_bus_set_sync_handler(bus, nullptr, nullptr, nullptr);
                gst_object_unref(bus);
            }

            // --- FIX: Explicitly release the window handle from the overlay ---
            if (_videoSink && GST_IS_VIDEO_OVERLAY(_videoSink))
            {
                gst_video_overlay_set_window_handle(GST_VIDEO_OVERLAY(_videoSink), 0);
            }

            gst_object_unref(_pipeline);
            _pipeline = nullptr;
        }

        if (_videoSink)
        {
            gst_object_unref(_videoSink);
            _videoSink = nullptr;
        }

        if (_videoCrop)
        {
            _videoCrop = nullptr;
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
                std::lock_guard<std::mutex> lock(self->_mutex);
                self->UpdateClientSizeLocked(reinterpret_cast<HWND>(hwndValue));
                GstElement* sink = GST_ELEMENT(GST_MESSAGE_SRC(message));
                if (sink && GST_IS_VIDEO_OVERLAY(sink))
                {
                    self->ApplySinkDisplayModeLocked(sink);
                    GstVideoOverlay* overlay = GST_VIDEO_OVERLAY(sink);
                    gst_video_overlay_set_window_handle(overlay, hwndValue);
                    gst_video_overlay_handle_events(overlay, TRUE);
                    if (self->_clientWidth > 0 && self->_clientHeight > 0)
                    {
                        gst_video_overlay_set_render_rectangle(overlay, 0, 0, self->_clientWidth, self->_clientHeight);
                    }

                    gst_video_overlay_expose(overlay);
                }
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
        else if (GST_MESSAGE_TYPE(message) == GST_MESSAGE_TAG)
        {
            GstTagList* tags = nullptr;
            gst_message_parse_tag(message, &tags);
            if (tags)
            {
                std::lock_guard<std::mutex> lock(self->_mutex);
                self->ApplyOrientationFromTagsLocked(tags);
                gst_tag_list_unref(tags);
            }
        }

        return GST_BUS_PASS;
    }

    void SetErrorLocked(const std::wstring& message)
    {
        std::lock_guard<std::mutex> lock(_errorMutex);
        _lastError = message;
    }

    void UpdateClientSizeLocked(HWND hwnd)
    {
        int width = 0;
        int height = 0;
        if (GetClientSize(hwnd, width, height))
        {
            _clientWidth = width;
            _clientHeight = height;
        }
    }

    void ApplyRenderRectangleUnlocked()
    {
        if (_hwnd == nullptr || _clientWidth <= 0 || _clientHeight <= 0)
            return;

        GstElement* sink = _videoSink;
        if (!sink)
            return;

        ApplyCropToFillLocked();
        ApplySinkDisplayModeLocked(sink);

        if (GST_IS_VIDEO_OVERLAY(sink))
        {
            gst_video_overlay_set_render_rectangle(
                GST_VIDEO_OVERLAY(sink),
                0,
                0,
                _clientWidth,
                _clientHeight);
        }
    }

    void ApplySinkDisplayModeLocked(GstElement* sink)
    {
        if (!sink)
            return;

        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sink), "force-aspect-ratio"))
        {
            g_object_set(G_OBJECT(sink), "force-aspect-ratio", FALSE, nullptr);
        }

        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sink), "add-borders"))
        {
            g_object_set(G_OBJECT(sink), "add-borders", FALSE, nullptr);
        }

        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sink), "redraw-on-update"))
        {
            g_object_set(G_OBJECT(sink), "redraw-on-update", TRUE, nullptr);
        }

        if (g_object_class_find_property(G_OBJECT_GET_CLASS(sink), "enable-last-sample"))
        {
            g_object_set(G_OBJECT(sink), "enable-last-sample", _lastSampleEnabled ? TRUE : FALSE, nullptr);
        }
    }

    void ApplyCropToFillLocked()
    {
        if (!_cropToFill)
        {
            if (_videoCrop)
            {
                g_object_set(
                    G_OBJECT(_videoCrop),
                    "left", 0,
                    "right", 0,
                    "top", 0,
                    "bottom", 0,
                    nullptr);
            }
            return;
        }
        if (!_videoCrop || _clientWidth <= 0 || _clientHeight <= 0)
            return;
        if (_sourceWidth <= 0 || _sourceHeight <= 0)
            return;

        int left = _contentCropLeft;
        int right = _contentCropRight;
        int top = _contentCropTop;
        int bottom = _contentCropBottom;

        double srcW = static_cast<double>(_sourceWidth - left - right);
        double srcH = static_cast<double>(_sourceHeight - top - bottom);

        if (srcW <= 1.0 || srcH <= 1.0)
            return;

        double dstW = static_cast<double>(_clientWidth);
        double dstH = static_cast<double>(_clientHeight);

        double srcAspect = srcW / srcH;
        double dstAspect = dstW / dstH;

        if (std::abs(srcAspect - dstAspect) > 0.001)
        {
            if (srcAspect > dstAspect)
            {
                double targetW = srcH * dstAspect;
                double extraCrop = std::max(0.0, srcW - targetW);
                int extra = static_cast<int>(std::round(extraCrop));
                left += extra / 2;
                right += extra - (extra / 2);
            }
            else
            {
                double targetH = srcW / dstAspect;
                double extraCrop = std::max(0.0, srcH - targetH);
                int extra = static_cast<int>(std::round(extraCrop));
                top += extra / 2;
                bottom += extra - (extra / 2);
            }
        }

        g_object_set(
            G_OBJECT(_videoCrop),
            "left", left,
            "right", right,
            "top", top,
            "bottom", bottom,
            nullptr);
    }

    void ApplyAutoRotationLocked()
    {
        // Do not auto-rotate based on panel aspect ratio.
        // Only manual rotation and real stream orientation tags should affect playback.
    }

    void ApplyWindowCapsLocked()
    {
        if (!_scaleCapsFilter || _clientWidth <= 0 || _clientHeight <= 0)
            return;

        GstCaps* caps = gst_caps_new_simple(
            "video/x-raw",
            "width", G_TYPE_INT, _clientWidth,
            "height", G_TYPE_INT, _clientHeight,
            "pixel-aspect-ratio", GST_TYPE_FRACTION, 1, 1,
            nullptr);

        if (caps)
        {
            g_object_set(G_OBJECT(_scaleCapsFilter), "caps", caps, nullptr);
            gst_caps_unref(caps);
        }

        ApplyRenderRectangleUnlocked();
    }

    void ApplyOrientationFromTagsLocked(GstTagList* tags)
    {
        if (_manualRotationOverride || !tags)
            return;

        gchar* orientation = nullptr;
        if (!gst_tag_list_get_string(tags, "image-orientation", &orientation) || !orientation)
        {
            if (!gst_tag_list_get_string(tags, "video-orientation", &orientation) || !orientation)
                return;
        }

        std::string value = orientation;
        g_free(orientation);

        std::transform(value.begin(), value.end(), value.begin(),
            [](unsigned char ch) { return static_cast<char>(std::tolower(ch)); });

        int degrees = 0;
        if (value.find("rotate-180") != std::string::npos)
            degrees = 180;
        else if (value.find("rotate-270") != std::string::npos)
            degrees = 270;
        else if (value.find("rotate-90") != std::string::npos)
            degrees = 90;
        else
            return;

        if (degrees == _rotationDegrees)
            return;

        _rotationDegrees = degrees;
        if (_videoFlip)
        {
            g_object_set(
                G_OBJECT(_videoFlip),
                "method",
                RotationToFlipMethod(_rotationDegrees),
                nullptr);
        }
        ApplyCropToFillLocked();
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
    GstElement* _videoCrop = nullptr;
    GstElement* _videoScale = nullptr;
    GstElement* _scaleCapsFilter = nullptr;
    GstElement* _videoSink = nullptr;
    int _rotationDegrees = 0;
    bool _manualRotationOverride = false;
    bool _lastSampleEnabled = true;
    bool _cropToFill = false;
    GstElement* _analysisCapsFilter = nullptr;

    bool _contentCropDetected = false;
    int _contentCropLeft = 0;
    int _contentCropRight = 0;
    int _contentCropTop = 0;
    int _contentCropBottom = 0;

    int _clientWidth = 0;
    int _clientHeight = 0;
    int _sourceWidth = 0;
    int _sourceHeight = 0;
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

TSVMS_PLAYBACK_API int tsplay_set_window_size(void* engine, int width, int height)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SetWindowSize(width, height);
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

TSVMS_PLAYBACK_API int tsplay_set_last_sample_enabled(void* engine, int enabled)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->SetLastSampleEnabled(enabled != 0);
}

TSVMS_PLAYBACK_API int tsplay_force_expose(void* engine)
{
    if (!engine) return 0;
    return static_cast<PlaybackEngine*>(engine)->ForceExpose();
}

TSVMS_PLAYBACK_API double tsplay_get_rate(void* engine)
{
    if (!engine) return 1.0;
    return static_cast<PlaybackEngine*>(engine)->GetRate();
}

}
