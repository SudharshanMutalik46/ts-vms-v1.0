using System;
using System.Net.Http;
using System.Text.Json.Nodes;
using System.Threading.Tasks;

namespace TSVmsDesktop.Services
{
    public interface IHealthService
    {
        Task<(bool IsHealthy, string Details)> CheckHealthAsync();
    }

    public class HealthService : IHealthService
    {
        private readonly HttpClient _httpClient;
        private const string HealthUrl = "http://127.0.0.1:8080/api/v1/healthz";

        public HealthService(HttpClient httpClient)
        {
            _httpClient = httpClient;
            _httpClient.Timeout = TimeSpan.FromSeconds(2);
        }

        public async Task<(bool IsHealthy, string Details)> CheckHealthAsync()
        {
            try
            {
                var response = await _httpClient.GetAsync(HealthUrl);
                if (response.IsSuccessStatusCode)
                {
                    var json = await response.Content.ReadAsStringAsync();
                    try 
                    {
                        var parsed = JsonNode.Parse(json);
                        return (true, parsed?.ToString() ?? "OK");
                    }
                    catch { return (true, json); }
                }
                return (false, $"Error: {response.StatusCode}");
            }
            catch (Exception ex)
            {
                return (false, $"Backend Offline: {ex.Message}");
            }
        }
    }
}
