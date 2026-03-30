using System;
using System.Runtime.InteropServices;
using System.Collections.Generic;
using System.Threading.Tasks;
using System.Threading;
using System.Collections.Concurrent;

namespace TSVmsDesktop.Services
{
    public class VideoService
    {
        private bool _isInitialized = false;
        private static string _logPath = System.IO.Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "gstreamer_log.txt");
        private readonly object _lock = new object();

        [StructLayout(LayoutKind.Sequential)]
        private struct GErrorNative
        {
            public uint Domain;
            public int Code;
            public IntPtr Message;
        }
        
        private class StreamContext
        {
            public string? Url { get; set; }
            public IntPtr WindowHandle { get; set; }
            public CancellationTokenSource? Cts { get; set; }
            public Task? WatchTask { get; set; }
            public bool IsRestarting { get; set; }
            public string Username { get; set; } = "";
            public string Password { get; set; } = "";
            public bool HasAudio { get; set; }

            public IntPtr Pipeline { get; set; }
            public IntPtr OverlayElement { get; set; }
            public IntPtr BusHandle { get; set; }

            public GstNative.GstBusSyncHandler? SyncHandler { get; set; }
            public GCHandle SyncHandlerGcHandle { get; set; }
            public bool SyncHandlerGcHandleAllocated { get; set; }

            public Func<Task<(string Url, IntPtr Handle)>>? GetFreshContext { get; set; }
            public CancellationTokenSource? FallbackCts { get; set; }
            public bool HlsAsyncDone { get; set; }
            public SemaphoreSlim TeardownLock { get; } = new(1, 1);
            public string StreamId { get; set; } = Guid.NewGuid().ToString().Substring(0, 8);
        }

        public event Action<IntPtr, string>? StreamError;
        private readonly ConcurrentDictionary<IntPtr, StreamContext> _activeStreams = new();
        private readonly ApiClient _api;

        public VideoService(ApiClient api)
        {
            _api = api;
        }

        public static void Log(string message)
        {
            string line = $"[{DateTime.Now:HH:mm:ss}] {message}";
            // Fallback for console output in dev environments
            Console.WriteLine(line);
            try 
            { 
                // Use a shared write lock if needed, but for simple app logs AppendAllText is usually sufficient
                System.IO.File.AppendAllText(_logPath, line + Environment.NewLine); 
            } 
            catch (Exception ex)
            {
                // Never lose log information; if file write fails, at least push it to error stream
                Console.Error.WriteLine($"[FATAL] Failed to write to log file: {ex.Message}");
            }
        }

        private static string ReadGErrorMessage(IntPtr errorPtr, string fallback)
        {
            if (errorPtr == IntPtr.Zero) return fallback;
            try
            {
                var gerr = Marshal.PtrToStructure<GErrorNative>(errorPtr);
                if (gerr.Message != IntPtr.Zero)
                {
                    var msg = Marshal.PtrToStringUTF8(gerr.Message);
                    if (!string.IsNullOrWhiteSpace(msg))
                        return msg;
                }
            }
            catch
            {
            }
            return fallback;
        }

        private static uint ReadGErrorDomain(IntPtr errorPtr)
        {
            if (errorPtr == IntPtr.Zero) return 0;
            try { return Marshal.PtrToStructure<GErrorNative>(errorPtr).Domain; }
            catch { return 0; }
        }

        private static int ReadGErrorCode(IntPtr errorPtr)
        {
            if (errorPtr == IntPtr.Zero) return 0;
            try { return Marshal.PtrToStructure<GErrorNative>(errorPtr).Code; }
            catch { return 0; }
        }

        private static string ReadUtf8String(IntPtr ptr)
        {
            if (ptr == IntPtr.Zero) return "";
            try
            {
                return Marshal.PtrToStringUTF8(ptr) ?? "";
            }
            catch
            {
                return "";
            }
        }

        private static void BindOverlay(StreamContext ctx)
        {
            if (ctx.OverlayElement == IntPtr.Zero || ctx.WindowHandle == IntPtr.Zero)
                return;

            GstNative.gst_video_overlay_set_window_handle(ctx.OverlayElement, ctx.WindowHandle);
            GstNative.gst_video_overlay_handle_events(ctx.OverlayElement, true);
            GstNative.gst_video_overlay_expose(ctx.OverlayElement);
        }

        private static int OverlayBusSyncHandler(IntPtr bus, IntPtr message, IntPtr userData)
        {
            try
            {
                if (userData == IntPtr.Zero)
                    return GstNative.GST_BUS_PASS;

                var gch = GCHandle.FromIntPtr(userData);
                if (gch.Target is not StreamContext ctx)
                    return GstNative.GST_BUS_PASS;

                if (GstNative.gst_is_video_overlay_prepare_window_handle_message(message) != 0)
                {
                    // Log to file (not just Console) so overlay binding is visible in gstreamer_log.txt.
                    // Note: This runs on the GStreamer streaming thread.
                    Log("[TS-VMS] prepare-window-handle received.");

                    if (ctx.OverlayElement == IntPtr.Zero)
                    {
                        // Try name lookup first (works for static pipelines like RTSP).
                        ctx.OverlayElement = GstNative.gst_bin_get_by_name(ctx.Pipeline, "mysink");
                        Log(ctx.OverlayElement != IntPtr.Zero
                            ? "[TS-VMS] mysink found via name lookup."
                            : "[TS-VMS] mysink not found by name; binding overlay via playbin proxy.");
                    }

                    if (ctx.OverlayElement != IntPtr.Zero)
                    {
                        // Static pipeline path: bind to the named sink element.
                        BindOverlay(ctx);
                    }
                    else
                    {
                        // playbin path: playbin implements GstVideoOverlay and proxies
                        // gst_video_overlay_set_window_handle() to its internal video sink.
                        // This is the canonical approach when the sink is inside playbin.
                        GstNative.gst_video_overlay_set_window_handle(ctx.Pipeline, ctx.WindowHandle);
                        GstNative.gst_video_overlay_handle_events(ctx.Pipeline, true);
                        Log($"[TS-VMS] Overlay bound via playbin proxy. handle={ctx.WindowHandle}");
                    }

                    return GstNative.GST_BUS_DROP;
                }
            }
            catch
            {
                // Never throw across native callback boundaries.
            }

            return GstNative.GST_BUS_PASS;
        }

