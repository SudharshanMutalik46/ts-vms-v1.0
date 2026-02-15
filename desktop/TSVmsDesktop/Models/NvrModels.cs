using System;
using System.Collections.Generic;
using System.Text.Json.Serialization;
using CommunityToolkit.Mvvm.ComponentModel;

namespace TSVmsDesktop.Models
{
    public partial class NvrModel : ObservableObject
    {
        [JsonPropertyName("id")] public string Id { get; set; } = string.Empty;
        [JsonPropertyName("name")] public string Name { get; set; } = "New NVR";
        [JsonPropertyName("ip_address")] public string IpAddress { get; set; } = "";
        [JsonPropertyName("port")] public int Port { get; set; } = 80;
        [JsonPropertyName("username")] public string Username { get; set; } = "";
        
        [JsonPropertyName("vendor")] 
        public string AdapterType { get; set; } = "onvif"; // hikvision_isapi, dahua_json, onvif, rtsp

        // UI Helpers
        [JsonIgnore] public string Status { get; set; } = "Unknown"; // Populated by health summary
        [JsonIgnore] public int LinkedCameraCount { get; set; }
    }

    public class NvrChannel
    {
        [JsonPropertyName("id")] public string Id { get; set; } = "";
        [JsonPropertyName("channel_ref")] public string ChannelRef { get; set; } = "";
        [JsonPropertyName("name")] public string Name { get; set; } = "";
        [JsonPropertyName("rtsp_main_url_sanitized")] public string RtspUrl { get; set; } = "";
        [JsonPropertyName("provision_state")] public string Status { get; set; } = "not_created"; // not_created, created
        
        [JsonIgnore] public bool IsSelected { get; set; }
    }

    public class NvrEvent
    {
        [JsonPropertyName("occurred_at")] public DateTime Timestamp { get; set; }
        [JsonPropertyName("event_type")] public string Type { get; set; } = ""; 
        [JsonPropertyName("channel_ref")] public string ChannelRef { get; set; } = "";
        [JsonPropertyName("severity")] public string Severity { get; set; } = "";
        
        [JsonIgnore] public string Message => $"{Type} on {ChannelRef} ({Severity})";
    }

    public class NvrHealthSummary
    {
        [JsonPropertyName("status")] public string Status { get; set; } = "Online";
        [JsonPropertyName("latency_ms")] public int LatencyMs { get; set; }
        [JsonPropertyName("uptime_seconds")] public long UptimeSeconds { get; set; }
        [JsonPropertyName("last_check")] public DateTime LastCheck { get; set; }
    }

    public class WindowsDiscoveryResult
    {
        [JsonPropertyName("result")] public string Result { get; set; } = "success";
        [JsonPropertyName("reason")] public string Reason { get; set; } = "";
        [JsonPropertyName("hosts")] public List<DiscoveredHost> Hosts { get; set; } = new();
        [JsonPropertyName("firewall_status")] public string FirewallStatus { get; set; } = "Unknown";
        
        // Client-side populated
        [JsonIgnore] public DateTime ScannedAt { get; set; } = DateTime.Now;
    }

    public class DiscoveredHost
    {
        [JsonPropertyName("ip")] public string Ip { get; set; } = "";
        [JsonPropertyName("interface")] public string Interface { get; set; } = "";
        [JsonPropertyName("source")] public string Source { get; set; } = "";
    }
}
