using System;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Threading;
using TSVmsDesktop.ViewModels;

namespace TSVmsDesktop.Views
{
    public partial class PlaybackView : System.Windows.Controls.UserControl
    {
        private const double DefaultPlaybackAspect = 16.0 / 9.0;

        public PlaybackView()
        {
            InitializeComponent();
            Loaded += PlaybackView_Loaded;
            Unloaded += PlaybackView_Unloaded;
            PlaybackHost.HandleCreated += PlaybackHost_HandleCreated;

            // IMPORTANT: resize the viewport, not the HwndHost directly.
            PlaybackViewport.SizeChanged += PlaybackViewport_SizeChanged;
        }

        private void UpdatePlaybackHostLayout()
        {
            double vw = PlaybackViewport.ActualWidth;
            double vh = PlaybackViewport.ActualHeight;

            if (vw <= 0 || vh <= 0)
                return;

            double hostW = vw;
            double hostH = hostW / DefaultPlaybackAspect;

            if (hostH > vh)
            {
                hostH = vh;
                hostW = hostH * DefaultPlaybackAspect;
            }

            hostW = Math.Max(64, Math.Floor(hostW));
            hostH = Math.Max(64, Math.Floor(hostH));

            PlaybackHostFrame.Width = hostW;
            PlaybackHostFrame.Height = hostH;

            PlaybackHost.Width = double.NaN;
            PlaybackHost.Height = double.NaN;
        }

        private async void PlaybackView_Loaded(object sender, RoutedEventArgs e)
        {
            UpdatePlaybackHostLayout();

            if (DataContext is PlaybackViewModel vm)
            {
                var hwnd = await WaitForPlaybackHostReadyAsync();
                await vm.AttachVideoHostAsync(hwnd);
                await vm.UpdateVideoHostSizeAsync((int)PlaybackHost.ActualWidth, (int)PlaybackHost.ActualHeight);
                await vm.InitializeAsync();
                await vm.EnsureActivePlaybackAsync();
            }
        }

        private async void PlaybackHost_HandleCreated(object? sender, IntPtr hwnd)
        {
            UpdatePlaybackHostLayout();

            if (DataContext is PlaybackViewModel vm)
            {
                hwnd = await WaitForPlaybackHostReadyAsync();
                await vm.AttachVideoHostAsync(hwnd);
                await vm.UpdateVideoHostSizeAsync((int)PlaybackHost.ActualWidth, (int)PlaybackHost.ActualHeight);
                await vm.EnsureActivePlaybackAsync();
            }
        }

        private async void PlaybackViewport_SizeChanged(object sender, SizeChangedEventArgs e)
        {
            UpdatePlaybackHostLayout();

            if (DataContext is PlaybackViewModel vm)
            {
                await vm.UpdateVideoHostSizeAsync((int)PlaybackHost.ActualWidth, (int)PlaybackHost.ActualHeight);
            }
        }

        private async void PlaybackView_Unloaded(object sender, RoutedEventArgs e)
        {
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
                return PlaybackHost.WindowHandle;

            for (int i = 0; i < 80; i++)
            {
                await Dispatcher.InvokeAsync(() => { }, DispatcherPriority.Loaded);
                if (IsPlaybackHostReady())
                    return PlaybackHost.WindowHandle;
                await Task.Delay(25);
            }

            return PlaybackHost.WindowHandle;
        }

        private bool IsPlaybackHostReady()
        {
            return PlaybackHost.WindowHandle != IntPtr.Zero &&
                   PlaybackHost.ActualWidth >= 64 &&
                   PlaybackHost.ActualHeight >= 64;
        }
    }
}
