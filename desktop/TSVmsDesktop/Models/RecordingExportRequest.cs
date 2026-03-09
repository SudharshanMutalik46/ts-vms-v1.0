using System;
using System.Text.Json.Serialization;

namespace TSVmsDesktop.Models
{
    public sealed class RecordingExportRequest
    {
        [JsonPropertyName("camera_id")]
        public string CameraId { get; set; } = "";

        [JsonPropertyName("start")]
        public DateTime FromTs { get; set; }

        [JsonPropertyName("end")]
        public DateTime ToTs { get; set; }

        [JsonPropertyName("format")]
        public string Format { get; set; } = "mp4";
    }
}
