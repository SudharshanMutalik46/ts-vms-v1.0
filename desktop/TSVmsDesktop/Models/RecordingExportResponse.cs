using System.Text.Json.Serialization;

namespace TSVmsDesktop.Models
{
    public sealed class RecordingExportResponse
    {
        [JsonPropertyName("export_id")]
        public string JobId { get; set; } = "";

        [JsonPropertyName("state")]
        public string State { get; set; } = "";

        [JsonPropertyName("download_url")]
        public string? DownloadUrl { get; set; }
    }
}
