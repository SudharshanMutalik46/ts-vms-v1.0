using System;
using System.Runtime.InteropServices;

namespace TSVmsDesktop.Services
{
    // This class maps the C functions from GStreamer DLLs to C#
    public static class GstNative
    {
        private const string DllName = "gstreamer-1.0-0.dll";
        private const string VideoDllName = "gstvideo-1.0-0.dll";
        private const string GObjectDllName = "gobject-2.0-0.dll";

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_init(ref int argc, ref IntPtr argv);

        [DllImport(GObjectDllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void g_object_set(IntPtr obj, string first_property_name, double value, IntPtr terminating_null);


        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_debug_set_default_threshold(int level);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern IntPtr gst_parse_launch(string pipeline_description, out IntPtr error);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int gst_element_set_state(IntPtr element, int state);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int gst_element_get_state(IntPtr element, out int state, out int pending, ulong timeout);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern IntPtr gst_bin_get_by_name(IntPtr bin, string name);

        [DllImport(VideoDllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_video_overlay_set_window_handle(IntPtr overlay, IntPtr handle);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_object_unref(IntPtr obj);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern IntPtr gst_element_get_bus(IntPtr element);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern IntPtr gst_bus_pop_filtered(IntPtr bus, int types);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern int gst_message_get_type(IntPtr message);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_message_parse_error(IntPtr message, out IntPtr error, out IntPtr debug);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_message_parse_warning(IntPtr message, out IntPtr error, out IntPtr debug);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_message_parse_state_changed(IntPtr message, out int oldstate, out int newstate, out int pending);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_message_parse_buffering(IntPtr message, out int percent);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_message_unref(IntPtr message);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern IntPtr gst_element_factory_find(string name);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        public static extern void gst_plugin_feature_set_rank(IntPtr feature, uint rank);

        // Rank constants
        public const uint GST_RANK_NONE = 0;
        public const uint GST_RANK_MARGINAL = 64;
        public const uint GST_RANK_SECONDARY = 128;
        public const uint GST_RANK_PRIMARY = 256;

        public const ulong GST_CLOCK_TIME_NONE = unchecked((ulong)-1);

        // States: 1=NULL, 2=READY, 3=PAUSED, 4=PLAYING
        public const int GST_STATE_NULL = 1;
        public const int GST_STATE_READY = 2;
        public const int GST_STATE_PAUSED = 3;
        public const int GST_STATE_PLAYING = 4;

        // Message types
        public const int GST_MESSAGE_EOS = 1;
        public const int GST_MESSAGE_ERROR = 2;
        public const int GST_MESSAGE_WARNING = 4;
        public const int GST_MESSAGE_INFO = 8;
        public const int GST_MESSAGE_TAG = 16;
        public const int GST_MESSAGE_BUFFERING = 32;
        public const int GST_MESSAGE_STATE_CHANGED = 64;
        public const int GST_MESSAGE_ASYNC_DONE = 1024;
    }
}
