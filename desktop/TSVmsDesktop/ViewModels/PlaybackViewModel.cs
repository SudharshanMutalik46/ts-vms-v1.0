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
    public partial class PlaybackViewModel : ObservableObject
    {
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
        private string _lastLoadedCameraId = string.Empty;
        private DateTime _lastLoadedDayLocal = DateTime.MinValue;
        private DateTime _lastLoadedWindowFromLocal = DateTime.MinValue;
        private DateTime _lastLoadedWindowToLocal = DateTime.MinValue;
        private DateTime _lastUiUpdateUtc = DateTime.MinValue;
        private double _lastUiTimelineSeconds = -1;
        private string _lastUiWallClock = string.Empty;


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
        public ObservableCollection<RecordingSegment> RecordingSegments { get; } = new();

        // GREEN segments positioned on red base
        public ObservableCollection<TimelineSegmentItem> TimelineSegments { get; } = new();
        public ObservableCollection<TimelineTickItem> TimelineTicks { get; } = new();

        [ObservableProperty] private CameraModel? _selectedCamera;

        [ObservableProperty] private string _cameraSearchText = "";

        [ObservableProperty] private string _statusMessage = "Select a camera, pick a day/time window, then play.";
        [ObservableProperty] private bool _isLoading;

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

        private async Task RefreshPlaybackUiFromEngineAsync()
        {
            if (_currentSegmentIndex < 0 || _currentSegmentIndex >= RecordingSegments.Count)
                return;

            var snapshot = await RunNativeAsync(() =>
            {
                int state = _playbackEngineService.GetState();
                double localSeconds = _playbackEngineService.GetPositionSeconds();
                return (state, localSeconds);
            }, "RefreshUi_GetStatePos");

            IsPlaying = snapshot.state == 2;

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
                await Task.Run(() => _playbackEngineService.AttachHost(hwnd));
                _hostAttached = true;
                _pollTimer.Start();

                // Immediately apply stashed size if we have one
                if (_lastHostWidth > 0 && _lastHostHeight > 0)
                {
                    await RunNativeAsync(
                        () => _playbackEngineService.RebindHost(_lastHostWidth, _lastHostHeight),
                        "Attach_RebindHost");
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
            }
            catch
            {
                // Ignore resize races during layout churn.
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

        private async Task StopAndClearEngineAsync()
        {
            try
            {
                await RunNativeAsync(() =>
                {
                    // Disable sticky last-sample rendering, then stop and force repaint.
                    _playbackEngineService.SetLastSampleEnabled(false);
                    _playbackEngineService.Stop();
                    _playbackEngineService.ForceExpose();
                });
            }
            catch
            {
                // Engine may not be initialized yet; that's fine.
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
        }

        private bool LoadedContextMatchesCurrentSelection()
        {
            try
            {
                if (SelectedCamera == null || _currentSession == null)
                    return false;

                return string.Equals(_lastLoadedCameraId, SelectedCamera.Id, StringComparison.Ordinal) &&
                       _lastLoadedDayLocal == SelectedDayLocal.Date &&
                       _lastLoadedWindowFromLocal == WindowFromLocal() &&
                       _lastLoadedWindowToLocal == WindowToLocal();
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

                await StopAndClearEngineAsync();
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

            if (SelectedCamera == null && AvailableCameras.Count > 0)
                SelectedCamera = AvailableCameras[0];

            if (SelectedCamera == null)
            {
                StatusMessage = "No cameras available.";
                ShowPlayerOverlay = true;
                PlayerOverlayTitle = "No cameras available";
                PlayerOverlaySubtitle = "Add or enable a camera, then reopen Playback.";
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
                    await StopAndClearEngineAsync();
                    ResetLoadedPlaybackStateOnly();
                }
            }

            // Otherwise always do a clean load.
            await LoadSegmentsAsync(SelectedCamera.Id);
        }

        partial void OnSelectedCameraChanged(CameraModel? value)
        {
            _ = HandleSelectedCameraChangedAsync(value);
        }

        private async Task HandleSelectedCameraChangedAsync(CameraModel? value)
        {
            ClearResumeState();

            SafeCancel(_switchCts);
            SafeCancel(_loadCts);

            await StopAndClearEngineAsync();
            ResetLoadedPlaybackStateOnly();

            if (value != null)
            {
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
                TimelineTicks.Add(new TimelineTickItem
                {
                    LeftPx = left,
                    Label = t.ToString("HH:mm")
                });
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
                foreach (var camera in _cameraService.AllCameras)
                    AvailableCameras.Add(camera);

                if (SelectedCamera == null && AvailableCameras.Count > 0)
                    SelectedCamera = AvailableCameras[0];
                else if (AvailableCameras.Count == 0)
                    StatusMessage = "No cameras available.";
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

            bool cameraChanged = !string.Equals(_lastLoadedCameraId, cameraId, StringComparison.Ordinal);
            int generation = Interlocked.Increment(ref _loadToken);

            ClearResumeState();

            // Clear old engine frame/session before building the new request.
            await StopAndClearEngineAsync();
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
                string cacheKey = $"{cameraId}|{fromUtc:o}|{toUtc:o}";

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
                if (_segmentCache.TryGetValue(cacheKey, out var cached) &&
                    (DateTime.UtcNow - cached.CachedAtUtc) < SegmentCacheTtl)
                {
                    _currentSession = cached.Session;
                    HttpStatusText = "200 OK (cache)";
                    ApiRowCount = _currentSession.Segments.Count.ToString();
                }
                else
                {
                    List<RecordingSegment> rawSegments;
                    try
                    {
                        rawSegments = await _recordingService.GetSegmentsAsync(cameraId, fromUtc, toUtc, _loadCts.Token);
                        if (generation != Volatile.Read(ref _loadToken))
                            return;
                        HttpStatusText = "200 OK";
                        ApiRowCount = rawSegments.Count.ToString();
                        LogPlaybackDebug($"LOAD_RESPONSE camera={cameraId} status=200 segments={rawSegments.Count}");
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

                    _currentSession = _manifestService.Build(cameraId, fromUtc, toUtc, rawSegments);
                    _segmentCache[cacheKey] = new CachedSession
                    {
                        CameraId = cameraId,
                        FromUtc = fromUtc,
                        ToUtc = toUtc,
                        Session = _currentSession,
                        CachedAtUtc = DateTime.UtcNow
                    };
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
                File.AppendAllText(
                    LogPaths.ApiDebugLogPath,
                    $"[{DateTime.Now:dd-MM-yyyy HH:mm:ss.fff}] [Playback] {message}{Environment.NewLine}");
            }
            catch
            {
                // Debug logging must never break playback.
            }
        }

        private void RebuildCoverageTimeline()
        {
            TimelineSegments.Clear();
            if (_currentSession == null) return;

            var width = Math.Max(1, TimelineWidthPx);

            foreach (var block in _currentSession.TimelineBlocks)
            {
                TimelineSegments.Add(new TimelineSegmentItem
                {
                    LeftPx = (block.StartOffsetSeconds / _currentSession.TotalWindowSeconds) * width,
                    WidthPx = Math.Max(2, ((block.EndOffsetSeconds - block.StartOffsetSeconds) / _currentSession.TotalWindowSeconds) * width),
                    Label = block.Label,
                    HasGapBefore = block.HasGapBefore
                });
            }
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

            if (resumeAfter)
            {
                await RunNativeAsync(() => _playbackEngineService.Play(), "ApplyOptions_Play");
                _shouldBePlaying = true;
                IsPlaying = true;
            }
            else
            {
                if (!alreadyPaused)
                    await RunNativeAsync(() => _playbackEngineService.Pause(), "ApplyOptions_Pause");

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
            if (!double.TryParse(deltaText, out double delta))
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
            }
            catch (Exception ex)
            {
                StatusMessage = $"Failed to load paused segment: {ex.Message}";
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
            }
            catch (Exception ex)
            {
                StatusMessage = $"Failed to load segment: {ex.Message}";
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
                    return (state, localSeconds, eosReached);
                }, "Poll_Snapshot");

                bool enginePlaying = snapshot.state == 2;
                double localSeconds = Math.Max(0, snapshot.localSeconds);
                int nativeIndex = _playbackEngineService.GetPlaylistIndex();

                // Sync segment index with engine
                if (nativeIndex >= 0 && nativeIndex < RecordingSegments.Count && nativeIndex != _currentSegmentIndex)
                {
                    _currentSegmentIndex = nativeIndex;
                    SelectedSegment = RecordingSegments[_currentSegmentIndex];
                    _lastTransitionTime = DateTime.Now;
                }

                var currentSegment = RecordingSegments[_currentSegmentIndex];
                DateTime calculatedTime = currentSegment.StartTs.AddSeconds(localSeconds);

                // Update UI - only if not in the middle of a transition jitter window
                if ((DateTime.Now - _lastTransitionTime).TotalSeconds > 0.5)
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

                IsPlaying = _shouldBePlaying || enginePlaying;

                // End of session handling
                if (_shouldBePlaying && snapshot.eosReached && _currentSegmentIndex >= RecordingSegments.Count - 1)
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
