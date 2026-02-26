using System;
using System.Net.Http;
using System.Net.Http.Json;
using System.Threading.Tasks;
using System.Collections.Generic;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class UserService
    {
        private readonly ApiClient _api;

        public UserService(ApiClient api)
        {
            _api = api;
        }

        public async Task<List<UserDto>> GetUsersAsync()
        {
            var response = await _api.GetAsync<UserListResponse>("/api/v1/users");
            return response?.Data ?? new List<UserDto>();
        }

        public async Task<UserDto?> GetUserAsync(string userId)
        {
            return await _api.GetAsync<UserDto>($"/api/v1/users/{userId}");
        }

        public async Task<string?> CreateUserAsync(CreateUserRequest request)
        {
            try {
                // Return type of API is CreateUserResponse which has Id
                var response = await _api.PostAsync<CreateUserRequest, CreateUserResponse>("/api/v1/users", request);
                return response?.Id;
            } catch { return null; }
        }

        public async Task<bool> DisableUserAsync(string userId)
        {
            return await _api.PostAsync($"/api/v1/users/{userId}/disable", new { });
        }

        public async Task<bool> EnableUserAsync(string userId)
        {
            return await _api.PostAsync($"/api/v1/users/{userId}/enable", new { });
        }

        public async Task<string?> ResetPasswordAsync(string userId)
        {
            // Assuming the backend returns the temp password in the body or standard 200 OK
            try 
            {
                var result = await _api.PostAsync<object, ResetPasswordResponse>($"/api/v1/users/{userId}/reset-password", new { });
                return result?.TemporaryPassword;
            }
            catch 
            {
                return null;
            }
        }

        public async Task<bool> AssignRoleAsync(string userId, string role, string tenantId)
        {
            var body = new AssignRoleRequest 
            { 
                ScopeType = "tenant",
                ScopeId = tenantId,
                Role = role
            };
            try 
            {
                return await _api.PutAsync($"/api/v1/users/{userId}/roles", body);
            }
            catch { return false; }
        }

        // New: Verify Login (Self-Test)
        public async Task<bool> VerifyLoginAsync(string email, string password, string tenantId, string baseUrl)
        {
            try
            {
                // Use a FRESH HttpClient to avoid using the Admin's existing session/headers
                using var client = new HttpClient();
                client.BaseAddress = new Uri(baseUrl);
                client.Timeout = TimeSpan.FromSeconds(5);

                var loginReq = new LoginRequest { email = email, password = password, tenant_id = tenantId };
                var response = await client.PostAsJsonAsync("/api/v1/auth/login", loginReq);
                
                return response.IsSuccessStatusCode;
            }
            catch
            {
                return false;
            }
        }

        public async Task<bool> DeleteUserAsync(string userId)
        {
            try 
            {
                return await _api.DeleteAsync($"/api/v1/users/{userId}");
            }
            catch { return false; }
        }

        public async Task<bool> UpdateUserAsync(string userId, string displayName)
        {
            try
            {
                var body = new UpdateUserRequest { DisplayName = displayName };
                // PUT /api/v1/users/{id}
                return await _api.PutAsync($"/api/v1/users/{userId}", body);
            }
            catch 
            { 
                return false; 
            }
        }

        public async Task<bool> ChangePasswordAsync(string oldPassword, string newPassword)
        {
            try
            {
                var body = new { old_password = oldPassword, new_password = newPassword };
                return await _api.PostAsync("/api/v1/auth/change-password", body);
            }
            catch
            {
                return false;
            }
        }

        public async Task<bool> CompletePasswordResetAsync(string token, string newPassword)
        {
            try
            {
                var body = new { token, new_password = newPassword };
                return await _api.PostAsync("/api/v1/auth/complete-reset", body);
            }
            catch
            {
                return false;
            }
        }

        public async Task<bool> SetPasswordAsync(string userId, string newPassword)
        {
            try
            {
                var body = new { new_password = newPassword };
                return await _api.PostAsync($"/api/v1/users/{userId}/password", body);
            }
            catch
            {
                return false;
            }
        }
    }

    public class UserListResponse
    {
        public List<UserDto> Data { get; set; } = new();
    }
}
