using System;
using System.Runtime.InteropServices;

namespace TSVmsDesktop.Interop
{
    internal static class NativePlayback
    {
        private const string DllName = "TSVmsPlaybackEngine.dll";

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Unicode)]
        internal static extern IntPtr tsplay_create();

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern void tsplay_destroy(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_initialize(IntPtr engine, IntPtr hwnd);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_set_window_handle(IntPtr engine, IntPtr hwnd);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_set_window_size(IntPtr engine, int width, int height);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Unicode)]
        internal static extern int tsplay_set_media_path(IntPtr engine, string path);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl, CharSet = CharSet.Unicode)]
        internal static extern int tsplay_set_playlist(
            IntPtr engine,
            [MarshalAs(UnmanagedType.LPArray, ArraySubType = UnmanagedType.LPWStr)] string[] paths,
            int count,
            int startIndex);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_get_playlist_index(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern double tsplay_get_rate(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_set_last_sample_enabled(IntPtr engine, int enabled);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_force_expose(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_play(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_pause(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_stop(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_seek_seconds(IntPtr engine, double seconds);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_set_rate(IntPtr engine, double rate);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_step_frame(IntPtr engine, int frames);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern double tsplay_get_position_seconds(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern double tsplay_get_duration_seconds(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_get_state(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int tsplay_has_reached_eos(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern IntPtr tsplay_get_last_error(IntPtr engine);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int TSPlayback_SetRotationDegrees(IntPtr engine, int degrees);

        [DllImport(DllName, CallingConvention = CallingConvention.Cdecl)]
        internal static extern int TSPlayback_GetRotationDegrees(IntPtr engine);
    }
}
