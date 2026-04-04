using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Linq;
using System.Net.Sockets;
using System.Threading.Tasks;
using System.Windows;
using Application = System.Windows.Application;
using MessageBox = System.Windows.MessageBox;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;
using TSVmsDesktop.Views;

namespace TSVmsDesktop.ViewModels
{
    public partial class CameraDetailsViewModel : ObservableObject
    {
        private readonly CameraService _camService;
        private readonly CredentialService _credService;
        private readonly MediaService _mediaService;
        private readonly MainViewModel _mainViewModel;
        private string _cameraId = "";

        [ObservableProperty] private CameraModel _camera = new();
        [ObservableProperty] private string _credUsername = "";
        [ObservableProperty] private string _credPassword = "";
        [ObservableProperty] private ObservableCollection<MediaProfile> _profiles = new();
        [ObservableProperty] private MediaProfile _selectedMainProfile = new();
        [ObservableProperty] private MediaProfile _selectedSubProfile = new();
        [ObservableProperty] private string _rtspValidationResult = "";
        [ObservableProperty] private string _editableCameraName = "";
        [ObservableProperty] private bool _isEditingName;

        [ObservableProperty] private string _healthStatus = "Unknown";
        [ObservableProperty] private string _healthStatusColor = "#999";
        [ObservableProperty] private string _healthLatency = "—";
        [ObservableProperty] private string _healthLastChecked = "Never";
        [ObservableProperty] private string _healthIpAddress = "—";
        [ObservableProperty] private string _healthPort = "—";
        [ObservableProperty] private string _healthRtspUrl = "—";
        [ObservableProperty] private string _healthIsEnabled = "—";
        [ObservableProperty] private bool _isHealthChecking;

        public CameraDetailsViewModel(CameraService cam, CredentialService cred, MediaService media, MainViewModel mainViewModel)
        {
            _camService = cam;
            _credService = cred;
            _mediaService = media;
            _mainViewModel = mainViewModel;
        }

        public async void Load(string camId)
        {
            _cameraId = camId;
            var cam = await _camService.GetCameraAsync(camId);
            if (cam == null)
            {
                MessageBox.Show("Camera no longer exists.", "Error", MessageBoxButton.OK, MessageBoxImage.Warning);
                Close();
                return;
            }

            Camera = cam;
            EditableCameraName = cam.Name;
            IsEditingName = false;
            CredUsername = "";
            CredPassword = "";
            Profiles.Clear();
            RtspValidationResult = "";
            SelectedMainProfile = new MediaProfile();
            SelectedSubProfile = new MediaProfile();

            HealthIpAddress = cam.IpAddress;
            HealthPort = cam.Port > 0 ? cam.Port.ToString() : "554";
            HealthRtspUrl = cam.EffectiveRtspUrl;
            HealthIsEnabled = cam.IsEnabled ? "Yes" : "No";

            RtspValidationResult = "Click Refresh Profiles to load media details.";
            await CheckHealth();
        }

