using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class WindowsService
    {
        private readonly ApiClient _api;
        public WindowsService(ApiClient api) => _api = api;

        public async Task<WindowsDiscoveryResult?> RunDiscoveryScanAsync() 
            => await _api.PostAsync<object, WindowsDiscoveryResult>("/api/v1/windows/discovery:scan", new { });
    }
}
