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
            public string Url { get; set; }
            public IntPtr WindowHandle { get; set; }
            public CancellationTokenSource CTS { get; set; }
            public Task WatchTask { get; set; }
            public bool IsRestarting { get; set; }
        }
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
                    System.Environment.SetEnvironmentVariable("GST_DEBUG", "3");
                    int argc = 0;
                    IntPtr argv = IntPtr.Zero;
                    GstNative.gst_init(ref argc, ref argv);
                    _isInitialized = true;
                    Log("[TS-VMS] GStreamer Backend Initialized Successfully.");
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

            // ULTRA-LOW LATENCY REAL-TIME TUNING:
            // 1. sync=false: Render frames IMMEDIATELY as they arrive (Zero display lag).
            // 2. queue max-size-time=0: Disable time-based buffering to prevent "lag buildup".
            // 3. buffer-size=0: Disable network-level pooling for real-time packet delivery.
            string pipelineStr = rtspUrl == "test" 
                ? "videotestsrc pattern=ball is-live=true ! videoconvert ! d3dvideosink name=mysink sync=false force-aspect-ratio=false"
                : $"uridecodebin uri={rtspUrl} ! queue max-size-buffers=1 max-size-bytes=0 max-size-time=0 ! d3d11upload ! d3d11convert ! d3d11videosink name=mysink sync=false force-aspect-ratio=false";

            Log($"[TS-VMS] Window Handle: {windowHandle}");

            IntPtr error = IntPtr.Zero;
            IntPtr pipeline = GstNative.gst_parse_launch(pipelineStr, out error);

            if (pipeline == IntPtr.Zero)
            {
                Log($"[TS-VMS] Pipeline Creation Failed for {rtspUrl}");
                return IntPtr.Zero;
            }

            // Always set handle on pipeline AND try sink search
            GstNative.gst_video_overlay_set_window_handle(pipeline, windowHandle);
            
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
                        Log($"[TS-VMS] Bus monitor started for {rtspUrl}");
                        while (!token.IsCancellationRequested) {
                            IntPtr msg = GstNative.gst_bus_pop_filtered(bus, 100); 
                            if (msg != IntPtr.Zero) {
                                int msgType = GstNative.gst_message_get_type(msg);
                                
                                if ((msgType & GstNative.GST_MESSAGE_ERROR) != 0) {
                                    IntPtr errPtr, debugPtr;
                                    GstNative.gst_message_parse_error(msg, out errPtr, out debugPtr);
                                    string errWrap = Marshal.PtrToStringAnsi(errPtr) ?? "Unknown Error";
                                    Log($"[GSTREAMER-ERROR] {errWrap}. Triggering auto-restart...");
                                    GstNative.gst_message_unref(msg);
                                    
                                    _ = RestartStreamAsync(pipeline);
                                    break;
                                }
                                else if ((msgType & GstNative.GST_MESSAGE_STATE_CHANGED) != 0) {
                                    int oldState, newState, pending;
                                    GstNative.gst_message_parse_state_changed(msg, out oldState, out newState, out pending);
                                    if (newState == GstNative.GST_STATE_NULL && !ctx.IsRestarting) {
                                        Log("[TS-VMS] State reached NULL. Possible connection loss.");
                                    }
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

            GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_PLAYING);
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
            StartStream(ctx.WindowHandle, ctx.Url);
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
                ctx.CTS.Cancel();
                // We don't await WatchTask here to avoid deadlock if called from UI thread
                GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_NULL);
                GstNative.gst_object_unref(pipeline);
                ctx.CTS.Dispose();
                Log("[TS-VMS] Stopped.");
            }
        }
    }
}
