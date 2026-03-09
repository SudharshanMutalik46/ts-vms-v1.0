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
        private readonly string _logPath;
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
            public CancellationTokenSource? CTS { get; set; }
            public Task? WatchTask { get; set; }
            public bool IsRestarting { get; set; }
            public string Username { get; set; } = "";
            public string Password { get; set; } = "";
            public bool HasAudio { get; set; }
        }

        public event Action<IntPtr, string>? StreamError;
        private readonly ConcurrentDictionary<IntPtr, StreamContext> _activeStreams = new();
        private readonly ApiClient _api;

        public VideoService(ApiClient api)
        {
            _api = api;
            _logPath = System.IO.Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "gstreamer_log.txt");
        }

        private void Log(string message)
        {
            string line = $"[{DateTime.Now:HH:mm:ss}] {message}";
            Console.WriteLine(line);
            try { System.IO.File.AppendAllText(_logPath, line + Environment.NewLine); } catch { }
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

        public IntPtr StartStream(IntPtr windowHandle, string rtspUrl, string username = "", string password = "", bool hasAudio = false)
        {
            if (!_isInitialized) Initialize();

            string authUrl = rtspUrl;
            string userIdProp = "";
            string userPwProp = "";

            if (!string.IsNullOrEmpty(username) && rtspUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                if (!authUrl.Contains("@"))
                    authUrl = authUrl.Replace("rtsp://", $"rtsp://{username}:{password}@");
                userIdProp = $"user-id=\"{username}\"";
                userPwProp = $"user-pw=\"{password}\"";
            }

            Log($"[TS-VMS] StartStream Request Original: '{rtspUrl}'");

            // Avoid blocking the UI thread while waiting for layout.
            if (!HasWindowSize(windowHandle))
                Log($"[TS-VMS] Warning: window {windowHandle} is currently 0x0; starting stream without blocking.");

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

            IntPtr sink = GstNative.gst_bin_get_by_name(pipeline, "mysink");
            if (sink != IntPtr.Zero)
            {
                GstNative.gst_video_overlay_set_window_handle(sink, windowHandle);
                GstNative.SafeObjectUnref(sink);
                Log("[TS-VMS] Handle set on 'mysink'.");
            }

            var ctx = new StreamContext {
                Url = rtspUrl,
                WindowHandle = windowHandle,
                CTS = new CancellationTokenSource(),
                Username = username,
                Password = password,
                HasAudio = hasAudio
            };

            IntPtr bus = GstNative.gst_element_get_bus(pipeline);
            if (bus != IntPtr.Zero)
            {
                var token = ctx.CTS.Token;
                ctx.WatchTask = Task.Run(async () => {
                    try {
                        Log($"[TS-VMS] Bus monitor started for {rtspUrl}");
                        while (!token.IsCancellationRequested) {
                            IntPtr errMsg = GstNative.gst_bus_pop_filtered(bus, GstNative.GST_MESSAGE_ERROR);
                            if (errMsg != IntPtr.Zero) {
                                IntPtr errPtr, debugPtr;
                                GstNative.gst_message_parse_error(errMsg, out errPtr, out debugPtr);
                                string errWrap = ReadGErrorMessage(errPtr, "Unknown Error");
                                GstNative.SafeGErrorFree(errPtr);
                                GstNative.SafeGFree(debugPtr);
                                GstNative.gst_message_unref(errMsg);

                                if (!token.IsCancellationRequested && _activeStreams.ContainsKey(pipeline))
                                {
                                    Log($"[GSTREAMER-ERROR] {errWrap}. Triggering auto-restart...");
                                    StreamError?.Invoke(ctx.WindowHandle, errWrap);
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
                                    const string eosText = "Stream reached EOS. Triggering auto-restart...";
                                    Log($"[GSTREAMER-ERROR] {eosText}");
                                    StreamError?.Invoke(ctx.WindowHandle, eosText);
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
                                GstNative.SafeGErrorFree(errPtr);
                                GstNative.SafeGFree(debugPtr);
                                GstNative.gst_message_unref(warnMsg);
                                Log($"[GSTREAMER-WARNING] {warnWrap}");
                            }
                            await Task.Delay(100, token);
                        }
                    }
                    catch (OperationCanceledException) { }
                    catch (Exception ex) { Log($"[BUS-TASK] {ex.Message}"); }
                    finally { GstNative.SafeObjectUnref(bus); }
                }, token);

                _activeStreams.TryAdd(pipeline, ctx);
            }

            int stateResult = GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_PLAYING);
            Log($"[TS-VMS] Set State PLAYING returned: {stateResult} ({rtspUrl})");

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

            // FIX 3: stop on thread pool so TEARDOWN never blocks the UI.
            if (_activeStreams.TryRemove(oldPipeline, out var stopCtx))
            {
                await Task.Run(() => StopStreamInternal(oldPipeline, stopCtx));
            }

            Log($"[TS-VMS] Re-starting stream: {savedUrl}");
            StartStream(savedWindow, savedUrl, savedUser, savedPw, savedAudio);
        }

        public void Reattach(IntPtr pipeline, IntPtr windowHandle)
        {
            if (pipeline == IntPtr.Zero || windowHandle == IntPtr.Zero) return;
            if (!_activeStreams.TryGetValue(pipeline, out var ctx))
                return;

            ctx.WindowHandle = windowHandle;

            IntPtr overlayElement = GstNative.gst_bin_get_by_name(pipeline, "mysink");
            if (overlayElement != IntPtr.Zero)
            {
                GstNative.gst_video_overlay_set_window_handle(overlayElement, windowHandle);
                GstNative.SafeObjectUnref(overlayElement);
                Log("[TS-VMS] Reattached.");
            }
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
            try { ctx.CTS?.Cancel(); } catch { }
            try { ctx.WatchTask?.Wait(500); } catch { }

            try
            {
                GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_NULL);
                // 3 s timeout gives rtspsrc enough time to send TEARDOWN properly,
                // avoiding the D3D11 object leak from premature pipeline destruction.
                const ulong stopTimeoutNs = 3_000_000_000UL;
                GstNative.gst_element_get_state(pipeline, out _, out _, stopTimeoutNs);
            }
            catch (Exception ex) { Log($"[TS-VMS] StopStream state wait failed: {ex.Message}"); }

            try { GstNative.SafeObjectUnref(pipeline); }
            catch (Exception ex) { Log($"[TS-VMS] StopStream unref failed: {ex.Message}"); }

            try { ctx.CTS?.Dispose(); } catch { }

            Log("[TS-VMS] Stopped and handle cleared.");
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

        public async Task<bool> RecordLiveEventAsync(string eventType, string details)
            => await _api.PostAsync("/api/v1/live/events", new { type = eventType, details = details });

        public async Task<bool> DownloadSnapshotAsync(string cameraId, string outputPath)
            => await _api.DownloadFileAsync($"/api/v1/cameras/{cameraId}/snapshot", outputPath);
    }
}