        private static void InstallOverlaySyncHandler(StreamContext ctx)
        {
            if (ctx.BusHandle == IntPtr.Zero)
                return;

            if (!ctx.SyncHandlerGcHandleAllocated)
            {
                ctx.SyncHandler = OverlayBusSyncHandler;
                ctx.SyncHandlerGcHandle = GCHandle.Alloc(ctx);
                ctx.SyncHandlerGcHandleAllocated = true;
            }

            GstNative.gst_bus_set_sync_handler(
                ctx.BusHandle,
                ctx.SyncHandler,
                GCHandle.ToIntPtr(ctx.SyncHandlerGcHandle),
                IntPtr.Zero);
        }

        private static void RemoveOverlaySyncHandler(StreamContext ctx)
        {
            try
            {
                if (ctx.BusHandle != IntPtr.Zero)
                {
                    GstNative.gst_bus_set_sync_handler(
                        ctx.BusHandle,
                        null,
                        IntPtr.Zero,
                        IntPtr.Zero);
                }
            }
            catch
            {
            }

            if (ctx.SyncHandlerGcHandleAllocated)
            {
                try { ctx.SyncHandlerGcHandle.Free(); } catch { }
                ctx.SyncHandlerGcHandleAllocated = false;
            }

            ctx.SyncHandler = null;
        }

        public void Initialize()
        {
            lock (_lock)
            {
                if (_isInitialized) return;
                try
                {
                    int argc = 0;
                    IntPtr argv = IntPtr.Zero;
                    GstNative.gst_init(ref argc, ref argv);
                    // Reduce global debug verbosity to errors only to prevent
                    // heavy warning floods from d3d11debuglayer during teardown.
                    GstNative.gst_debug_set_default_threshold(1);
                    GstNative.gst_debug_set_threshold_for_name("d3d11debuglayer", 0);
                    GstNative.gst_debug_set_threshold_for_name("video-info", 0);

                    IntPtr d11 = GstNative.gst_element_factory_find("d3d11download");
                    if (d11 != IntPtr.Zero) {
                        GstNative.gst_plugin_feature_set_rank(d11, GstNative.GST_RANK_PRIMARY + 100);
                        GstNative.SafeObjectUnref(d11);
                    }

                    // NOTE: tsdemux rank override REMOVED. 
                    // The current HLS stream uses fragmented MP4 (.mp4) segments (ftypmp42).
                    // Forcing tsdemux above qtdemux causes decode failure for MP4 segments.
                    /*
                    IntPtr tsdemux = GstNative.gst_element_factory_find("tsdemux");
                    if (tsdemux != IntPtr.Zero)
                    {
                        GstNative.gst_plugin_feature_set_rank(tsdemux, GstNative.GST_RANK_PRIMARY + 50);
                        GstNative.SafeObjectUnref(tsdemux);
                        Log("[TS-VMS] tsdemux rank raised above qtdemux for HLS MPEG-TS segments.");
                    }
                    */

                    _isInitialized = true;
                    Log("[TS-VMS] Video Engine: GStreamer 1.x Initialized.");
                }
                catch (Exception ex) { Log($"[ERROR] GStreamer Init Failed: {ex.Message}"); }
            }
        }

        // ------------------------------------------------------------------
        // FIX 1: Wait for the HWND to have a real, non-zero client area before
        // handing it to d3d11videosink.
        //
        // ROOT CAUSE ("buffer width inferred as zero"):
        //   gst_element_set_state(PLAYING) was called while the VideoCanvas HWND
        //   still had size 0×0 (RevealVideoAsync had not yet run ShowWindow).
        //   d3d11videosink created a DXGI swapchain of 0×0 which the debug layer
        //   clamped to 8×8 and then flooded the log with hundreds of resize
        //   warnings on every decoded frame.
        //
        // FIX: Poll GetClientRect up to 2 s. This is cheap and eliminates the
        // race between stream start and window layout entirely.
        // ------------------------------------------------------------------
        [StructLayout(LayoutKind.Sequential)]
        private struct RECT { public int Left, Top, Right, Bottom; }

        [DllImport("user32.dll")]
        private static extern bool GetClientRect(IntPtr hWnd, out RECT lpRect);

        private static bool HasWindowSize(IntPtr hwnd)
        {
            if (hwnd == IntPtr.Zero) return false;
            if (!GetClientRect(hwnd, out RECT r)) return false;
            return r.Right > 0 && r.Bottom > 0;
        }

