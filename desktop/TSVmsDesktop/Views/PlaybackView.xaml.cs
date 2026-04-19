using System;
using System.Collections.Generic;
using System.ComponentModel;
using System.Linq;
using System.Threading;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using TSVmsDesktop.Controls;
using TSVmsDesktop.Models;
using TSVmsDesktop.ViewModels;

namespace TSVmsDesktop.Views
{
    public partial class PlaybackView : System.Windows.Controls.UserControl
    {
        private const double DefaultPlaybackAspect = 16.0 / 9.0;
        private CancellationTokenSource? _hostAttachCts;
        private readonly SemaphoreSlim _layoutSyncGate = new(1, 1);
        private bool _layoutSyncInFlight;
        private bool _suppressCameraSelectionEvents;
        private CancellationTokenSource? _layoutSyncDebounceCts;
        private CancellationTokenSource? _selectionRefreshCts;

        private static int GetVisibleTileCount(PlaybackViewModel vm)
            => vm.PlaybackLayoutMode == PlaybackLayoutMode.Quad ? 4 : 1;

        private static int GetEffectiveLayoutCount(PlaybackViewModel vm)
            => vm.PlaybackLayoutMode == PlaybackLayoutMode.Quad ? 4 : 1;

        public PlaybackView()
        {
            InitializeComponent();
            Loaded += PlaybackView_Loaded;
            Unloaded += PlaybackView_Unloaded;
            DataContextChanged += PlaybackView_DataContextChanged;
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
            if (DataContext is not PlaybackViewModel vm)
                return;

            ApplyPlaybackTileLayout(vm);
            SyncCameraSelectionFromViewModel(vm);
            await EnsureHostsAttachedAsync(vm);
            await vm.InitializeAsync();
            await vm.EnsureActivePlaybackAsync();
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
            _hostAttachCts?.Cancel();
            _hostAttachCts = null;

            if (DataContext is PlaybackViewModel vm)
                await vm.DeactivateAsync();
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
            else if (e.PropertyName == nameof(PlaybackViewModel.SelectedPlaybackCount) ||
                     e.PropertyName == nameof(PlaybackViewModel.PlaybackLayoutMode))
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
            _hostAttachCts?.Cancel();
            _hostAttachCts = new CancellationTokenSource();
            var token = _hostAttachCts.Token;

            int activeCount = GetEffectiveLayoutCount(vm);

            for (int i = 0; i < 120; i++)
            {
                if (token.IsCancellationRequested)
                    return;

                bool activeHostsReady = true;
                for (int slotIndex = 0; slotIndex < activeCount; slotIndex++)
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

                    for (int slotIndex = 0; slotIndex < activeCount; slotIndex++)
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

            int slotIndex = Convert.ToInt32(host.Tag);

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
                await vm.AttachSecondaryVideoHostAsync(slotIndex, host.Handle);
                await vm.UpdateSecondaryVideoHostSizeAsync(slotIndex, px.widthPx, px.heightPx);
            }
        }

        private void PlaybackTileHost_SizeChanged(object sender, SizeChangedEventArgs e)
        {
            // Runtime resizing disabled by request.
        }

        private async void PlaybackCameraCheckBox_Changed(object sender, RoutedEventArgs e)
        {
            if (_suppressCameraSelectionEvents || DataContext is not PlaybackViewModel vm)
                return;

            if (sender is not System.Windows.Controls.CheckBox checkBox ||
                checkBox.DataContext is not PlaybackCameraChoice changedChoice ||
                changedChoice.Camera == null)
                return;

            bool isNowChecked = changedChoice.IsSelected;

            if (isNowChecked && vm.SelectedPlaybackCount >= 4)
            {
                var alreadySelected = vm.GetSelectedPlaybackCameras()
                    .Any(c => string.Equals(c.Id, changedChoice.Camera.Id, StringComparison.OrdinalIgnoreCase));

                if (!alreadySelected)
                {
                    changedChoice.IsSelected = false;
                    return;
                }
            }

            await vm.SetPlaybackCameraCheckedAsync(changedChoice.Camera, isNowChecked);
        }
    }
}
