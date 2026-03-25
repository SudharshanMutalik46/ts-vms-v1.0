using System.Text.Json.Serialization;
using System.Threading.Tasks;

namespace TSVmsDesktop.Services
{
    public class LiveSessionService
    {
        private readonly ApiClient _api;
        public LiveSessionService(ApiClient api) => _api = api;

        public async Task<LiveStreamSession?> StartSessionAsync(string cameraId, string quality = "sub")
        {
            return await _api.PostAsync<object, LiveStreamSession>(
                $"/api/v1/cameras/{cameraId}/live/start",
                new { quality, view_mode = "grid" });
        }
    }

    public class LiveFallbackPolicy
    {
        [JsonPropertyName("webrtc_connect_timeout_ms")]
        public int WebRtcConnectTimeoutMs { get; set; }

        [JsonPropertyName("webrtc_track_timeout_ms")]
        public int WebRtcTrackTimeoutMs { get; set; }

        [JsonPropertyName("max_auto_retries")]
        public int MaxAutoRetries { get; set; }

        [JsonPropertyName("retry_backoff_ms")]
        public System.Collections.Generic.List<int>? RetryBackoffMs { get; set; }
    }

    public class LiveStreamSession
    {
        [JsonPropertyName("viewer_session_id")] public string ViewerSessionId { get; set; } = "";
        [JsonPropertyName("expires_at")]        public long   ExpiresAt       { get; set; }
        [JsonPropertyName("primary")]           public string Primary         { get; set; } = "webrtc";
        [JsonPropertyName("fallback")]          public string Fallback        { get; set; } = "hls";
        [JsonPropertyName("hls")]               public LiveStreamHls?    Hls    { get; set; }
        [JsonPropertyName("webrtc")]            public LiveStreamWebRtc? WebRtc { get; set; }
        [JsonPropertyName("fallback_policy")]   public LiveFallbackPolicy? FallbackPolicy { get; set; }
    }

    public class LiveStreamHls
    {
        [JsonPropertyName("playlist_url")]    public string PlaylistUrl    { get; set; } = "";
        [JsonPropertyName("target_latency_ms")] public int  TargetLatencyMs { get; set; }
    }

    public class LiveStreamWebRtc
    {
        [JsonPropertyName("sfu_url")]            public string SfuUrl           { get; set; } = "";
        [JsonPropertyName("room_id")]            public string RoomId           { get; set; } = "";
        [JsonPropertyName("connect_timeout_ms")] public int    ConnectTimeoutMs { get; set; }
    }
}
