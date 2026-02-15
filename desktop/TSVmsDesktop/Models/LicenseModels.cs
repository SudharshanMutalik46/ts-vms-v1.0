using System;
using System.Collections.Generic;
using System.Text.Json.Serialization;

namespace TSVmsDesktop.Models
{
    public class LicenseStatus
    {
        [JsonPropertyName("valid")]
        public bool Valid { get; set; }

        [JsonPropertyName("status")]
        public string Status { get; set; } = "Unknown"; // Valid, Invalid, Expired

        [JsonPropertyName("expiry")]
        public DateTime Expiry { get; set; }

        [JsonPropertyName("features")]
        public List<string> Features { get; set; } = new();

        [JsonPropertyName("quotas")]
        public Dictionary<string, int> Quotas { get; set; } = new();

        [JsonPropertyName("usage")]
        public Dictionary<string, int> Usage { get; set; } = new();

        [JsonPropertyName("fingerprint")]
        public string Fingerprint { get; set; } = "";
    }
}
