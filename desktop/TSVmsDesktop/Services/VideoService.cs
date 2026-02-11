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

            string pipelineStr;

            if (rtspUrl == "test")
            {
                // FORCE d3d11videosink. Do NOT use autovideosink.
                pipelineStr = "videotestsrc pattern=ball ! videoconvert ! d3d11videosink name=mysink";
            }
            else
            {
                // REAL RTSP PIPELINE
                // Using d3d11videosink ensures we can attach the Window Handle
                pipelineStr = $"rtspsrc location={rtspUrl} latency=0 ! rtph264depay ! h264parse ! avdec_h264 ! videoconvert ! d3d11videosink name=mysink";
            }

            IntPtr error = IntPtr.Zero;
            IntPtr pipeline = GstNative.gst_parse_launch(pipelineStr, out error);

            if (pipeline == IntPtr.Zero || error != IntPtr.Zero)
            {
                System.Diagnostics.Debug.WriteLine($"[TS-VMS] Failed to create pipeline for {rtspUrl}");
                return IntPtr.Zero;
            }

            // Bind to Window
            // Since we are using d3d11videosink directly, this 'get_by_name' will return the actual sink
            // which supports the Overlay interface. The crash will stop.
            IntPtr overlayElement = GstNative.gst_bin_get_by_name(pipeline, "mysink");
            
            if (overlayElement != IntPtr.Zero)
            {
                GstNative.gst_video_overlay_set_window_handle(overlayElement, windowHandle);
                GstNative.gst_object_unref(overlayElement);
            }
            else 
            {
                 System.Diagnostics.Debug.WriteLine("[TS-VMS] CRITICAL: 'mysink' element not found.");
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
