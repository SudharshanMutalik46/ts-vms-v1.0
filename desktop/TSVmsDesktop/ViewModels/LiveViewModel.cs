using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection; // Added for GetRequiredService
using System.Collections.ObjectModel;
using System.Linq;
using System;
using System.Threading.Tasks;
using TSVmsDesktop.Services;
using TSVmsDesktop.Models;

using System.Collections.Concurrent;

namespace TSVmsDesktop.ViewModels
{
    public partial class CameraSlot : ObservableObject
    {
        [ObservableProperty] private string _overlayText = "";
        
        [ObservableProperty] 
        private bool _isConnected = false; 

        [ObservableProperty] private bool _isSelected = false;
        [ObservableProperty] private bool _isLoading = false; 

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
        public string Username { get; set; } = "";
        public string Password { get; set; } = "";
        
        [ObservableProperty] private bool _hasAudioCapability = false;
        [ObservableProperty] private bool _isAudioPlaying = false;
        
        [ObservableProperty] private CameraViewModel? _cameraVM;
    }

    public partial class LiveViewModel : ObservableObject
    {
        private readonly VideoService _videoService;
        private readonly CameraService _cameraService;
        private readonly CredentialService _credentialService;
        private readonly RecordingService _recordingService;
        private readonly IServiceProvider _serviceProvider; // Lazy resolution to break circular dependency
        
        private System.Threading.SemaphoreSlim _refreshLock = new(1, 1);
        private System.Threading.CancellationTokenSource? _refreshCts;

        private System.Threading.CancellationTokenSource? _pollCts;
        private bool _isActive;
        private bool _isPollingStarted;

        public ObservableCollection<CameraSlot> CameraGrid { get; } = new();

        public LiveViewModel(VideoService videoService, CameraService cameraService, CredentialService credentialService, RecordingService recordingService, IServiceProvider serviceProvider) 
        {
            _videoService = videoService;
            _cameraService = cameraService;
            _credentialService = credentialService;
            _recordingService = recordingService;
            _serviceProvider = serviceProvider;

            _videoService.Initialize();
            _videoService.StreamError += OnStreamError;
            
            // Subscribe to camera updates to keep grid in sync (Debounced)
            _cameraService.AllCameras.CollectionChanged += (s, e) => RequestRefresh();
        }



        private async void RequestRefresh()
        {
            if (!_isActive) return;

            _refreshCts?.Cancel();
            _refreshCts = new System.Threading.CancellationTokenSource();
            var token = _refreshCts.Token;

            try
            {
                await Task.Delay(250, token);
                if (!token.IsCancellationRequested)
                {
                    await RefreshGrid();
                }
            }
            catch (TaskCanceledException)
            {
            }
        }

        public async Task ActivateAsync()
        {
            _isActive = true;

            if (_cameraService.AllCameras.Count == 0)
            {
                await _cameraService.LoadCamerasAsync();
            }

            await RefreshGrid();

            if (!_isPollingStarted)
            {
                _pollCts = new System.Threading.CancellationTokenSource();
                _isPollingStarted = true;
                _ = StartStatusPolling(_pollCts.Token);
            }
        }

        public void Deactivate()
        {
            _isActive = false;
            try { _refreshCts?.Cancel(); } catch { }

            try { _pollCts?.Cancel(); } catch { }
            _pollCts = null;
            _isPollingStarted = false;

            IsFullScreen = false;
            FullScreenUrl = string.Empty;
            SelectedCameraName = string.Empty;

            foreach (var slot in CameraGrid)
            {
                slot.IsConnected = false;
                slot.IsAudioPlaying = false;
            }

            OnPropertyChanged(nameof(ActiveStreamCount));
        }

        public async Task OnViewActivated()
        {
            await ActivateAsync();
        }
        private async Task StartStatusPolling(System.Threading.CancellationToken token)
        {
            try
            {
                while (!token.IsCancellationRequested)
                {
                    try
                    {
                        if (_isActive && !IsFullScreen)
                        {
                            await _cameraService.LoadHealthAsync();
                            RequestRefresh();
                        }
                    }
                    catch (Exception ex)
                    {
                        System.Diagnostics.Debug.WriteLine($"[LiveVM] Polling failed: {ex.Message}");
                    }

                    await Task.Delay(5000, token);
                }
            }
            catch (TaskCanceledException)
            {
            }
        }



