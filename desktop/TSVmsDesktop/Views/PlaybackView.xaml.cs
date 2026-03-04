using System;
using System.Windows.Controls;
using System.Windows.Input;
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
        }

        private async void PlaybackView_Loaded(object sender, System.Windows.RoutedEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm)
            {
                vm.AttachVideoHost(PlaybackHost.WindowHandle);
                await vm.InitializeAsync();
            }
        }

        private void PlaybackHost_HandleCreated(object? sender, IntPtr hwnd)
        {
            if (DataContext is PlaybackViewModel vm)
                vm.AttachVideoHost(hwnd);
        }

        private void PlaybackView_Unloaded(object sender, System.Windows.RoutedEventArgs e)
        {
            // keep view model/service alive across navigation if DI scopes choose to reuse it
        }

        private void TimelineSlider_PreviewMouseLeftButtonUp(object sender, MouseButtonEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm)
                vm.SeekFromTimeline(TimelineSlider.Value);
        }

        private async void SegmentsGrid_MouseDoubleClick(object sender, MouseButtonEventArgs e)
        {
            if (DataContext is PlaybackViewModel vm && vm.SelectedSegment != null)
            {
                await vm.PlaySegmentAsync(vm.SelectedSegment);
            }
        }
    }
}
