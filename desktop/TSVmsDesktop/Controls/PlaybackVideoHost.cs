using System;
using System.Runtime.InteropServices;
using System.Windows;
using System.Windows.Interop;

namespace TSVmsDesktop.Controls
{
    public class PlaybackVideoHost : HwndHost
    {
        private IntPtr _hwnd = IntPtr.Zero;

        public IntPtr WindowHandle => _hwnd;

        public event EventHandler<IntPtr>? HandleCreated;

        // --- WIN32 CONSTANTS & STRUCTS ---
        private const int WM_WINDOWPOSCHANGING = 0x0046;
        private const int WS_CHILD = 0x40000000;
        private const int WS_VISIBLE = 0x10000000;
        private const int WS_CLIPSIBLINGS = 0x04000000;
        private const int WS_CLIPCHILDREN = 0x02000000;

        [DllImport("user32.dll", SetLastError = true)]
        private static extern IntPtr CreateWindowEx(int dwExStyle, string lpClassName, string lpWindowName, int dwStyle, int x, int y, int nWidth, int nHeight, IntPtr hWndParent, IntPtr hMenu, IntPtr hInstance, IntPtr lpParam);

        [DllImport("user32.dll", SetLastError = true)]
        private static extern bool DestroyWindow(IntPtr hwnd);

        [StructLayout(LayoutKind.Sequential)]
        public struct WINDOWPOS
        {
            public IntPtr hwnd;
            public IntPtr hwndInsertAfter;
            public int x;
            public int y;
            public int cx;
            public int cy;
            public uint flags;
        }

        protected override HandleRef BuildWindowCore(HandleRef hwndParent)
        {
            _hwnd = CreateWindowEx(
                0,
                "static",
                string.Empty,
                WS_CHILD | WS_VISIBLE | WS_CLIPSIBLINGS | WS_CLIPCHILDREN,
                0,
                0,
                100,
                100,
                hwndParent.Handle,
                IntPtr.Zero,
                IntPtr.Zero,
                IntPtr.Zero);

            HandleCreated?.Invoke(this, _hwnd);
            return new HandleRef(this, _hwnd);
        }

        protected override void DestroyWindowCore(HandleRef hwnd)
        {
            DestroyWindow(hwnd.Handle);
            _hwnd = IntPtr.Zero;
        }

        // --- THE FIX: INTERCEPT OS RESIZE MESSAGES ---
        protected override IntPtr WndProc(IntPtr hwnd, int msg, IntPtr wParam, IntPtr lParam, ref bool handled)
        {
            if (msg == WM_WINDOWPOSCHANGING)
            {
                var pos = Marshal.PtrToStructure<WINDOWPOS>(lParam);
                bool changed = false;

                // FIX: NV12 Hardware Decoding requires swapchains to be larger than 
                // the GPU macroblock size. We force a safe minimum of 64x64.
                if (pos.cx < 64) { pos.cx = 64; changed = true; }
                if (pos.cy < 64) { pos.cy = 64; changed = true; }

                if (changed)
                {
                    Marshal.StructureToPtr(pos, lParam, false);
                }
            }

            return base.WndProc(hwnd, msg, wParam, lParam, ref handled);
        }

        protected override void OnWindowPositionChanged(Rect rcBoundingBox)
        {
            // Force the WPF layout engine to also respect the 64x64 minimum
            double safeWidth = rcBoundingBox.IsEmpty || rcBoundingBox.Width < 64 ? 64 : rcBoundingBox.Width;
            double safeHeight = rcBoundingBox.IsEmpty || rcBoundingBox.Height < 64 ? 64 : rcBoundingBox.Height;
            double safeX = rcBoundingBox.IsEmpty ? 0 : rcBoundingBox.X;
            double safeY = rcBoundingBox.IsEmpty ? 0 : rcBoundingBox.Y;

            Rect safeRect = new Rect(safeX, safeY, safeWidth, safeHeight);
            
            base.OnWindowPositionChanged(safeRect);
        }
    }
}
