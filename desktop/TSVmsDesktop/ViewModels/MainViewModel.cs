using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.Services;
using System; // Added for IServiceProvider
using System.Windows;

namespace TSVmsDesktop.ViewModels
{
    public partial class MainViewModel : ObservableObject
    {
        private readonly IServiceProvider _serviceProvider;
        private readonly IConfigService _configService;
        private readonly ISecureStorageService _secureStorageService;
        private readonly ISessionService _session;

        [ObservableProperty] private object? _currentView;
        [ObservableProperty] private bool _isLoggedIn = false;
        [ObservableProperty] private string _windowTitle = "TS-VMS Enterprise v1.0";
        [ObservableProperty] private bool _isKioskMode = false;
        [ObservableProperty] private string _currentPage = "Live";

        // RBAC Properties
        public bool CanViewAudit 
        {
            get 
            {
                bool allowed = _session.HasPermission("audit.read");
                Console.WriteLine($"[RBAC-Check] CanViewAudit: {allowed}");
                return allowed;
            }
        }
        public bool CanViewUsers => _session.HasPermission("user.read");
        public bool CanViewLicense => _session.HasPermission("license.read");

        public MainViewModel(IServiceProvider serviceProvider, IConfigService configService, ISecureStorageService secureStorageService, ISessionService session)
        {
            _serviceProvider = serviceProvider;
            _configService = configService;
            _secureStorageService = secureStorageService;
            _session = session;
            
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
                    Console.WriteLine("[Auth] No saved token.");
                    NavigateToLogin();
                    return;
                }

                Console.WriteLine("[Auth] Saved token found. Restoring identity...");
                
                // Use a local variable to capture success/failure
                bool success = false;
                
                // Get ApiClient from scope
                var apiClient = _serviceProvider.GetRequiredService<Services.ApiClient>();
                
                // Fetch Identity from Backend
                var identity = await apiClient.GetAsync<TSVmsDesktop.Models.UserIdentity>("/api/v1/debug/me");
                
                if (identity != null)
                {
                    Console.WriteLine($"[Auth] Identity found: {identity.Username}. Roles: {string.Join(",", identity.Roles)}");
                    
                    // Update Session Service (Singleton)
                    _session.SetIdentity(identity);
                    
                    // SUCCESS
                    success = true;
                }
                else
                {
                    Console.WriteLine("[Auth] Identity response was NULL.");
                }

                // Finalize on UI Thread
                System.Windows.Application.Current.Dispatcher.Invoke(() => 
                {
                    if (success)
                    {
                        IsLoggedIn = true;
                        RefreshRbacUI(); // Critical: Trigger bindings
                        NavigateToLive();
                    }
                    else
                    {
                        Console.WriteLine("[Auth] Restoration failed. Logout.");
                        NavigateToLogout();
                    }
                });
            }
            catch (Exception ex)
            {
                Console.WriteLine($"[Auth] Critical Error in Restoration: {ex.Message}");
                // Fail safe
                System.Windows.Application.Current.Dispatcher.Invoke(() => NavigateToLogout());
            }
        }

        public void RefreshRbacUI()
        {
            OnPropertyChanged(nameof(CanViewAudit));
            OnPropertyChanged(nameof(CanViewUsers));
            OnPropertyChanged(nameof(CanViewLicense));
        }

        // --- NAVIGATION COMMANDS ---

        [RelayCommand] public void NavigateToLogin() => CurrentView = _serviceProvider.GetRequiredService<LoginViewModel>();

        [RelayCommand]
        public void ToggleKioskMode() => IsKioskMode = !IsKioskMode;

        [RelayCommand] 
        public void NavigateToLive() 
        { 
            if(IsLoggedIn) {
                CurrentView = _serviceProvider.GetRequiredService<LiveViewModel>();
                CurrentPage = "Live"; 
            }
        }

        [RelayCommand] 
        public void NavigateToCameras() 
        { 
            if(IsLoggedIn) {
                CurrentView = _serviceProvider.GetRequiredService<CamerasViewModel>();
                CurrentPage = "Cameras"; 
            }
        }

        [RelayCommand] 
        public void NavigateToHealth() 
        { 
            CurrentView = _serviceProvider.GetRequiredService<HealthViewModel>();
            CurrentPage = "Health"; 
        }

        [RelayCommand] 
        public void NavigateToSettings() 
        { 
            CurrentView = _serviceProvider.GetRequiredService<SettingsViewModel>();
            CurrentPage = "Settings"; 
        }

        [RelayCommand] 
        public void NavigateToAudit() 
        { 
            if(CanViewAudit) {
                CurrentView = _serviceProvider.GetRequiredService<AuditViewModel>();
                CurrentPage = "Audit"; 
            }
        }

        [RelayCommand]
        public async Task NavigateToLogout() 
        { 
            try
            {
                var apiClient = _serviceProvider.GetRequiredService<Services.ApiClient>();
                await _session.LogoutAsync(apiClient);
            }
            finally
            {
                IsLoggedIn = false; 
                NavigateToLogin(); 
            }
        }

        [RelayCommand]
        public void OnWindowClosing() => _configService.Save();
    }
}
