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
        private const string DEFAULT_SITE_ID = "00000000-0000-0000-0000-000000000001";

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

            try
            {
                Console.WriteLine("[DEBUG] Starting discovery run...");
                var runId = await _service.StartDiscoveryAsync();
                Console.WriteLine($"[DEBUG] Discovery Run ID: {runId ?? "NULL"}");
                
                if(runId != null)
                {
                    // Poll run status until completed
                    bool isComplete = false;
                    for (int i = 0; i < 30; i++) // Poll for up to 30 seconds
                    {
                        await Task.Delay(1000);
                        var runStatus = await _service.GetRunStatusAsync(runId);
                        if (runStatus != null && (runStatus.Status == "completed" || runStatus.Status == "failed"))
                        {
                            isComplete = true;
                            break;
                        }
                        DiscoveryStatus = $"Scanning network... {i + 1}s";
                        System.Diagnostics.Debug.WriteLine($"[Discovery] Run {runId} status: {runStatus?.Status ?? "unknown"}");
                    }
                    
                    if (!isComplete)
                    {
                         DiscoveryStatus = "Scan timed out (Backend still running).";
                    }
                    else
                    {
                        DiscoveryStatus = "Retrieving results...";
                        var results = await _service.GetDiscoveredDevicesAsync(runId);
                        Console.WriteLine($"[DEBUG] Found {results.Count} devices.");

                        await System.Windows.Application.Current.Dispatcher.InvokeAsync(() => 
                        {
                            Devices.Clear();
                            if (results.Count == 0)
                            {
                                 DiscoveryStatus = "No devices found.";
                            }
                            else
                            {
                                foreach (var d in results) Devices.Add(d);
                                DiscoveryStatus = $"Found {results.Count} devices. Probing details...";
                            }
                        });
                        
                        if(results.Count > 0)
                        {
                            // AUTO-PROBE LOGIC (Temporary for Phase 2.3 verification)
                            try 
                            {
                                // 1. Create Bootstrap Credential (Hardcoded for typical ONVIF)
                                var credId = await _service.SetOnvifCredentialsAsync("admin", "123456");
                                if (!string.IsNullOrEmpty(credId))
                                {
                                    int probedCount = 0;
                                    foreach(var d in results)
                                    {
                                        bool success = await _service.ProbeDeviceAsync(d.Id, credId);
                                        if(success) probedCount++;
                                    }
                                    
                                    // 2. Refresh results to get the RTSP URIs populated by the probe
                                    if (probedCount > 0)
                                    {
                                        results = await _service.GetDiscoveredDevicesAsync(runId);
                                        await System.Windows.Application.Current.Dispatcher.InvokeAsync(() => 
                                        {
                                            Devices.Clear();
                                            foreach (var d in results) Devices.Add(d);
                                            DiscoveryStatus = $"Scan complete. Found {results.Count} devices ({probedCount} probed).";
                                        });
                                    }
                                }
                            }
                            catch (Exception probEx)
                            {
                                 System.Diagnostics.Debug.WriteLine($"[Probe] Auto-probe failed: {probEx.Message}");
                                 await System.Windows.Application.Current.Dispatcher.InvokeAsync(() => DiscoveryStatus = $"Scan complete. Found {results.Count} devices (Probe failed).");
                            }
                        }
                    }
                }
                else
                {
                    DiscoveryStatus = "Failed to start discovery run.";
                }
            }
            catch (Exception ex)
            {
                Console.WriteLine($"[DEBUG] Discovery Exception: {ex.Message}");
                DiscoveryStatus = $"Error: {ex.Message}";
            }
            finally
            {
                IsScanning = false;
            }
        }

        [RelayCommand]
        public async Task AdoptDevice(DiscoveredDevice device)
        {
            if (device == null || device.IsClaimed) return;

            // Debug Logging
            if (device.RtspUris != null)
            {
                Console.WriteLine($"[DEBUG] Adopting {device.IpAddress}. Raw URIs: {string.Join(", ", device.RtspUris)}");
            }
            else
            {
                Console.WriteLine($"[DEBUG] Adopting {device.IpAddress}. No URIs found.");
            }

            var parsedUrl = ParseRtspUrl(device.RtspUris);
            Console.WriteLine($"[DEBUG] Parsed URL: {parsedUrl}");

            var fallbackUrl = GetFallbackUrl(device);
            Console.WriteLine($"[DEBUG] Fallback URL: {fallbackUrl}");

            var finalUrl = !string.IsNullOrEmpty(parsedUrl) ? parsedUrl : fallbackUrl;
            Console.WriteLine($"[DEBUG] Final URL to use: {finalUrl}");

            var cam = new CameraModel
            {
                Name = !string.IsNullOrWhiteSpace(device.Manufacturer) ? $"{device.Manufacturer} {device.Model}" : "New ONVIF Camera",
                IpAddress = device.IpAddress,
                Model = device.Model,
                RtspUrl = finalUrl, 
                SiteId = DEFAULT_SITE_ID,
                IsEnabled = true
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
                // System.Windows.MessageBox.Show("Device added to inventory.", "Success", MessageBoxButton.OK, MessageBoxImage.Information);
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

        private string ParseRtspUrl(System.Collections.Generic.IList<string>? uris)
        {
            if (uris == null || uris.Count == 0) return "";
            
            // Try to find ANY valid RTSP url
            foreach(var raw in uris)
            {
                if (string.IsNullOrWhiteSpace(raw)) continue;
                
                // If contains pipe, split
                if (raw.Contains("|"))
                {
                    var parts = raw.Split('|');
                    if (parts.Length > 1 && parts[1].StartsWith("rtsp", System.StringComparison.OrdinalIgnoreCase)) 
                        return parts[1];
                }
                // If matches rtsp://
                if (raw.StartsWith("rtsp", System.StringComparison.OrdinalIgnoreCase))
                    return raw;
            }
            return "";
        }

        private string GetFallbackUrl(DiscoveredDevice device)
        {
            var ip = device.IpAddress;
            if (string.IsNullOrEmpty(ip)) return "";
            
            // Manual overrides for known tricky brands
            // But since backend has a fuzzer, we should trust backend results first.
            // If we are here, backend returned NO URIs.
            
            return $"rtsp://{ip}:554/stream"; // Default to generic /stream if unknown
        }
    }
}
