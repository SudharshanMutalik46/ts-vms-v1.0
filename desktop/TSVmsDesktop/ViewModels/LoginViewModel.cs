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
        [ObservableProperty] private string _displayName = string.Empty;
        [ObservableProperty] private string _confirmPassword = string.Empty;
        [ObservableProperty] private string _errorMessage = string.Empty;
        [ObservableProperty] private bool _rememberMe = false;
        [ObservableProperty] private bool _isSignupMode = false;
        
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
        public void ToggleSignup()
        {
            IsSignupMode = !IsSignupMode;
            ErrorMessage = "";

            if (!IsSignupMode)
            {
                DisplayName = string.Empty;
                ConfirmPassword = string.Empty;
            }
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
        public async Task Signup()
        {
            var email = Username.Trim();
            var displayName = DisplayName.Trim();
            var password = Password.Trim();
            var confirmPassword = ConfirmPassword.Trim();

            if (string.IsNullOrWhiteSpace(email) || string.IsNullOrWhiteSpace(password))
            {
                ErrorMessage = "Email and password are required.";
                return;
            }
            if (password.Length < 8)
            {
                ErrorMessage = "Password must be at least 8 characters.";
                return;
            }
            if (!string.Equals(password, confirmPassword, StringComparison.Ordinal))
            {
                ErrorMessage = "Passwords do not match.";
                return;
            }

            ErrorMessage = "Creating account...";

            try
            {
                var registerReq = new TSVmsDesktop.Models.RegisterRequest
                {
                    email = email,
                    password = password,
                    display_name = displayName,
                    tenant_id = _sessionService.TenantId
                };

                var response = await _apiClient.PostAsync<TSVmsDesktop.Models.RegisterRequest, TSVmsDesktop.Models.RegisterResponse>(
                    "/api/v1/auth/register",
                    registerReq);

                if (response != null && !string.IsNullOrWhiteSpace(response.id))
                {
                    IsSignupMode = false;
                    ErrorMessage = "Signup successful. Signing you in...";
                    await Login();
                    return;
                }

                ErrorMessage = "Signup failed. Try again.";
            }
            catch (Exception ex)
            {
                var message = ex.Message ?? string.Empty;
                if (message.Contains("Conflict", StringComparison.OrdinalIgnoreCase) ||
                    message.Contains("Duplicate entry", StringComparison.OrdinalIgnoreCase) ||
                    message.Contains("email_exists", StringComparison.OrdinalIgnoreCase))
                {
                    ErrorMessage = "This email is already registered.";
                    return;
                }
                ErrorMessage = "Signup failed. Check server connection and tenant settings.";
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
