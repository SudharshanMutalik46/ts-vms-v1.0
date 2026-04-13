using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.IO;
using System.Linq;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Threading;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class PlaybackTileSlot : ObservableObject
    {
        [ObservableProperty] private int _slotIndex;
        [ObservableProperty] private string _cameraId = string.Empty;
        [ObservableProperty] private string _cameraName = "Select a camera";
        [ObservableProperty] private string _ipAddress = string.Empty;
        [ObservableProperty] private string _statusText = "No camera selected";
        [ObservableProperty] private bool _hasCamera;
    }

    public partial class PlaybackCameraChoice : ObservableObject
    {
        public CameraModel Camera { get; init; } = new();
        public string Name => Camera.Name;

        [ObservableProperty] private bool _isSelected;
    }

    public partial class PlaybackTimelineRow : ObservableObject
    {
        [ObservableProperty] private int _slotIndex;
        [ObservableProperty] private string _label = "";
        [ObservableProperty] private string _cameraName = "";
        [ObservableProperty] private bool _isVisible;
        [ObservableProperty] private double _widthPx = 900;

        public ObservableCollection<TimelineSegmentItem> Segments { get; } = new();
        public ObservableCollection<TimelineTickItem> Ticks { get; } = new();
    }

    public partial class PlaybackViewModel : ObservableObject
    {
        private const int MaxPlaybackTiles = 4;
        private const double SecondarySeekSafetyThresholdSeconds = 0.0;
        private readonly ApiClient _apiClient;
        private readonly CameraService _cameraService;
        private readonly PlaybackEngineService _playbackEngineService;
        private readonly PlaybackManifestService _manifestService;
        private readonly RecordingService _recordingService;
        private readonly DispatcherTimer _pollTimer;
        private int _pollInFlight;
        private int _suspendPolling;
        private int _attachInFlight;
        private int _loadToken;
        private int _lastHostWidth;
        private int _lastHostHeight;
        private CancellationTokenSource? _loadCts;
        private CancellationTokenSource? _switchCts;
        private int _switchToken;
        private string _pendingCameraId = string.Empty;
        private readonly Dictionary<string, CachedSession> _segmentCache = new();
        private static readonly TimeSpan SegmentCacheTtl = TimeSpan.FromMinutes(2);
        private readonly List<PlaybackEngineService> _secondaryPlaybackEngines = new();
        private readonly List<SemaphoreSlim> _secondaryNativeOpGates = new();
        private readonly List<PlaybackSessionModel?> _secondarySessions = new();
        private readonly List<int> _secondaryHostWidths = new();
        private readonly List<int> _secondaryHostHeights = new();
        private readonly List<bool> _secondaryHostsAttached = new();
        private readonly List<CameraModel> _selectedPlaybackCameras = new();
        private readonly List<bool> _secondaryPendingReload = new() { false, false, false };
        private readonly SemaphoreSlim _selectionChangeGate = new(1, 1);
        private readonly SemaphoreSlim _loadSegmentsGate = new(1, 1);
        private List<string> _lastLoadedPlaybackCameraIds = new();
        private string _lastLoadedCameraId = string.Empty;
        private DateTime _lastLoadedDayLocal = DateTime.MinValue;
        private DateTime _lastLoadedWindowFromLocal = DateTime.MinValue;
        private DateTime _lastLoadedWindowToLocal = DateTime.MinValue;
        private DateTime _lastUiUpdateUtc = DateTime.MinValue;
        private double _lastUiTimelineSeconds = -1;
        private string _lastUiWallClock = string.Empty;
        private bool _suppressSelectedCameraChangedLoad;

        private double _lastEnginePositionSeconds = -1;
        private int _lastEnginePlaylistIndex = -1;
        private DateTime _lastEngineMotionUtc = DateTime.MinValue;


        private bool _initialized;
        private int _currentSegmentIndex = -1;

        private double _savedPlaybackPosition = 0;
        private int _savedSegmentIndex = -1;
        private bool _wasPlayingBeforeDeactivate = false;
        private bool _hasResumeState = false;
        private bool _isDeactivating = false;
        private bool _hostAttached = false;

        private readonly SemaphoreSlim _nativeOpGate = new(1, 1);

        private double _desiredPlaybackRate = 1.0;
        
        private bool _shouldBePlaying = false;

        private bool _isUpdatingUI = false; // Slider Re-entrancy protection
        private DateTime _lastTransitionTime = DateTime.Now;

        private PlaybackSessionModel? _currentSession;




        // Stronger validation so we don't restore the wrong session
        private string _resumeCameraId = string.Empty;
        private DateTime _resumeDayLocal = DateTime.MinValue;

        private sealed class CachedSession
        {
            public string CameraId { get; init; } = string.Empty;
            public DateTime FromUtc { get; init; }
            public DateTime ToUtc { get; init; }
            public PlaybackSessionModel Session { get; init; } = default!;
            public DateTime CachedAtUtc { get; init; }
        }

        public ObservableCollection<CameraModel> AvailableCameras { get; } = new();
        public ObservableCollection<PlaybackCameraChoice> AvailablePlaybackCameras { get; } = new();
        public ObservableCollection<RecordingSegment> RecordingSegments { get; } = new();
        public ObservableCollection<PlaybackTileSlot> PlaybackSlots { get; } = new();

        // GREEN segments positioned on red base
        public ObservableCollection<TimelineSegmentItem> TimelineSegments { get; } = new();
        public ObservableCollection<TimelineTickItem> TimelineTicks { get; } = new();
        public ObservableCollection<PlaybackTimelineRow> PlaybackTimelines { get; } = new();

        [ObservableProperty] private CameraModel? _selectedCamera;

        [ObservableProperty] private string _cameraSearchText = "";

        [ObservableProperty] private string _statusMessage = "Select a camera, pick a day/time window, then play.";
        [ObservableProperty] private bool _isLoading;
        [ObservableProperty] private int _selectedPlaybackCount;

        // Wall-clock time display
        [ObservableProperty] private string _currentWallClockText = "--:--:--";
        [ObservableProperty] private string _coverageSummaryText = "Coverage: - | Gaps: -";
        [ObservableProperty] private string _windowSummaryText = "";

        // Overlay
        [ObservableProperty] private bool _showPlayerOverlay = true;
        [ObservableProperty] private string _playerOverlayTitle = "Select a time to start playback";
        [ObservableProperty] private string _playerOverlaySubtitle = "Use the timeline (green = recording, red = no recording). Double-click to play.";

        // Playback state
        [ObservableProperty] private bool _isPlaying;
        [ObservableProperty] private bool _hasSegments;
        [ObservableProperty] private bool _hasMediaLoaded;

        [ObservableProperty] private string _playbackRateText = "1x";
        [ObservableProperty] private double _videoAspectRatio = 16.0 / 9.0;

        public bool IsSinglePlaybackLayout => SelectedPlaybackCount <= 1;
        public bool IsMultiPlaybackLayout => SelectedPlaybackCount > 1;

        // Window + Calendar
        [ObservableProperty] private DateTime _selectedDayLocal = DateTime.Today;
        [ObservableProperty] private string _windowFromTimeText = "00:00";
        [ObservableProperty] private string _windowToTimeText = "23:59";

        // For API diagnostic display
        [ObservableProperty] private string _selectedCameraDebugId = "";
        [ObservableProperty] private string _queryFromUtc = "";
        [ObservableProperty] private string _queryToUtc = "";
        [ObservableProperty] private string _segmentsApiUri = "";
        [ObservableProperty] private string _httpStatusText = "";
        [ObservableProperty] private string _apiRowCount = "0";
        [ObservableProperty] private bool _isDiagnosticsExpanded;

        // Advanced segment selection
        [ObservableProperty] private RecordingSegment? _selectedSegment;

        // Timeline sizing/playhead
        [ObservableProperty] private double _timelineWidthPx = 900;
        [ObservableProperty] private double _playheadLeftPx = 0;

        // Slider is now wall-clock seconds within window
        [ObservableProperty] private double _currentTimelineSeconds;
        [ObservableProperty] private double _totalTimelineSeconds = 1;

        // Jump/export (kept as you already have)
        [ObservableProperty] private string _jumpToLocalText = DateTime.Now.ToString("yyyy-MM-dd HH:mm:ss");
        [ObservableProperty] private DateTime _exportStartLocal = DateTime.Now.AddMinutes(-10);
        [ObservableProperty] private DateTime _exportEndLocal = DateTime.Now;
        [ObservableProperty] private RecordingExportResponse? _lastExport;

        public string LastExportDisplay =>
            LastExport == null
                ? "No export submitted"
                : $"Export {LastExport.JobId} | State: {LastExport.State} | URL: {LastExport.DownloadUrl ?? "(none)"}";

        public PlaybackViewModel(
            ApiClient apiClient,
            CameraService cameraService,
            PlaybackEngineService playbackEngineService,
            PlaybackManifestService manifestService,
            RecordingService recordingService)
        {
            _apiClient = apiClient;
            _cameraService = cameraService;
            _playbackEngineService = playbackEngineService;
            _manifestService = manifestService;
            _recordingService = recordingService;

            // Reduce UI churn during playback to keep the app responsive.
            _pollTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(1000) };
            _pollTimer.Tick += PollTimer_Tick;

            for (int i = 0; i < MaxPlaybackTiles; i++)
            {
                PlaybackSlots.Add(new PlaybackTileSlot { SlotIndex = i + 1 });
                PlaybackTimelines.Add(new PlaybackTimelineRow { SlotIndex = i + 1, Label = $"C{i + 1}", IsVisible = false });

                if (i == 0)
                    continue;

                _secondaryPlaybackEngines.Add(new PlaybackEngineService());
                _secondaryNativeOpGates.Add(new SemaphoreSlim(1, 1));
                _secondarySessions.Add(null);
                _secondaryHostWidths.Add(0);
                _secondaryHostHeights.Add(0);
                _secondaryHostsAttached.Add(false);
            }

            IsDiagnosticsExpanded = false;
        }

        private async Task RunNativeAsync(Action action, string name = "unnamed")
        {
            await _nativeOpGate.WaitAsync();
            try
            {
                await Task.Run(action);
            }
            finally
            {
                _nativeOpGate.Release();
            }
        }

        private async Task<T> RunNativeAsync<T>(Func<T> func, string name = "unnamed")
        {
            await _nativeOpGate.WaitAsync();
            try
            {
                return await Task.Run(func);
            }
            finally
            {
                _nativeOpGate.Release();
            }
        }

        private async Task RunSecondaryNativeAsync(int secondaryIndex, Action action, string name = "unnamed")
        {
            if (secondaryIndex < 0 || secondaryIndex >= _secondaryNativeOpGates.Count)
                return;

            var gate = _secondaryNativeOpGates[secondaryIndex];
            await gate.WaitAsync();
            try
            {
                await Task.Run(action);
            }
            finally
            {
                gate.Release();
            }
        }

        public IReadOnlyList<CameraModel> GetSelectedPlaybackCameras()
        {
            return _selectedPlaybackCameras.ToList();
        }

        public async Task SetSelectedPlaybackCamerasAsync(IReadOnlyList<CameraModel> cameras)
        {
            await _selectionChangeGate.WaitAsync();
            try
            {
                var normalized = cameras
                    .Where(c => c != null && !string.IsNullOrWhiteSpace(c.Id))
                    .GroupBy(c => c.Id, StringComparer.OrdinalIgnoreCase)
                    .Select(g => g.First())
                    .Take(MaxPlaybackTiles)
                    .ToList();

                _selectedPlaybackCameras.Clear();
                _selectedPlaybackCameras.AddRange(normalized);
                UpdatePlaybackSlotsFromSelection();
                LogPlaybackDebug(
                    normalized.Count == 0
                        ? "PLAYBACK_SELECTION cleared"
                        : $"PLAYBACK_SELECTION count={normalized.Count} cameras={string.Join(", ", normalized.Select((camera, index) => $"slot{index + 1}:{camera.Name}[{camera.Id}]"))}");

                var primary = normalized.FirstOrDefault();
                if (primary == null)
                {
                    _suppressSelectedCameraChangedLoad = true;
                    SelectedCamera = null;
                    _suppressSelectedCameraChangedLoad = false;
                    await StopAndClearAllPlaybackEnginesAsync();
                    ResetLoadedPlaybackStateOnly();
                    ShowPlayerOverlay = true;
                    PlayerOverlayTitle = "Select a camera to start playback";
                    PlayerOverlaySubtitle = "You can choose up to 4 cameras for synchronized playback.";
                    StatusMessage = "Select up to 4 cameras for playback.";
                    return;
                }

                if (!string.Equals(SelectedCamera?.Id, primary.Id, StringComparison.OrdinalIgnoreCase))
                {
                    _suppressSelectedCameraChangedLoad = true;
                    SelectedCamera = primary;
                    _suppressSelectedCameraChangedLoad = false;
                }

                bool canReusePrimarySession =
                    _currentSession != null &&
                    string.Equals(_lastLoadedCameraId, primary.Id, StringComparison.OrdinalIgnoreCase) &&
                    _lastLoadedDayLocal == SelectedDayLocal.Date &&
                    _lastLoadedWindowFromLocal == WindowFromLocal() &&
                    _lastLoadedWindowToLocal == WindowToLocal();

                if (canReusePrimarySession)
                {
                    LogPlaybackDebug(
                        $"PLAYBACK_SELECTION_REUSE primary={primary.Name}[{primary.Id}] count={normalized.Count} timelineSeconds={CurrentTimelineSeconds:0.###}");
                    await LoadSecondarySlotsAsync(
                        _currentSession.WindowStartUtc,
                        _currentSession.WindowEndUtc,
                        CurrentTimelineSeconds,
                        autoPlay: _shouldBePlaying || IsPlaying,
                        CancellationToken.None,
                        Volatile.Read(ref _loadToken));
                    CaptureLoadedPlaybackSelection();
                    return;
                }

                await Task.Yield();
                await LoadSegmentsAsync(primary.Id);
            }
            finally
            {
                _selectionChangeGate.Release();
            }
        }

        private void UpdatePlaybackSlotsFromSelection()
        {
            SelectedPlaybackCount = _selectedPlaybackCameras.Count;
            OnPropertyChanged(nameof(IsSinglePlaybackLayout));
            OnPropertyChanged(nameof(IsMultiPlaybackLayout));

            for (int i = 0; i < PlaybackSlots.Count; i++)
            {
                var slot = PlaybackSlots[i];
                if (i < _selectedPlaybackCameras.Count)
                {
                    var camera = _selectedPlaybackCameras[i];
                    slot.CameraId = camera.Id;
                    slot.CameraName = camera.Name;
                    slot.IpAddress = camera.IpAddress ?? string.Empty;
                    slot.StatusText = i == 0 ? "Primary playback" : "Synchronized playback";
                    slot.HasCamera = true;
                }
                else
                {
                    slot.CameraId = string.Empty;
                    slot.CameraName = "Select a camera";
                    slot.IpAddress = string.Empty;
                    slot.StatusText = "No camera selected";
                    slot.HasCamera = false;
                }
            }
        }

        private async Task RefreshPlaybackUiFromEngineAsync()
        {
            if (_currentSegmentIndex < 0 || _currentSegmentIndex >= RecordingSegments.Count)
                return;

            var snapshot = await RunNativeAsync(() =>
            {
                int state = _playbackEngineService.GetState();
                double localSeconds = _playbackEngineService.GetPositionSeconds();
                bool eosReached = _playbackEngineService.HasReachedEos();
                var videoSize = _playbackEngineService.GetVideoSize();
                return (state, localSeconds, eosReached, videoSize.width, videoSize.height);
            }, "RefreshUi_GetStatePos");

            IsPlaying = snapshot.state == 2 && !snapshot.eosReached;

            if (snapshot.width > 0 && snapshot.height > 0)
            {
                VideoAspectRatio = Math.Max(0.3, Math.Min(3.5, (double)snapshot.width / snapshot.height));
            }

            var seg = RecordingSegments[_currentSegmentIndex];
            double local = Math.Max(0, snapshot.localSeconds);
            var posUtc = seg.StartTs.AddSeconds(local);

            CurrentWallClockText = posUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");

            _isUpdatingUI = true;
            CurrentTimelineSeconds = Math.Max(0, (posUtc - WindowStartUtc()).TotalSeconds);
            _isUpdatingUI = false;

            UpdatePlayheadPx();
        }



        private async Task ApplyPlaybackOptionsAfterLoadAsync(bool shouldPlay)
        {
            await ApplyDesiredRateAsync(shouldPlay);
        }

        private async Task LoadSegmentForCurrentModeAsync(int index, double localOffsetSeconds = 0)
        {
            bool shouldPlay = IsPlaying;

            if (shouldPlay)
                await LoadAndPlaySegmentAsync(index, localOffsetSeconds);
            else
                await LoadSegmentPausedAsync(index, localOffsetSeconds);
        }

        public async Task InitializeAsync()
        {
            if (!_initialized)
            {
                _initialized = true;

                try
                {
                    _playbackEngineService.EnsureNativeDllPresent(AppDomain.CurrentDomain.BaseDirectory);
                }
                catch (Exception ex)
                {
                    StatusMessage = ex.Message;
                }
            }

            // Always refresh cameras on page entry / relogin.
            await LoadCamerasAsync();
            UpdateWindowSummaryText();
            BuildTimelineTicks();
        }

        public async Task AttachVideoHostAsync(IntPtr hwnd)
        {
            if (hwnd == IntPtr.Zero) return;
            if (Interlocked.Exchange(ref _attachInFlight, 1) == 1) return;

            try
            {
                LogPlaybackDebug($"PLAYBACK_HOST_ATTACH slot=1 hwnd={hwnd}");
                await Task.Run(() => _playbackEngineService.AttachHost(hwnd));
                _hostAttached = true;
                _pollTimer.Start();

                if (_lastHostWidth > 0 && _lastHostHeight > 0)
                {
                    await RunNativeAsync(
                        () => _playbackEngineService.RebindHost(_lastHostWidth, _lastHostHeight),
                        "Attach_RebindHost");
                }

                await RunNativeAsync(() => _playbackEngineService.ForceExpose(), "Attach_ForceExpose");

                if (_currentSession != null && _currentSegmentIndex >= 0 && _currentSegmentIndex < RecordingSegments.Count)
                {
                    var landedUtc = _currentSession.WindowStartUtc.AddSeconds(CurrentTimelineSeconds);
                    var seek = _manifestService.Resolve(_currentSession, CurrentTimelineSeconds);
                    if (seek != null)
                    {
                        if (_shouldBePlaying || IsPlaying)
                            await LoadAndPlaySegmentAsync(seek.SegmentIndex, seek.LocalOffsetSeconds);
                        else
                            await LoadSegmentPausedAsync(seek.SegmentIndex, seek.LocalOffsetSeconds);
                    }
                }
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
            finally
            {
                Interlocked.Exchange(ref _attachInFlight, 0);
            }
        }

        public void AttachVideoHost(IntPtr hwnd)
        {
            _ = AttachVideoHostAsync(hwnd);
        }

        public async Task UpdateVideoHostSizeAsync(int width, int height)
        {
            if (width > 0 && height > 0)
            {
                _lastHostWidth = width;
                _lastHostHeight = height;
            }

            if (!_hostAttached)
                return;

            try
            {
                await RunNativeAsync(() => _playbackEngineService.RebindHost(width, height), "UpdateRebind");
                await RunNativeAsync(() => _playbackEngineService.ForceExpose(), "Update_ForceExpose");
            }
            catch
            {
                // Ignore resize races during layout churn.
            }
        }

        public async Task AttachSecondaryVideoHostAsync(int slotIndex, IntPtr hwnd)
        {
            int secondaryIndex = slotIndex - 1;
            if (secondaryIndex < 0 || secondaryIndex >= _secondaryPlaybackEngines.Count || hwnd == IntPtr.Zero)
                return;

            try
            {
                LogPlaybackDebug($"PLAYBACK_HOST_ATTACH slot={slotIndex + 1} hwnd={hwnd}");
                await Task.Run(() => _secondaryPlaybackEngines[secondaryIndex].AttachHost(hwnd));
                _secondaryHostsAttached[secondaryIndex] = true;

                int width = _secondaryHostWidths[secondaryIndex];
                int height = _secondaryHostHeights[secondaryIndex];
                if (width > 0 && height > 0)
                {
                    await RunSecondaryNativeAsync(
                        secondaryIndex,
                        () =>
                        {
                            _secondaryPlaybackEngines[secondaryIndex].RebindHost(width, height);
                            _secondaryPlaybackEngines[secondaryIndex].ForceExpose();
                        },
                        $"AttachSecondary_{slotIndex}_Rebind");
                }

                await RunSecondaryNativeAsync(
                    secondaryIndex,
                    () => _secondaryPlaybackEngines[secondaryIndex].ForceExpose(),
                    $"AttachSecondary_{slotIndex}_ForceExpose");

                if (_secondaryPendingReload[secondaryIndex])
                {
                    LogPlaybackDebug(
                        $"PLAYBACK_HOST_ATTACH_RELOAD slot={slotIndex + 1} pendingReload={_secondaryPendingReload[secondaryIndex]} hasSession={_secondarySessions[secondaryIndex] != null}");
                    await LoadSecondarySlotAtWindowSecondsAsync(
                        slotIndex,
                        CurrentTimelineSeconds,
                        _shouldBePlaying || IsPlaying);
                }
            }
            catch (Exception ex)
            {
                LogPlaybackDebug($"PLAYBACK_HOST_ATTACH_ERROR slot={slotIndex + 1} error={ex.Message}");
                // Secondary panes should not block primary playback.
            }
        }

        public async Task UpdateSecondaryVideoHostSizeAsync(int slotIndex, int width, int height)
        {
            int secondaryIndex = slotIndex - 1;
            if (secondaryIndex < 0 || secondaryIndex >= _secondaryPlaybackEngines.Count)
                return;

            if (width > 0 && height > 0)
            {
                _secondaryHostWidths[secondaryIndex] = width;
                _secondaryHostHeights[secondaryIndex] = height;
            }

            if (!_secondaryHostsAttached[secondaryIndex])
                return;

            try
            {
                await RunSecondaryNativeAsync(
                    secondaryIndex,
                    () =>
                    {
                        _secondaryPlaybackEngines[secondaryIndex].RebindHost(width, height);
                        _secondaryPlaybackEngines[secondaryIndex].ForceExpose();
                    },
                    $"ResizeSecondary_{slotIndex}_Rebind");
            }
            catch
            {
                // Ignore layout churn on secondary panes.
            }
        }

        private async Task ScheduleLoadForCameraAsync(string cameraId)
        {
            try
            {
                SafeCancel(_switchCts);
                _switchCts = new CancellationTokenSource();

                int switchToken = Interlocked.Increment(ref _switchToken);
                _pendingCameraId = cameraId;

                await Task.Delay(150, _switchCts.Token);

                if (switchToken != Volatile.Read(ref _switchToken))
                    return;

                await LoadSegmentsAsync(cameraId, _switchCts.Token);
            }
            catch (OperationCanceledException)
            {
                // Switched again.
            }
            catch
            {
                // Switched again or task canceled.
            }
        }

        public void Deactivate()
        {
            _ = DeactivateAsync();
        }

        private void CaptureResumeState()
        {
            if (!HasMediaLoaded) return;
            if (_currentSegmentIndex < 0) return;
            if (SelectedCamera == null) return;

            _savedSegmentIndex = _currentSegmentIndex;
            _wasPlayingBeforeDeactivate = IsPlaying;
            _resumeCameraId = SelectedCamera.Id ?? string.Empty;
            _resumeDayLocal = SelectedDayLocal.Date;

            try
            {
                double pos = _playbackEngineService.GetPositionSeconds();
                if (double.IsNaN(pos) || double.IsInfinity(pos) || pos < 0)
                    pos = 0;

                _savedPlaybackPosition = pos;
            }
            catch
            {
                _savedPlaybackPosition = 0;
            }

            _hasResumeState = true;
        }

        private void ClearResumeState()
        {
            _savedPlaybackPosition = 0;
            _savedSegmentIndex = -1;
            _wasPlayingBeforeDeactivate = false;
            _hasResumeState = false;
            _resumeCameraId = string.Empty;
            _resumeDayLocal = DateTime.MinValue;
        }

        private static void SafeCancel(CancellationTokenSource? cts)
        {
            try
            {
                cts?.Cancel();
            }
            catch
            {
                // ignore
            }
        }

        private async Task StopAndClearEngineAsync(bool clearHostBinding = false)
        {
            try
            {
                await RunNativeAsync(() =>
                {
                    _playbackEngineService.SetLastSampleEnabled(false);
                    _playbackEngineService.Stop();
                    _playbackEngineService.ForceExpose();
                    if (clearHostBinding)
                        _playbackEngineService.AttachHost(IntPtr.Zero);
                    _playbackEngineService.ResetEngine();
                });
            }
            catch
            {
                // Engine may not be initialized yet; that's fine.
            }
            finally
            {
                if (clearHostBinding)
                {
                    _hostAttached = false;
                    _lastHostWidth = 0;
                    _lastHostHeight = 0;
                }
            }
        }

        private void CloseEngineQuietly(PlaybackEngineService service)
        {
            try
            {
                service.AttachHost(IntPtr.Zero);
            }
            catch
            {
                // Absolute safety for disposal path
            }
        }

        private async Task StopAndClearSecondaryEngineAsync(int secondaryIndex, bool clearHostBinding = false)
        {
            if (secondaryIndex < 0 || secondaryIndex >= _secondaryPlaybackEngines.Count)
                return;

            try
            {
                await RunSecondaryNativeAsync(secondaryIndex, () =>
                {
                    _secondaryPlaybackEngines[secondaryIndex].SetLastSampleEnabled(false);
                    _secondaryPlaybackEngines[secondaryIndex].Stop();
                    _secondaryPlaybackEngines[secondaryIndex].ForceExpose();
                    if (clearHostBinding)
                        _secondaryPlaybackEngines[secondaryIndex].AttachHost(IntPtr.Zero);
                    _secondaryPlaybackEngines[secondaryIndex].ResetEngine();
                }, $"StopSecondary_{secondaryIndex}");
            }
            catch
            {
                // Secondary pane may not be initialized yet.
            }
            finally
            {
                _secondarySessions[secondaryIndex] = null;
                _secondaryPendingReload[secondaryIndex] = false;
                if (clearHostBinding)
                {
                    _secondaryHostsAttached[secondaryIndex] = false;
                    _secondaryHostWidths[secondaryIndex] = 0;
                    _secondaryHostHeights[secondaryIndex] = 0;
                }
            }
        }

        private void CloseSecondaryEngineQuietly(int index)
        {
            try
            {
                _secondaryPlaybackEngines[index].AttachHost(IntPtr.Zero);
            }
            catch
            {
                // Absolute safety for disposal path
            }
        }

        private async Task StopAndClearAllPlaybackEnginesAsync(bool clearHostBindings = false)
        {
            await StopAndClearEngineAsync(clearHostBindings);
            for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
            {
                await StopAndClearSecondaryEngineAsync(i, clearHostBindings);
            }
        }

        private void ResetLoadedPlaybackStateOnly()
        {
            _currentSession = null;
            _currentSegmentIndex = -1;

            _savedPlaybackPosition = 0;
            _savedSegmentIndex = -1;
            _wasPlayingBeforeDeactivate = false;
            _hasResumeState = false;

            _shouldBePlaying = false;
            _desiredPlaybackRate = 1.0;
            PlaybackRateText = "1x";

            _lastLoadedCameraId = string.Empty;
            _lastLoadedDayLocal = DateTime.MinValue;
            _lastLoadedWindowFromLocal = DateTime.MinValue;
            _lastLoadedWindowToLocal = DateTime.MinValue;

            _lastLoadedPlaybackCameraIds.Clear();

            for (int i = 0; i < _secondaryPendingReload.Count; i++)
            {
                _secondaryPendingReload[i] = false;
            }

            SelectedSegment = null;

            RecordingSegments.Clear();
            TimelineSegments.Clear();

            HasSegments = false;
            HasMediaLoaded = false;
            IsPlaying = false;

            CurrentWallClockText = "--:--:--";
            CurrentTimelineSeconds = 0;
            PlayheadLeftPx = 0;

            CoverageSummaryText = "Coverage: - | Gaps: -";

            ShowPlayerOverlay = true;
            PlayerOverlayTitle = "Select a time to start playback";
            PlayerOverlaySubtitle = "Use the timeline (green = recording, red = no recording). Double-click to play.";

            StatusMessage = SelectedCamera == null
                ? "Select a camera, pick a day/time window, then play."
                : "Select a time to start playback";

            for (int i = 0; i < PlaybackSlots.Count; i++)
            {
                PlaybackSlots[i].StatusText = PlaybackSlots[i].HasCamera
                    ? (i == 0 ? "Primary playback" : "Synchronized playback")
                    : "No camera selected";
            }
        }

        private void CaptureLoadedPlaybackSelection()
        {
            _lastLoadedPlaybackCameraIds = _selectedPlaybackCameras
                .Select(c => c.Id)
                .Where(id => !string.IsNullOrWhiteSpace(id))
                .ToList();
        }

        private bool LoadedContextMatchesCurrentSelection()
        {
            try
            {
                if (SelectedCamera == null || _currentSession == null)
                    return false;

                var currentIds = _selectedPlaybackCameras
                    .Select(c => c.Id)
                    .Where(id => !string.IsNullOrWhiteSpace(id))
                    .ToList();

                return string.Equals(_lastLoadedCameraId, SelectedCamera.Id, StringComparison.Ordinal) &&
                       _lastLoadedDayLocal == SelectedDayLocal.Date &&
                       _lastLoadedWindowFromLocal == WindowFromLocal() &&
                       _lastLoadedWindowToLocal == WindowToLocal() &&
                       _lastLoadedPlaybackCameraIds.SequenceEqual(currentIds, StringComparer.OrdinalIgnoreCase);
            }
            catch
            {
                return false;
            }
        }

        private DateTime WindowFromLocal() => GetWindowLocal().startLocal;
        private DateTime WindowToLocal() => GetWindowLocal().endLocal;

        private async Task RestoreResumeStateAsync()
        {
            ShowPlayerOverlay = false;

            if (_wasPlayingBeforeDeactivate)
                await LoadAndPlaySegmentAsync(_savedSegmentIndex, _savedPlaybackPosition);
            else
                await LoadSegmentPausedAsync(_savedSegmentIndex, _savedPlaybackPosition);
        }

        private bool CanResumeCurrentContext()
        {
            if (!_hasResumeState) return false;
            if (SelectedCamera == null) return false;
            if (!_hostAttached) return false;

            if (!string.Equals(SelectedCamera.Id, _resumeCameraId, StringComparison.Ordinal))
                return false;

            if (SelectedDayLocal.Date != _resumeDayLocal.Date)
                return false;

            if (_savedSegmentIndex < 0)
                return false;

            if (_savedSegmentIndex >= RecordingSegments.Count)
                return false;

            return true;
        }

        public async Task DeactivateAsync()
        {
            if (_isDeactivating)
                return;

            _isDeactivating = true;

            try
            {
                _pollTimer.Stop();
                Interlocked.Exchange(ref _suspendPolling, 1);

                ClearResumeState();

                SafeCancel(_switchCts);
                SafeCancel(_loadCts);

                await StopAndClearAllPlaybackEnginesAsync(clearHostBindings: true);
                ResetLoadedPlaybackStateOnly();
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
                _isDeactivating = false;
            }
        }

        public async Task EnsureActivePlaybackAsync()
        {
            if (!_hostAttached)
                return;

            // Always refresh cameras after relogin / repeated navigation.
            await LoadCamerasAsync();

            if (AvailableCameras.Count == 0)
            {
                StatusMessage = "No cameras available.";
                ShowPlayerOverlay = true;
                PlayerOverlayTitle = "No cameras available";
                PlayerOverlaySubtitle = "Add or enable a camera, then reopen Playback.";
                return;
            }

            if (SelectedCamera == null || _selectedPlaybackCameras.Count == 0)
            {
                await StopAndClearAllPlaybackEnginesAsync();
                ResetLoadedPlaybackStateOnly();
                ShowPlayerOverlay = true;
                PlayerOverlayTitle = "Select a camera to start playback";
                PlayerOverlaySubtitle = "You can choose up to 4 cameras for synchronized playback.";
                StatusMessage = "Select up to 4 cameras for playback.";
                return;
            }

            // Only resume if it is the same exact playback context.
            if (CanResumeCurrentContext() && LoadedContextMatchesCurrentSelection())
            {
                try
                {
                    await RestoreResumeStateAsync();
                    return;
                }
                catch
                {
                    ClearResumeState();
                    await StopAndClearAllPlaybackEnginesAsync();
                    ResetLoadedPlaybackStateOnly();
                }
            }

            // Otherwise always do a clean load.
            await LoadSegmentsAsync(SelectedCamera.Id);
        }

        partial void OnSelectedCameraChanged(CameraModel? value)
        {
            if (_suppressSelectedCameraChangedLoad)
                return;

            _ = HandleSelectedCameraChangedAsync(value);
        }

        private async Task HandleSelectedCameraChangedAsync(CameraModel? value)
        {
            ClearResumeState();

            SafeCancel(_switchCts);
            SafeCancel(_loadCts);

            await StopAndClearAllPlaybackEnginesAsync();
            ResetLoadedPlaybackStateOnly();

            if (value != null)
            {
                _selectedPlaybackCameras.RemoveAll(c => string.Equals(c.Id, value.Id, StringComparison.OrdinalIgnoreCase));
                _selectedPlaybackCameras.Insert(0, value);
                while (_selectedPlaybackCameras.Count > MaxPlaybackTiles)
                    _selectedPlaybackCameras.RemoveAt(_selectedPlaybackCameras.Count - 1);
                UpdatePlaybackSlotsFromSelection();
                await ScheduleLoadForCameraAsync(value.Id);
            }
        }

        partial void OnSelectedDayLocalChanged(DateTime value)
        {
            ClearResumeState();

            // when day changes, update jump text default and load again if camera selected
            JumpToLocalText = value.ToString("yyyy-MM-dd") + " 00:00:00";
            UpdateWindowSummaryText();
            BuildTimelineTicks();

            if (SelectedCamera != null)
                _ = LoadSegmentsAsync(SelectedCamera.Id);
        }

        partial void OnWindowFromTimeTextChanged(string value)
        {
            UpdateWindowSummaryText();
            BuildTimelineTicks();
        }

        partial void OnWindowToTimeTextChanged(string value)
        {
            UpdateWindowSummaryText();
            BuildTimelineTicks();
        }

        private void UpdateWindowSummaryText()
        {
            var (startLocal, endLocal, ok, err) = GetWindowLocal();
            if (!ok)
            {
                WindowSummaryText = err;
                return;
            }
            WindowSummaryText = $"Window: {startLocal:yyyy-MM-dd HH:mm:ss}  →  {endLocal:HH:mm:ss}";
        }

        partial void OnHasMediaLoadedChanged(bool value)
        {
            TogglePlayPauseCommand.NotifyCanExecuteChanged();
            StopPlaybackCommand.NotifyCanExecuteChanged();
            JumpSecondsCommand.NotifyCanExecuteChanged();
            SetPlaybackRateCommand.NotifyCanExecuteChanged();
            StepFrameCommand.NotifyCanExecuteChanged();
            RotateLeftCommand.NotifyCanExecuteChanged();
            RotateRightCommand.NotifyCanExecuteChanged();
            ResetRotationCommand.NotifyCanExecuteChanged();
        }

        partial void OnHasSegmentsChanged(bool value)
        {
            TogglePlayPauseCommand.NotifyCanExecuteChanged();
            PrevRecordingBlockCommand.NotifyCanExecuteChanged();
            NextRecordingBlockCommand.NotifyCanExecuteChanged();
        }

        private (DateTime startLocal, DateTime endLocal, bool ok, string err) GetWindowLocal()
        {
            if (!TryParseTime(WindowFromTimeText, out var from))
                return (DateTime.MinValue, DateTime.MinValue, false, "Invalid From time (HH:mm or HH:mm:ss)");

            if (!TryParseTime(WindowToTimeText, out var to))
                return (DateTime.MinValue, DateTime.MinValue, false, "Invalid To time (HH:mm or HH:mm:ss)");

            var start = SelectedDayLocal.Date.Add(from);
            var end = SelectedDayLocal.Date.Add(to);

            if (end <= start)
                return (DateTime.MinValue, DateTime.MinValue, false, "End must be after Start");

            return (start, end, true, "");
        }

        private static bool TryParseTime(string text, out TimeSpan time)
        {
            time = default;

            if (string.IsNullOrWhiteSpace(text)) return false;
            text = text.Trim();

            // Support HH:mm or HH:mm:ss
            if (TimeSpan.TryParse(text, out time))
                return true;

            return false;
        }

        private DateTime WindowStartUtc()
        {
            var (startLocal, _, ok, _) = GetWindowLocal();
            if (!ok) return DateTime.UtcNow.AddHours(-1);
            return DateTime.SpecifyKind(startLocal, DateTimeKind.Local).ToUniversalTime();
        }

        private DateTime WindowEndUtc()
        {
            var (_, endLocal, ok, _) = GetWindowLocal();
            if (!ok) return DateTime.UtcNow;
            return DateTime.SpecifyKind(endLocal, DateTimeKind.Local).ToUniversalTime();
        }

        private void UpdatePlayheadPx()
        {
            var total = Math.Max(1, TotalTimelineSeconds);
            var ratio = Math.Max(0, Math.Min(1, CurrentTimelineSeconds / total));
            PlayheadLeftPx = ratio * Math.Max(1, TimelineWidthPx);
        }

        public void UpdateTimelineWidth(double width)
        {
            if (width > 50)
            {
                TimelineWidthPx = width - 6;
                foreach (var row in PlaybackTimelines)
                {
                    row.WidthPx = TimelineWidthPx;
                }
                RebuildCoverageTimeline();
                BuildTimelineTicks();
                UpdatePlayheadPx();
            }
        }

        private void BuildTimelineTicks()
        {
            TimelineTicks.Clear();

            var (startLocal, endLocal, ok, _) = GetWindowLocal();
            if (!ok) return;

            var startUtc = DateTime.SpecifyKind(startLocal, DateTimeKind.Local).ToUniversalTime();
            var endUtc = DateTime.SpecifyKind(endLocal, DateTimeKind.Local).ToUniversalTime();

            var totalSec = Math.Max(1, (endUtc - startUtc).TotalSeconds);
            var width = Math.Max(1, TimelineWidthPx);

            // pick tick step based on duration
            double minutes = (endUtc - startUtc).TotalMinutes;
            int stepMin =
                minutes <= 60 ? 10 :
                minutes <= 6 * 60 ? 30 :
                60;

            // Align to step
            var first = new DateTime(startLocal.Year, startLocal.Month, startLocal.Day, startLocal.Hour, startLocal.Minute, 0)
                .AddMinutes(stepMin - (startLocal.Minute % stepMin));
            if (first < startLocal) first = first.AddMinutes(stepMin);

            for (var t = first; t <= endLocal; t = t.AddMinutes(stepMin))
            {
                var utc = DateTime.SpecifyKind(t, DateTimeKind.Local).ToUniversalTime();
                var left = ((utc - startUtc).TotalSeconds / totalSec) * width;
                var tick = new TimelineTickItem
                {
                    LeftPx = left,
                    Label = t.ToString("HH:mm")
                };
                TimelineTicks.Add(tick);
            }

            // Sync ticks to each visible row if row-specific ticks are desired
            foreach (var row in PlaybackTimelines)
            {
                row.Ticks.Clear();
                if (!row.IsVisible) continue;
                foreach (var t in TimelineTicks) row.Ticks.Add(t);
            }
        }

        // ---------------- Commands ----------------

        [RelayCommand]
        public async Task LoadCamerasAsync()
        {
            try
            {
                IsLoading = true;
                await _cameraService.LoadCamerasAsync();
                AvailableCameras.Clear();
                AvailablePlaybackCameras.Clear();
                foreach (var camera in _cameraService.AllCameras)
                {
                    AvailableCameras.Add(camera);
                    AvailablePlaybackCameras.Add(new PlaybackCameraChoice
                    {
                        Camera = camera,
                        IsSelected = _selectedPlaybackCameras.Any(c => string.Equals(c.Id, camera.Id, StringComparison.OrdinalIgnoreCase))
                    });
                }

                if (AvailableCameras.Count == 0)
                {
                    _selectedPlaybackCameras.Clear();
                    UpdatePlaybackSlotsFromSelection();
                    StatusMessage = "No cameras available.";
                }
                else if (_selectedPlaybackCameras.Count == 0)
                {
                    UpdatePlaybackSlotsFromSelection();
                    _suppressSelectedCameraChangedLoad = true;
                    SelectedCamera = null;
                    _suppressSelectedCameraChangedLoad = false;
                    ShowPlayerOverlay = true;
                    PlayerOverlayTitle = "Select a camera to start playback";
                    PlayerOverlaySubtitle = "You can choose up to 4 cameras for synchronized playback.";
                    StatusMessage = "Select up to 4 cameras for playback.";
                }
            }
            catch (Exception ex)
            {
                StatusMessage = $"Failed to load cameras: {ex.Message}";
            }
            finally
            {
                IsLoading = false;
            }
        }

        // Load current selected camera with current day/window
        [RelayCommand]
        public async Task LoadSelectedCameraWindowAsync()
        {
            if (SelectedCamera == null)
            {
                StatusMessage = "Select a camera first.";
                return;
            }

            await LoadSegmentsAsync(SelectedCamera.Id);
        }

        public async Task RefreshPlaybackSurfacesAsync()
        {
            if (SelectedCamera == null || _selectedPlaybackCameras.Count == 0)
                return;

            if (_currentSession == null || !HasSegments)
            {
                await LoadSegmentsAsync(SelectedCamera.Id);
                return;
            }

            await SeekToWindowSecondsAsync(CurrentTimelineSeconds, autoPlay: IsPlaying || _shouldBePlaying);
        }

        // Presets (relative to now, but keeps SelectedDayLocal aligned to today)
        [RelayCommand]
        public void Preset15m()
        {
            var now = DateTime.Now;
            SelectedDayLocal = now.Date;
            WindowToTimeText = now.ToString("HH:mm");
            WindowFromTimeText = now.AddMinutes(-15).ToString("HH:mm");
        }

        [RelayCommand]
        public void Preset1h()
        {
            var now = DateTime.Now;
            SelectedDayLocal = now.Date;
            WindowToTimeText = now.ToString("HH:mm");
            WindowFromTimeText = now.AddHours(-1).ToString("HH:mm");
        }

        [RelayCommand]
        public void Preset6h()
        {
            var now = DateTime.Now;
            SelectedDayLocal = now.Date;
            WindowToTimeText = now.ToString("HH:mm");
            WindowFromTimeText = now.AddHours(-6).ToString("HH:mm");
        }

        [RelayCommand]
        public void Preset24h()
        {
            var now = DateTime.Now;
            SelectedDayLocal = now.Date;
            WindowToTimeText = now.ToString("HH:mm");
            WindowFromTimeText = now.AddHours(-24).ToString("HH:mm");
        }

        // Main loader (keeps existing command name binding)
        [RelayCommand]
        public async Task LoadSegmentsAsync(string cameraId, CancellationToken externalToken = default)
        {
            if (string.IsNullOrWhiteSpace(cameraId))
                return;

            try
            {
                await _loadSegmentsGate.WaitAsync(externalToken);
            }
            catch (OperationCanceledException)
            {
                return;
            }

            try
            {
                var primarySelection = AvailableCameras.FirstOrDefault(c => string.Equals(c.Id, cameraId, StringComparison.OrdinalIgnoreCase));
                if (primarySelection != null)
                {
                    _selectedPlaybackCameras.RemoveAll(c => string.Equals(c.Id, cameraId, StringComparison.OrdinalIgnoreCase));
                    _selectedPlaybackCameras.Insert(0, primarySelection);
                    while (_selectedPlaybackCameras.Count > MaxPlaybackTiles)
                        _selectedPlaybackCameras.RemoveAt(_selectedPlaybackCameras.Count - 1);
                    UpdatePlaybackSlotsFromSelection();
                }

                bool cameraChanged = !string.Equals(_lastLoadedCameraId, cameraId, StringComparison.Ordinal);
                int generation = Interlocked.Increment(ref _loadToken);

                ClearResumeState();

                // Clear old engine frame/session before building the new request.
                await StopAndClearAllPlaybackEnginesAsync();
                ResetLoadedPlaybackStateOnly();

                Interlocked.Exchange(ref _suspendPolling, 1);
                if (externalToken.IsCancellationRequested)
                {
                    Interlocked.Exchange(ref _suspendPolling, 0);
                    return;
                }

                var (startLocal, endLocal, ok, err) = GetWindowLocal();
                if (!ok)
                {
                    StatusMessage = err;
                    Interlocked.Exchange(ref _suspendPolling, 0);
                    return;
                }

                try
                {
                    IsLoading = true;
                    ShowPlayerOverlay = true;
                    PlayerOverlayTitle = "Loading...";
                    PlayerOverlaySubtitle = "Fetching recording coverage...";
                    _loadCts?.Cancel();
                    _loadCts = CancellationTokenSource.CreateLinkedTokenSource(externalToken);
                    _loadCts.CancelAfter(TimeSpan.FromSeconds(45));
                    try
                    {
                        await RunNativeAsync(() =>
                        {
                            // Prevent stale frames when switching cameras or reloads.
                            _playbackEngineService.SetLastSampleEnabled(false);
                            _playbackEngineService.Stop();
                            if (cameraChanged)
                                _playbackEngineService.ResetEngine();
                        }, "Load_Reset");
                    }
                    catch
                    {
                        // If stop fails, continue loading; we just want to clear stale frames.
                    }

                    RecordingSegments.Clear();
                    TimelineSegments.Clear();
                    HasSegments = false;
                    HasMediaLoaded = false;

                    _shouldBePlaying = false;
                    _desiredPlaybackRate = 1.0;
                    PlaybackRateText = "1x";

                    SelectedCameraDebugId = cameraId;

                    var fromUtc = DateTime.SpecifyKind(startLocal, DateTimeKind.Local).ToUniversalTime();
                    var toUtc = DateTime.SpecifyKind(endLocal, DateTimeKind.Local).ToUniversalTime();
                    _lastLoadedCameraId = cameraId;
                    _lastLoadedDayLocal = SelectedDayLocal.Date;
                    _lastLoadedWindowFromLocal = WindowFromLocal();
                    _lastLoadedWindowToLocal = WindowToLocal();

                    QueryFromUtc = fromUtc.ToString("o");
                    QueryToUtc = toUtc.ToString("o");

                    SegmentsApiUri = $"api/v1/recording/cameras/{cameraId}/segments?from={QueryFromUtc}&to={QueryToUtc}";
                    LogPlaybackDebug(
                        $"LOAD_START camera={cameraId} day={SelectedDayLocal:yyyy-MM-dd} " +
                        $"window={startLocal:yyyy-MM-dd HH:mm:ss}->{endLocal:yyyy-MM-dd HH:mm:ss} " +
                        $"utc={QueryFromUtc}->{QueryToUtc} uri={SegmentsApiUri}");
                    try
                    {
                        _currentSession = await GetOrBuildSessionAsync(cameraId, fromUtc, toUtc, _loadCts.Token, generation);
                        if (_currentSession == null)
                            return;

                        HttpStatusText = _segmentCache.ContainsKey($"{cameraId}|{fromUtc:o}|{toUtc:o}") ? "200 OK" : "200 OK";
                        ApiRowCount = _currentSession.Segments.Count.ToString();
                        LogPlaybackDebug($"LOAD_RESPONSE camera={cameraId} status=200 segments={_currentSession.Segments.Count}");
                    }
                    catch (OperationCanceledException)
                    {
                        return;
                    }
                    catch (Exception ex)
                    {
                        HttpStatusText = "ERROR";
                        ApiRowCount = "0";
                        LogPlaybackDebug($"LOAD_ERROR camera={cameraId} exception={ex}");
                        if (generation != Volatile.Read(ref _loadToken))
                            return;
                        StatusMessage = $"Segments API failed: {ex.Message}";
                        ShowPlayerOverlay = true;
                        PlayerOverlayTitle = "No Recording Available";
                        PlayerOverlaySubtitle = $"API error: {ex.Message}";
                        return;
                    }
                    if (generation != Volatile.Read(ref _loadToken))
                        return;

                    ApiRowCount = _currentSession.Segments.Count.ToString();

                    foreach (var s in _currentSession.Segments)
                        RecordingSegments.Add(s.Segment);

                    HasSegments = _currentSession.Segments.Count > 0;
                    TotalTimelineSeconds = _currentSession.TotalWindowSeconds;

                    UpdatePlayheadPx();
                    RebuildCoverageTimeline();
                    BuildTimelineTicks();
                    UpdateCoverageSummary();

                    if (!HasSegments)
                    {
                        _currentSegmentIndex = -1;
                        IsPlaying = false;
                        ShowPlayerOverlay = true;
                        PlayerOverlayTitle = "No Recording Available";
                        PlayerOverlaySubtitle = "No footage exists in the selected day/time window. Pick another date from the calendar.";
                        StatusMessage = "No recording for selected window.";
                        CurrentWallClockText = "--:--:--";
                        LogPlaybackDebug($"LOAD_EMPTY camera={cameraId} day={SelectedDayLocal:yyyy-MM-dd} no_segments");
                        return;
                    }

                    var seek = _manifestService.Resolve(_currentSession, CurrentTimelineSeconds);
                    if (seek != null)
                    {
                        _currentSegmentIndex = seek.SegmentIndex;
                        await LoadSegmentPausedAsync(_currentSegmentIndex, seek.LocalOffsetSeconds);
                        if (generation != Volatile.Read(ref _loadToken))
                            return;

                        var posUtc = seek.Segment.Segment.StartTs.AddSeconds(seek.LocalOffsetSeconds);
                        _isUpdatingUI = true;
                        CurrentTimelineSeconds = Math.Max(0, (posUtc - fromUtc).TotalSeconds);
                        _isUpdatingUI = false;
                    }
                    else
                    {
                        _currentSegmentIndex = 0;
                        await LoadSegmentPausedAsync(0, 0);
                        if (generation != Volatile.Read(ref _loadToken))
                            return;
                    }

                    await LoadSecondarySlotsAsync(fromUtc, toUtc, CurrentTimelineSeconds, autoPlay: false, _loadCts.Token, generation);

                    CaptureLoadedPlaybackSelection();
                    UpdatePlayheadPx();
                    ShowPlayerOverlay = false;
                    StatusMessage = $"Loaded recording coverage for {startLocal:yyyy-MM-dd} ({_currentSession.Segments.Count} segments).";
                    LogPlaybackDebug(
                        $"LOAD_DONE camera={cameraId} segments={_currentSession.Segments.Count} " +
                        $"timelineSeconds={_currentSession.TotalWindowSeconds:0.##} selectedIndex={_currentSegmentIndex}");
                }
                catch (Exception ex)
                {
                    HttpStatusText = "EXCEPTION";
                    ApiRowCount = "0";
                    LogPlaybackDebug($"LOAD_EXCEPTION camera={cameraId} exception={ex}");
                    if (generation != Volatile.Read(ref _loadToken))
                        return;
                    StatusMessage = $"Failed to load recording: {ex.Message}";
                    ShowPlayerOverlay = true;
                    PlayerOverlayTitle = "Playback Error";
                    PlayerOverlaySubtitle = ex.Message;
                }
                finally
                {
                    if (generation == Volatile.Read(ref _loadToken))
                    {
                        IsLoading = false;
                        UpdateWindowSummaryText();
                        Interlocked.Exchange(ref _suspendPolling, 0);
                    }
                }
            }
            finally
            {
                _loadSegmentsGate.Release();
            }
        }

        private void UpdateCoverageSummary()
        {
            var fromUtc = WindowStartUtc();
            var toUtc = WindowEndUtc();
            if (toUtc <= fromUtc)
            {
                CoverageSummaryText = "Coverage: - | Gaps: -";
                return;
            }

            var ordered = RecordingSegments.OrderBy(s => s.StartTs).ToList();

            double recordedSec = 0;
            int gaps = 0;

            DateTime cursor = fromUtc;

            foreach (var s in ordered)
            {
                var segStart = s.StartTs < fromUtc ? fromUtc : s.StartTs;
                var segEnd = s.EndTs > toUtc ? toUtc : s.EndTs;

                if (segEnd <= segStart) continue;

                if (segStart > cursor.AddSeconds(1))
                    gaps++;

                recordedSec += (segEnd - segStart).TotalSeconds;
                cursor = segEnd;
            }

            if (cursor < toUtc.AddSeconds(-1))
                gaps++;

            var recorded = TimeSpan.FromSeconds(Math.Max(0, recordedSec));
            CoverageSummaryText = $"Coverage: {recorded:hh\\:mm\\:ss} recorded  |  Gaps: {gaps}";
        }

        private void LogPlaybackDebug(string message)
        {
            try
            {
                VideoService.Log($"[TS-VMS] [Playback] {message}");
                File.AppendAllText(
                    LogPaths.ApiDebugLogPath,
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss.fff}] [Playback] {message}{Environment.NewLine}");
            }
            catch
            {
                // Debug logging must never break playback.
            }
        }

        private async Task<PlaybackSessionModel?> GetOrBuildSessionAsync(
            string cameraId,
            DateTime fromUtc,
            DateTime toUtc,
            CancellationToken token,
            int generation)
        {
            string cacheKey = $"{cameraId}|{fromUtc:o}|{toUtc:o}";
            if (_segmentCache.TryGetValue(cacheKey, out var cached) &&
                (DateTime.UtcNow - cached.CachedAtUtc) < SegmentCacheTtl)
            {
                return cached.Session;
            }

            var rawSegments = await _recordingService.GetSegmentsAsync(cameraId, fromUtc, toUtc, token);
            if (generation != Volatile.Read(ref _loadToken))
                return null;

            var session = _manifestService.Build(cameraId, fromUtc, toUtc, rawSegments);
            _segmentCache[cacheKey] = new CachedSession
            {
                CameraId = cameraId,
                FromUtc = fromUtc,
                ToUtc = toUtc,
                Session = session,
                CachedAtUtc = DateTime.UtcNow
            };
            return session;
        }

        private async Task LoadSecondarySlotsAsync(
            DateTime fromUtc,
            DateTime toUtc,
            double windowSeconds,
            bool autoPlay,
            CancellationToken token,
            int generation)
        {
            for (int slotIndex = 1; slotIndex < MaxPlaybackTiles; slotIndex++)
            {
                int secondaryIndex = slotIndex - 1;

                if (generation != Volatile.Read(ref _loadToken))
                    return;

                if (slotIndex >= _selectedPlaybackCameras.Count)
                {
                    _secondaryPendingReload[secondaryIndex] = false;
                    LogPlaybackDebug($"PLAYBACK_SLOT_CLEAR slot={slotIndex + 1}");
                    await StopAndClearSecondaryEngineAsync(secondaryIndex);
                    continue;
                }

                var camera = _selectedPlaybackCameras[slotIndex];

                try
                {
                    if (_secondarySessions[secondaryIndex] != null &&
                        !string.Equals(_secondarySessions[secondaryIndex]!.CameraId, camera.Id, StringComparison.OrdinalIgnoreCase))
                    {
                        LogPlaybackDebug(
                            $"PLAYBACK_SLOT_CAMERA_CHANGED slot={slotIndex + 1} from={_secondarySessions[secondaryIndex]!.CameraId} to={camera.Id}");
                        await StopAndClearSecondaryEngineAsync(secondaryIndex);
                    }

                    var session = await GetOrBuildSessionAsync(camera.Id, fromUtc, toUtc, token, generation);
                    if (generation != Volatile.Read(ref _loadToken))
                        return;

                    _secondarySessions[secondaryIndex] = session;

                    if (session == null || session.Segments.Count == 0)
                    {
                        _secondaryPendingReload[secondaryIndex] = false;
                        PlaybackSlots[slotIndex].StatusText = "No recording in selected window";
                        LogPlaybackDebug($"PLAYBACK_SLOT_EMPTY slot={slotIndex + 1} camera={camera.Name}[{camera.Id}]");
                        await StopAndClearSecondaryEngineAsync(secondaryIndex);
                        continue;
                    }

                    PlaybackSlots[slotIndex].StatusText = autoPlay ? "Playing in sync" : "Ready";
                    LogPlaybackDebug(
                        $"PLAYBACK_SLOT_READY slot={slotIndex + 1} camera={camera.Name}[{camera.Id}] segments={session.Segments.Count} autoPlay={autoPlay}");
                    await LoadSecondarySlotAtWindowSecondsAsync(slotIndex, windowSeconds, autoPlay);
                }
                catch (Exception ex)
                {
                    _secondaryPendingReload[secondaryIndex] = false;
                    PlaybackSlots[slotIndex].StatusText = "Failed to load";
                    LogPlaybackDebug($"PLAYBACK_SLOT_ERROR slot={slotIndex + 1} camera={camera.Name}[{camera.Id}] error={ex.Message}");
                    await StopAndClearSecondaryEngineAsync(secondaryIndex);
                }
            }

            CaptureLoadedPlaybackSelection();
        }

        private async Task LoadSecondarySlotAtWindowSecondsAsync(int slotIndex, double windowSeconds, bool autoPlay)
        {
            int generation = Volatile.Read(ref _loadToken);
            int secondaryIndex = slotIndex - 1;
            if (secondaryIndex < 0 || secondaryIndex >= _secondaryPlaybackEngines.Count)
                return;

            if (generation != Volatile.Read(ref _loadToken))
                return;

            var session = _secondarySessions[secondaryIndex];
            if (session == null || session.Segments.Count == 0)
                return;

            if (!_secondaryHostsAttached[secondaryIndex])
            {
                _secondaryPendingReload[secondaryIndex] = true;
                LogPlaybackDebug($"PLAYBACK_SLOT_WAIT_HOST slot={slotIndex + 1}");
                return;
            }

            var seek = _manifestService.Resolve(session, windowSeconds);
            if (seek == null)
            {
                _secondaryPendingReload[secondaryIndex] = false;
                PlaybackSlots[slotIndex].StatusText = "No recording at selected time";
                LogPlaybackDebug($"PLAYBACK_SLOT_NO_SEGMENT slot={slotIndex + 1} windowSeconds={windowSeconds:0.###}");
                await StopAndClearSecondaryEngineAsync(secondaryIndex);
                return;
            }

            _secondaryPendingReload[secondaryIndex] = false;

            double safeOffset = Math.Max(0, seek.LocalOffsetSeconds);
            if (seek.Segment.Segment.DurationSeconds > 0.25)
                safeOffset = Math.Min(safeOffset, seek.Segment.Segment.DurationSeconds - 0.25);
            else
                safeOffset = 0;

            int width = _secondaryHostWidths[secondaryIndex];
            int height = _secondaryHostHeights[secondaryIndex];
            double requestedRate = Math.Clamp(_desiredPlaybackRate <= 0 ? 1.0 : _desiredPlaybackRate, 0.25, 4.0);
            LogPlaybackDebug(
                $"PLAYBACK_SLOT_LOAD_START slot={slotIndex + 1} camera={PlaybackSlots[slotIndex].CameraName} segmentStart={seek.Segment.Segment.StartTs:o} offsetSeconds={safeOffset:0.###} autoPlay={autoPlay} size={width}x{height}");

                await RunSecondaryNativeAsync(secondaryIndex, () =>
                {
                    var engine = _secondaryPlaybackEngines[secondaryIndex];
                    engine.SetLastSampleEnabled(true);
                    engine.LoadSession(session, seek.SegmentIndex);
                    engine.RebindHost(width, height);
                    engine.ForceExpose();
                    engine.Play();
                }, $"LoadSecondarySlot_{slotIndex}_Init");
            LogPlaybackDebug($"PLAYBACK_SLOT_LOAD_INIT_DONE slot={slotIndex + 1}");

            if (generation != Volatile.Read(ref _loadToken))
                return;

            await Task.Delay(120);

            if (generation != Volatile.Read(ref _loadToken))
                return;

            await RunSecondaryNativeAsync(secondaryIndex, () =>
            {
                var engine = _secondaryPlaybackEngines[secondaryIndex];
                engine.Pause();
                engine.ForceExpose();
            }, $"LoadSecondarySlot_{slotIndex}_Pause");
            LogPlaybackDebug($"PLAYBACK_SLOT_LOAD_PAUSE_DONE slot={slotIndex + 1}");

            bool shouldSeekSecondary = safeOffset > 0.05 && safeOffset <= SecondarySeekSafetyThresholdSeconds;
            if (safeOffset > 0.05 && !shouldSeekSecondary)
            {
                LogPlaybackDebug(
                    $"PLAYBACK_SLOT_SEEK_SKIPPED slot={slotIndex + 1} offsetSeconds={safeOffset:0.###} threshold={SecondarySeekSafetyThresholdSeconds:0.###}");
            }

            if (shouldSeekSecondary)
            {
                if (generation != Volatile.Read(ref _loadToken))
                    return;

                await Task.Delay(120);

                if (generation != Volatile.Read(ref _loadToken))
                    return;

                await RunSecondaryNativeAsync(secondaryIndex, () =>
                {
                    var engine = _secondaryPlaybackEngines[secondaryIndex];
                    engine.Seek(safeOffset);
                    engine.ForceExpose();
                }, $"LoadSecondarySlot_{slotIndex}_Seek");
                LogPlaybackDebug($"PLAYBACK_SLOT_LOAD_SEEK_DONE slot={slotIndex + 1}");
            }

            if (generation != Volatile.Read(ref _loadToken))
                return;

            await RunSecondaryNativeAsync(secondaryIndex, () =>
            {
                var engine = _secondaryPlaybackEngines[secondaryIndex];
                engine.SetRate(requestedRate);
                if (autoPlay)
                    engine.Play();
                else
                    engine.Pause();

                engine.ForceExpose();
            }, $"LoadSecondarySlot_{slotIndex}_Finalize");
            LogPlaybackDebug(
                $"PLAYBACK_SLOT_OPENED slot={slotIndex + 1} camera={PlaybackSlots[slotIndex].CameraName} segmentStart={seek.Segment.Segment.StartTs:o} offsetSeconds={safeOffset:0.###} autoPlay={autoPlay}");
        }

        private void RebuildCoverageTimeline()
        {
            // Primary row (C1)
            var pRow = PlaybackTimelines[0];
            pRow.Segments.Clear();
            pRow.IsVisible = SelectedCamera != null;
            pRow.CameraName = SelectedCamera?.Name ?? "Select a camera";

            if (_currentSession != null)
            {
                var width = Math.Max(1, pRow.WidthPx);
                foreach (var block in _currentSession.TimelineBlocks)
                {
                    pRow.Segments.Add(new TimelineSegmentItem
                    {
                        LeftPx = (block.StartOffsetSeconds / _currentSession.TotalWindowSeconds) * width,
                        WidthPx = Math.Max(2, ((block.EndOffsetSeconds - block.StartOffsetSeconds) / _currentSession.TotalWindowSeconds) * width),
                        Label = block.Label,
                        HasGapBefore = block.HasGapBefore
                    });
                }
            }

            // Secondary rows (C2, C3, C4)
            for (int i = 0; i < _secondarySessions.Count; i++)
            {
                var row = PlaybackTimelines[i + 1];
                var session = _secondarySessions[i];
                var camera = (i + 1 < _selectedPlaybackCameras.Count) ? _selectedPlaybackCameras[i + 1] : null;

                row.Segments.Clear();
                row.IsVisible = camera != null;
                row.CameraName = camera?.Name ?? "";

                if (session != null && camera != null)
                {
                    var width = Math.Max(1, row.WidthPx);
                    foreach (var block in session.TimelineBlocks)
                    {
                        row.Segments.Add(new TimelineSegmentItem
                        {
                            LeftPx = (block.StartOffsetSeconds / session.TotalWindowSeconds) * width,
                            WidthPx = Math.Max(2, ((block.EndOffsetSeconds - block.StartOffsetSeconds) / session.TotalWindowSeconds) * width),
                            Label = block.Label,
                            HasGapBefore = block.HasGapBefore
                        });
                    }
                }
            }

            // Sync legacy collection for backward compatibility/simplicity
            TimelineSegments.Clear();
            foreach (var s in pRow.Segments) TimelineSegments.Add(s);
        }

        // Click/slider seek in wall-clock seconds (window-relative)
        public async Task SeekToWindowSecondsAsync(double windowSeconds, bool autoPlay)
        {
            if (_isUpdatingUI || _currentSession == null)
                return;

            if ((DateTime.Now - _lastTransitionTime).TotalSeconds < 1.0)
                return;

            var seek = _manifestService.Resolve(_currentSession, windowSeconds);
            if (seek == null)
            {
                ShowPlayerOverlay = true;
                PlayerOverlayTitle = "End of Recording Window";
                PlayerOverlaySubtitle = "No more recorded footage after this point.";
                return;
            }

            _currentSegmentIndex = seek.SegmentIndex;

            if (autoPlay)
                await LoadAndPlaySegmentAsync(_currentSegmentIndex, seek.LocalOffsetSeconds);
            else
                await LoadSegmentPausedAsync(_currentSegmentIndex, seek.LocalOffsetSeconds);

            var landedUtc = seek.Segment.Segment.StartTs.AddSeconds(seek.LocalOffsetSeconds);
            CurrentWallClockText = landedUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");

            _isUpdatingUI = true;
            CurrentTimelineSeconds = Math.Max(0, (landedUtc - _currentSession.WindowStartUtc).TotalSeconds);
            _isUpdatingUI = false;

            await LoadSecondarySlotsAsync(
                _currentSession.WindowStartUtc,
                _currentSession.WindowEndUtc,
                CurrentTimelineSeconds,
                autoPlay,
                CancellationToken.None,
                Volatile.Read(ref _loadToken));

            CaptureLoadedPlaybackSelection();

            UpdatePlayheadPx();
            ShowPlayerOverlay = false;

            if (seek.LandedAfterGap)
                StatusMessage = "Gap skipped to next available recording.";
        }

        private async Task ApplyDesiredRateAsync(bool resumeAfter, bool alreadyPaused = false)
        {
            double requested = _desiredPlaybackRate <= 0 ? 1.0 : _desiredPlaybackRate;
            requested = Math.Clamp(requested, 0.25, 4.0);

            if (RotationDegrees != 0)
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees), "ApplyOptions_SetRotation");

            await RunNativeAsync(() => _playbackEngineService.SetRate(requested), "ApplyOptions_SetRate");
            for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
            {
                if (_secondarySessions[i] == null)
                    continue;

                await RunSecondaryNativeAsync(i, () =>
                {
                    if (RotationDegrees != 0)
                        _secondaryPlaybackEngines[i].SetRotationDegrees(RotationDegrees);

                    _secondaryPlaybackEngines[i].SetRate(requested);
                }, $"ApplyOptions_SecondaryRate_{i + 1}");
            }

            if (resumeAfter)
            {
                await RunNativeAsync(() => _playbackEngineService.Play(), "ApplyOptions_Play");
                for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
                {
                    if (_secondarySessions[i] != null)
                        await RunSecondaryNativeAsync(i, () => _secondaryPlaybackEngines[i].Play(), $"ApplyOptions_PlaySecondary_{i + 1}");
                }
                _shouldBePlaying = true;
                IsPlaying = true;
            }
            else
            {
                if (!alreadyPaused)
                    await RunNativeAsync(() => _playbackEngineService.Pause(), "ApplyOptions_Pause");
                for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
                {
                    if (_secondarySessions[i] != null)
                        await RunSecondaryNativeAsync(i, () => _secondaryPlaybackEngines[i].Pause(), $"ApplyOptions_PauseSecondary_{i + 1}");
                }

                _shouldBePlaying = false;
                IsPlaying = false;
            }

            PlaybackRateText = $"{requested:0.##}x";
            StatusMessage = "";
        }

        [RelayCommand]
        public async Task PlaySegmentAsync(RecordingSegment? segment)
        {
            if (segment == null) return;
            int index = RecordingSegments.IndexOf(segment);
            if (index < 0) index = 0;
            SelectedSegment = segment;
            await LoadAndPlaySegmentAsync(index, 0);
        }

        [RelayCommand]
        public async Task TogglePlayPause()
        {
            try
            {
                Interlocked.Exchange(ref _suspendPolling, 1);

                if (!HasSegments)
                {
                    StatusMessage = "No recording available for this window.";
                    return;
                }

                if (!HasMediaLoaded)
                {
                    await SeekToWindowSecondsAsync(CurrentTimelineSeconds, autoPlay: true);
                    return;
                }

                if (IsPlaying)
                {
                    await RunNativeAsync(() => _playbackEngineService.Pause(), "Toggle_Pause");
                    for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
                    {
                        if (_secondarySessions[i] != null)
                            await RunSecondaryNativeAsync(i, () => _secondaryPlaybackEngines[i].Pause(), $"Toggle_PauseSecondary_{i + 1}");
                    }
                    IsPlaying = false;
                    _shouldBePlaying = false;
                }
                else
                {
                    await ApplyDesiredRateAsync(true);
                }

                await RefreshPlaybackUiFromEngineAsync();
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        [RelayCommand]
        public async Task StopPlayback()
        {
            try
            {
                Interlocked.Exchange(ref _suspendPolling, 1);

                if (!HasMediaLoaded || _currentSegmentIndex < 0 || _currentSegmentIndex >= RecordingSegments.Count)
                    return;

                _shouldBePlaying = false;
                _desiredPlaybackRate = 1.0;
                PlaybackRateText = "1x";
                await RunNativeAsync(() => _playbackEngineService.SetRate(1.0), "Stop_SetRate");

                await LoadSegmentPausedAsync(_currentSegmentIndex, 0);
                if (_currentSession != null)
                    await LoadSecondarySlotsAsync(_currentSession.WindowStartUtc, _currentSession.WindowEndUtc, CurrentTimelineSeconds, autoPlay: false, CancellationToken.None, Volatile.Read(ref _loadToken));
                await RefreshPlaybackUiFromEngineAsync();
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        [RelayCommand]
        public async Task JumpSeconds(string? deltaText)
        {
            if (!double.TryParse(deltaText, System.Globalization.NumberStyles.Float,
                    System.Globalization.CultureInfo.InvariantCulture, out double delta))
                return;

            try
            {
                Interlocked.Exchange(ref _suspendPolling, 1);
                await SeekToWindowSecondsAsync(CurrentTimelineSeconds + delta, autoPlay: IsPlaying);
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        [RelayCommand] public async Task SeekBackCommand() => await JumpSeconds("-10");
        [RelayCommand] public async Task SeekForwardCommand() => await JumpSeconds("10");
        [RelayCommand] public async Task PreviousCommand() => await JumpSeconds("-300");
        [RelayCommand] public async Task NextCommand() => await JumpSeconds("300");
        [RelayCommand] public async Task SetRate1xCommand() => await SetPlaybackRate("1");
        [RelayCommand] public async Task SetRate2xCommand() => await SetPlaybackRate("2");
        [RelayCommand] public async Task SetRate4xCommand() => await SetPlaybackRate("4");

        [RelayCommand]
        public async Task SetPlaybackRate(string? rateText)
        {
            if (!double.TryParse(rateText, System.Globalization.NumberStyles.Float,
                    System.Globalization.CultureInfo.InvariantCulture, out double rate))
                return;

            try
            {
                Interlocked.Exchange(ref _suspendPolling, 1);

                _desiredPlaybackRate = rate;

                if (!HasMediaLoaded)
                {
                    PlaybackRateText = $"{rate:0.##}x";
                    return;
                }

                bool resumeAfter = IsPlaying || _shouldBePlaying;

                await RunNativeAsync(() => _playbackEngineService.Pause(), "SetRate_Pause");
                await ApplyDesiredRateAsync(resumeAfter);
                for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
                {
                    if (_secondarySessions[i] == null)
                        continue;

                    double requestedRate = Math.Clamp(_desiredPlaybackRate <= 0 ? 1.0 : _desiredPlaybackRate, 0.25, 4.0);
                    await RunSecondaryNativeAsync(i, () =>
                    {
                        _secondaryPlaybackEngines[i].Pause();
                        _secondaryPlaybackEngines[i].SetRate(requestedRate);
                        if (resumeAfter)
                            _secondaryPlaybackEngines[i].Play();
                    }, $"SetRate_Secondary_{i + 1}");
                }

                await RefreshPlaybackUiFromEngineAsync();
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        [RelayCommand]
        public async Task StepFrame(string? directionText)
        {
            if (!HasMediaLoaded)
            {
                StatusMessage = "Load recording before frame stepping.";
                return;
            }

            if (!int.TryParse(directionText, out int direction))
                direction = 1;

            direction = direction < 0 ? -1 : 1;

            try
            {
                if (IsPlaying)
                    await RunNativeAsync(() => _playbackEngineService.Pause(), "StepFrame_Pause");

                IsPlaying = false;

                await RunNativeAsync(() => _playbackEngineService.StepFrame(direction), "StepFrame_DoStep");
                await RefreshPlaybackUiFromEngineAsync();
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [ObservableProperty] private int _rotationDegrees;

        [RelayCommand]
        public async Task RotateLeft()
        {
            try
            {
                RotationDegrees = (RotationDegrees + 270) % 360;
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees), "RotateLeft_Set");
                for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
                {
                    if (_secondarySessions[i] != null)
                        await RunSecondaryNativeAsync(i, () => _secondaryPlaybackEngines[i].SetRotationDegrees(RotationDegrees), $"RotateLeft_Secondary_{i + 1}");
                }
                await RefreshPlaybackUiFromEngineAsync();
                StatusMessage = $"Playback rotation: {RotationDegrees}°";
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public async Task RotateRight()
        {
            try
            {
                RotationDegrees = (RotationDegrees + 90) % 360;
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees), "RotateRight_Set");
                for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
                {
                    if (_secondarySessions[i] != null)
                        await RunSecondaryNativeAsync(i, () => _secondaryPlaybackEngines[i].SetRotationDegrees(RotationDegrees), $"RotateRight_Secondary_{i + 1}");
                }
                await RefreshPlaybackUiFromEngineAsync();
                StatusMessage = $"Playback rotation: {RotationDegrees}°";
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public async Task ResetRotation()
        {
            try
            {
                RotationDegrees = 0;
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees), "ResetRotation_Set");
                for (int i = 0; i < _secondaryPlaybackEngines.Count; i++)
                {
                    if (_secondarySessions[i] != null)
                        await RunSecondaryNativeAsync(i, () => _secondaryPlaybackEngines[i].SetRotationDegrees(RotationDegrees), $"ResetRotation_Secondary_{i + 1}");
                }
                await RefreshPlaybackUiFromEngineAsync();
                StatusMessage = "Playback rotation reset.";
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public async Task PrevRecordingBlock()
        {
            if (RecordingSegments.Count == 0) return;

            try
            {
                Interlocked.Exchange(ref _suspendPolling, 1);

                int current = _currentSegmentIndex < 0 ? 0 : _currentSegmentIndex;
                int idx = Math.Max(0, current - 1);

                await LoadSegmentForCurrentModeAsync(idx, 0);
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        [RelayCommand]
        public async Task NextRecordingBlock()
        {
            if (RecordingSegments.Count == 0) return;

            try
            {
                Interlocked.Exchange(ref _suspendPolling, 1);

                int current = _currentSegmentIndex < 0 ? 0 : _currentSegmentIndex;
                int idx = Math.Min(RecordingSegments.Count - 1, current + 1);

                await LoadSegmentForCurrentModeAsync(idx, 0);
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        // Jump box: yyyy-MM-dd HH:mm:ss local
        [RelayCommand]
        public async Task JumpToTimeAsync()
        {
            if (!DateTime.TryParse(JumpToLocalText, out var local))
            {
                StatusMessage = "Invalid jump time. Use yyyy-MM-dd HH:mm:ss";
                return;
            }

            if (_currentSession == null) return;

            var utc = DateTime.SpecifyKind(local, DateTimeKind.Local).ToUniversalTime();
            double windowSeconds = (utc - _currentSession.WindowStartUtc).TotalSeconds;
            await SeekToWindowSecondsAsync(windowSeconds, autoPlay: IsPlaying);
        }

        // Export (desktop-side unchanged; backend still needs route)
        [RelayCommand]
        public async Task ExportRangeAsync()
        {
            if (SelectedCamera == null) { StatusMessage = "Select a camera first."; return; }
            var startUtc = DateTime.SpecifyKind(ExportStartLocal, DateTimeKind.Local).ToUniversalTime();
            var endUtc   = DateTime.SpecifyKind(ExportEndLocal,   DateTimeKind.Local).ToUniversalTime();
            if (endUtc <= startUtc) { StatusMessage = "Export end must be after start."; return; }
            try
            {
                StatusMessage = "Submitting export...";
                LastExport = await _recordingService.QueueExportAsync(SelectedCamera.Id, startUtc, endUtc);
                StatusMessage = $"Export submitted {LastExport?.JobId}";
                OnPropertyChanged(nameof(LastExportDisplay));
            }
            catch (Exception ex) { StatusMessage = $"Export failed: {ex.Message}"; }
        }

        private async Task LoadSegmentPausedAsync(int index, double localOffsetSeconds)
        {
            Interlocked.Exchange(ref _suspendPolling, 1);
            try
            {
                if (index < 0 || index >= RecordingSegments.Count)
                    return;

                var segment = RecordingSegments[index];

                double safeOffset = Math.Max(0, localOffsetSeconds);
                if (segment.DurationSeconds > 0.25)
                    safeOffset = Math.Min(safeOffset, segment.DurationSeconds - 0.25);
                else
                    safeOffset = 0;

                _currentSegmentIndex = index;
                SelectedSegment = segment;
                HasMediaLoaded = true;

                await RunNativeAsync(() =>
                {
                    _playbackEngineService.SetLastSampleEnabled(true);
                    if (_currentSession != null)
                        _playbackEngineService.LoadSession(_currentSession, index);
                    else
                        _playbackEngineService.Load(segment.Path);

                    _playbackEngineService.RebindHost(_lastHostWidth, _lastHostHeight);
                    _playbackEngineService.ForceExpose();

                    // Re-bind the sink to the current host to avoid stale frames.
                    // Give the native pipeline a chance to preroll and paint a frame
                    // before we settle back into paused state.
                    _playbackEngineService.Play();
                }, "LoadPaused_Init");

                await Task.Delay(120);

                await RunNativeAsync(() =>
                {
                    _playbackEngineService.Pause();
                    _playbackEngineService.ForceExpose();
                }, "LoadPaused_Pause");

                if (safeOffset > 0.05)
                {
                    await Task.Delay(120);
                    await RunNativeAsync(() =>
                    {
                        _playbackEngineService.Seek(safeOffset);
                        _playbackEngineService.ForceExpose();
                    }, "LoadPaused_Seek");
                }

                await ApplyDesiredRateAsync(false, alreadyPaused: true);

                var posUtc = segment.StartTs.AddSeconds(safeOffset);
                CurrentWallClockText = posUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");
                CurrentTimelineSeconds = Math.Max(0, (posUtc - WindowStartUtc()).TotalSeconds);
                UpdatePlayheadPx();

                ShowPlayerOverlay = false;
                LogPlaybackDebug(
                    $"PLAYBACK_PRIMARY_OPENED mode=paused slot=1 camera={SelectedCamera?.Name}[{SelectedCamera?.Id}] segmentIndex={index} segmentStart={segment.StartTs:o} offsetSeconds={safeOffset:0.###} wallClock={CurrentWallClockText}");
            }
            catch (Exception ex)
            {
                StatusMessage = $"Failed to load paused segment: {ex.Message}";
                LogPlaybackDebug($"PLAYBACK_PRIMARY_ERROR mode=paused camera={SelectedCamera?.Name}[{SelectedCamera?.Id}] error={ex.Message}");
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        private async Task LoadAndPlaySegmentAsync(int index, double localOffsetSeconds)
        {
            Interlocked.Exchange(ref _suspendPolling, 1);
            try
            {
                if (index < 0 || index >= RecordingSegments.Count)
                    return;

                var segment = RecordingSegments[index];

                double safeOffset = Math.Max(0, localOffsetSeconds);
                if (segment.DurationSeconds > 0.25)
                    safeOffset = Math.Min(safeOffset, segment.DurationSeconds - 0.25);
                else
                    safeOffset = 0;

                _currentSegmentIndex = index;
                SelectedSegment = segment;
                HasMediaLoaded = true;

                await RunNativeAsync(() =>
                {
                    _playbackEngineService.SetLastSampleEnabled(true);
                    if (_currentSession != null)
                        _playbackEngineService.LoadSession(_currentSession, index);
                    else
                        _playbackEngineService.Load(segment.Path);

                    _playbackEngineService.RebindHost(_lastHostWidth, _lastHostHeight);
                    _playbackEngineService.ForceExpose();

                    _playbackEngineService.Pause();
                }, "LoadAndPlay_Init");

                if (safeOffset > 0.05)
                {
                    await Task.Delay(120);
                    await RunNativeAsync(() =>
                    {
                        _playbackEngineService.Seek(safeOffset);
                        _playbackEngineService.ForceExpose();
                    }, "LoadAndPlay_Seek");
                }

                await ApplyPlaybackOptionsAfterLoadAsync(true);

                var posUtc = segment.StartTs.AddSeconds(safeOffset);
                CurrentWallClockText = posUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");
                CurrentTimelineSeconds = Math.Max(0, (posUtc - WindowStartUtc()).TotalSeconds);
                UpdatePlayheadPx();

                ShowPlayerOverlay = false;
                LogPlaybackDebug(
                    $"PLAYBACK_PRIMARY_OPENED mode=play slot=1 camera={SelectedCamera?.Name}[{SelectedCamera?.Id}] segmentIndex={index} segmentStart={segment.StartTs:o} offsetSeconds={safeOffset:0.###} wallClock={CurrentWallClockText}");
            }
            catch (Exception ex)
            {
                StatusMessage = $"Failed to load segment: {ex.Message}";
                LogPlaybackDebug($"PLAYBACK_PRIMARY_ERROR mode=play camera={SelectedCamera?.Name}[{SelectedCamera?.Id}] error={ex.Message}");
            }
            finally
            {
                Interlocked.Exchange(ref _suspendPolling, 0);
            }
        }

        private async void PollTimer_Tick(object? sender, EventArgs e)
        {
            if (Volatile.Read(ref _suspendPolling) == 1)
                return;

            if (Interlocked.Exchange(ref _pollInFlight, 1) == 1)
                return;

            try
            {
                if (_currentSession == null || _currentSegmentIndex < 0 || _currentSegmentIndex >= RecordingSegments.Count)
                    return;

                if (_nativeOpGate.CurrentCount == 0)
                    return;

                var snapshot = await RunNativeAsync(() =>
                {
                    int state = _playbackEngineService.GetState();
                    double localSeconds = _playbackEngineService.GetPositionSeconds();
                    bool eosReached = _playbackEngineService.HasReachedEos();
                    int playlistIndex = _playbackEngineService.GetPlaylistIndex();
                    var videoSize = _playbackEngineService.GetVideoSize();
                    return (state, localSeconds, eosReached, playlistIndex, videoSize.width, videoSize.height);
                }, "Poll_Snapshot");

                bool enginePlaying = snapshot.state == 2;
                double localSeconds = Math.Max(0, snapshot.localSeconds);

                bool enginePositionMoved =
                    snapshot.playlistIndex != _lastEnginePlaylistIndex ||
                    _lastEnginePositionSeconds < 0 ||
                    Math.Abs(localSeconds - _lastEnginePositionSeconds) >= 0.05;

                if (enginePositionMoved)
                {
                    _lastEnginePositionSeconds = localSeconds;
                    _lastEnginePlaylistIndex = snapshot.playlistIndex;
                    _lastEngineMotionUtc = DateTime.UtcNow;
                }

                bool playbackMotionHealthy =
                    !enginePlaying ||
                    snapshot.eosReached ||
                    _lastEngineMotionUtc == DateTime.MinValue ||
                    (DateTime.UtcNow - _lastEngineMotionUtc) <= TimeSpan.FromSeconds(1.5);

                bool actualPlaying = enginePlaying && playbackMotionHealthy && !snapshot.eosReached;

                if (snapshot.width > 0 && snapshot.height > 0)
                {
                    VideoAspectRatio = Math.Max(0.3, Math.Min(3.5, (double)snapshot.width / snapshot.height));
                }

                if (snapshot.playlistIndex >= 0 &&
                    snapshot.playlistIndex < RecordingSegments.Count &&
                    snapshot.playlistIndex != _currentSegmentIndex)
                {
                    _currentSegmentIndex = snapshot.playlistIndex;
                    SelectedSegment = RecordingSegments[_currentSegmentIndex];
                }

                var currentSegment = RecordingSegments[_currentSegmentIndex];
                DateTime calculatedTime = currentSegment.StartTs.AddSeconds(localSeconds);

                if (playbackMotionHealthy && (DateTime.Now - _lastTransitionTime).TotalSeconds > 0.5)
                {
                    var wallClock = calculatedTime.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");
                    var timelineSeconds = Math.Max(0, (calculatedTime - _currentSession.WindowStartUtc).TotalSeconds);

                    bool shouldUpdate =
                        wallClock != _lastUiWallClock ||
                        Math.Abs(timelineSeconds - _lastUiTimelineSeconds) >= 0.25;

                    if (shouldUpdate)
                    {
                        CurrentWallClockText = wallClock;
                        _lastUiWallClock = wallClock;

                        _isUpdatingUI = true;
                        CurrentTimelineSeconds = timelineSeconds;
                        _isUpdatingUI = false;

                        _lastUiTimelineSeconds = timelineSeconds;
                        UpdatePlayheadPx();
                        _lastUiUpdateUtc = DateTime.UtcNow;
                    }
                }

                // FIX: visible playback state must follow actual engine playback, not user intent
                IsPlaying = actualPlaying;

                // Optional: if playback was requested but engine is no longer progressing, clear intent
                if (_shouldBePlaying && !actualPlaying && !snapshot.eosReached)
                {
                    _shouldBePlaying = false;
                }

                if (_shouldBePlaying && snapshot.eosReached)
                {
                    if (_currentSegmentIndex >= RecordingSegments.Count - 1)
                    {
                        await RunNativeAsync(() => _playbackEngineService.Pause(), "Poll_EndOfSession_Pause");
                        IsPlaying = false;
                        _shouldBePlaying = false;
                        ShowPlayerOverlay = true;
                        PlayerOverlayTitle = "End of Recording Window";
                        PlayerOverlaySubtitle = "No more recorded footage in the selected window.";
                        StatusMessage = "Reached end of recording window.";
                    }
                }
            }
            catch (Exception ex)
            {
                StatusMessage = $"Poll error: {ex.Message}";
            }
            finally
            {
                Interlocked.Exchange(ref _pollInFlight, 0);
            }
        }

    }
}
