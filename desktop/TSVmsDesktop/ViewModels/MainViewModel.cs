using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class MainViewModel : ObservableObject
    {
        private readonly IServiceProvider _serviceProvider;
        private readonly IConfigService _configService;
        private readonly ISecureStorageService _secureStorageService;

        [ObservableProperty] private object? _currentView;
        [ObservableProperty] private bool _isLoggedIn = false;
        [ObservableProperty] private string _windowTitle = "TS-VMS Enterprise v1.0";
        
        // This property controls the Sidebar Highlighting
        [ObservableProperty] private string _currentPage = "Live"; 

        public MainViewModel(IServiceProvider serviceProvider, IConfigService configService, ISecureStorageService secureStorageService)
        {
            _serviceProvider = serviceProvider;
            _configService = configService;
            _secureStorageService = secureStorageService;
            
            _configService.Load();
            // CheckForSavedSession(); // Moved to explicit call to avoid circular dependency
        }

        public void CheckForSavedSession()
        {
            string? token = _secureStorageService.GetToken();
            if (!string.IsNullOrEmpty(token)) {
                IsLoggedIn = true;
                NavigateToLive();
            } else {
                NavigateToLogin();
            }
        }

        [RelayCommand] public void NavigateToLogin() => CurrentView = _serviceProvider.GetRequiredService<LoginViewModel>();

        [RelayCommand] 
        public void NavigateToLive() 
        { 
            if(IsLoggedIn) {
                CurrentView = _serviceProvider.GetRequiredService<LiveViewModel>();
                CurrentPage = "Live"; // HIGHLIGHTS "Live View"
            }
        }

        [RelayCommand] 
        public void NavigateToCameras() 
        { 
            if(IsLoggedIn) {
                CurrentView = _serviceProvider.GetRequiredService<CamerasViewModel>();
                CurrentPage = "Cameras"; // HIGHLIGHTS "Cameras"
            }
        }

        [RelayCommand] 
        public void NavigateToHealth() 
        { 
            CurrentView = _serviceProvider.GetRequiredService<HealthViewModel>();
            CurrentPage = "Health"; // HIGHLIGHTS "System Health"
        }

        [RelayCommand] 
        public void NavigateToSettings() 
        { 
            CurrentView = _serviceProvider.GetRequiredService<SettingsViewModel>();
            CurrentPage = "Settings"; // HIGHLIGHTS "Settings"
        }

        [RelayCommand]
        public void NavigateToLogout() 
        { 
            _secureStorageService.ClearToken();
            IsLoggedIn = false; 
            NavigateToLogin(); 
        }

        [RelayCommand]
        public void OnWindowClosing() => _configService.Save();
    }
}
