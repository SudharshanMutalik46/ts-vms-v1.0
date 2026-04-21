using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection;
using System.Collections.ObjectModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows; 
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;
using TSVmsDesktop.Views; 

namespace TSVmsDesktop.ViewModels
{
    public partial class CamerasViewModel : ObservableObject
    {
        private readonly CameraService _cameraService;
        private readonly CredentialService _credentialService;
        private readonly MediaService _mediaService;
        private readonly VideoService _videoService;
        private readonly MainViewModel _mainViewModel;

        // BINDING DIRECTLY to the Service's collection ensures updates are instant
        public ObservableCollection<CameraModel> Cameras => _cameraService.AllCameras;
        
        [ObservableProperty] private bool _isMultiSelectMode;
        [ObservableProperty] private ObservableCollection<CameraModel> _selectedCameras = new();

        public CamerasViewModel(CameraService cameraService, CredentialService credentialService, MediaService mediaService, VideoService videoService, MainViewModel mainViewModel)
        {
            _cameraService = cameraService;
            _credentialService = credentialService;
            _mediaService = mediaService;
            _videoService = videoService;
            _mainViewModel = mainViewModel;
            _ = RefreshList(); // Auto-load and check status
        }

        [RelayCommand]
        public async Task RefreshList() 
        {
            await _cameraService.LoadCamerasAsync();
            _ = CheckAllStatuses();
        }

        private async Task CheckAllStatuses()
        {
            var tasks = Cameras.Select(async c => 
            {
                if (!c.IsEnabled) 
                {
                    c.Status = "Disabled";
                    return;
                }
                
                c.Status = "Checking...";
                try 
                {
                    using var client = new System.Net.Sockets.TcpClient();
                    var connectTask = client.ConnectAsync(c.IpAddress, c.Port > 0 ? c.Port : 554);
                    // 2 second timeout
                    if (await Task.WhenAny(connectTask, Task.Delay(2000)) == connectTask)
                    {
                        await connectTask; // Throw if failed
                        c.Status = "Online";
                    }
                    else
                    {
                        c.Status = "Offline";
                    }
                }
                catch
                {
                    c.Status = "Offline";
                }
            });
            await Task.WhenAll(tasks);
        }

        [RelayCommand]
        public async Task EnableSelected()
        {
            var ids = Cameras.Where(c => c.IsSelected).Select(c => c.Id).ToList();
            if (ids.Any()) 
            {
                await _cameraService.BulkOpAsync(ids, "enable");
                await RefreshList();
            }
        }

        [RelayCommand]
        public async Task DisableSelected()
        {
            var ids = Cameras.Where(c => c.IsSelected).Select(c => c.Id).ToList();
            if (ids.Any()) 
            {
                await _cameraService.BulkOpAsync(ids, "disable");
                await RefreshList();
            }
        }
        
        [RelayCommand]
        public void OpenDetails(CameraModel cam)
        {
            if (cam == null) return;
            ((App)App.Current).Services.GetRequiredService<MainViewModel>().NavigateToCameraDetails(cam.Id);
        }

        [RelayCommand]
        public void OpenDiscovery()
        {
            ((App)App.Current).Services.GetRequiredService<MainViewModel>().NavigateToDiscovery();
        }

