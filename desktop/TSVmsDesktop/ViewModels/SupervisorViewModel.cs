using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Threading.Tasks;
using System.Windows.Threading;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class SupervisorViewModel : ObservableObject
    {
        private readonly SupervisorService _supervisorService;
        private readonly DispatcherTimer _timer;

        [ObservableProperty] private string _healthStatus = "Checking...";
        [ObservableProperty] private string _details = "";
        [ObservableProperty] private DateTime _lastCheck;
        [ObservableProperty] private string _systemState = "Unknown"; // Ready, Degraded, Down

        public SupervisorViewModel(SupervisorService supervisorService)
        {
            _supervisorService = supervisorService;
            _timer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(3) };
            _timer.Tick += async (s, e) => await CheckHealth();
            _timer.Start();
            _ = CheckHealth();
        }

        [RelayCommand]
        public async Task CheckHealth()
        {
            var (healthy, details) = await _supervisorService.GetSystemHealthAsync();
            LastCheck = DateTime.Now;
            Details = details;
            
            if (healthy)
            {
                HealthStatus = "Online";
                SystemState = "Ready";
            }
            else
            {
                HealthStatus = "Unreachable / Error";
                SystemState = "Down";
            }
        }

        [RelayCommand]
        public void OpenLogs()
        {
            _supervisorService.OpenLogFolder();
        }

        [RelayCommand]
        public void OpenEventViewer()
        {
            _supervisorService.OpenEventViewer();
        }

        [RelayCommand]
        public void CopyDiagnostics()
        {
            var diag = $"TIMESTAMP: {DateTime.UtcNow}\nSTATUS: {SystemState}\nDETAILS:\n{Details}";
            System.Windows.Clipboard.SetText(diag);
            System.Windows.MessageBox.Show("Diagnostics copied to clipboard.");
        }
    }
}
