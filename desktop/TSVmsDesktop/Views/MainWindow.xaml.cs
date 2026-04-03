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
            
            // Render-Delay Fix: Prevents visual artifacts when restoring from minimized state
            this.StateChanged += MainWindow_StateChanged;

            DataContext = App.Current.Services.GetRequiredService<MainViewModel>();
            if (DataContext is MainViewModel vm)
            {
                vm.PropertyChanged += OnViewModelPropertyChanged;
            }
        }

        private async void MainWindow_StateChanged(object? sender, EventArgs e)
        {
            if (ViewLayoutContainer == null) return;

            if (this.WindowState == WindowState.Minimized)
            {
                // Hide video content immediately on minimize
                ViewLayoutContainer.Visibility = Visibility.Hidden;
            }
            else if (this.WindowState == WindowState.Normal || this.WindowState == WindowState.Maximized)
            {
                // NUCLEAR OPTION: Wait a full half-second (500ms) to guarantee 
                // the OS has finished its "restore" animation and WPF has 
                // repainted the entire 8-slot grid before we reveal video layers.
                await Task.Delay(500); 
                
                ViewLayoutContainer.Visibility = Visibility.Visible;
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

                    // 2. Switch to a true borderless fullscreen window.
                    // Maximized alone leaves the taskbar visible; using a borderless
                    // normal window sized to the screen gives a cleaner kiosk handoff.
                    this.WindowStyle = WindowStyle.None;
                    this.ResizeMode = ResizeMode.NoResize;
                    this.WindowState = WindowState.Normal;
                    this.Left = SystemParameters.VirtualScreenLeft;
                    this.Top = SystemParameters.VirtualScreenTop;
                    this.Width = SystemParameters.VirtualScreenWidth;
                    this.Height = SystemParameters.VirtualScreenHeight;
                    this.Topmost = true;
                    this.Activate();

                    System.Diagnostics.Debug.WriteLine("[MainWindow] FULLSCREEN ENGAGED (Seamless)");
                }
                else
                {
                    // 3. Restore State Seamlessly
                    this.Topmost = false;
                    this.WindowStyle = _previousStyle;
                    this.ResizeMode = _previousResizeMode;
                    this.WindowState = _previousState;
                    this.Activate();

                    System.Diagnostics.Debug.WriteLine($"[MainWindow] FULLSCREEN EXIT. Restored to {_previousState}");
                }
            }
        }

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
