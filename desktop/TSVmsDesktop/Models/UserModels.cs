using System;
using System.Collections.Generic;
using System.Text.Json.Serialization;

namespace TSVmsDesktop.Models
{
    public class UserDto
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = "";

        [JsonPropertyName("email")]
        public string Email { get; set; } = "";

        [JsonPropertyName("display_name")]
        public string DisplayName { get; set; } = "";

        [JsonPropertyName("username")]
        public string Username { get; set; } = ""; // Keep for compatibility if needed, but UI should prefer DisplayName

        [JsonPropertyName("is_disabled")]
        public bool IsDisabled { get; set; }

        [JsonPropertyName("roles")]
        public List<string> Roles { get; set; } = new();
        
        [JsonPropertyName("created_at")]
        public DateTime CreatedAt { get; set; }

        // UI Helpers
        [JsonIgnore]
        public string Initials 
        {
            get 
            {
                if (!string.IsNullOrEmpty(DisplayName)) return DisplayName.Substring(0, 1).ToUpper();
                if (!string.IsNullOrEmpty(Username)) return Username.Substring(0, 1).ToUpper(); // Fallback
                return "?";
            }
        }

        [JsonIgnore]
        public string StatusColor => IsDisabled ? "#FFCDD2" : "#C8E6C9"; // Red-100 vs Green-100

        [JsonIgnore]
        public string StatusText => IsDisabled ? "Disabled" : "Active";
    }

    public class CreateUserRequest
    {
        [JsonPropertyName("email")]
        public string Email { get; set; } = "";

        [JsonPropertyName("display_name")]
        public string DisplayName { get; set; } = "";

        [JsonPropertyName("password")]
        public string Password { get; set; } = "";

        [JsonPropertyName("tenant_id")]
        public string TenantId { get; set; } = "";
    }

    public class UpdateUserRequest
    {
        [JsonPropertyName("display_name")]
        public string DisplayName { get; set; } = "";
    }

    public class CreateUserResponse
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = "";
    }

    public class ResetPasswordResponse
    {
        [JsonPropertyName("temporary_password")]
        public string TemporaryPassword { get; set; } = "";
    }
    
    public class AssignRoleRequest
    {
        [JsonPropertyName("scope_type")]
        public string ScopeType { get; set; } = "tenant"; // tenant or site

        [JsonPropertyName("scope_id")]
        public string ScopeId { get; set; } = "";
        
        [JsonPropertyName("role")]
        public string Role { get; set; } = ""; // e.g. "admin", "viewer"
    }
}
