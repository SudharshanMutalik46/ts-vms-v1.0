using System;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Threading;
using System.Threading.Tasks;
using TSVmsDesktop.ViewModels;

namespace TSVmsDesktop.Views
{
    public partial class PlaybackView : System.Windows.Controls.UserControl
    {
        public PlaybackView()
        {
            InitializeComponent();
            Loaded += PlaybackView_Loaded;
            Unloaded += PlaybackView_Unloaded;
            PlaybackHost.HandleCreated += PlaybackHost_HandleCreated;
            PlaybackHost.SizeChanged += PlaybackHost_SizeChanged;
        }

        private async void PlaybackView_Loaded(object sender, RoutedEventArgs e)
        {
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
            if (DataContext is PlaybackViewModel vm)
            {
                hwnd = await WaitForPlaybackHostReadyAsync();
                await vm.AttachVideoHostAsync(hwnd);
                await vm.UpdateVideoHostSizeAsync((int)PlaybackHost.ActualWidth, (int)PlaybackHost.ActualHeight);
                await vm.EnsureActivePlaybackAsync();
            }
        }

        private async void PlaybackHost_SizeChanged(object sender, SizeChangedEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm)
            {
                await vm.UpdateVideoHostSizeAsync((int)e.NewSize.Width, (int)e.NewSize.Height);
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