        [RelayCommand]
        public async Task AddCamera()
        {
            var dialog = new AddCameraWindow();
            dialog.Owner = System.Windows.Application.Current.MainWindow; 
            
            bool? result = dialog.ShowDialog();

            if (result == true && dialog.CreatedCamera != null)
            {
                var created = dialog.CreatedCamera;
                created.RtspUrl = NormalizeRtspUrl(created.RtspUrl ?? "");

                ParseRtspCredentials(created.RtspUrl, out var sanitizedUrl, out var urlUser, out var urlPass);
                created.RtspUrl = sanitizedUrl;

                string effectiveUser = !string.IsNullOrWhiteSpace(dialog.CameraUsername) ? dialog.CameraUsername : urlUser;
                string effectivePass = !string.IsNullOrWhiteSpace(dialog.CameraUsername) ? dialog.CameraPassword : urlPass;

                if (string.IsNullOrWhiteSpace(created.RtspUrl) ||
                    !TryParseRtspHostPort(created.RtspUrl, out var host, out var port))
                {
                    ShowInfo("Validation", "Please enter a valid RTSP URL.");
                    return;
                }

                if (string.IsNullOrWhiteSpace(created.IpAddress) || created.IpAddress == "Unknown IP")
                {
                    created.IpAddress = host;
                }
                if (created.Port <= 0)
                {
                    created.Port = port;
                }

                await _cameraService.LoadCamerasAsync();
                bool duplicateExists = Cameras.Any(c =>
                {
                    string existingHost = c.IpAddress?.Trim() ?? "";
                    int existingPort = c.Port > 0 ? c.Port : 554;
                    return existingHost.Equals(host, System.StringComparison.OrdinalIgnoreCase) &&
                           existingPort == port;
                });
                if (duplicateExists)
                {
                    ShowInfo("Duplicate Camera", $"Camera already exists for {host}:{port}. Remove old one or use a different stream endpoint.");
                    return;
                }

                if (!string.IsNullOrWhiteSpace(effectiveUser))
                {
                    var preProbe = await _videoService.CanOpenRtspWithCredentialsAsync(
                        created.RtspUrl ?? "",
                        effectiveUser,
                        effectivePass);
                    if (!preProbe.Success)
                    {
                        ShowInfo("Authentication Failed", "Wrong username/password or stream is not accessible.");
                        return;
                    }
                }

                bool createOk = await _cameraService.CreateCameraAsync(created);
                if (!createOk)
                {
                    ShowInfo("Add Camera Failed", "Camera was not added. It may already exist (same IP/port) or the backend rejected this stream.");
                    return;
                }

                // Save credentials if provided
                if (!string.IsNullOrWhiteSpace(effectiveUser))
                {
                    // Wait briefly for list refresh and resolve newly created row
                    await Task.Delay(400);
                    var added = Cameras.FirstOrDefault(c =>
                        (c.IpAddress ?? "").Equals(host, System.StringComparison.OrdinalIgnoreCase) &&
                        (c.Port > 0 ? c.Port : 554) == port);
                    if (added != null && !string.IsNullOrEmpty(added.Id))
                    {
                        var credSaved = await _credentialService.UpdateCredentialsAsync(added.Id, effectiveUser, effectivePass);

                        if (!credSaved)
                        {
                            await _cameraService.DeleteCameraAsync(added.Id);
                            ShowInfo("Authentication Failed", "Wrong username/password. Camera was not added.");
                            return;
                        }

                        var validation = await _mediaService.ValidateRtspAsync(added.Id);
                        if (IsValidationAuthFailure(validation))
                        {
                            await _cameraService.DeleteCameraAsync(added.Id);
                            ShowInfo("Validation Failed", "RTSP authentication failed. Check username/password and try again.");
                            return;
                        }

                        ShowInfo("Success", "Camera added and credentials saved.");
                    }
                }
            }
        }

        [RelayCommand]
        public void DeleteCamera(CameraModel cam)
        {
            if (cam == null) return;

            var confirm = new ConfirmDialogWindow(
                "Confirm Delete",
                $"Are you sure you want to delete '{cam.Name}'?")
            {
                Owner = System.Windows.Application.Current.MainWindow
            };

            if (confirm.ShowDialog() == true)
            {
                _cameraService.RemoveCamera(cam);
            }
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

        private static string NormalizeRtspUrl(string url)
        {
            if (string.IsNullOrWhiteSpace(url)) return "";
            string n = System.Net.WebUtility.HtmlDecode(url.Trim());
            if (!n.StartsWith("rtsp://", System.StringComparison.OrdinalIgnoreCase) &&
                !n.StartsWith("rtsps://", System.StringComparison.OrdinalIgnoreCase))
            {
                n = "rtsp://" + n.TrimStart('/');
            }
            return n;
        }

        private static bool TryParseRtspHostPort(string url, out string host, out int port)
        {
            host = "";
            port = 554;
            try
            {
                if (!System.Uri.TryCreate(url, System.UriKind.Absolute, out var uri))
                    return false;

                host = uri.Host?.Trim() ?? "";
                if (string.IsNullOrWhiteSpace(host))
                    return false;

                port = uri.Port > 0 ? uri.Port : 554;
                return true;
            }
            catch
            {
                return false;
            }
        }

        private static void ParseRtspCredentials(string url, out string sanitizedUrl, out string username, out string password)
        {
            username = "";
            password = "";
            sanitizedUrl = NormalizeRtspUrl(url);

            try
            {
                if (!System.Uri.TryCreate(sanitizedUrl, System.UriKind.Absolute, out var uri))
                    return;

                if (string.IsNullOrWhiteSpace(uri.UserInfo))
                    return;

                var parts = uri.UserInfo.Split(':', 2);
                username = System.Uri.UnescapeDataString(parts[0]);
                password = parts.Length > 1 ? System.Uri.UnescapeDataString(parts[1]) : "";

                var builder = new System.UriBuilder(uri)
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
    }
}


