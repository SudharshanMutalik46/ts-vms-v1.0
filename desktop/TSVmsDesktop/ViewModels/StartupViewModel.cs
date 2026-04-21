using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Threading.Tasks;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class StartupViewModel : ObservableObject
    {
        private readonly IHealthService _healthService;
        private bool _started;
        private readonly object _startLock = new();
        
        // FIX: Use an Action instead of the heavy MainViewModel dependency
        public Action? OnStartupSuccess { get; set; }

        [ObservableProperty] private string _statusText = "Connecting to VMS Backend...";
        [ObservableProperty] private string _details = "";
        [ObservableProperty] private bool _isRetryVisible = false;
        [ObservableProperty] private bool _isLoading = true;

        // FIX: Removed MainViewModel from constructor parameters
        public StartupViewModel(IHealthService healthService)
        {
            _healthService = healthService;
        }

        public void StartIfNeeded()
        {
            lock (_startLock)
            {
                if (_started) return;
                _started = true;
            }

            _ = InitializeSystem();
        }

        private async Task InitializeSystem()
        {
            IsLoading = true;
            IsRetryVisible = false;
            StatusText = "Checking Database Connectivity...";
            Details = string.Empty;

            for (int i = 0; i < 5; i++)
            {
                var health = await _healthService.CheckHealthAsync();
                if (health.IsHealthy)
                {
                    StatusText = "System Ready. Launching...";
                    await Task.Delay(500); 
                    
                    // FIX: Invoke the callback safely on the UI thread
                    System.Windows.Application.Current.Dispatcher.Invoke(() => OnStartupSuccess?.Invoke());
                    return;
                }
                
                StatusText = $"Waiting for services... ({i+1}/5)";
                Details = health.Details;
                await Task.Delay(2000);
            }

            // Failed
            IsLoading = false;
            IsRetryVisible = true;
            StatusText = "Backend Unreachable";
        }

        [RelayCommand]
        public async Task Retry()
        {
            await InitializeSystem();
        }

        [RelayCommand]
        public void CopyDiagnostics()
        {
            System.Windows.Clipboard.SetText($"Timestamp: {DateTime.Now}\nStatus: {StatusText}\nDetails: {Details}");
            System.Windows.MessageBox.Show("Diagnostics copied to clipboard.");
        }
    }
}
