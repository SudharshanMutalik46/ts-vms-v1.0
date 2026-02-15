using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class LicenseService
    {
        private readonly ApiClient _api;

        public LicenseService(ApiClient api)
        {
            _api = api;
        }

        public async Task<LicenseStatus?> GetStatusAsync()
        {
            return await _api.GetAsync<LicenseStatus>("/api/v1/license/status");
        }

        public async Task<bool> ReloadLicenseAsync()
        {
            // POST to reload, expects 200 OK
            try 
            {
                // We use PostAsync<object> to send an empty body if needed, or just a wrapper
                // The plan used PostAsync<object>("/api/v1/license/reload", new { });
                // ApiClient has PostAsync<T> which returns bool.
                return await _api.PostAsync("/api/v1/license/reload", new { });
            }
            catch 
            {
                return false;
            }
        }
    }
}
