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
        private bool _layoutSyncInFlight;
        private bool _suppressCameraSelectionEvents;

        public PlaybackView()
        {
            InitializeComponent();
            Loaded += PlaybackView_Loaded;
            Unloaded += PlaybackView_Unloaded;
            DataContextChanged += PlaybackView_DataContextChanged;
            PlaybackViewport.SizeChanged += PlaybackViewport_SizeChanged;
            LayoutUpdated += PlaybackView_LayoutUpdated;
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
            double availableHeight = ActualHeight > 0 ? ActualHeight : PlaybackContentGrid.ActualHeight;
            double reservedHeight = 320;
            double maxStageHeight = Math.Max(260, availableHeight - reservedHeight);

            double targetAspect = DefaultPlaybackAspect;

            if (DataContext is PlaybackViewModel vm &&
                vm.SelectedPlaybackCount <= 1 &&
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
            PlaybackStage.Height = contentHeight;
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
            if (DataContext is PlaybackViewModel vm)
            {
                ApplyPlaybackTileLayout(vm.SelectedPlaybackCount);
                await EnsureHostsAttachedAsync(vm);
                SyncCameraSelectionFromViewModel(vm);
                await vm.InitializeAsync();
                await vm.EnsureActivePlaybackAsync();
            }
        }

        private async void PlaybackViewport_SizeChanged(object sender, SizeChangedEventArgs e)
        {
            await SyncPlaybackHostLayoutAsync();
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

        private async void PlaybackTimelineHost_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
        {
            if (sender is not FrameworkElement fe || DataContext is not PlaybackViewModel vm)
                return;

            var pos = e.GetPosition(fe);
            double width = Math.Max(1, fe.ActualWidth);
            double ratio = Math.Max(0, Math.Min(1, pos.X / width));
            double seconds = ratio * Math.Max(1, vm.TotalTimelineSeconds);

            bool autoPlay = e.ClickCount >= 2;
            await vm.SeekToWindowSecondsAsync(seconds, autoPlay);
        }

        private void WindowSlider_DragCompleted(object sender, System.Windows.Controls.Primitives.DragCompletedEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm)
                _ = vm.SeekToWindowSecondsAsync(WindowSlider.Value, autoPlay: false);
        }

        private async void PlaybackView_LayoutUpdated(object? sender, EventArgs e)
        {
            await SyncPlaybackHostLayoutAsync();
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
            if (e.PropertyName == nameof(PlaybackViewModel.VideoAspectRatio) &&
                sender is PlaybackViewModel v &&
                v.SelectedPlaybackCount <= 1)
            {
                await SyncPlaybackHostLayoutAsync();
            }
            else if (e.PropertyName == nameof(PlaybackViewModel.SelectedPlaybackCount) && sender is PlaybackViewModel vm)
            {
                ApplyPlaybackTileLayout(vm.SelectedPlaybackCount);
                SyncCameraSelectionFromViewModel(vm);
                await Task.Delay(1);
                await EnsureHostsAttachedAsync(vm);
                await SyncPlaybackHostLayoutAsync();
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

        private void ApplyPlaybackTileLayout(int selectedCount)
        {
            if (PlaybackTiles == null || PlaybackTiles.Count != 4)
                return;

            ResetTileLayout(PlaybackTile1, 0, 0);
            ResetTileLayout(PlaybackTile2, 0, 1);
            ResetTileLayout(PlaybackTile3, 1, 0);
            ResetTileLayout(PlaybackTile4, 1, 1);

            switch (selectedCount)
            {
                case 0:
                case 1:
                    Grid.SetRowSpan(PlaybackTile1, 2);
                    Grid.SetColumnSpan(PlaybackTile1, 2);
                    PlaybackTile2.Visibility = Visibility.Hidden;
                    PlaybackTile3.Visibility = Visibility.Hidden;
                    PlaybackTile4.Visibility = Visibility.Hidden;
                    break;

                case 2:
                    Grid.SetRowSpan(PlaybackTile1, 2);
                    Grid.SetRowSpan(PlaybackTile2, 2);
                    PlaybackTile3.Visibility = Visibility.Hidden;
                    PlaybackTile4.Visibility = Visibility.Hidden;
                    break;

                case 3:
                    // Primary on top, two secondary tiles below.
                    Grid.SetRow(PlaybackTile1, 0);
                    Grid.SetColumn(PlaybackTile1, 0);
                    Grid.SetRowSpan(PlaybackTile1, 1);
                    Grid.SetColumnSpan(PlaybackTile1, 2);

                    Grid.SetRow(PlaybackTile2, 1);
                    Grid.SetColumn(PlaybackTile2, 0);
                    Grid.SetRowSpan(PlaybackTile2, 1);
                    Grid.SetColumnSpan(PlaybackTile2, 1);

                    Grid.SetRow(PlaybackTile3, 1);
                    Grid.SetColumn(PlaybackTile3, 1);
                    Grid.SetRowSpan(PlaybackTile3, 1);
                    Grid.SetColumnSpan(PlaybackTile3, 1);

                    PlaybackTile4.Visibility = Visibility.Hidden;
                    break;

                default:
                    break;
            }
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

            int activeCount = vm.SelectedPlaybackCount <= 0
                ? 1
                : Math.Min(vm.SelectedPlaybackCount, PlaybackHosts.Count);

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

            int activeCount = vm.SelectedPlaybackCount <= 0
                ? 1
                : Math.Min(vm.SelectedPlaybackCount, PlaybackHosts.Count);

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
            var px = ToPixelSize(host, Math.Max(64, host.ActualWidth), Math.Max(64, host.ActualHeight));
            if (slotIndex == 0)
            {
                await vm.AttachVideoHostAsync(host.Handle);
                await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
            }
            else
            {
                await vm.AttachSecondaryVideoHostAsync(slotIndex, host.Handle);
                await vm.UpdateSecondaryVideoHostSizeAsync(slotIndex, px.widthPx, px.heightPx);
            }
        }

        private async void PlaybackTileHost_SizeChanged(object sender, SizeChangedEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm || sender is not VideoCanvas host)
                return;

            int slotIndex = Convert.ToInt32(host.Tag);
            var px = ToPixelSize(host, Math.Max(64, e.NewSize.Width), Math.Max(64, e.NewSize.Height));
            if (slotIndex == 0)
                await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
            else
                await vm.UpdateSecondaryVideoHostSizeAsync(slotIndex, px.widthPx, px.heightPx);
        }

        private async void PlaybackCameraCheckBox_Changed(object sender, RoutedEventArgs e)
        {
            if (_suppressCameraSelectionEvents || DataContext is not PlaybackViewModel vm)
                return;

            var selected = vm.AvailablePlaybackCameras
                .Where(c => c.IsSelected)
                .Select(c => c.Camera)
                .Take(4)
                .ToList();

            if (vm.AvailablePlaybackCameras.Count(c => c.IsSelected) > 4)
            {
                if (sender is System.Windows.Controls.CheckBox checkBox && checkBox.DataContext is PlaybackCameraChoice choice)
                    choice.IsSelected = false;
                return;
            }

            await vm.SetSelectedPlaybackCamerasAsync(selected);
        }
    }
}
