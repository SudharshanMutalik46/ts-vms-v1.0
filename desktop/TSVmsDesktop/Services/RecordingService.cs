using System;
using System.Collections.Generic;
using System.Linq;
using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class RecordingService
    {
        private readonly ApiClient _api;
        private RecordingStatusResponse _lastStatusResponse = new();
        private Dictionary<string, string> _lastStatusMap = new(StringComparer.OrdinalIgnoreCase);
        private DateTime _lastStatusFetch = DateTime.MinValue;
        private readonly System.Threading.SemaphoreSlim _fetchLock = new(1, 1);
        private static readonly TimeSpan CacheDuration = TimeSpan.FromSeconds(2);

        public RecordingService(ApiClient api)
        {
            _api = api;
        }

        public async Task<RecordingStatusResponse> GetStatusSnapshotAsync()
        {
            if (DateTime.Now - _lastStatusFetch < CacheDuration)
                return _lastStatusResponse;

            await _fetchLock.WaitAsync();
            try
            {
                if (DateTime.Now - _lastStatusFetch < CacheDuration)
                    return _lastStatusResponse;

                var status = await _api.GetAsync<RecordingStatusResponse>("/api/v1/recording/status");
                _lastStatusResponse = status ?? new RecordingStatusResponse();
                var map = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);

                if (_lastStatusResponse.Workers != null)
                {
                    foreach (var worker in _lastStatusResponse.Workers)
                    {
                        if (!string.IsNullOrWhiteSpace(worker?.CameraId))
                            map[worker.CameraId] = worker.State ?? string.Empty;
                    }
                }

                _lastStatusMap = map;
                _lastStatusFetch = DateTime.Now;
                return _lastStatusResponse;
            }
            catch
            {
                return _lastStatusResponse; // Return stale cache on error
            }
            finally
            {
                _fetchLock.Release();
            }
        }

        public async Task<Dictionary<string, string>> GetAllStatusesAsync()
        {
            var snapshot = await GetStatusSnapshotAsync();
            var map = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
            foreach (var worker in snapshot.Workers)
            {
                if (!string.IsNullOrWhiteSpace(worker?.CameraId))
                    map[worker.CameraId] = worker.State ?? string.Empty;
            }

            return map;
        }

        public async Task<string> GetCameraStatusAsync(string cameraId)
        {
            if (string.IsNullOrWhiteSpace(cameraId)) return "UNKNOWN";

            var map = await GetAllStatusesAsync();
            return map.TryGetValue(cameraId, out var state) && !string.IsNullOrWhiteSpace(state)
                ? state
                : "UNKNOWN";
        }

        public async Task<List<RecordingSchedule>> GetSchedulesAsync()
        {
            return await _api.GetAsync<List<RecordingSchedule>>("/api/v1/recording/schedules")
                   ?? new List<RecordingSchedule>();
        }

        public async Task<RecordingSchedule?> GetScheduleAsync(string cameraId)
        {
            var all = await GetSchedulesAsync();
            return all.FirstOrDefault(x => string.Equals(x.CameraId, cameraId, StringComparison.OrdinalIgnoreCase));
        }

        public Task<bool> SaveScheduleAsync(RecordingSchedule schedule)
        {
            return _api.PostAsync("/api/v1/recording/schedules", schedule);
        }

        public async Task<List<RecordingSegment>> GetSegmentsAsync(string cameraId, DateTime fromUtc, DateTime toUtc, System.Threading.CancellationToken cancellationToken = default)
        {
            if (string.IsNullOrWhiteSpace(cameraId))
                return new List<RecordingSegment>();

            string from = Uri.EscapeDataString(fromUtc.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"));
            string to = Uri.EscapeDataString(toUtc.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"));
            string url = $"/api/v1/recording/cameras/{cameraId}/segments?from={from}&to={to}";

            return await _api.GetAsync<List<RecordingSegment>>(url, cancellationToken) ?? new List<RecordingSegment>();
        }

        public async Task<List<RecordedCamera>> GetRecordedCamerasAsync(DateTime fromUtc, DateTime toUtc, System.Threading.CancellationToken cancellationToken = default)
        {
            string from = Uri.EscapeDataString(fromUtc.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"));
            string to = Uri.EscapeDataString(toUtc.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"));
            string url = $"/api/v1/recording/cameras-with-recordings?from={from}&to={to}";
            return await _api.GetAsync<List<RecordedCamera>>(url, cancellationToken) ?? new List<RecordedCamera>();
        }

        public Task<bool> StartCameraAsync(string cameraId) =>
            _api.PostAsync($"/api/v1/recording/cameras/{cameraId}/start", new { });

        public Task<bool> StopCameraAsync(string cameraId) =>
            _api.PostAsync($"/api/v1/recording/cameras/{cameraId}/stop", new { });

        public Task<bool> PauseCameraAsync(string cameraId) =>
            _api.PostAsync($"/api/v1/recording/cameras/{cameraId}/pause", new { });

        public Task<bool> ResumeCameraAsync(string cameraId) =>
            _api.PostAsync($"/api/v1/recording/cameras/{cameraId}/resume", new { });

        public async Task<RecordingExportResponse?> QueueExportAsync(string cameraId, DateTime fromUtc, DateTime toUtc)
        {
            var req = new RecordingExportRequest
            {
                CameraId = cameraId,
                FromTs = fromUtc.ToUniversalTime(),
                ToTs = toUtc.ToUniversalTime(),
                Format = "mp4"
            };

            return await _api.PostAsync<RecordingExportRequest, RecordingExportResponse>("/api/v1/recording/exports", req);
        }

        public Task<bool> DownloadExportAsync(string downloadUrl, string targetPath)
        {
            return _api.DownloadFileAsync(downloadUrl, targetPath);
        }

        public async Task<double> EstimateWriteRateBytesPerSecondAsync(IEnumerable<string> cameraIds, int lookbackMinutes = 5)
        {
            if (cameraIds == null) return 0;

            var nowUtc = DateTime.UtcNow;
            var fromUtc = nowUtc.AddMinutes(-Math.Max(1, lookbackMinutes));
            var uniqueIds = cameraIds
                .Where(id => !string.IsNullOrWhiteSpace(id))
                .Distinct(StringComparer.OrdinalIgnoreCase)
                .ToArray();

            if (uniqueIds.Length == 0) return 0;

            double totalBytes = 0;
            double totalSeconds = 0;

            foreach (var cameraId in uniqueIds)
            {
                List<RecordingSegment> segments;
                try
                {
                    segments = await GetSegmentsAsync(cameraId, fromUtc, nowUtc);
                }
                catch
                {
                    continue;
                }

                foreach (var segment in segments
                    .Where(s => s.SizeBytes > 0 && s.DurationSeconds > 0)
                    .OrderByDescending(s => s.EndTs)
                    .Take(12))
                {
                    totalBytes += segment.SizeBytes;
                    totalSeconds += segment.DurationSeconds;
                }
            }

            return totalSeconds > 0 ? totalBytes / totalSeconds : 0;
        }

        public async Task<bool> WaitAndDownloadExportAsync(
            RecordingExportResponse job,
            string targetPath,
            int maxAttempts = 20,
            int delayMs = 3000)
        {
            if (job == null || string.IsNullOrWhiteSpace(job.DownloadUrl))
                return false;

            for (int attempt = 0; attempt < maxAttempts; attempt++)
            {
                if (await DownloadExportAsync(job.DownloadUrl, targetPath))
                    return true;

                await Task.Delay(delayMs);
            }

            return false;
        }

        public string ToUiState(string rawState)
        {
            if (string.IsNullOrWhiteSpace(rawState)) return "Unknown";

            return rawState.ToUpperInvariant() switch
            {
                "RECORDING" => "Recording",
                "PAUSED" => "Paused",
                "RETRYING" => "Retrying",
                "STOPPED" => "Stopped",
                "THROTTLED_BY_LICENSE" => "License Throttled",
                "UNKNOWN" => "Unknown",
                _ => rawState
            };
        }

        public string ToUiColor(string rawState)
        {
            return rawState?.ToUpperInvariant() switch
            {
                "RECORDING" => "#16A34A",
                "PAUSED" => "#F59E0B",
                "RETRYING" => "#EF4444",
                "THROTTLED_BY_LICENSE" => "#8B5CF6",
                "STOPPED" => "#64748B",
                _ => "#94A3B8"
            };
        }
    }
}
