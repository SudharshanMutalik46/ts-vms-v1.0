using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection;
using System.Collections.ObjectModel;
using System.Linq;
using System;
using System.Threading.Tasks;
using TSVmsDesktop.Services;
using TSVmsDesktop.Models;
using System.Collections.Concurrent;
using System.Collections.Generic;

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
        public IntPtr WindowHandle { get; set; } = IntPtr.Zero; 
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
        private readonly IServiceProvider _serviceProvider;
        
        private System.Threading.SemaphoreSlim _refreshLock = new(1, 1);
        private System.Threading.CancellationTokenSource? _refreshCts;
        private System.Threading.CancellationTokenSource? _pollCts;
        private bool _isActive;
        private bool _isPollingStarted;

        private readonly ConcurrentDictionary<string, CameraMediaInfo?> _mediaInfoCache = new();
        private readonly ConcurrentDictionary<string, string> _mainStreamCache = new();
        private readonly ConcurrentDictionary<string, string> _subStreamCache = new();

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
            catch (TaskCanceledException) { }
        }

        public async Task ActivateAsync()
        {
            _isActive = true;

            _mainStreamCache.Clear();
            _subStreamCache.Clear();
            _mediaInfoCache.Clear();

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

        public async Task OnViewActivated() => await ActivateAsync();

        private async Task StartStatusPolling(System.Threading.CancellationToken token)
        {
            try
            {
                while (!token.IsCancellationRequested)
                {
                    if (_isActive && !IsFullScreen)
                    {
                        await _cameraService.LoadHealthAsync();
                        RequestRefresh();
                    }
                    await Task.Delay(5000, token);
                }
            }
            catch (TaskCanceledException) { }
        }

        [ObservableProperty] private string _fullScreenUrl = "";
        [ObservableProperty] private bool _fullScreenHasAudio = false;
        [ObservableProperty] private bool _isFullScreen = false;
        [ObservableProperty] private string _selectedCameraName = "";
        [ObservableProperty] private bool _isSyncing = false;

        [ObservableProperty] [NotifyPropertyChangedFor(nameof(CurrentPageDisplay))] private int _currentPage = 0;
        [ObservableProperty] [NotifyPropertyChangedFor(nameof(CurrentPageDisplay))] private int _totalPages = 1;
        [ObservableProperty] [NotifyPropertyChangedFor(nameof(GridSize))] private int _gridRows = 4;
        [ObservableProperty] [NotifyPropertyChangedFor(nameof(GridSize))] private int _gridColumns = 4;

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
            if (SelectedSlot != null) SelectedSlot.IsSelected = false;
            SelectedSlot = slot;
            if (SelectedSlot != null) SelectedSlot.IsSelected = true;
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
                await _cameraService.LoadHealthAsync();
                RequestRefresh();
                await Task.Delay(800);
            }
            finally { IsSyncing = false; }
        }

        [RelayCommand]
        public async Task ConnectAll()
        {
            if (!_isActive) return;

            foreach (var slot in CameraGrid)
            {
                if (!string.IsNullOrEmpty(slot.Id) && !slot.IsConnected &&
                    string.Equals(slot.BackendStatus, "Online", StringComparison.OrdinalIgnoreCase))
                {
                    await FetchCredentialsForSlot(slot);
                    slot.IsConnected = true;
                    slot.CameraName = string.IsNullOrEmpty(slot.CameraName) ? "Live Stream" : slot.CameraName;
                    await Task.Delay(600);
                }
            }
            UpdateAudioStates();
            OnPropertyChanged(nameof(ActiveStreamCount));
        }

        private async Task<CameraMediaInfo?> GetMediaInfoCachedAsync(string cameraId, bool forceRefresh = false)
        {
            if (string.IsNullOrWhiteSpace(cameraId)) return null;
            if (!forceRefresh && _mediaInfoCache.TryGetValue(cameraId, out var cached)) return cached;

            try
            {
                var mediaService = _serviceProvider.GetService<MediaService>();
                if (mediaService == null) return null;
                var info = await mediaService.GetMediaInfoAsync(cameraId);
                _mediaInfoCache[cameraId] = info;
                return info;
            }
            catch { return null; }
        }

        private static string InjectCredentialsIfMissing(string url, string username, string password)
        {
            if (string.IsNullOrWhiteSpace(url)) return "";
            if (string.IsNullOrWhiteSpace(username)) return url;
            if (!url.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase)) return url;
            if (url.Contains("@")) return url;

            string user = Uri.EscapeDataString(username ?? "");
            string pass = Uri.EscapeDataString(password ?? "");
            return $"rtsp://{user}:{pass}@{url.Substring(7)}";
        }

        private async Task<string> ResolvePreferredSubUrlAsync(CameraModel cam, string username, string password)
        {
            if (cam == null) return "";
            var info = await GetMediaInfoCachedAsync(cam.Id);
            string url = "";

            if (!string.IsNullOrWhiteSpace(info?.Selection?.SubRtsp)) url = info.Selection.SubRtsp;
            else if (!string.IsNullOrWhiteSpace(info?.Selection?.MainRtsp)) url = info.Selection.MainRtsp;
            else if (!string.IsNullOrWhiteSpace(cam.RtspUrl)) url = cam.RtspUrl;
            else url = cam.EffectiveRtspUrl; 

            return InjectCredentialsIfMissing(url, username, password);
        }

        private async Task<string> ResolvePreferredMainUrlAsync(CameraModel cam, string username, string password)
        {
            if (cam == null) return "";
            var info = await GetMediaInfoCachedAsync(cam.Id);
            string url = "";

            if (!string.IsNullOrWhiteSpace(info?.Selection?.MainRtsp)) url = info.Selection.MainRtsp;
            else if (!string.IsNullOrWhiteSpace(info?.Selection?.SubRtsp)) url = info.Selection.SubRtsp;
            else if (!string.IsNullOrWhiteSpace(cam.RtspUrl)) url = cam.RtspUrl;
            else url = cam.EffectiveRtspUrl;

            return InjectCredentialsIfMissing(url, username, password);
        }

        private void InvalidateStreamCaches(string cameraId)
        {
            if (string.IsNullOrWhiteSpace(cameraId)) return;
            _mediaInfoCache.TryRemove(cameraId, out _);
            _mainStreamCache.TryRemove(cameraId, out _);
            _subStreamCache.TryRemove(cameraId, out _);
        }

        private async Task FetchCredentialsForSlot(CameraSlot slot)
        {
            var cam = _cameraService.AllCameras.FirstOrDefault(c => c.Id == slot.Id);
            if (cam == null) return;

            if (string.IsNullOrWhiteSpace(cam.Username))
            {
                var creds = await _credentialService.GetCredentialsAsync(cam.Id);
                if (creds != null)
                {
                    cam.Username = creds.Username;
                    cam.Password = creds.Password;
                }
                else if (!string.IsNullOrWhiteSpace(cam.IpAddress) && cam.IpAddress.StartsWith("192.168.1."))
                {
                    cam.Username = "admin";
                    cam.Password = "123456";
                }
            }

            slot.Username = cam.Username ?? "";
            slot.Password = cam.Password ?? "";
            InvalidateStreamCaches(cam.Id);

            string resolvedSub = await ResolvePreferredSubUrlAsync(cam, slot.Username, slot.Password);
            if (!string.IsNullOrWhiteSpace(resolvedSub))
            {
                slot.RtspUrl = resolvedSub;
                _subStreamCache[cam.Id] = resolvedSub;
            }
        }

        [RelayCommand]
        public async Task ConnectDemo() => await ConnectAll();

        private async Task RefreshGrid()
        {
            if (!_isActive) return;
            await _refreshLock.WaitAsync();
            try 
            {
                if (System.Windows.Application.Current.Dispatcher.Thread != System.Threading.Thread.CurrentThread)
                {
                     var op = System.Windows.Application.Current.Dispatcher.InvokeAsync(() => RefreshGridInternal());
                     await op.Task.Unwrap();
                }
                else await RefreshGridInternal();
            }
            finally { _refreshLock.Release(); }
        }

        private async Task RefreshGridInternal()
        {
            var rawCameras = _cameraService.AllCameras.ToList();
            var uniqueCameras = rawCameras
                .GroupBy(c => 
                {
                    string key = c.IpAddress?.Trim() ?? "Unknown";
                    if (string.IsNullOrEmpty(key) || key == "127.0.0.1" || key == "localhost")
                    {
                         if (Uri.TryCreate(c.EffectiveRtspUrl, UriKind.Absolute, out var uri)) key = uri.Host;
                    }
                    return key;
                })
                .Select(g => g.OrderByDescending(c => c.Status?.ToLower() == "online").ThenByDescending(c => c.Status?.ToLower() == "checking").First())
                .OrderByDescending(c => c.Status?.ToLower() == "online").ThenBy(c => c.Name).ToList();

            TotalPages = (int)Math.Ceiling((double)uniqueCameras.Count / GridSize);
            if (TotalPages == 0) TotalPages = 1;
            if (CurrentPage >= TotalPages) CurrentPage = Math.Max(0, TotalPages - 1);

            var visibleBatch = uniqueCameras.Skip(CurrentPage * GridSize).Take(GridSize).ToList();

            var toRemove = CameraGrid.Where(s => visibleBatch.All(c => c.Id != s.Id)).ToList();
            foreach (var slot in toRemove)
            {
                if (slot.PipelineHandle != IntPtr.Zero)
                {
                    _videoService.StopStream(slot.PipelineHandle);
                    slot.PipelineHandle = IntPtr.Zero;
                }
                CameraGrid.Remove(slot);
            }

            foreach (var cam in visibleBatch)
            {
                if (IsFullScreen && cam.Name == SelectedCameraName) continue;

                var existing = CameraGrid.FirstOrDefault(s => s.Id == cam.Id);
                if (existing == null)
                {
                    var slot = new CameraSlot
                    {
                        Id = cam.Id,
                        CameraName = cam.Name,
                        RtspUrl = cam.RtspUrl,
                        OverlayText = cam.Name,
                        BackendStatus = cam.Status ?? "Offline",
                        IsConnected = false,
                        HasAudioCapability = cam.Capabilities?.HasAudio ?? false,
                        CameraVM = new CameraViewModel(_recordingService) { CameraId = cam.Id }
                    };
                    UpdateIp(slot, cam);
                    await FetchCredentialsForSlot(slot);
                    CameraGrid.Add(slot);
                    _ = slot.CameraVM.PollRecordingStatusAsync();
                }
                else
                {
                    if (existing.CameraName != cam.Name) existing.CameraName = cam.Name;
                    if (existing.OverlayText != cam.Name) existing.OverlayText = cam.Name;
                    
                    if (existing.BackendStatus != cam.Status) 
                    {
                        existing.BackendStatus = cam.Status ?? "Offline";
                        if (string.Equals(cam.Status, "Offline", StringComparison.OrdinalIgnoreCase) && existing.IsConnected)
                        {
                            existing.IsConnected = false;
                            if (existing.PipelineHandle != IntPtr.Zero)
                            {
                                _videoService.StopStream(existing.PipelineHandle);
                                existing.PipelineHandle = IntPtr.Zero;
                            }
                        }
                    }

                    string resolvedSub = await ResolvePreferredSubUrlAsync(cam, existing.Username, existing.Password);
                    if (!string.IsNullOrWhiteSpace(resolvedSub) && existing.RtspUrl != resolvedSub)
                    {
                        existing.RtspUrl = resolvedSub;
                        _subStreamCache[cam.Id] = resolvedSub;
                    }

                    existing.HasAudioCapability = cam.Capabilities?.HasAudio ?? false;
                    UpdateIp(existing, cam);
                    if (existing.CameraVM != null) _ = existing.CameraVM.PollRecordingStatusAsync();
                }
            }
            OnPropertyChanged(nameof(ActiveStreamCount));
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
                 if (Uri.TryCreate(cam.EffectiveRtspUrl, UriKind.Absolute, out var uri)) slot.IpAddress = uri.Host;
                 else if (!string.IsNullOrEmpty(cam.IpAddress)) slot.IpAddress = cam.IpAddress;
             }
             catch { }
        }

        private async Task<string> GetMainStreamUrlAsync(CameraSlot slot)
        {
            if (_mainStreamCache.TryGetValue(slot.Id, out var cached) && !string.IsNullOrWhiteSpace(cached))
                return cached;

            var cam = _cameraService.AllCameras.FirstOrDefault(c => c.Id == slot.Id);
            if (cam == null) return slot.RtspUrl;

            string mainUrl = await ResolvePreferredMainUrlAsync(cam, slot.Username, slot.Password);
            if (string.IsNullOrWhiteSpace(mainUrl)) mainUrl = slot.RtspUrl;

            _mainStreamCache[slot.Id] = mainUrl;
            return mainUrl;
        }

        public int ActiveStreamCount => CameraGrid.Count(c => c.IsConnected);

        [RelayCommand]
        public async Task EnterFullScreen(CameraSlot slot)
        {
            if (slot == null || string.IsNullOrEmpty(slot.RtspUrl)) return;
            SelectedCameraName = slot.CameraName;
            IsFullScreen = true;

            string mainUrl = await GetMainStreamUrlAsync(slot);
            _subStreamCache[slot.Id] = slot.RtspUrl;
            slot.RtspUrl = string.Empty; 

            await Task.Delay(800); 

            if (!string.IsNullOrEmpty(mainUrl) && mainUrl != _subStreamCache[slot.Id]) FullScreenUrl = mainUrl;
            else FullScreenUrl = _subStreamCache[slot.Id];

            FullScreenHasAudio = slot.HasAudioCapability;
            var mainVm = _serviceProvider.GetRequiredService<MainViewModel>();
            mainVm.IsKioskMode = true; 
            UpdateAudioStates();
        }

        [RelayCommand]
        public async Task ExitFullScreen() 
        {
            FullScreenUrl = string.Empty;
            var activeSlot = CameraGrid.FirstOrDefault(s => s.CameraName == SelectedCameraName);
            await Task.Delay(800);
            
            if (activeSlot != null && _subStreamCache.TryGetValue(activeSlot.Id, out var subUrl))
            {
                activeSlot.RtspUrl = subUrl; 
            }

            IsFullScreen = false;
            var mainVm = _serviceProvider.GetRequiredService<MainViewModel>();
            mainVm.IsKioskMode = false;
            UpdateAudioStates();
        }

        [RelayCommand]
        public void Snapshot(string cameraName) { }

        public void UpdateAudioStates()
        {
            foreach (var slot in CameraGrid)
            {
                if (slot.PipelineHandle == IntPtr.Zero) continue;
                bool shouldPlayAudio = false;
                if (slot.HasAudioCapability)
                {
                    if (IsFullScreen) shouldPlayAudio = (FullScreenUrl == slot.RtspUrl);
                    _videoService.SetVolume(slot.PipelineHandle, shouldPlayAudio ? 1.0 : 0.0);
                }
                slot.IsAudioPlaying = shouldPlayAudio; 
            }
        }

        private void OnStreamError(IntPtr windowHandle, string message)
        {
            var slot = CameraGrid.FirstOrDefault(s => s.WindowHandle == windowHandle);
            if (slot != null)
            {
                slot.IsConnected = false;
                slot.IsStreamFailed = true;
                slot.StreamErrorMessage = message;
                OnPropertyChanged(nameof(ActiveStreamCount));
                return;
            }

            if (IsFullScreen)
            {
                var activeSlot = CameraGrid.FirstOrDefault(s => s.CameraName == SelectedCameraName);
                if (activeSlot != null && FullScreenUrl != activeSlot.RtspUrl)
                {
                    FullScreenUrl = activeSlot.RtspUrl;
                }
            }
        }
    }
}
