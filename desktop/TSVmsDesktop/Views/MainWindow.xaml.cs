using System.Windows;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.Services;
using TSVmsDesktop.ViewModels;

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

        private void OnViewModelPropertyChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
        {
            if (e.PropertyName == nameof(MainViewModel.IsKioskMode))
            {
                var vm = (MainViewModel)sender!;
                if (vm.IsKioskMode)
                {
                    this.WindowStyle = WindowStyle.SingleBorderWindow; // Keep Title Bar
                    this.WindowState = WindowState.Maximized;
                    this.ResizeMode = ResizeMode.CanResize;

                    this.Topmost = false; // Optional, usually true for real kiosk but let's be nice
                }
                else
                {
                    this.WindowStyle = WindowStyle.SingleBorderWindow;
                    this.WindowState = WindowState.Normal;
                    this.ResizeMode = ResizeMode.CanResize;
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
