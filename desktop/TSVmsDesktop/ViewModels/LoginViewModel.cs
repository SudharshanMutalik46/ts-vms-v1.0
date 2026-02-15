using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;

namespace TSVmsDesktop.ViewModels
{
    public partial class LoginViewModel : ObservableObject
    {
        private readonly MainViewModel _mainViewModel;
        private readonly Services.ApiClient _apiClient;
        private readonly Services.SessionService _sessionService;
        private readonly Services.SettingsService _settings;
        private readonly Services.ISecureStorageService _secure;

        [ObservableProperty] private string _username = string.Empty;
        [ObservableProperty] private string _password = string.Empty;
        [ObservableProperty] private string _errorMessage = string.Empty;
        [ObservableProperty] private bool _rememberMe = false;
        
        // NEW: Toggle Password Visibility
        [ObservableProperty] private bool _isPasswordVisible = false;
        [ObservableProperty] private bool _isLoading = false;

        public LoginViewModel(MainViewModel mainVm, Services.ApiClient api, Services.SessionService session, Services.SettingsService settings, Services.ISecureStorageService secure)
        {
            _mainViewModel = mainVm;
            _apiClient = api;
            _sessionService = session;
            _settings = settings;
            _secure = secure;

            LoadSavedCredentials();
        }

        private void LoadSavedCredentials()
        {
            // Load Username
            if (!string.IsNullOrEmpty(_settings.CurrentSettings.SavedUsername))
            {
                Username = _settings.CurrentSettings.SavedUsername;
                RememberMe = true;
            }

            // Load Password (Decrypt)
            if (!string.IsNullOrEmpty(_settings.CurrentSettings.SavedPasswordEncrypted))
            {
                try 
                {
                    Password = _secure.Decrypt(_settings.CurrentSettings.SavedPasswordEncrypted);
                }
                catch { /* Ignore decrypt errors */ }
            }
        }

        [RelayCommand]
        public void TogglePassword()
        {
            IsPasswordVisible = !IsPasswordVisible;
        }

        [RelayCommand]
        public async Task Login()
        {
            ErrorMessage = "Signing in...";

            try
            {
                var loginReq = new TSVmsDesktop.Models.LoginRequest
                {
                    email = Username,
                    password = Password,
                    tenant_id = _sessionService.TenantId
                };

                var response = await _apiClient.PostAsync<TSVmsDesktop.Models.LoginRequest, TSVmsDesktop.Models.LoginResponse>("/api/v1/auth/login", loginReq);
                
                if (response != null)
                {
                    _sessionService.SetTokens(response.access_token, response.refresh_token);

                    // Fetch Identity (Clean JSON Mode)
                    try
                    {
                        var identity = await _apiClient.GetAsync<TSVmsDesktop.Models.UserIdentity>("/api/v1/debug/me");
                        if (identity != null)
                        {
                            _sessionService.SetIdentity(identity);
                            
                            // FORCE UI REFRESH
                            if (_mainViewModel != null)
                            {
                                _mainViewModel.RefreshRbacUI();
                            }
                        }
                    }
                    catch (Exception ex)
                    {
                        System.Diagnostics.Debug.WriteLine($"Identity fetch failed: {ex.Message}");
                    }

                    if (RememberMe)
                    {
                        _settings.CurrentSettings.SavedUsername = Username;
                        _settings.CurrentSettings.SavedPasswordEncrypted = _secure.Encrypt(Password);
                    }
                    else
                    {
                        _settings.CurrentSettings.SavedUsername = "";
                        _settings.CurrentSettings.SavedPasswordEncrypted = "";
                    }
                    _settings.Save();

                    if (_mainViewModel != null)
                    {
                        _mainViewModel.IsLoggedIn = true;
                        _mainViewModel.NavigateToLive();
                    }
                    ErrorMessage = "";
                }
            }
            catch (Exception)
            {
                ErrorMessage = "Invalid email or password.";
            }
        }

        [RelayCommand]
        public void BypassLogin()
        {
            // FAKE SUCCESS
            _sessionService.SetTokens("fake-access-token", "fake-refresh-token");
            
            // Create a dummy admin identity
            var dummyUser = new TSVmsDesktop.Models.UserIdentity 
            { 
                Username = "admin", 
                Roles = new System.Collections.Generic.List<string> { "admin" },
                Permissions = new System.Collections.Generic.List<string> { "audit.read", "user.read" } 
            };
            _sessionService.SetIdentity(dummyUser);

            _mainViewModel.IsLoggedIn = true;
            _mainViewModel.NavigateToLive();
        }
    }
}
