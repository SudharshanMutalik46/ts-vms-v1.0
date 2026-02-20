using System;
using System.Collections.Generic;
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
        [JsonPropertyName("ip_address")] public string IpAddress { get; set; } = "";
        [JsonPropertyName("manufacturer")] public string Manufacturer { get; set; } = "";
        [JsonPropertyName("model")] public string Model { get; set; } = "";
        [JsonPropertyName("is_claimed")] public bool IsClaimed { get; set; }
        [JsonPropertyName("xaddrs")] public List<string> XAddrs { get; set; } = new(); // Service URIs
        [JsonPropertyName("rtsp_uris")] public List<string> RtspUris { get; set; } = new(); // Discovered RTSP Endpoints
    }

    // --- Phase 2.4: Media ---
    public class MediaProfile
    {
        [JsonPropertyName("profile_token")] public string Token { get; set; } = "";
        [JsonPropertyName("profile_name")] public string Name { get; set; } = "";
        [JsonPropertyName("video_codec")] public string Encoding { get; set; } = "H264";
        [JsonPropertyName("width")] public int Width { get; set; }
        [JsonPropertyName("height")] public int Height { get; set; }
        [JsonPropertyName("fps")] public double Framerate { get; set; }
        [JsonPropertyName("bitrate_kbps")] public int Bitrate { get; set; }

        [JsonIgnore]
        public string Resolution => $"{Width}x{Height}";
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
        [JsonPropertyName("sub_profile_token")] public string SubProfileToken { get; set; } = "";
        [JsonPropertyName("sub_rtsp_url_sanitized")] public string SubRtsp { get; set; } = "";
        [JsonPropertyName("sub_supported")] public bool SubSupported { get; set; }
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
