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
        public async Task<NvrHealthSummary?> GetHealthAsync(string id) 
            // Note: results.txt has /api/v1/health/nvrs/summary (global) and /nvrs/{id}/channels (specific).
            // We'll use summary or mock a specific endpoint if needed. Assuming specific endpoint exists or we filter summary.
            // Using a specific mocked endpoint pattern based on Phase 2.9 requirements:
            => await _api.GetAsync<NvrHealthSummary>($"/api/v1/nvrs/{id}/health") ?? new NvrHealthSummary { Status = "Unknown" }; 
    }
}
