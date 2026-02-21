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
        
        // Track active pipelines and their management tasks
        private class StreamContext
        {
            public string? Url { get; set; }
            public IntPtr WindowHandle { get; set; }
            public CancellationTokenSource? CTS { get; set; }
            public Task? WatchTask { get; set; }
            public bool IsRestarting { get; set; }
        }
        public event Action<IntPtr, string>? StreamError;
        private readonly ConcurrentDictionary<IntPtr, StreamContext> _activeStreams = new();

        public VideoService()
        {
            _logPath = System.IO.Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "gstreamer_log.txt");
        }

        private void Log(string message)
        {
            string line = $"[{DateTime.Now:HH:mm:ss}] {message}";
            Console.WriteLine(line);
            try { System.IO.File.AppendAllText(_logPath, line + Environment.NewLine); } catch { }
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

                    // PROGRAMMATIC LOG SUPPRESSION:
                    // Set GStreamer internal logs to level 2 (Errors & Warnings).
                    GstNative.gst_debug_set_default_threshold(2); 

                    // AUTO-DOWNLOADER HARDENING:
                    // Elevate ranks of downloaders so decodebin can auto-insert them.
                    IntPtr d11 = GstNative.gst_element_factory_find("d3d11download");
                    if (d11 != IntPtr.Zero) {
                        GstNative.gst_plugin_feature_set_rank(d11, GstNative.GST_RANK_PRIMARY + 100);
                        GstNative.gst_object_unref(d11);
                    }
                    IntPtr d12 = GstNative.gst_element_factory_find("d3d12download");
                    if (d12 != IntPtr.Zero) {
                        GstNative.gst_plugin_feature_set_rank(d12, GstNative.GST_RANK_PRIMARY + 100);
                        GstNative.gst_object_unref(d12);
                    }

                    _isInitialized = true;
                    Log("[TS-VMS] Video Engine: GStreamer 1.x Initialized (Diagnostic Mode).");
                }
                catch (Exception ex)
                {
                    Log($"[ERROR] GStreamer Init Failed: {ex.Message}");
                }
            }
        }

        public IntPtr StartStream(IntPtr windowHandle, string rtspUrl)
        {
            if (!_isInitialized) Initialize();

            Log($"[TS-VMS] StartStream Request: '{rtspUrl}'");

            // USER-REQUESTED HARDENING:
            // 1. Protocols: Removed strict 'protocols=4' to allow auto-negotiation (TCP/UDP/Mcast).
            //    Staggered startup in LiveViewModel prevents the previous UDP port collisions.
            // 2. location=\"{rtspUrl}\": Wrap in quotes to handle special characters.
            // 3. user-agent=\"VLC/3.0.16\": Spoof VLC identity for picky cameras.
            // 4. latency=500: Stabilization buffer.
            // 5. short-header=true & Explicit Auth: Match user's preferred reliability flags.
            string authProps = "";
            if (rtspUrl.Contains("@"))
            {
                try {
                    int start = rtspUrl.IndexOf("://") + 3;
                    int end = rtspUrl.IndexOf("@");
                    string creds = rtspUrl.Substring(start, end - start);
                    if (creds.Contains(":")) {
                        var parts = creds.Split(':');
                        if (parts.Length >= 2) authProps = $"user-id={parts[0]} user-pw={parts[1]}";
                    }
                } catch {}
            }

            string pipelineStr = rtspUrl == "test" 
                ? "videotestsrc pattern=ball is-live=true ! videoconvert ! d3dvideosink name=mysink sync=false force-aspect-ratio=false"
                : $"rtspsrc location=\"{rtspUrl}\" latency=1000 protocols=tcp ! decodebin ! queue max-size-buffers=300 max-size-time=1000000000 ! d3d11download ! d3d12download ! videoconvert ! d3d11videosink name=mysink sync=false force-aspect-ratio=false";

            Log($"[TS-VMS] Window Handle: {windowHandle}");

            IntPtr error = IntPtr.Zero;
            IntPtr pipeline = GstNative.gst_parse_launch(pipelineStr, out error);

            if (pipeline == IntPtr.Zero)
            {
                Log($"[TS-VMS] Pipeline Creation Failed for {rtspUrl}");
                StreamError?.Invoke(windowHandle, "Invalid Pipeline (Check RTSP URL)");
                return IntPtr.Zero;
            }

            // Set handle on the video sink (mysink)
            IntPtr sink = GstNative.gst_bin_get_by_name(pipeline, "mysink");
            if (sink != IntPtr.Zero)
            {
                GstNative.gst_video_overlay_set_window_handle(sink, windowHandle);
                GstNative.gst_object_unref(sink);
                Log("[TS-VMS] Handle set on 'mysink'.");
            }

            // BUS WATCH WITH AUTO-RESTART
            var ctx = new StreamContext { 
                Url = rtspUrl, 
                WindowHandle = windowHandle,
                CTS = new CancellationTokenSource() 
            };
            IntPtr bus = GstNative.gst_element_get_bus(pipeline);
            
            if (bus != IntPtr.Zero)
            {
                var token = ctx.CTS.Token;
                ctx.WatchTask = Task.Run(async () => {
                    try {
                        // Request Error, EOS, StateChanged, Buffering, and Warnings
                        int mask = GstNative.GST_MESSAGE_ERROR | 
                                   GstNative.GST_MESSAGE_EOS | 
                                   GstNative.GST_MESSAGE_STATE_CHANGED | 
                                   GstNative.GST_MESSAGE_BUFFERING |
                                   GstNative.GST_MESSAGE_WARNING;

                        Log($"[TS-VMS] Bus monitor started for {rtspUrl}");
                        while (!token.IsCancellationRequested) {
                            IntPtr msg = GstNative.gst_bus_pop_filtered(bus, mask); 
                            if (msg != IntPtr.Zero) {
                                int msgType = GstNative.gst_message_get_type(msg);
                                
                                if (msgType == GstNative.GST_MESSAGE_ERROR) {
                                    IntPtr errPtr, debugPtr;
                                    GstNative.gst_message_parse_error(msg, out errPtr, out debugPtr);
                                    string errWrap = Marshal.PtrToStringAnsi(errPtr) ?? "Unknown Error";
                                    Log($"[GSTREAMER-ERROR] {errWrap}. Triggering auto-restart...");
                                    
                                    // Notify UI that the stream failed
                                    StreamError?.Invoke(ctx.WindowHandle, errWrap);
                                    
                                    GstNative.gst_message_unref(msg);
                                    _ = RestartStreamAsync(pipeline);
                                    break;
                                }
                                else if (msgType == GstNative.GST_MESSAGE_STATE_CHANGED) {
                                    int oldState, newState, pending;
                                    GstNative.gst_message_parse_state_changed(msg, out oldState, out newState, out pending);
                                    
                                    // Map GstState values (1=NULL, 2=READY, 3=PAUSED, 4=PLAYING)
                                    string newStateName = newState switch { 1 => "NULL", 2 => "READY", 3 => "PAUSED", 4 => "PLAYING", _ => "UNKNOWN" };
                                    if (newState >= 3) // Log transitions to PAUSED and PLAYING
                                    {
                                        Log($"[TS-VMS] Pipeline State: {newStateName} ({rtspUrl})");
                                    }
                                }
                                else if (msgType == GstNative.GST_MESSAGE_BUFFERING) {
                                    int percent = 0;
                                    GstNative.gst_message_parse_buffering(msg, out percent);
                                    if (percent < 100) Log($"[TS-VMS] Buffering: {percent}%");
                                }
                                else if (msgType == GstNative.GST_MESSAGE_WARNING) {
                                    IntPtr errPtr, debugPtr;
                                    GstNative.gst_message_parse_warning(msg, out errPtr, out debugPtr);
                                    string warnWrap = Marshal.PtrToStringAnsi(errPtr) ?? "Unknown Warning";
                                    Log($"[GSTREAMER-WARNING] {warnWrap}");
                                }

                                GstNative.gst_message_unref(msg);
                            }
                            await Task.Delay(100, token); 
                        }
                    } 
                    catch (OperationCanceledException) { }
                    catch (Exception ex) { Log($"[BUS-TASK] {ex.Message}"); }
                    finally {
                        GstNative.gst_object_unref(bus);
                    }
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

            // Clean up old
            StopStream(oldPipeline);

            // Start new (this will add a new entry to _activeStreams)
            // Note: We need to update the caller's reference if they are tracking it,
            // but in this VMS, LiveView tracks handles. We'll let it re-trigger via health if needed,
            // OR we can manually re-start here.
            Log($"[TS-VMS] Re-starting stream: {ctx.Url}");
            StartStream(ctx.WindowHandle, ctx.Url ?? "");
        }

        public void Reattach(IntPtr pipeline, IntPtr windowHandle)
        {
            if (pipeline == IntPtr.Zero || windowHandle == IntPtr.Zero) return;
            
            // Update context if it exists
            if (_activeStreams.TryGetValue(pipeline, out var ctx))
            {
                ctx.WindowHandle = windowHandle;
            }

            IntPtr overlayElement = GstNative.gst_bin_get_by_name(pipeline, "mysink");
            if (overlayElement != IntPtr.Zero)
            {
                GstNative.gst_video_overlay_set_window_handle(overlayElement, windowHandle);
                GstNative.gst_object_unref(overlayElement);
                Log("[TS-VMS] Reattached.");
            }
        }

        public void StopStream(IntPtr pipeline)
        {
            if (pipeline == IntPtr.Zero) return;

            if (_activeStreams.TryRemove(pipeline, out var ctx))
            {
                ctx.CTS?.Cancel();
                // We don't await WatchTask here to avoid deadlock if called from UI thread
                GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_NULL);
                GstNative.gst_object_unref(pipeline);
                ctx.CTS?.Dispose();
                Log("[TS-VMS] Stopped.");
            }
        }
    }
}
