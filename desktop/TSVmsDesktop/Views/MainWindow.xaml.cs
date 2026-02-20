using System.Windows;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.Services;
using TSVmsDesktop.ViewModels;
using System.Runtime.InteropServices;

namespace TSVmsDesktop.Views
{
    public partial class MainWindow : Window
    {
        public MainWindow()
        {
            // CRITICAL: Links to MainWindow.xaml
            InitializeComponent();
            DataContext = App.Current.Services.GetRequiredService<MainViewModel>();
            if (DataContext is MainViewModel vm)
            {
                vm.PropertyChanged += OnViewModelPropertyChanged;
            }
        }

        private WindowState _previousState = WindowState.Normal;
        private WindowStyle _previousStyle = WindowStyle.SingleBorderWindow;
        private ResizeMode _previousResizeMode = ResizeMode.CanResize;

        private void OnViewModelPropertyChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
        {
            if (e.PropertyName == nameof(MainViewModel.IsKioskMode))
            {
                var vm = (MainViewModel)sender!;
                System.Diagnostics.Debug.WriteLine($"[MainWindow] IsKioskMode changed to: {vm.IsKioskMode}");
                
                if (vm.IsKioskMode)
                {
                    // 1. Capture State before transition
                    _previousState = this.WindowState;
                    _previousStyle = this.WindowStyle;
                    _previousResizeMode = this.ResizeMode;

                    // 2. Win32 Seamless Transition (No hiding, no peeking)
                    // We avoid Visibility.Collapsed because it shows the background apps.
                    // Instead, we use native Win32 style changes to "snap" to full screen.
                    
                    var hwnd = new System.Windows.Interop.WindowInteropHelper(this).Handle;
                    int style = GetWindowLong(hwnd, GWL_STYLE);
                    
                    // Remove TitleBar and Borders
                    SetWindowLong(hwnd, GWL_STYLE, style & ~(WS_CAPTION | WS_THICKFRAME));
                    
                    this.Topmost = true;
                    this.WindowState = WindowState.Maximized;
                    this.Activate();

                    System.Diagnostics.Debug.WriteLine("[MainWindow] FULLSCREEN ENGAGED (Seamless)");
                }
                else
                {
                    // 3. Restore State Seamlessly
                    var hwnd = new System.Windows.Interop.WindowInteropHelper(this).Handle;
                    int style = GetWindowLong(hwnd, GWL_STYLE);

                    // Restore TitleBar and Borders
                    SetWindowLong(hwnd, GWL_STYLE, style | WS_CAPTION | WS_THICKFRAME);

                    this.Topmost = false;
                    this.WindowState = _previousState;
                    this.Activate();

                    System.Diagnostics.Debug.WriteLine($"[MainWindow] FULLSCREEN EXIT. Restored to {_previousState}");
                }
            }
        }

        [DllImport("user32.dll")]
        private static extern int GetWindowLong(IntPtr hWnd, int nIndex);
        
        [DllImport("user32.dll")]
        private static extern int SetWindowLong(IntPtr hWnd, int nIndex, int dwNewLong);

        private const int GWL_STYLE = -16;
        private const int WS_CAPTION = 0x00C00000;
        private const int WS_THICKFRAME = 0x00040000;
        private const int WS_MAXIMIZEBOX = 0x00010000;
        private const int WS_MINIMIZEBOX = 0x00020000;

        private void Window_Loaded(object sender, RoutedEventArgs e)
        {
            var config = App.Current.Services.GetRequiredService<IConfigService>().Settings;
            this.Top = config.WindowTop;
            this.Left = config.WindowLeft;
            this.Width = config.WindowWidth;
            this.Height = config.WindowHeight;
            if (config.IsMaximized) this.WindowState = WindowState.Maximized;
            
            // Safety check for off-screen windows
            if (this.Left > SystemParameters.VirtualScreenWidth || this.Top > SystemParameters.VirtualScreenHeight)
            {
                this.Left = 100;
                this.Top = 100;
            }
        }

        private void Window_Closing(object sender, System.ComponentModel.CancelEventArgs e)
        {
            var configService = App.Current.Services.GetRequiredService<IConfigService>();
            var config = configService.Settings;
            
            if (this.WindowState == WindowState.Normal) {
                config.WindowTop = this.Top; 
                config.WindowLeft = this.Left;
                config.WindowWidth = this.Width; 
                config.WindowHeight = this.Height;
            }
            config.IsMaximized = (this.WindowState == WindowState.Maximized);
            configService.Save();
            
            if (DataContext is MainViewModel vm) 
            {
                vm.OnWindowClosing();
            }
        }
    }
}
