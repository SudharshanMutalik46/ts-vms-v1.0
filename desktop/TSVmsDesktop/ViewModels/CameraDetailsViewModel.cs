using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Net.Sockets;
using System.Threading.Tasks;
using System.Windows;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

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

        // Health tab properties
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
            _camService = cam; _credService = cred; _mediaService = media; _mainViewModel = mainViewModel;
        }

        public async void Load(string camId)
        {
            _cameraId = camId;
            var cam = await _camService.GetCameraAsync(camId);
            
            if (cam == null)
            {
                 // Orphaned: Camera no longer exists
                 System.Windows.MessageBox.Show("Camera no longer exists.", "Error", MessageBoxButton.OK, MessageBoxImage.Warning);
                 Close();
                 return;
            }

            Camera = cam;
            // Clear previous state
            CredUsername = "";
            CredPassword = "";
            Profiles.Clear();
            RtspValidationResult = "";

            // Populate health info
            HealthIpAddress = cam.IpAddress;
            HealthPort = cam.Port > 0 ? cam.Port.ToString() : "554";
            HealthRtspUrl = cam.EffectiveRtspUrl;
            HealthIsEnabled = cam.IsEnabled ? "Yes" : "No";

            await FetchProfiles();
            
            // Auto-sync if no profiles found OR if they seem to be "old" data (no audio codec info)
            bool needsSync = Profiles.Count == 0 || System.Linq.Enumerable.All(Profiles, p => string.IsNullOrEmpty(p.AudioCodec) || p.AudioCodec == "—");
            
            if (needsSync)
            {
                await SyncProfiles();
            }
            await CheckHealth();
        }

        [RelayCommand]
        public async Task SaveCredentials()
        {
            if(string.IsNullOrWhiteSpace(CredUsername) && string.IsNullOrWhiteSpace(CredPassword))
            {
                 System.Windows.MessageBox.Show("Please enter username and password.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                 return;
            }

            try
            {
                bool success = await _credService.UpdateCredentialsAsync(_cameraId, CredUsername, CredPassword);
                if (success) 
                {
                    System.Windows.MessageBox.Show("Credentials saved successfully.", "Success", MessageBoxButton.OK, MessageBoxImage.Information);
                    
                    // Trigger auto-discovery & selection of profiles to ensure a selection exists for validation
                    RtspValidationResult = "Discovering profiles...";
                    await _mediaService.SelectProfilesAsync(_cameraId, "", ""); // Triggers backend auto-select
                    
                    await FetchProfiles();

                    // Auto-validate RTSP
                    await ValidateRtsp();
                }
                else
                {
                    System.Windows.MessageBox.Show("Failed to save credentials.", "Error", MessageBoxButton.OK, MessageBoxImage.Error);
                }
            }
            catch (System.Exception ex)
            {
                RtspValidationResult = $"Error: {ex.Message}";
                System.Windows.MessageBox.Show($"Server Error while saving: {ex.Message}\nCheck api_debug_log.txt for details.", "Server Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public async Task SyncProfiles()
        {
            RtspValidationResult = "Syncing profiles from camera...";
            try
            {
                // Trigger backend re-discovery (POST)
                bool success = await _mediaService.SelectProfilesAsync(_cameraId, "", "");
                if (success)
                {
                    RtspValidationResult = "Sync successful. Loading...";
                    await FetchProfiles();
                }
                else
                {
                    RtspValidationResult = "Sync failed. Check credentials.";
                }
            }
            catch (System.Exception ex)
            {
                RtspValidationResult = $"Sync Error: {ex.Message}";
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
                foreach(var p in list) 
                {
                    if (selection != null)
                    {
                        if (p.Token == selection.MainProfileToken) p.TypeDisplay = "MAIN";
                        else if (p.Token == selection.SubProfileToken) p.TypeDisplay = "SUB";
                        else p.TypeDisplay = "—";
                    }
                    Profiles.Add(p);
                }
                
                if (Profiles.Count == 0 && !string.IsNullOrWhiteSpace(RtspValidationResult) && !RtspValidationResult.Contains("Syncing"))
                {
                     RtspValidationResult = "No profiles found. Try Sync.";
                }
                else if (string.IsNullOrWhiteSpace(RtspValidationResult) || RtspValidationResult.Contains("Loaded"))
                {
                    RtspValidationResult = $"Loaded {list.Count} profiles.";
                }
            }
            catch (System.Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"FetchProfiles Error: {ex.Message}");
                RtspValidationResult = $"Fetch Error: {ex.Message}";
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
                        await FetchProfiles(); // Refresh list if it was empty
                    }
                    else if (res.Status == "queued")
                    {
                        RtspValidationResult = "Validation Started (Queued)...";
                        _ = PollValidationResult(); // Run in background
                    }
                    else if (res.Error != null && res.Error.Contains("credentials not found"))
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
            catch (System.Exception ex)
            {
                if (ex.Message.Contains("InternalServerError"))
                    RtspValidationResult = "Server Error: Check Credentials & Camera Status";
                else
                    RtspValidationResult = $"Error: {ex.Message}";
            }
        }

        private async Task PollValidationResult()
        {
            // Poll for up to 30 seconds
            for (int i = 0; i < 15; i++)
            {
                await Task.Delay(2000);
                try
                {
                    var info = await _mediaService.GetMediaInfoAsync(_cameraId);
                    if (info?.ValidationResults != null && info.ValidationResults.Count > 0)
                    {
                        // Check main variant for result
                        var main = info.ValidationResults.Find(r => r.Variant == "main");
                        if (main != null && main.Status != "queued" && main.Status != "pending")
                        {
                            if (main.Status == "success" || main.Status == "valid")
                                RtspValidationResult = $"Success! Latency: {main.LatencyMs}ms";
                            else
                                RtspValidationResult = $"Failed: {main.Status} ({main.Error})";
                            
                            await FetchProfiles(); // Refresh profiles now that discovery is done
                            return;
                        }
                    }
                }
                catch (System.Exception ex)
                {
                    System.Diagnostics.Debug.WriteLine($"Polling error: {ex.Message}");
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
                System.Diagnostics.Debug.WriteLine($"Health check error: {ex.Message}");
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
