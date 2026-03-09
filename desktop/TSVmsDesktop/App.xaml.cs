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
            System.Diagnostics.Debug.WriteLine("[BOOTSTRAP] App Constructor Started");
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
            services.AddSingleton<Services.PlaybackService>();
            services.AddSingleton<Services.RecordingService>();
            services.AddSingleton<Services.PlaybackManifestService>();
            services.AddSingleton<Services.PlaybackEngineService>();

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
            services.AddTransient<PlaybackViewModel>();

            Services = services.BuildServiceProvider();
            System.Diagnostics.Debug.WriteLine("[BOOTSTRAP] Services Built");
        }

        protected override void OnStartup(StartupEventArgs e)
        {
            Console.WriteLine("DEBUG: OnStartup Started");
            // Force GStreamer to ignore experimental D3D12 decoders and fallback to stable D3D11
            Environment.SetEnvironmentVariable("GST_PLUGIN_FEATURE_RANK", "d3d12h264dec:NONE,d3d12h265dec:NONE,d3d12convert:NONE,d3d12videosink:NONE");
            // Keep runtime logs minimal; d3d11debuglayer warning floods can stall the app.
            Environment.SetEnvironmentVariable("GST_DEBUG", "*:1,d3d11debuglayer:0,video-info:0");

            // Add native DLL directories to PATH for PlaybackEngine and GStreamer dependencies
            var currentPath = Environment.GetEnvironmentVariable("PATH") ?? "";
            var gstPath = @"C:\Program Files\gstreamer\1.0\msvc_x86_64\bin";
            var localNativePath = System.IO.Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "native", "win-x64");
            
            if (!currentPath.Contains(gstPath, StringComparison.OrdinalIgnoreCase))
                currentPath = gstPath + ";" + currentPath;
            if (!currentPath.Contains(localNativePath, StringComparison.OrdinalIgnoreCase))
                currentPath = localNativePath + ";" + currentPath;
                
            Environment.SetEnvironmentVariable("PATH", currentPath);

            base.OnStartup(e);

            try
            {
                System.Diagnostics.Debug.WriteLine("[BOOTSTRAP] Resolving MainViewModel...");
                var mainVm = Services.GetRequiredService<MainViewModel>();
                // mainVm.CheckForSavedSession(); // logic moved to MainViewModel constructor / StartupViewModel
                
                System.Diagnostics.Debug.WriteLine("[BOOTSTRAP] Resolving MainWindow...");
                var mainWindow = Services.GetRequiredService<MainWindow>();
                
                System.Diagnostics.Debug.WriteLine("[BOOTSTRAP] Showing MainWindow...");
                mainWindow.Show();
                Console.WriteLine("[BOOTSTRAP] Application Ready (MainWindow Shown)");
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
