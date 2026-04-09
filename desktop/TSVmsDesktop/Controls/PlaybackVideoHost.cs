using System;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows.Interop;

namespace TSVmsDesktop.Controls
{
    public class PlaybackVideoHost : HwndHost
    {
        private const int WM_WINDOWPOSCHANGING = 0x0046;
        private const int WS_CHILD = 0x40000000;
        private const int WS_VISIBLE = 0x10000000;
        private const int WS_CLIPCHILDREN = 0x02000000;
        private const int WS_CLIPSIBLINGS = 0x04000000;
        private const int BLACK_BRUSH = 4;

        private const string WndClassName = "TSVmsPlaybackVideoHost";

        private static int _classRegistered;

        private IntPtr _hwnd = IntPtr.Zero;

        public IntPtr WindowHandle => _hwnd;

        public event EventHandler<IntPtr>? HandleCreated;

        [StructLayout(LayoutKind.Sequential)]
        private struct WINDOWPOS
        {
            public IntPtr hwnd;
            public IntPtr hwndInsertAfter;
            public int x;
            public int y;
            public int cx;
            public int cy;
            public uint flags;
        }

        [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
        private struct WNDCLASSEX
        {
            public uint cbSize;
            public uint style;
            public IntPtr lpfnWndProc;
            public int cbClsExtra;
            public int cbWndExtra;
            public IntPtr hInstance;
            public IntPtr hIcon;
            public IntPtr hCursor;
            public IntPtr hbrBackground;
            public string? lpszMenuName;
            public string lpszClassName;
            public IntPtr hIconSm;
        }

        [StructLayout(LayoutKind.Sequential)]
        private struct RECT
        {
            public int left;
            public int top;
            public int right;
            public int bottom;
        }

        protected override HandleRef BuildWindowCore(HandleRef hwndParent)
        {
            EnsureClassRegistered();

            int width = Math.Max(64, (int)Math.Round(ActualWidth));
            int height = Math.Max(64, (int)Math.Round(ActualHeight));

            _hwnd = CreateWindowEx(
                0,
                WndClassName,
                string.Empty,
                WS_CHILD | WS_VISIBLE | WS_CLIPCHILDREN | WS_CLIPSIBLINGS,
                0,
                0,
                width,
                height,
                hwndParent.Handle,
                IntPtr.Zero,
                GetModuleHandle(null),
                IntPtr.Zero);

            if (_hwnd == IntPtr.Zero)
            {
                int err = Marshal.GetLastWin32Error();
                throw new InvalidOperationException(
                    $"CreateWindowEx('{WndClassName}') failed: Win32 error {err}");
            }

            HandleCreated?.Invoke(this, _hwnd);
            return new HandleRef(this, _hwnd);
        }

        protected override void DestroyWindowCore(HandleRef hwnd)
        {
            DestroyWindow(hwnd.Handle);
            _hwnd = IntPtr.Zero;
        }

        public void ClearSurface()
        {
            if (_hwnd == IntPtr.Zero)
                return;

            if (GetClientRect(_hwnd, out var rc))
            {
                IntPtr brush = GetStockObject(BLACK_BRUSH);
                FillRect(_hwnd, ref rc, brush);
            }
        }


        protected override IntPtr WndProc(IntPtr hwnd, int msg, IntPtr wParam, IntPtr lParam, ref bool handled)
        {
            if (msg == WM_WINDOWPOSCHANGING)
            {
                var pos = Marshal.PtrToStructure<WINDOWPOS>(lParam);
                bool changed = false;

                // Keep the native sink backed by a minimum drawable surface so it
                // can initialize cleanly before the WPF layout settles.
                if (pos.cx < 64) { pos.cx = 64; changed = true; }
                if (pos.cy < 64) { pos.cy = 64; changed = true; }

                if (changed)
                    Marshal.StructureToPtr(pos, lParam, false);
            }

            return base.WndProc(hwnd, msg, wParam, lParam, ref handled);
        }

        protected override void OnWindowPositionChanged(System.Windows.Rect rcBoundingBox)
        {
            if (rcBoundingBox.IsEmpty)
            {
                base.OnWindowPositionChanged(rcBoundingBox);
                return;
            }

            // Ensure we never pass zero-size to the base class as it can cause 
            // the hosted native window to be hidden or incorrectly clipped.
            double safeWidth = Math.Max(64, rcBoundingBox.Width);
            double safeHeight = Math.Max(64, rcBoundingBox.Height);

            base.OnWindowPositionChanged(new System.Windows.Rect(rcBoundingBox.X, rcBoundingBox.Y, safeWidth, safeHeight));
            
            // If the window is ready, clear the surface to prevent "ghosting" 
            // from previous layout states during rapid resizing.
            if (_hwnd != IntPtr.Zero)
            {
                ClearSurface();
            }
        }

        private static void EnsureClassRegistered()
        {
            if (Interlocked.CompareExchange(ref _classRegistered, 1, 0) != 0)
                return;

            var wc = new WNDCLASSEX
            {
                cbSize = (uint)Marshal.SizeOf<WNDCLASSEX>(),
                style = 0,
                lpfnWndProc = GetDefWindowProcPtr(),
                cbClsExtra = 0,
                cbWndExtra = 0,
                hInstance = GetModuleHandle(null),
                hIcon = IntPtr.Zero,
                hCursor = IntPtr.Zero,
                hbrBackground = GetStockObject(BLACK_BRUSH),
                lpszMenuName = null,
                lpszClassName = WndClassName,
                hIconSm = IntPtr.Zero,
            };

            ushort atom = RegisterClassEx(ref wc);
            if (atom == 0)
            {
                int err = Marshal.GetLastWin32Error();
                if (err != 1410)
                {
                    throw new InvalidOperationException(
                        $"RegisterClassEx failed for '{WndClassName}': Win32 error {err}");
                }
            }
        }

        private static IntPtr GetDefWindowProcPtr()
        {
            IntPtr user32 = LoadLibrary("user32.dll");
            return GetProcAddress(user32, "DefWindowProcW");
        }

        [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern ushort RegisterClassEx([In] ref WNDCLASSEX lpwcx);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern IntPtr GetModuleHandle(string? lpModuleName);

        [DllImport("user32.dll", EntryPoint = "CreateWindowExW", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern IntPtr CreateWindowEx(
            int dwExStyle,
            string lpszClassName,
            string lpszWindowName,
            int style,
            int x,
            int y,
            int width,
            int height,
            IntPtr hwndParent,
            IntPtr hMenu,
            IntPtr hInst,
            IntPtr pvParam);

        [DllImport("user32.dll", EntryPoint = "DestroyWindow", CharSet = CharSet.Unicode)]
        private static extern bool DestroyWindow(IntPtr hwnd);

        [DllImport("user32.dll", SetLastError = true)]
        private static extern bool GetClientRect(IntPtr hWnd, out RECT lpRect);

        [DllImport("kernel32.dll", CharSet = CharSet.Ansi)]
        private static extern IntPtr LoadLibrary(string lpFileName);

        [DllImport("kernel32.dll", CharSet = CharSet.Ansi)]
        private static extern IntPtr GetProcAddress(IntPtr hModule, string lpProcName);

        [DllImport("user32.dll")]
        private static extern int FillRect(IntPtr hDC, [In] ref RECT lprc, IntPtr hbr);

        [DllImport("gdi32.dll", SetLastError = true)]
        private static extern IntPtr GetStockObject(int fnObject);
    }
}
