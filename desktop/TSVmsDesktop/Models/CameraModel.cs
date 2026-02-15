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

        /// <summary>
        /// Returns the explicit RtspUrl if set, otherwise constructs one from IpAddress and Port.
        /// This is needed because the backend only stores ip_address and port, not a full RTSP URL.
        /// </summary>
        [JsonIgnore]
        public string EffectiveRtspUrl
        {
            get
            {
                if (!string.IsNullOrWhiteSpace(RtspUrl)) return RtspUrl;
                if (!string.IsNullOrWhiteSpace(IpAddress) && IpAddress != "127.0.0.1")
                {
                    int rtspPort = Port > 0 ? Port : 554;
                    return $"rtsp://{IpAddress}:{rtspPort}/live/0/MAIN";
                }
                return "";
            }
        }
        
        [JsonIgnore]
        public string Thumbnail { get; set; } = "/Images/cam_placeholder.png"; 

        [ObservableProperty]
        [JsonIgnore]
        private bool _isSelected;
    }
}
