using System;
using System.Runtime.InteropServices;
using System.Windows.Interop;

namespace TSVmsDesktop.Controls
{
    public class VideoCanvas : HwndHost
    {
        internal const int WS_CHILD = 0x40000000;
        internal const int WS_VISIBLE = 0x10000000;

        public new IntPtr Handle { get; private set; }

        protected override HandleRef BuildWindowCore(HandleRef hwndParent)
        {
            // Use "static" class with SS_BLACKRECT style (0x4) to make it black
            // Or just a standard static control. 
            // Note: In a real GStreamer app, GStreamer paints over this instantly.
            
            Handle = CreateWindowEx(
                0, "static", "",
                WS_CHILD | WS_VISIBLE,
                0, 0, (int)Width, (int)Height,
                hwndParent.Handle,
                IntPtr.Zero,
                IntPtr.Zero,
                IntPtr.Zero);

            return new HandleRef(this, Handle);
        }

        protected override void DestroyWindowCore(HandleRef hwnd)
        {
            DestroyWindow(hwnd.Handle);
        }

        [DllImport("user32.dll", EntryPoint = "CreateWindowEx", CharSet = CharSet.Unicode)]
        internal static extern IntPtr CreateWindowEx(int dwExStyle, string lpszClassName, string lpszWindowName, int style, int x, int y, int width, int height, IntPtr hwndParent, IntPtr hMenu, IntPtr hInst, IntPtr pvParam);

        [DllImport("user32.dll", EntryPoint = "DestroyWindow", CharSet = CharSet.Unicode)]
        internal static extern bool DestroyWindow(IntPtr hwnd);
    }
}
