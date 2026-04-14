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
        private static readonly object _logLock = new object();
        private readonly object _lock = new object();
        private static readonly ConcurrentDictionary<string, string> _codecCache = new(StringComparer.OrdinalIgnoreCase);

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
            public DateTime StartedAtUtc { get; set; } = DateTime.UtcNow;
            public DateTime LastProgressAtUtc { get; set; } = DateTime.UtcNow;
            public long LastProgressPositionNs { get; set; } = -1;
            public string Username { get; set; } = "";
            public string Password { get; set; } = "";
            public bool HasAudio { get; set; }
            public string RtspTransport { get; set; } = "tcp";
            public string? DetectedCodec { get; set; }

            public IntPtr Pipeline { get; set; }
            public IntPtr OverlayElement { get; set; }
            public IntPtr BusHandle { get; set; }
            public bool OverlayBound { get; set; }

            public GstNative.GstBusSyncHandler? SyncHandler { get; set; }
            public GCHandle SyncHandlerGcHandle { get; set; }
            public bool SyncHandlerGcHandleAllocated { get; set; }

            public Func<Task<(string Url, IntPtr Handle)>>? GetFreshContext { get; set; }
            public CancellationTokenSource? FallbackCts { get; set; }
            public SemaphoreSlim TeardownLock { get; } = new(1, 1);
            public string StreamId { get; set; } = Guid.NewGuid().ToString().Substring(0, 8);
        }

        public event Action<IntPtr, string>? StreamError;
        public event Action<IntPtr>? StreamReady;
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
                lock (_logLock)
                {
                    System.IO.File.AppendAllText(_logPath, line + Environment.NewLine); 
                }
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

        private static string NormalizeRtspTransport(string transport) => "tcp";

        private static string InjectRtspCredentials(string rtspUrl, string username, string password)
        {
            if (string.IsNullOrWhiteSpace(rtspUrl)) return rtspUrl;
            if (!rtspUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase)) return rtspUrl;
            if (rtspUrl.Contains("@", StringComparison.Ordinal)) return rtspUrl;
            if (string.IsNullOrWhiteSpace(username)) return rtspUrl;

            string userInfo = username;
            if (!string.IsNullOrWhiteSpace(password))
                userInfo += ":" + password;

            return "rtsp://" + userInfo + "@" + rtspUrl.Substring("rtsp://".Length);
        }

        private static string ResolveGstDiscovererPath()
        {
            string? overridePath = Environment.GetEnvironmentVariable("TS_VMS_GST_DISCOVERER");
            if (!string.IsNullOrWhiteSpace(overridePath) && System.IO.File.Exists(overridePath))
                return overridePath;

            string baseDir = AppDomain.CurrentDomain.BaseDirectory;
            string[] bundledCandidates =
            {
                System.IO.Path.Combine(baseDir, "gst-discoverer-1.0.exe"),
                System.IO.Path.GetFullPath(System.IO.Path.Combine(baseDir, "..", "..", "tools", "gstreamer", "bin", "gst-discoverer-1.0.exe")),
            };
            foreach (string candidate in bundledCandidates)
            {
                if (System.IO.File.Exists(candidate))
                    return candidate;
            }

            // MSVC-based GStreamer standard env var
            string? gstMsvcRoot = Environment.GetEnvironmentVariable("GSTREAMER_1_0_ROOT_MSVC_X86_64");
            if (!string.IsNullOrWhiteSpace(gstMsvcRoot))
            {
                string candidate = System.IO.Path.Combine(gstMsvcRoot, "bin", "gst-discoverer-1.0.exe");
                if (System.IO.File.Exists(candidate))
                    return candidate;
            }

            // MinGW-based GStreamer standard env var
            string? gstRoot = Environment.GetEnvironmentVariable("GSTREAMER_1_0_ROOT_X86_64");
            if (!string.IsNullOrWhiteSpace(gstRoot))
            {
                string candidate = System.IO.Path.Combine(gstRoot, "bin", "gst-discoverer-1.0.exe");
                if (System.IO.File.Exists(candidate))
                    return candidate;
            }

            // Fallback to searching PATH
            try
            {
                var proc = new System.Diagnostics.Process {
                    StartInfo = {
                        FileName = "where",
                        Arguments = "gst-discoverer-1.0.exe",
                        RedirectStandardOutput = true,
                        UseShellExecute = false,
                        CreateNoWindow = true
                    }
                };
                proc.Start();
                string output = proc.StandardOutput.ReadToEnd();
                proc.WaitForExit();
                if (proc.ExitCode == 0)
                {
                    string path = output.Split(new[] { Environment.NewLine }, StringSplitOptions.RemoveEmptyEntries)[0];
                    if (System.IO.File.Exists(path)) return path;
                }
            } catch { }

            return "gst-discoverer-1.0.exe";
        }

        private static string ParseDiscovererCodec(string output)
        {
            if (string.IsNullOrWhiteSpace(output)) return "";
            string txt = output.ToLowerInvariant();
            if (txt.Contains("video/x-h265") || txt.Contains("h.265") || txt.Contains("h265/90000") || txt.Contains("hevc"))
                return "h265";
            if (txt.Contains("video/x-h264") || txt.Contains("h.264") || txt.Contains("h264/90000") || txt.Contains("avc"))
                return "h264";
            return "";
        }

        private static async Task<string> DetectCodecWithGstDiscoverer(string rtspUrl, string username, string password)
        {
            string discoverer = ResolveGstDiscovererPath();
            string probeUrl = InjectRtspCredentials(rtspUrl, username, password);
            string args = $"\"{probeUrl}\"";

            var psi = new System.Diagnostics.ProcessStartInfo
            {
                FileName = discoverer,
                Arguments = args,
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                UseShellExecute = false,
                CreateNoWindow = true
            };

            try
            {
                using var proc = System.Diagnostics.Process.Start(psi);
                if (proc == null) return "";

                var outputTask = proc.StandardOutput.ReadToEndAsync();
                var errorTask = proc.StandardError.ReadToEndAsync();

                const int timeoutMs = 15000;
                var waited = 0;
                while (!proc.HasExited && waited < timeoutMs)
                {
                    await Task.Delay(100).ConfigureAwait(false);
                    waited += 100;
                }

                if (!proc.HasExited)
                {
                    try { proc.Kill(true); } catch { }
                    Log($"[TS-VMS] gst-discoverer timeout after {timeoutMs}ms for {probeUrl}");
                    return "";
                }

                string output = await outputTask.ConfigureAwait(false);
                string err = await errorTask.ConfigureAwait(false);
                string combined = output + "\n" + err;
                return ParseDiscovererCodec(combined);
            }
            catch (Exception ex)
            {
                Log($"[TS-VMS] gst-discoverer failed: {ex.Message}");
                return "";
            }
        }

        private static void StartLiveCodecProbe(StreamContext ctx, string authUrl, string username, string password)
        {
            if (_codecCache.TryGetValue(authUrl, out var cached))
            {
                ctx.DetectedCodec = cached;
                Log($"[TS-VMS] Live codec cache hit: {authUrl} -> {cached}");
                return;
            }

            _ = Task.Run(async () =>
            {
                try
                {
                    string codec = await DetectCodecWithGstDiscoverer(authUrl, username, password)
                        .ConfigureAwait(false);
                    if (!string.IsNullOrWhiteSpace(codec))
                    {
                        _codecCache[authUrl] = codec;
                        ctx.DetectedCodec = codec;
                        Log($"[TS-VMS] Live gst-discoverer codec: {authUrl} -> {codec}");
                    }
                    else
                    {
                        Log($"[TS-VMS] Live gst-discoverer codec not found for {authUrl}");
                    }
                }
                catch (Exception ex)
                {
                    Log($"[TS-VMS] Live gst-discoverer probe error: {ex.Message}");
                }
            });
        }

        private static string StripRtspCredentials(string rtspUrl, out string username, out string password)
        {
            username = "";
            password = "";

            if (string.IsNullOrWhiteSpace(rtspUrl) ||
                !rtspUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                return rtspUrl ?? "";
            }

            int schemeEnd = rtspUrl.IndexOf("://", StringComparison.OrdinalIgnoreCase);
            if (schemeEnd < 0)
                return rtspUrl;

            int authorityStart = schemeEnd + 3;
            int atIndex = rtspUrl.IndexOf('@', authorityStart);
            if (atIndex < 0)
                return rtspUrl;

            int pathStart = rtspUrl.IndexOf('/', authorityStart);
            int queryStart = rtspUrl.IndexOf('?', authorityStart);
            int authorityEnd = rtspUrl.Length;

            if (pathStart >= 0)
                authorityEnd = pathStart;
            else if (queryStart >= 0)
                authorityEnd = queryStart;

            if (atIndex > authorityEnd)
                return rtspUrl;

            string userInfo = rtspUrl.Substring(authorityStart, atIndex - authorityStart);
            int colonIndex = userInfo.IndexOf(':');
            if (colonIndex >= 0)
            {
                username = Uri.UnescapeDataString(userInfo.Substring(0, colonIndex));
                password = Uri.UnescapeDataString(userInfo.Substring(colonIndex + 1));
            }
            else
            {
                username = Uri.UnescapeDataString(userInfo);
            }

            return rtspUrl.Substring(0, authorityStart) + rtspUrl.Substring(atIndex + 1);
        }

        private static bool IsStreamStall(string message)
        {
            return message.Contains("stalled", StringComparison.OrdinalIgnoreCase)
                || message.Contains("no frame progress", StringComparison.OrdinalIgnoreCase)
                || message.Contains("freeze", StringComparison.OrdinalIgnoreCase);
        }

        private static bool IsRtspSetupFailure(string errWrap, string debugWrap)
        {
            string combined = $"{errWrap} {debugWrap}";
            return combined.Contains("setup failed", StringComparison.OrdinalIgnoreCase)
                || combined.Contains("Could not write to resource", StringComparison.OrdinalIgnoreCase)
                || combined.Contains("Error (404): Not Found", StringComparison.OrdinalIgnoreCase)
                || combined.Contains("404", StringComparison.OrdinalIgnoreCase)
                || combined.Contains("open_from_sdp", StringComparison.OrdinalIgnoreCase)
                || combined.Contains("setup_streams_start", StringComparison.OrdinalIgnoreCase);
        }

        private static void BindOverlay(StreamContext ctx)
        {
            if (ctx.OverlayBound || ctx.OverlayElement == IntPtr.Zero || ctx.WindowHandle == IntPtr.Zero)
                return;

            GstNative.gst_video_overlay_set_window_handle(ctx.OverlayElement, ctx.WindowHandle);
            GstNative.gst_video_overlay_handle_events(ctx.OverlayElement, true);
            GstNative.gst_video_overlay_expose(ctx.OverlayElement);
            ctx.OverlayBound = true;
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
                        if (ctx.OverlayBound)
                            return GstNative.GST_BUS_DROP;

                        // playbin path: playbin implements GstVideoOverlay and proxies
                        // gst_video_overlay_set_window_handle() to its internal video sink.
                        // This is the canonical approach when the sink is inside playbin.
                        GstNative.gst_video_overlay_set_window_handle(ctx.Pipeline, ctx.WindowHandle);
                        GstNative.gst_video_overlay_handle_events(ctx.Pipeline, true);
                        ctx.OverlayBound = true;
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

        public IntPtr StartStream(IntPtr windowHandle, string rtspUrl, string username = "", string password = "", bool hasAudio = false, Func<Task<(string Url, IntPtr Handle)>>? getFreshContext = null, string rtspTransport = "tcp", string cameraName = "")
        {
            if (!_isInitialized) Initialize();

            // HLS is no longer supported — only RTSP pipelines are created.
            if (rtspUrl.StartsWith("http://", StringComparison.OrdinalIgnoreCase) ||
                rtspUrl.StartsWith("https://", StringComparison.OrdinalIgnoreCase))
            {
                Log($"[TS-VMS] HTTP URL rejected (HLS removed): '{rtspUrl}'");
                StreamError?.Invoke(windowHandle, "HLS is not supported");
                return IntPtr.Zero;
            }

            string transport = NormalizeRtspTransport(rtspTransport);
            string userAgent = string.IsNullOrWhiteSpace(Environment.GetEnvironmentVariable("TS_VMS_RTSP_USER_AGENT"))
                ? "LibVLC/3.0.20"
                : Environment.GetEnvironmentVariable("TS_VMS_RTSP_USER_AGENT")!;

            string authUrl = StripRtspCredentials(rtspUrl, out string urlUser, out string urlPassword);
            string userIdProp = "";
            string userPwProp = "";

            string effectiveUser = !string.IsNullOrWhiteSpace(username) ? username : urlUser;
            string effectivePassword = !string.IsNullOrWhiteSpace(password) ? password : urlPassword;

            if (!string.IsNullOrEmpty(effectiveUser) && authUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                if (!string.Equals(rtspUrl, authUrl, StringComparison.Ordinal))
                {
                    Log("[TS-VMS] RTSP URL contained credentials; stripped userinfo and will pass auth props explicitly.");
                }

                userIdProp = $"user-id=\"{effectiveUser}\"";
                userPwProp = $"user-pw=\"{effectivePassword}\"";
            }

            Log($"[TS-VMS] StartStream Request Original: '{rtspUrl}' transport={transport}");
            Log($"[TS-VMS] RTSP target sanitized to '{authUrl}' with user-agent='{userAgent}'");

            // Avoid blocking the UI thread while waiting for layout.
            if (!WaitForWindowSize(windowHandle, 2000))
            {
                Log($"[TS-VMS] Window {windowHandle} never became ready; aborting stream start.");
                StreamError?.Invoke(windowHandle, "Video surface not ready");
                return IntPtr.Zero;
            }

            Log($"[TS-VMS] Window Handle: {windowHandle}");

            // ------------------------------------------------------------------
            // ALWAYS consume non-video RTP pads and add textoverlay for persistent names.
            // ------------------------------------------------------------------
            string audioPart = hasAudio
                ? "rtspsrc_src. ! application/x-rtp,media=audio ! queue ! decodebin3 name=abind abind. ! queue ! audioconvert ! audioresample ! volume name=myvolume ! autoaudiosink sync=false "
                : "rtspsrc_src. ! application/x-rtp,media=audio ! queue ! fakesink sync=false async=false ";

            string pipelineStr =
                $"rtspsrc location=\"{authUrl}\" user-agent=\"{userAgent}\" {userIdProp} {userPwProp} latency=500 drop-on-latency=true protocols={transport} timeout=10000000 tcp-timeout=10000000 name=rtspsrc_src " +
                $"rtspsrc_src. ! application/x-rtp,media=video ! queue ! decodebin3 name=vdbin " +
                $"vdbin. ! queue ! textoverlay text=\"{cameraName.Replace("\"", "\\\"")}\" valignment=top halignment=right font-desc=\"Sans Bold 10\" ! d3d11videosink name=mysink sync=false force-aspect-ratio=false " +
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
                GetFreshContext = getFreshContext,
                RtspTransport = transport,
                FallbackCts = new CancellationTokenSource(),
                StartedAtUtc = DateTime.UtcNow,
                LastProgressAtUtc = DateTime.UtcNow,
                LastProgressPositionNs = -1
            };

            // Background codec probe for live view (non-blocking).
            StartLiveCodecProbe(ctx, authUrl, effectiveUser, effectivePassword);

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
                            if (TryCheckForStall(ctx, pipeline, token))
                            {
                                break;
                            }

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

                                    if (string.Equals(ctx.RtspTransport, "tcp", StringComparison.OrdinalIgnoreCase) &&
                                        IsRtspSetupFailure(errWrap, debugWrap))
                                    {
                                        Log($"[TS-VMS] {ctx.StreamId} TCP RTSP setup failed; stopping stream.");
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
                                    else if (!liveCtx.OverlayBound)
                                    {
                                        GstNative.gst_video_overlay_set_window_handle(pipeline, liveCtx.WindowHandle);
                                        GstNative.gst_video_overlay_handle_events(pipeline, true);
                                        GstNative.gst_video_overlay_expose(pipeline);
                                        liveCtx.OverlayBound = true;
                                    }
                                    Log($"[TS-VMS] RTSP: overlay exposed at ASYNC_DONE handle={liveCtx.WindowHandle}");
                                    StreamReady?.Invoke(liveCtx.WindowHandle);
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

        private bool TryCheckForStall(StreamContext ctx, IntPtr pipeline, CancellationToken token)
        {
            if (token.IsCancellationRequested)
                return false;

            var age = DateTime.UtcNow - ctx.StartedAtUtc;
            if (age.TotalSeconds < 10)
                return false;

            if (!GstNative.gst_element_query_position(pipeline, GstNative.GST_FORMAT_TIME, out long positionNs))
                return false;

            if (positionNs < 0)
                return false;

            if (ctx.LastProgressPositionNs < 0 || positionNs > ctx.LastProgressPositionNs + 250_000_000L)
            {
                ctx.LastProgressPositionNs = positionNs;
                ctx.LastProgressAtUtc = DateTime.UtcNow;
                return false;
            }

            if ((DateTime.UtcNow - ctx.LastProgressAtUtc).TotalSeconds < 12)
                return false;

            if (ctx.IsRestarting)
                return false;

            Log($"[TS-VMS] {ctx.StreamId} RTSP stream stalled at {positionNs / 1_000_000_000.0:F2}s; notifying UI.");
            StreamError?.Invoke(ctx.WindowHandle, "RTSP stream stalled");
            return true;
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
            var savedTransport = ctx.RtspTransport;

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
            StartStream(savedWindow, savedUrl, savedUser, savedPw, savedAudio, savedGetFreshContext, savedTransport);
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
            ctx.OverlayBound = false;
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

                IntPtr overlayToRelease = ctx.OverlayElement;
                Task? watchTask = ctx.WatchTask;

                ctx.OverlayElement = IntPtr.Zero;
                ctx.OverlayBound = false;

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

                // Let the bus watcher release its own refs before we drop the stream's
                // final references. Without this, teardown can race with the watch
                // task finalizer and produce spurious gst_object_unref criticals.
                if (watchTask != null)
                {
                    try
                    {
                        if (!watchTask.Wait(TimeSpan.FromSeconds(2)))
                            Log($"[TS-VMS] {ctx.StreamId} Bus monitor did not exit within timeout.");
                    }
                    catch (AggregateException ex) when (ex.InnerExceptions.All(inner => inner is TaskCanceledException or OperationCanceledException))
                    {
                    }
                    catch (OperationCanceledException)
                    {
                    }
                    catch (Exception ex)
                    {
                        Log($"[TS-VMS] {ctx.StreamId} Bus monitor wait failed: {ex.Message}");
                    }
                    finally
                    {
                        ctx.WatchTask = null;
                    }
                }

                // Unref components
                try
                {
                    if (overlayToRelease != IntPtr.Zero)
                    {
                        Log($"[TS-VMS] {ctx.StreamId} Unreferencing OverlayElement...");
                        GstNative.SafeObjectUnref(overlayToRelease);
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
                        Log($"[TS-VMS] {ctx.StreamId} Unreferencing Pipeline...");
                        GstNative.SafeObjectUnref(pipeline);
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

        public async Task<bool> RecordLiveEventAsync(string eventType, string details)
            => await _api.PostAsync("/api/v1/live/events", new { type = eventType, details = details });

        public async Task<bool> DownloadSnapshotAsync(string cameraId, string outputPath)
            => await _api.DownloadFileAsync($"/api/v1/cameras/{cameraId}/snapshot", outputPath);
    }
}
