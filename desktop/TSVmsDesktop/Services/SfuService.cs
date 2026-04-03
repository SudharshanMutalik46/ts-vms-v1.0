using System;
using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class SfuService
    {
        private readonly ApiClient _api;

        public SfuService(ApiClient api)
        {
            _api = api;
        }

        public async Task<string> JoinRoomAsync(string roomId)
        {
            var res = await _api.PostAsync<object, object>($"/api/v1/sfu/rooms/{roomId}/join", new { });
            return res?.ToString() ?? "";
        }

        public async Task<bool> ResumeConsumerAsync(string consumerId)
        {
            return await _api.PostAsync($"/api/v1/sfu/consumers/{consumerId}/resume", new { });
        }

        public async Task<bool> RegisterProducerAsync(object producerData)
        {
            return await _api.PostAsync("/api/v1/sfu/producers", producerData);
        }

        public async Task<RtpCapabilities?> GetRtpCapabilitiesAsync(string roomId)
        {
            return await _api.GetAsync<RtpCapabilities>($"/api/v1/sfu/rooms/{roomId}/rtp-capabilities");
        }

        public async Task<TransportInfo?> CreateTransportAsync(string roomId)
        {
             // WebRTC playback uses the dedicated transport endpoint.
             return await _api.PostAsync<object, TransportInfo>($"/api/v1/sfu/rooms/{roomId}/transports/webrtc", new { type = "recv" });
        }

        public async Task<bool> ConnectTransportAsync(string transportId, object dtlsParameters)
        {
            return await _api.PostAsync($"/api/v1/sfu/transports/{transportId}/connect", new { dtlsParameters });
        }

        public async Task<ConsumeResponse?> ConsumeAsync(string roomId, string transportId, object rtpCapabilities)
        {
            return await _api.PostAsync<object, ConsumeResponse>($"/api/v1/sfu/rooms/{roomId}/transports/{transportId}/consume", new { rtpCapabilities });
        }
        
        public async Task LeaveSessionAsync(string sessionId)
        {
             await _api.PostAsync($"/api/v1/sfu/sessions/{sessionId}/leave", new {});
        }
    }

    // Placeholder DTOs for Mediasoup
    public class RtpCapabilities { /* ... JSON ... */ }
    public class TransportInfo { 
        [System.Text.Json.Serialization.JsonPropertyName("id")] public string Id { get; set; } = "";
        /* IceCandidates, IceParameters, DtlsParameters... */ 
    }
    public class ConsumeResponse { 
        [System.Text.Json.Serialization.JsonPropertyName("id")] public string Id { get; set; } = "";
        /* ProducerId, Kind, RtpParameters... */ 
    }
}
