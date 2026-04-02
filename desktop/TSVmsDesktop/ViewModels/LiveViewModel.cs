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
    /// <summary>Priority cascade for live streaming: WebRtc → Rtsp.</summary>
    public enum StreamTier { WebRtc, Rtsp }

    public partial class CameraSlot : ObservableObject
    {
        [ObservableProperty] private string _overlayText = "";

        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(IsGStreamerActive))]
        [NotifyPropertyChangedFor(nameof(IsWebRtcActive))]
        private bool _isConnected = false;

        [ObservableProperty] private bool _isSelected = false;
        [ObservableProperty] private bool _isLoading = false;

        /// <summary>Current streaming tier. Changes trigger WebRtc/GStreamer surface swap.</summary>
        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(IsGStreamerActive))]
        [NotifyPropertyChangedFor(nameof(IsWebRtcActive))]
        private StreamTier _activeTier = StreamTier.Rtsp;

        /// <summary>True when VideoCanvas (GStreamer) should be visible.</summary>
        public bool IsGStreamerActive => IsConnected && ActiveTier == StreamTier.Rtsp;

        /// <summary>True when WebView2 (WebRTC) surface should be visible.</summary>
        public bool IsWebRtcActive => LiveViewModel.WebRtcEnabled && IsConnected && ActiveTier == StreamTier.WebRtc;

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
        [ObservableProperty]
        [NotifyPropertyChangedFor(nameof(IsTransientStatusVisible))]
        private string _transientStatusText = "";
        [ObservableProperty] private bool _isReconnectInProgress = false;

        public bool IsTransientStatusVisible => !string.IsNullOrWhiteSpace(TransientStatusText);

        public string Id { get; set; } = "";
        public IntPtr PipelineHandle { get; set; } = IntPtr.Zero;
        public IntPtr WindowHandle { get; set; } = IntPtr.Zero;
        /// <summary>Direct RTSP URL — used when ActiveTier == Rtsp.</summary>
        public string RtspUrl { get; set; } = "";
        public string FullScreenRtspUrl { get; set; } = "";
        public string MainRtspUrl { get; set; } = "";
        /// <summary>SFU base URL (e.g. http://localhost:8080/api/v1/sfu) — used when ActiveTier == WebRtc.</summary>
        public string WebRtcSfuUrl { get; set; } = "";
        /// <summary>Camera UUID passed to the SFU as the room identifier.</summary>
        public string WebRtcRoomId { get; set; } = "";
        public string PreferredCodec { get; set; } = "";
        public string WebRtcCodecPreference { get; set; } = "";
        public bool WebRtcRetriedWithH264 { get; set; } = false;
        public int WebRtcTimeoutMs { get; set; } = 5000;
        public int WebRtcTrackTimeoutMs { get; set; } = 2500;
        public string Username { get; set; } = "";
        public string Password { get; set; } = "";
        public string RtspTransport { get; set; } = "udp";
        public bool RtspRetriedWithTcp { get; set; } = false;
        public bool RtspRetriedWithUdp { get; set; } = false;
        public bool RtspRetriedWithoutQuery { get; set; } = false;
        public string SessionId { get; set; } = "";
        public StreamTier PreferredPrimaryTier { get; set; } = StreamTier.Rtsp;
        public StreamTier PreferredFallbackTier { get; set; } = StreamTier.Rtsp;
        public bool IsPipelineStarting { get; set; } = false;
        /// <summary>UTC time when the current pipeline was last started. Used to enforce a minimum lifetime guard.</summary>
        public DateTime PipelineStartedAt { get; set; } = DateTime.MinValue;
        /// <summary>
        /// Set to true after NavigateToString is called for WebRTC.
        /// Prevents the restart loop caused by brief IsConnected toggles
        /// (status polls, AdvanceToNextTier) re-navigating an already-playing WebView2.
        /// Reset when the WebRtc tier is abandoned (ActiveTier changes away).
        /// </summary>
        public bool IsWebRtcStarted { get; set; } = false;
        public bool RtspRetriedWithMain { get; set; } = false;

        [ObservableProperty] private bool _hasAudioCapability = false;
        [ObservableProperty] private bool _isAudioPlaying = false;
        
        [ObservableProperty] private CameraViewModel? _cameraVM;
    }

    public partial class LiveViewModel : ObservableObject
    {
        public static bool WebRtcEnabled =>
            string.Equals(
                Environment.GetEnvironmentVariable("TS_VMS_WEBRTC_ENABLED"),
                "true",
                StringComparison.OrdinalIgnoreCase);

        private readonly VideoService _videoService;
        private readonly CameraService _cameraService;
        private readonly CredentialService _credentialService;
        private readonly RecordingService _recordingService;
        private readonly LiveSessionService _liveSessionService;
        private readonly MediaService _mediaService;
        private readonly IServiceProvider _serviceProvider;
        
        private System.Threading.SemaphoreSlim _refreshLock = new(1, 1);
        private System.Threading.CancellationTokenSource? _refreshCts;
        private System.Threading.CancellationTokenSource? _pollCts;
        private bool _isActive;
        private bool _isPollingStarted;

        private readonly ConcurrentDictionary<string, CameraMediaInfo?> _mediaInfoCache = new();
        private readonly ConcurrentDictionary<string, string> _mainStreamCache = new();
        private readonly ConcurrentDictionary<string, string> _subStreamCache = new();

        private static StreamTier? ParseTier(string? tier) =>
            tier?.Trim().ToLowerInvariant() switch
            {
                "webrtc" => StreamTier.WebRtc,
                "rtsp"   => StreamTier.Rtsp,
                _        => null,
            };

        private static string NormalizeCodec(string? codec)
        {
            if (string.IsNullOrWhiteSpace(codec)) return "";
            string value = codec.Trim().ToUpperInvariant().Replace(".", "").Replace(" ", "");
            if (value.Contains("H264") || value == "AVC") return "H264";
            if (value.Contains("H265") || value == "HEVC") return "H265";
            return "";
        }

        private static string ResolveWebRtcCodec(string? preferredCodec)
        {
            var normalized = NormalizeCodec(preferredCodec);
            if (string.IsNullOrWhiteSpace(normalized)) return "H264";
            return string.Equals(normalized, "H265", StringComparison.OrdinalIgnoreCase)
                ? "H264"
                : normalized;
        }

        public ObservableCollection<CameraSlot> CameraGrid { get; } = new();

        public LiveViewModel(VideoService videoService, CameraService cameraService, MediaService mediaService, CredentialService credentialService, RecordingService recordingService, LiveSessionService liveSessionService, IServiceProvider serviceProvider)
        {
            _videoService = videoService;
            _cameraService = cameraService;
            _mediaService = mediaService;
            _credentialService = credentialService;
            _recordingService = recordingService;
            _liveSessionService = liveSessionService;
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

            // Load health BEFORE the first grid render so cameras already have
            // their true Online/Offline status when ConnectAll() runs.
            // Without this, cameras start as "Checking…" and ConnectAll() skips
            // them entirely — the user would have to wait 5 s for the poll to fire.
            await _cameraService.LoadHealthAsync();

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
            FullScreenRtspUrl = string.Empty;
            SelectedCameraName = string.Empty;

            foreach (var slot in CameraGrid)
            {
                slot.IsConnected = false;
                slot.IsAudioPlaying = false;
                slot.IsReconnectInProgress = false;
                slot.TransientStatusText = "";
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
        [ObservableProperty] private string _fullScreenRtspUrl = "";
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
                // Aggressively re-interrogate ONVIF for all cameras to resolve "Confusion"
                foreach (var cam in _cameraService.AllCameras)
                {
                    _ = _mediaService.SelectProfilesAsync(cam.Id, "", "");
                }

                await _cameraService.LoadHealthAsync();
                await RefreshGrid();
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
                    if (!WebRtcEnabled && slot.ActiveTier == StreamTier.WebRtc)
                    {
                        slot.ActiveTier = StreamTier.Rtsp;
                        slot.IsWebRtcStarted = false;
                    }

                    // Only fetch credentials/session if not already resolved during slot creation.
                    if (string.IsNullOrEmpty(slot.RtspUrl))
                        await FetchCredentialsForSlot(slot);

                    slot.IsConnected = true;
                    slot.CameraName = string.IsNullOrEmpty(slot.CameraName) ? "Live Stream" : slot.CameraName;
                    await Task.Delay(200); // Reduced from 600 ms — just enough to stagger pipeline creation
                }
            }
            UpdateAudioStates();
            OnPropertyChanged(nameof(ActiveStreamCount));
        }

        [RelayCommand]
        public async Task ReconnectStream(CameraSlot? slot)
        {
            if (slot == null || slot.IsReconnectInProgress) return;

            if (!WebRtcEnabled && slot.ActiveTier == StreamTier.WebRtc)
            {
                slot.ActiveTier = StreamTier.Rtsp;
            }

            var retryTier = slot.ActiveTier;
            VideoService.Log($"[TS-VMS] Reconnect requested cam={slot.CameraName} tier={retryTier}");

            slot.IsReconnectInProgress = true;
            slot.IsStreamFailed = false;
            slot.StreamErrorMessage = "";
            slot.TransientStatusText = $"Reconnecting {retryTier}...";

            try
            {
                if (WebRtcEnabled && retryTier == StreamTier.WebRtc)
                {
                    slot.IsWebRtcStarted = false;
                }

                if (slot.PipelineHandle != IntPtr.Zero)
                {
                    var oldHandle = slot.PipelineHandle;
                    slot.PipelineHandle = IntPtr.Zero;
                    slot.WindowHandle = IntPtr.Zero;
                    _ = _videoService.StopStreamAsync(oldHandle);
                }

                slot.IsConnected = false;
                await Task.Delay(350);
                slot.IsConnected = true;

                VideoService.Log($"[TS-VMS] Reconnect restart queued cam={slot.CameraName} tier={retryTier}");
            }
            catch (Exception ex)
            {
                VideoService.Log($"[TS-VMS] Reconnect failed to start cam={slot.CameraName} tier={retryTier}: {ex.Message}");
                slot.TransientStatusText = "Reconnect failed";
                slot.IsStreamFailed = true;
                slot.StreamErrorMessage = ex.Message;
                _ = ClearReconnectStatusAsync(slot);
            }
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
            else if (!string.IsNullOrWhiteSpace(cam.RtspUrl)) url = cam.RtspUrl;
            else url = cam.EffectiveRtspUrl; 

            // Unescape XML entities like &amp; commonly returned by ONVIF
            url = System.Net.WebUtility.HtmlDecode(url ?? "");

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

            // Unescape XML entities
            url = System.Net.WebUtility.HtmlDecode(url ?? "");

            return InjectCredentialsIfMissing(url, username, password);
        }

        private async Task<string> ResolvePreferredCodecAsync(CameraModel cam)
        {
            if (cam == null) return "";

            var info = await GetMediaInfoCachedAsync(cam.Id);
            string codec = NormalizeCodec(info?.Selection?.SubCodec);
            if (!string.IsNullOrWhiteSpace(codec)) return codec;

            codec = NormalizeCodec(info?.Selection?.MainCodec);
            if (!string.IsNullOrWhiteSpace(codec)) return codec;

            return "";
        }

        private void InvalidateStreamCaches(string cameraId)
        {
            if (string.IsNullOrWhiteSpace(cameraId)) return;
            _mediaInfoCache.TryRemove(cameraId, out _);
            _mainStreamCache.TryRemove(cameraId, out _);
            _subStreamCache.TryRemove(cameraId, out _);
        }

        public async Task FetchCredentialsForSlot(CameraSlot slot)
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
            }

            slot.Username = cam.Username ?? "";
            slot.Password = cam.Password ?? "";
            slot.RtspRetriedWithTcp = false;
            slot.RtspRetriedWithUdp = false;
            slot.RtspRetriedWithoutQuery = false;
            InvalidateStreamCaches(cam.Id);

            string resolvedMain = await ResolvePreferredMainUrlAsync(cam, slot.Username, slot.Password);
            slot.MainRtspUrl = resolvedMain;

            // --- Always resolve RTSP as the final fallback tier ---
            string resolvedSub = await ResolvePreferredSubUrlAsync(cam, slot.Username, slot.Password);
            slot.RtspUrl = resolvedSub;
            slot.RtspRetriedWithMain = false;
            slot.PreferredCodec = await ResolvePreferredCodecAsync(cam);
            slot.WebRtcCodecPreference = ResolveWebRtcCodec(slot.PreferredCodec);
            slot.PreferredFallbackTier = StreamTier.Rtsp;

            // Autonomous Interrogation fallback
            if (string.IsNullOrWhiteSpace(slot.PreferredCodec))
            {
                var mediaService = _serviceProvider.GetService<MediaService>();
                if (mediaService != null)
                {
                    // Trigger sync (passing nulls triggers full auto-discovery in backend)
                    await mediaService.SelectProfilesAsync(cam.Id, null!, null!);
                    InvalidateStreamCaches(cam.Id);
                    slot.PreferredCodec = await ResolvePreferredCodecAsync(cam);
                    slot.WebRtcCodecPreference = ResolveWebRtcCodec(slot.PreferredCodec);
                    slot.PreferredFallbackTier = StreamTier.Rtsp;
                }
            }

            if (!WebRtcEnabled)
            {
                slot.SessionId = "";
                slot.WebRtcSfuUrl = "";
                slot.WebRtcRoomId = "";
                slot.WebRtcCodecPreference = "";
                slot.WebRtcRetriedWithH264 = false;
                slot.WebRtcTimeoutMs = 5000;
                slot.WebRtcTrackTimeoutMs = 2500;
                slot.IsWebRtcStarted = false;
                slot.PreferredPrimaryTier = StreamTier.Rtsp;
                slot.PreferredFallbackTier = StreamTier.Rtsp;
                slot.ActiveTier = StreamTier.Rtsp;

                if (!string.IsNullOrWhiteSpace(resolvedSub))
                    _subStreamCache[cam.Id] = resolvedSub;

                VideoService.Log($"[TS-VMS] Tier=Rtsp cam={slot.CameraName} codec={slot.PreferredCodec} webRTC=disabled");
                return;
            }

            // --- Try live/start to populate WebRTC tier ---
            try
            {
                var session = await _liveSessionService.StartSessionAsync(cam.Id, "sub");
                StreamTier? sessionPrimaryTier = null;
                if (session != null)
                {
                    slot.SessionId = session.ViewerSessionId;
                    if (string.IsNullOrWhiteSpace(slot.PreferredCodec))
                        slot.PreferredCodec = NormalizeCodec(session.SelectedCodec);
                    if (string.IsNullOrWhiteSpace(slot.WebRtcCodecPreference))
                        slot.WebRtcCodecPreference = ResolveWebRtcCodec(slot.PreferredCodec);
                    sessionPrimaryTier = ParseTier(session.Primary);
                    slot.PreferredPrimaryTier = sessionPrimaryTier ?? StreamTier.Rtsp;
                    slot.PreferredFallbackTier = StreamTier.Rtsp;

                    // WebRTC tier
                    if (!string.IsNullOrWhiteSpace(session.WebRtc?.SfuUrl))
                    {
                        slot.WebRtcSfuUrl = session.WebRtc.SfuUrl;

                        slot.WebRtcRoomId =
                            !string.IsNullOrWhiteSpace(session.WebRtc.RoomId)
                                ? session.WebRtc.RoomId
                                : cam.Id;

                        slot.WebRtcTimeoutMs =
                            session.WebRtc.ConnectTimeoutMs > 0
                                ? session.WebRtc.ConnectTimeoutMs
                                : (session.FallbackPolicy?.WebRtcConnectTimeoutMs > 0 ? session.FallbackPolicy.WebRtcConnectTimeoutMs : 5000);

                        slot.WebRtcTrackTimeoutMs =
                            session.FallbackPolicy?.WebRtcTrackTimeoutMs > 0
                                ? session.FallbackPolicy.WebRtcTrackTimeoutMs
                                : 2500;
                    }
                }

                // If WebRTC is available, prefer it first.
                if (!string.IsNullOrEmpty(slot.WebRtcSfuUrl))
                {
                    slot.ActiveTier = StreamTier.WebRtc;
                    VideoService.Log($"[TS-VMS] Tier=WebRtc cam={slot.CameraName} codec={slot.WebRtcCodecPreference} preferred=desktop sfuUrl={slot.WebRtcSfuUrl}");
                    if (!string.IsNullOrWhiteSpace(resolvedSub))
                        _subStreamCache[cam.Id] = resolvedSub;
                    return;
                }

                if (sessionPrimaryTier == StreamTier.WebRtc && !string.IsNullOrEmpty(slot.WebRtcSfuUrl))
                {
                    slot.ActiveTier = StreamTier.WebRtc;
                    VideoService.Log($"[TS-VMS] Tier=WebRtc cam={slot.CameraName} codec={slot.WebRtcCodecPreference} primary=session sfuUrl={slot.WebRtcSfuUrl}");
                    if (!string.IsNullOrWhiteSpace(resolvedSub))
                        _subStreamCache[cam.Id] = resolvedSub;
                    return;
                }
                if (sessionPrimaryTier == StreamTier.Rtsp && !string.IsNullOrEmpty(slot.RtspUrl))
                {
                    slot.ActiveTier = StreamTier.Rtsp;
                    VideoService.Log($"[TS-VMS] Tier=Rtsp cam={slot.CameraName} codec={slot.PreferredCodec} primary=session");
                    if (!string.IsNullOrWhiteSpace(resolvedSub))
                        _subStreamCache[cam.Id] = resolvedSub;
                    return;
                }
            }
            catch (Exception ex)
            {
                VideoService.Log($"[TS-VMS] live/start failed cam={cam.Name} id={cam.Id}: {ex.Message}");
            }

            // --- Set initial tier based on what is available ---
            bool preferWebRtc = !string.IsNullOrEmpty(slot.WebRtcSfuUrl);

            if (preferWebRtc)
            {
                slot.ActiveTier = StreamTier.WebRtc;
                VideoService.Log($"[TS-VMS] Tier=WebRtc cam={slot.CameraName} codec={slot.WebRtcCodecPreference} sfuUrl={slot.WebRtcSfuUrl}");
            }
            else if (!string.IsNullOrEmpty(slot.RtspUrl))
            {
                slot.ActiveTier = StreamTier.Rtsp;
                VideoService.Log($"[TS-VMS] Tier=Rtsp cam={slot.CameraName} codec={slot.PreferredCodec}");
            }
            else
            {
                slot.ActiveTier = StreamTier.Rtsp;
                VideoService.Log($"[TS-VMS] Tier=Rtsp cam={slot.CameraName} (no WebRTC)");
            }

            if (!string.IsNullOrWhiteSpace(resolvedSub))
                _subStreamCache[cam.Id] = resolvedSub;
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
                    if (!string.IsNullOrWhiteSpace(resolvedSub) &&
                        !existing.RtspRetriedWithMain &&
                        existing.RtspUrl != resolvedSub)
                    {
                        existing.RtspUrl = resolvedSub;
                        _subStreamCache[cam.Id] = resolvedSub;
                    }

                    existing.HasAudioCapability = cam.Capabilities?.HasAudio ?? false;
                    UpdateIp(existing, cam);
                    if (!WebRtcEnabled && existing.ActiveTier == StreamTier.WebRtc)
                    {
                        existing.ActiveTier = StreamTier.Rtsp;
                        existing.IsWebRtcStarted = false;
                    }
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

            FullScreenRtspUrl = FullScreenUrl;

            FullScreenHasAudio = slot.HasAudioCapability;
            var mainVm = _serviceProvider.GetRequiredService<MainViewModel>();
            mainVm.IsKioskMode = true; 
            UpdateAudioStates();
        }

        [RelayCommand]
        public async Task ExitFullScreen() 
        {
            FullScreenUrl = string.Empty;
            FullScreenRtspUrl = string.Empty;
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
                    if (IsFullScreen) shouldPlayAudio = (FullScreenUrl == slot.RtspUrl || FullScreenUrl == FullScreenRtspUrl);
                    _videoService.SetVolume(slot.PipelineHandle, shouldPlayAudio ? 1.0 : 0.0);
                }
                slot.IsAudioPlaying = shouldPlayAudio; 
            }
        }

        // ── WebRTC ─────────────────────────────────────────────────────────────────

        public async Task CompleteReconnectAsync(CameraSlot slot, string result)
        {
            if (slot == null || !slot.IsReconnectInProgress) return;

            VideoService.Log($"[TS-VMS] Reconnect result cam={slot.CameraName}: {result}");
            slot.TransientStatusText = "Reconnected";
            await ClearReconnectStatusAsync(slot);
        }

        public async Task FailReconnectAsync(CameraSlot slot, string reason)
        {
            if (slot == null || !slot.IsReconnectInProgress) return;

            VideoService.Log($"[TS-VMS] Reconnect failed cam={slot.CameraName}: {reason}");
            slot.TransientStatusText = "Reconnect failed";
            await ClearReconnectStatusAsync(slot);
        }

        private async Task ClearReconnectStatusAsync(CameraSlot slot)
        {
            await Task.Delay(1400);
            slot.TransientStatusText = "";
            slot.IsReconnectInProgress = false;
        }

        /// <summary>Called by LiveView code-behind when the WebRTC page posts a failure message.</summary>
        public void OnWebRtcFailed(CameraSlot slot, string reason)
        {
            VideoService.Log($"[TS-VMS] WebRTC failed cam={slot.CameraName}: {reason}");
            AdvanceToNextTier(slot, "WebRTC: " + reason);
        }

        // ── Tier cascade ────────────────────────────────────────────────────────

        private void AdvanceToNextTier(CameraSlot slot, string reason)
        {
            VideoService.Log(
                $"[TS-VMS] Tier advance {slot.ActiveTier} → next for {slot.CameraName} ({reason})");

            // --- Phase 3.5: H.265 -> H.264 SFU Transcode Fallback ---
            // If H.265 WebRTC failed (e.g. GPU out of resources or SFU 501), 
            // try one more time with H.264 preference before abandoning the WebRTC tier.
            if (WebRtcEnabled &&
                slot.ActiveTier == StreamTier.WebRtc && 
                slot.WebRtcCodecPreference == "H265" && 
                !slot.WebRtcRetriedWithH264)
            {
                VideoService.Log($"[TS-VMS] WebRTC H.265 failed for {slot.CameraName}, retrying with H.264 preference.");
                slot.WebRtcRetriedWithH264 = true;
                slot.WebRtcCodecPreference = "H264";
                if (slot.IsReconnectInProgress)
                {
                    slot.TransientStatusText = "Reconnecting via H264...";
                }

                System.Windows.Application.Current?.Dispatcher.InvokeAsync(async () =>
                {
                    slot.IsConnected = false;
                    await System.Threading.Tasks.Task.Delay(500);
                    slot.IsConnected = true;
                });
                return;
            }

            // Stop current GStreamer pipeline (WebRTC pipeline is inside WebView2)
            var oldHandle = slot.PipelineHandle;
            if (oldHandle != IntPtr.Zero)
            {
                slot.PipelineHandle = IntPtr.Zero;
                _ = _videoService.StopStreamAsync(oldHandle);
            }

            // Determine next tier
            var next = WebRtcEnabled && slot.ActiveTier == StreamTier.WebRtc
                ? StreamTier.WebRtc
                : StreamTier.Rtsp;

            // Check that the next tier has a URL
            string nextUrl = next == StreamTier.Rtsp ? slot.RtspUrl : "";
            if (string.IsNullOrEmpty(nextUrl))
            {
                slot.IsStreamFailed    = true;
                slot.StreamErrorMessage = "No stream URL available for any tier";
                if (slot.IsReconnectInProgress)
                {
                    _ = FailReconnectAsync(slot, "No stream URL available for any tier");
                }
                OnPropertyChanged(nameof(ActiveStreamCount));
                return;
            }

            slot.ActiveTier = next;
            slot.IsStreamFailed    = false;
            slot.StreamErrorMessage = "";
            if (slot.IsReconnectInProgress)
            {
                slot.TransientStatusText = $"Reconnecting via {next}...";
            }
            if (next != StreamTier.WebRtc)
            {
                slot.IsWebRtcStarted = false;
            }

            // Re-trigger the IsVisible path by toggling IsConnected
            // Staggered delay to allow GStreamer teardown to complete NULL state transition.
            System.Windows.Application.Current?.Dispatcher.InvokeAsync(async () =>
            {
                slot.IsConnected = false;
                await Task.Delay(1000);
                slot.IsConnected = true;
            });
            OnPropertyChanged(nameof(ActiveStreamCount));
        }

        private static bool IsPermanentGStreamerError(string msg) =>
            msg.Contains("Not Found",               StringComparison.OrdinalIgnoreCase)
         || msg.Contains("Unauthorized",            StringComparison.OrdinalIgnoreCase)
         || msg.Contains("Forbidden",               StringComparison.OrdinalIgnoreCase)
         || msg.Contains("Could not connect",       StringComparison.OrdinalIgnoreCase)
         || msg.Contains("404", StringComparison.Ordinal)
         || msg.Contains("401", StringComparison.Ordinal);

        private static bool IsRtspSetupError(string msg) =>
            msg.Contains("setup failed", StringComparison.OrdinalIgnoreCase)
         || msg.Contains("Could not write to resource", StringComparison.OrdinalIgnoreCase)
         || msg.Contains("open_from_sdp", StringComparison.OrdinalIgnoreCase)
         || msg.Contains("setup_streams_start", StringComparison.OrdinalIgnoreCase)
         || msg.Contains("404", StringComparison.Ordinal)
         || msg.Contains("Not Found", StringComparison.OrdinalIgnoreCase);

        private static string StripRtspQuery(string url)
        {
            if (string.IsNullOrWhiteSpace(url)) return "";
            int idx = url.IndexOf('?');
            return idx >= 0 ? url[..idx] : url;
        }

        private void OnStreamError(IntPtr windowHandle, string message)
        {
            var slot = CameraGrid.FirstOrDefault(s => s.WindowHandle == windowHandle);
            if (slot == null) return;

            if (slot.ActiveTier == StreamTier.Rtsp && IsRtspSetupError(message))
            {
                if (string.Equals(slot.RtspTransport, "udp", StringComparison.OrdinalIgnoreCase) &&
                    !slot.RtspRetriedWithTcp &&
                    !message.Contains("404", StringComparison.OrdinalIgnoreCase) &&
                    !message.Contains("Not Found", StringComparison.OrdinalIgnoreCase))
                {
                    VideoService.Log($"[TS-VMS] RTSP UDP setup failed for {slot.CameraName}; switching future starts to TCP.");
                    slot.RtspRetriedWithTcp = true;
                    slot.RtspTransport = "tcp";
                }
                else if (string.Equals(slot.RtspTransport, "tcp", StringComparison.OrdinalIgnoreCase) &&
                         !slot.RtspRetriedWithUdp)
                {
                    VideoService.Log($"[TS-VMS] RTSP TCP setup failed for {slot.CameraName}; retrying once with UDP.");
                    slot.RtspRetriedWithUdp = true;
                    slot.RtspTransport = "udp";
                }
                else if (!slot.RtspRetriedWithoutQuery)
                {
                    string queryless = StripRtspQuery(slot.RtspUrl);
                    if (!string.IsNullOrWhiteSpace(queryless) &&
                        !string.Equals(queryless, slot.RtspUrl, StringComparison.OrdinalIgnoreCase))
                    {
                        VideoService.Log($"[TS-VMS] RTSP query-string fallback for {slot.CameraName}; retrying with '{queryless}'.");
                        slot.RtspRetriedWithoutQuery = true;
                        slot.RtspUrl = queryless;
                    }
                }

                if (!slot.RtspRetriedWithMain &&
                    !string.IsNullOrWhiteSpace(slot.MainRtspUrl) &&
                    !string.Equals(slot.RtspUrl, slot.MainRtspUrl, StringComparison.OrdinalIgnoreCase))
                {
                    VideoService.Log($"[TS-VMS] RTSP sub-stream failed for {slot.CameraName}; retrying once with main stream.");
                    slot.RtspRetriedWithMain = true;
                    slot.RtspUrl = slot.MainRtspUrl;

                    var retrySlot = slot;
                    System.Windows.Application.Current?.Dispatcher.InvokeAsync(async () =>
                    {
                        await Task.Delay(1200);
                        if (retrySlot.ActiveTier == StreamTier.Rtsp &&
                            retrySlot.RtspRetriedWithMain &&
                            string.Equals(retrySlot.RtspUrl, retrySlot.MainRtspUrl, StringComparison.OrdinalIgnoreCase))
                        {
                            retrySlot.IsStreamFailed = false;
                            retrySlot.StreamErrorMessage = "";
                            retrySlot.IsConnected = true;
                        }
                    });
                    slot.IsConnected = false;
                    slot.IsStreamFailed = true;
                    slot.StreamErrorMessage = message;
                    if (slot.PipelineHandle != IntPtr.Zero)
                    {
                        var h = slot.PipelineHandle;
                        slot.PipelineHandle = IntPtr.Zero;
                        _videoService.StopStream(h);
                    }
                    OnPropertyChanged(nameof(ActiveStreamCount));
                    return;
                }
            }

            slot.IsConnected       = false;
            slot.IsStreamFailed    = true;
            slot.StreamErrorMessage = message;

            if (slot.PipelineHandle != IntPtr.Zero)
            {
                var h = slot.PipelineHandle;
                slot.PipelineHandle = IntPtr.Zero;
                _videoService.StopStream(h);
            }

            OnPropertyChanged(nameof(ActiveStreamCount));
        }
    }
}
