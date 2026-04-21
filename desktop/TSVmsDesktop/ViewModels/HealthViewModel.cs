using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Media;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class ServiceStatus : ObservableObject
    {
        [ObservableProperty] private string _name = string.Empty;
        [ObservableProperty] private string _status = "Checking...";
        [ObservableProperty] private System.Windows.Media.Brush _color = System.Windows.Media.Brushes.Gray;
    }

    public partial class HealthViewModel : ObservableObject
    {
        private readonly IHealthService _healthService;
        private readonly RecordingService _recordingService;
        private readonly AuditService _auditService;
        private PeriodicTimer? _timer;
        private readonly SemaphoreSlim _refreshLock = new(1, 1);
        private DateTime _lastIoSampleAt = DateTime.MinValue;
        private double _lastIoBytesPerSecond = 0;

        [ObservableProperty] private string _statusMessage = "Initializing...";
        [ObservableProperty] private string _jsonDetails = "{}";
        [ObservableProperty] private System.Windows.Media.Brush _statusColor = System.Windows.Media.Brushes.Gray;
        [ObservableProperty] private DateTime _lastCheck;

        // Phase 4 recording telemetry
        [ObservableProperty] private int _activeRecordingsCount;
        [ObservableProperty] private string _diskWriteRate = "0.0 MB/s";
        [ObservableProperty] private string _globalFrameDropRate = "Idle";
        [ObservableProperty] private string _engineStateColor = "#94A3B8";

        public ObservableCollection<ServiceStatus> BackendServices { get; } = new();

        public HealthViewModel(IHealthService healthService, RecordingService recordingService, AuditService auditService)
        {
            _healthService = healthService;
            _recordingService = recordingService;
            _auditService = auditService;

            InitializeServiceList();
            StartPolling();
        }

        private void InitializeServiceList()
        {
            BackendServices.Clear();
            BackendServices.Add(new ServiceStatus { Name = "Control Plane" });
            BackendServices.Add(new ServiceStatus { Name = "Media Plane" });
            BackendServices.Add(new ServiceStatus { Name = "Recording Engine" });
            BackendServices.Add(new ServiceStatus { Name = "Metadata / Export" });
            BackendServices.Add(new ServiceStatus { Name = "Tiered Storage" });
        }

        private void StartPolling()
        {
            _ = PollLoopAsync();
        }

        private async Task PollLoopAsync()
        {
            _timer?.Dispose();
            _timer = new PeriodicTimer(TimeSpan.FromSeconds(5));

            await CheckHealth();

            while (await _timer.WaitForNextTickAsync())
            {
                await CheckHealth();
            }
        }

        [RelayCommand]
        public async Task CheckHealth()
        {
            if (!await _refreshLock.WaitAsync(0))
                return;

            try
            {
                var result = await _healthService.CheckHealthAsync();
                LastCheck = DateTime.Now;
                JsonDetails = result.Details;

                if (result.IsHealthy)
                {
                    StatusMessage = "SYSTEM ONLINE";
                    StatusColor = System.Windows.Media.Brushes.LimeGreen;

                    foreach (var svc in BackendServices)
                    {
                        svc.Status = "Running";
                        svc.Color = System.Windows.Media.Brushes.LimeGreen;
                    }
                }
                else
                {
                    StatusMessage = "SYSTEM DEGRADED";
                    StatusColor = System.Windows.Media.Brushes.OrangeRed;

                    foreach (var svc in BackendServices)
                    {
                        svc.Status = "Degraded";
                        svc.Color = System.Windows.Media.Brushes.OrangeRed;
                    }
                }

                await FetchRecordingTelemetry(result.IsHealthy);
            }
            catch (Exception ex)
            {
                StatusMessage = "CONNECTION FAILED";
                StatusColor = System.Windows.Media.Brushes.Red;
                JsonDetails = ex.Message;
                ActiveRecordingsCount = 0;
                DiskWriteRate = "Error";
                GlobalFrameDropRate = "Unavailable";
                EngineStateColor = "#EF4444";

                foreach (var svc in BackendServices)
                {
                    svc.Status = "Offline";
                    svc.Color = System.Windows.Media.Brushes.Red;
                }
            }
            finally
            {
                _refreshLock.Release();
            }
        }

        private async Task FetchRecordingTelemetry(bool systemHealthy)
        {
            try
            {
                var snapshot = await _recordingService.GetStatusSnapshotAsync();
                var workers = snapshot.Workers ?? Array.Empty<RecordingWorkerStatus>();
                var nowUtc = DateTime.UtcNow;

                int recording = workers.Count(w => string.Equals(w.State, "RECORDING", StringComparison.OrdinalIgnoreCase));
                int paused = workers.Count(w => w.Paused || string.Equals(w.State, "PAUSED", StringComparison.OrdinalIgnoreCase));
                int retrying = workers.Count(w => w.Retries > 0 || string.Equals(w.State, "RETRYING", StringComparison.OrdinalIgnoreCase));
                int licensed = workers.Count(w => string.Equals(w.State, "THROTTLED_BY_LICENSE", StringComparison.OrdinalIgnoreCase));
                int errored = workers.Count(w => string.Equals(w.State, "ERROR", StringComparison.OrdinalIgnoreCase));
                int stale = workers.Count(w =>
                    w.LastHeartbeat > DateTime.MinValue &&
                    (nowUtc - w.LastHeartbeat.ToUniversalTime()).TotalSeconds > 20);

                ActiveRecordingsCount = recording;

                var activeCameraIds = workers
                    .Where(w => string.Equals(w.State, "RECORDING", StringComparison.OrdinalIgnoreCase))
                    .Select(w => w.CameraId)
                    .Where(id => !string.IsNullOrWhiteSpace(id))
                    .ToArray();

                if (activeCameraIds.Length == 0)
                {
                    _lastIoBytesPerSecond = 0;
                    _lastIoSampleAt = DateTime.UtcNow;
                }
                else if ((DateTime.UtcNow - _lastIoSampleAt).TotalSeconds >= 20)
                {
                    _lastIoBytesPerSecond = await _recordingService.EstimateWriteRateBytesPerSecondAsync(activeCameraIds, 5);
                    _lastIoSampleAt = DateTime.UtcNow;
                }

                DiskWriteRate = $"{(_lastIoBytesPerSecond / (1024d * 1024d)):F2} MB/s";

                if (!systemHealthy)
                {
                    GlobalFrameDropRate = "Service degraded";
                    EngineStateColor = "#EF4444";
                }
                else if (errored > 0 || retrying > 0)
                {
                    GlobalFrameDropRate = $"Errors: {Math.Max(errored, retrying)}";
                    EngineStateColor = "#EF4444";
                }
                else if (licensed > 0)
                {
                    GlobalFrameDropRate = $"License throttled: {licensed}";
                    EngineStateColor = "#8B5CF6";
                }
                else if (paused > 0)
                {
                    GlobalFrameDropRate = $"Paused: {paused}";
                    EngineStateColor = "#F59E0B";
                }
                else if (stale > 0)
                {
                    GlobalFrameDropRate = $"Heartbeat lag: {stale}";
                    EngineStateColor = "#F59E0B";
                }
                else if (recording > 0)
                {
                    GlobalFrameDropRate = "Healthy";
                    EngineStateColor = "#10B981";
                }
                else
                {
                    GlobalFrameDropRate = "Idle";
                    EngineStateColor = "#94A3B8";
                }
            }
            catch
            {
                ActiveRecordingsCount = 0;
                DiskWriteRate = "Error";
                GlobalFrameDropRate = "Unavailable";
                EngineStateColor = "#EF4444";
            }
        }

        [RelayCommand]
        public async Task ExportLogs()
        {
            var dialog = new Microsoft.Win32.SaveFileDialog
            {
                Filter = "CSV Files|*.csv",
                FileName = $"audit_log_{DateTime.Now:yyyyMMdd_HHmm}.csv"
            };

            if (dialog.ShowDialog() != true)
                return;

            bool success = await _auditService.ExportLogsAsync(dialog.FileName, null, null);
            if (success)
                System.Windows.MessageBox.Show("Logs exported successfully.", "Health", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Information);
            else
                System.Windows.MessageBox.Show("Failed to export logs.", "Health", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
        }
    }
}
