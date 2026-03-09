using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.Services;
using System;
using System.Windows;
using System.Threading.Tasks;

namespace TSVmsDesktop.ViewModels
{
    public partial class MainViewModel : ObservableObject
    {
        private readonly IServiceProvider _serviceProvider;
        private readonly IConfigService _configService;
        private readonly ISecureStorageService _secureStorageService;
        private readonly ISessionService _session;

        // Exposed for persistent binding in MainWindow
        public LiveViewModel LiveVM { get; }

        [ObservableProperty] private object? _currentView;
        [ObservableProperty] private bool _isLoggedIn = false;
        [ObservableProperty] private string _windowTitle = "TS-VMS Enterprise v1.0";
        [ObservableProperty] private bool _isKioskMode = false;
        // Default to "Startup" so LiveView is hidden initially
        [ObservableProperty] private string _currentPage = "Startup";

        // RBAC Properties
        public bool CanViewAudit 
        {
            get 
            {
                bool allowed = _session.HasPermission("audit.read");
                return allowed;
            }
        }
        // Allow all users to access User Management (Backend filters list to "Self" if no permission)
        public bool CanViewUsers => _session.IsLoggedIn;
        public bool CanViewLicense => _session.HasPermission("license.read");

        public MainViewModel(IServiceProvider serviceProvider, IConfigService configService, ISecureStorageService secureStorageService, ISessionService session, LiveViewModel liveVm)
        {
            _serviceProvider = serviceProvider;
            _configService = configService;
            _secureStorageService = secureStorageService;
            _session = session;
            LiveVM = liveVm; // Injected Singleton
            
            _configService.Load();
            
            // FIX: Resolve StartupViewModel and assign the callback manually
            var startupVm = _serviceProvider.GetRequiredService<StartupViewModel>();
            startupVm.OnStartupSuccess = this.OnStartupComplete;

            CurrentView = startupVm;
        }

        public void OnStartupComplete()
        {
            CheckForSavedSession();
        }

        public async void CheckForSavedSession()
        {
            try
            {
                if (string.IsNullOrEmpty(_session.AccessToken)) 
                {
                    NavigateToLogin();
                    return;
                }
                
                // Get ApiClient from scope
                var apiClient = _serviceProvider.GetRequiredService<Services.ApiClient>();
                
                // Fetch Identity from Backend
                var identity = await apiClient.GetAsync<TSVmsDesktop.Models.UserIdentity>("/api/v1/debug/me");
                
                if (identity != null)
                {
                    _session.SetIdentity(identity);
                    
                    System.Windows.Application.Current.Dispatcher.Invoke(() => 
                    {
                        IsLoggedIn = true;
                        RefreshRbacUI();
                        NavigateToLive();
                    });
                }
                else
                {
                    _ = NavigateToLogout();
                }
            }
            catch
            {
                await System.Windows.Application.Current.Dispatcher.InvokeAsync(async () => await NavigateToLogout());
            }
        }

        public void RefreshRbacUI()
        {
            OnPropertyChanged(nameof(CanViewAudit));
            OnPropertyChanged(nameof(CanViewUsers));
            OnPropertyChanged(nameof(CanViewLicense));
        }

        private void DeactivateLiveIfNeeded()
        {
            if (CurrentPage == "Live")
            {
                LiveVM.Deactivate();
            }
        }

        private void DeactivatePlaybackIfNeeded()
        {
            if (CurrentPage == "Playback")
            {
                var playbackVm = _serviceProvider.GetRequiredService<PlaybackViewModel>();
                playbackVm.Deactivate();
            }
        }

        private void DeactivateAllEngines()
        {
            DeactivateLiveIfNeeded();
            DeactivatePlaybackIfNeeded();
        }

        // --- NAVIGATION COMMANDS ---
        
        [RelayCommand]
        public void NavigateToLogin()
        {
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<LoginViewModel>();
            CurrentPage = "Login";
        }

        [RelayCommand]
        public void ToggleKioskMode() => IsKioskMode = !IsKioskMode;

        [RelayCommand]
        public void NavigateToLive()
        {
            if (!IsLoggedIn) return;

            DeactivatePlaybackIfNeeded();
            CurrentView = LiveVM;
            CurrentPage = "Live";
            _ = LiveVM.ActivateAsync();
        }

        [RelayCommand]
        public void NavigateToCameras()
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<CamerasViewModel>();
            CurrentPage = "Cameras";
        }

        [RelayCommand]
        public void NavigateToHealth()
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<HealthViewModel>();
            CurrentPage = "Health";
        }

        [RelayCommand]
        public void NavigateToSettings()
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<SettingsViewModel>();
            CurrentPage = "Settings";
        }

        [RelayCommand]
        public void NavigateToAudit()
        {
            if (!CanViewAudit) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<AuditViewModel>();
            CurrentPage = "Audit Log";
        }

        [RelayCommand]
        public void NavigateToLicense()
        {
            if (!CanViewLicense) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<LicenseViewModel>();
            CurrentPage = "License";
        }

        [RelayCommand]
        public void NavigateToUsers()
        {
            if (!CanViewUsers) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<UsersViewModel>();
            CurrentPage = "Users";
        }

        [RelayCommand]
        public void NavigateToSupervisor()
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<SupervisorViewModel>();
            CurrentPage = "Supervisor";
        }

        [RelayCommand]
        public void NavigateToCameraDetails(string cameraId)
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            var detailsVm = _serviceProvider.GetRequiredService<CameraDetailsViewModel>();
            detailsVm.Load(cameraId);
            CurrentView = detailsVm;
            CurrentPage = "Camera Details";
        }

        [RelayCommand]
        public void NavigateToDiscovery()
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<OnvifDiscoveryViewModel>();
            CurrentPage = "ONVIF Discovery";
        }

        [RelayCommand]
        public void NavigateToPlayback()
        {
            if (!IsLoggedIn) return;
            DeactivateLiveIfNeeded();
            var vm = _serviceProvider.GetRequiredService<PlaybackViewModel>();
            CurrentView = vm;
            CurrentPage = "Playback";
        }

        [RelayCommand]
        public void NavigateToNvrs()
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<NvrsViewModel>();
            CurrentPage = "NVRs";
        }

        [RelayCommand]
        public void NavigateToNvrDetails(string id)
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            var vm = _serviceProvider.GetRequiredService<NvrDetailsViewModel>();
            vm.Load(id);
            CurrentView = vm;
            CurrentPage = "NVR Details";
        }

        [RelayCommand]
        public void NavigateToWinDiscovery()
        {
            if (!IsLoggedIn) return;
            DeactivateAllEngines();
            CurrentView = _serviceProvider.GetRequiredService<WindowsDiscoveryViewModel>();
            CurrentPage = "Win Discovery";
        }

        [RelayCommand]
        public async Task NavigateToLogout()
        {
            DeactivateAllEngines();
            try
            {
                var apiClient = _serviceProvider.GetRequiredService<Services.ApiClient>();
                await _session.LogoutAsync(apiClient);
            }
            finally
            {
                IsLoggedIn = false;
                CurrentView = _serviceProvider.GetRequiredService<LoginViewModel>();
                CurrentPage = "Login";
            }
        }

        [RelayCommand]
        public void OnWindowClosing() => _configService.Save();
    }
}