        [ObservableProperty] private string _fullScreenUrl = "";
        [ObservableProperty] private bool _fullScreenHasAudio = false;
        [ObservableProperty] private bool _isFullScreen = false;
        [ObservableProperty] private string _selectedCameraName = "";
        [ObservableProperty] private bool _isSyncing = false;

        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(CurrentPageDisplay))]
        private int _currentPage = 0;

        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(CurrentPageDisplay))]
        private int _totalPages = 1;

        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(GridSize))]
        private int _gridRows = 4;

        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(GridSize))]
        private int _gridColumns = 4;

        public int GridSize => GridRows * GridColumns;

        public string CurrentPageDisplay => $"Page {CurrentPage + 1} of {TotalPages}";

        [RelayCommand]
        public void SetLayout(string sizeParam)
        {
            if (int.TryParse(sizeParam, out int size) && size >= 1 && size <= 4)
            {
                GridRows = size;
                GridColumns = size;
                CurrentPage = 0; 
                RequestRefresh();
            }
        }

        [ObservableProperty] private CameraSlot? _selectedSlot;

        public void SelectSlot(CameraSlot slot)
        {
            if (SelectedSlot != null)
            {
                SelectedSlot.IsSelected = false;
            }
            SelectedSlot = slot;
            if (SelectedSlot != null)
            {
                SelectedSlot.IsSelected = true;
            }
        }

        [RelayCommand]
        public void NextPage()
        {
            if (CurrentPage < TotalPages - 1)
            {
                CurrentPage++;
                RequestRefresh();
            }
        }

        [RelayCommand]
        public void PreviousPage()
        {
            if (CurrentPage > 0)
            {
                CurrentPage--;
                RequestRefresh();
            }
        }

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
            if (!_isActive) return;

            System.Diagnostics.Debug.WriteLine("[TS-VMS] Connecting all cameras...");

            foreach (var slot in CameraGrid)
            {
                if (!string.IsNullOrEmpty(slot.Id) &&
                    !slot.IsConnected &&
                    string.Equals(slot.BackendStatus, "Online", StringComparison.OrdinalIgnoreCase))
                {
                    try
                    {
                        await FetchCredentialsForSlot(slot);
                    }
                    catch (Exception ex)
                    {
                        System.Diagnostics.Debug.WriteLine($"[Live] Backend session start failed: {ex.Message}");
                    }

                    slot.IsConnected = true;
                    slot.CameraName = string.IsNullOrEmpty(slot.CameraName) ? "Live Stream" : slot.CameraName;
                    await Task.Delay(600);
                }
            }

            UpdateAudioStates();
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
                    slot.Username = creds.Username;
                    slot.Password = creds.Password;
                    slot.RtspUrl = cam.SubStreamUrl ?? ""; // Update slot URL with injected creds (SUBSTREAM)
                }
                else 
                {
                     System.Diagnostics.Debug.WriteLine($"[DEBUG] FAILED to fetch creds for {cam.IpAddress} ({cam.Id})");
                     // Safe fallback for the user's specific local subnet
                     if (cam.IpAddress.StartsWith("192.168.1."))
                     {
                         cam.Username = "admin";
                         cam.Password = "123456";
                         slot.Username = "admin";
                         slot.Password = "123456";
                         slot.RtspUrl = cam.SubStreamUrl; // (SUBSTREAM)
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
            if (!_isActive) return;
            await _refreshLock.WaitAsync();

            try 
            {
                // Ensure we are on UI thread
                if (System.Windows.Application.Current.Dispatcher.Thread != System.Threading.Thread.CurrentThread)
                {
                     // Important: InvokeAsync(async ...) returns Task<Task>; unwrap it so we
                     // do not release _refreshLock before RefreshGridInternal actually finishes.
                     var op = System.Windows.Application.Current.Dispatcher.InvokeAsync(() => RefreshGridInternal());
                     await op.Task.Unwrap();
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

            TotalPages = (int)Math.Ceiling((double)uniqueCameras.Count / GridSize);
            if (TotalPages == 0) TotalPages = 1;
            if (CurrentPage >= TotalPages) CurrentPage = Math.Max(0, TotalPages - 1);

            var visibleBatch = uniqueCameras.Skip(CurrentPage * GridSize).Take(GridSize).ToList();

            Console.WriteLine($"[LiveVM] Grid Refreshed: {visibleBatch.Count} cameras visible on Page {CurrentPage + 1}/{TotalPages}. Total unique: {uniqueCameras.Count}");
            System.Diagnostics.Debug.WriteLine($"[LiveVM] Current Grid Count: {CameraGrid.Count}");

            // REMOVED FILTER: var onlineCameras = realCameras.Where(c => string.Equals(c.Status, "Online", StringComparison.OrdinalIgnoreCase)).ToList();

            // 1. Remove cameras that were deleted, hidden by dedup, OR NOT ON CURRENT PAGE
            var toRemove = CameraGrid.Where(s => visibleBatch.All(c => c.Id != s.Id)).ToList();
            
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
            foreach (var cam in visibleBatch)
            {
                // ADD THIS SAFEGUARD: Skip modifying the slot if it's currently in full screen
                // We use SelectedCameraName to properly skip over the Active camera.
                if (IsFullScreen && cam.Name == SelectedCameraName)
                {
                    continue;
                }

                var existing = CameraGrid.FirstOrDefault(s => s.Id == cam.Id);
                if (existing == null)
                {
                    var slot = new CameraSlot
                    {
                        Id = cam.Id,
                        CameraName = cam.Name,
                        RtspUrl = cam.SubStreamUrl,
                        OverlayText = cam.Name,
                        BackendStatus = cam.Status ?? "Offline",
                        IsConnected = false,
                        HasAudioCapability = cam.Capabilities?.HasAudio ?? false,
                        CameraVM = new CameraViewModel(_recordingService) { CameraId = cam.Id }
                    };
                    
                    UpdateIp(slot, cam);
                    await FetchCredentialsForSlot(slot);
                    CameraGrid.Add(slot);
                    
                    // Trigger initial poll
                    _ = slot.CameraVM.PollRecordingStatusAsync();
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
                    if (existing.RtspUrl != cam.SubStreamUrl && !string.IsNullOrEmpty(cam.SubStreamUrl))
                    {
                         existing.RtspUrl = cam.SubStreamUrl;
                    }
                    existing.HasAudioCapability = cam.Capabilities?.HasAudio ?? false;
                    UpdateIp(existing, cam);
                    
                    // Trigger poll on existing slots
                    if (existing.CameraVM != null)
                    {
                        _ = existing.CameraVM.PollRecordingStatusAsync();
                    }
                }
            }
            OnPropertyChanged(nameof(ActiveStreamCount));
            
            // AUTO-START: Detect newly online cameras during polling and connect them.
            // This is idempotent; it won't restart already-connected slots.
            if (_isActive)
            {
                await ConnectAll();
                UpdateAudioStates();
            }
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
        private ConcurrentDictionary<string, string> _mainStreamCache = new();
        private ConcurrentDictionary<string, string> _subStreamCache = new();

        private async Task<string> GetMainStreamUrlAsync(CameraSlot slot)
        {
            if (_mainStreamCache.TryGetValue(slot.Id, out var cached))
                return cached;

            var cam = _cameraService.AllCameras.FirstOrDefault(c => c.Id == slot.Id);
            if (cam == null) return slot.RtspUrl;

            string mainUrl = "";

            try
            {
                // Prioritize backend MediaService mapping if the API is available
                // Note: since MediaService isn't in scope we safely look for alternative if provided or omit API call assuming user logic requested generic wrapper here.
                // Assuming "MediaService" is available in the real DI container, else wrap it in try-catch. Let's try to resolve it.
                // In context: The user provided code requiring MediaService gRPC wrapper
                var mediaService = _serviceProvider.GetService<MediaService>();
                if (mediaService != null)
                {
                    var mediaInfo = await mediaService.GetMediaInfoAsync(slot.Id);
                    
                    if (mediaInfo != null && !string.IsNullOrEmpty(mediaInfo.Selection?.MainRtsp))
                    {
                        mainUrl = mediaInfo.Selection.MainRtsp;
                        
                        // Safely inject credentials if missing
                        if (!string.IsNullOrEmpty(slot.Username) && !mainUrl.Contains("@"))
                        {
                            if (mainUrl.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
                            {
                                mainUrl = $"rtsp://{slot.Username}:{slot.Password}@{mainUrl.Substring(7)}";
                            }
                        }
                    }
                }
            }
            catch 
            { 
                // Ignore API lookup errors and gracefully fall through
            }

            // Fallback to reversed local logic
            if (string.IsNullOrEmpty(mainUrl)) mainUrl = cam.MainStreamUrl;
            // Ultimate fallback to existing URL
            if (string.IsNullOrEmpty(mainUrl)) mainUrl = slot.RtspUrl;

            _mainStreamCache[slot.Id] = mainUrl;
            return mainUrl;
        }

        // --- Dashboard Stats ---
        public int ActiveStreamCount => CameraGrid.Count(c => c.IsConnected);

        [RelayCommand]
        public async Task EnterFullScreen(CameraSlot slot)
        {
            Console.WriteLine($"[LiveVM] EnterFullScreen for: {slot?.CameraName}");
            if (slot == null || string.IsNullOrEmpty(slot.RtspUrl)) return;
            
            // IMMEDIATELY raise the shield against the background loop
            SelectedCameraName = slot.CameraName;
            IsFullScreen = true;

            string mainUrl = await GetMainStreamUrlAsync(slot);

            // 1. Cache the grid's sub-stream URL and kill the grid connection
            _subStreamCache[slot.Id] = slot.RtspUrl;
            slot.RtspUrl = string.Empty; 

            // 2. Give the IP camera 800ms to physically teardown the old socket
            Console.WriteLine($"[LiveVM] Waiting for {slot.CameraName} to release socket...");
            await Task.Delay(800); 

            // 3. Request the new stream
            if (!string.IsNullOrEmpty(mainUrl) && mainUrl != _subStreamCache[slot.Id])
            {
                Console.WriteLine($"[LiveVM] Selected stream for FullScreen: [MAIN]");
                FullScreenUrl = mainUrl;
            }
            else
            {
                Console.WriteLine($"[LiveVM] Selected stream for FullScreen: [SUB/IDENTICAL]");
                FullScreenUrl = _subStreamCache[slot.Id];
            }

            FullScreenHasAudio = slot.HasAudioCapability;

            var mainVm = _serviceProvider.GetRequiredService<MainViewModel>();
            mainVm.IsKioskMode = true; 
            UpdateAudioStates();
        }

        [RelayCommand]
        public async Task ExitFullScreen() 
        {
            Console.WriteLine($"[LiveVM] Exiting FullScreen");
            
            // 1. Kill the MAIN stream to free up the camera connection
            FullScreenUrl = string.Empty;
            
            var activeSlot = CameraGrid.FirstOrDefault(s => s.CameraName == SelectedCameraName);

            // 2. Add delay before restarting the sub-stream
            // The background loop is STILL BLOCKED during this delay
            await Task.Delay(800);
            
            // 3. Restore its SUB stream connection
            if (activeSlot != null && _subStreamCache.TryGetValue(activeSlot.Id, out var subUrl))
            {
                Console.WriteLine($"[LiveVM] Restoring grid stream...");
                activeSlot.RtspUrl = subUrl; 
            }

            // 4. NOW we can unblock the background loop and hide the UI
            IsFullScreen = false;

            var mainVm = _serviceProvider.GetRequiredService<MainViewModel>();
            mainVm.IsKioskMode = false;
            UpdateAudioStates();
        }



        [RelayCommand]
        public void Snapshot(string cameraName)
        {
            // Placeholder for Snapshot Logic
            System.Diagnostics.Debug.WriteLine($"[Snapshot] Taking snapshot of {cameraName}");
        }

        public void UpdateAudioStates()
        {
            foreach (var slot in CameraGrid)
            {
                if (slot.PipelineHandle == IntPtr.Zero) continue;
                    
                bool shouldPlayAudio = false;

                if (slot.HasAudioCapability)
                {
                    if (IsFullScreen)
                        shouldPlayAudio = (FullScreenUrl == slot.RtspUrl);

                    // Tell GStreamer to set volume
                    _videoService.SetVolume(slot.PipelineHandle, shouldPlayAudio ? 1.0 : 0.0);
                }

                // Tell the XAML UI to update the icon (Green vs Grey)
                slot.IsAudioPlaying = shouldPlayAudio; 
            }
        }

        private void OnStreamError(IntPtr windowHandle, string message)
        {
            // 1. Handle Grid Camera Error
            var slot = CameraGrid.FirstOrDefault(s => s.WindowHandle == windowHandle);
            if (slot != null)
            {
                System.Diagnostics.Debug.WriteLine($"[LiveVM] Stream Error for {slot.CameraName}: {message}");
                slot.IsConnected = false;
                slot.IsStreamFailed = true;
                slot.StreamErrorMessage = message;
                
                OnPropertyChanged(nameof(ActiveStreamCount));
                return;
            }

            // 2. Handle Full Screen Error / Fallback (Triggered if pipeline fails on H.264/Unreachable Main)
            if (IsFullScreen)
            {
                var activeSlot = CameraGrid.FirstOrDefault(s => s.CameraName == SelectedCameraName);
                if (activeSlot != null && FullScreenUrl != activeSlot.RtspUrl)
                {
                    if (message.ToLower().Contains("h264") || message.ToLower().Contains("codec"))
                    {
                        System.Diagnostics.Debug.WriteLine($"[LiveVM] MAIN stream codec issue (H.264?). App expects H.265. Falling back to SUB stream.");
                    }
                    else
                    {
                        System.Diagnostics.Debug.WriteLine($"[LiveVM] FullScreen MAIN stream failed: {message}. Falling back to SUB stream.");
                    }
                    
                    // Setting FullScreenUrl instantly forces WPF to re-evaluate and restart the canvas under the Sub URL.
                    FullScreenUrl = activeSlot.RtspUrl;
                }
            }
        }

    }
}
