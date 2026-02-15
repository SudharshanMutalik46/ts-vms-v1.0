using System;
using System.Diagnostics;

namespace TSVmsDesktop.Services
{
    public class VideoService
    {
        private bool _isInitialized = false;
        private string _logPath = System.IO.Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Desktop), "video_debug.txt");

        private void Log(string message)
        {
            string line = $"[{DateTime.Now:HH:mm:ss}] {message}";
            Console.WriteLine(line);
            try { System.IO.File.AppendAllText(_logPath, line + Environment.NewLine); } catch { }
        }

        public void Initialize()
        {
            if (_isInitialized) return;

            try
            {
                // Initialize GStreamer Core
                int argc = 0;
                IntPtr argv = IntPtr.Zero;
                GstNative.gst_init(ref argc, ref argv);
                _isInitialized = true;
                Log("[TS-VMS] GStreamer Backend Initialized Successfully.");
            }
            catch (DllNotFoundException)
            {
                Log("[ERROR] Could not find gstreamer-1.0-0.dll. Check your PATH environment variable.");
            }
            catch (Exception ex)
            {
                Log($"[ERROR] GStreamer Init Failed: {ex.Message}");
            }
        }

        public IntPtr StartStream(IntPtr windowHandle, string rtspUrl)
        {
            if (!_isInitialized) Initialize();

            Log($"[TS-VMS] Attempting to play URL: '{rtspUrl}'");

            string pipelineStr;

            if (rtspUrl == "test")
            {
                pipelineStr = "videotestsrc pattern=ball ! videoconvert ! d3d11videosink name=mysink";
            }
            else
            {
                // HIGH PERFORMANCE PIPELINE
                // 1. latency=500: Increases buffer to 0.5s. Reduces "panic" buffering drops.
                // 2. queue: Adds Multithreading! Separates network, decoding, and rendering.
                // 3. sync=false: Ensures we display frames as fast as they arrive.
                pipelineStr = $"rtspsrc location={rtspUrl} latency=500 protocols=tcp ! queue max-size-buffers=3 ! rtph264depay ! h264parse ! decodebin ! queue max-size-buffers=3 ! videoconvert ! queue min-threshold-buffers=1 ! d3d11videosink name=mysink sync=false";
            }

            IntPtr error = IntPtr.Zero;
            IntPtr pipeline = GstNative.gst_parse_launch(pipelineStr, out error);

            if (pipeline == IntPtr.Zero || error != IntPtr.Zero)
            {
                if (error != IntPtr.Zero)
                     Log($"[TS-VMS] Pipeline Error: Check logs for details.");
                else 
                     Log($"[TS-VMS] Pipeline Creation Failed for {rtspUrl}");
                return IntPtr.Zero;
            }

            IntPtr overlayElement = GstNative.gst_bin_get_by_name(pipeline, "mysink");
            
            if (overlayElement != IntPtr.Zero)
            {
                GstNative.gst_video_overlay_set_window_handle(overlayElement, windowHandle);
                GstNative.gst_object_unref(overlayElement);
            }
            else 
            {
                Log("[TS-VMS] CRITICAL: 'mysink' element not found.");
            }

            GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_PLAYING);
            return pipeline;
        }

        public void StopStream(IntPtr pipeline)
        {
            if (pipeline != IntPtr.Zero)
            {
                GstNative.gst_element_set_state(pipeline, GstNative.GST_STATE_NULL);
                GstNative.gst_object_unref(pipeline); // Properly unref
            }
        }
    }
}
