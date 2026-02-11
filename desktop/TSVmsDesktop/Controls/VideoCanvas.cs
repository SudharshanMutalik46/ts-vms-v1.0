using System;
using System.Runtime.InteropServices;
using System.Windows.Interop;

namespace TSVmsDesktop.Controls
{
    public class VideoCanvas : HwndHost
    {
        internal const int WS_CHILD = 0x40000000;
        internal const int WS_VISIBLE = 0x10000000;
        internal const int WS_CLIPSIBLINGS = 0x04000000; // Critical for overlapping controls

        public new IntPtr Handle { get; private set; }

        private static WndProcDelegate? _wndProcDelegate;
        private static bool _classRegistered = false;

        protected override HandleRef BuildWindowCore(HandleRef hwndParent)
        {
            // 1. REGISTER CUSTOM CLASS (Only once)
            string className = "TSVideoClass";
            
            if (!_classRegistered)
            {
                WNDCLASS wc = new WNDCLASS();
                wc.style = 0; 
                _wndProcDelegate = new WndProcDelegate(StaticWndProc);
                wc.lpfnWndProc = Marshal.GetFunctionPointerForDelegate(_wndProcDelegate);
                wc.cbClsExtra = 0;
                wc.cbWndExtra = 0;
                wc.hInstance = Marshal.GetHINSTANCE(typeof(VideoCanvas).Module);
                wc.hIcon = IntPtr.Zero;
                wc.hCursor = LoadCursor(IntPtr.Zero, 32512); // IDC_ARROW
                wc.hbrBackground = IntPtr.Zero; // <--- CRITICAL: NO BACKGROUND PAINTING!
                wc.lpszMenuName = "";
                wc.lpszClassName = className;

                RegisterClass(ref wc);
                _classRegistered = true;
            }

            // 2. CREATE WINDOW
            Handle = CreateWindowEx(
                0, 
                className, 
                "",
                WS_CHILD | WS_VISIBLE | WS_CLIPSIBLINGS, 
                0, 0, (int)Width, (int)Height,
                hwndParent.Handle,
                IntPtr.Zero,
                Marshal.GetHINSTANCE(typeof(VideoCanvas).Module),
                IntPtr.Zero);

            return new HandleRef(this, Handle);
        }

        protected override void DestroyWindowCore(HandleRef hwnd)
        {
            DestroyWindow(hwnd.Handle);
        }

        // Dummy WndProc to handle basic messages
        private static IntPtr StaticWndProc(IntPtr hwnd, uint msg, IntPtr wParam, IntPtr lParam)
        {
            return DefWindowProc(hwnd, msg, wParam, lParam);
        }

        // --- P/INVOKE DECLARATIONS ---
        
        [StructLayout(LayoutKind.Sequential)]
        public struct WNDCLASS
        {
            public uint style;
            public IntPtr lpfnWndProc;
            public int cbClsExtra;
            public int cbWndExtra;
            public IntPtr hInstance;
            public IntPtr hIcon;
            public IntPtr hCursor;
            public IntPtr hbrBackground;
            public string lpszMenuName;
            public string lpszClassName;
        }

        private delegate IntPtr WndProcDelegate(IntPtr hwnd, uint msg, IntPtr wParam, IntPtr lParam);

        [DllImport("user32.dll")]
        static extern ushort RegisterClass(ref WNDCLASS lpWndClass);

        [DllImport("user32.dll")]
        static extern IntPtr DefWindowProc(IntPtr hWnd, uint uMsg, IntPtr wParam, IntPtr lParam);

        [DllImport("user32.dll")]
        static extern IntPtr LoadCursor(IntPtr hInstance, int lpCursorName);

        [DllImport("user32.dll", EntryPoint = "CreateWindowEx", CharSet = CharSet.Unicode)]
        internal static extern IntPtr CreateWindowEx(int dwExStyle, string lpszClassName, string lpszWindowName, int style, int x, int y, int width, int height, IntPtr hwndParent, IntPtr hMenu, IntPtr hInst, IntPtr pvParam);

        [DllImport("user32.dll", EntryPoint = "DestroyWindow", CharSet = CharSet.Unicode)]
        internal static extern bool DestroyWindow(IntPtr hwnd);
    }
}
