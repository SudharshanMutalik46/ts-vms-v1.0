using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
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
        private readonly DispatcherTimer _pollTimer;
        private int _pollInFlight;
        private int _suspendPolling;
        private int _attachInFlight;

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
        
        private int _segmentAdvanceInFlight = 0;
        private bool _shouldBePlaying = false;

        // Stronger validation so we don't restore the wrong session
        private string _resumeCameraId = string.Empty;
        private DateTime _resumeDayLocal = DateTime.MinValue;

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
            PlaybackManifestService manifestService)
        {
            _apiClient = apiClient;
            _cameraService = cameraService;
            _playbackEngineService = playbackEngineService;
            _manifestService = manifestService;

            _pollTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(100) };
            _pollTimer.Tick += PollTimer_Tick;

            IsDiagnosticsExpanded = false;
        }

        private async Task RunNativeAsync(Action action)
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

        private async Task<T> RunNativeAsync<T>(Func<T> func)
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
            });

            IsPlaying = snapshot.state == 2;

            var seg = RecordingSegments[_currentSegmentIndex];
            double local = Math.Max(0, snapshot.localSeconds);
            var posUtc = seg.StartTs.AddSeconds(local);

            CurrentWallClockText = posUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");
            CurrentTimelineSeconds = Math.Max(0, (posUtc - WindowStartUtc()).TotalSeconds);
            UpdatePlayheadPx();
        }

        private async Task ApplyPlaybackOptionsAfterLoadAsync(bool shouldPlay)
        {
            if (RotationDegrees != 0)
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees));

            // ALWAYS apply the desired rate, even when it is 1x,
            // otherwise a previous 4x can remain sticky in the native engine.
            await RunNativeAsync(() => _playbackEngineService.SetRate(_desiredPlaybackRate));

            if (shouldPlay)
            {
                await RunNativeAsync(() => _playbackEngineService.Play());
                _shouldBePlaying = true;
            }
            else
            {
                await RunNativeAsync(() => _playbackEngineService.Pause());
                _shouldBePlaying = false;
            }
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
            if (_initialized) return;
            _initialized = true;

            try
            {
                _playbackEngineService.EnsureNativeDllPresent(AppDomain.CurrentDomain.BaseDirectory);
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }

            await LoadCamerasAsync();
            UpdateWindowSummaryText();
            BuildTimelineTicks(); // even before data
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
                try
                {
                    _pollTimer.Stop();
                }
                catch
                {
                    // keep shutdown path resilient
                }

                // Save state BEFORE stop, but only once per deactivate cycle
                CaptureResumeState();

                try
                {
                    await Task.Run(() =>
                    {
                        try
                        {
                            _playbackEngineService.Stop();
                        }
                        catch
                        {
                        }
                    });
                }
                catch
                {
                    // ignore teardown race during view switch
                }

                // Engine is stopped, but UI/session state is preserved for instant resume
                IsPlaying = false;

                // IMPORTANT:
                // Do NOT clear these during tab switch:
                // HasMediaLoaded = false;
                // _currentSegmentIndex = -1;
                // CurrentWallClockText = "--:--:--";
                // ShowPlayerOverlay = true;
            }
            finally
            {
                _isDeactivating = false;
            }
        }

        public async Task EnsureActivePlaybackAsync()
        {
            if (SelectedCamera == null && AvailableCameras.Count > 0)
                SelectedCamera = AvailableCameras[0];

            if (SelectedCamera == null)
                return;

            if (!_hostAttached)
                return;

            // Fast resume path after Live <-> Playback switch
            if (CanResumeCurrentContext())
            {
                try
                {
                    ShowPlayerOverlay = false;

                    if (_wasPlayingBeforeDeactivate)
                        await LoadAndPlaySegmentAsync(_savedSegmentIndex, _savedPlaybackPosition);
                    else
                        await LoadSegmentPausedAsync(_savedSegmentIndex, _savedPlaybackPosition);

                    return;
                }
                catch (Exception ex)
                {
                    StatusMessage = $"Resume failed, falling back to reload: {ex.Message}";
                    ClearResumeState();
                }
            }

            // If segments already loaded and current segment still valid, reload that segment
            // into the newly attached playback host instead of requerying everything.
            if (RecordingSegments.Count > 0 &&
                _currentSegmentIndex >= 0 &&
                _currentSegmentIndex < RecordingSegments.Count)
            {
                try
                {
                    if (IsPlaying)
                        await LoadAndPlaySegmentAsync(_currentSegmentIndex, 0);
                    else
                        await LoadSegmentPausedAsync(_currentSegmentIndex, 0);

                    return;
                }
                catch (Exception ex)
                {
                    StatusMessage = $"Host reload failed, reloading timeline: {ex.Message}";
                }
            }

            // Cold path
            await LoadSegmentsAsync(SelectedCamera.Id);
        }

        partial void OnSelectedCameraChanged(CameraModel? value)
        {
            ClearResumeState();

            if (value != null)
                _ = LoadSegmentsAsync(value.Id);
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
        public async Task LoadSegmentsAsync(string cameraId)
        {
            if (string.IsNullOrWhiteSpace(cameraId))
                return;

            var (startLocal, endLocal, ok, err) = GetWindowLocal();
            if (!ok)
            {
                StatusMessage = err;
                return;
            }

            try
            {
                IsLoading = true;
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

                QueryFromUtc = fromUtc.ToString("o");
                QueryToUtc = toUtc.ToString("o");

                string uri =
                    $"/api/v1/recording/cameras/{Uri.EscapeDataString(cameraId)}/segments" +
                    $"?from={Uri.EscapeDataString(QueryFromUtc)}&to={Uri.EscapeDataString(QueryToUtc)}";

                SegmentsApiUri = uri;
                HttpStatusText = "";
                ApiRowCount = "0";

                var response = await _apiClient.GetAsync(uri);
                HttpStatusText = $"{(int)response.StatusCode} {response.ReasonPhrase}";
                string json = await response.Content.ReadAsStringAsync();

                if (!response.IsSuccessStatusCode)
                {
                    StatusMessage = $"Segments API failed: {HttpStatusText}";
                    ShowPlayerOverlay = true;
                    PlayerOverlayTitle = "No Recording Available";
                    PlayerOverlaySubtitle = $"API error: {HttpStatusText}";
                    return;
                }

                var segments = ParseSegments(json)
                    .OrderBy(s => s.StartTs)
                    .ToList();

                ApiRowCount = segments.Count.ToString();

                foreach (var s in segments)
                    RecordingSegments.Add(s);

                HasSegments = segments.Count > 0;

                // wall-clock total seconds (includes gaps)
                TotalTimelineSeconds = Math.Max(1, (toUtc - fromUtc).TotalSeconds);
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
                    PlayerOverlaySubtitle = "No footage exists in the selected day/time window.";
                    StatusMessage = "No recording for selected window.";
                    CurrentWallClockText = "--:--:--";
                    return;
                }

                DateTime targetUtc = fromUtc.AddSeconds(
                    Math.Max(0, Math.Min(CurrentTimelineSeconds, (toUtc - fromUtc).TotalSeconds)));

                int hitIndex = segments.FindIndex(s => s.StartTs <= targetUtc && s.EndTs >= targetUtc);

                if (hitIndex < 0)
                {
                    hitIndex = segments.FindIndex(s => s.StartTs >= targetUtc);
                    if (hitIndex < 0)
                        hitIndex = segments.Count - 1;
                }

                _currentSegmentIndex = hitIndex;

                double preloadOffset = 0;
                var seg = segments[_currentSegmentIndex];

                if (targetUtc >= seg.StartTs && targetUtc <= seg.EndTs)
                    preloadOffset = Math.Max(0, (targetUtc - seg.StartTs).TotalSeconds);

                await LoadSegmentPausedAsync(_currentSegmentIndex, preloadOffset);

                var posUtc = seg.StartTs.AddSeconds(preloadOffset);
                CurrentTimelineSeconds = Math.Max(0, (posUtc - fromUtc).TotalSeconds);
                UpdatePlayheadPx();

                ShowPlayerOverlay = false;
                StatusMessage = $"Loaded recording coverage for {startLocal:yyyy-MM-dd} ({segments.Count} segments internal).";
            }
            catch (Exception ex)
            {
                HttpStatusText = "EXCEPTION";
                ApiRowCount = "0";
                StatusMessage = $"Failed to load recording: {ex.Message}";
                ShowPlayerOverlay = true;
                PlayerOverlayTitle = "Playback Error";
                PlayerOverlaySubtitle = ex.Message;
            }
            finally
            {
                IsLoading = false;
                UpdateWindowSummaryText();
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

        private void RebuildCoverageTimeline()
        {
            TimelineSegments.Clear();

            if (RecordingSegments.Count == 0) return;

            var fromUtc = WindowStartUtc();
            var toUtc = WindowEndUtc();
            if (toUtc <= fromUtc) return;

            var totalSec = Math.Max(1, (toUtc - fromUtc).TotalSeconds);
            var width = Math.Max(1, TimelineWidthPx);

            double Left(DateTime utc) => Math.Clamp(((utc - fromUtc).TotalSeconds / totalSec), 0, 1) * width;
            double Width(DateTime a, DateTime b)
            {
                var w = (((b - a).TotalSeconds / totalSec) * width);
                return Math.Max(2, w);
            }

            var ordered = RecordingSegments.OrderBy(s => s.StartTs).ToList();
            foreach (var s in ordered)
            {
                var segStart = s.StartTs < fromUtc ? fromUtc : s.StartTs;
                var segEnd = s.EndTs > toUtc ? toUtc : s.EndTs;
                if (segEnd <= segStart) continue;

                TimelineSegments.Add(new TimelineSegmentItem
                {
                    Segment = s,
                    StartUtc = s.StartTs,
                    EndUtc = s.EndTs,
                    LeftPx = Left(segStart),
                    WidthPx = Width(segStart, segEnd),
                    Label = s.StartTs.ToLocalTime().ToString("HH:mm:ss"),
                    IsSelected = false
                });
            }
        }

        // Click/slider seek in wall-clock seconds (window-relative)
        public async Task SeekToWindowSecondsAsync(double windowSeconds, bool autoPlay)
        {
            var fromUtc = WindowStartUtc();
            var toUtc = WindowEndUtc();
            if (toUtc <= fromUtc) return;

            windowSeconds = Math.Max(0, Math.Min(TotalTimelineSeconds, windowSeconds));
            var targetUtc = fromUtc.AddSeconds(windowSeconds);

            await SeekToUtcAsync(targetUtc, autoPlay);
        }

        private async Task SeekToUtcAsync(DateTime targetUtc, bool autoPlay)
        {
            if (RecordingSegments.Count == 0)
            {
                ShowPlayerOverlay = true;
                PlayerOverlayTitle = "No Recording Available";
                PlayerOverlaySubtitle = "No segments loaded.";
                return;
            }

            var ordered = RecordingSegments.OrderBy(s => s.StartTs).ToList();

            var hitIndex = ordered.FindIndex(s => s.StartTs <= targetUtc && s.EndTs >= targetUtc);
            if (hitIndex >= 0)
            {
                var seg = ordered[hitIndex];
                double localOffset = Math.Max(0, (targetUtc - seg.StartTs).TotalSeconds);
                // Map back to RecordingSegments index
                int index = RecordingSegments.IndexOf(seg);
                if (index < 0) index = hitIndex;

                if (autoPlay)
                    await LoadAndPlaySegmentAsync(index, localOffset);
                else
                    await LoadSegmentPausedAsync(index, localOffset);

                ShowPlayerOverlay = false;
                return;
            }

            // target is in a gap (red)
            try
            {
                if (HasMediaLoaded)
                    await RunNativeAsync(() => _playbackEngineService.Pause());
            }
            catch
            {
                // ignore pause failures when selecting a gap
            }
            IsPlaying = false;

            ShowPlayerOverlay = true;
            PlayerOverlayTitle = "No Recording Available";
            PlayerOverlaySubtitle = "Selected time falls inside a gap (red).";

            // update wall-clock display anyway
            CurrentWallClockText = targetUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");
            CurrentTimelineSeconds = Math.Max(0, (targetUtc - WindowStartUtc()).TotalSeconds);
            UpdatePlayheadPx();

            await Task.CompletedTask;
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
                    await RunNativeAsync(() => _playbackEngineService.Pause());
                    IsPlaying = false;
                    _shouldBePlaying = false;
                }
                else
                {
                    await RunNativeAsync(() => _playbackEngineService.Play());
                    IsPlaying = true;
                    _shouldBePlaying = true;
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
                await RunNativeAsync(() => _playbackEngineService.SetRate(1.0));

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
                PlaybackRateText = $"{rate:0.##}x";

                if (!HasMediaLoaded)
                    return;

                await RunNativeAsync(() => _playbackEngineService.SetRate(rate));

                if (!IsPlaying)
                    await RunNativeAsync(() => _playbackEngineService.Pause());

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
                    await RunNativeAsync(() => _playbackEngineService.Pause());

                IsPlaying = false;

                await RunNativeAsync(() => _playbackEngineService.StepFrame(direction));
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
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees));
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
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees));
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
                await RunNativeAsync(() => _playbackEngineService.SetRotationDegrees(RotationDegrees));
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

            var utc = DateTime.SpecifyKind(local, DateTimeKind.Local).ToUniversalTime();
            await SeekToUtcAsync(utc, autoPlay: IsPlaying);
        }

        // Export (desktop-side unchanged; backend still needs route)
        [RelayCommand]
        public async Task ExportRangeAsync()
        {
            if (SelectedCamera == null)
            {
                StatusMessage = "Select a camera first.";
                return;
            }

            var startUtc = DateTime.SpecifyKind(ExportStartLocal, DateTimeKind.Local).ToUniversalTime();
            var endUtc = DateTime.SpecifyKind(ExportEndLocal, DateTimeKind.Local).ToUniversalTime();

            if (endUtc <= startUtc)
            {
                StatusMessage = "Export end must be after start.";
                return;
            }

            try
            {
                StatusMessage = "Submitting export...";

                var req = new RecordingExportRequest
                {
                    CameraId = SelectedCamera.Id,
                    FromTs = startUtc,
                    ToTs = endUtc,
                    Format = "mp4"
                };

                LastExport = await _apiClient.PostAsync<RecordingExportRequest, RecordingExportResponse>(
                    "/api/v1/recording/exports", req);

                StatusMessage = $"Export submitted: {LastExport?.JobId}";
                OnPropertyChanged(nameof(LastExportDisplay));
            }
            catch (Exception ex)
            {
                StatusMessage = $"Export failed: {ex.Message}";
            }
        }

        private async Task LoadSegmentPausedAsync(int index, double localOffsetSeconds)
        {
            if (index < 0 || index >= RecordingSegments.Count)
                return;

            var segment = RecordingSegments[index];

            double safeOffset = Math.Max(0, localOffsetSeconds);
            if (segment.DurationSeconds > 0.25)
                safeOffset = Math.Min(safeOffset, segment.DurationSeconds - 0.25);
            else
                safeOffset = 0;

            try
            {
                await RunNativeAsync(() =>
                {
                    _playbackEngineService.Load(segment.Path);
                    _playbackEngineService.Pause();
                });

                if (safeOffset > 0.05)
                {
                    await Task.Delay(120);
                    await RunNativeAsync(() => _playbackEngineService.Seek(safeOffset));
                }

                await ApplyPlaybackOptionsAfterLoadAsync(false);

                _currentSegmentIndex = index;
                SelectedSegment = segment;
                HasMediaLoaded = true;
                IsPlaying = false;
                _shouldBePlaying = false;

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
        }

        private async Task LoadAndPlaySegmentAsync(int index, double localOffsetSeconds)
        {
            if (index < 0 || index >= RecordingSegments.Count)
                return;

            var segment = RecordingSegments[index];

            double safeOffset = Math.Max(0, localOffsetSeconds);
            if (segment.DurationSeconds > 0.25)
                safeOffset = Math.Min(safeOffset, segment.DurationSeconds - 0.25);
            else
                safeOffset = 0;

            await RunNativeAsync(() =>
            {
                _playbackEngineService.Load(segment.Path);
                _playbackEngineService.Pause();
            });

            if (safeOffset > 0.05)
            {
                await Task.Delay(120);
                await RunNativeAsync(() => _playbackEngineService.Seek(safeOffset));
            }

            await ApplyPlaybackOptionsAfterLoadAsync(true);

            _currentSegmentIndex = index;
            SelectedSegment = segment;
            HasMediaLoaded = true;
            IsPlaying = true;
            _shouldBePlaying = true;

            var posUtc = segment.StartTs.AddSeconds(safeOffset);
            CurrentWallClockText = posUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");
            CurrentTimelineSeconds = Math.Max(0, (posUtc - WindowStartUtc()).TotalSeconds);
            UpdatePlayheadPx();

            ShowPlayerOverlay = false;
        }

        private async void PollTimer_Tick(object? sender, EventArgs e)
        {
            if (Volatile.Read(ref _suspendPolling) == 1)
                return;

            if (Interlocked.Exchange(ref _pollInFlight, 1) == 1)
                return;

            try
            {
                if (_currentSegmentIndex < 0 || _currentSegmentIndex >= RecordingSegments.Count)
                    return;

                // If a user action / segment transition already owns native gate, do not queue behind it.
                if (_nativeOpGate.CurrentCount == 0)
                    return;

                var snapshot = await RunNativeAsync(() =>
                {
                    int state = _playbackEngineService.GetState();
                    double localSeconds = _playbackEngineService.GetPositionSeconds();
                    return (state, localSeconds);
                });

                bool enginePlaying = snapshot.state == 2;

                // keep UI in "playing intent" during transitions / fast playback
                IsPlaying = _shouldBePlaying || enginePlaying;
                double localSeconds = snapshot.localSeconds;

                var seg = RecordingSegments[_currentSegmentIndex];
                var posUtc = seg.StartTs.AddSeconds(Math.Max(0, localSeconds));

                // update wall-clock timeline
                CurrentWallClockText = posUtc.ToLocalTime().ToString("yyyy-MM-dd HH:mm:ss");
                CurrentTimelineSeconds = Math.Max(0, (posUtc - WindowStartUtc()).TotalSeconds);
                UpdatePlayheadPx();

                double segDur = seg.DurationSeconds;

                // Look ahead earlier, especially at higher speeds.
                double endLookAheadSec = Math.Max(0.35, 0.75 * Math.Max(1.0, _desiredPlaybackRate));

                if (_shouldBePlaying && localSeconds >= Math.Max(0, segDur - endLookAheadSec))
                {
                    if (_currentSegmentIndex + 1 < RecordingSegments.Count)
                    {
                        int nextIndex = _currentSegmentIndex + 1;
                        var next = RecordingSegments[nextIndex];
                        var gapSec = Math.Max(0, (next.StartTs - seg.EndTs).TotalSeconds);

                        if (Interlocked.Exchange(ref _segmentAdvanceInFlight, 1) == 0)
                        {
                            try
                            {
                                Interlocked.Exchange(ref _suspendPolling, 1);

                                // Seamless continuous mode:
                                // always jump to next available recording block instead of stopping on gap
                                await LoadAndPlaySegmentAsync(nextIndex, 0);
                                _shouldBePlaying = true;

                                ShowPlayerOverlay = false;

                                if (gapSec > 0.05)
                                    StatusMessage = $"Skipped gap {gapSec:0.###}s and continued playback.";
                                else
                                    StatusMessage = $"Advanced to segment {nextIndex}.";
                            }
                            finally
                            {
                                Interlocked.Exchange(ref _suspendPolling, 0);
                                Interlocked.Exchange(ref _segmentAdvanceInFlight, 0);
                            }
                        }
                    }
                    else
                    {
                        await RunNativeAsync(() => _playbackEngineService.Pause());
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

        private static List<RecordingSegment> ParseSegments(string json)
        {
            if (string.IsNullOrWhiteSpace(json))
                return new List<RecordingSegment>();

            var options = new JsonSerializerOptions { PropertyNameCaseInsensitive = true };
            List<RecordingSegment> result = new List<RecordingSegment>();

            try
            {
                var direct = JsonSerializer.Deserialize<List<RecordingSegment>>(json, options);
                if (direct != null && direct.Count > 0)
                    result = direct;
            }
            catch { }

            if (result.Count == 0)
            {
                try
                {
                    var envelope = JsonSerializer.Deserialize<RecordingSegmentsEnvelope>(json, options);
                    if (envelope?.Segments != null)
                        result = envelope.Segments.ToList();
                }
                catch { }
            }

            // Fix timezone confusion: API payloads with timezone offsets (+05:30) get deserialized natively as Local.
            // By strictly forcing them to Utc here, all timeline subtraction offsets function correctly against WindowStartUtc.
            foreach (var seg in result)
            {
                seg.StartTs = seg.StartTs.ToUniversalTime();
                seg.EndTs = seg.EndTs.ToUniversalTime();
            }

            return result;
        }
    }
}
