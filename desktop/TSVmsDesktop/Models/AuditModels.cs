using System;
using System.Text.Json.Serialization;

namespace TSVmsDesktop.Models
{
    public class AuditEvent
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("timestamp")]
        public DateTime Timestamp { get; set; }

        [JsonPropertyName("actor")]
        public string Actor { get; set; } = string.Empty;

        [JsonPropertyName("action")]
        public string Action { get; set; } = string.Empty;

        [JsonPropertyName("resource")]
        public string Resource { get; set; } = string.Empty;

        [JsonPropertyName("result")]
        public string Result { get; set; } = string.Empty; // "success", "failure"

        [JsonPropertyName("details")]
        public string Details { get; set; } = string.Empty;

        [JsonPropertyName("client_ip")]
        public string ClientIp { get; set; } = string.Empty;
    }

    public class AuditExportRequest
    {
        [JsonPropertyName("format")]
        public string Format { get; set; } = "csv"; // csv or json
        
        [JsonPropertyName("start_time")]
        public DateTime? StartTime { get; set; }
        
        [JsonPropertyName("end_time")]
        public DateTime? EndTime { get; set; }
    }
}
