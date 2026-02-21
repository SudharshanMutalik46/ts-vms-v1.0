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
            // Use 0,0,1,1 as initial size; WPF will resize it via OnWindowPositionChanged
            Handle = CreateWindowEx(
                0, 
                className, 
                "",
                WS_CHILD | WS_CLIPSIBLINGS, // Removed WS_VISIBLE to prevent flash-before-ui
                0, 0, 100, 100,
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

        [DllImport("user32.dll")]
        static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);

        [DllImport("user32.dll")]
        static extern bool SetLayeredWindowAttributes(IntPtr hwnd, uint crKey, byte bAlpha, uint dwFlags);

        [DllImport("user32.dll")]
        static extern int GetWindowLong(IntPtr hWnd, int nIndex);

        [DllImport("user32.dll")]
        static extern int SetWindowLong(IntPtr hWnd, int nIndex, int dwNewLong);

        private const int SW_SHOW = 5;
        private const int GWL_EXSTYLE = -20;
        private const int WS_EX_LAYERED = 0x80000;
        private const int LWA_ALPHA = 0x2;

        public VideoCanvas()
        {
            // We NO LONGER set this.Visibility = Hidden here. 
            // Local values override Bindings, which was causing offline cameras 
            // to show a "white" empty video surface instead of the "NO SIGNAL" UI.
            
            this.IsVisibleChanged += VideoCanvas_IsVisibleChanged;
            this.Loaded += VideoCanvas_Loaded;
        }

        private void VideoCanvas_Loaded(object sender, System.Windows.RoutedEventArgs e)
        {
            // The 150ms rendering delay is now robustly handled in IsVisibleChanged
        }

        private async void VideoCanvas_IsVisibleChanged(object sender, System.Windows.DependencyPropertyChangedEventArgs e)
        {
            if ((bool)e.NewValue) // Becoming Visible
            {
                System.Diagnostics.Debug.WriteLine($"[VideoCanvas] Becoming Visible. Starting reveal...");
                await RevealVideoAsync();
            }
            else // Becoming Hidden (Camera went offline!)
            {
                System.Diagnostics.Debug.WriteLine($"[VideoCanvas] Becoming Hidden.");
                if (Handle != IntPtr.Zero) ShowWindow(Handle, 0); 
            }
        }

        private async System.Threading.Tasks.Task RevealVideoAsync()
        {
            // 1. Wait for Handle (Ensures logic works even if fired before BuildWindowCore)
            int retries = 50; // 500ms max wait
            while (Handle == IntPtr.Zero && retries-- > 0)
            {
                await System.Threading.Tasks.Task.Delay(10);
                if (!this.IsVisible) return;
            }

            if (Handle == IntPtr.Zero) return; // Still no handle, bail.

            // 2. Prepare for Layering (Opacity control)
            int style = GetWindowLong(Handle, GWL_EXSTYLE);
            SetWindowLong(Handle, GWL_EXSTYLE, style | WS_EX_LAYERED);
            SetLayeredWindowAttributes(Handle, 0, 0, LWA_ALPHA); // Start at 0% opacity

            // 3. Hide HWND initially to ensure WPF renders background first
            ShowWindow(Handle, 0); // SW_HIDE

            // 4. Synchronization delay (Increased to 400ms)
            // Combined with 500ms in MainWindow, this ensures the UI has over 900ms 
            // of head-start to paint before the Win32 surface appears.
            await System.Threading.Tasks.Task.Delay(400); 
            
            if (!this.IsVisible) return;

            // 5. Show at 0 opacity
            ShowWindow(Handle, SW_SHOW);

            // 6. Smooth Fade In Animation (approx 250ms)
            for (int i = 0; i <= 255; i += 32) // 8 steps
            {
                if (!this.IsVisible) break;
                SetLayeredWindowAttributes(Handle, 0, (byte)Math.Min(255, i), LWA_ALPHA);
                await System.Threading.Tasks.Task.Delay(30);
            }

            // 7. Ensure final state is fully opaque
            if (this.IsVisible) SetLayeredWindowAttributes(Handle, 0, 255, LWA_ALPHA);
        }
    }
}
