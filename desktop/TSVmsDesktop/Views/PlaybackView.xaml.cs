using System;
using System.ComponentModel;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;
using TSVmsDesktop.ViewModels;

namespace TSVmsDesktop.Views
{
    public partial class PlaybackView : System.Windows.Controls.UserControl
    {
        private const double DefaultPlaybackAspect = 16.0 / 9.0;
        private CancellationTokenSource? _hostAttachCts;
        private bool _layoutSyncInFlight;
        private int _lastSyncedWidthPx;
        private int _lastSyncedHeightPx;

        public PlaybackView()
        {
            InitializeComponent();
            Loaded += PlaybackView_Loaded;
            Unloaded += PlaybackView_Unloaded;
            DataContextChanged += PlaybackView_DataContextChanged;

            // IMPORTANT: resize the viewport, not the HwndHost directly.
            PlaybackViewport.SizeChanged += PlaybackViewport_SizeChanged;
            LayoutUpdated += PlaybackView_LayoutUpdated;
        }

        private (double width, double height) UpdatePlaybackHostLayout()
        {
            if (PlaybackContentGrid == null || PlaybackContentGrid.ActualWidth <= 0)
                return (0, 0);

            double availableWidth = Math.Max(64, PlaybackContentGrid.ActualWidth);
            double availableHeight = ActualHeight > 0 ? ActualHeight : PlaybackContentGrid.ActualHeight;
            double reservedHeight = 168;
            double maxStageHeight = Math.Max(360, availableHeight - reservedHeight);
            double targetAspect = DefaultPlaybackAspect;

            if (DataContext is PlaybackViewModel vm && vm.VideoAspectRatio > 0.1)
            {
                targetAspect = vm.VideoAspectRatio;
            }

            // Keep the height bounded by the operator controls, but always let the
            // playback stage consume the full horizontal workspace.
            double contentHeight = Math.Max(64, Math.Floor(maxStageHeight));
            double contentWidth = Math.Max(64, Math.Floor(contentHeight * targetAspect));

            if (contentWidth > availableWidth)
            {
                contentWidth = Math.Max(64, Math.Floor(availableWidth));
                contentHeight = Math.Max(64, Math.Floor(contentWidth / targetAspect));
            }

            double stageWidth = Math.Max(64, Math.Floor(availableWidth));

            PlaybackStage.Width = stageWidth;
            PlaybackStage.Height = contentHeight;
            PlaybackHostFrame.Width = contentWidth;
            PlaybackHostFrame.Height = contentHeight;

            PlaybackHost.Width = contentWidth;
            PlaybackHost.Height = contentHeight;

            return (contentWidth, contentHeight);
        }

        private (int widthPx, int heightPx) ToPixelSize(double widthDip, double heightDip)
        {
            var dpi = VisualTreeHelper.GetDpi(this);
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
                await EnsureHostAttachedAsync(vm);
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
            {
                await vm.DeactivateAsync();
            }
        }

        private void CoverageHost_OnSizeChanged(object sender, SizeChangedEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm)
            {
                vm.UpdateTimelineWidth(e.NewSize.Width);
            }
        }

        private async void CoverageHost_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
        {
            if (DataContext is not PlaybackViewModel vm)
                return;

            var pos = e.GetPosition(CoverageHost);
            double width = Math.Max(1, CoverageHost.ActualWidth);
            double ratio = Math.Max(0, Math.Min(1, pos.X / width));
            double seconds = ratio * Math.Max(1, vm.TotalTimelineSeconds);

            bool autoPlay = e.ClickCount >= 2;
            await vm.SeekToWindowSecondsAsync(seconds, autoPlay);
        }

        private void WindowSlider_DragCompleted(object sender, System.Windows.Controls.Primitives.DragCompletedEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm)
            {
                _ = vm.SeekToWindowSecondsAsync(WindowSlider.Value, autoPlay: false);
            }
        }

        private async Task<IntPtr> WaitForPlaybackHostReadyAsync()
        {
            if (IsPlaybackHostReady())
                return PlaybackHost.Handle;

            for (int i = 0; i < 80; i++)
            {
                await Dispatcher.InvokeAsync(() => { }, DispatcherPriority.Loaded);
                if (IsPlaybackHostReady())
                    return PlaybackHost.Handle;
                await Task.Delay(25);
            }

            return PlaybackHost.Handle;
        }

        private bool IsPlaybackHostReady()
        {
            return PlaybackHost.Handle != IntPtr.Zero &&
                   PlaybackHost.ActualWidth >= 64 &&
                   PlaybackHost.ActualHeight >= 64;
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
        }

        private async void PlaybackViewModel_PropertyChanged(object? sender, PropertyChangedEventArgs e)
        {
            if (e.PropertyName == nameof(PlaybackViewModel.VideoAspectRatio))
            {
                await SyncPlaybackHostLayoutAsync();
            }
        }

        private async Task EnsureHostAttachedAsync(PlaybackViewModel vm)
        {
            _hostAttachCts?.Cancel();
            _hostAttachCts = new CancellationTokenSource();
            var token = _hostAttachCts.Token;

            for (int i = 0; i < 120; i++)
            {
                if (token.IsCancellationRequested)
                    return;

                if (IsPlaybackHostReady())
                {
                    var size = UpdatePlaybackHostLayout();
                    var px = ToPixelSize(size.width, size.height);

                    await vm.AttachVideoHostAsync(PlaybackHost.Handle);
                    await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
                    _lastSyncedWidthPx = px.widthPx;
                    _lastSyncedHeightPx = px.heightPx;
                    return;
                }

                await Task.Delay(50, token);
            }
        }

        private async Task SyncPlaybackHostLayoutAsync()
        {
            if (_layoutSyncInFlight)
                return;

            if (DataContext is not PlaybackViewModel vm)
                return;

            var size = UpdatePlaybackHostLayout();
            if (size.width <= 0 || size.height <= 0)
                return;

            var px = ToPixelSize(size.width, size.height);
            if (px.widthPx == _lastSyncedWidthPx && px.heightPx == _lastSyncedHeightPx)
                return;

            _layoutSyncInFlight = true;
            try
            {
                if (PlaybackHost.Handle != IntPtr.Zero)
                {
                    await vm.UpdateVideoHostSizeAsync(px.widthPx, px.heightPx);
                    _lastSyncedWidthPx = px.widthPx;
                    _lastSyncedHeightPx = px.heightPx;
                }
            }
            finally
            {
                _layoutSyncInFlight = false;
            }
        }
    }
}
