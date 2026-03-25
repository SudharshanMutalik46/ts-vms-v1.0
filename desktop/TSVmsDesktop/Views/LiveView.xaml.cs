using System;
using System.Windows;
using System.Windows.Controls;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.ViewModels;
using TSVmsDesktop.Services;
using System.Linq;
using TSVmsDesktop.Controls;
using Microsoft.Web.WebView2.Wpf;
using Microsoft.Web.WebView2.Core;
using System.Text.Json;
using System.Collections.Generic;
using System.Threading.Tasks;

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

        // 3. The Actual Logic to Start the GStreamer Stream (HLS or RTSP tile)
        private async void StartVideo(VideoCanvas canvas)
        {
            // Wait for Win32 handle.
            int retries = 50;
            while (canvas.Handle == IntPtr.Zero && retries-- > 0)
                await Task.Delay(10);

            if (canvas.Handle == IntPtr.Zero) return;

            // Wait for non-zero layout size without blocking the dispatcher.
            retries = 100;
            while ((canvas.ActualWidth < 2 || canvas.ActualHeight < 2) && retries-- > 0)
                await Task.Delay(10);

            if (canvas.DataContext is CameraSlot slot)
            {
                var videoService = App.Current.Services.GetRequiredService<VideoService>();

                // Prevent duplicate streams, but RE-ATTACH if the window changed
                if (slot.PipelineHandle != IntPtr.Zero)
                {
                    videoService.Reattach(slot.PipelineHandle, canvas.Handle);
                    return;
                }

                // Pick URL based on active tier (VideoCanvas is only shown for Hls/Rtsp)
                string urlToPlay = slot.ActiveTier == StreamTier.Hls
                    ? slot.HlsUrl
                    : slot.RtspUrl;

                if (string.IsNullOrEmpty(urlToPlay)) urlToPlay = slot.RtspUrl; // ultimate fallback
                if (string.IsNullOrEmpty(urlToPlay)) return;

                System.Diagnostics.Debug.WriteLine(
                    $"[TS-VMS] StartVideo tier={slot.ActiveTier} cam={slot.CameraName} url={urlToPlay}");

                slot.WindowHandle   = canvas.Handle;
                slot.PipelineHandle = await Task.Run(() =>
                    videoService.StartStream(canvas.Handle, urlToPlay,
                                             slot.Username, slot.Password, slot.HasAudioCapability));
            }
        }

        // ── WebRTC Surface Handlers ─────────────────────────────────────────────

        private void WebRtcSurface_IsVisibleChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (sender is not WebView2 webView) return;

            if (webView.Visibility == Visibility.Visible)
            {
                if (webView.DataContext is CameraSlot slot)
                    _ = StartWebRtcStream(webView, slot);
                return;
            }

            // Hidden: only clean up when GENUINELY leaving the WebRtc tier.
            // If the slot is still on WebRtc tier (IsConnected just briefly toggled),
            // do NOT navigate to about:blank — that would kill a playing video.
            if (webView.DataContext is CameraSlot hiddenSlot &&
                hiddenSlot.ActiveTier == StreamTier.WebRtc)
            {
                // Brief toggle — leave the WebView2 page alive so video resumes when re-shown.
                return;
            }

            // Tier changed away from WebRtc (or no slot): clean up and allow fresh start.
            if (webView.DataContext is CameraSlot leavingSlot)
                leavingSlot.IsWebRtcStarted = false;

            try
            {
                if (webView.CoreWebView2 != null)
                    webView.CoreWebView2.Navigate("about:blank");
            }
            catch { }
        }

        // Shared WebView2 environment: forces software H264 decoding to avoid the
        // hardware-accelerated black-frame bug in embedded WPF WebView2 controls.
        private static Microsoft.Web.WebView2.Core.CoreWebView2Environment? _sharedWv2Env;
        private static readonly System.Threading.SemaphoreSlim _wv2EnvLock = new(1, 1);

        private static async Task<Microsoft.Web.WebView2.Core.CoreWebView2Environment> GetSharedWv2EnvAsync()
        {
            if (_sharedWv2Env != null) return _sharedWv2Env;
            await _wv2EnvLock.WaitAsync();
            try
            {
                _sharedWv2Env ??= await Microsoft.Web.WebView2.Core.CoreWebView2Environment.CreateAsync(
                    options: new Microsoft.Web.WebView2.Core.CoreWebView2EnvironmentOptions(
                        "--autoplay-policy=no-user-gesture-required"));
            }
            finally { _wv2EnvLock.Release(); }
            return _sharedWv2Env!;
        }

        private async Task StartWebRtcStream(WebView2 webView, CameraSlot slot)
        {
            // Guard: only start if still on WebRtc tier and connected
            if (slot.ActiveTier != StreamTier.WebRtc || !slot.IsConnected) return;

            // Restart-loop guard: if we already navigated for this slot, don't do it again.
            // IsWebRtcStarted is reset in WebRtcSurface_IsVisibleChanged only when the
            // slot genuinely leaves the WebRtc tier, so this prevents repeated
            // NavigateToString calls caused by brief IsConnected toggles from status polling.
            if (slot.IsWebRtcStarted) return;
            slot.IsWebRtcStarted = true; // set before any await to block concurrent calls

            System.Console.WriteLine($"[TS-VMS] WebRTC: starting for cam={slot.CameraName} sfuUrl={slot.WebRtcSfuUrl} roomId={slot.WebRtcRoomId}");

            try
            {
                var app            = (App)System.Windows.Application.Current;
                var sessionService = app.Services.GetRequiredService<ISessionService>();
                string token       = sessionService.AccessToken ?? "";

                var env = await GetSharedWv2EnvAsync();
                await webView.EnsureCoreWebView2Async(env);
                webView.DefaultBackgroundColor = System.Drawing.Color.Black;
                webView.CoreWebView2.Settings.IsWebMessageEnabled = true;

                // Detach any previously attached handler to avoid duplicate callbacks
                // Store handler reference as Tag so it can be removed on next call
                if (webView.Tag is EventHandler<Microsoft.Web.WebView2.Core.CoreWebView2WebMessageReceivedEventArgs> oldHandler)
                {
                    webView.CoreWebView2.WebMessageReceived -= oldHandler;
                }

                EventHandler<Microsoft.Web.WebView2.Core.CoreWebView2WebMessageReceivedEventArgs> msgHandler = (s, ev) =>
                {
                    // Ignore stale messages if tier has already advanced
                    if (slot.ActiveTier != StreamTier.WebRtc) return;
                    HandleWebRtcMessage(slot, ev.TryGetWebMessageAsString());
                };
                webView.Tag = msgHandler;
                webView.CoreWebView2.WebMessageReceived += msgHandler;

                string html = BuildWebRtcHtml(
                    slot.WebRtcSfuUrl,
                    slot.WebRtcRoomId,
                    slot.SessionId,
                    token,
                    slot.WebRtcTimeoutMs,
                    slot.WebRtcTrackTimeoutMs);

                webView.NavigateToString(html);
                System.Console.WriteLine($"[TS-VMS] WebRTC: HTML loaded for cam={slot.CameraName}");
            }
            catch (Exception ex)
            {
                System.Console.WriteLine($"[TS-VMS] WebRTC: WebView2 init failed for cam={slot.CameraName}: {ex.Message}");
                _ = Dispatcher.BeginInvoke(() =>
                {
                    var vm = DataContext as LiveViewModel;
                    vm?.OnWebRtcFailed(slot, ex.Message);
                });
            }
        }

        private void HandleWebRtcMessage(CameraSlot slot, string json)
        {
            try
            {
                var msg = JsonSerializer.Deserialize<Dictionary<string, string>>(json);
                if (msg == null) return;

                msg.TryGetValue("type",   out var type);
                msg.TryGetValue("reason", out var reason);

                System.Console.WriteLine($"[TS-VMS] WebRTC: message type={type} reason={reason} cam={slot.CameraName}");

                Dispatcher.BeginInvoke(() =>
                {
                    var vm = DataContext as LiveViewModel;
                    if (type == "webrtc-connected")
                    {
                        System.Console.WriteLine($"[TS-VMS] WebRTC: CONNECTED cam={slot.CameraName}");
                        slot.IsStreamFailed    = false;
                        slot.StreamErrorMessage = "";
                    }
                    else if (type == "webrtc-failed")
                    {
                        System.Console.WriteLine($"[TS-VMS] WebRTC: FAILED cam={slot.CameraName} reason={reason}");
                        slot.IsWebRtcStarted = false; // allow AdvanceToNextTier to clean up
                        vm?.OnWebRtcFailed(slot, reason ?? "unknown");
                    }
                });
            }
            catch { }
        }

        /// <summary>
        /// Injects params directly into the HTML template so no separate file load
        /// or virtual-host mapping is required.
        /// </summary>
        private static string BuildWebRtcHtml(
            string sfuUrl,
            string cameraId,
            string sessionId,
            string token,
            int timeoutMs,
            int trackTimeoutMs)
        {
            // Load template from Assets folder next to the executable
            string assetPath = System.IO.Path.Combine(
                AppDomain.CurrentDomain.BaseDirectory, "Assets", "webrtc_player.html");

            string template = System.IO.File.Exists(assetPath)
                ? System.IO.File.ReadAllText(assetPath)
                : "<!DOCTYPE html><html><body><script>window.chrome.webview.postMessage(JSON.stringify({type:'webrtc-failed',reason:'template missing'}));</script></body></html>";

            string paramsJson = JsonSerializer.Serialize(new
            {
                sfuUrl,
                cameraId,
                sessionId,
                token,
                timeoutMs,
                trackTimeoutMs
            });

            return template.Replace("%%PARAMS%%", paramsJson);
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
