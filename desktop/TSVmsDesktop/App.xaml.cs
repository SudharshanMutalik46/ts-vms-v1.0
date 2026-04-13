using Microsoft.Extensions.DependencyInjection;
using System;
using System.IO;
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

        private static string? ResolveBundledGStreamerBin()
        {
            string baseDir = AppDomain.CurrentDomain.BaseDirectory;
            string? explicitRoot = Environment.GetEnvironmentVariable("TS_VMS_GSTREAMER_ROOT");
            if (!string.IsNullOrWhiteSpace(explicitRoot))
            {
                string explicitBin = Path.Combine(explicitRoot, "bin");
                if (Directory.Exists(explicitBin))
                    return explicitBin;
                if (Directory.Exists(explicitRoot))
                    return explicitRoot;
            }

            string[] candidates =
            {
                Path.Combine(baseDir, "gstreamer", "bin"),
                Path.GetFullPath(Path.Combine(baseDir, "..", "..", "tools", "gstreamer", "bin")),
            };

            foreach (string candidate in candidates)
            {
                if (Directory.Exists(candidate))
                    return candidate;
            }

            string? gstRoot = Environment.GetEnvironmentVariable("GSTREAMER_1_0_ROOT_X86_64");
            if (!string.IsNullOrWhiteSpace(gstRoot))
            {
                string candidate = Path.Combine(gstRoot, "bin");
                if (Directory.Exists(candidate))
                    return candidate;
            }

            return null;
        }

        public App()
        {
            VideoService.Log("[BOOTSTRAP] App Constructor Started");
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
            services.AddSingleton<Services.LiveSessionService>();
            services.AddSingleton<Services.PlaybackTimelineBuilder>();
            services.AddSingleton<Services.GapResolver>();
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
            VideoService.Log("[BOOTSTRAP] Services Built");
        }

        protected override void OnStartup(StartupEventArgs e)
        {
            VideoService.Log("DEBUG: OnStartup Started");
            // Force GStreamer to ignore experimental D3D12 decoders and fallback to stable D3D11
            Environment.SetEnvironmentVariable("GST_PLUGIN_FEATURE_RANK", "d3d12h264dec:NONE,d3d12h265dec:NONE,d3d12convert:NONE,d3d12videosink:NONE");
            // Ultra-verbose for soup and adaptivedemux to find the root cause of the "no fragments" stall.
            Environment.SetEnvironmentVariable("GST_DEBUG", "*:1,d3d11debuglayer:0,video-info:0");

            // Add native DLL directories to PATH for PlaybackEngine and GStreamer dependencies
            var currentPath = Environment.GetEnvironmentVariable("PATH") ?? "";
            var gstPath = ResolveBundledGStreamerBin();
            var localNativePath = System.IO.Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "native", "win-x64");
            
            if (!string.IsNullOrWhiteSpace(gstPath) && !currentPath.Contains(gstPath, StringComparison.OrdinalIgnoreCase))
                currentPath = gstPath + ";" + currentPath;
            if (!currentPath.Contains(localNativePath, StringComparison.OrdinalIgnoreCase))
                currentPath = localNativePath + ";" + currentPath;
                
            Environment.SetEnvironmentVariable("PATH", currentPath);

            base.OnStartup(e);

            try
            {
                VideoService.Log("[BOOTSTRAP] Resolving MainViewModel...");
                var mainVm = Services.GetRequiredService<MainViewModel>();
                // mainVm.CheckForSavedSession(); // logic moved to MainViewModel constructor / StartupViewModel
                
                VideoService.Log("[BOOTSTRAP] Resolving MainWindow...");
                var mainWindow = Services.GetRequiredService<MainWindow>();
                
                VideoService.Log("[BOOTSTRAP] Showing MainWindow...");
                mainWindow.Show();
                VideoService.Log("[BOOTSTRAP] Application Ready (MainWindow Shown)");
            }
            catch (Exception ex)
            {
                VideoService.Log($"FATAL ERROR: {ex.Message}");
                VideoService.Log(ex.StackTrace ?? "No stack trace available.");
                if(ex.InnerException != null) 
                {
                    VideoService.Log($"INNER ERROR: {ex.InnerException.Message}");
                    VideoService.Log(ex.InnerException.StackTrace ?? "No inner stack trace available.");
                }
            }
        }
    }
}
