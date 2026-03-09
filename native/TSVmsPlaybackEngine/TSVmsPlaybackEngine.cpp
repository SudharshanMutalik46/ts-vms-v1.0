#include "TSVmsPlaybackEngine.h"

#include <gst/gst.h>
#include <gst/video/videooverlay.h>
#include <gst/video/video.h>

#include <string>
#include <mutex>
#include <atomic>
#include <memory>

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

        std::string uri = FilePathToUri(path);
        gst_element_set_state(_pipeline, GST_STATE_READY);
        g_object_set(G_OBJECT(_pipeline), "uri", uri.c_str(), nullptr);

        _currentRate = 1.0;
        _lastPath = path ? path : L"";
        _mediaLoaded = !_lastPath.empty();

        return _mediaLoaded ? 1 : 0;
    }

    int Play()
    {
        std::lock_guard<std::mutex> lock(_mutex);

        if (!_pipeline || !_mediaLoaded)
        {
            SetErrorLocked(L"Load a recorded segment first");
            return 0;
        }

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
        gst_element_set_state(_pipeline, GST_STATE_READY);
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

        // IMPORTANT:
        // Seek is unreliable from READY.
        // Move pipeline to PAUSED first so the stream prerolls and becomes seekable.
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

        // Clamp seek position against known duration if available
        gint64 dur = 0;
        if (gst_element_query_duration(_pipeline, GST_FORMAT_TIME, &dur) && dur > 0)
        {
            double durSec = static_cast<double>(dur) / GST_SECOND;
            if (durSec > 0.25 && seconds > durSec - 0.25)
                seconds = durSec - 0.25;
        }

        gint64 pos = static_cast<gint64>(seconds * GST_SECOND);

        // First attempt: accurate seek
        gboolean ok = gst_element_seek(
            _pipeline,
            _currentRate,
            GST_FORMAT_TIME,
            static_cast<GstSeekFlags>(GST_SEEK_FLAG_FLUSH | GST_SEEK_FLAG_ACCURATE | GST_SEEK_FLAG_KEY_UNIT),
            GST_SEEK_TYPE_SET, pos,
            GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);

        // Fallback: simpler seek if accurate seek fails on this stream/container
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

        return 1;
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

        GstState state = GST_STATE_NULL;
        GstState pending = GST_STATE_VOID_PENDING;
        gst_element_get_state(_pipeline, &state, &pending, 200 * GST_MSECOND);

        if (state < GST_STATE_PAUSED)
        {
            gst_element_set_state(_pipeline, GST_STATE_PAUSED);
            gst_element_get_state(_pipeline, &state, &pending, 500 * GST_MSECOND);
        }

        gint64 pos = 0;
        if (!gst_element_query_position(_pipeline, GST_FORMAT_TIME, &pos))
            pos = 0;

        gboolean ok = gst_element_seek(
            _pipeline,
            rate,
            GST_FORMAT_TIME,
            static_cast<GstSeekFlags>(GST_SEEK_FLAG_FLUSH | GST_SEEK_FLAG_ACCURATE),
            GST_SEEK_TYPE_SET, pos,
            GST_SEEK_TYPE_NONE, GST_CLOCK_TIME_NONE);

        if (!ok)
        {
            SetErrorLocked(L"Requested playback speed is not supported for the current stream");
            return 0;
        }

        _currentRate = rate;
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

        // Forward frame step via Gst step event
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

        // Backward step fallback: tiny accurate seek backward
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

    double GetPositionSeconds()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        if (!_pipeline) return 0.0;
        gint64 pos = 0;
        if (!gst_element_query_position(_pipeline, GST_FORMAT_TIME, &pos))
            return 0.0;
        return static_cast<double>(pos) / GST_SECOND;
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

    const wchar_t* GetLastError()
    {
        std::lock_guard<std::mutex> lock(_mutex);
        return _lastError.c_str();
    }

private:
    // -----------------------------------------------------------------------
    // FIX 1: Build a video-filter bin that safely bridges D3D11 GPU memory
    // to the CPU before videoflip processes it.
    //
    // ROOT CAUSE:
    //   playbin auto-selects d3d11h265dec for H.265/HEVC streams. That decoder
    //   outputs video/x-raw(memory:D3D11Memory) — frames sitting on the GPU.
    //   videoflip only accepts plain system-memory frames. Without a download
    //   step the caps negotiation fails, d3d11h265dec cannot allocate its output
    //   buffer pool, and gst_element_set_state(PAUSED) hangs the calling thread
    //   long enough to trigger the Windows "Not Responding" dialog.
    //
    // FIX:
    //   Wrap the filter as a GstBin:  d3d11download -> videoconvert -> videoflip
    //   d3d11download copies D3D11Memory frames to system RAM (zero-copy fast
    //   path when possible). videoconvert ensures any residual format mismatch
    //   is resolved. videoflip then works on plain NV12/I420 system frames.
    //   If d3d11download is not available (older GStreamer build) we fall back
    //   to videoconvert alone, which forces a software decode path and is still
    //   correct, just slightly slower.
    // -----------------------------------------------------------------------
    GstElement* BuildVideoFilterBin()
    {
        GstElement* bin = gst_bin_new("tsvms_video_filter_bin");
        if (!bin) return nullptr;

        // Try to get d3d11download; it may not exist on every GStreamer install.
        GstElement* d3d11dl = gst_element_factory_make("d3d11download", "tsvms_d3d11dl");

        // videoconvert resolves any remaining pixel-format mismatch after download.
        GstElement* convert = gst_element_factory_make("videoconvert", "tsvms_convert");

        _videoFlip = gst_element_factory_make("videoflip", "tsvms_video_flip");

        if (!convert || !_videoFlip)
        {
            // Extremely unlikely but clean up and return null so playbin
            // uses its built-in pipeline (no rotation, but at least it plays).
            if (d3d11dl) gst_object_unref(d3d11dl);
            if (convert) gst_object_unref(convert);
            if (_videoFlip) { gst_object_unref(_videoFlip); _videoFlip = nullptr; }
            gst_object_unref(bin);
            return nullptr;
        }

        g_object_set(G_OBJECT(_videoFlip), "method", RotationToFlipMethod(_rotationDegrees), nullptr);

        if (d3d11dl)
        {
            // Full chain: d3d11download -> videoconvert -> videoflip
            gst_bin_add_many(GST_BIN(bin), d3d11dl, convert, _videoFlip, nullptr);
            if (!gst_element_link_many(d3d11dl, convert, _videoFlip, nullptr))
            {
                // Link failed — fall back to the two-element chain below.
                gst_bin_remove_many(GST_BIN(bin), d3d11dl, convert, _videoFlip, nullptr);
                gst_object_unref(d3d11dl);
                d3d11dl = nullptr;
                gst_bin_add_many(GST_BIN(bin), convert, _videoFlip, nullptr);
                gst_element_link(convert, _videoFlip);
            }
        }
        else
        {
            // d3d11download unavailable: videoconvert -> videoflip only.
            // GStreamer will insert a software decoder upstream automatically.
            gst_bin_add_many(GST_BIN(bin), convert, _videoFlip, nullptr);
            gst_element_link(convert, _videoFlip);
        }

        // Expose sink ghost pad (entry point of the bin).
        GstElement* firstElem = d3d11dl ? d3d11dl : convert;
        GstPad* sinkPad = gst_element_get_static_pad(firstElem, "sink");
        gst_element_add_pad(bin, gst_ghost_pad_new("sink", sinkPad));
        gst_object_unref(sinkPad);

        // Expose src ghost pad (exit point of the bin).
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

        // FIX 1 applied here: use the safe filter bin instead of bare videoflip.
        GstElement* filterBin = BuildVideoFilterBin();
        if (filterBin)
        {
            g_object_set(G_OBJECT(_pipeline), "video-filter", filterBin, nullptr);
        }

        // Playback-specific sink strategy:
        // Use Direct3D 11 sink as primary renderer to match Live View and 
        // ensure stable docking into WPF HwndHost.
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
                g_object_set(G_OBJECT(videoSink), "sync", FALSE, nullptr);
            }

            g_object_set(G_OBJECT(_pipeline), "video-sink", videoSink, nullptr);
        }

        GstBus* bus = gst_element_get_bus(_pipeline);
        gst_bus_set_sync_handler(bus, &PlaybackEngine::BusSyncHandler, this, nullptr);
        gst_object_unref(bus);
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
        // _videoFlip is owned by the bin / pipeline; do not unref separately.
        _videoFlip = nullptr;
    }

    static GstBusSyncReply BusSyncHandler(GstBus*, GstMessage* message, gpointer userData)
    {
        auto* self = static_cast<PlaybackEngine*>(userData);
        if (!self) return GST_BUS_PASS;

        if (gst_is_video_overlay_prepare_window_handle_message(message))
        {
            // Do not lock _mutex here. State changes (Play/Pause/Load) hold _mutex
            // while GStreamer can synchronously emit prepare-window-handle on the
            // same thread; taking _mutex again can deadlock the UI.
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

        if (GST_MESSAGE_TYPE(message) == GST_MESSAGE_ERROR)
        {
            GError* err = nullptr;
            gchar* dbg = nullptr;
            gst_message_parse_error(message, &err, &dbg);
            if (err)
            {
                std::lock_guard<std::mutex> lock(self->_mutex);
                self->_lastError = Utf8ToWide(err->message);
                g_error_free(err);
            }
            if (dbg) g_free(dbg);
        }

        return GST_BUS_PASS;
    }

    void SetErrorLocked(const std::wstring& message)
    {
        _lastError = message;
    }

private:
    std::mutex _mutex;
    GstElement* _pipeline = nullptr;
    HWND _hwnd = nullptr;
    std::atomic<guintptr> _overlayHwnd { 0 };
    double _currentRate = 1.0;
    std::wstring _lastError;
    std::wstring _lastPath;
    bool _mediaLoaded = false;
    GstElement* _videoFlip = nullptr;
    int _rotationDegrees = 0;
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

}
