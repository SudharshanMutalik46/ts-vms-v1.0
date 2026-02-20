using System.Text.Json.Serialization;
using CommunityToolkit.Mvvm.ComponentModel;

namespace TSVmsDesktop.Models
{
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
                    url = $"rtsp://{IpAddress}:{rtspPort}/live/0/MAIN";
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
        
        [JsonIgnore]
        public string Thumbnail { get; set; } = "/Images/cam_placeholder.png"; 

        [ObservableProperty]
        [JsonIgnore]
        private bool _isSelected;
    }
}
