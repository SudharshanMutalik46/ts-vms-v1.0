using System;
using System.Net;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Diagnostics;
using System.Text.Json;
using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class ApiClient
    {
        private readonly HttpClient _http;
        private readonly ISessionService _session;
        private readonly SettingsService _settings;
        private static readonly System.Threading.SemaphoreSlim _refreshLock =
            new System.Threading.SemaphoreSlim(1, 1);

        private const int MaxRetries = 3;

        public ApiClient(ISessionService session, SettingsService settings)
        {
            _session = session;
            _settings = settings;

            _http = new HttpClient
            {
                Timeout = TimeSpan.FromSeconds(15)
            };
        }

        private void EnsureBaseUrl()
        {
            var raw = _settings.CurrentSettings.BaseUrl?.Trim();

            if (string.IsNullOrWhiteSpace(raw))
                throw new InvalidOperationException("Base URL is not configured.");

            if (!raw.EndsWith("/"))
                raw += "/";

            var desired = new Uri(raw, UriKind.Absolute);

            if (_http.BaseAddress == null || _http.BaseAddress != desired)
            {
                _http.BaseAddress = desired;
            }
        }

        private string BuildAbsoluteUri(string uri)
        {
            EnsureBaseUrl();
            return new Uri(_http.BaseAddress!, uri).ToString();
        }

        private async Task<HttpResponseMessage> SendAsync(
            HttpRequestMessage request,
            bool allowAuthRetry = true,
            int retryCount = 0,
            System.Threading.CancellationToken cancellationToken = default)
        {
            EnsureBaseUrl();

            string? tokenUsed = _session.AccessToken;
            var started = Stopwatch.StartNew();
            var absoluteUri = BuildAbsoluteUri(request.RequestUri?.ToString() ?? string.Empty);

            if (!string.IsNullOrEmpty(tokenUsed))
            {
                request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", tokenUsed);
            }

            try
            {
                var response = await _http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, cancellationToken);
                started.Stop();

                if (request.RequestUri != null && request.RequestUri.OriginalString.Contains("/recording/", StringComparison.OrdinalIgnoreCase))
                {
                    System.Diagnostics.Debug.WriteLine(
                        $"[ApiClient] {request.Method} {absoluteUri} -> {(int)response.StatusCode} in {started.ElapsedMilliseconds}ms");
                }

                if (response.StatusCode == HttpStatusCode.Conflict)
                {
                    throw new Exception("Duplicate entry: This resource already exists.");
                }

                if (response.StatusCode == HttpStatusCode.Forbidden)
                {
                    System.Diagnostics.Debug.WriteLine("[ApiClient] 403 Forbidden - Access Denied");
                }

                if (response.StatusCode == (HttpStatusCode)429)
                {
                    if (retryCount >= MaxRetries)
                        return response;

                    var retryAfter =
                        response.Headers.RetryAfter?.Delta ??
                        TimeSpan.FromSeconds(Math.Pow(2, retryCount));

                    System.Diagnostics.Debug.WriteLine(
                        $"[RateLimit] 429 received. Retrying in {retryAfter.TotalSeconds}s...");

                    await Task.Delay(retryAfter);

                    var newReq = await CloneRequestAsync(request);
                    return await SendAsync(newReq, allowAuthRetry, retryCount + 1, cancellationToken);
                }

                if (response.StatusCode == HttpStatusCode.Unauthorized && allowAuthRetry)
                {
                    if (_session.AccessToken != tokenUsed && !string.IsNullOrEmpty(_session.AccessToken))
                    {
                        var newReq = await CloneRequestAsync(request);
                        return await SendAsync(newReq, false, 0, cancellationToken);
                    }

                    await _refreshLock.WaitAsync();
                    try
                    {
                        if (_session.AccessToken != tokenUsed && !string.IsNullOrEmpty(_session.AccessToken))
                        {
                            var newReq = await CloneRequestAsync(request);
                            return await SendAsync(newReq, false, 0, cancellationToken);
                        }

                        if (await PerformRefreshAsync())
                        {
                            var newReq = await CloneRequestAsync(request);
                            return await SendAsync(newReq, false, 0, cancellationToken);
                        }
                        else
                        {
                            _session.Clear();
                        }
                    }
                    finally
                    {
                        _refreshLock.Release();
                    }
                }

                return response;
            }
            catch (TaskCanceledException)
            {
                started.Stop();
                if (request.RequestUri != null && request.RequestUri.OriginalString.Contains("/recording/", StringComparison.OrdinalIgnoreCase))
                {
                    System.Diagnostics.Debug.WriteLine(
                        $"[ApiClient] TIMEOUT {request.Method} {absoluteUri} after {started.ElapsedMilliseconds}ms");
                }
                throw;
            }
            catch (Exception ex)
            {
                started.Stop();
                if (request.RequestUri != null && request.RequestUri.OriginalString.Contains("/recording/", StringComparison.OrdinalIgnoreCase))
                {
                    System.Diagnostics.Debug.WriteLine(
                        $"[ApiClient] ERROR {request.Method} {absoluteUri} after {started.ElapsedMilliseconds}ms: {ex.Message}");
                }
                throw new Exception($"Network Error: {ex.Message}", ex);
            }
        }

        // Safer clone: copies content bytes too, so retries do not reuse disposed streams.
        private async Task<HttpRequestMessage> CloneRequestAsync(HttpRequestMessage req)
        {
            var clone = new HttpRequestMessage(req.Method, req.RequestUri);

            if (req.Content != null)
            {
                var bytes = await req.Content.ReadAsByteArrayAsync();
                var content = new ByteArrayContent(bytes);

                foreach (var header in req.Content.Headers)
                    content.Headers.TryAddWithoutValidation(header.Key, header.Value);

                clone.Content = content;
            }

            foreach (var header in req.Headers)
                clone.Headers.TryAddWithoutValidation(header.Key, header.Value);

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

                EnsureBaseUrl();

                var req = new HttpRequestMessage(HttpMethod.Post, "/api/v1/auth/refresh")
                {
                    Content = JsonContent.Create(new RefreshRequest
                    {
                        refresh_token = _session.RefreshToken
                    })
                };

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
                    System.Diagnostics.Debug.WriteLine(
                        $"[Auth] Token Refresh Failed: {res.StatusCode} - {errInfo}");
                }
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"[Auth] Token Refresh Exception: {ex.Message}");
            }

            return false;
        }

        public Task<HttpResponseMessage> GetAsync(string uri, System.Threading.CancellationToken cancellationToken = default)
        {
            var req = new HttpRequestMessage(HttpMethod.Get, uri);
            return SendAsync(req, cancellationToken: cancellationToken);
        }

        public async Task<T?> GetAsync<T>(string uri, System.Threading.CancellationToken cancellationToken = default)
        {
            var req = new HttpRequestMessage(HttpMethod.Get, uri);
            var res = await SendAsync(req, cancellationToken: cancellationToken);

            string rawResponse = await res.Content.ReadAsStringAsync();
            string absoluteUri = BuildAbsoluteUri(uri);

            if (!res.IsSuccessStatusCode)
            {
                string err =
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] GET {absoluteUri} FAILED ({res.StatusCode}):\n{rawResponse}\n";
                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", err);
                return default;
            }

            if (uri.Contains("media-profiles", StringComparison.OrdinalIgnoreCase) ||
                uri.Contains("/recording/", StringComparison.OrdinalIgnoreCase))
            {
                string successMsg =
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] GET {absoluteUri} SUCCESS. Raw JSON:\n{rawResponse}\n";
                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", successMsg);
            }

            try
            {
                var options = new JsonSerializerOptions
                {
                    PropertyNameCaseInsensitive = true
                };
                return JsonSerializer.Deserialize<T>(rawResponse, options);
            }
            catch (Exception ex)
            {
                var sb = new System.Text.StringBuilder();
                sb.AppendLine("--------------------------------------------------");
                sb.AppendLine($"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] GET JSON PARSE ERROR");
                sb.AppendLine($"URI: {absoluteUri}");
                sb.AppendLine($"Exception: {ex.GetType().Name}: {ex.Message}");
                sb.AppendLine("RAW RESPONSE START >>>");
                sb.AppendLine(rawResponse);
                sb.AppendLine("<<< RAW RESPONSE END");
                sb.AppendLine("--------------------------------------------------");

                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", sb.ToString());
                throw;
            }
        }

        public async Task<bool> PostAsync<T>(string uri, T body)
        {
            var req = new HttpRequestMessage(HttpMethod.Post, uri)
            {
                Content = JsonContent.Create(body)
            };

            var res = await SendAsync(req);
            string absoluteUri = BuildAbsoluteUri(uri);

            if (!res.IsSuccessStatusCode)
            {
                string raw = await res.Content.ReadAsStringAsync();
                string err =
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] POST {absoluteUri} FAILED ({res.StatusCode}):\n{raw}\n";
                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", err);
            }

            return res.IsSuccessStatusCode;
        }

        public async Task<TResponse?> PostAsync<TRequest, TResponse>(string uri, TRequest body)
        {
            var req = new HttpRequestMessage(HttpMethod.Post, uri)
            {
                Content = JsonContent.Create(body)
            };

            var res = await SendAsync(req);
            string rawResponse = await res.Content.ReadAsStringAsync();
            string absoluteUri = BuildAbsoluteUri(uri);

            if (!res.IsSuccessStatusCode)
            {
                string err =
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] POST {absoluteUri} FAILED ({res.StatusCode}):\n{rawResponse}\n";
                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", err);
                throw new Exception($"Server Error ({res.StatusCode}). See logs\\api_debug_log.txt");
            }

            try
            {
                var options = new JsonSerializerOptions
                {
                    PropertyNameCaseInsensitive = true
                };
                return JsonSerializer.Deserialize<TResponse>(rawResponse, options);
            }
            catch (Exception ex)
            {
                var sb = new System.Text.StringBuilder();
                sb.AppendLine("--------------------------------------------------");
                sb.AppendLine($"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] POST JSON PARSE ERROR");
                sb.AppendLine($"URI: {absoluteUri}");
                sb.AppendLine($"Exception: {ex.GetType().Name}: {ex.Message}");
                sb.AppendLine("RAW RESPONSE START >>>");
                sb.AppendLine(rawResponse);
                sb.AppendLine("<<< RAW RESPONSE END");
                sb.AppendLine("--------------------------------------------------");

                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", sb.ToString());
                throw new Exception("JSON Parse Error! Check logs\\api_debug_log.txt.");
            }
        }

        public async Task<bool> PutAsync<T>(string uri, T body)
        {
            var req = new HttpRequestMessage(HttpMethod.Put, uri)
            {
                Content = JsonContent.Create(body)
            };

            var res = await SendAsync(req);
            string absoluteUri = BuildAbsoluteUri(uri);

            if (!res.IsSuccessStatusCode)
            {
                string raw = await res.Content.ReadAsStringAsync();
                string err =
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] PUT {absoluteUri} FAILED ({res.StatusCode}):\n{raw}\n";
                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", err);
            }

            return res.IsSuccessStatusCode;
        }

        public async Task<bool> DeleteAsync(string uri)
        {
            var req = new HttpRequestMessage(HttpMethod.Delete, uri);
            var res = await SendAsync(req);
            string absoluteUri = BuildAbsoluteUri(uri);

            if (!res.IsSuccessStatusCode)
            {
                string raw = await res.Content.ReadAsStringAsync();
                string err =
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] DELETE {absoluteUri} FAILED ({res.StatusCode}):\n{raw}\n";
                System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", err);
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
                var req = new HttpRequestMessage(HttpMethod.Post, uri)
                {
                    Content = JsonContent.Create(body)
                };

                var res = await SendAsync(req);
                string absoluteUri = BuildAbsoluteUri(uri);

                if (!res.IsSuccessStatusCode)
                {
                    string raw = await res.Content.ReadAsStringAsync();
                    string err =
                        $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] DOWNLOAD {absoluteUri} FAILED ({res.StatusCode}):\n{raw}\n";
                    System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", err);
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
                System.IO.File.AppendAllText(
                    @"LogPaths.ApiDebugLogPath",
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] Download Exception: {ex.Message}\n");
                return false;
            }
        }

        public async Task<bool> DownloadFileAsync(string uri, string outputPath)
        {
            try
            {
                var req = new HttpRequestMessage(HttpMethod.Get, uri);
                var res = await SendAsync(req);
                string absoluteUri = BuildAbsoluteUri(uri);

                if (!res.IsSuccessStatusCode)
                {
                    string raw = await res.Content.ReadAsStringAsync();
                    string err =
                        $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] DOWNLOAD GET {absoluteUri} FAILED ({res.StatusCode}):\n{raw}\n";
                    System.IO.File.AppendAllText(@"LogPaths.ApiDebugLogPath", err);
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
                System.IO.File.AppendAllText(
                    @"LogPaths.ApiDebugLogPath",
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss}] Download GET Exception: {ex.Message}\n");
                return false;
            }
        }

        public async Task<List<IFrameEntry>> GetIFrameIndexAsync(string cameraId, DateTime from, DateTime to)
        {
            string url = $"/api/v1/recording/iframes?camera_id={cameraId}&from={from:yyyy-MM-ddTHH:mm:ssZ}&to={to:yyyy-MM-ddTHH:mm:ssZ}";
            var results = await GetAsync<List<IFrameEntry>>(url);
            return results ?? new List<IFrameEntry>();
        }
    }

    public class IFrameEntry
    {
        public string SegPath { get; set; } = "";
        public double PtsSeconds { get; set; }
        public DateTime WallClockUtc { get; set; }
    }
}
