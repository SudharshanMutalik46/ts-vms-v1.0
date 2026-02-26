using System.Text.Json.Serialization;
using CommunityToolkit.Mvvm.ComponentModel;

namespace TSVmsDesktop.Models
{
    public class CameraCapabilitiesDto
    {
        [JsonPropertyName("has_audio")]
        public bool HasAudio { get; set; }
    }

    public partial class CameraModel : ObservableObject
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = string.Empty;

        [JsonPropertyName("name")]
        public string Name { get; set; } = "New Camera";

        [JsonPropertyName("ip_address")]
        public string IpAddress { get; set; } = "127.0.0.1";

        // Required by backend
        [JsonPropertyName("site_id")]
        public string SiteId { get; set; } = "";

        // NEW: Capture the Port from JSON (default to 554 RTSP)
        [JsonPropertyName("port")]
        public int Port { get; set; } = 554;

        [JsonPropertyName("is_enabled")]
        public bool IsEnabled { get; set; }

        // We update this manually after our TCP Ping
        [ObservableProperty] 
        [JsonPropertyName("status")]
        private string _status = "Checking...";

        [JsonPropertyName("model")]
        public string Model { get; set; } = "Generic";

        [JsonPropertyName("rtsp_url")]
        public string RtspUrl { get; set; } = "";

        [JsonIgnore]
        public string Username { get; set; } = "";

        [JsonIgnore]
        public string Password { get; set; } = "";

        [JsonPropertyName("capabilities")]
        public CameraCapabilitiesDto? Capabilities { get; set; }

        /// <summary>
        /// Returns the explicit RtspUrl if set, otherwise constructs one from IpAddress and Port.
        /// Injects credentials if available.
        /// </summary>
        [JsonIgnore]
        public string EffectiveRtspUrl
        {
            get
            {
                string url = !string.IsNullOrWhiteSpace(RtspUrl) ? RtspUrl : "";
                
                if (string.IsNullOrWhiteSpace(url) && !string.IsNullOrWhiteSpace(IpAddress) && IpAddress != "127.0.0.1")
                {
                    int rtspPort = Port > 0 ? Port : 554;
                    url = $"rtsp://{IpAddress}:{rtspPort}/live/0/SUB";
                }

                if (string.IsNullOrWhiteSpace(url)) return "";

                // Inject credentials if we have them and they aren't already in the URL
                if (!string.IsNullOrWhiteSpace(Username) && !url.Contains("@"))
                {
                    try
                    {
                        if (url.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
                        {
                            return $"rtsp://{Username}:{Password}@{url.Substring(7)}";
                        }
                    }
                    catch { }
                }

                return url;
            }
        }
        
        /// <summary>
        /// Returns a lower-resolution sub-stream URL if possible, for use in the multi-camera grid.
        /// </summary>
        [JsonIgnore]
        public string SubStreamUrl
        {
            get
            {
                string url = EffectiveRtspUrl;
                if (!string.IsNullOrWhiteSpace(url))
                {
                    // 1. Hikvision (e.g. /Streaming/Channels/101 -> 102)
                    if (url.Contains("/Channels/101", StringComparison.OrdinalIgnoreCase))
                    {
                        url = url.Replace("/Channels/101", "/Channels/102", StringComparison.OrdinalIgnoreCase);
                    }
                    // 2. Dahua (e.g. channel=1&subtype=0 -> subtype=1)
                    else if (url.Contains("subtype=0", StringComparison.OrdinalIgnoreCase))
                    {
                        url = url.Replace("subtype=0", "subtype=1", StringComparison.OrdinalIgnoreCase);
                    }
                    else if (url.Contains("/cam/realmonitor?", StringComparison.OrdinalIgnoreCase) && !url.Contains("subtype=1", StringComparison.OrdinalIgnoreCase))
                    {
                        url += "&subtype=1";
                    }
                    // 3. Generic ONVIF Main -> Sub
                    else if (url.Contains("/MAIN", StringComparison.OrdinalIgnoreCase))
                    {
                        url = url.Replace("/MAIN", "/SUB", StringComparison.OrdinalIgnoreCase);
                    }
                    else if (url.Contains("stream=0", StringComparison.OrdinalIgnoreCase))
                    {
                        url = url.Replace("stream=0", "stream=1", StringComparison.OrdinalIgnoreCase);
                    }
                    else if (url.Contains("Profile_1", StringComparison.OrdinalIgnoreCase))
                    {
                        // Some ONVIF schemas use Profile_1 vs Profile_2 or _3
                        url = url.Replace("Profile_1", "Profile_2", StringComparison.OrdinalIgnoreCase);
                    }
                }
                return url;
            }
        }

        [JsonIgnore]
        public string Thumbnail { get; set; } = "/Images/cam_placeholder.png"; 

        [ObservableProperty]
        [JsonIgnore]
        private bool _isSelected;

        /// <summary>
        /// Returns a higher-resolution main-stream URL if possible.
        /// </summary>
        [JsonIgnore]
        public string MainStreamUrl
        {
            get
            {
                string url = EffectiveRtspUrl;
                if (!string.IsNullOrWhiteSpace(url))
                {
                    // 1. Hikvision (e.g. /Streaming/Channels/102 -> 101)
                    if (url.Contains("/Channels/102", StringComparison.OrdinalIgnoreCase))
                        url = url.Replace("/Channels/102", "/Channels/101", StringComparison.OrdinalIgnoreCase);
                    
                    // 2. Dahua (e.g. channel=1&subtype=1 -> subtype=0)
                    else if (url.Contains("subtype=1", StringComparison.OrdinalIgnoreCase))
                        url = url.Replace("subtype=1", "subtype=0", StringComparison.OrdinalIgnoreCase);
                    
                    // 3. Generic ONVIF Sub -> Main
                    else if (url.Contains("/SUB", StringComparison.OrdinalIgnoreCase))
                        url = url.Replace("/SUB", "/MAIN", StringComparison.OrdinalIgnoreCase);
                        
                    else if (url.Contains("stream=1", StringComparison.OrdinalIgnoreCase))
                        url = url.Replace("stream=1", "stream=0", StringComparison.OrdinalIgnoreCase);
                        
                    else if (url.Contains("Profile_2", StringComparison.OrdinalIgnoreCase))
                        url = url.Replace("Profile_2", "Profile_1", StringComparison.OrdinalIgnoreCase);
                }
                return url;
            }
        }
    }
}
