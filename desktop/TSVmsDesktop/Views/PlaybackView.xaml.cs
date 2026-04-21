using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Interop;
using System.Windows.Media;
using System.Windows.Media.Animation;
using TSVmsDesktop.Controls;
using TSVmsDesktop.Models;
using TSVmsDesktop.ViewModels;

namespace TSVmsDesktop.Views
{
    public partial class PlaybackView : System.Windows.Controls.UserControl
    {
        public static readonly DependencyProperty IsOwnerWindowActiveProperty =
            DependencyProperty.Register(
                nameof(IsOwnerWindowActive),
                typeof(bool),
                typeof(PlaybackView),
                new PropertyMetadata(false));

        public bool IsOwnerWindowActive
        {
            get => (bool)GetValue(IsOwnerWindowActiveProperty);
            private set => SetValue(IsOwnerWindowActiveProperty, value);
        }

        private const double DefaultPlaybackAspect = 16.0 / 9.0;
        private CancellationTokenSource? _hostAttachCts;
        private readonly SemaphoreSlim _layoutSyncGate = new(1, 1);
        private bool _layoutSyncInFlight;
        private bool _suppressCameraSelectionEvents;
        private CancellationTokenSource? _layoutSyncDebounceCts;
        private CancellationTokenSource? _selectionRefreshCts;
        private Window? _ownerWindow;
        private Window? _playbackFullScreenWindow;
        private Grid? _playbackFullScreenContentHost;
        private Grid? _playbackFullScreenTimelineHost;
        private bool _isFullScreenActive;
        private bool _isFullScreenTransitionInFlight;

        private static int GetVisibleTileCount(PlaybackViewModel vm)
            => vm.PlaybackLayoutMode == PlaybackLayoutMode.Quad ? 4 : 1;

        private static int GetEffectiveLayoutCount(PlaybackViewModel vm)
            => vm.PlaybackLayoutMode == PlaybackLayoutMode.Quad ? 4 : 1;

        private static bool SlotHasCamera(PlaybackViewModel vm, int slotIndex)
        {
            return slotIndex >= 0 &&
                   slotIndex < vm.PlaybackSlots.Count &&
                   vm.PlaybackSlots[slotIndex].HasCamera;
        }

        public PlaybackView()
        {
            InitializeComponent();
            Loaded += PlaybackView_Loaded;
            Unloaded += PlaybackView_Unloaded;
            DataContextChanged += PlaybackView_DataContextChanged;
            IsVisibleChanged += PlaybackView_IsVisibleChanged;
            PreviewKeyDown += PlaybackView_PreviewKeyDown;
            PlaybackViewport.SizeChanged += PlaybackViewport_SizeChanged;
        }

        private IReadOnlyList<VideoCanvas> PlaybackHosts => new[]
        {
            PlaybackHost1,
            PlaybackHost2,
            PlaybackHost3,
            PlaybackHost4
        };

        private IReadOnlyList<Border> PlaybackTiles => new[]
        {
            PlaybackTile1,
            PlaybackTile2,
            PlaybackTile3,
            PlaybackTile4
        };


        private (double width, double height) UpdatePlaybackHostLayout()
        {
            if (PlaybackContentGrid == null || PlaybackContentGrid.ActualWidth <= 0)
                return (0, 0);

            double availableWidth = Math.Max(64, PlaybackContentGrid.ActualWidth);
            double totalHeight = PlaybackContentGrid.ActualHeight > 0
                ? PlaybackContentGrid.ActualHeight
                : (ActualHeight > 0 ? ActualHeight : PlaybackContentGrid.ActualHeight);

            double topBarHeight = PlaybackTopBar?.ActualHeight ?? 0;
            double chromePadding = 8;
            double maxStageHeight = Math.Max(260, totalHeight - topBarHeight - chromePadding);

            double targetAspect = DefaultPlaybackAspect;

            if (DataContext is PlaybackViewModel vm &&
                vm.PlaybackLayoutMode == PlaybackLayoutMode.Single &&
                vm.VideoAspectRatio > 0.3)
            {
                targetAspect = vm.VideoAspectRatio;
            }

            double contentHeight = Math.Max(64, Math.Floor(maxStageHeight));
            double contentWidth = Math.Max(64, Math.Floor(contentHeight * targetAspect));

            if (contentWidth > availableWidth)
            {
                contentWidth = Math.Max(64, Math.Floor(availableWidth));
                contentHeight = Math.Max(64, Math.Floor(contentWidth / targetAspect));
            }

            PlaybackStage.Width = Math.Max(64, Math.Floor(availableWidth));
            PlaybackStage.Height = Math.Max(64, Math.Min(contentHeight, maxStageHeight));
            return (contentWidth, contentHeight);
        }