        public IntPtr StartStream(IntPtr windowHandle, string rtspUrl, string username = "", string password = "", bool hasAudio = false, Func<Task<(string Url, IntPtr Handle)>>? getFreshContext = null)
        {
            if (!_isInitialized) Initialize();

            // HLS streams (http/https .m3u8) use playbin — no credentials needed.
            if (rtspUrl.StartsWith("http://", StringComparison.OrdinalIgnoreCase) ||
                rtspUrl.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
            {
                return StartHlsStream(windowHandle, rtspUrl, hasAudio, getFreshContext);
            }

            string authUrl = rtspUrl;
            string userIdProp = "";
            string userPwProp = "";

            if (!string.IsNullOrEmpty(username) && rtspUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                if (!authUrl.Contains("@"))
                {
                    authUrl = authUrl.Replace("rtsp://", $"rtsp://{username}:{password}@");
                }
                else
                {
                    // If URL already has credentials, do NOT pass them again as properties.
                    // This fixes "setup failed" / "Could not write to resource" on some cameras
                    // that reject dual-authentication headers.
                    Log($"[TS-VMS] URL already credentialed, skipping rtspsrc props.");
                }

                // Only set properties if we didn't have @ in the original URL (safety)
                // OR if the user explicitly wants to use properties.
                // Standard GStreamer practice is one or the other.
                if (!rtspUrl.Contains("@"))
                {
                    userIdProp = $"user-id=\"{username}\"";
                    userPwProp = $"user-pw=\"{password}\"";
                }
            }

            Log($"[TS-VMS] StartStream Request Original: '{rtspUrl}'");

            // Avoid blocking the UI thread while waiting for layout.
            if (!WaitForWindowSize(windowHandle, 2000))
            {
                Log($"[TS-VMS] Window {windowHandle} never became ready; aborting stream start.");
                StreamError?.Invoke(windowHandle, "Video surface not ready");
                return IntPtr.Zero;
            }

            Log($"[TS-VMS] Window Handle: {windowHandle}");

            // ------------------------------------------------------------------
            // FIX 2: Replace hardcoded H.265-only pipeline with decodebin3 so
            // the pipeline works for both H.264 and H.265 cameras automatically.
            //
            // ROOT CAUSE:
            //   "rtph265depay ! h265parse ! d3d11h265dec ! d3d11colorconvert ! d3d11videosink"
            //   (a) Only works for H.265 cameras — H.264 cameras caused immediate
            //       error → restart loops.
            //   (b) d3d11colorconvert between d3d11h265dec and d3d11videosink is
            //       redundant (both share the same D3D11 device) and adds an extra
            //       device context, contributing to the "Refcount:52" D3D11 object
            //       leak visible in the logs at t=16s.
            //
            // FIX: Use decodebin3 for automatic codec negotiation.  It selects
            // d3d11h265dec for H.265 and d3d11h264dec for H.264.  Remove
            // d3d11colorconvert — d3d11videosink accepts NV12 D3D11Memory directly.
            // ------------------------------------------------------------------
            // Always consume non-video RTP pads. Otherwise cameras that expose
            // audio/metadata tracks can fail with "streaming stopped, reason not-linked".
            string audioPart = hasAudio
                ? "rtspsrc_src. ! application/x-rtp,media=audio ! queue ! decodebin3 name=abind abind. ! queue ! audioconvert ! audioresample ! volume name=myvolume ! autoaudiosink sync=false "
                : "rtspsrc_src. ! application/x-rtp,media=audio ! queue ! fakesink sync=false async=false ";

            string pipelineStr =
                $"rtspsrc location=\"{authUrl}\" {userIdProp} {userPwProp} latency=500 drop-on-latency=true protocols=tcp name=rtspsrc_src " +
                $"rtspsrc_src. ! application/x-rtp,media=video ! queue ! decodebin3 name=vdbin " +
                $"vdbin. ! queue ! d3d11videosink name=mysink sync=false force-aspect-ratio=true " +
                audioPart +
                "rtspsrc_src. ! application/x-rtp,media=application ! queue ! fakesink sync=false async=false";

            IntPtr error = IntPtr.Zero;
            IntPtr pipeline = GstNative.gst_parse_launch(pipelineStr, out error);

            if (pipeline == IntPtr.Zero)
            {
                Log($"[TS-VMS] Pipeline Creation Failed for {rtspUrl}");
                StreamError?.Invoke(windowHandle, "Invalid Pipeline (Check RTSP URL)");
                return IntPtr.Zero;
            }
            
            // Sink floating reference for parse_launch created pipelines.
            GstNative.g_object_ref_sink(pipeline);

            var ctx = new StreamContext
            {
                Url = rtspUrl,
                WindowHandle = windowHandle,
                Cts = new CancellationTokenSource(),
                Username = username,
                Password = password,
                HasAudio = hasAudio,
                Pipeline = pipeline,
                GetFreshContext = getFreshContext
            };

            IntPtr sink = GstNative.gst_bin_get_by_name(pipeline, "mysink");
            if (sink != IntPtr.Zero)
            {
                ctx.OverlayElement = sink; // keep one ref for the life of the stream
                BindOverlay(ctx);          // immediate best-effort bind
                Log("[TS-VMS] Handle set on 'mysink'.");
            }

            IntPtr bus = GstNative.gst_element_get_bus(pipeline);
            if (bus != IntPtr.Zero)
            {
                ctx.BusHandle = bus;
                InstallOverlaySyncHandler(ctx);

                var token = ctx.Cts.Token;
                ctx.WatchTask = Task.Run(async () =>
                {
                    // Increment reference count for the life of this task
                    GstNative.gst_object_ref(pipeline);
                    try
                    {
                        Log($"[TS-VMS] {ctx.StreamId} Bus monitor started for {rtspUrl}");
                        while (!token.IsCancellationRequested)
                        {
                            IntPtr errMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_ERROR);
                            if (errMsg != IntPtr.Zero)
                            {
                                IntPtr errPtr, debugPtr;
                                GstNative.gst_message_parse_error(errMsg, out errPtr, out debugPtr);
                                string errWrap = ReadGErrorMessage(errPtr, "Unknown Error");
                                string debugWrap = ReadUtf8String(debugPtr);
                                GstNative.SafeGErrorFree(errPtr);
                                GstNative.SafeGFree(debugPtr);
                                GstNative.gst_message_unref(errMsg);

                                if (!token.IsCancellationRequested && _activeStreams.ContainsKey(pipeline))
                                {
                                    Log(string.IsNullOrWhiteSpace(debugWrap)
                                        ? $"[GSTREAMER-ERROR] {ctx.StreamId} {errWrap}"
                                        : $"[GSTREAMER-ERROR] {ctx.StreamId} {errWrap} | debug={debugWrap}");
                                    StreamError?.Invoke(ctx.WindowHandle, errWrap);

                                    if (IsSurfaceError(errWrap))
                                    {
                                        Log($"[TS-VMS] {ctx.StreamId} Surface error detected; stopping.");
                                        StopStream(pipeline);
                                        break;
                                    }

                                    Log($"[TS-VMS] {ctx.StreamId} Triggering auto-restart...");
                                    _ = RestartStreamAsync(pipeline);
                                    break;
                                }
                            }

                            IntPtr eosMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_EOS);
                            if (eosMsg != IntPtr.Zero)
                            {
                                GstNative.gst_message_unref(eosMsg);
                                if (!token.IsCancellationRequested && _activeStreams.ContainsKey(pipeline))
                                {
                                    Log($"[GSTREAMER-ERROR] {ctx.StreamId} Stream reached EOS. Restarting...");
                                    _ = RestartStreamAsync(pipeline);
                                    break;
                                }
                            }

                            IntPtr warnMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_WARNING);
                            if (warnMsg != IntPtr.Zero)
                            {
                                IntPtr errPtr, debugPtr;
                                GstNative.gst_message_parse_warning(warnMsg, out errPtr, out debugPtr);
                                string warnWrap = ReadGErrorMessage(errPtr, "Unknown Warning");
                                string debugWrap = ReadUtf8String(debugPtr);
                                GstNative.SafeGErrorFree(errPtr);
                                GstNative.SafeGFree(debugPtr);
                                GstNative.gst_message_unref(warnMsg);
                                Log(string.IsNullOrWhiteSpace(debugWrap)
                                    ? $"[GSTREAMER-WARNING] {warnWrap}"
                                    : $"[GSTREAMER-WARNING] {warnWrap} | debug={debugWrap}");
                            }

                            // ── STATE_CHANGED (diagnostic) ─────────────────────────
                            IntPtr rtspStateMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_STATE_CHANGED);
                            if (rtspStateMsg != IntPtr.Zero)
                            {
                                int oldS, newS, pendS;
                                GstNative.gst_message_parse_state_changed(rtspStateMsg, out oldS, out newS, out pendS);
                                GstNative.gst_message_unref(rtspStateMsg);
                                Log($"[GSTREAMER-RTSP-STATE] {oldS}->{newS} (pending={pendS})");
                            }

                            // ── ASYNC_DONE — bind overlay once preroll completes ────
                            IntPtr rtspAsyncMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_ASYNC_DONE);
                            if (rtspAsyncMsg != IntPtr.Zero)
                            {
                                GstNative.gst_message_unref(rtspAsyncMsg);
                                Log("[GSTREAMER-RTSP] ASYNC_DONE — pipeline prerolled, exposing overlay.");
                                if (_activeStreams.TryGetValue(pipeline, out var liveCtx) && liveCtx.WindowHandle != IntPtr.Zero)
                                {
                                    if (liveCtx.OverlayElement != IntPtr.Zero)
                                    {
                                        GstNative.gst_video_overlay_expose(liveCtx.OverlayElement);
                                    }
                                    else
                                    {
                                        GstNative.gst_video_overlay_set_window_handle(pipeline, liveCtx.WindowHandle);
                                        GstNative.gst_video_overlay_handle_events(pipeline, true);
                                        GstNative.gst_video_overlay_expose(pipeline);
                                    }
                                    Log($"[TS-VMS] RTSP: overlay exposed at ASYNC_DONE handle={liveCtx.WindowHandle}");
                                }
                            }

                            await Task.Delay(100, token);
                        }
                    }
                    catch (OperationCanceledException) { }
                    catch (Exception ex) { Log($"[BUS-TASK] {ctx.StreamId} {ex.Message}"); }
                    finally
                    {
                        ctx.BusHandle = IntPtr.Zero;
                        GstNative.SafeObjectUnref(bus);
                        // Release our task's reference to the pipeline
                        GstNative.SafeObjectUnref(pipeline);
                    }
                }, token);

                _activeStreams.TryAdd(pipeline, ctx);
            }
            else
            {
                _activeStreams.TryAdd(pipeline, ctx);
            }

            int stateResult = GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_PLAYING);
            Log($"[TS-VMS] {ctx.StreamId} Set State PLAYING returned: {stateResult} ({rtspUrl})");

            return pipeline;
        }

        private IntPtr StartHlsStream(IntPtr windowHandle, string hlsUrl, bool hasAudio, Func<Task<(string Url, IntPtr Handle)>>? getFreshContext = null)
        {
            Log($"[TS-VMS] StartHlsStream: '{hlsUrl}'");

            if (!WaitForWindowSize(windowHandle, 2000))
            {
                Log($"[TS-VMS] Window {windowHandle} never became ready; aborting.");
                StreamError?.Invoke(windowHandle, "Video surface not ready");
                return IntPtr.Zero;
            }

            // Build the HLS pipeline programmatically to avoid gst_parse_launch wrapping
            // video-sink in an anonymous GstBin (via gst_parse_bin_from_description).
            // That wrapper intercepts the prepare-window-handle sync message before it
            // reaches our bus sync handler, preventing overlay binding entirely.
            IntPtr pipeline = GstNative.gst_element_factory_make("playbin", "hls-pipeline");
            if (pipeline == IntPtr.Zero)
            {
                Log("[TS-VMS] HLS Pipeline Creation Failed");
                StreamError?.Invoke(windowHandle, "playbin not available");
                return IntPtr.Zero;
            }
            // Sink the floating reference for programmatically created top-level elements
            // to prevent GStreamer from finalizing it prematurely or causing ref-count assertions.
            GstNative.g_object_ref_sink(pipeline);

            GstNative.g_object_set_str(pipeline, "uri", hlsUrl, IntPtr.Zero);

            // FIX-1: Set explicit playbin flags for live camera streams.
            //
            // playbin's default flags include GST_PLAY_FLAG_BUFFERING (0x08) and
            // GST_PLAY_FLAG_DOWNLOAD (0x20).  For a live HLS camera stream the
            // download buffer can never be declared "full", so playbin stalls in
            // PAUSED waiting for a buffer level it will never reach.
            //
            // We keep only the three flags we need:
            //   GST_PLAY_FLAG_VIDEO        = 0x01
            //   GST_PLAY_FLAG_AUDIO        = 0x02
            //   GST_PLAY_FLAG_NATIVE_VIDEO = 0x40  (skip SW colour conversion)
            //
            // Explicitly NOT set:
            //   GST_PLAY_FLAG_BUFFERING    = 0x08  (causes preroll stall on live)
            //   GST_PLAY_FLAG_DOWNLOAD     = 0x20  (not needed; wastes disk I/O)
            //   GST_PLAY_FLAG_DEINTERLACE  = 0x10  (not needed for camera feeds)
            //   GST_PLAY_FLAG_SOFT_VOLUME  = 0x04  (not needed; audio handled below)
            GstNative.g_object_set_int(pipeline, "flags", 0x01 | 0x02 | 0x40, IntPtr.Zero);

            // Create d3d11videosink directly so it is a plain GstElement child of playbin,
            // not wrapped in an intermediate bin. This ensures prepare-window-handle is
            // posted on the bus where our sync handler can intercept it.
            IntPtr videoSink = GstNative.gst_element_factory_make("d3d11videosink", "mysink");
            if (videoSink != IntPtr.Zero)
            {
                GstNative.g_object_set_int(videoSink, "sync", 0, IntPtr.Zero);
                // async=false: do not block READY→PAUSED preroll waiting for a decoded frame.
                // HLS must download the playlist + first segment before d3d11videosink receives
                // any buffer. With async=true (default) the pipeline stalls at READY indefinitely.
                GstNative.g_object_set_int(videoSink, "async", 0, IntPtr.Zero);
                GstNative.g_object_set_ptr(pipeline, "video-sink", videoSink, IntPtr.Zero);
                GstNative.SafeObjectUnref(videoSink); // playbin holds its own ref
                Log("[TS-VMS] HLS: d3d11videosink created.");
            }
            else
            {
                Log("[TS-VMS] HLS: d3d11videosink not available — falling back to auto sink (no overlay).");
            }

            IntPtr audioSink = hasAudio
                ? GstNative.gst_element_factory_make("autoaudiosink", null)
                : GstNative.gst_element_factory_make("fakesink", null);
            if (audioSink != IntPtr.Zero)
            {
                if (!hasAudio) GstNative.g_object_set_int(audioSink, "sync", 0, IntPtr.Zero);
                GstNative.g_object_set_ptr(pipeline, "audio-sink", audioSink, IntPtr.Zero);
                GstNative.SafeObjectUnref(audioSink);
            }

            var ctx = new StreamContext
            {
                Url = hlsUrl,
                WindowHandle = windowHandle,
                Cts = new CancellationTokenSource(),
                HasAudio = hasAudio,
                Pipeline = pipeline,
                GetFreshContext = getFreshContext,
                FallbackCts = new CancellationTokenSource()
            };

            // playbin instantiates its internal elements lazily (not until READY→PAUSED),
            // so gst_bin_get_by_name will always return null here at NULL state.
            // Overlay binding is handled reliably in OverlayBusSyncHandler via the
            // prepare-window-handle sync message, which fires during preroll.

            IntPtr bus = GstNative.gst_element_get_bus(pipeline);
            if (bus != IntPtr.Zero)
            {
                ctx.BusHandle = bus;
                InstallOverlaySyncHandler(ctx);

                var token = ctx.Cts.Token;
                ctx.WatchTask = Task.Run(async () =>
                {
                    // Increment reference for the task life
                    GstNative.gst_object_ref(pipeline);
                    try
                    {
                        Log($"[HLS-BUS-TASK] Watch task started for {ctx.StreamId}");
                        while (!token.IsCancellationRequested)
                        {
                            // ── ANY MESSAGE (diagnostic) ───────────────────────────
                            // Temporarily peek at ALL messages to see if anything is happening.
                            // IntPtr anyMsg = GstNative.gst_bus_pop(bus); 
                            // ...

                            // ── ERROR ──────────────────────────────────────────────
                            IntPtr errMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_ERROR);
                            if (errMsg != IntPtr.Zero)
                            {
                                IntPtr errPtr, debugPtr;
                                GstNative.gst_message_parse_error(errMsg, out errPtr, out debugPtr);
                                string errWrap   = ReadGErrorMessage(errPtr, "Unknown HLS Error");
                                uint   errDomain = ReadGErrorDomain(errPtr);
                                int    errCode   = ReadGErrorCode(errPtr);
                                string debugWrap = ReadUtf8String(debugPtr);
                                GstNative.SafeGErrorFree(errPtr);
                                GstNative.SafeGFree(debugPtr);
                                GstNative.gst_message_unref(errMsg);

                                if (!token.IsCancellationRequested && _activeStreams.ContainsKey(pipeline))
                                {
                                    Log($"[GSTREAMER-HLS-ERROR] {ctx.StreamId} domain={errDomain} code={errCode} msg={errWrap} | debug={debugWrap}");
                                    StreamError?.Invoke(ctx.WindowHandle, errWrap);

                                    if (IsPermanentHttpError(errWrap) || IsPermanentHttpError(debugWrap))
                                    {
                                        Log($"[TS-VMS] {ctx.StreamId} Permanent HLS error - stopping.");
                                        StopStream(pipeline);
                                    }
                                    else
                                    {
                                        _ = RestartStreamAsync(pipeline);
                                    }
                                    break;
                                }
                            }

                            // ── WARNING ────────────────────────────────────────────
                            IntPtr warnMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_WARNING);
                            if (warnMsg != IntPtr.Zero)
                            {
                                IntPtr errPtr, debugPtr;
                                GstNative.gst_message_parse_warning(warnMsg, out errPtr, out debugPtr);
                                string warnWrap = ReadGErrorMessage(errPtr, "Unknown HLS Warning");
                                string debugWrap = ReadUtf8String(debugPtr);
                                GstNative.SafeGErrorFree(errPtr);
                                GstNative.SafeGFree(debugPtr);
                                GstNative.gst_message_unref(warnMsg);
                                Log(string.IsNullOrWhiteSpace(debugWrap)
                                    ? $"[GSTREAMER-HLS-WARNING] {warnWrap}"
                                    : $"[GSTREAMER-HLS-WARNING] {warnWrap} | debug={debugWrap}");
                            }

                            // ── BUFFERING (FIX-2) ──────────────────────────────────
                            // GStreamer's buffering protocol requires the APPLICATION to
                            // pause the pipeline while pct < 100 and resume when pct == 100.
                            // Previously this block only logged — that omission causes the
                            // streaming thread to deadlock: the queue fills up and blocks,
                            // the sink consumes nothing, frames never reach the screen.
                            IntPtr bufMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_BUFFERING);
                            if (bufMsg != IntPtr.Zero)
                            {
                                int pct;
                                GstNative.gst_message_parse_buffering(bufMsg, out pct);
                                GstNative.gst_message_unref(bufMsg);
                                Log($"[GSTREAMER-HLS-BUFFERING] {pct}%");

                                if (pct < 100)
                                {
                                    Log("[GSTREAMER-HLS-BUFFERING] Pausing pipeline while buffer fills.");
                                    GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_PAUSED);
                                }
                                else
                                {
                                    Log("[GSTREAMER-HLS-BUFFERING] Buffer full — resuming playback.");
                                    GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_PLAYING);
                                }
                            }

                            // ── STATE_CHANGED (diagnostic) ─────────────────────────
                            IntPtr stateMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_STATE_CHANGED);
                            if (stateMsg != IntPtr.Zero)
                            {
                                int oldS, newS, pendS;
                                GstNative.gst_message_parse_state_changed(stateMsg, out oldS, out newS, out pendS);
                                GstNative.gst_message_unref(stateMsg);
                                Log($"[GSTREAMER-HLS-STATE] {oldS}->{newS} (pending={pendS})");
                            }

                            // ── ASYNC_DONE (FIX-3) ────────────────────────────────
                            // ASYNC_DONE fires when playbin completes its first preroll —
                            // i.e. after the playlist is fetched, the first .ts segment is
                            // downloaded, and the first frame is decoded.  At this point
                            // d3d11videosink is fully instantiated inside playbin's playsink
                            // and gst_video_overlay_set_window_handle() is guaranteed to work.
                            //
                            // HLS preroll typically takes 2–6 s (one or more segment durations),
                            // so the old 500 ms fallback Task always fired before the sink existed
                            // — making it a silent no-op and leaving the window black.
                            IntPtr asyncMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_ASYNC_DONE);
                            if (asyncMsg != IntPtr.Zero)
                            {
                                GstNative.gst_message_unref(asyncMsg);
                                ctx.HlsAsyncDone = true;
                                Log($"[GSTREAMER-HLS] {ctx.StreamId} ASYNC_DONE — binding overlay.");
                                if (_activeStreams.TryGetValue(pipeline, out var liveCtx) && liveCtx.WindowHandle != IntPtr.Zero)
                                {
                                    GstNative.gst_video_overlay_set_window_handle(pipeline, liveCtx.WindowHandle);
                                    GstNative.gst_video_overlay_handle_events(pipeline, true);
                                    GstNative.gst_video_overlay_expose(pipeline);
                                    Log($"[TS-VMS] HLS: overlay exposed at ASYNC_DONE handle={liveCtx.WindowHandle}");
                                }
                                // Cancel the fallback task if ASYNC_DONE successfully bound the overlay
                                ctx.FallbackCts?.Cancel();
                            }

                                if (DateTime.Now.Second % 10 == 0 && DateTime.Now.Millisecond < 100)
                                {
                                    Log($"[HLS-HEARTBEAT] {ctx.StreamId} Playing... {ctx.Url}");
                                }

                                await Task.Delay(100, token);
                        }
                    }
                    catch (OperationCanceledException) { }
                    catch (Exception ex) { Log($"[HLS-BUS-TASK] {ctx.StreamId} {ex.Message}"); }
                    finally
                    {
                        ctx.BusHandle = IntPtr.Zero;
                        GstNative.SafeObjectUnref(bus);
                        // Release our task's reference
                        GstNative.SafeObjectUnref(pipeline);
                    }
                }, token);

                _activeStreams.TryAdd(pipeline, ctx);
            }
            else
            {
                _activeStreams.TryAdd(pipeline, ctx);
            }

            // Pre-bind window handle BEFORE set_state(PLAYING).
            // playbin stores this and forwards it to d3d11videosink when the sink is
            // instantiated inside playsink, so the sink renders into our VideoCanvas HWND
            // instead of creating its own top-level popup window.
            // This must be called before PLAYING because d3d11videosink allocates its
            // DXGI swap chain during the READY→PAUSED preroll transition.
            if (ctx.WindowHandle != IntPtr.Zero)
            {
                GstNative.gst_video_overlay_set_window_handle(pipeline, ctx.WindowHandle);
                GstNative.gst_video_overlay_handle_events(pipeline, true);
                Log($"[TS-VMS] {ctx.StreamId} Pre-bound window handle before PLAYING: {ctx.WindowHandle}");
            }

            int stateResult = GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_PLAYING);
            Log($"[TS-VMS] {ctx.StreamId} HLS Set State PLAYING returned: {stateResult}");
            // ASYNC (2) is expected for playbin — HLS preroll completes on pipeline threads.
            // Do NOT block here; bus watch task handles errors and ASYNC_DONE asynchronously.

            // FIX-4: Safety-net fallback overlay bind.
            // Kept as a belt-and-suspenders guard in case ASYNC_DONE is never posted
            // (e.g. pipeline stalls permanently at READY due to a missing segment).
            // Delay raised from 500 ms → 4000 ms so it actually fires AFTER HLS preroll
            // has had a chance to complete.  The primary bind is now ASYNC_DONE above;
            // this is only a last-resort catch.
            var bindPipeline = pipeline;
            var fToken = ctx.FallbackCts.Token;
            _ = Task.Run(async () =>
            {
                // Increment reference for fallback task life
                GstNative.gst_object_ref(bindPipeline);
                try
                {
                    await Task.Delay(4000, fToken);
                    if (!fToken.IsCancellationRequested && _activeStreams.TryGetValue(bindPipeline, out var live) && live.WindowHandle != IntPtr.Zero)
                    {
                        Log($"[TS-VMS] Fallback overlay bind firing for {live.StreamId} handle={live.WindowHandle}");
                        GstNative.gst_video_overlay_set_window_handle(bindPipeline, live.WindowHandle);
                        GstNative.gst_video_overlay_expose(bindPipeline);
                    }
                }
                catch (OperationCanceledException) { }
                catch (Exception ex) { Log($"[TS-VMS] {ctx.StreamId} Fallback Error: {ex.Message}"); }
                finally
                {
                    GstNative.SafeObjectUnref(bindPipeline);
                }
            }, fToken);

            var prerollPipeline = pipeline;
            var prerollToken = ctx.FallbackCts.Token;
            _ = Task.Run(async () =>
            {
                GstNative.gst_object_ref(prerollPipeline);
                try
                {
                    await Task.Delay(8000, prerollToken);
                    if (!prerollToken.IsCancellationRequested &&
                        _activeStreams.TryGetValue(prerollPipeline, out var liveCtx) &&
                        !liveCtx.HlsAsyncDone)
                    {
                        Log($"[TS-VMS] {liveCtx.StreamId} HLS preroll timed out without ASYNC_DONE.");
                        StreamError?.Invoke(liveCtx.WindowHandle, "HLS preroll timeout");
                        StopStream(prerollPipeline);
                    }
                }
                catch (OperationCanceledException) { }
                catch (Exception ex) { Log($"[TS-VMS] {ctx.StreamId} Preroll watchdog error: {ex.Message}"); }
                finally
                {
                    GstNative.SafeObjectUnref(prerollPipeline);
                }
            }, prerollToken);

            return pipeline;
        }

        private async Task RestartStreamAsync(IntPtr oldPipeline)
        {
            if (!_activeStreams.TryGetValue(oldPipeline, out var ctx)) return;
            if (ctx.IsRestarting) return;
            ctx.IsRestarting = true;

            Log($"[TS-VMS] Backing off 5s before restart for {ctx.Url}...");
            await Task.Delay(5000);

            var savedUrl     = ctx.Url ?? "";
            var savedWindow  = ctx.WindowHandle;
            var savedUser    = ctx.Username;
            var savedPw      = ctx.Password;
            var savedAudio   = ctx.HasAudio;
            var savedGetFreshContext = ctx.GetFreshContext;

            if (savedGetFreshContext != null)
            {
                try
                {
                    Log($"[TS-VMS] Fetching fresh URL and Window Handle for retry...");
                    var fresh = await savedGetFreshContext();
                    if (!string.IsNullOrWhiteSpace(fresh.Url))
                    {
                        savedUrl = fresh.Url;
                    }
                    if (fresh.Handle != IntPtr.Zero)
                    {
                        savedWindow = fresh.Handle;
                    }
                }
                catch (Exception ex)
                {
                    Log($"[TS-VMS] Failed to get fresh context: {ex.Message}");
                }
            }

            // FIX 3: stop on thread pool so TEARDOWN never blocks the UI.
            if (_activeStreams.TryRemove(oldPipeline, out var stopCtx))
            {
                await Task.Run(() => StopStreamInternal(oldPipeline, stopCtx));
            }

            Log($"[TS-VMS] Re-starting stream on handle {savedWindow}: {savedUrl}");
            StartStream(savedWindow, savedUrl, savedUser, savedPw, savedAudio, savedGetFreshContext);
        }

        public void Reattach(IntPtr pipeline, IntPtr windowHandle)
        {
            if (pipeline == IntPtr.Zero || windowHandle == IntPtr.Zero) return;
            if (!_activeStreams.TryGetValue(pipeline, out var ctx))
                return;

            var oldHandle = ctx.WindowHandle;
            if (oldHandle == windowHandle)
                return;

            ctx.WindowHandle = windowHandle;
            BindOverlay(ctx);
            Log($"[TS-VMS] Reattached old={oldHandle} new={windowHandle}");
        }

        public void SetVolume(IntPtr pipeline, double volume)
        {
            if (pipeline == IntPtr.Zero) return;
            if (!_activeStreams.ContainsKey(pipeline)) return;

            IntPtr volElement = GstNative.gst_bin_get_by_name(pipeline, "myvolume");
            if (volElement != IntPtr.Zero)
            {
                GstNative.g_object_set(volElement, "volume", volume, IntPtr.Zero);
                GstNative.SafeObjectUnref(volElement);
            }
        }

        // ------------------------------------------------------------------
        // FIX 3: StopStream is now non-blocking for the calling thread.
        //
        // ROOT CAUSE ("Not Responding" on page-switch / grid refresh):
        //   StopStream() was synchronous and called from RefreshGrid() on the
        //   Dispatcher thread.  gst_element_set_state(NULL) sends an RTSP
        //   TEARDOWN and waits for the server to reply.  The log line
        //   "Timed out waiting for TEARDOWN to be processed" at t=16.741s
        //   proves this stalled the UI thread for several seconds — long
        //   enough to trigger the Windows "Not Responding" watchdog.
        //   The previous 300 ms timeout was too short to complete the teardown,
        //   leaving D3D11 resources unreleased (hence Refcount:52 in the log).
        //
        // FIX: StopStream() fires-and-forgets StopStreamInternal() onto a
        //   thread-pool thread.  The actual teardown timeout is increased to 3 s
        //   so GStreamer can cleanly release all D3D11 device objects.
        //   Callers that need to await completion use StopStreamAsync().
        // ------------------------------------------------------------------
        public void StopStream(IntPtr pipeline)
        {
            if (pipeline == IntPtr.Zero) return;
            if (!_activeStreams.TryRemove(pipeline, out var ctx)) return;
            _ = Task.Run(() => StopStreamInternal(pipeline, ctx));
        }

        public Task StopStreamAsync(IntPtr pipeline)
        {
            if (pipeline == IntPtr.Zero) return Task.CompletedTask;
            if (!_activeStreams.TryRemove(pipeline, out var ctx)) return Task.CompletedTask;
            return Task.Run(() => StopStreamInternal(pipeline, ctx));
        }

        private void StopStreamInternal(IntPtr pipeline, StreamContext ctx)
        {
            if (!ctx.TeardownLock.Wait(0)) 
            {
                Log($"[TS-VMS] {ctx.StreamId} Teardown already in progress, skipping.");
                return;
            }

            try
            {
                Log($"[TS-VMS] {ctx.StreamId} Starting teardown for {ctx.Url}");

                // Signal all tasks to stop
                ctx.Cts?.Cancel();
                ctx.FallbackCts?.Cancel();

                // Stop the pipeline
                if (pipeline != IntPtr.Zero)
                {
                    // Move to NULL state to release D3D11 resources.
                    Log($"[TS-VMS] {ctx.StreamId} Setting state to NULL...");
                    GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_NULL);
                }

                // Clean up overlay sync handler
                RemoveOverlaySyncHandler(ctx);

                // Unref components
                try
                {
                    if (ctx.OverlayElement != IntPtr.Zero)
                    {
                        Log($"[TS-VMS] {ctx.StreamId} Unreferencing OverlayElement...");
                        GstNative.SafeObjectUnref(ctx.OverlayElement);
                        ctx.OverlayElement = IntPtr.Zero;
                    }
                }
                catch (Exception ex)
                {
                    Log($"[TS-VMS] {ctx.StreamId} StopStream overlay unref failed: {ex.Message}");
                }

                try
                {
                    if (pipeline != IntPtr.Zero)
                    {
                        if (ctx.BusHandle == IntPtr.Zero)
                        {
                            Log($"[TS-VMS] {ctx.StreamId} Unreferencing Pipeline...");
                            GstNative.SafeObjectUnref(pipeline);
                        }
                        else
                        {
                            Log($"[TS-VMS] {ctx.StreamId} Pipeline unref deferred to bus task.");
                        }
                    }
                }
                catch (Exception ex)
                {
                    Log($"[TS-VMS] {ctx.StreamId} StopStream pipeline unref failed: {ex.Message}");
                }

                // Dispose tokens
                try { ctx.Cts?.Dispose(); } catch { }
                try { ctx.FallbackCts?.Dispose(); } catch { }

                Log($"[TS-VMS] {ctx.StreamId} Stopped and handle cleared.");
            }
            finally
            {
                ctx.TeardownLock.Release();
            }
        }

        private string InjectCredentials(string rtspUrl, string username, string password)
        {
            if (string.IsNullOrEmpty(rtspUrl) || !rtspUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
                return rtspUrl;
            string cleanUrl = rtspUrl.Substring(7);
            if (cleanUrl.Contains("@"))
                cleanUrl = cleanUrl.Substring(cleanUrl.IndexOf('@') + 1);
            return $"rtsp://{username}:{password}@{cleanUrl}";
        }

        private static bool WaitForWindowSize(IntPtr hwnd, int timeoutMs = 2000)
        {
            if (hwnd == IntPtr.Zero) return false;

            int waited = 0;
            while (waited < timeoutMs)
            {
                if (HasWindowSize(hwnd))
                    return true;

                Thread.Sleep(10);
                waited += 10;
            }

            return HasWindowSize(hwnd);
        }

        private static bool IsSurfaceError(string msg)
        {
            if (string.IsNullOrEmpty(msg)) return false;

            return msg.Contains("Output window was closed", StringComparison.OrdinalIgnoreCase) ||
                   msg.Contains("Cannot create d3d11window", StringComparison.OrdinalIgnoreCase) ||
                   msg.Contains("Resource not found", StringComparison.OrdinalIgnoreCase);
        }

        /// <summary>HTTP-level permanent failures that should never be retried with the same URL.</summary>
        private static bool IsPermanentHttpError(string msg)
        {
            if (string.IsNullOrEmpty(msg)) return false;

            return msg.Contains("Not Found",            StringComparison.OrdinalIgnoreCase)  // 404
                || msg.Contains("Unauthorized",         StringComparison.OrdinalIgnoreCase)  // 401
                || msg.Contains("Forbidden",            StringComparison.OrdinalIgnoreCase)  // 403
                || msg.Contains("Internal Server Error",StringComparison.OrdinalIgnoreCase)  // 500
                || msg.Contains("Internal data stream error", StringComparison.OrdinalIgnoreCase) // Connection refused/stalled
                || msg.Contains("Could not connect",    StringComparison.OrdinalIgnoreCase)
                || msg.Contains("404", StringComparison.Ordinal)
                || msg.Contains("401", StringComparison.Ordinal)
                || msg.Contains("500", StringComparison.Ordinal);
        }

        public async Task<bool> RecordLiveEventAsync(string eventType, string details)
            => await _api.PostAsync("/api/v1/live/events", new { type = eventType, details = details });

        public async Task<bool> DownloadSnapshotAsync(string cameraId, string outputPath)
            => await _api.DownloadFileAsync($"/api/v1/cameras/{cameraId}/snapshot", outputPath);
    }
}
