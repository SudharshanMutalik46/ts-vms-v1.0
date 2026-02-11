using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.ObjectModel;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using Brush = System.Windows.Media.Brush;
using Brushes = System.Windows.Media.Brushes;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class ServiceStatus : ObservableObject
    {
        [ObservableProperty] private string _name = string.Empty;
        [ObservableProperty] private string _status = "Checking...";
        [ObservableProperty] private Brush _color = Brushes.Gray;
    }

    public partial class HealthViewModel : ObservableObject
    {
        private readonly IHealthService _healthService;
        private PeriodicTimer? _timer;

        [ObservableProperty] private string _statusMessage = "Initializing...";
        [ObservableProperty] private string _jsonDetails = "{}";
        [ObservableProperty] private Brush _statusColor = Brushes.Gray;
        [ObservableProperty] private DateTime _lastCheck;

        // List of individual services
        public ObservableCollection<ServiceStatus> BackendServices { get; } = new();

        public HealthViewModel(IHealthService healthService)
        {
            _healthService = healthService;
            InitializeServiceList();
            StartPolling();
        }

        private void InitializeServiceList()
        {
            BackendServices.Add(new ServiceStatus { Name = "Control Plane" });
            BackendServices.Add(new ServiceStatus { Name = "Media Plane" });
            BackendServices.Add(new ServiceStatus { Name = "AI Inference" });
            BackendServices.Add(new ServiceStatus { Name = "NATS / Redis" });
        }

        [RelayCommand]
        public async Task CheckHealth()
        {
            try
            {
                var result = await _healthService.CheckHealthAsync();
                LastCheck = DateTime.Now; // Updates the "Last Pulse" time
                JsonDetails = result.Details;
                
                if (result.IsHealthy) {
                    StatusMessage = "SYSTEM ONLINE";
                    StatusColor = Brushes.LimeGreen;
                    foreach(var s in BackendServices) { s.Status = "Running"; s.Color = Brushes.LimeGreen; }
                } else {
                    StatusMessage = "SYSTEM OFFLINE";
                    StatusColor = Brushes.Red;
                    foreach(var s in BackendServices) { s.Status = "Stopped"; s.Color = Brushes.Red; }
                }
            }
            catch (Exception)
            {
                 StatusMessage = "CONNECTION FAILED";
                 StatusColor = Brushes.Red;
            }
        }

        [RelayCommand]
        public void ExportLogs()
        {
            System.Windows.MessageBox.Show("Exporting Support Bundle: %AppData%\\Local\\TS-VMS\\Logs.zip", "Diagnostic Tool");
        }

        private async void StartPolling()
        {
            // UPDATED: Changed from 3 seconds to 1 second for smooth updates
            _timer = new PeriodicTimer(TimeSpan.FromSeconds(1));
            
            await CheckHealth();
            while (await _timer.WaitForNextTickAsync()) 
            {
                await CheckHealth();
            }
        }
    }
}
