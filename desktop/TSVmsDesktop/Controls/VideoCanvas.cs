using System;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows.Interop;

namespace TSVmsDesktop.Controls
{
    public class VideoCanvas : HwndHost
    {
        internal const int WS_CHILD      = 0x40000000;
        internal const int WS_VISIBLE    = 0x10000000;
        internal const int WS_CLIPCHILDREN = 0x02000000;
        internal const int WS_CLIPSIBLINGS = 0x04000000;
        private const int BLACK_BRUSH = 4;

        private const int WM_WINDOWPOSCHANGING = 0x0046;
        private const int WM_MOUSEMOVE = 0x0200;
        private const int WM_MOUSELEAVE = 0x02A3;
        private const int TME_LEAVE = 0x00000002;

        // Custom WNDCLASS name — d3d11videosink requires a subclassable window.
        // The built-in "static" class causes d3d11videosink to fall back to creating
        // its own top-level popup window instead of embedding into the child HWND.
        private const string WndClassName = "TSVmsVideoCanvas";

        private static int _classRegistered = 0; // 0 = unregistered, 1 = registered
        private bool _mouseTracking;

        public event EventHandler? NativeMouseEnter;
        public event EventHandler? NativeMouseMove;
        public event EventHandler? NativeMouseLeave;

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

