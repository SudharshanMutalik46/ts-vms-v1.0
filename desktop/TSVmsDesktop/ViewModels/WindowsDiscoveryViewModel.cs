using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Threading.Tasks;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class WindowsDiscoveryViewModel : ObservableObject
    {
        private readonly WindowsService _winService;
        [ObservableProperty] private string _scanResult = "Ready to scan.";
        [ObservableProperty] private string _firewallInfo = "Unknown";
        [ObservableProperty] private bool _isScanning;

        public WindowsDiscoveryViewModel(WindowsService winService)
        {
            _winService = winService;
        }

        [RelayCommand]
        public async Task RunScan()
        {
            IsScanning = true;
            ScanResult = "Scanning local network protocols (SSDP/WS-Discovery)...";
            
            var result = await _winService.RunDiscoveryScanAsync();
            if (result != null)
            {
                ScanResult = $"Scan Complete.\nDevices Found: {result.Hosts.Count}\n" + string.Join("\n", result.Hosts.Select(h => $"{h.Ip} ({h.Interface}) - {h.Source}"));
                FirewallInfo = result.FirewallStatus;
            }
            else
            {
                ScanResult = "Scan Failed. Check Backend Logs.";
            }
            IsScanning = false;
        }
    }
}