        private static (int widthPx, int heightPx) ToPixelSize(Visual visual, double widthDip, double heightDip)
        {
            var dpi = VisualTreeHelper.GetDpi(visual);
            double scaleX = dpi.DpiScaleX <= 0 ? 1.0 : dpi.DpiScaleX;
            double scaleY = dpi.DpiScaleY <= 0 ? 1.0 : dpi.DpiScaleY;
            int widthPx = Math.Max(64, (int)Math.Round(widthDip * scaleX));
            int heightPx = Math.Max(64, (int)Math.Round(heightDip * scaleY));
            return (widthPx, heightPx);
        }


        private async void PlaybackView_Loaded(object sender, RoutedEventArgs e)
        {
            AttachOwnerWindowHandlers();
            UpdateOwnerWindowState();

            if (DataContext is not PlaybackViewModel vm)
                return;

            ApplyPlaybackTileLayout(vm);
            await vm.InitializeAsync();
            SyncCameraSelectionFromViewModel(vm);

            // DO NOT auto-attach hosts here.
            // DO NOT auto-start playback here.
        }

        private async void PlaybackViewport_SizeChanged(object sender, SizeChangedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;

            // Only sync the full host layout for single-camera playback.
            // Multi-camera playback should not be driven by live viewport resize churn.
            if (vm.PlaybackLayoutMode == PlaybackLayoutMode.Single)
                await SchedulePlaybackHostLayoutSyncAsync();
        }

        private async Task SchedulePlaybackHostLayoutSyncAsync(int delayMs = 80)
        {
            _layoutSyncDebounceCts?.Cancel();
            _layoutSyncDebounceCts = new CancellationTokenSource();
            var token = _layoutSyncDebounceCts.Token;

            try
            {
                await Task.Delay(delayMs, token);
                if (token.IsCancellationRequested)
                    return;

                await SyncPlaybackHostLayoutAsync();
            }
            catch (TaskCanceledException)
            {
            }
        }

        private async void PlaybackView_Unloaded(object sender, RoutedEventArgs e)
        {
            await ExitPlaybackFullScreenAsync(animated: false);

            _hostAttachCts?.Cancel();
            _hostAttachCts = null;
            DetachOwnerWindowHandlers();
            IsOwnerWindowActive = false;

            // Popup creates its own HWND and can otherwise linger visually if the view is removed.
            if (PlaybackOverlayPopup != null)
                PlaybackOverlayPopup.IsOpen = false;

            if (DataContext is PlaybackViewModel vm)
                await vm.DeactivateAsync();
        }

        private void PlaybackView_IsVisibleChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            UpdateOwnerWindowState();
            if (!(e.NewValue is bool isVisible) || !isVisible)
            {
                if (PlaybackOverlayPopup != null)
                    PlaybackOverlayPopup.IsOpen = false;
            }
        }

        private void AttachOwnerWindowHandlers()
        {
            var window = Window.GetWindow(this);
            if (window == null || ReferenceEquals(window, _ownerWindow))
                return;

            DetachOwnerWindowHandlers();
            _ownerWindow = window;
            _ownerWindow.Activated += OwnerWindow_ActivationChanged;
            _ownerWindow.Deactivated += OwnerWindow_ActivationChanged;
            _ownerWindow.StateChanged += OwnerWindow_ActivationChanged;
        }

        private void DetachOwnerWindowHandlers()
        {
            if (_ownerWindow == null)
                return;

            _ownerWindow.Activated -= OwnerWindow_ActivationChanged;
            _ownerWindow.Deactivated -= OwnerWindow_ActivationChanged;
            _ownerWindow.StateChanged -= OwnerWindow_ActivationChanged;
            _ownerWindow = null;
        }

        private void OwnerWindow_ActivationChanged(object? sender, EventArgs e)
        {
            UpdateOwnerWindowState();
            if (!IsOwnerWindowActive && PlaybackOverlayPopup != null)
                PlaybackOverlayPopup.IsOpen = false;
        }

