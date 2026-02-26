using System.Collections.Generic;
using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class NvrService
    {
        private readonly ApiClient _api;
        public NvrService(ApiClient api) => _api = api;

        // Inventory
        public async Task<List<NvrModel>> GetNvrsAsync()
        {
             var result = await _api.GetAsync<PaginatedResponse<NvrModel>>("/api/v1/nvrs");
             return result?.Data ?? new List<NvrModel>();
        }
        
        // Helper for unwrapping API responses
        public class PaginatedResponse<T>
        {
            public List<T> Data { get; set; } = new();
            public int Total { get; set; }
        }

        public async Task<bool> CreateNvrAsync(NvrModel nvr) => await _api.PostAsync("/api/v1/nvrs", nvr);
        public async Task<bool> UpdateNvrAsync(NvrModel nvr) => await _api.PutAsync($"/api/v1/nvrs/{nvr.Id}", nvr);
        public async Task<bool> DeleteNvrAsync(string id) => await _api.DeleteAsync($"/api/v1/nvrs/{id}");

        // Credentials (Separate secure call)
        public async Task<bool> SetCredentialsAsync(string id, string username, string password) 
            => await _api.PutAsync($"/api/v1/nvrs/{id}/credentials", new { username, password });

        // Phase 2.8: Discovery & Provisioning
        public async Task<bool> TestConnectionAsync(string id) => await _api.PostAsync($"/api/v1/nvrs/{id}/test-connection", new { });
        
        public async Task<bool> StartDiscoveryAsync(string id) => await _api.PostAsync($"/api/v1/nvrs/{id}/discover-channels", new { });
        
        public async Task<List<NvrChannel>> GetChannelsAsync(string id) 
        {
            var result = await _api.GetAsync<PaginatedResponse<NvrChannel>>($"/api/v1/nvrs/{id}/channels");
            return result?.Data ?? new List<NvrChannel>();
        }

        public async Task<bool> ProvisionCamerasAsync(string id, List<string> channelIds) 
            => await _api.PostAsync($"/api/v1/nvrs/{id}/provision-cameras", new { channel_ids = channelIds });

        // Phase 2.10: Events
        public async Task<List<NvrEvent>> GetEventsAsync(string id) 
            => await _api.GetAsync<List<NvrEvent>>($"/api/v1/nvrs/{id}/adapter/events") ?? new();

        // Phase 2.9: Health
        // Phase 2.9: Health
        public async Task<NvrHealthSummary?> GetHealthAsync(string id) 
            => await _api.GetAsync<NvrHealthSummary>($"/api/v1/nvrs/{id}/health") ?? new NvrHealthSummary { Status = "Unknown" }; 

        public async Task<NvrModel?> GetNvrAsync(string id) => await _api.GetAsync<NvrModel>($"/api/v1/nvrs/{id}");

        // NVR Cameras Linked
        public async Task<bool> UpsertNvrCamerasAsync(string id, object data) => await _api.PutAsync($"/api/v1/nvrs/{id}/cameras", data);
        public async Task<object?> GetNvrCamerasAsync(string id) => await _api.GetAsync<object>($"/api/v1/nvrs/{id}/cameras");
        public async Task<bool> DeleteNvrCamerasAsync(string id) => await _api.DeleteAsync($"/api/v1/nvrs/{id}/cameras");

        // NVR Credentials
        public async Task<object?> GetNvrCredentialsAsync(string id) => await _api.GetAsync<object>($"/api/v1/nvrs/{id}/credentials");
        public async Task<bool> DeleteNvrCredentialsAsync(string id) => await _api.DeleteAsync($"/api/v1/nvrs/{id}/credentials");

        // Adapter specific
        public async Task<object?> GetAdapterDeviceInfoAsync(string id) => await _api.GetAsync<object>($"/api/v1/nvrs/{id}/adapter/device-info");
        public async Task<object?> GetAdapterChannelsAsync(string id) => await _api.GetAsync<object>($"/api/v1/nvrs/{id}/adapter/channels");

        // Advanced Channel validation & bulk
        public async Task<bool> ValidateChannelsAsync(string id, object data) => await _api.PostAsync($"/api/v1/nvrs/{id}/validate-channels", data);
        public async Task<bool> BulkChannelOpAsync(string id, object data) => await _api.PostAsync($"/api/v1/nvrs/{id}/channels/bulk", data);

        // Real Health Endpoints
        public async Task<object?> GetGlobalNvrHealthSummaryAsync() => await _api.GetAsync<object>("/api/v1/health/nvrs/summary");
        public async Task<object?> GetNvrChannelHealthAsync(string id) => await _api.GetAsync<object>($"/api/v1/health/nvrs/{id}/channels");
    }
}
