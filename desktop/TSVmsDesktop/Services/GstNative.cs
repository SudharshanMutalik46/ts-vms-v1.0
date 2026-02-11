using System;
using System.Runtime.InteropServices;

namespace TSVmsDesktop.Services
{
    // This class maps the C functions from GStreamer DLLs to C#
    public static class GstNative
    {
        private const string DllName = "gstreamer-1.0-0.dll";
        private const string VideoDllName = "gstvideo-1.0-0.dll";

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_init(ref int argc, ref IntPtr argv);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern IntPtr gst_parse_launch(string pipeline_description, out IntPtr error);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int gst_element_set_state(IntPtr element, int state);

        [DllImport(VideoDllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_video_overlay_set_window_handle(IntPtr overlay, IntPtr handle);

        // NEW: Function to find an element inside the pipeline by its name
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern IntPtr gst_bin_get_by_name(IntPtr bin, string name);

        // NEW: Function to release objects (cleanup)
        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_object_unref(IntPtr obj);

        // States: 1=NULL, 2=READY, 3=PAUSED, 4=PLAYING
        public const int GST_STATE_PLAYING = 4;
        public const int GST_STATE_NULL = 1;
    }
}
