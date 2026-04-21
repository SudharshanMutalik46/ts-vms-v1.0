using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.ObjectModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;
using TSVmsDesktop.Views;

namespace TSVmsDesktop.ViewModels
{
    public partial class OnvifDiscoveryViewModel : ObservableObject
    {
        private readonly OnvifService _service;
        private readonly CameraService _camService; 
        private readonly CredentialService _credService; 
        private readonly MediaService _mediaService;
        private readonly VideoService _videoService;
        private readonly MainViewModel _mainViewModel;
        private const string DEFAULT_SITE_ID = "00000000-0000-0000-0000-000000000001";

        private string? _currentRunId; 

        [ObservableProperty] private string _discoveryStatus = "Ready to Scan";
        [ObservableProperty] private bool _isScanning;
        [ObservableProperty] private ObservableCollection<DiscoveredDevice> _devices = new();

        public OnvifDiscoveryViewModel(OnvifService service, CameraService camService, CredentialService credService, MediaService mediaService, VideoService videoService, MainViewModel mainViewModel)
        {
            _service = service;
            _camService = camService; 
            _credService = credService;
            _mediaService = mediaService;
            _videoService = videoService;
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
                    finalRtspUrl = NormalizeRtspUrl(ParseRtspUrl(updatedDevice.RtspUris));
                    // Update displayed row with fresh probe metadata
                    device.Name = updatedDevice.Name;
                    device.Manufacturer = updatedDevice.Manufacturer;
                    device.Model = updatedDevice.Model;
                    device.RtspUris = updatedDevice.RtspUris;
                    device.MediaProfiles = updatedDevice.MediaProfiles;
                    device.HasAudio = updatedDevice.HasAudio;
                    device.Ptz = updatedDevice.Ptz;
                    device.PtzSupported = updatedDevice.PtzSupported;
                }
            }

            // 4. BLOCK NON-ONVIF CAMERAS (Strict ONVIF Requirement)
            if (string.IsNullOrEmpty(finalRtspUrl))
            {
                if (await ShowManualFallbackAsync(device)) return;
                DiscoveryStatus = "ONVIF Handshake Failed.";
                return;
            }

