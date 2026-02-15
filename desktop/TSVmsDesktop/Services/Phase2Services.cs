using System.Collections.Generic;
using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class OnvifService
    {
        private readonly ApiClient _api;
        public OnvifService(ApiClient api) => _api = api;

        // Phase 2.3
        public async Task<string?> StartDiscoveryAsync()
        {
            // POST /api/v1/onvif/discovery-runs -> Returns { "id": "..." }
            var res = await _api.PostAsync<object, DiscoveryRun>("/api/v1/onvif/discovery-runs", new { });
            return res?.Id;
        }

        public async Task<DiscoveryRun?> GetRunStatusAsync(string runId)
        {
            return await _api.GetAsync<DiscoveryRun>($"/api/v1/onvif/discovery-runs/{runId}");
        }

        public async Task<List<DiscoveredDevice>> GetDiscoveredDevicesAsync(string runId)
        {
             // Pass run_id query param
            return await _api.GetAsync<List<DiscoveredDevice>>($"/api/v1/onvif/discovered-devices?discovery_run_id={runId}") ?? new();
        }

        public async Task<bool> ProbeDeviceAsync(string deviceId, string credId)
        {
            return await _api.PostAsync($"/api/v1/onvif/discovered-devices/{deviceId}/probe", new { credential_id = credId });
        }

        public async Task<string?> SetOnvifCredentialsAsync(string username, string password)
        {
            // Returns { "id": "..." }
            var res = await _api.PostAsync<object, Dictionary<string,string>>("/api/v1/onvif/credentials", new { username, password });
            return res != null && res.ContainsKey("id") ? res["id"] : null;
        }
    }

    public class MediaService
    {
        private readonly ApiClient _api;
        public MediaService(ApiClient api) => _api = api;

        // Phase 2.4
        public async Task<List<MediaProfile>> GetProfilesAsync(string camId)
        {
            return await _api.GetAsync<List<MediaProfile>>($"/api/v1/cameras/{camId}/media-profiles") ?? new();
        }

        public async Task<bool> SelectProfilesAsync(string camId, string mainToken, string subToken)
        {
            return await _api.PostAsync($"/api/v1/cameras/{camId}/select-media-profiles", new { main_token = mainToken, sub_token = subToken });
        }

        public async Task<RtspValidationResult?> ValidateRtspAsync(string camId)
        {
            return await _api.PostAsync<object, RtspValidationResult>($"/api/v1/cameras/{camId}/validate-rtsp", new { });
        }

        public async Task<CameraMediaInfo?> GetMediaInfoAsync(string camId)
        {
            return await _api.GetAsync<CameraMediaInfo>($"/api/v1/cameras/{camId}/media-selection");
        }
    }

    public class CredentialService
    {
        private readonly ApiClient _api;
        public CredentialService(ApiClient api) => _api = api;

        // Phase 2.2
        public async Task<bool> UpdateCredentialsAsync(string camId, string username, string password)
        {
            return await _api.PutAsync($"/api/v1/cameras/{camId}/credentials", new { username, password });
        }
    }

    public class AlertsService
    {
        private readonly ApiClient _api;
        public AlertsService(ApiClient api) => _api = api;

        // Phase 2.5
        public async Task<List<CameraAlert>> GetAlertsAsync()
        {
            return await _api.GetAsync<List<CameraAlert>>("/api/v1/alerts/cameras") ?? new();
        }
    }
}
