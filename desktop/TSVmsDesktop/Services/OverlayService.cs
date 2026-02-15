using System.Collections.Generic;
using System.Threading.Tasks;

namespace TSVmsDesktop.Services
{
    public class OverlayService
    {
        private readonly ApiClient _api;

        public OverlayService(ApiClient api)
        {
            _api = api;
        }

        public async Task<bool> EnableOverlayAsync(string sessionId)
        {
            return await _api.PostAsync($"/api/v1/live/{sessionId}/overlay/enable", new { });
        }

        public async Task<bool> DisableOverlayAsync(string sessionId)
        {
            return await _api.PostAsync($"/api/v1/live/{sessionId}/overlay/disable", new { });
        }

        public async Task<List<DetectionBox>> GetLatestDetectionsAsync(string cameraId)
        {
            return await _api.GetAsync<List<DetectionBox>>($"/api/v1/cameras/{cameraId}/detections/latest") ?? new List<DetectionBox>();
        }
    }

    public class DetectionBox
    {
        [System.Text.Json.Serialization.JsonPropertyName("label")]
        public string Label { get; set; } = "Object";
        
        [System.Text.Json.Serialization.JsonPropertyName("confidence")]
        public float Confidence { get; set; }
        // Normalized 0..1
        [System.Text.Json.Serialization.JsonPropertyName("x")]
        public float X { get; set; }
        
        [System.Text.Json.Serialization.JsonPropertyName("y")]
        public float Y { get; set; }
        
        [System.Text.Json.Serialization.JsonPropertyName("w")]
        public float Width { get; set; }
        
        [System.Text.Json.Serialization.JsonPropertyName("h")]
        public float Height { get; set; }
    }
}
