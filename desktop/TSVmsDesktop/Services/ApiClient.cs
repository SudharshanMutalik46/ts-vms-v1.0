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
        private static readonly System.Threading.SemaphoreSlim _refreshLock = new System.Threading.SemaphoreSlim(1, 1);
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

            // Capture current token before sending
            string tokenUsed = _session.AccessToken;
            
            if (!string.IsNullOrEmpty(tokenUsed))
            {
                request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", tokenUsed);
            }

            try 
            {
                // We use ResponseHeadersRead to avoid buffering large responses mostly, but here it's fine.
                var response = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead);

                // 0. Handle 409 Conflict (Duplicates)
                if (response.StatusCode == HttpStatusCode.Conflict)
                {
                    throw new Exception("Duplicate entry: This resource already exists.");
                }

                // 0. Handle 403 Forbidden
                if (response.StatusCode == HttpStatusCode.Forbidden) 
                {
                    System.Diagnostics.Debug.WriteLine("[ApiClient] 403 Forbidden - Access Denied");
                    // return default; // We can't return default here easily as it returns HttpResponseMessage.
                    // We should probably let it fall through or return the response so the caller handles it, 
                    // OR if we want to suppress it:
                    // But SendAsync returns HttpResponseMessage. Returning response is fine, but maybe mark it?
                    // The plan says "return default" but SendAsync returns HttpResponseMessage. 
                    // The "return default" likely referred to the generic GetAsync/PostAsync wrappers.
                    // However, if I throw here, it bubbles up. If I return response, IsSuccessStatusCode is false.
                    // Let's stick to returning response but maybe Log it clearly. 
                    // The plan snippet: "if (response.StatusCode == HttpStatusCode.Forbidden) { ... return default; }"
                    // That snippet was likely intended for the GetAsync/PostAsync wrappers or needed adaptation.
                    // I will let it fall through to the caller where IsSuccessStatusCode check will catch it,
                    // BUT I will add the specific catch in GetAsync/PostAsync implementation or here?
                    // Actually, let's look at where SendAsync is used.
                    // It returns HttpResponseMessage.
                    // If I return response, the caller checks IsSuccessStatusCode.
                    // If I want to interrupt, I should throw or handle it.
                    // The generic wrappers return default(T) if !IsSuccessStatusCode.
                }

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
                if (response.StatusCode == HttpStatusCode.Unauthorized && allowAuthRetry)
                {
                    // Check if token has changed since we sent the request (another thread might have refreshed it)
                    if (_session.AccessToken != tokenUsed && !string.IsNullOrEmpty(_session.AccessToken))
                    {
                        // Token already refreshed by someone else, retry immediately with new token
                        var newReq = CloneRequest(request);
                        return await SendAsync(newReq, false, 0); // Retry once with new token
                    }

                    // Otherwise, we need to refresh
                    await _refreshLock.WaitAsync();
                    try
                    {
                        // Double check after acquiring lock
                         if (_session.AccessToken != tokenUsed && !string.IsNullOrEmpty(_session.AccessToken))
                        {
                             // Token refreshed while we waited
                             var newReq = CloneRequest(request);
                             return await SendAsync(newReq, false, 0);
                        }

                        if (await PerformRefreshAsync())
                        {
                            var newReq = CloneRequest(request);
                            return await SendAsync(newReq, false, 0); // Retry with new token
                        }
                        else 
                        {
                            _session.Clear(); // Logout if refresh fails
                        }
                    }
                    finally
                    {
                        _refreshLock.Release();
                    }
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
            if (string.IsNullOrEmpty(_session.RefreshToken)) 
            {
                System.Diagnostics.Debug.WriteLine("[Auth] PerformRefreshAsync: No RefreshToken available.");
                return false;
            }

            try
            {
                System.Diagnostics.Debug.WriteLine("[Auth] Attempting Token Refresh...");
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
                        System.Diagnostics.Debug.WriteLine("[Auth] Token Refresh Successful.");
                        _session.SetTokens(data.access_token, data.refresh_token);
                        return true;
                    }
                }
                else
                {
                    string errInfo = await res.Content.ReadAsStringAsync();
                    System.Diagnostics.Debug.WriteLine($"[Auth] Token Refresh Failed: {res.StatusCode} - {errInfo}");
                }
            }
            catch (Exception ex)
            {
                 System.Diagnostics.Debug.WriteLine($"[Auth] Token Refresh Exception: {ex.Message}");
            }
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

            // SUCCESS LOGGING for specific endpoints (Media Profiles)
            if (uri.Contains("media-profiles"))
            {
                string successMsg = $"[{DateTime.Now}] GET {uri} SUCCESS. Raw JSON:\n{rawResponse}\n";
                System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", successMsg);
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

        public async Task<bool> PutAsync<T>(string uri, T body)
        {
            var req = new HttpRequestMessage(HttpMethod.Put, uri);
            req.Content = JsonContent.Create(body);
            var res = await SendAsync(req);
            
            if (!res.IsSuccessStatusCode)
            {
               string raw = await res.Content.ReadAsStringAsync();
               string err = $"[{DateTime.Now}] PUT {uri} FAILED ({res.StatusCode}):\n{raw}\n";
               System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", err);
            }
            
            return res.IsSuccessStatusCode;
        }

        public async Task<bool> DeleteAsync(string uri)
        {
            var req = new HttpRequestMessage(HttpMethod.Delete, uri);
            var res = await SendAsync(req);
            
            if (!res.IsSuccessStatusCode)
            {
               string raw = await res.Content.ReadAsStringAsync();
               string err = $"[{DateTime.Now}] DELETE {uri} FAILED ({res.StatusCode}):\n{raw}\n";
               System.IO.File.AppendAllText(@"C:\Users\sudha\Desktop\api_debug_log.txt", err);
            }
            
            return res.IsSuccessStatusCode;
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
