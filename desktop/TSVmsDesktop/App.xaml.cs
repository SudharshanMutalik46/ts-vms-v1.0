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
            services.AddSingleton<ISecureStorageService, SecureStorageService>();
            services.AddSingleton<Services.SettingsService>();
            services.AddSingleton<Services.SessionService>();
            services.AddSingleton<Services.ISessionService>(p => p.GetRequiredService<Services.SessionService>());
            services.AddSingleton<Services.ApiClient>();
            services.AddSingleton<Services.VideoService>();
            services.AddSingleton<Services.CameraService>();
            services.AddSingleton<Services.OnvifService>();
            services.AddSingleton<Services.MediaService>();
            services.AddSingleton<Services.CredentialService>();
            services.AddSingleton<Services.AlertsService>();
            services.AddHttpClient<IHealthService, HealthService>();
            services.AddSingleton<Services.AuditService>(); // New
            services.AddSingleton<Services.LicenseService>();
            services.AddSingleton<Services.UserService>();
            services.AddSingleton<Services.SupervisorService>();
            services.AddSingleton<Services.NvrService>();
            services.AddSingleton<Services.WindowsService>();
            services.AddSingleton<Services.SiteService>();

            services.AddSingleton<MainViewModel>();
            services.AddTransient<StartupViewModel>(); // New
            services.AddTransient<LoginViewModel>();
            services.AddSingleton<LiveViewModel>(); // Changed to Singleton to persist state/video
            services.AddTransient<CamerasViewModel>();
            services.AddTransient<CameraDetailsViewModel>();
            services.AddTransient<OnvifDiscoveryViewModel>();
            services.AddTransient<AuditViewModel>(); // New
            services.AddTransient<HealthViewModel>();
            services.AddTransient<SystemHealthViewModel>();
            services.AddTransient<SettingsViewModel>();
            services.AddTransient<MainWindow>();
            services.AddTransient<LicenseViewModel>();
            services.AddTransient<UsersViewModel>();
            services.AddTransient<SupervisorViewModel>();
            services.AddTransient<NvrsViewModel>();
            services.AddTransient<NvrDetailsViewModel>();
            services.AddTransient<WindowsDiscoveryViewModel>();

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
                // mainVm.CheckForSavedSession(); // logic moved to MainViewModel constructor / StartupViewModel
                
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
