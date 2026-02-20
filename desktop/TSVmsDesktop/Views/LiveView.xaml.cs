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
        private IntPtr _fullScreenPipeline = IntPtr.Zero;

        public LiveView()
        {
            InitializeComponent();
            this.DataContextChanged += OnDataContextChanged;
        }

        private void OnDataContextChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (DataContext is LiveViewModel vm)
            {
                vm.PropertyChanged += Vm_PropertyChanged;
            }
        }

        // Listen for Full Screen state changes
        private void Vm_PropertyChanged(object? sender, System.ComponentModel.PropertyChangedEventArgs e)
        {
            if (e.PropertyName == "IsFullScreen")
            {
                var vm = (LiveViewModel)DataContext;
                if (!vm.IsFullScreen)
                {
                    // STOP FULL SCREEN
                    StopFullScreenStream();
                }
                // START is handled by FullScreenPlayer_IsVisibleChanged
            }
        }

        // 1. This runs when the tile becomes Visible (IsConnected = true)
        private void VideoSurface_IsVisibleChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (sender is VideoCanvas canvas)
            {
                if (canvas.Visibility == Visibility.Visible)
                {
                    if (!canvas.IsLoaded)
                    {
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
                canvas.Loaded -= VideoSurface_Loaded;
                StartVideo(canvas);
            }
        }

        // 3. The Actual Logic to Start the Stream (Grid tiles)
        private void StartVideo(VideoCanvas canvas)
        {
            if (canvas.Handle == IntPtr.Zero) return;

            if (canvas.DataContext is CameraSlot slot)
            {
                var videoService = App.Current.Services.GetRequiredService<VideoService>();
                
                // Prevent duplicate streams, but RE-ATTACH if the window changed
                if (slot.PipelineHandle != IntPtr.Zero) 
                {
                    videoService.Reattach(slot.PipelineHandle, canvas.Handle);
                    return;
                }

                // Fallback to "test" if URL is empty
                string urlToPlay = string.IsNullOrEmpty(slot.RtspUrl) ? "test" : slot.RtspUrl;

                System.Diagnostics.Debug.WriteLine($"[TS-VMS] Requesting Stream for {slot.CameraName} (URL: {urlToPlay})");

                slot.PipelineHandle = videoService.StartStream(canvas.Handle, urlToPlay);
            }
        }

        private void FullScreenPlayer_IsVisibleChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (sender is VideoCanvas canvas && canvas.Visibility == Visibility.Visible)
            {
                // Ensure the view has focus so the ESC key works immediately
                this.Focus();

                if (!canvas.IsLoaded)
                {
                    canvas.Loaded += FullScreenPlayer_Loaded;
                    return;
                }
                StartFullScreenStream(canvas);
            }
        }

        private void FullScreenPlayer_Loaded(object sender, RoutedEventArgs e)
        {
            if (sender is VideoCanvas canvas)
            {
                canvas.Loaded -= FullScreenPlayer_Loaded;
                StartFullScreenStream(canvas);
            }
        }

        private void StartFullScreenStream(VideoCanvas canvas)
        {
            if (canvas.Handle == IntPtr.Zero) return;
            if (_fullScreenPipeline != IntPtr.Zero) return; // Already playing

            var vm = DataContext as LiveViewModel;
            if (vm == null || string.IsNullOrEmpty(vm.FullScreenUrl)) return;

            var videoService = App.Current.Services.GetRequiredService<VideoService>();
            
            System.Diagnostics.Debug.WriteLine($"[TS-VMS] Starting Full Screen Stream: {vm.FullScreenUrl}");
            _fullScreenPipeline = videoService.StartStream(canvas.Handle, vm.FullScreenUrl);
        }

        private void StopFullScreenStream()
        {
            if (_fullScreenPipeline != IntPtr.Zero)
            {
                var app = (App)System.Windows.Application.Current;
                if (app?.Services == null) return;

                var videoService = app.Services.GetRequiredService<VideoService>();
                videoService.StopStream(_fullScreenPipeline);
                _fullScreenPipeline = IntPtr.Zero;
                
                System.Diagnostics.Debug.WriteLine("[TS-VMS] Full Screen Stream Stopped.");
            }
        }

        // 5. Cleanup when leaving the view
        private void UserControl_Unloaded(object sender, RoutedEventArgs e)
        {
            var app = (App)System.Windows.Application.Current;
            if (app?.Services == null) return;

            var videoService = app.Services.GetRequiredService<VideoService>();
            
            // Reset Full Screen State to close Popup
            if (this.DataContext is LiveViewModel currentVm)
            {
                currentVm.IsFullScreen = false;
            }

            // Stop full screen if active
            StopFullScreenStream();

            // Stop all grid streams
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
