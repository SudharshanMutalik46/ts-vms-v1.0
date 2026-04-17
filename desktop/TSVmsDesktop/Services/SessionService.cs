using System.Collections.Generic;
using System.Linq;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public interface ISessionService
    {
        string? AccessToken { get; }
        string? RefreshToken { get; }
        UserIdentity? CurrentUser { get; } // New
        event Action? SessionCleared;
        void SetTokens(string access, string refresh);
        void SetIdentity(UserIdentity user);
        bool HasPermission(string permission);
        Task LogoutAsync(ApiClient apiClient); // New
        void Clear();
        string TenantId { get; }
        bool IsLoggedIn { get; }
    }

    public class SessionService : ISessionService
    {
        private readonly ISecureStorageService _storage;
        private string? _accessToken;
        private string? _refreshToken;
        public event Action? SessionCleared;
        public string? AccessToken => _accessToken;
        public string? RefreshToken => _refreshToken;
        public UserIdentity? CurrentUser { get; private set; }
        public string TenantId => CurrentUser?.TenantId ?? "00000000-0000-0000-0000-000000000001";
        public bool IsLoggedIn => CurrentUser != null && !string.IsNullOrEmpty(_accessToken);

        public void SetTokens(string access, string refresh)
        {
            _accessToken = access;
            _refreshToken = refresh;
            _storage.SetToken($"{access}|{refresh}");
        }

        public void SetIdentity(UserIdentity user)
        {
            CurrentUser = user;
        }

        public bool HasPermission(string permission)
        {
            if (CurrentUser == null) return false;
            // Short-circuit for Admin and Operator roles (Case-Insensitive)
            if (CurrentUser.Roles != null && CurrentUser.Roles.Any(r => 
                r.Equals("admin", StringComparison.OrdinalIgnoreCase) || 
                r.Equals("operator", StringComparison.OrdinalIgnoreCase))) 
            {
                return true;
            }
            return CurrentUser.Permissions != null && CurrentUser.Permissions.Contains(permission);
        }
        
        // ... (existing properties)

        public SessionService(ISecureStorageService storage)
        {
            _storage = storage;
            Load();
        }

        // ... (existing methods)

        public async Task LogoutAsync(ApiClient apiClient)
        {
            if (!string.IsNullOrEmpty(_accessToken))
            {
                try 
                {
                    VideoService.Log("[Session] LogoutAsync: notifying backend...");
                    await apiClient.PostAsync<object>("/api/v1/auth/logout", new { });
                }
                catch (Exception ex)
                {
                    VideoService.Log($"[Session] LogoutAsync warning: {ex.Message}");
                }
            }
            Clear();
        }

        public void Clear()
        {
            VideoService.Log("[Session] Clearing session.");
            _accessToken = null;
            _refreshToken = null;
            CurrentUser = null;
            _storage.ClearToken();
            SessionCleared?.Invoke();
        }

        private void Load()
        {
            var raw = _storage.GetToken();
            if (!string.IsNullOrEmpty(raw))
            {
                var parts = raw.Split('|');
                _accessToken = parts[0];
                if (parts.Length > 1) _refreshToken = parts[1];
            }
        }
    }
}
