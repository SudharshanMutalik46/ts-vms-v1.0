using CommunityToolkit.Mvvm.ComponentModel;
using System;
using System.Collections.Generic;
using System.Linq;
using System.Text.Json.Serialization;

namespace TSVmsDesktop.Models
{
    // --- Phase 2.1: Groups ---
    public class CameraGroup
    {
        [JsonPropertyName("id")] public string Id { get; set; } = "";
        [JsonPropertyName("name")] public string Name { get; set; } = "";
        [JsonPropertyName("description")] public string Description { get; set; } = "";
        [JsonPropertyName("member_ids")] public List<string> MemberIds { get; set; } = new();
    }

    // --- Phase 2.3: ONVIF ---
    public class DiscoveryRun
    {
        [JsonPropertyName("id")] public string Id { get; set; } = "";
        [JsonPropertyName("status")] public string Status { get; set; } = ""; // running, completed
        [JsonPropertyName("devices_found")] public int DevicesFound { get; set; }
    }

    public class DiscoveredDevice
    {
        [JsonPropertyName("id")] public string Id { get; set; } = "";
        [JsonPropertyName("name")] public string Name { get; set; } = "";
        [JsonPropertyName("ip_address")] public string IpAddress { get; set; } = "";
        [JsonPropertyName("manufacturer")] public string Manufacturer { get; set; } = "";
        [JsonPropertyName("model")] public string Model { get; set; } = "";
        [JsonPropertyName("is_claimed")] public bool IsClaimed { get; set; }
        [JsonPropertyName("has_audio")] public bool? HasAudio { get; set; }
        [JsonPropertyName("ptz")] public bool? Ptz { get; set; }
        [JsonPropertyName("ptz_supported")] public bool? PtzSupported { get; set; }
        [JsonPropertyName("xaddrs")] public List<string> XAddrs { get; set; } = new(); // Service URIs
        [JsonPropertyName("rtsp_uris")] public List<string> RtspUris { get; set; } = new(); // Discovered RTSP Endpoints
        [JsonPropertyName("media_profiles")] public List<MediaProfile> MediaProfiles { get; set; } = new();

        [JsonIgnore]
        public string EncodingDisplay => MediaProfiles?.FirstOrDefault()?.Encoding ?? "UNKNOWN";
        [JsonIgnore]
        public string ResolutionDisplay => MediaProfiles?.FirstOrDefault()?.Resolution ?? "0x0";
        [JsonIgnore]
        public string BitrateDisplay => (MediaProfiles?.FirstOrDefault()?.Bitrate ?? 0).ToString();
        [JsonIgnore]
        public string CameraNameDisplay => !string.IsNullOrWhiteSpace(Name)
            ? Name
            : (!string.IsNullOrWhiteSpace(Manufacturer) || !string.IsNullOrWhiteSpace(Model)
                ? $"{Manufacturer} {Model}".Trim()
                : IpAddress);
        [JsonIgnore]
        public string AudioDisplay
        {
            get
            {
                if (HasAudio.HasValue) return HasAudio.Value ? "Yes" : "No";
                var codec = MediaProfiles?.FirstOrDefault()?.AudioCodec ?? "";
                if (string.IsNullOrWhiteSpace(codec)) return "No";
                var c = codec.Trim().ToLowerInvariant();
                return c == "-" || c == "—" || c == "â€”" || c == "none" || c == "unknown" ? "No" : "Yes";
            }
        }
        [JsonIgnore]
        public string PtzDisplay => (Ptz ?? PtzSupported ?? false) ? "Yes" : "No";
        [JsonIgnore]
        public string RtspDisplay
        {
            get
            {
                var raw = RtspUris?.FirstOrDefault(u => !string.IsNullOrWhiteSpace(u)) ?? "";
                if (string.IsNullOrWhiteSpace(raw)) return "-";
                var val = raw.Contains("|") ? raw.Split('|').LastOrDefault() ?? raw : raw;
                if (val.Length <= 64) return val;
                return val.Substring(0, 61) + "...";
            }
        }
    }

    // --- Phase 2.4: Media ---
    public partial class MediaProfile : ObservableObject
    {
        [JsonPropertyName("profile_token")] public string Token { get; set; } = "";
        [JsonPropertyName("profile_name")] public string Name { get; set; } = "";
        [JsonPropertyName("video_codec")] public string Encoding { get; set; } = "H264";
        [JsonPropertyName("audio_codec")] public string AudioCodec { get; set; } = "—";
        [JsonPropertyName("width")] public int Width { get; set; }
        [JsonPropertyName("height")] public int Height { get; set; }
        [JsonPropertyName("fps")] public double Framerate { get; set; }
        [JsonPropertyName("bitrate_kbps")] public int Bitrate { get; set; }

        [ObservableProperty]
        [JsonPropertyName("rtsp_url_sanitized")] 
        private string _rtspUrl = "";

        [JsonIgnore]
        public string Resolution => $"{Width}x{Height}";

        [JsonIgnore]
        public string AudioDisplay => (string.IsNullOrEmpty(AudioCodec) || AudioCodec == "—") ? "NO" : "YES";

        [JsonIgnore]
        public string TypeDisplay { get; set; } = "—";
    }

    public class RtspValidationResult
    {
        [JsonPropertyName("variant")] public string Variant { get; set; } = "";
        [JsonPropertyName("success")] public bool Success { get; set; } // Only for immediate response
        [JsonPropertyName("last_error_code")] public string Error { get; set; } = "";
        [JsonPropertyName("rtt_ms")] public int LatencyMs { get; set; }
        [JsonPropertyName("status")] public string Status { get; set; } = "";
        [JsonPropertyName("validated_at")] public DateTime ValidatedAt { get; set; }
    }

    public class CameraStreamSelection
    {
        [JsonPropertyName("main_profile_token")] public string MainProfileToken { get; set; } = "";
        [JsonPropertyName("main_rtsp_url_sanitized")] public string MainRtsp { get; set; } = "";
        [JsonPropertyName("main_supported")] public bool MainSupported { get; set; }
        [JsonPropertyName("main_codec")] public string MainCodec { get; set; } = "";
        
        [JsonPropertyName("sub_profile_token")] public string SubProfileToken { get; set; } = "";
        [JsonPropertyName("sub_rtsp_url_sanitized")] public string SubRtsp { get; set; } = "";
        [JsonPropertyName("sub_supported")] public bool SubSupported { get; set; }
        [JsonPropertyName("sub_codec")] public string SubCodec { get; set; } = "";
        [JsonPropertyName("sub_is_same_as_main")] public bool SubIsSameAsMain { get; set; }
    }

    public class CameraMediaInfo
    {
        [JsonPropertyName("selection")] public CameraStreamSelection Selection { get; set; } = new();
        [JsonPropertyName("validation")] public List<RtspValidationResult> ValidationResults { get; set; } = new();
    }
    
    // --- Phase 2.5: Alerts ---
    public class CameraAlert
    {
        [JsonPropertyName("id")] public string Id { get; set; } = "";
        [JsonPropertyName("camera_id")] public string CameraId { get; set; } = "";
        [JsonPropertyName("type")] public string Type { get; set; } = ""; // offline, auth_fail
        [JsonPropertyName("message")] public string Message { get; set; } = "";
        [JsonPropertyName("timestamp")] public DateTime Timestamp { get; set; }
        [JsonPropertyName("severity")] public string Severity { get; set; } = "warning";
    }
}