        [RelayCommand]
        public async Task SaveCredentials()
        {
            if (string.IsNullOrWhiteSpace(CredUsername) && string.IsNullOrWhiteSpace(CredPassword))
            {
                MessageBox.Show("Please enter username and password.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            try
            {
                bool success = await _credService.UpdateCredentialsAsync(_cameraId, CredUsername, CredPassword);
                if (success)
                {
                    MessageBox.Show("Credentials saved successfully.", "Success", MessageBoxButton.OK, MessageBoxImage.Information);
                    RtspValidationResult = "Discovering profiles...";
                    await _mediaService.SelectProfilesAsync(_cameraId, "", "");
                    await FetchProfiles();
                    await ValidateRtsp();
                }
                else
                {
                    MessageBox.Show("Failed to save credentials.", "Error", MessageBoxButton.OK, MessageBoxImage.Error);
                }
            }
            catch (Exception ex)
            {
                RtspValidationResult = $"Error: {ex.Message}";
                MessageBox.Show($"Server Error while saving: {ex.Message}\nCheck api_debug_log.txt for details.", "Server Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public async Task OpenRtspEditor()
        {
            if (Camera == null)
                return;

            if (Profiles.Count == 0)
            {
                await FetchProfiles();
            }

            var dialog = new RtspEditorWindow(
                Camera.RtspUrl ?? "",
                Camera.Port > 0 ? Camera.Port : 554,
                CredUsername,
                CredPassword,
                Profiles,
                SelectedMainProfile?.Token ?? "",
                SelectedSubProfile?.Token ?? "")
            {
                Owner = Application.Current.MainWindow
            };

            if (dialog.ShowDialog() != true)
                return;

            await ApplyRtspEditorResult(dialog);
        }

        [RelayCommand]
        public void StartEditName()
        {
            EditableCameraName = Camera.Name;
            IsEditingName = true;
        }

        [RelayCommand]
        public void CancelEditName()
        {
            EditableCameraName = Camera.Name;
            IsEditingName = false;
        }

        [RelayCommand]
        public async Task SaveName()
        {
            var newName = EditableCameraName?.Trim() ?? "";
            if (string.IsNullOrWhiteSpace(newName))
            {
                MessageBox.Show("Please enter a camera name.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            if (string.Equals(newName, Camera.Name, StringComparison.Ordinal))
            {
                IsEditingName = false;
                return;
            }

            try
            {
                var updated = new CameraModel
                {
                    Id = Camera.Id,
                    Name = newName,
                    IpAddress = Camera.IpAddress,
                    SiteId = Camera.SiteId,
                    Port = Camera.Port,
                    RtspUrl = Camera.RtspUrl,
                    IsEnabled = Camera.IsEnabled,
                    Model = Camera.Model,
                    Thumbnail = Camera.Thumbnail,
                    Capabilities = Camera.Capabilities
                };

                bool success = await _camService.UpdateCameraAsync(updated);
                if (success)
                {
                    EditableCameraName = newName;
                    var refreshed = await _camService.GetCameraAsync(_cameraId);
                    Camera = refreshed ?? updated;
                    IsEditingName = false;
                    MessageBox.Show("Camera name updated successfully.", "Success", MessageBoxButton.OK, MessageBoxImage.Information);
                }
                else
                {
                    MessageBox.Show("Failed to update camera name.", "Error", MessageBoxButton.OK, MessageBoxImage.Error);
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show($"Failed to update camera name: {ex.Message}", "Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public async Task SyncProfiles()
        {
            RtspValidationResult = "Syncing profiles from camera...";
            try
            {
                bool success = await _mediaService.SelectProfilesAsync(_cameraId, "", "");
                if (success)
                {
                    RtspValidationResult = "Sync successful. Loading...";
                    await FetchProfiles();
                }
                else
                {
                    RtspValidationResult = "Sync failed. Check camera access.";
                }
            }
            catch (Exception ex)
            {
                RtspValidationResult = IsAuthError(ex)
                    ? "Camera media access is not available for this account."
                    : $"Sync Error: {ex.Message}";
            }
        }

        [RelayCommand]
        public async Task FetchProfiles()
        {
            try
            {
                var list = await _mediaService.GetProfilesAsync(_cameraId);
                var info = await _mediaService.GetMediaInfoAsync(_cameraId);
                var selection = info?.Selection;

                Profiles.Clear();
                SelectedMainProfile = new MediaProfile();
                SelectedSubProfile = new MediaProfile();
                foreach (var p in list)
                {
                    if (selection != null)
                    {
                        if (p.Token == selection.MainProfileToken) p.TypeDisplay = "MAIN";
                        else if (p.Token == selection.SubProfileToken) p.TypeDisplay = "SUB";
                        else p.TypeDisplay = "—";
                    }
                    Profiles.Add(p);
                }

                if (selection != null)
                {
                    SelectedMainProfile = Profiles.FirstOrDefault(p => p.Token == selection.MainProfileToken) ?? new MediaProfile();
                    SelectedSubProfile = Profiles.FirstOrDefault(p => p.Token == selection.SubProfileToken) ?? new MediaProfile();
                }

                if (Profiles.Count == 0 && (string.IsNullOrWhiteSpace(RtspValidationResult) || RtspValidationResult.Contains("Loaded", StringComparison.OrdinalIgnoreCase)))
                {
                    RtspValidationResult = "No profiles loaded. Click Refresh Profiles.";
                }
                else if (string.IsNullOrWhiteSpace(RtspValidationResult) || RtspValidationResult.Contains("Loaded", StringComparison.OrdinalIgnoreCase))
                {
                    RtspValidationResult = $"Loaded {list.Count} profiles.";
                }
            }
            catch (Exception ex)
            {
                Debug.WriteLine($"FetchProfiles Error: {ex.Message}");
                RtspValidationResult = IsAuthError(ex)
                    ? "Camera media access is not available for this account."
                    : "Unable to load profiles. Click Refresh Profiles to try again.";
            }
        }

        private async Task ApplyRtspEditorResult(RtspEditorWindow dialog)
        {
            string rtspUrl = dialog.RtspUrl.Trim();
            if (string.IsNullOrWhiteSpace(rtspUrl) || !rtspUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                MessageBox.Show("Please enter a valid RTSP URL.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            var updated = new CameraModel
            {
                Id = Camera.Id,
                Name = Camera.Name,
                IpAddress = Camera.IpAddress,
                SiteId = Camera.SiteId,
                Port = dialog.Port > 0 ? dialog.Port : Camera.Port,
                RtspUrl = rtspUrl,
                IsEnabled = Camera.IsEnabled,
                Model = Camera.Model,
                Thumbnail = Camera.Thumbnail,
                Capabilities = Camera.Capabilities
            };

            try
            {
                bool cameraSaved = await _camService.UpdateCameraAsync(updated);
                if (!cameraSaved)
                {
                    MessageBox.Show("Failed to update camera RTSP URL.", "Error", MessageBoxButton.OK, MessageBoxImage.Error);
                    return;
                }

                bool credSaved = await _credService.UpdateCredentialsAsync(_cameraId, dialog.Username, dialog.Password);
                if (!credSaved)
                {
                    MessageBox.Show("RTSP URL updated, but credentials could not be saved.", "Warning", MessageBoxButton.OK, MessageBoxImage.Warning);
                }

                string mainToken = dialog.SelectedMainProfile?.Token ?? "";
                string subToken = dialog.SelectedSubProfile?.Token ?? "";
                if (!string.IsNullOrWhiteSpace(mainToken) || !string.IsNullOrWhiteSpace(subToken))
                {
                    await _mediaService.SelectProfilesAsync(_cameraId, mainToken, subToken);
                }

                var refreshed = await _camService.GetCameraAsync(_cameraId);
                Camera = refreshed ?? updated;
                HealthRtspUrl = Camera.EffectiveRtspUrl;

                await FetchProfiles();
                await ValidateRtsp();

                MessageBox.Show("RTSP settings updated successfully.", "Success", MessageBoxButton.OK, MessageBoxImage.Information);
            }
            catch (Exception ex)
            {
                MessageBox.Show($"Failed to update RTSP settings: {ex.Message}", "Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public async Task ValidateRtsp()
        {
            RtspValidationResult = "Validating...";
            try
            {
                var res = await _mediaService.ValidateRtspAsync(_cameraId);
                if (res != null)
                {
                    if (res.Success)
                    {
                        RtspValidationResult = $"Success! Latency: {res.LatencyMs}ms";
                        await FetchProfiles();
                    }
                    else if (res.Status == "queued")
                    {
                        RtspValidationResult = "Validation Started (Queued)...";
                        _ = PollValidationResult();
                    }
                    else if (res.Error != null && res.Error.Contains("credentials not found", StringComparison.OrdinalIgnoreCase))
                    {
                        RtspValidationResult = "Missing Credentials. Please Save.";
                    }
                    else
                    {
                        RtspValidationResult = $"Failed: {res.Error}";
                    }
                }
                else
                {
                    RtspValidationResult = "Server Error (Null Response)";
                }
            }
            catch (Exception ex)
            {
                if (IsAuthError(ex))
                    RtspValidationResult = "Camera media access is not available for this account.";
                else if (ex.Message.Contains("InternalServerError", StringComparison.OrdinalIgnoreCase))
                    RtspValidationResult = "Server Error: Check Credentials & Camera Status";
                else
                    RtspValidationResult = $"Error: {ex.Message}";
            }
        }

        private static bool IsAuthError(Exception ex)
        {
            var msg = ex.Message ?? "";
            return msg.Contains("401", StringComparison.OrdinalIgnoreCase)
                || msg.Contains("403", StringComparison.OrdinalIgnoreCase)
                || msg.Contains("unauthorized", StringComparison.OrdinalIgnoreCase)
                || msg.Contains("forbidden", StringComparison.OrdinalIgnoreCase)
                || msg.Contains("ERR_RBAC_DENIED", StringComparison.OrdinalIgnoreCase);
        }

        private async Task PollValidationResult()
        {
            for (int i = 0; i < 15; i++)
            {
                await Task.Delay(2000);
                try
                {
                    var info = await _mediaService.GetMediaInfoAsync(_cameraId);
                    if (info?.ValidationResults != null && info.ValidationResults.Count > 0)
                    {
                        var main = info.ValidationResults.Find(r => r.Variant == "main");
                        if (main != null && main.Status != "queued" && main.Status != "pending")
                        {
                            if (main.Status == "success" || main.Status == "valid")
                                RtspValidationResult = $"Success! Latency: {main.LatencyMs}ms";
                            else
                                RtspValidationResult = $"Failed: {main.Status} ({main.Error})";

                            await FetchProfiles();
                            return;
                        }
                    }
                }
                catch (Exception ex)
                {
                    Debug.WriteLine($"Polling error: {ex.Message}");
                }
            }
            RtspValidationResult = "Validation timed out. Try refreshing profiles.";
        }

        [RelayCommand]
        public async Task CheckHealth()
        {
            IsHealthChecking = true;
            HealthStatus = "Checking...";
            HealthStatusColor = "#999";
            HealthLatency = "...";

            try
            {
                int port = Camera.Port > 0 ? Camera.Port : 554;
                var sw = Stopwatch.StartNew();

                using var client = new TcpClient();
                var connectTask = client.ConnectAsync(Camera.IpAddress, port);

                if (await Task.WhenAny(connectTask, Task.Delay(3000)) == connectTask)
                {
                    await connectTask;
                    sw.Stop();
                    HealthStatus = "Online";
                    HealthStatusColor = "#2E7D32";
                    HealthLatency = $"{sw.ElapsedMilliseconds} ms";
                }
                else
                {
                    sw.Stop();
                    HealthStatus = "Offline (Timeout)";
                    HealthStatusColor = "#C62828";
                    HealthLatency = "> 3000 ms";
                }
            }
            catch (Exception ex)
            {
                HealthStatus = "Offline";
                HealthStatusColor = "#C62828";
                HealthLatency = "N/A";
                Debug.WriteLine($"Health check error: {ex.Message}");
            }
            finally
            {
                HealthLastChecked = DateTime.Now.ToString("HH:mm:ss");
                IsHealthChecking = false;
            }
        }

        [RelayCommand]
        public void Close()
        {
            _mainViewModel.NavigateToCameras();
        }
    }
}
