using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.Generic;
using System.Collections.ObjectModel;
using System.Linq;
using System.Net.Http;
using System.Text.Json;
using System.Threading.Tasks;
using System.Windows;
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

        private PlaybackManifestService.PlaybackManifest? _manifest;
        private int _currentSegmentIndex = -1;
        private bool _initialized;

        public ObservableCollection<CameraModel> AvailableCameras { get; } = new();
        public ObservableCollection<RecordingSegment> RecordingSegments { get; } = new();
        public ObservableCollection<PlaybackTimelineBlock> TimelineBlocks { get; } = new();

        [ObservableProperty] private CameraModel? _selectedCamera;
        [ObservableProperty] private string _statusMessage = "Select a camera to inspect recorded footage.";
        [ObservableProperty] private string _currentVideoTitle = "No segment selected";
        [ObservableProperty] private string _currentTimeText = "00:00:00";
        [ObservableProperty] private string _totalTimeText = "00:00:00";
        [ObservableProperty] private string _playbackRateText = "1x";
        [ObservableProperty] private bool _isLoading;
        [ObservableProperty] private bool _isPlaying;
        [ObservableProperty] private double _currentTimelineSeconds;
        [ObservableProperty] private double _totalTimelineSeconds = 1;
        [ObservableProperty] private bool _hasSegments;
        [ObservableProperty] private bool _hasMediaLoaded;

        [ObservableProperty] private string _selectedCameraDebugId = "";
        [ObservableProperty] private string _queryFromUtc = "";
        [ObservableProperty] private string _queryToUtc = "";
        [ObservableProperty] private string _segmentsApiUri = "";
        [ObservableProperty] private string _httpStatusText = "";
        [ObservableProperty] private string _apiRowCount = "0";
        [ObservableProperty] private string _diagnosticMessage = "Ready";
        [ObservableProperty] private bool _showDiagnostics = true;

        [ObservableProperty] private RecordingSegment? _selectedSegment;
        [ObservableProperty] private int _rotationDegrees;
        [ObservableProperty] private bool _isDiagnosticsExpanded;

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

            _pollTimer = new DispatcherTimer
            {
                Interval = TimeSpan.FromMilliseconds(250)
            };
            _pollTimer.Tick += PollTimer_Tick;

            RotationDegrees = 0;
            IsDiagnosticsExpanded = false;
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
        }

        public void AttachVideoHost(IntPtr hwnd)
        {
            if (hwnd == IntPtr.Zero) return;
            try
            {
                _playbackEngineService.AttachHost(hwnd);
                _pollTimer.Start();
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        partial void OnSelectedCameraChanged(CameraModel? value)
        {
            if (value != null)
                _ = LoadSegmentsAsync(value.Id);
        }

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

        [RelayCommand]
        public async Task RefreshSelectedCameraAsync()
        {
            if (string.IsNullOrWhiteSpace(SelectedCameraDebugId))
            {
                StatusMessage = "No camera selected.";
                return;
            }

            await LoadSegmentsAsync(SelectedCameraDebugId);
        }

        [RelayCommand]
        public void CopyDiagnostics()
        {
            var text =
                $"CameraId: {SelectedCameraDebugId}{Environment.NewLine}" +
                $"FromUtc: {QueryFromUtc}{Environment.NewLine}" +
                $"ToUtc: {QueryToUtc}{Environment.NewLine}" +
                $"SegmentsApi: {SegmentsApiUri}{Environment.NewLine}" +
                $"HttpStatus: {HttpStatusText}{Environment.NewLine}" +
                $"ApiRowCount: {ApiRowCount}{Environment.NewLine}" +
                $"StatusMessage: {StatusMessage}{Environment.NewLine}" +
                $"Diagnostic: {DiagnosticMessage}";

            System.Windows.Clipboard.SetText(text);
            StatusMessage = "Playback diagnostics copied to clipboard.";
        }

        [RelayCommand]
        public async Task LoadSegmentsAsync(string cameraId)
        {
            if (string.IsNullOrWhiteSpace(cameraId))
                return;

            try
            {
                IsLoading = true;
                RecordingSegments.Clear();
                TimelineBlocks.Clear();
                HasSegments = false;
                HasMediaLoaded = false;

                SelectedCameraDebugId = cameraId;

                var fromUtc = DateTime.UtcNow.AddHours(-24);
                var toUtc = DateTime.UtcNow;

                QueryFromUtc = fromUtc.ToString("o");
                QueryToUtc = toUtc.ToString("o");

                string uri =
                    $"/api/v1/recording/cameras/{Uri.EscapeDataString(cameraId)}/segments" +
                    $"?from={Uri.EscapeDataString(QueryFromUtc)}&to={Uri.EscapeDataString(QueryToUtc)}";

                SegmentsApiUri = uri;
                HttpStatusText = "";
                ApiRowCount = "0";
                DiagnosticMessage = "Querying recording segments...";

                // Preferred path if ApiClient now exposes GetAsync
                var response = await _apiClient.GetAsync(uri);
                HttpStatusText = $"{(int)response.StatusCode} {response.ReasonPhrase}";

                string json = await response.Content.ReadAsStringAsync();

                if (!response.IsSuccessStatusCode)
                {
                    StatusMessage = $"Segments API failed: {HttpStatusText}";
                    DiagnosticMessage = $"API failed for CameraId={cameraId}";
                    return;
                }

                var segments = ParseSegments(json)
                    .OrderBy(s => s.StartTs)
                    .ToList();

                ApiRowCount = segments.Count.ToString();

                foreach (var segment in segments)
                    RecordingSegments.Add(segment);

                SelectedSegment = null;

                _manifest = _manifestService.Build(segments);

                foreach (var block in _manifest.TimelineBlocks)
                    TimelineBlocks.Add(block);

                TotalTimelineSeconds = Math.Max(1, _manifest.TotalDurationSeconds);
                CurrentTimelineSeconds = 0;
                HasSegments = segments.Count > 0;

                if (!HasSegments)
                {
                    _currentSegmentIndex = -1;
                    CurrentVideoTitle = "No segment selected";
                    CurrentTimeText = "00:00:00";
                    TotalTimeText = "00:00:00";
                    StatusMessage = "No recorded segments found for the selected range.";
                    DiagnosticMessage =
                        $"No rows returned. CameraId={cameraId}, From={QueryFromUtc}, To={QueryToUtc}, HTTP={HttpStatusText}";
                    return;
                }

                _currentSegmentIndex = segments.Count - 1;
                SelectedSegment = RecordingSegments[_currentSegmentIndex];
                CurrentVideoTitle = SelectedSegment.FileName;
                StatusMessage = $"Loaded {segments.Count} recorded segments for Last 24 Hours.";
                DiagnosticMessage =
                    $"Rows={segments.Count}, CameraId={cameraId}, First={segments.First().StartTs:o}, Last={segments.Last().EndTs:o}";

                await LoadSegmentPausedAsync(_currentSegmentIndex, 0);
            }
            catch (Exception ex)
            {
                HttpStatusText = "EXCEPTION";
                ApiRowCount = "0";
                StatusMessage = $"Failed to load recorded segments: {ex.Message}";
                DiagnosticMessage = $"Exception for CameraId={cameraId}: {ex.Message}";
            }
            finally
            {
                IsLoading = false;
            }
        }

        [RelayCommand]
        public async Task PlaySegmentAsync(RecordingSegment? segment)
        {
            if (segment == null) return;

            try
            {
                int index = RecordingSegments.IndexOf(segment);
                if (index < 0) index = 0;

                SelectedSegment = segment;
                await LoadAndPlaySegmentAsync(index, 0);
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public void TogglePlayPause()
        {
            try
            {
                if (!HasSegments)
                {
                    StatusMessage = "No recorded segments available for this camera.";
                    return;
                }

                if (!HasMediaLoaded)
                {
                    _ = LoadAndPlaySegmentAsync(
                        _currentSegmentIndex >= 0 ? _currentSegmentIndex : 0,
                        0);
                    return;
                }

                if (IsPlaying)
                {
                    _playbackEngineService.Pause();
                    IsPlaying = false;
                }
                else
                {
                    _playbackEngineService.Play();
                    IsPlaying = true;
                }
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public void StopPlayback()
        {
            try
            {
                if (!HasMediaLoaded || _currentSegmentIndex < 0 || _currentSegmentIndex >= RecordingSegments.Count)
                    return;

                _playbackEngineService.Pause();
                _playbackEngineService.Seek(0);

                IsPlaying = false;
                CurrentTimeText = "00:00:00";
                CurrentTimelineSeconds = _manifestService.GetGlobalOffset(
                    RecordingSegments.ToList(),
                    _currentSegmentIndex,
                    0);
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public void JumpSeconds(string? deltaText)
        {
            if (!double.TryParse(deltaText, out double delta)) return;
            SeekGlobal(CurrentTimelineSeconds + delta);
        }

        [RelayCommand]
        public void SetPlaybackRate(string? rateText)
        {
            if (!HasMediaLoaded)
            {
                StatusMessage = "Load a recorded segment before changing playback speed.";
                return;
            }

            if (!double.TryParse(
                    rateText,
                    System.Globalization.NumberStyles.Float,
                    System.Globalization.CultureInfo.InvariantCulture,
                    out double rate))
                return;

            try
            {
                _playbackEngineService.SetRate(rate);
                PlaybackRateText = $"{rate:0.##}x";
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public void StepFrame(string? directionText)
        {
            if (!HasMediaLoaded)
            {
                StatusMessage = "Load a recorded segment before frame stepping.";
                return;
            }

            if (!int.TryParse(directionText, out int direction))
                direction = 1;

            direction = direction < 0 ? -1 : 1;

            try
            {
                if (IsPlaying)
                    _playbackEngineService.Pause();

                IsPlaying = false;
                _playbackEngineService.StepFrame(direction);
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        [RelayCommand]
        public void RotateLeft()
        {
            RotationDegrees = (RotationDegrees + 270) % 360;
            _playbackEngineService.SetRotationDegrees(RotationDegrees);
            StatusMessage = $"Playback rotation: {RotationDegrees}°";
        }

        [RelayCommand]
        public void RotateRight()
        {
            RotationDegrees = (RotationDegrees + 90) % 360;
            _playbackEngineService.SetRotationDegrees(RotationDegrees);
            StatusMessage = $"Playback rotation: {RotationDegrees}°";
        }

        [RelayCommand]
        public void ResetRotation()
        {
            RotationDegrees = 0;
            _playbackEngineService.SetRotationDegrees(RotationDegrees);
            StatusMessage = "Playback rotation reset.";
        }

        public void SeekFromTimeline(double globalSeconds)
        {
            SeekGlobal(globalSeconds);
        }

        private async Task LoadSegmentPausedAsync(int index, double localOffsetSeconds)
        {
            if (index < 0 || index >= RecordingSegments.Count)
                return;

            var segment = RecordingSegments[index];

            _playbackEngineService.Load(segment.Path);

            if (localOffsetSeconds > 0)
                _playbackEngineService.Seek(localOffsetSeconds);

            _playbackEngineService.Pause();

            _currentSegmentIndex = index;
            SelectedSegment = segment;
            CurrentVideoTitle = segment.FileName;
            TotalTimeText = TimeSpan.FromSeconds(Math.Max(0, segment.DurationSeconds)).ToString(@"hh\:mm\:ss");
            CurrentTimeText = TimeSpan.FromSeconds(Math.Max(0, localOffsetSeconds)).ToString(@"hh\:mm\:ss");
            CurrentTimelineSeconds = _manifestService.GetGlobalOffset(
                RecordingSegments.ToList(),
                _currentSegmentIndex,
                localOffsetSeconds);

            HasMediaLoaded = true;
            IsPlaying = false;

            await Task.CompletedTask;
        }

        private async Task LoadAndPlaySegmentAsync(int index, double localOffsetSeconds)
        {
            if (index < 0 || index >= RecordingSegments.Count)
                return;

            var segment = RecordingSegments[index];

            _playbackEngineService.Load(segment.Path);

            if (localOffsetSeconds > 0)
                _playbackEngineService.Seek(localOffsetSeconds);

            _playbackEngineService.Play();

            _currentSegmentIndex = index;
            SelectedSegment = segment;
            CurrentVideoTitle = segment.FileName;
            TotalTimeText = TimeSpan.FromSeconds(Math.Max(0, segment.DurationSeconds)).ToString(@"hh\:mm\:ss");
            CurrentTimeText = TimeSpan.FromSeconds(Math.Max(0, localOffsetSeconds)).ToString(@"hh\:mm\:ss");
            CurrentTimelineSeconds = _manifestService.GetGlobalOffset(
                RecordingSegments.ToList(),
                _currentSegmentIndex,
                localOffsetSeconds);

            HasMediaLoaded = true;
            IsPlaying = true;

            await Task.CompletedTask;
        }

        private void SeekGlobal(double globalSeconds)
        {
            try
            {
                if (_manifest == null || _manifest.Segments.Count == 0)
                    return;

                globalSeconds = Math.Max(0, Math.Min(TotalTimelineSeconds, globalSeconds));
                var resolved = _manifestService.Resolve(_manifest.Segments, globalSeconds);
                if (resolved == null) return;

                bool changingSegment = resolved.SegmentIndex != _currentSegmentIndex;
                if (changingSegment)
                {
                    _ = LoadAndPlaySegmentAsync(resolved.SegmentIndex, resolved.LocalOffsetSeconds);
                }
                else
                {
                    _playbackEngineService.Seek(resolved.LocalOffsetSeconds);
                    CurrentTimelineSeconds = globalSeconds;
                }
            }
            catch (Exception ex)
            {
                StatusMessage = ex.Message;
            }
        }

        private void PollTimer_Tick(object? sender, EventArgs e)
        {
            try
            {
                if (_currentSegmentIndex < 0 || _currentSegmentIndex >= RecordingSegments.Count)
                    return;

                int state = _playbackEngineService.GetState();
                IsPlaying = state == 2;

                double localSeconds = _playbackEngineService.GetPositionSeconds();
                CurrentTimeText = TimeSpan.FromSeconds(Math.Max(0, localSeconds)).ToString(@"hh\:mm\:ss");
                CurrentTimelineSeconds = _manifestService.GetGlobalOffset(RecordingSegments.ToList(), _currentSegmentIndex, localSeconds);

                double currentSegmentDuration = RecordingSegments[_currentSegmentIndex].DurationSeconds;
                if (IsPlaying && localSeconds >= Math.Max(0, currentSegmentDuration - 0.35))
                {
                    if (_currentSegmentIndex + 1 < RecordingSegments.Count)
                    {
                        _ = LoadAndPlaySegmentAsync(_currentSegmentIndex + 1, 0);
                    }
                    else
                    {
                        IsPlaying = false;
                    }
                }
            }
            catch
            {
                // keep UI responsive even if polling hits a transient native/runtime issue
            }
        }

        private static List<RecordingSegment> ParseSegments(string json)
        {
            if (string.IsNullOrWhiteSpace(json))
                return new List<RecordingSegment>();

            var options = new JsonSerializerOptions { PropertyNameCaseInsensitive = true };

            try
            {
                var direct = JsonSerializer.Deserialize<List<RecordingSegment>>(json, options);
                if (direct != null && direct.Count > 0)
                    return direct;
            }
            catch { }

            try
            {
                var envelope = JsonSerializer.Deserialize<RecordingSegmentsEnvelope>(json, options);
                if (envelope?.Segments != null)
                    return envelope.Segments.ToList();
            }
            catch { }

            return new List<RecordingSegment>();
        }
    }
}
