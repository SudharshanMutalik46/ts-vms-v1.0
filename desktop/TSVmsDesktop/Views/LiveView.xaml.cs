using System;
using System.Windows;
using System.Windows.Controls;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.ViewModels;
using TSVmsDesktop.Services;
using System.Linq;
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

        private async void UserControl_Loaded(object sender, RoutedEventArgs e)
        {
            if (this.DataContext is LiveViewModel vm)
            {
                await vm.ActivateAsync();
            }
        }

        private void OnDataContextChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (e.OldValue is LiveViewModel oldVm)
            {
                oldVm.PropertyChanged -= Vm_PropertyChanged;
            }
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
                else
                {
                    // Ensure the stream starts if we enter full screen while the control is already loaded
                    if (FullScreenPlayer.IsLoaded && FullScreenPlayer.Visibility == Visibility.Visible) 
                    {
                        StartFullScreenStream(FullScreenPlayer);
                    }
                }
            }
            // If the URL changes while already in Full Screen (e.g., from Double Click or another selection method)
            if (e.PropertyName == "FullScreenUrl")
            {
                var vm = (LiveViewModel)DataContext;
                if (vm.IsFullScreen && FullScreenPlayer.IsLoaded && FullScreenPlayer.Visibility == Visibility.Visible)
                {
                    StopFullScreenStream();
                    StartFullScreenStream(FullScreenPlayer);
                }
            }
        }

        private async void CameraGrid_MouseLeftButtonDown(object sender, System.Windows.Input.MouseButtonEventArgs e)
        {
            if (sender is FrameworkElement element && element.DataContext is CameraSlot slot)
            {
                if (this.DataContext is LiveViewModel vm)
                {
                    if (e.ClickCount == 1)
                    {
                        vm.SelectSlot(slot);
                    }
                    else if (e.ClickCount == 2)
                    {
                        await vm.EnterFullScreen(slot);
                    }
                }
            }
        }

        // 1. This runs when the tile becomes Visible (IsConnected = true)
        private void VideoSurface_IsVisibleChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (sender is not VideoCanvas canvas)
                return;

            if (canvas.Visibility == Visibility.Visible)
            {
                if (!canvas.IsLoaded)
                {
                    canvas.Loaded -= VideoSurface_Loaded;
                    canvas.Loaded += VideoSurface_Loaded;
                    return;
                }

                StartVideo(canvas);
                return;
            }

            if (canvas.DataContext is CameraSlot slot && slot.PipelineHandle != IntPtr.Zero)
            {
                var app = (App)System.Windows.Application.Current;
                if (app?.Services == null) return;

                var videoService = app.Services.GetRequiredService<VideoService>();
                var handle = slot.PipelineHandle;

                slot.PipelineHandle = IntPtr.Zero;
                slot.WindowHandle = IntPtr.Zero;
                slot.IsConnected = false;

                _ = System.Threading.Tasks.Task.Run(() =>
                {
                    try
                    {
                        videoService.StopStream(handle);
                    }
                    catch (Exception ex)
                    {
                        System.Diagnostics.Debug.WriteLine($"[TS-VMS] Hidden stop failed: {ex.Message}");
                    }
                });
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
        private async void StartVideo(VideoCanvas canvas)
        {
            // Wait for Win32 handle.
            int retries = 50;
            while (canvas.Handle == IntPtr.Zero && retries-- > 0)
            {
                await System.Threading.Tasks.Task.Delay(10);
            }

            if (canvas.Handle == IntPtr.Zero) return;

            // Wait for non-zero layout size without blocking the dispatcher.
            retries = 100;
            while ((canvas.ActualWidth < 2 || canvas.ActualHeight < 2) && retries-- > 0)
            {
                await System.Threading.Tasks.Task.Delay(10);
            }
            
            if (canvas.DataContext is CameraSlot slot)
            {
                var videoService = App.Current.Services.GetRequiredService<VideoService>();

                // Prevent duplicate streams, but RE-ATTACH if the window changed
                if (slot.PipelineHandle != IntPtr.Zero)
                {
                    videoService.Reattach(slot.PipelineHandle, canvas.Handle);
                    return;
                }

                string urlToPlay = string.IsNullOrEmpty(slot.RtspUrl) ? "test" : slot.RtspUrl;
                System.Diagnostics.Debug.WriteLine($"[TS-VMS] Requesting Stream for {slot.CameraName} (URL: {urlToPlay})");

                slot.WindowHandle = canvas.Handle;
                slot.PipelineHandle = await System.Threading.Tasks.Task.Run(() =>
                    videoService.StartStream(canvas.Handle, urlToPlay, slot.Username, slot.Password, slot.HasAudioCapability));
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

        private async void StartFullScreenStream(VideoCanvas canvas)
        {
            // Wait for handle.
            int retries = 50;
            while (canvas.Handle == IntPtr.Zero && retries-- > 0)
            {
                await System.Threading.Tasks.Task.Delay(10);
            }

            if (canvas.Handle == IntPtr.Zero) return;
            if (_fullScreenPipeline != IntPtr.Zero) return; // Already playing

            // Wait for non-zero layout size without blocking the dispatcher.
            retries = 100;
            while ((canvas.ActualWidth < 2 || canvas.ActualHeight < 2) && retries-- > 0)
            {
                await System.Threading.Tasks.Task.Delay(10);
            }

            var vm = DataContext as LiveViewModel;
            if (vm == null || string.IsNullOrEmpty(vm.FullScreenUrl)) return;

            var videoService = App.Current.Services.GetRequiredService<VideoService>();
            
            System.Diagnostics.Debug.WriteLine($"[TS-VMS] Starting Full Screen Stream: {vm.FullScreenUrl}");
            _fullScreenPipeline = await System.Threading.Tasks.Task.Run(() =>
                videoService.StartStream(canvas.Handle, vm.FullScreenUrl, "", "", vm.FullScreenHasAudio));
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
        private async void UserControl_Unloaded(object sender, RoutedEventArgs e)
        {
            var app = (App)System.Windows.Application.Current;
            if (app?.Services == null) return;

            var videoService = app.Services.GetRequiredService<VideoService>();

            if (this.DataContext is LiveViewModel currentVm)
            {
                currentVm.Deactivate();
            }

            StopFullScreenStream();

            if (this.DataContext is LiveViewModel vm)
            {
                var handles = vm.CameraGrid
                    .Where(slot => slot.PipelineHandle != IntPtr.Zero)
                    .Select(slot => slot.PipelineHandle)
                    .ToList();

                foreach (var slot in vm.CameraGrid)
                {
                    slot.PipelineHandle = IntPtr.Zero;
                    slot.WindowHandle = IntPtr.Zero;
                    slot.IsConnected = false;
                }

                await System.Threading.Tasks.Task.Run(() =>
                {
                    foreach (var handle in handles)
                    {
                        try
                        {
                            videoService.StopStream(handle);
                        }
                        catch (Exception ex)
                        {
                            System.Diagnostics.Debug.WriteLine($"[TS-VMS] Background stop failed: {ex.Message}");
                        }
                    }
                });
            }
        }
    }
}
