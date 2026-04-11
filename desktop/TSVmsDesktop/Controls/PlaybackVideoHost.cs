using System;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows;
using System.Windows.Interop;
using System.Windows.Media;

namespace TSVmsDesktop.Controls
{
    public class PlaybackVideoHost : HwndHost
    {
        private const int WS_CHILD = 0x40000000;
        private const int WS_VISIBLE = 0x10000000;
        private const int WS_CLIPCHILDREN = 0x02000000;
        private const int WS_CLIPSIBLINGS = 0x04000000;
        private const int BLACK_BRUSH = 4;

        private const uint SWP_NOZORDER = 0x0004;
        private const uint SWP_NOACTIVATE = 0x0010;
        private const uint SWP_NOOWNERZORDER = 0x0200;

        // Custom WNDCLASS name — must be subclassable so d3d11videosink embeds
        // instead of creating its own top-level popup window.
        private const string WndClassName = "TSVmsPlaybackVideoHost";

        private static int _classRegistered;
        private IntPtr _hwnd = IntPtr.Zero;

        public IntPtr WindowHandle => _hwnd;
        public event EventHandler<IntPtr>? HandleCreated;

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

        protected override HandleRef BuildWindowCore(HandleRef hwndParent)
        {
            EnsureClassRegistered();

            var dpi = VisualTreeHelper.GetDpi(this);
            double scaleX = dpi.DpiScaleX <= 0 ? 1.0 : dpi.DpiScaleX;
            double scaleY = dpi.DpiScaleY <= 0 ? 1.0 : dpi.DpiScaleY;

            int widthPx = Math.Max(64, (int)Math.Round(ActualWidth * scaleX));
            int heightPx = Math.Max(64, (int)Math.Round(ActualHeight * scaleY));

            _hwnd = CreateWindowEx(
                0,
                WndClassName,
                string.Empty,
                WS_CHILD | WS_VISIBLE | WS_CLIPCHILDREN | WS_CLIPSIBLINGS,
                0,
                0,
                widthPx,
                heightPx,
                hwndParent.Handle,
                IntPtr.Zero,
                IntPtr.Zero,
                IntPtr.Zero);

            HandleCreated?.Invoke(this, _hwnd);
            SyncChildBounds();

            return new HandleRef(this, _hwnd);
        }

        protected override void DestroyWindowCore(HandleRef hwnd)
        {
            if (hwnd.Handle != IntPtr.Zero)
                DestroyWindow(hwnd.Handle);

            _hwnd = IntPtr.Zero;
        }

        protected override void OnRenderSizeChanged(SizeChangedInfo sizeInfo)
        {
            base.OnRenderSizeChanged(sizeInfo);
            SyncChildBounds();
        }

        private void SyncChildBounds()
        {
            if (_hwnd == IntPtr.Zero)
                return;

            var dpi = VisualTreeHelper.GetDpi(this);
            double scaleX = dpi.DpiScaleX <= 0 ? 1.0 : dpi.DpiScaleX;
            double scaleY = dpi.DpiScaleY <= 0 ? 1.0 : dpi.DpiScaleY;

            int width = Math.Max(64, (int)Math.Round(ActualWidth * scaleX));
            int height = Math.Max(64, (int)Math.Round(ActualHeight * scaleY));

            SetWindowPos(
                _hwnd,
                IntPtr.Zero,
                0,
                0,
                width,
                height,
                SWP_NOZORDER | SWP_NOACTIVATE | SWP_NOOWNERZORDER);
        }

        [DllImport("user32.dll", EntryPoint = "CreateWindowExW", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern IntPtr CreateWindowEx(
            int dwExStyle,
            string lpClassName,
            string lpWindowName,
            int dwStyle,
            int x,
            int y,
            int nWidth,
            int nHeight,
            IntPtr hWndParent,
            IntPtr hMenu,
            IntPtr hInstance,
            IntPtr lpParam);

        [DllImport("user32.dll", SetLastError = true)]
        private static extern bool DestroyWindow(IntPtr hWnd);

        [DllImport("user32.dll", SetLastError = true)]
        private static extern bool SetWindowPos(
            IntPtr hWnd,
            IntPtr hWndInsertAfter,
            int X,
            int Y,
            int cx,
            int cy,
            uint uFlags);

        [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern ushort RegisterClassEx(ref WNDCLASSEX lpwcx);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern IntPtr GetModuleHandle(string? lpModuleName);

        [DllImport("kernel32.dll", CharSet = CharSet.Ansi)]
        private static extern IntPtr LoadLibrary(string lpFileName);

        [DllImport("kernel32.dll", CharSet = CharSet.Ansi)]
        private static extern IntPtr GetProcAddress(IntPtr hModule, string lpProcName);

        [DllImport("gdi32.dll")]
        private static extern IntPtr GetStockObject(int fnObject);

        private static void EnsureClassRegistered()
        {
            if (Interlocked.CompareExchange(ref _classRegistered, 1, 0) != 0)
                return;

            var wc = new WNDCLASSEX
            {
                cbSize = (uint)Marshal.SizeOf<WNDCLASSEX>(),
                lpfnWndProc = GetDefWindowProcPtr(),
                hInstance = GetModuleHandle(null),
                hbrBackground = GetStockObject(BLACK_BRUSH),
                lpszClassName = WndClassName
            };

            ushort atom = RegisterClassEx(ref wc);
            if (atom == 0)
            {
                int err = Marshal.GetLastWin32Error();
                if (err != 1410) // ERROR_CLASS_ALREADY_EXISTS
                    throw new InvalidOperationException($"RegisterClassEx failed for '{WndClassName}': Win32 error {err}");
            }
        }

        private static IntPtr GetDefWindowProcPtr()
        {
            IntPtr user32 = LoadLibrary("user32.dll");
            return GetProcAddress(user32, "DefWindowProcW");
        }
    }
}
