using Microsoft.Extensions.DependencyInjection;
using System;
using System.Windows;
using Application = System.Windows.Application;
using TSVmsDesktop.Services;
using TSVmsDesktop.ViewModels;
using TSVmsDesktop.Views;

namespace TSVmsDesktop
{
    public partial class App : Application
    {
        public new static App Current => (App)Application.Current;
        public IServiceProvider Services { get; }

        public App()
        {
            Console.WriteLine("DEBUG: App Constructor Started");
            InitializeComponent(); // REQUIRED for App.xaml resources
            
            var services = new ServiceCollection();

            services.AddSingleton<IConfigService, ConfigService>();
            services.AddSingleton<Services.VideoService>();
            services.AddSingleton<Services.CameraService>();
            services.AddSingleton<ISecureStorageService, SecureStorageService>();
            services.AddSingleton<Services.SettingsService>();
            services.AddHttpClient<IHealthService, HealthService>();

            services.AddSingleton<MainViewModel>();
            services.AddTransient<LoginViewModel>();
            services.AddTransient<LiveViewModel>();
            services.AddTransient<CamerasViewModel>();
            services.AddTransient<HealthViewModel>();
            services.AddTransient<SystemHealthViewModel>();
            services.AddTransient<SettingsViewModel>();
            services.AddTransient<MainWindow>();

            Services = services.BuildServiceProvider();
            Console.WriteLine("DEBUG: Services Built");
        }

        protected override void OnStartup(StartupEventArgs e)
        {
            Console.WriteLine("DEBUG: OnStartup Started");
            base.OnStartup(e);

            try
            {
                Console.WriteLine("DEBUG: Resolving MainViewModel...");
                var mainVm = Services.GetRequiredService<MainViewModel>();
                mainVm.CheckForSavedSession(); // Perform initial navigation here
                
                Console.WriteLine("DEBUG: Resolving MainWindow...");
                var mainWindow = Services.GetRequiredService<MainWindow>();
                
                Console.WriteLine("DEBUG: Showing MainWindow...");
                mainWindow.Show();
                Console.WriteLine("DEBUG: MainWindow Shown");
            }
            catch (Exception ex)
            {
                Console.WriteLine($"FATAL ERROR: {ex.Message}");
                Console.WriteLine(ex.StackTrace);
                if(ex.InnerException != null) 
                {
                    Console.WriteLine($"INNER ERROR: {ex.InnerException.Message}");
                    Console.WriteLine(ex.InnerException.StackTrace);
                }
            }
        }
    }
}
