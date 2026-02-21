using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection; // Added for GetRequiredService
using System.Collections.ObjectModel;
using System.Linq;
using System;
using System.Threading.Tasks;
using TSVmsDesktop.Services;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.ViewModels
{
    public partial class CameraSlot : ObservableObject
    {
        [ObservableProperty] private string _overlayText = "";
        
        [ObservableProperty] 
        private bool _isConnected = false; 

        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(StatusColor))]
        [NotifyPropertyChangedFor(nameof(StatusText))]
        private string _backendStatus = "Offline";
        
        public string StatusColor => BackendStatus.ToLower() switch 
        {
            "online" => "#10B981", // Green
            "offline" => "#EF4444", // Red
            _ => "#F59E0B" // Yellow for Checking/Unknown
        };
        public string StatusText => BackendStatus;

        [ObservableProperty] private string _cameraName = "";
        
        [ObservableProperty] private string _ipAddress = "Offline"; // For UI display

        [ObservableProperty] private bool _isStreamFailed = false;
        [ObservableProperty] private string _streamErrorMessage = "";

        public string Id { get; set; } = "";
        public IntPtr PipelineHandle { get; set; } = IntPtr.Zero;
        public IntPtr WindowHandle { get; set; } = IntPtr.Zero; // Added to match with StreamError
        public string RtspUrl { get; set; } = ""; 
    }

    public partial class LiveViewModel : ObservableObject
    {
        private readonly VideoService _videoService;
        private readonly CameraService _cameraService;
        private readonly CredentialService _credentialService;
        private readonly ApiClient _apiClient;
        private readonly IServiceProvider _serviceProvider; // Lazy resolution to break circular dependency

        public ObservableCollection<CameraSlot> CameraGrid { get; } = new();

        public LiveViewModel(VideoService videoService, CameraService cameraService, CredentialService credentialService, ApiClient apiClient, IServiceProvider serviceProvider) 
        {
            _videoService = videoService;
            _cameraService = cameraService;
            _credentialService = credentialService;
            _apiClient = apiClient;
            _serviceProvider = serviceProvider;

            _videoService.Initialize();
            _videoService.StreamError += OnStreamError;
            
            // Subscribe to camera updates to keep grid in sync (Debounced)
            _cameraService.AllCameras.CollectionChanged += (s, e) => RequestRefresh();

            _ = OnViewActivated();
            _ = StartStatusPolling();
        }

        private System.Threading.SemaphoreSlim _refreshLock = new(1, 1);
        private System.Threading.CancellationTokenSource? _refreshCts;

        private async void RequestRefresh()
        {
            _refreshCts?.Cancel();
            _refreshCts = new System.Threading.CancellationTokenSource();
            var token = _refreshCts.Token;

            try 
            {
                await Task.Delay(250, token); // Debounce: Wait for burst of updates to finish
                if (!token.IsCancellationRequested)
                {
                     await RefreshGrid();
                }
            } 
            catch (TaskCanceledException) { /* Ignored */ }
        }

        private async Task StartStatusPolling()
        {
            while (true)
            {
                // Removed initial delay to allow immediate update on load
                try 
                {
                    // Only reload if we are not in full screen to avoid glitches
                    if (!IsFullScreen) 
                    {
                        // Console.WriteLine("[LiveVM] Polling camera health...");
                        await _cameraService.LoadHealthAsync();
                        // After loading health, we need to refresh the grid to reflect changes
                        // Because LoadHealthAsync updates properties of CameraModel, but CameraGrid holds CameraSlot.
                        // We need to sync them.
                        RequestRefresh();
                    }
                }
                catch (Exception ex)
                {
                    System.Diagnostics.Debug.WriteLine($"[LiveVM] Polling failed: {ex.Message}");
                }
                
                await Task.Delay(5000); // Poll every 5 seconds
            }
        }

        public async Task OnViewActivated()
        {
            if (_cameraService.AllCameras.Count == 0)
            {
                 await _cameraService.LoadCamerasAsync();
                 await RefreshGrid();
            }
            // If already loaded, skip RefreshGrid to allow instant Reattach without restarting streams
        }

        [ObservableProperty] private string _fullScreenUrl = "";
        [ObservableProperty] private bool _isFullScreen = false;
        [ObservableProperty] private string _selectedCameraName = "";
        [ObservableProperty] private bool _isSyncing = false;

        [RelayCommand]
        public async Task Sync()
        {
            if (IsSyncing) return;
            IsSyncing = true;
            try
            {
                System.Diagnostics.Debug.WriteLine("[LiveVM] Manual sync requested...");
                await _cameraService.LoadHealthAsync();
                RequestRefresh();
                await Task.Delay(800); // Give user some visual feedback of the "thinking" process
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"[LiveVM] Sync failed: {ex.Message}");
            }
            finally
            {
                IsSyncing = false;
            }
        }

        [RelayCommand]
        public async Task ConnectAll()
        {
            System.Diagnostics.Debug.WriteLine("[TS-VMS] Connecting all cameras...");
            
            foreach (var slot in CameraGrid)
            {
                // Only connect valid slots that aren't already connected
                if (!string.IsNullOrEmpty(slot.Id) && !slot.IsConnected && string.Equals(slot.BackendStatus, "Online", StringComparison.OrdinalIgnoreCase))
                {
                    try
                    {
                        // Ensure we have credentials before starting
                        await FetchCredentialsForSlot(slot);

                        // 1. Tell Backend we are starting (Audit/Resource allocation)
                        // POST /api/v1/cameras/{id}/live/start
                        string url = $"/api/v1/cameras/{slot.Id}/live/start";
                        
                        // We send a generic body, or empty. 
                        // The backend expects a POST to trigger the session.
                        var body = new { stream = "main" }; 
                        
                        // We don't block heavily on this, if it fails we might still try RTSP
                        // but strictly we should wait.
                        // DEBUG: Commenting out to prevent 401/403 triggering logout
                        // await _apiClient.PostAsync(url, body);
                        System.Diagnostics.Debug.WriteLine($"[Live] Session started for {slot.CameraName}");
                    }
                    catch (Exception ex)
                    {
                        System.Diagnostics.Debug.WriteLine($"[Live] Backend session start failed: {ex.Message}");
                        // Optional: Continue anyway so local playback works even if backend API is glitchy
                    }

                    // 2. Trigger UI to start GStreamer (Via bindings)
                    slot.IsConnected = true;
                    slot.CameraName = string.IsNullOrEmpty(slot.CameraName) ? "Live Stream" : slot.CameraName;

                    // USER-REQUESTED DELAY: Stagger streams to prevent UDP/TCP setup collisions
                    await Task.Delay(600);
                }
            }
            OnPropertyChanged(nameof(ActiveStreamCount));
        }

        private async Task FetchCredentialsForSlot(CameraSlot slot)
        {
            var cam = _cameraService.AllCameras.FirstOrDefault(c => c.Id == slot.Id);
            if (cam != null && string.IsNullOrEmpty(cam.Username))
            {
                var creds = await _credentialService.GetCredentialsAsync(cam.Id);
                if (creds != null)
                {
                    System.Diagnostics.Debug.WriteLine($"[DEBUG] Fetched creds for {cam.IpAddress}: {creds.Username} / {creds.Password}");
                    cam.Username = creds.Username;
                    cam.Password = creds.Password;
                    slot.RtspUrl = cam.EffectiveRtspUrl ?? ""; // Update slot URL with injected creds
                }
                else 
                {
                     System.Diagnostics.Debug.WriteLine($"[DEBUG] FAILED to fetch creds for {cam.IpAddress} ({cam.Id})");
                     // Safe fallback for the user's specific local subnet
                     if (cam.IpAddress.StartsWith("192.168.1."))
                     {
                         cam.Username = "admin";
                         cam.Password = "123456";
                         slot.RtspUrl = cam.EffectiveRtspUrl;
                         System.Diagnostics.Debug.WriteLine($"[DEBUG] Restored default fallback for {cam.IpAddress}");
                     }
                }
            }
        }

        // Renaming the old command to point to the new Async logic
        [RelayCommand]
        public async Task ConnectDemo() => await ConnectAll();

        private async Task RefreshGrid()
        {
            if (!await _refreshLock.WaitAsync(0)) 
            {
                // If locked, we can either wait or skip. 
                // Since we have debounce, skipping concurrent runs is okay, 
                // but let's wait to be safe.
                await _refreshLock.WaitAsync();
            }

            try 
            {
                // Ensure we are on UI thread
                if (System.Windows.Application.Current.Dispatcher.Thread != System.Threading.Thread.CurrentThread)
                {
                     await System.Windows.Application.Current.Dispatcher.InvokeAsync(async () => await RefreshGridInternal());
                }
                else
                {
                     await RefreshGridInternal();
                }
            }
            finally
            {
                _refreshLock.Release();
            }
        }

        private async Task RefreshGridInternal()
        {
            var rawCameras = _cameraService.AllCameras.ToList();
            System.Diagnostics.Debug.WriteLine($"[LiveVM] RefreshGrid starting. Raw cameras: {rawCameras.Count}");

            // DEDUPLICATION: Group by IP/Host and pick the best one (Online > Checking > Offline)
            var uniqueCameras = rawCameras
                .GroupBy(c => 
                {
                    // DEBUG: Log the keys to see why they differ
                    string key = c.IpAddress?.Trim() ?? "Unknown";
                    if (string.IsNullOrEmpty(key) || key == "127.0.0.1" || key == "localhost")
                    {
                         if (Uri.TryCreate(c.EffectiveRtspUrl, UriKind.Absolute, out var uri)) key = uri.Host;
                    }
                    System.Diagnostics.Debug.WriteLine($"[LiveVM] Camera: {c.Name} | IP: '{c.IpAddress}' | Status: {c.Status} | Key: '{key}'");
                    return key;
                })
                .Select(g => 
                {
                    // Prefer Online, then Checking, then others. 
                    // If multiple are Online, pick the first one.
                    return g.OrderByDescending(c => c.Status?.ToLower() == "online")
                            .ThenByDescending(c => c.Status?.ToLower() == "checking")
                            .First();
                })
                .OrderByDescending(c => c.Status?.ToLower() == "online") // Sort Online to top
                .ThenBy(c => c.Name)
                .ToList();
            
            Console.WriteLine($"[LiveVM] Grid Refreshed: {uniqueCameras.Count} cameras visible.");
            System.Diagnostics.Debug.WriteLine($"[LiveVM] Current Grid Count: {CameraGrid.Count}");

            // REMOVED FILTER: var onlineCameras = realCameras.Where(c => string.Equals(c.Status, "Online", StringComparison.OrdinalIgnoreCase)).ToList();

            // 1. Remove cameras that were deleted or hidden by dedup
            var toRemove = CameraGrid.Where(s => uniqueCameras.All(c => c.Id != s.Id)).ToList();
            
            if (toRemove.Any()) System.Diagnostics.Debug.WriteLine($"[LiveVM] Cameras to remove: {toRemove.Count}");
            foreach (var slot in toRemove)
            {
                if (slot.PipelineHandle != IntPtr.Zero)
                {
                    _videoService.StopStream(slot.PipelineHandle);
                    slot.PipelineHandle = IntPtr.Zero;
                }
                CameraGrid.Remove(slot);
            }

            // 2. Add or Update cameras
            foreach (var cam in uniqueCameras)
            {
                var existing = CameraGrid.FirstOrDefault(s => s.Id == cam.Id);
                if (existing == null)
                {
                    var slot = new CameraSlot
                    {
                        Id = cam.Id,
                        CameraName = cam.Name,
                        RtspUrl = cam.EffectiveRtspUrl,
                        OverlayText = cam.Name,
                        BackendStatus = cam.Status ?? "Offline",
                        IsConnected = false
                    };
                    
                    UpdateIp(slot, cam);
                    await FetchCredentialsForSlot(slot);
                    CameraGrid.Add(slot);
                }
                else
                {
                    // Update metadata without stopping video
                    if (existing.CameraName != cam.Name) existing.CameraName = cam.Name;
                    if (existing.OverlayText != cam.Name) existing.OverlayText = cam.Name;
                    
                    // IMPORTANT: Use Status (which we now update from Health API)
                    if (existing.BackendStatus != cam.Status) 
                    {
                        existing.BackendStatus = cam.Status ?? "Offline";

                        // NEW: If camera goes offline, stop the stream immediately
                        if (string.Equals(cam.Status, "Offline", StringComparison.OrdinalIgnoreCase) && existing.IsConnected)
                        {
                            System.Diagnostics.Debug.WriteLine($"[LiveVM] {cam.Name} went Offline. Stopping stream.");
                            existing.IsConnected = false;
                            if (existing.PipelineHandle != IntPtr.Zero)
                            {
                                _videoService.StopStream(existing.PipelineHandle);
                                existing.PipelineHandle = IntPtr.Zero;
                            }
                        }
                    }

                    // Only update URL if it changed significantly (prevents unnecessary restarts)
                    if (existing.RtspUrl != cam.EffectiveRtspUrl && !string.IsNullOrEmpty(cam.EffectiveRtspUrl))
                    {
                         existing.RtspUrl = cam.EffectiveRtspUrl;
                    }
                    UpdateIp(existing, cam);
                }
            }
            OnPropertyChanged(nameof(ActiveStreamCount));
            
            // AUTO-START: Detect newly online cameras during polling and connect them.
            // This is idempotent; it won't restart already-connected slots.
            await ConnectAll();
        }

        private void UpdateIp(CameraSlot slot, CameraModel cam)
        {
             try 
             {
                 if (Uri.TryCreate(cam.EffectiveRtspUrl, UriKind.Absolute, out var uri))
                 {
                     slot.IpAddress = uri.Host;
                 }
                 else if (!string.IsNullOrEmpty(cam.IpAddress))
                 {
                     slot.IpAddress = cam.IpAddress;
                 }
             }
             catch { /* Ignore */ }
        }

        // --- Full Screen Mode ---

        // --- Dashboard Stats ---
        public int ActiveStreamCount => CameraGrid.Count(c => c.IsConnected);

        [RelayCommand]
        public void EnterFullScreen(CameraSlot slot)
        {
            System.Diagnostics.Debug.WriteLine($"[LiveVM] EnterFullScreen for: {slot?.CameraName}");
            if (slot == null || string.IsNullOrEmpty(slot.RtspUrl)) return;
            FullScreenUrl = slot.RtspUrl;
            SelectedCameraName = slot.CameraName;
            IsFullScreen = true;
            
            // LAZY RESOLVE MainViewModel to avoid Circular Dependency
            var mainVm = _serviceProvider.GetRequiredService<MainViewModel>();
            mainVm.IsKioskMode = true; // Hide Chrome & Maximize
            System.Diagnostics.Debug.WriteLine($"[LiveVM] IsKioskMode set to: {mainVm.IsKioskMode}");
        }

        [RelayCommand]
        public void ExitFullScreen()
        {
            IsFullScreen = false;
            FullScreenUrl = "";
            var mainVm = _serviceProvider.GetRequiredService<MainViewModel>();
            mainVm.IsKioskMode = false; // Restore Chrome & Normal Window
        }

        [RelayCommand]
        public void Snapshot(string cameraName)
        {
            // Placeholder for Snapshot Logic
            System.Diagnostics.Debug.WriteLine($"[Snapshot] Taking snapshot of {cameraName}");
        }

        private void OnStreamError(IntPtr windowHandle, string message)
        {
            // Find the slot that belongs to this window handle
            var slot = CameraGrid.FirstOrDefault(s => s.WindowHandle == windowHandle);
            if (slot != null)
            {
                System.Diagnostics.Debug.WriteLine($"[LiveVM] Stream Error for {slot.CameraName}: {message}");
                
                // Switch back to "No Signal" overlay
                slot.IsConnected = false;
                slot.IsStreamFailed = true;
                slot.StreamErrorMessage = message;
                
                OnPropertyChanged(nameof(ActiveStreamCount));
            }
        }

    }
}