        // WNDCLASSEX for RegisterClassEx
        [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
        private struct WNDCLASSEX
        {
            public uint    cbSize;
            public uint    style;
            public IntPtr  lpfnWndProc;   // DefWindowProc — lets d3d11videosink subclass freely
            public int     cbClsExtra;
            public int     cbWndExtra;
            public IntPtr  hInstance;
            public IntPtr  hIcon;
            public IntPtr  hCursor;
            public IntPtr  hbrBackground;
            public string? lpszMenuName;
            public string  lpszClassName;
            public IntPtr  hIconSm;
        }

        public new IntPtr Handle { get; private set; }

        /// <summary>
        /// Registers the custom WNDCLASS once per process.
        /// Uses DefWindowProc so d3d11videosink can SetWindowSubclass / SetWindowLongPtr
        /// without hitting the "static" class restrictions.
        /// </summary>
        private static void EnsureClassRegistered()
        {
            if (Interlocked.CompareExchange(ref _classRegistered, 1, 0) != 0)
                return; // already registered

            var wc = new WNDCLASSEX
            {
                cbSize        = (uint)Marshal.SizeOf<WNDCLASSEX>(),
                style         = 0,
                lpfnWndProc   = GetDefWindowProcPtr(),
                cbClsExtra    = 0,
                cbWndExtra    = 0,
                hInstance     = GetModuleHandle(null),
                hIcon         = IntPtr.Zero,
                hCursor       = IntPtr.Zero,
                hbrBackground = GetStockObject(BLACK_BRUSH),
                lpszMenuName  = null,
                lpszClassName = WndClassName,
                hIconSm       = IntPtr.Zero,
            };

            ushort atom = RegisterClassEx(ref wc);
            if (atom == 0)
            {
                int err = Marshal.GetLastWin32Error();
                // ERROR_CLASS_ALREADY_EXISTS (1410) is fine — another thread beat us.
                if (err != 1410)
                    throw new InvalidOperationException(
                        $"RegisterClassEx failed for '{WndClassName}': Win32 error {err}");
            }
        }

        protected override HandleRef BuildWindowCore(HandleRef hwndParent)
        {
            EnsureClassRegistered();

            int style = WS_CHILD | WS_VISIBLE | WS_CLIPCHILDREN | WS_CLIPSIBLINGS;

            Handle = CreateWindowEx(
                0,
                WndClassName,   // custom class — fully subclassable by d3d11videosink
                "",
                style,
                0, 0, (int)this.ActualWidth, (int)this.ActualHeight,
                hwndParent.Handle,
                IntPtr.Zero,
                GetModuleHandle(null),
                IntPtr.Zero);

            if (Handle == IntPtr.Zero)
            {
                int err = Marshal.GetLastWin32Error();
                throw new InvalidOperationException(
                    $"CreateWindowEx('{WndClassName}') failed: Win32 error {err}");
            }

            return new HandleRef(this, Handle);
        }

        protected override void DestroyWindowCore(HandleRef hwnd)
        {
            _mouseTracking = false;
            DestroyWindow(hwnd.Handle);
        }

        protected override IntPtr WndProc(IntPtr hwnd, int msg, IntPtr wParam, IntPtr lParam, ref bool handled)
        {
            if (msg == WM_WINDOWPOSCHANGING)
            {
                var pos = Marshal.PtrToStructure<WINDOWPOS>(lParam);
                bool changed = false;

                // NV12 hardware decoding requires swapchains larger than GPU macroblock size.
                // Enforce a safe minimum of 64×64.
                if (pos.cx < 64) { pos.cx = 64; changed = true; }
                if (pos.cy < 64) { pos.cy = 64; changed = true; }

                if (changed)
                    Marshal.StructureToPtr(pos, lParam, false);
            }
            else if (msg == WM_MOUSEMOVE)
            {
                if (!_mouseTracking)
                {
                    TrackMouseEventForLeave(hwnd);
                    _mouseTracking = true;
                    NativeMouseEnter?.Invoke(this, EventArgs.Empty);
                }

                NativeMouseMove?.Invoke(this, EventArgs.Empty);
            }
            else if (msg == WM_MOUSELEAVE)
            {
                _mouseTracking = false;
                NativeMouseLeave?.Invoke(this, EventArgs.Empty);
            }

            return base.WndProc(hwnd, msg, wParam, lParam, ref handled);
        }

        protected override void OnWindowPositionChanged(System.Windows.Rect rcBoundingBox)
        {
            double safeWidth  = rcBoundingBox.IsEmpty || rcBoundingBox.Width  < 64 ? 64 : rcBoundingBox.Width;
            double safeHeight = rcBoundingBox.IsEmpty || rcBoundingBox.Height < 64 ? 64 : rcBoundingBox.Height;
            double safeX      = rcBoundingBox.IsEmpty ? 0 : rcBoundingBox.X;
            double safeY      = rcBoundingBox.IsEmpty ? 0 : rcBoundingBox.Y;

            base.OnWindowPositionChanged(new System.Windows.Rect(safeX, safeY, safeWidth, safeHeight));
        }

        // ── P/Invoke ────────────────────────────────────────────────────────────

        [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern ushort RegisterClassEx([In] ref WNDCLASSEX lpwcx);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern IntPtr GetModuleHandle(string? lpModuleName);

        [DllImport("user32.dll", EntryPoint = "CreateWindowExW", CharSet = CharSet.Unicode, SetLastError = true)]
        internal static extern IntPtr CreateWindowEx(
            int dwExStyle, string lpszClassName, string lpszWindowName, int style,
            int x, int y, int width, int height,
            IntPtr hwndParent, IntPtr hMenu, IntPtr hInst, IntPtr pvParam);

        [DllImport("user32.dll", EntryPoint = "DestroyWindow", CharSet = CharSet.Unicode)]
        internal static extern bool DestroyWindow(IntPtr hwnd);

        /// <summary>
        /// Returns the function pointer for DefWindowProcW so we can store it in WNDCLASSEX.
        /// </summary>
        private static IntPtr GetDefWindowProcPtr()
        {
            IntPtr user32 = LoadLibrary("user32.dll");
            return GetProcAddress(user32, "DefWindowProcW");
        }

        [DllImport("kernel32.dll", CharSet = CharSet.Ansi)]
        private static extern IntPtr LoadLibrary(string lpFileName);

        [DllImport("kernel32.dll", CharSet = CharSet.Ansi)]
        private static extern IntPtr GetProcAddress(IntPtr hModule, string lpProcName);

        [DllImport("gdi32.dll", SetLastError = true)]
        private static extern IntPtr GetStockObject(int fnObject);

        [StructLayout(LayoutKind.Sequential)]
        private struct TRACKMOUSEEVENT
        {
            public uint cbSize;
            public uint dwFlags;
            public IntPtr hwndTrack;
            public uint dwHoverTime;
        }

        [DllImport("user32.dll", SetLastError = true)]
        private static extern bool TrackMouseEvent(ref TRACKMOUSEEVENT lpEventTrack);

        private static void TrackMouseEventForLeave(IntPtr hwnd)
        {
            var tme = new TRACKMOUSEEVENT
            {
                cbSize = (uint)Marshal.SizeOf<TRACKMOUSEEVENT>(),
                dwFlags = TME_LEAVE,
                hwndTrack = hwnd,
                dwHoverTime = 0
            };

            TrackMouseEvent(ref tme);
        }
    }
}