        private void UpdateOwnerWindowState()
        {
            var window = _ownerWindow ?? Window.GetWindow(this);
            IsOwnerWindowActive = window?.IsActive == true && IsVisible && IsLoaded;
        }

        private void PlaybackTimelineHost_OnSizeChanged(object sender, SizeChangedEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm)
                vm.UpdateTimelineWidth(e.NewSize.Width);
        }

        private bool _isDraggingScrubber = false;

        private void TimelineHost_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
        {
            if (sender is not FrameworkElement fe || DataContext is not PlaybackViewModel vm) return;

            fe.CaptureMouse();
            _isDraggingScrubber = true;
            vm.IsTimelineScrubbing = true;

            UpdateScrubberPosition(fe, e, vm);
        }

        private void TimelineHost_MouseMove(object sender, System.Windows.Input.MouseEventArgs e)
        {
            if (!_isDraggingScrubber || sender is not FrameworkElement fe || DataContext is not PlaybackViewModel vm) return;

            UpdateScrubberPosition(fe, e, vm);
        }

        private async void TimelineHost_MouseLeftButtonUp(object sender, MouseButtonEventArgs e)
        {
            if (!_isDraggingScrubber || sender is not FrameworkElement fe || DataContext is not PlaybackViewModel vm) return;

            fe.ReleaseMouseCapture();
            _isDraggingScrubber = false;
            vm.IsTimelineScrubbing = false;

            UpdateScrubberPosition(fe, e, vm);

            await vm.SeekToWindowSecondsAsync(vm.CurrentTimelineSeconds, autoPlay: vm.ShouldResumePlayback);
        }

        private void UpdateScrubberPosition(FrameworkElement fe, System.Windows.Input.MouseEventArgs e, PlaybackViewModel vm)
        {
            var pos = e.GetPosition(fe);
            double paddingX = 6.0; // TimelineHost horizontal padding
            double trackWidth = Math.Max(1, fe.ActualWidth - (paddingX * 2));
            double mouseX = pos.X - paddingX;
            double ratio = Math.Max(0, Math.Min(1, mouseX / trackWidth));
            
            vm.CurrentTimelineSeconds = ratio * Math.Max(1, vm.TotalTimelineSeconds);
        }

        private async void TimelineHost_MouseWheel(object sender, MouseWheelEventArgs e)
        {
            if (sender is not FrameworkElement fe || DataContext is not PlaybackViewModel vm)
                return;

            var pos = e.GetPosition(fe);
            double paddingX = 6.0;
            double trackWidth = Math.Max(1, fe.ActualWidth - (paddingX * 2));
            double mouseX = pos.X - paddingX;
            double ratio = Math.Max(0, Math.Min(1, mouseX / trackWidth));
            double targetSeconds = ratio * Math.Max(1, vm.TotalTimelineSeconds);

            await vm.ZoomTimelineAtWindowSecondsAsync(targetSeconds, zoomIn: e.Delta > 0);
            e.Handled = true;
        }


        private void PlaybackView_DataContextChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (e.OldValue is INotifyPropertyChanged oldNotify)
                oldNotify.PropertyChanged -= PlaybackViewModel_PropertyChanged;

            if (e.NewValue is INotifyPropertyChanged newNotify)
                newNotify.PropertyChanged += PlaybackViewModel_PropertyChanged;

            if (e.NewValue is PlaybackViewModel vm)
                SyncCameraSelectionFromViewModel(vm);

            if (_playbackFullScreenWindow != null)
                _playbackFullScreenWindow.DataContext = e.NewValue;
        }

        private void SyncCameraSelectionFromViewModel(PlaybackViewModel vm)
        {
            _suppressCameraSelectionEvents = true;
            try
            {
                var selectedIds = vm.GetSelectedPlaybackCameras()
                    .Select(c => c.Id)
                    .Where(id => !string.IsNullOrWhiteSpace(id))
                    .ToHashSet(StringComparer.OrdinalIgnoreCase);

                foreach (var choice in vm.AvailablePlaybackCameras)
                    choice.IsSelected = selectedIds.Contains(choice.Camera.Id);
            }
            finally
            {
                _suppressCameraSelectionEvents = false;
            }
        }

        private async void PlaybackViewModel_PropertyChanged(object? sender, PropertyChangedEventArgs e)
        {
            if (sender is not PlaybackViewModel vm)
                return;

            if (e.PropertyName == nameof(PlaybackViewModel.VideoAspectRatio) &&
                vm.PlaybackLayoutMode == PlaybackLayoutMode.Single)
            {
                await SchedulePlaybackHostLayoutSyncAsync();
            }
            else if (e.PropertyName == nameof(PlaybackViewModel.SelectedPlaybackCount))
            {
                // Selection-count changes happen during checkbox interactions.
                // Do not trigger heavy reload/layout pipelines here, otherwise
                // we race with the main selection load and cancel it repeatedly.
                ApplyPlaybackTileLayout(vm);
                SyncCameraSelectionFromViewModel(vm);
                if (_isFullScreenActive && vm.SelectedPlaybackCount == 0)
                    await ExitPlaybackFullScreenAsync(animated: true);
            }
            else if (e.PropertyName == nameof(PlaybackViewModel.PlaybackLayoutMode))
            {
                ApplyPlaybackTileLayout(vm);
                SyncCameraSelectionFromViewModel(vm);

                if (vm.PlaybackLayoutMode == PlaybackLayoutMode.Single)
                {
                    await Task.Delay(1);
                    await EnsureHostsAttachedAsync(vm);
                    await Task.Delay(120);
                    await RefreshVisiblePlaybackHostSizesAsync(vm);
                    await vm.RefreshPrimaryPlaybackAfterLayoutAsync();
                    await SchedulePlaybackHostLayoutSyncAsync(120);
                }
                else
                {
                    await ScheduleStableSelectionRefreshAsync(vm);
                }
            }
        }

        private async Task ScheduleStableSelectionRefreshAsync(PlaybackViewModel vm)
        {
            _selectionRefreshCts?.Cancel();
            _selectionRefreshCts = new CancellationTokenSource();
            var token = _selectionRefreshCts.Token;

            try
            {
                await Task.Delay(180, token);

                ApplyPlaybackTileLayout(vm);
                SyncCameraSelectionFromViewModel(vm);

                await Task.Delay(1, token);
                await EnsureHostsAttachedAsync(vm);

                await Task.Delay(120, token);
                await RefreshVisiblePlaybackHostSizesAsync(vm);

                if (vm.PlaybackLayoutMode == PlaybackLayoutMode.Quad)
                    await vm.RunStableSelectionRefreshAsync();
            }
            catch (TaskCanceledException)
            {
            }
        }

        private async Task RefreshVisiblePlaybackHostSizesAsync(PlaybackViewModel vm)
        {
            int activeCount = vm.PlaybackLayoutMode == PlaybackLayoutMode.Single
                ? 1
                : Math.Min(4, PlaybackHosts.Count);

            for (int slotIndex = 0; slotIndex < activeCount; slotIndex++)
            {
                var host = PlaybackHosts[slotIndex];
                if (!IsHostReady(host))
                    continue;

                var px = ToPixelSize(host, host.ActualWidth, host.ActualHeight);

                if (slotIndex == 0)
                    await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
                else
                    await vm.UpdateSecondaryVideoHostSizeAsync(slotIndex, px.widthPx, px.heightPx);
            }
        }

        private void ResetTileLayout(Border tile, int row, int column)
        {
            tile.Visibility = Visibility.Visible;
            Grid.SetRow(tile, row);
            Grid.SetColumn(tile, column);
            Grid.SetRowSpan(tile, 1);
            Grid.SetColumnSpan(tile, 1);
        }

        private void ApplyPlaybackTileLayout(PlaybackViewModel vm)
        {
            if (PlaybackTiles == null || PlaybackTiles.Count != 4)
                return;

            ResetTileLayout(PlaybackTile1, 0, 0);
            ResetTileLayout(PlaybackTile2, 0, 1);
            ResetTileLayout(PlaybackTile3, 1, 0);
            ResetTileLayout(PlaybackTile4, 1, 1);

            // Manual mode toggle:
            // Single View -> tile 1 full size, others hidden
            // Quad View   -> all 4 tiles visible in 2x2 grid
            if (vm.PlaybackLayoutMode == PlaybackLayoutMode.Quad)
            {
                PlaybackTile1.Visibility = Visibility.Visible;
                PlaybackTile2.Visibility = Visibility.Visible;
                PlaybackTile3.Visibility = Visibility.Visible;
                PlaybackTile4.Visibility = Visibility.Visible;
                return;
            }

            Grid.SetRow(PlaybackTile1, 0);
            Grid.SetColumn(PlaybackTile1, 0);
            Grid.SetRowSpan(PlaybackTile1, 2);
            Grid.SetColumnSpan(PlaybackTile1, 2);

            PlaybackTile2.Visibility = Visibility.Hidden;
            PlaybackTile3.Visibility = Visibility.Hidden;
            PlaybackTile4.Visibility = Visibility.Hidden;
        }

        private static bool IsHostReady(VideoCanvas host)
        {
            return host.Handle != IntPtr.Zero &&
                   host.ActualWidth >= 64 &&
                   host.ActualHeight >= 64;
        }

        private async Task EnsureHostsAttachedAsync(PlaybackViewModel vm)
        {
            if (!vm.PlaybackActivatedByUser)
                return;

            _hostAttachCts?.Cancel();
            _hostAttachCts = new CancellationTokenSource();
            var token = _hostAttachCts.Token;

            int activeCount = GetEffectiveLayoutCount(vm);

            var slotsToAttach = Enumerable.Range(0, activeCount)
                .Where(slotIndex => SlotHasCamera(vm, slotIndex))
                .ToArray();

            if (slotsToAttach.Length == 0)
                return;

            for (int i = 0; i < 120; i++)
            {
                if (token.IsCancellationRequested)
                    return;

                bool activeHostsReady = true;
                foreach (int slotIndex in slotsToAttach)
                {
                    if (!IsHostReady(PlaybackHosts[slotIndex]))
                    {
                        activeHostsReady = false;
                        break;
                    }
                }

                if (activeHostsReady)
                {
                    UpdatePlaybackHostLayout();

                    foreach (int slotIndex in slotsToAttach)
                    {
                        var host = PlaybackHosts[slotIndex];
                        var px = ToPixelSize(host, host.ActualWidth, host.ActualHeight);

                        if (slotIndex == 0)
                        {
                            if (!vm.IsPrimaryHostAlreadyAttached(host.Handle))
                                await vm.AttachVideoHostAsync(host.Handle);

                            await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
                        }
                        else
                        {
                            if (!vm.IsSecondaryHostAlreadyAttached(slotIndex, host.Handle))
                                await vm.AttachSecondaryVideoHostAsync(slotIndex, host.Handle);

                            await vm.UpdateSecondaryVideoHostSizeAsync(slotIndex, px.widthPx, px.heightPx);
                        }
                    }

                    return;
                }

                await Task.Delay(50, token);
            }
        }

        private async Task SyncPlaybackHostLayoutAsync()
        {
            if (_layoutSyncInFlight || DataContext is not PlaybackViewModel vm)
                return;

            var size = UpdatePlaybackHostLayout();
            if (size.width <= 0 || size.height <= 0)
                return;

            int activeCount = GetEffectiveLayoutCount(vm);

            _layoutSyncInFlight = true;
            try
            {
                for (int slotIndex = 0; slotIndex < activeCount; slotIndex++)
                {
                    var host = PlaybackHosts[slotIndex];
                    if (!IsHostReady(host))
                        continue;

                    var px = ToPixelSize(host, host.ActualWidth, host.ActualHeight);
                    if (slotIndex == 0)
                        await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
                    else
                        await vm.UpdateSecondaryVideoHostSizeAsync(slotIndex, px.widthPx, px.heightPx);
                }
            }
            finally
            {
                _layoutSyncInFlight = false;
            }
        }

        private async void PlaybackTileHost_Loaded(object sender, RoutedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm || sender is not VideoCanvas host)
                return;

            if (!vm.PlaybackActivatedByUser)
                return;

            int slotIndex = Convert.ToInt32(host.Tag);

            if (!SlotHasCamera(vm, slotIndex))
                return;

            if (slotIndex > 0 && vm.IsSecondaryHostAlreadyAttached(slotIndex, host.Handle))
                return;

            var px = ToPixelSize(host, Math.Max(64, host.ActualWidth), Math.Max(64, host.ActualHeight));

            TSVmsDesktop.Services.VideoService.Log(
                $"[TS-VMS] [Playback] PLAYBACK_HOST_ATTACH slot={slotIndex + 1} hwnd={host.Handle} size={px.widthPx}x{px.heightPx}");

            if (slotIndex == 0)
            {
                if (!vm.IsPrimaryHostAlreadyAttached(host.Handle))
                    await vm.AttachVideoHostAsync(host.Handle);

                await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
            }
            else
            {
                if (!vm.IsSecondaryHostAlreadyAttached(slotIndex, host.Handle))
                    await vm.AttachSecondaryVideoHostAsync(slotIndex, host.Handle);

                await vm.UpdateSecondaryVideoHostSizeAsync(slotIndex, px.widthPx, px.heightPx);
            }
        }

        private void PlaybackTileHost_SizeChanged(object sender, SizeChangedEventArgs e)
        {
            // Runtime resizing disabled by request.
        }

        private async void PlaybackCameraCheckBox_Click(object sender, RoutedEventArgs e)
        {
            if (_suppressCameraSelectionEvents || DataContext is not PlaybackViewModel vm)
                return;

            if (sender is not System.Windows.Controls.CheckBox checkBox ||
                checkBox.DataContext is not PlaybackCameraChoice changedChoice ||
                changedChoice.Camera == null)
                return;

            bool isNowChecked = checkBox.IsChecked == true;

            if (isNowChecked && vm.SelectedPlaybackCount >= 4)
            {
                var alreadySelected = vm.GetSelectedPlaybackCameras()
                    .Any(c => string.Equals(c.Id, changedChoice.Camera.Id, StringComparison.OrdinalIgnoreCase));

                if (!alreadySelected)
                {
                    _suppressCameraSelectionEvents = true;
                    try
                    {
                        checkBox.IsChecked = false;
                        changedChoice.IsSelected = false;
                    }
                    finally
                    {
                        _suppressCameraSelectionEvents = false;
                    }
                    return;
                }
            }

            vm.MarkPlaybackActivatedByUser();
            await vm.SetPlaybackCameraCheckedAsync(changedChoice.Camera, isNowChecked);

            if (!isNowChecked)
                return;

            ApplyPlaybackTileLayout(vm);
            SyncCameraSelectionFromViewModel(vm);

            await Task.Delay(1);
            await EnsureHostsAttachedAsync(vm);
            await Task.Delay(120);
            await RefreshVisiblePlaybackHostSizesAsync(vm);
            await vm.RefreshPrimaryPlaybackAfterLayoutAsync();

            if (vm.PlaybackLayoutMode == PlaybackLayoutMode.Quad)
                await vm.RunStableSelectionRefreshAsync();
        }

        private async void SingleLayoutButton_Click(object sender, RoutedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;
            if (vm.IsLoading)
                return;

            vm.MarkPlaybackActivatedByUser();
            if (vm.SetSinglePlaybackLayoutCommand.CanExecute(null))
                await vm.SetSinglePlaybackLayoutCommand.ExecuteAsync(null);
        }

        private async void QuadLayoutButton_Click(object sender, RoutedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;
            if (vm.IsLoading)
                return;

            vm.MarkPlaybackActivatedByUser();
            if (vm.SetQuadPlaybackLayoutCommand.CanExecute(null))
                await vm.SetQuadPlaybackLayoutCommand.ExecuteAsync(null);
        }

        private async void SeekBackButton_Click(object sender, RoutedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;
            if (vm.IsLoading)
                return;

            await vm.SeekBackCommand();
        }

        private async void SeekForwardButton_Click(object sender, RoutedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;
            if (vm.IsLoading)
                return;

            await vm.SeekForwardCommand();
        }

        private async void PreviousJumpButton_Click(object sender, RoutedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;
            if (vm.IsLoading)
                return;

            await vm.PreviousCommand();
        }

        private async void NextJumpButton_Click(object sender, RoutedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;
            if (vm.IsLoading)
                return;

            await vm.NextCommand();
        }

        private async void ToggleFullScreenButton_Click(object sender, RoutedEventArgs e)
        {
            if (_isFullScreenTransitionInFlight)
                return;
            if (DataContext is not PlaybackViewModel vm || vm.SelectedPlaybackCount <= 0)
                return;

            if (_isFullScreenActive)
                await ExitPlaybackFullScreenAsync(animated: true);
            else
                await EnterPlaybackFullScreenAsync();
        }

        private async void ExitFullScreenButton_Click(object sender, RoutedEventArgs e)
        {
            await ExitPlaybackFullScreenAsync(animated: true);
        }

        private async void PlaybackView_PreviewKeyDown(object sender, System.Windows.Input.KeyEventArgs e)
        {
            if (e.Key == Key.Escape && _isFullScreenActive)
            {
                e.Handled = true;
                await ExitPlaybackFullScreenAsync(animated: true);
            }
        }

        private async Task EnterPlaybackFullScreenAsync()
        {
            if (_isFullScreenActive || _isFullScreenTransitionInFlight)
                return;

            _isFullScreenTransitionInFlight = true;
            try
            {
                EnsurePlaybackFullScreenWindow();
                if (_playbackFullScreenWindow == null ||
                    _playbackFullScreenContentHost == null ||
                    _playbackFullScreenTimelineHost == null)
                {
                    return;
                }

                _playbackFullScreenWindow.DataContext = DataContext;

                if (PlaybackContentGrid.Parent is System.Windows.Controls.Panel rootPanel)
                    rootPanel.Children.Remove(PlaybackContentGrid);
                if (PlaybackTimelinePanel.Parent is System.Windows.Controls.Panel timelinePanel)
                    timelinePanel.Children.Remove(PlaybackTimelinePanel);

                _playbackFullScreenContentHost.Children.Add(PlaybackContentGrid);
                _playbackFullScreenTimelineHost.Children.Add(PlaybackTimelinePanel);

                Grid.SetRow(PlaybackContentGrid, 0);
                Grid.SetColumn(PlaybackContentGrid, 0);
                Grid.SetRow(PlaybackTimelinePanel, 0);
                Grid.SetColumn(PlaybackTimelinePanel, 0);
                Grid.SetColumnSpan(PlaybackTimelinePanel, 1);

                _playbackFullScreenWindow.Opacity = 0;
                _playbackFullScreenWindow.Show();
                _playbackFullScreenWindow.Activate();

                var fadeIn = new DoubleAnimation(0, 1, TimeSpan.FromMilliseconds(230))
                {
                    EasingFunction = new QuadraticEase { EasingMode = EasingMode.EaseOut }
                };
                _playbackFullScreenWindow.BeginAnimation(Window.OpacityProperty, fadeIn);
                _isFullScreenActive = true;

                await Task.Delay(30);
                ApplyPlaybackLayoutAfterFullScreenToggle();
                _playbackFullScreenWindow.Focus();
            }
            finally
            {
                _isFullScreenTransitionInFlight = false;
            }
        }

        private async Task ExitPlaybackFullScreenAsync(bool animated)
        {
            if (!_isFullScreenActive || _isFullScreenTransitionInFlight)
                return;

            _isFullScreenTransitionInFlight = true;
            try
            {
                if (animated && _playbackFullScreenWindow != null)
                {
                    var fadeOut = new DoubleAnimation(_playbackFullScreenWindow.Opacity, 0, TimeSpan.FromMilliseconds(190))
                    {
                        EasingFunction = new QuadraticEase { EasingMode = EasingMode.EaseIn }
                    };
                    _playbackFullScreenWindow.BeginAnimation(Window.OpacityProperty, fadeOut);
                    await Task.Delay(200);
                }

                if (PlaybackContentGrid.Parent is System.Windows.Controls.Panel fsContentPanel)
                    fsContentPanel.Children.Remove(PlaybackContentGrid);
                if (PlaybackTimelinePanel.Parent is System.Windows.Controls.Panel fsTimelinePanel)
                    fsTimelinePanel.Children.Remove(PlaybackTimelinePanel);

                PlaybackRootGrid.Children.Add(PlaybackContentGrid);
                PlaybackRootGrid.Children.Add(PlaybackTimelinePanel);

                Grid.SetRow(PlaybackContentGrid, 0);
                Grid.SetColumn(PlaybackContentGrid, 2);

                Grid.SetRow(PlaybackTimelinePanel, 1);
                Grid.SetColumn(PlaybackTimelinePanel, 0);
                Grid.SetColumnSpan(PlaybackTimelinePanel, 3);

                if (_playbackFullScreenWindow != null)
                {
                    _playbackFullScreenWindow.BeginAnimation(Window.OpacityProperty, null);
                    _playbackFullScreenWindow.Hide();
                }

                PlaybackRootGrid.Opacity = 0;
                PlaybackRootGrid.BeginAnimation(UIElement.OpacityProperty, new DoubleAnimation(0, 1, TimeSpan.FromMilliseconds(180))
                {
                    EasingFunction = new QuadraticEase { EasingMode = EasingMode.EaseOut }
                });

                _isFullScreenActive = false;

                await Task.Delay(20);
                ApplyPlaybackLayoutAfterFullScreenToggle();
            }
            finally
            {
                _isFullScreenTransitionInFlight = false;
            }
        }

        private void EnsurePlaybackFullScreenWindow()
        {
            if (_playbackFullScreenWindow != null)
                return;

            var owner = Window.GetWindow(this);

            var root = new Grid
            {
                Background = new System.Windows.Media.SolidColorBrush(
                    (System.Windows.Media.Color)System.Windows.Media.ColorConverter.ConvertFromString("#151515"))
            };
            root.RowDefinitions.Add(new RowDefinition { Height = new GridLength(1, GridUnitType.Star) });
            root.RowDefinitions.Add(new RowDefinition { Height = GridLength.Auto });

            _playbackFullScreenContentHost = new Grid();
            _playbackFullScreenTimelineHost = new Grid();
            Grid.SetRow(_playbackFullScreenContentHost, 0);
            Grid.SetRow(_playbackFullScreenTimelineHost, 1);
            root.Children.Add(_playbackFullScreenContentHost);
            root.Children.Add(_playbackFullScreenTimelineHost);

            var exitButton = new System.Windows.Controls.Button
            {
                Width = 34,
                Height = 34,
                Content = "X",
                ToolTip = "Exit Fullscreen",
                Margin = new Thickness(0, 10, 12, 0),
                HorizontalAlignment = System.Windows.HorizontalAlignment.Right,
                VerticalAlignment = System.Windows.VerticalAlignment.Top,
                Background = new System.Windows.Media.SolidColorBrush(
                    (System.Windows.Media.Color)System.Windows.Media.ColorConverter.ConvertFromString("#AA1E1F22")),
                Foreground = System.Windows.Media.Brushes.White,
                BorderBrush = new System.Windows.Media.SolidColorBrush(
                    (System.Windows.Media.Color)System.Windows.Media.ColorConverter.ConvertFromString("#4A4A4F")),
                BorderThickness = new Thickness(1)
            };
            exitButton.Click += ExitFullScreenButton_Click;
            root.Children.Add(exitButton);

            _playbackFullScreenWindow = new Window
            {
                Content = root,
                WindowStyle = WindowStyle.None,
                ResizeMode = ResizeMode.NoResize,
                ShowInTaskbar = false,
                Background = System.Windows.Media.Brushes.Black,
                Topmost = true,
                WindowStartupLocation = WindowStartupLocation.Manual,
                Focusable = true,
                DataContext = DataContext
            };

            if (owner != null)
            {
                _playbackFullScreenWindow.Owner = owner;

                var ownerHwnd = new WindowInteropHelper(owner).Handle;
                var screen = System.Windows.Forms.Screen.FromHandle(ownerHwnd);
                _playbackFullScreenWindow.Left = screen.Bounds.Left;
                _playbackFullScreenWindow.Top = screen.Bounds.Top;
                _playbackFullScreenWindow.Width = screen.Bounds.Width;
                _playbackFullScreenWindow.Height = screen.Bounds.Height;
            }
            else
            {
                _playbackFullScreenWindow.Left = SystemParameters.VirtualScreenLeft;
                _playbackFullScreenWindow.Top = SystemParameters.VirtualScreenTop;
                _playbackFullScreenWindow.Width = SystemParameters.VirtualScreenWidth;
                _playbackFullScreenWindow.Height = SystemParameters.VirtualScreenHeight;
            }

            _playbackFullScreenWindow.PreviewKeyDown += FullScreenWindow_PreviewKeyDown;
            _playbackFullScreenWindow.KeyDown += FullScreenWindow_PreviewKeyDown;
            _playbackFullScreenWindow.Closed += (_, _) =>
            {
                _isFullScreenActive = false;
                _playbackFullScreenWindow = null;
                _playbackFullScreenContentHost = null;
                _playbackFullScreenTimelineHost = null;
            };
        }

        private async void FullScreenWindow_PreviewKeyDown(object sender, System.Windows.Input.KeyEventArgs e)
        {
            if (e.Key != Key.Escape || !_isFullScreenActive)
                return;

            e.Handled = true;
            await ExitPlaybackFullScreenAsync(animated: true);
        }

        private void ApplyPlaybackLayoutAfterFullScreenToggle()
        {
            if (DataContext is not PlaybackViewModel vm)
                return;

            ApplyPlaybackTileLayout(vm);
            _ = SchedulePlaybackHostLayoutSyncAsync(60);
        }
    }
}

