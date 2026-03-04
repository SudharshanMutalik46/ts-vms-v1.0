using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Threading.Tasks;
using System.Windows.Media;
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
        private PeriodicTimer? _timer;

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

        public HealthViewModel(IHealthService healthService, RecordingService recordingService)
        {
            _healthService = healthService;
            _recordingService = recordingService;

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

                await FetchRecordingTelemetry();
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
        }

        private async Task FetchRecordingTelemetry()
        {
            try
            {
                Dictionary<string, string> statuses = await _recordingService.GetAllStatusesAsync();

                int recording = 0;
                int paused = 0;
                int retrying = 0;
                int licensed = 0;

                foreach (var kvp in statuses)
                {
                    switch ((kvp.Value ?? "").ToUpperInvariant())
                    {
                        case "RECORDING":
                            recording++;
                            break;
                        case "PAUSED":
                            paused++;
                            break;
                        case "RETRYING":
                            retrying++;
                            break;
                        case "THROTTLED_BY_LICENSE":
                            licensed++;
                            break;
                    }
                }

                ActiveRecordingsCount = recording;
                DiskWriteRate = $"{(recording * 0.50):F1} MB/s";

                if (retrying > 0)
                {
                    GlobalFrameDropRate = $"Retry loops active: {retrying}";
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
    }
}