            // 4.5 Strict stream auth check (prevents adding camera with wrong password)
            var rtspProbe = await _videoService.CanOpenRtspWithCredentialsAsync(finalRtspUrl, dialog.Username, dialog.Password);
            if (!rtspProbe.Success)
            {
                DiscoveryStatus = "Adoption failed: invalid credentials/stream.";
                ShowInfo("Authentication Failed", "Wrong username/password or stream is not accessible.");
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
                // 6. Save credentials and validate stream before finalizing adoption
                await Task.Delay(400);
                var addedCam = _camService.AllCameras.FirstOrDefault(c => c.IpAddress == cam.IpAddress && c.Name == cam.Name);

                if (addedCam == null)
                {
                    DiscoveryStatus = "Adoption failed: camera record not found after create.";
                    ShowInfo("Adoption Error", "Camera was created but could not be verified. Please retry.");
                    return;
                }

                bool credSaved = true;
                if (!string.IsNullOrWhiteSpace(dialog.Username))
                {
                    credSaved = await _credService.UpdateCredentialsAsync(addedCam.Id, dialog.Username, dialog.Password);
                }

                if (!credSaved)
                {
                    await _camService.DeleteCameraAsync(addedCam.Id);
                    DiscoveryStatus = "Adoption failed: invalid credentials.";
                    ShowInfo("Authentication Failed", "Wrong username/password. Camera was not added.");
                    return;
                }

                var validation = await _mediaService.ValidateRtspAsync(addedCam.Id);
                if (IsValidationAuthFailure(validation))
                {
                    await _camService.DeleteCameraAsync(addedCam.Id);
                    DiscoveryStatus = "Adoption failed: stream validation failed.";
                    ShowInfo("Adoption Failed", "Camera authentication/stream validation failed. Camera was not added.");
                    return;
                }

                // Keep camera on pending/transient validation responses to avoid false negatives.
                if (!IsValidationAcceptable(validation))
                {
                    DiscoveryStatus = "Camera adopted; stream validation is still in progress.";
                }

                // 7. Update UI
                device.IsClaimed = true;
                var index = Devices.IndexOf(device);
                if (index != -1) Devices[index] = device;

                DiscoveryStatus = "Camera Adopted (ONVIF)!";
                
                await ShowAdoptionPreviewAndNavigateAsync(addedCam, device, finalRtspUrl);
            }
            else
            {
                ShowInfo("Adoption Error", "Failed to add ONVIF device to the server.");
                DiscoveryStatus = "Adoption Failed.";
            }
        }

        private async Task SaveManualCamera(DiscoveredDevice device, string rtspUrl, string username, string password)
        {
            rtspUrl = NormalizeRtspUrl(rtspUrl);
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

            var rtspProbe = await _videoService.CanOpenRtspWithCredentialsAsync(rtspUrl, username, password);
            if (!rtspProbe.Success)
            {
                ShowInfo("Authentication Failed", "Wrong username/password or stream is not accessible.");
                DiscoveryStatus = "Adoption failed: invalid RTSP/credentials.";
                return;
            }
            
            if (_camService.AllCameras.Any(c => c.IpAddress == cam.IpAddress))
            {
                 System.Windows.MessageBox.Show($"Camera with IP {cam.IpAddress} is already registered.", "Duplicate Camera", MessageBoxButton.OK, MessageBoxImage.Warning);
                 return;
            }

            bool success = await _camService.CreateCameraAsync(cam);

            if (success)
            {
                await Task.Delay(400);
                var addedCam = _camService.AllCameras.FirstOrDefault(c => c.IpAddress == cam.IpAddress && c.Name == cam.Name);

                if (addedCam == null)
                {
                    ShowInfo("Adoption Error", "Camera was created but could not be verified. Please retry.");
                    DiscoveryStatus = "Adoption failed.";
                    return;
                }

                bool credSaved = true;
                if (!string.IsNullOrWhiteSpace(username))
                {
                    credSaved = await _credService.UpdateCredentialsAsync(addedCam.Id, username, password);
                }

                if (!credSaved)
                {
                    await _camService.DeleteCameraAsync(addedCam.Id);
                    ShowInfo("Authentication Failed", "Wrong username/password. Camera was not added.");
                    DiscoveryStatus = "Adoption failed: invalid credentials.";
                    return;
                }

                var validation = await _mediaService.ValidateRtspAsync(addedCam.Id);
                if (IsValidationAuthFailure(validation))
                {
                    await _camService.DeleteCameraAsync(addedCam.Id);
                    ShowInfo("Validation Failed", "RTSP authentication failed. Check username/password and try again.");
                    DiscoveryStatus = "Adoption failed: invalid RTSP/credentials.";
                    return;
                }
                
                if (!IsValidationAcceptable(validation))
                {
                    DiscoveryStatus = "Camera adopted; stream validation is still in progress.";
                }

                device.IsClaimed = true;
                var index = Devices.IndexOf(device);
                if (index != -1) Devices[index] = device;

                DiscoveryStatus = "Camera Adopted (Manual)!";
                await ShowAdoptionPreviewAndNavigateAsync(addedCam, device, rtspUrl);
            }
            else
            {
                ShowInfo("Adoption Error", "Failed to add camera to the server.");
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
            
            string fallbackUrl = "";

            foreach(var raw in uris)
            {
                if (string.IsNullOrWhiteSpace(raw)) continue;
                
                string url = raw;
                if (raw.Contains("|"))
                {
                    var parts = raw.Split('|');
                    if (parts.Length > 1 && parts[1].StartsWith("rtsp", System.StringComparison.OrdinalIgnoreCase)) 
                        url = parts[1];
                }

                if (url.StartsWith("rtsp", System.StringComparison.OrdinalIgnoreCase))
                {
                    url = NormalizeRtspUrl(url);
                    // Save the very first URL as a fallback just in case a sub-stream isn't found
                    if (string.IsNullOrEmpty(fallbackUrl)) fallbackUrl = url;

                    // Search the URL string for common sub-stream identifiers
                    string lowerUrl = url.ToLower();
                    if (lowerUrl.Contains("profiletoken=profile_1") || 
                        lowerUrl.Contains("profile=1") ||
                        lowerUrl.Contains("subtype=1") || 
                        lowerUrl.Contains("stream=1") || 
                        lowerUrl.Contains("channels/102") || 
                        lowerUrl.Contains("sub"))
                    {
                        return url; // Found the sub-stream! Return it immediately.
                    }
                }
            }
            
            // If no keywords matched, try returning the second URL in the list 
            // (ONVIF cameras almost always list Main stream first, Sub stream second)
            var parsedUrls = uris.Where(u => u.ToLower().Contains("rtsp")).ToList();
            if (parsedUrls.Count > 1) 
            {
                string secondUrl = parsedUrls[1].Contains("|") ? parsedUrls[1].Split('|')[1] : parsedUrls[1];
                return NormalizeRtspUrl(secondUrl); 
            }

            return NormalizeRtspUrl(fallbackUrl);
        }

        private static bool IsValidationAcceptable(RtspValidationResult? validation)
        {
            if (validation == null) return true;
            var status = (validation.Status ?? "").Trim().ToLowerInvariant();
            if (validation.Success) return true;
            return status == "ok" || status == "healthy" || status == "valid" || status == "success" || status == "queued" || status == "pending";
        }

        private static bool IsValidationAuthFailure(RtspValidationResult? validation)
        {
            if (validation == null) return false;
            var status = (validation.Status ?? "").ToLowerInvariant();
            var error = (validation.Error ?? "").ToLowerInvariant();
            return status.Contains("auth") ||
                   status.Contains("unauthorized") ||
                   status.Contains("forbidden") ||
                   status.Contains("credentials") ||
                   error.Contains("auth") ||
                   error.Contains("unauthorized") ||
                   error.Contains("forbidden") ||
                   error.Contains("credentials");
        }

        private static void ShowInfo(string title, string message)
        {
            var dialog = new InfoDialogWindow(title, message)
            {
                Owner = System.Windows.Application.Current.MainWindow
            };
            dialog.ShowDialog();
        }

        private async Task ShowAdoptionPreviewAndNavigateAsync(CameraModel? addedCam, DiscoveredDevice device, string finalRtspUrl)
        {
            finalRtspUrl = NormalizeRtspUrl(finalRtspUrl);
            string previewUser = "";
            string previewPass = "";
            try
            {
                if (addedCam != null && !string.IsNullOrWhiteSpace(addedCam.Id))
                {
                    var c = await _credService.GetCredentialsAsync(addedCam.Id);
                    previewUser = c?.Username ?? "";
                    previewPass = c?.Password ?? "";
                }
            }
            catch { }

            string snapshotPath = "";
            try
            {
                if (addedCam != null && !string.IsNullOrWhiteSpace(addedCam.Id))
                {
                    string dir = System.IO.Path.Combine(System.IO.Path.GetTempPath(), "tsvms_preview");
                    System.IO.Directory.CreateDirectory(dir);
                    snapshotPath = System.IO.Path.Combine(dir, $"{addedCam.Id}.jpg");
                    await _videoService.DownloadSnapshotAsync(addedCam.Id, snapshotPath);
                }
            }
            catch
            {
                snapshotPath = "";
            }

            string details =
                $"Name: {device.CameraNameDisplay}\n" +
                $"IP: {device.IpAddress}\n" +
                $"Manufacturer: {device.Manufacturer}\n" +
                $"Model: {device.Model}\n" +
                $"RTSP: {(string.IsNullOrWhiteSpace(finalRtspUrl) ? "-" : finalRtspUrl)}\n" +
                $"Video Codec: {device.EncodingDisplay}\n" +
                $"Audio: {device.AudioDisplay}\n" +
                $"PTZ: {device.PtzDisplay}\n" +
                $"Resolution: {device.ResolutionDisplay}\n" +
                $"Bitrate: {device.BitrateDisplay} kbps";

            var preview = new AdoptionPreviewWindow(
                details,
                finalRtspUrl,
                string.IsNullOrWhiteSpace(snapshotPath) ? null : snapshotPath,
                previewUser,
                previewPass,
                async (newUrl) => await RetryAdoptionUrlAsync(addedCam, NormalizeRtspUrl(newUrl)))
            {
                Owner = System.Windows.Application.Current.MainWindow
            };
            var result = preview.ShowDialog();
            if (result == true && preview.OpenLiveAfterClose)
            {
                _mainViewModel.NavigateToLive();
            }
        }

        private async Task<AdoptionPreviewWindow.RetryResult> RetryAdoptionUrlAsync(CameraModel? cam, string newUrl)
        {
            newUrl = NormalizeRtspUrl(newUrl);
            if (cam == null || string.IsNullOrWhiteSpace(cam.Id))
            {
                return new AdoptionPreviewWindow.RetryResult { Success = false, Message = "Camera is unavailable for retry." };
            }

            try
            {
                var creds = await _credService.GetCredentialsAsync(cam.Id);
                ParseRtspCredentials(newUrl, out var sanitizedUrl, out var urlUser, out var urlPass);
                string probeUser = !string.IsNullOrWhiteSpace(urlUser) ? urlUser : (creds?.Username ?? "");
                string probePass = !string.IsNullOrWhiteSpace(urlUser) ? urlPass : (creds?.Password ?? "");

                var probe = await _videoService.CanOpenRtspWithCredentialsAsync(sanitizedUrl, probeUser, probePass);
                if (!probe.Success)
                {
                    return new AdoptionPreviewWindow.RetryResult
                    {
                        Success = false,
                        Message = "Retry failed: invalid URL or credentials."
                    };
                }

                var latest = await _camService.GetCameraAsync(cam.Id);
                if (latest == null)
                {
                    return new AdoptionPreviewWindow.RetryResult { Success = false, Message = "Retry failed: camera not found." };
                }

                latest.RtspUrl = sanitizedUrl;
                bool updated = await _camService.UpdateCameraAsync(latest);
                if (!updated)
                {
                    return new AdoptionPreviewWindow.RetryResult { Success = false, Message = "Retry failed: could not update camera URL." };
                }

                if (!string.IsNullOrWhiteSpace(probeUser))
                {
                    await _credService.UpdateCredentialsAsync(cam.Id, probeUser, probePass);
                }

                await _mediaService.UpdateManualStreamUrlsAsync(cam.Id, sanitizedUrl, sanitizedUrl);
                await _camService.ManualHealthRecheckAsync(cam.Id);

                string snap = "";
                for (int attempt = 0; attempt < 3; attempt++)
                {
                    try
                    {
                        string dir = System.IO.Path.Combine(System.IO.Path.GetTempPath(), "tsvms_preview");
                        System.IO.Directory.CreateDirectory(dir);
                        snap = System.IO.Path.Combine(dir, $"{cam.Id}.jpg");
                        await _videoService.DownloadSnapshotAsync(cam.Id, snap);
                        if (System.IO.File.Exists(snap) && new System.IO.FileInfo(snap).Length > 1024)
                        {
                            break;
                        }
                    }
                    catch
                    {
                        snap = "";
                    }
                    await Task.Delay(700);
                }

                return new AdoptionPreviewWindow.RetryResult
                {
                    Success = true,
                    Message = string.IsNullOrWhiteSpace(snap)
                        ? "URL updated and stream verified. Preview still pending."
                        : "URL updated and stream verified.",
                    SnapshotPath = string.IsNullOrWhiteSpace(snap) ? null : snap,
                    RtspUrl = sanitizedUrl,
                    Username = probeUser,
                    Password = probePass
                };
            }
            catch (Exception ex)
            {
                return new AdoptionPreviewWindow.RetryResult
                {
                    Success = false,
                    Message = $"Retry failed: {ex.Message}"
                };
            }
        }

        private static void ParseRtspCredentials(string url, out string sanitizedUrl, out string username, out string password)
        {
            username = "";
            password = "";
            sanitizedUrl = NormalizeRtspUrl(url);

            try
            {
                if (!Uri.TryCreate(sanitizedUrl, UriKind.Absolute, out var uri))
                    return;

                if (string.IsNullOrWhiteSpace(uri.UserInfo))
                    return;

                var parts = uri.UserInfo.Split(':', 2);
                username = Uri.UnescapeDataString(parts[0]);
                password = parts.Length > 1 ? Uri.UnescapeDataString(parts[1]) : "";

                var builder = new UriBuilder(uri)
                {
                    UserName = "",
                    Password = ""
                };
                sanitizedUrl = builder.Uri.ToString();
            }
            catch
            {
                username = "";
                password = "";
            }
        }

        private static string NormalizeRtspUrl(string url)
        {
            if (string.IsNullOrWhiteSpace(url)) return "";
            string n = System.Net.WebUtility.HtmlDecode(url.Trim());
            if (!n.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase) &&
                !n.StartsWith("rtsps://", StringComparison.OrdinalIgnoreCase))
            {
                n = "rtsp://" + n.TrimStart('/');
            }
            return n;
        }
    }
}


