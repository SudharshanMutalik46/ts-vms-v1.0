using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.ObjectModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class OnvifDiscoveryViewModel : ObservableObject
    {
        private readonly OnvifService _service;
        private readonly CameraService _camService; 
        private readonly CredentialService _credService; 
        private readonly MainViewModel _mainViewModel;
        private const string DEFAULT_SITE_ID = "00000000-0000-0000-0000-000000000001";

        private string? _currentRunId; 

        [ObservableProperty] private string _discoveryStatus = "Ready to Scan";
        [ObservableProperty] private bool _isScanning;
        [ObservableProperty] private ObservableCollection<DiscoveredDevice> _devices = new();

        public OnvifDiscoveryViewModel(OnvifService service, CameraService camService, CredentialService credService, MainViewModel mainViewModel)
        {
            _service = service;
            _camService = camService; 
            _credService = credService;
            _mainViewModel = mainViewModel;
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
                _currentRunId = await _service.StartDiscoveryAsync();
                
                if(_currentRunId != null)
                {
                    bool isComplete = false;
                    for (int i = 0; i < 30; i++) 
                    {
                        await Task.Delay(1000);
                        var runStatus = await _service.GetRunStatusAsync(_currentRunId);
                        if (runStatus != null && (runStatus.Status == "completed" || runStatus.Status == "failed"))
                        {
                            isComplete = true;
                            break;
                        }
                        DiscoveryStatus = $"Scanning network... {i + 1}s";
                    }
                    
                    if (!isComplete)
                    {
                         DiscoveryStatus = "Scan timed out (Backend still running).";
                    }
                    else
                    {
                        DiscoveryStatus = "Retrieving results...";
                        var results = await _service.GetDiscoveredDevicesAsync(_currentRunId);

                        await System.Windows.Application.Current.Dispatcher.InvokeAsync(() => 
                        {
                            Devices.Clear();
                            if (results.Count == 0)
                            {
                                 DiscoveryStatus = "No devices found.";
                            }
                            else
                            {
                                var existingIps = _camService.AllCameras.Select(c => c.IpAddress).ToHashSet();
                                int count = 0;
                                foreach (var d in results) 
                                {
                                    if (!existingIps.Contains(d.IpAddress))
                                    {
                                        Devices.Add(d);
                                        count++;
                                    }
                                    else 
                                    {
                                        System.Diagnostics.Debug.WriteLine($"[Discovery] Hiding already adopted device: {d.IpAddress}");
                                    }
                                }
                                DiscoveryStatus = $"Scan complete. Found {count} new devices (hidden {results.Count - count} already adopted).";
                            }
                        });
                    }
                }
                else
                {
                    DiscoveryStatus = "Failed to start discovery run.";
                }
            }
            catch (Exception ex)
            {
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

            // 1. Prompt the user for credentials
            var dialog = new Views.CredentialPromptWindow(device.IpAddress)
            {
                Owner = System.Windows.Application.Current.MainWindow
            };
            
            if (dialog.ShowDialog() != true) return; 

            DiscoveryStatus = $"Negotiating with {device.IpAddress} via ONVIF...";
            string finalRtspUrl = "";
            bool onvifSuccess = false;

            // 2. ATTEMPT ONVIF PROBE
            try 
            {
                var credId = await _service.SetOnvifCredentialsAsync(dialog.Username, dialog.Password);
                if (!string.IsNullOrEmpty(credId))
                {
                    onvifSuccess = await _service.ProbeDeviceAsync(device.Id, credId);
                }
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"[Adopt] ONVIF Probe Failed: {ex.Message}");
            }

            if (!onvifSuccess)
            {
                if (await ShowManualFallbackAsync(device)) return;
                DiscoveryStatus = "Adoption Cancelled.";
                return;
            }

            // 3. EXTRACT ONVIF URL
            if (onvifSuccess && !string.IsNullOrEmpty(_currentRunId))
            {
                // Re-fetch the device to get the RTSP URIs populated by the successful probe
                var updatedDevices = await _service.GetDiscoveredDevicesAsync(_currentRunId);
                var updatedDevice = updatedDevices.FirstOrDefault(d => d.Id == device.Id);
                
                if (updatedDevice != null && updatedDevice.RtspUris != null)
                {
                    finalRtspUrl = ParseRtspUrl(updatedDevice.RtspUris);
                }
            }

            // 4. BLOCK NON-ONVIF CAMERAS (Strict ONVIF Requirement)
            if (string.IsNullOrEmpty(finalRtspUrl))
            {
                if (await ShowManualFallbackAsync(device)) return;
                DiscoveryStatus = "ONVIF Handshake Failed.";
                return;
            }

            // 5. Create the Camera Record
            DiscoveryStatus = "Saving ONVIF camera to server...";
            var cam = new CameraModel
            {
                Name = !string.IsNullOrWhiteSpace(device.Manufacturer) ? $"{device.Manufacturer} {device.Model}" : "New ONVIF Camera",
                IpAddress = device.IpAddress,
                Model = device.Model,
                RtspUrl = finalRtspUrl, 
                SiteId = DEFAULT_SITE_ID,
                IsEnabled = true
            };
            
            // Final check just in case scan results were stale
            if (_camService.AllCameras.Any(c => c.IpAddress == cam.IpAddress))
            {
                 System.Windows.MessageBox.Show($"Camera with IP {cam.IpAddress} is already registered.", "Duplicate Camera", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Warning);
                 DiscoveryStatus = "Adoption Cancelled (Duplicate).";
                 return;
            }

            bool success = await _camService.CreateCameraAsync(cam);
            
            if (success) 
            {
                // 6. Save credentials to backend
                await Task.Delay(500);
                var addedCam = _camService.AllCameras.FirstOrDefault(c => c.IpAddress == cam.IpAddress && c.Name == cam.Name);
                
                if (addedCam != null && !string.IsNullOrWhiteSpace(dialog.Username))
                {
                    await _credService.UpdateCredentialsAsync(addedCam.Id, dialog.Username, dialog.Password);
                }

                // 7. Update UI
                device.IsClaimed = true;
                var index = Devices.IndexOf(device);
                if (index != -1) Devices[index] = device;

                DiscoveryStatus = "Camera Adopted (v-ONVIF)!";
                
                // Auto-navigate to Live View
                _mainViewModel.NavigateToLive();
            }
            else
            {
                System.Windows.MessageBox.Show("Failed to add ONVIF device to the server.", "Adoption Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
                DiscoveryStatus = "Adoption Failed.";
            }
        }

        private async Task SaveManualCamera(DiscoveredDevice device, string rtspUrl, string username, string password)
        {
            DiscoveryStatus = "Saving Manual camera to server...";
            var cam = new CameraModel
            {
                Name = !string.IsNullOrWhiteSpace(device.Manufacturer) ? $"{device.Manufacturer} {device.Model} (Manual)" : $"Manual Camera {device.IpAddress}",
                IpAddress = device.IpAddress,
                Model = device.Model ?? "Manual Entry",
                RtspUrl = rtspUrl, 
                SiteId = DEFAULT_SITE_ID,
                IsEnabled = true
            };
            
            if (_camService.AllCameras.Any(c => c.IpAddress == cam.IpAddress))
            {
                 System.Windows.MessageBox.Show($"Camera with IP {cam.IpAddress} is already registered.", "Duplicate Camera", MessageBoxButton.OK, MessageBoxImage.Warning);
                 return;
            }

            bool success = await _camService.CreateCameraAsync(cam);
            
            if (success) 
            {
                await Task.Delay(500);
                var addedCam = _camService.AllCameras.FirstOrDefault(c => c.IpAddress == cam.IpAddress && c.Name == cam.Name);
                
                if (addedCam != null && !string.IsNullOrWhiteSpace(username))
                {
                    await _credService.UpdateCredentialsAsync(addedCam.Id, username, password);
                }

                device.IsClaimed = true;
                var index = Devices.IndexOf(device);
                if (index != -1) Devices[index] = device;

                DiscoveryStatus = "Camera Adopted (Manual)!";
                _mainViewModel.NavigateToLive();
            }
            else
            {
                System.Windows.MessageBox.Show("Failed to add camera to the server.", "Adoption Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }

        private async Task<bool> ShowManualFallbackAsync(DiscoveredDevice device)
        {
            var result = System.Windows.MessageBox.Show(
                "ONVIF handshake failed (No stream URI found).\n\n" +
                "Would you like to enter the RTSP details manually?",
                "Handshake Failed",
                MessageBoxButton.YesNo,
                MessageBoxImage.Question);

            if (result == MessageBoxResult.Yes)
            {
                var manualDialog = new Views.ManualAdoptionWindow(device.IpAddress)
                {
                    Owner = System.Windows.Application.Current.MainWindow
                };

                if (manualDialog.ShowDialog() == true)
                {
                    await SaveManualCamera(device, manualDialog.Url, manualDialog.Username, manualDialog.Password);
                    return true;
                }
            }
            return false;
        }

        [RelayCommand]
        public void Close()
        {
            _mainViewModel.NavigateToCameras();
        }

        private string ParseRtspUrl(System.Collections.Generic.IList<string>? uris)
        {
            if (uris == null || uris.Count == 0) return "";
            foreach(var raw in uris)
            {
                if (string.IsNullOrWhiteSpace(raw)) continue;
                if (raw.Contains("|"))
                {
                    var parts = raw.Split('|');
                    if (parts.Length > 1 && parts[1].StartsWith("rtsp", System.StringComparison.OrdinalIgnoreCase)) 
                        return parts[1];
                }
                if (raw.StartsWith("rtsp", System.StringComparison.OrdinalIgnoreCase))
                    return raw;
            }
            return "";
        }
    }
}
