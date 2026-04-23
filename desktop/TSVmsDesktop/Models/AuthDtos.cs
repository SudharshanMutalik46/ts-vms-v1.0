namespace TSVmsDesktop.Models
{
    public class LoginRequest
    {
        public string email { get; set; } = string.Empty;
        public string password { get; set; } = string.Empty;
        public string tenant_id { get; set; } = string.Empty;
    }

    public class LoginResponse
    {
        public string access_token { get; set; } = string.Empty;
        public string refresh_token { get; set; } = string.Empty;
        public int expires_in { get; set; }
    }

    public class RefreshRequest
    {
        public string refresh_token { get; set; } = string.Empty;
    }

    public class RegisterRequest
    {
        public string email { get; set; } = string.Empty;
        public string password { get; set; } = string.Empty;
        public string display_name { get; set; } = string.Empty;
        public string tenant_id { get; set; } = string.Empty;
    }

    public class RegisterResponse
    {
        public string id { get; set; } = string.Empty;
    }
}
