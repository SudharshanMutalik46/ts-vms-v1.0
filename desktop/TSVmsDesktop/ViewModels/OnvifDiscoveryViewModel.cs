using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using System.Threading.Tasks;
using System.Windows;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;
using System.Linq;

namespace TSVmsDesktop.ViewModels
{
    public partial class OnvifDiscoveryViewModel : ObservableObject
    {
        private readonly OnvifService _service;
        private readonly CameraService _camService; // To adopt
        private readonly MainViewModel _mainViewModel;

        [ObservableProperty] private string _discoveryStatus = "Ready to Scan";
        [ObservableProperty] private bool _isScanning;
        [ObservableProperty] private ObservableCollection<DiscoveredDevice> _devices = new();

        public OnvifDiscoveryViewModel(OnvifService service, CameraService camService, MainViewModel mainViewModel)
        {
            _service = service; _camService = camService; _mainViewModel = mainViewModel;
        }

        [RelayCommand]
        public async Task StartScan()
        {
            if (IsScanning) return;

            IsScanning = true;
            DiscoveryStatus = "Starting initial probe...";
            Devices.Clear();

            var runId = await _service.StartDiscoveryAsync();
            
            if(runId != null)
            {
                // Poll for a few seconds (Simplified logic, better to have a real job status check)
                // In a real app we might poll /api/v1/onvif/discovery-runs/{id}
                for(int i=0; i<5; i++)
                {
                    await Task.Delay(1000);
                    DiscoveryStatus = $"Scanning network... ({i+1}s)";
                }
                DiscoveryStatus = "Retrieving results...";
                
                var results = await _service.GetDiscoveredDevicesAsync(runId);
                
                if (results.Count == 0)
                {
                     DiscoveryStatus = "No devices found.";
                }
                else
                {
                    DiscoveryStatus = $"Scan complete. Found {results.Count} devices.";
                }

                foreach(var d in results) Devices.Add(d);
            }
            IsScanning = false;
        }

        [RelayCommand]
        public async Task AdoptDevice(DiscoveredDevice device)
        {
            if (device == null || device.IsClaimed) return;

            // Convert DiscoveredDevice to CameraModel and POST
            // We assume default credentials or prompt?
            // For now, simpler implementation: Create camera with "Unknown" credentials waiting for update
            
            var cam = new CameraModel
            {
                Name = !string.IsNullOrWhiteSpace(device.Manufacturer) ? $"{device.Manufacturer} {device.Model}" : "New ONVIF Camera",
                IpAddress = device.IpAddress,
                Model = device.Model,
                Status = "Online",
                RtspUrl = !string.IsNullOrWhiteSpace(device.XAddr) ? device.XAddr : "" // Approximate
            };
            
            // We might need to handle credentials if the discovery service didn't have them
            // But let's assume we just add it to inventory
            
            bool success = await _camService.CreateCameraAsync(cam);
            if(success) 
            {
                device.IsClaimed = true; // Update UI
                // Force UI update manually since ObservableObject might not catch nested property change if not observable
                // Actually DiscoveredDevice is not ObservableObject, so we might need to refresh list or re-assign
                var index = Devices.IndexOf(device);
                if (index != -1)
                {
                    Devices[index] = device; // Trigger change
                }
                System.Windows.MessageBox.Show("Device added to inventory.", "Success", MessageBoxButton.OK, MessageBoxImage.Information);
            }
            else
            {
                System.Windows.MessageBox.Show("Failed to add device.", "Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public void Close()
        {
            _mainViewModel.NavigateToCameras();
        }
    }
}
