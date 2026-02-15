using System;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;
using System.Threading.Tasks;
using System.Net;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class ApiClient
    {
        private readonly HttpClient _http;
        private readonly ISessionService _session;
        private readonly SettingsService _settings;
        private bool _isRefreshing = false;
        private const int MaxRetries = 3;

        public ApiClient(ISessionService session, SettingsService settings)
        {
            _session = session;
            _settings = settings;
            _http = new HttpClient
            {
                Timeout = TimeSpan.FromSeconds(15) // Increased timeout for stability
            };
        }

        private void EnsureBaseUrl()
        {
            if (_http.BaseAddress == null)
            {
                _http.BaseAddress = new Uri(_settings.CurrentSettings.BaseUrl);
            }
        }

        private async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, bool allowAuthRetry = true, int retryCount = 0)
        {
            EnsureBaseUrl();

            if (!string.IsNullOrEmpty(_session.AccessToken))
            {
                request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", _session.AccessToken);
            }

            try 
            {
                // We use ResponseHeadersRead to avoid buffering large responses mostly, but here it's fine.
                var response = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead);

                // 1. Handle Rate Limiting (429)
                if (response.StatusCode == (HttpStatusCode)429)
                {
                    if (retryCount >= MaxRetries) return response; // Give up

                    var retryAfter = response.Headers.RetryAfter?.Delta ?? TimeSpan.FromSeconds(Math.Pow(2, retryCount)); // Backoff
                    
                    System.Diagnostics.Debug.WriteLine($"[RateLimit] 429 received. Retrying in {retryAfter.TotalSeconds}s...");
                    
                    await Task.Delay(retryAfter);
                    
                    // Clone request for retry
                    var newReq = CloneRequest(request);
                    return await SendAsync(newReq, allowAuthRetry, retryCount + 1);
                }

                // 2. Handle Auth (401)
                if (response.StatusCode == HttpStatusCode.Unauthorized && allowAuthRetry && !_isRefreshing)
                {
                    _isRefreshing = true;
                    if (await PerformRefreshAsync())
                    {
                        _isRefreshing = false;
                        var newReq = CloneRequest(request);
                        return await SendAsync(newReq, false, 0); // Reset retry count for fresh token
                    }
                    _isRefreshing = false;
                    _session.Clear(); 
                    // Refresh failed, effectively logged out. Caller will handle 401 response.
                }

                return response;
            }
            catch (TaskCanceledException) { throw; } // Timeout
            catch (Exception ex) 
            { 
                throw new Exception($"Network Error: {ex.Message}", ex);
            }
        }

        // Helper to clone request (HttpRequestMessage cannot be reused)
        private HttpRequestMessage CloneRequest(HttpRequestMessage req)
        {
            var clone = new HttpRequestMessage(req.Method, req.RequestUri);
            if (req.Content != null) 
            {
                 // Note: Ideally we buffer content if we expect to retry.
                 // For JSON payloads this is usually fine as they are small and buffered by JsonContent.
                 clone.Content = req.Content; 
            }
            foreach (var header in req.Headers) clone.Headers.TryAddWithoutValidation(header.Key, header.Value);
            return clone;
        }

        private async Task<bool> PerformRefreshAsync()
        {
            if (string.IsNullOrEmpty(_session.RefreshToken)) return false;

            try
            {
                // Create a separate request to avoid infinite loops in SendAsync
                var req = new HttpRequestMessage(HttpMethod.Post, "/api/v1/auth/refresh");
                req.Content = JsonContent.Create(new RefreshRequest { refresh_token = _session.RefreshToken });
                
                // Use raw http send to avoid recursive auth checks
                var res = await _http.SendAsync(req);

                if (res.IsSuccessStatusCode)
                {
                    var data = await res.Content.ReadFromJsonAsync<LoginResponse>();
                    if (data != null)
                    {
                        _session.SetTokens(data.access_token, data.refresh_token);
                        return true;
                    }
                }
            }
            catch { }
            return false;
        }

        public async Task<T?> GetAsync<T>(string uri)
        {
            var req = new HttpRequestMessage(HttpMethod.Get, uri);
            var res = await SendAsync(req);
            
            string rawResponse = await res.Content.ReadAsStringAsync();

            if (!res.IsSuccessStatusCode)
            {
                // DEBUG: Log GetAsync failures too
                string err = $"[{DateTime.Now}] GET {uri} FAILED ({res.StatusCode}):\n{rawResponse}\n";
                System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", err);
                return default;
            }

            try
            {
                var options = new JsonSerializerOptions { PropertyNameCaseInsensitive = true };
                return JsonSerializer.Deserialize<T>(rawResponse, options);
            }
            catch (Exception ex)
            {
                 // CRITICAL DEBUGGING: Log to file
                var sb = new System.Text.StringBuilder();
                sb.AppendLine("--------------------------------------------------");
                sb.AppendLine($"[{DateTime.Now}] GET JSON PARSE ERROR");
                sb.AppendLine($"URI: {uri}");
                sb.AppendLine($"Exception: {ex.GetType().Name}: {ex.Message}");
                sb.AppendLine("RAW RESPONSE START >>>");
                sb.AppendLine(rawResponse);
                sb.AppendLine("<<< RAW RESPONSE END");
                sb.AppendLine("--------------------------------------------------");
                
                System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", sb.ToString());
                throw;
            }
        }

        public async Task<bool> PostAsync<T>(string uri, T body)
        {
            var req = new HttpRequestMessage(HttpMethod.Post, uri);
            req.Content = JsonContent.Create(body);
            var res = await SendAsync(req);
            if (!res.IsSuccessStatusCode)
            {
               string raw = await res.Content.ReadAsStringAsync();
               string err = $"[{DateTime.Now}] POST {uri} FAILED ({res.StatusCode}):\n{raw}\n";
               System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", err);
            }
            return res.IsSuccessStatusCode;
        }

        public async Task<TResponse?> PostAsync<TRequest, TResponse>(string uri, TRequest body)
        {
            var req = new HttpRequestMessage(HttpMethod.Post, uri);
            req.Content = JsonContent.Create(body);

            var res = await SendAsync(req);
            string rawResponse = await res.Content.ReadAsStringAsync();

            if (!res.IsSuccessStatusCode)
            {
                string err = $"[{DateTime.Now}] POST {uri} FAILED ({res.StatusCode}):\n{rawResponse}\n";
                System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", err);
                throw new Exception($"Server Error ({res.StatusCode}). See api_debug_log.txt");
            }

            try
            {
                var options = new JsonSerializerOptions { PropertyNameCaseInsensitive = true };
                return JsonSerializer.Deserialize<TResponse>(rawResponse, options);
            }
            catch (Exception ex)
            {
                // CRITICAL DEBUGGING: Log to file
                var sb = new System.Text.StringBuilder();
                sb.AppendLine("--------------------------------------------------");
                sb.AppendLine($"[{DateTime.Now}] POST JSON PARSE ERROR");
                sb.AppendLine($"URI: {uri}");
                sb.AppendLine($"Exception: {ex.GetType().Name}: {ex.Message}");
                sb.AppendLine("RAW RESPONSE START >>>");
                sb.AppendLine(rawResponse);
                sb.AppendLine("<<< RAW RESPONSE END");
                sb.AppendLine("--------------------------------------------------");
                
                System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", sb.ToString());

                // Throw generic exception so LoginViewModel shows it
                throw new Exception("JSON Parse Error! Check api_debug_log.txt on Desktop.");
            }
        }
        public async Task<string> GetStringAsync(string uri)
        {
            try 
            {
                var req = new HttpRequestMessage(HttpMethod.Get, uri);
                var res = await SendAsync(req);
                return await res.Content.ReadAsStringAsync();
            }
            catch 
            {
                return string.Empty;
            }
        }

        public async Task<bool> DownloadFileAsync<TRequest>(string uri, TRequest body, string outputPath)
        {
            try
            {
                var req = new HttpRequestMessage(HttpMethod.Post, uri);
                req.Content = JsonContent.Create(body);

                // Use SendAsync to handle auth/retries
                var res = await SendAsync(req);

                if (!res.IsSuccessStatusCode)
                {
                    string raw = await res.Content.ReadAsStringAsync();
                    string err = $"[{DateTime.Now}] DOWNLOAD {uri} FAILED ({res.StatusCode}):\n{raw}\n";
                    System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", err);
                    return false;
                }

                using (var fs = new System.IO.FileStream(outputPath, System.IO.FileMode.Create, System.IO.FileAccess.Write))
                {
                    await res.Content.CopyToAsync(fs);
                }
                return true;
            }
            catch (Exception ex)
            {
                System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", $"[{DateTime.Now}] Download Exception: {ex.Message}\n");
                return false;
            }
        }
    }
}
