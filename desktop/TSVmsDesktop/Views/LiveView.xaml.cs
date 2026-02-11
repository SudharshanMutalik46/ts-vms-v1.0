using System;
using System.Windows;
using System.Windows.Controls;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.ViewModels;
using TSVmsDesktop.Services;
using TSVmsDesktop.Controls;

namespace TSVmsDesktop.Views
{
    public partial class LiveView : System.Windows.Controls.UserControl
    {
        public LiveView()
        {
            InitializeComponent();
        }

        // 1. This runs when the tile becomes Visible (IsConnected = true)
        private void VideoSurface_IsVisibleChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (sender is VideoCanvas canvas)
            {
                if (canvas.Visibility == Visibility.Visible)
                {
                    // Ensure the visual is fully loaded to get a valid Handle
                    if (!canvas.IsLoaded)
                    {
                        // If not loaded yet, wait for the Loaded event instead
                        canvas.Loaded -= VideoSurface_Loaded; 
                        canvas.Loaded += VideoSurface_Loaded;
                        return;
                    }

                    StartVideo(canvas);
                }
            }
        }

        // 2. Fallback: Runs if the control wasn't loaded during the visibility change
        private void VideoSurface_Loaded(object sender, RoutedEventArgs e)
        {
            if (sender is VideoCanvas canvas)
            {
                canvas.Loaded -= VideoSurface_Loaded; // Remove handler so it doesn't fire twice
                StartVideo(canvas);
            }
        }

        // 3. The Actual Logic to Start the Stream
        private void StartVideo(VideoCanvas canvas)
        {
            if (canvas.Handle == IntPtr.Zero) return;

            if (canvas.DataContext is CameraSlot slot)
            {
                // Prevent duplicate streams
                if (slot.PipelineHandle != IntPtr.Zero) return;

                // FIX: Use 'Services' as defined in App.xaml.cs
                var videoService = App.Current.Services.GetRequiredService<VideoService>();
                
                // Fallback to "test" if URL is empty
                string urlToPlay = string.IsNullOrEmpty(slot.RtspUrl) ? "test" : slot.RtspUrl;

                // Log to Debug Output
                System.Diagnostics.Debug.WriteLine($"[TS-VMS] Requesting Stream for {slot.CameraName} (URL: {urlToPlay})");

                slot.PipelineHandle = videoService.StartStream(canvas.Handle, urlToPlay);
            }
        }

        // 4. Cleanup when leaving the view
        private void UserControl_Unloaded(object sender, RoutedEventArgs e)
        {
            var app = (App)System.Windows.Application.Current;
            if (app?.Services == null) return; // Safety check during shutdown

            var videoService = app.Services.GetRequiredService<VideoService>();
            
            if (this.DataContext is LiveViewModel vm)
            {
                foreach (var slot in vm.CameraGrid)
                {
                    if (slot.PipelineHandle != IntPtr.Zero)
                    {
                        videoService.StopStream(slot.PipelineHandle);
                        slot.PipelineHandle = IntPtr.Zero;
                    }
                }
            }
        }
    }
}
