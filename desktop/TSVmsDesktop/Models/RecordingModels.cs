using System;
using System.Text.Json.Serialization;

namespace TSVmsDesktop.Models
{
    public class RecordingSegment
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("camera_id")]
        public string CameraId { get; set; } = string.Empty;

        [JsonPropertyName("start_ts")]
        public DateTime StartTs { get; set; }

        [JsonPropertyName("end_ts")]
        public DateTime EndTs { get; set; }

        [JsonPropertyName("duration_ms")]
        public long DurationMs { get; set; }

        [JsonPropertyName("path")]
        public string Path { get; set; } = string.Empty;

        [JsonPropertyName("size_bytes")]
        public long SizeBytes { get; set; }

        [JsonPropertyName("is_protected")]
        public bool IsProtected { get; set; }

        [JsonIgnore]
        public string FileName => System.IO.Path.GetFileName(Path);

        [JsonIgnore]
        public double DurationSeconds => DurationMs > 0 ? DurationMs / 1000.0 : Math.Max(0, (EndTs - StartTs).TotalSeconds);

        [JsonIgnore]
        public string DurationText => TimeSpan.FromSeconds(DurationSeconds).ToString(@"hh\:mm\:ss");

        [JsonIgnore]
        public string SizeText => SizeBytes <= 0 ? "0 B" : $"{SizeBytes / 1024d / 1024d:0.##} MB";
    }

    public class RecordingSegmentsEnvelope
    {
        [JsonPropertyName("segments")]
        public RecordingSegment[] Segments { get; set; } = Array.Empty<RecordingSegment>();
    }

    public class RecordingSchedule
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("camera_id")]
        public string CameraId { get; set; } = string.Empty;

        [JsonPropertyName("is_active")]
        public bool IsActive { get; set; }

        [JsonPropertyName("retention_days")]
        public int RetentionDays { get; set; }
    }

}
