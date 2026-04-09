using System;
using System.Windows.Threading;
using System.Windows;
using System.Windows.Controls;
using TSVmsDesktop.ViewModels;
using TSVmsDesktop.Services;
using System.Linq;
using TSVmsDesktop.Controls;
using Microsoft.Web.WebView2.Wpf;
using Microsoft.Web.WebView2.Core;
using System.Text.Json;
using System.Collections.Generic;
using System.Threading.Tasks;
using System.Windows.Media.Animation;
using System.Windows.Controls.Primitives;
using System.Windows.Media;
using System.Windows.Input;
using Microsoft.Extensions.DependencyInjection;
using WpfButton = System.Windows.Controls.Button;
using WpfGrid = System.Windows.Controls.Grid;
using WpfTextBlock = System.Windows.Controls.TextBlock;
using WpfThickness = System.Windows.Thickness;
using WpfBrushes = System.Windows.Media.Brushes;
using WpfColor = System.Windows.Media.Color;
using WpfFontWeights = System.Windows.FontWeights;
using WpfHorizontalAlignment = System.Windows.HorizontalAlignment;
using WpfVerticalAlignment = System.Windows.VerticalAlignment;
using WpfTextTrimming = System.Windows.TextTrimming;
using WpfPopupAnimation = System.Windows.Controls.Primitives.PopupAnimation;
using WpfPlacementMode = System.Windows.Controls.Primitives.PlacementMode;

namespace TSVmsDesktop.Views
{
    public partial class LiveView : System.Windows.Controls.UserControl
    {
        private IntPtr _fullScreenPipeline = IntPtr.Zero;
        private bool _isFullScreenStarting = false;
        private Popup? _fullScreenOverlayPopup;
        private WpfGrid? _fullScreenOverlayRoot;
        private DispatcherTimer _hoverTimer;
        private bool _isTileOverlayHovered;
        private bool _isMoreOptionsMenuOpen;

        public LiveView()
        {
            InitializeComponent();
            CreateFullScreenOverlayPopup();
            this.DataContextChanged += OnDataContextChanged;

            _hoverTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(150) };
            _hoverTimer.Tick += HoverTimer_Tick;
        }

        private void HoverTimer_Tick(object? sender, EventArgs e)
        {
            _hoverTimer.Stop();
            if (_isTileOverlayHovered)
                return;

            UpdateHoverFromPointer();
        }

        private void CreateFullScreenOverlayPopup()
        {
            if (_fullScreenOverlayPopup != null) return;

            var closeButton = new WpfButton
            {
                Width = 44,
                Height = 44,
                Cursor = System.Windows.Input.Cursors.Hand,
                ToolTip = "Exit Full Screen (Esc)",
                Background = WpfBrushes.Transparent,
                BorderThickness = new WpfThickness(0, 0, 0, 0),
                Content = new WpfGrid()
            };

            var closeGlyph = new WpfTextBlock
            {
                Text = "X",
                FontSize = 20,
                FontWeight = WpfFontWeights.Bold,
                Foreground = WpfBrushes.White,
                HorizontalAlignment = WpfHorizontalAlignment.Center,
                VerticalAlignment = WpfVerticalAlignment.Center,
                Margin = new WpfThickness(0, -1, 0, 0)
            };
            var closeHalo = new System.Windows.Shapes.Ellipse
            {
                Width = 44,
                Height = 44,
                Fill = new SolidColorBrush(WpfColor.FromArgb(180, 17, 24, 39)),
                Stroke = new SolidColorBrush(WpfColor.FromArgb(140, 51, 65, 85)),
                StrokeThickness = 1
            };
            ((WpfGrid)closeButton.Content).Children.Add(closeHalo);
            ((WpfGrid)closeButton.Content).Children.Add(closeGlyph);
            closeButton.Click += (_, __) =>
            {
                if (DataContext is LiveViewModel vm)
                {
                    vm.ExitFullScreenCommand.Execute(null);
                }
            };

            _fullScreenOverlayRoot = new WpfGrid
            {
                Background = WpfBrushes.Transparent
            };
            _fullScreenOverlayRoot.Children.Add(closeButton);
            closeButton.HorizontalAlignment = WpfHorizontalAlignment.Right;
            closeButton.VerticalAlignment = WpfVerticalAlignment.Top;
            closeButton.Margin = new WpfThickness(0, 16, 16, 0);

            _fullScreenOverlayPopup = new Popup
            {
                Placement = WpfPlacementMode.Relative,
                AllowsTransparency = true,
                StaysOpen = true,
                PopupAnimation = WpfPopupAnimation.Fade,
                PlacementTarget = FullScreenGrid,
                Child = _fullScreenOverlayRoot
            };
        }


        private CustomPopupPlacement[] TileActionPopup_Placement(System.Windows.Size popupSize, System.Windows.Size targetSize, System.Windows.Point offset)
        {
            // Center horizontally: (TargetWidth - PopupWidth) / 2
            // Position at bottom with 15px margin: TargetHeight - PopupHeight - 15
            var x = (targetSize.Width - popupSize.Width) / 2;
            var y = targetSize.Height - popupSize.Height - 15;

            return new[]
            {
                new CustomPopupPlacement(new System.Windows.Point(x, y), PopupPrimaryAxis.None)
            };
        }

        private async void UserControl_Loaded(object sender, RoutedEventArgs e)
        {
            if (this.DataContext is LiveViewModel vm && !vm.IsActive)
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

            if (_fullScreenOverlayRoot != null)
            {
                _fullScreenOverlayRoot.DataContext = DataContext;
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
                    _ = HideFullScreenShellAsync();
                }
                else
                {
                    ShowFullScreenShellAsync();
                }
            }
            // If the URL changes while already in Full Screen (e.g., from Double Click or another selection method)
            if (e.PropertyName == "FullScreenUrl")
            {
                var vm = (LiveViewModel)DataContext;
                if (vm.IsFullScreen && FullScreenPlayer.IsLoaded && FullScreenPlayer.Visibility == Visibility.Visible)
                {
                    SetFullScreenLoading(true);
                    StopFullScreenStream();
                    StartFullScreenStream(FullScreenPlayer);
                }
            }
        }

        private void SetFullScreenLoading(bool isVisible)
        {
            if (FullScreenLoadingOverlay == null)
                return;

            FullScreenLoadingOverlay.Visibility = isVisible ? Visibility.Visible : Visibility.Collapsed;
            FullScreenLoadingOverlay.Opacity = isVisible ? 1 : 0;
        }

        private void SetDashboardVisible(bool isVisible)
        {
            if (DashboardGrid == null)
                return;

            DashboardGrid.Visibility = isVisible ? Visibility.Visible : Visibility.Collapsed;
        }

        private void ShowFullScreenShellAsync()
        {
            if (FullScreenGrid == null) return;

            SetDashboardVisible(true);
            DashboardGrid.Opacity = 1;
            FullScreenGrid.Visibility = Visibility.Visible;
            FullScreenGrid.Opacity = 0;
            SetFullScreenLoading(true);

            if (FullScreenCurtain != null)
            {
                FullScreenCurtain.BeginAnimation(OpacityProperty, null);
                FullScreenCurtain.Opacity = 0;
            }

            if (_fullScreenOverlayRoot != null)
            {
                _fullScreenOverlayRoot.DataContext = DataContext;
            }

            if (_fullScreenOverlayPopup != null)
            {
                _fullScreenOverlayPopup.IsOpen = true;
            }
        }

        private async Task HideFullScreenShellAsync()
        {
            if (FullScreenGrid == null) return;

            SetDashboardVisible(true);
            DashboardGrid.Opacity = 0;
            SetFullScreenLoading(false);

            if (FullScreenCurtain != null)
            {
                FullScreenCurtain.BeginAnimation(OpacityProperty, new DoubleAnimation(0, 1, TimeSpan.FromMilliseconds(220))
                {
                    EasingFunction = new CubicEase { EasingMode = EasingMode.EaseIn }
                });
            }

            var dashboardFade = new DoubleAnimation(1, TimeSpan.FromMilliseconds(200))
            {
                EasingFunction = new CubicEase { EasingMode = EasingMode.EaseOut }
            };
            DashboardGrid.BeginAnimation(OpacityProperty, dashboardFade);

            await Task.Delay(220);

            if (_fullScreenOverlayPopup != null)
            {
                _fullScreenOverlayPopup.IsOpen = false;
            }

            StopFullScreenStream();
            FullScreenGrid.Visibility = Visibility.Collapsed;
            FullScreenGrid.Opacity = 1;
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

        private void TileRoot_MouseEnter(object sender, System.Windows.Input.MouseEventArgs e)
        {
            UpdateHoverFromPointer();
        }

        private void TileRoot_MouseLeave(object sender, System.Windows.Input.MouseEventArgs e)
        {
            UpdateHoverFromPointer();
        }

        private void TileRoot_MouseMove(object sender, System.Windows.Input.MouseEventArgs e)
        {
            UpdateHoverFromPointer();
        }

        private void TileOverlay_MouseEnter(object sender, System.Windows.Input.MouseEventArgs e)
        {
            _isTileOverlayHovered = true;
            _hoverTimer.Stop();
            UpdateHoverFromPointer();
        }

        private void TileOverlay_MouseLeave(object sender, System.Windows.Input.MouseEventArgs e)
        {
            _isTileOverlayHovered = false;
            _hoverTimer.Start();
        }

        private void MoreOptionsButton_Click(object sender, RoutedEventArgs e)
        {
            if (sender is not WpfButton button || button.ContextMenu == null)
                return;

            button.ContextMenu.PlacementTarget = button;
            button.ContextMenu.DataContext = button.DataContext;
            button.ContextMenu.IsOpen = true;
        }

        private void MoreOptionsMenu_Opened(object sender, RoutedEventArgs e)
        {
            _isMoreOptionsMenuOpen = true;
            _hoverTimer.Stop();
        }

        private void MoreOptionsMenu_Closed(object sender, RoutedEventArgs e)
        {
            _isMoreOptionsMenuOpen = false;
            UpdateHoverFromPointer();
        }

        private void VideoSurface_NativeMouseEnter(object sender, EventArgs e)
        {
            if (sender is FrameworkElement element)
            {
                _isTileOverlayHovered = false;
                HandleHoverElement(element);
            }
        }

        private void VideoSurface_NativeMouseMove(object sender, EventArgs e)
        {
            if (sender is FrameworkElement element)
            {
                _isTileOverlayHovered = false;
                HandleHoverElement(element);
            }
        }

        private void VideoSurface_NativeMouseLeave(object sender, EventArgs e)
        {
            _hoverTimer.Start();
        }

        private void LiveViewRoot_PreviewMouseMove(object sender, System.Windows.Input.MouseEventArgs e)
        {
            UpdateHoverFromPointer();
        }

        private void LiveViewRoot_MouseLeave(object sender, System.Windows.Input.MouseEventArgs e)
        {
            _hoverTimer.Start();
        }

        private void UpdateHoverFromPointer()
        {
            if (LiveViewRoot == null)
                return;

            if (_isTileOverlayHovered || _isMoreOptionsMenuOpen)
                return;

            var hit = LiveViewRoot.InputHitTest(Mouse.GetPosition(LiveViewRoot)) as DependencyObject;
            var slot = FindCameraSlot(hit);

            if (slot != null && DataContext is LiveViewModel vm)
            {
                FrameworkElement? target = FindPopupTarget(hit);
                SetHoverState(vm, slot, target ?? LiveViewRoot);
                return;
            }

            ClearHoverState();
            CloseTileOverlay();
        }

        private void HandleHoverElement(FrameworkElement element)
        {
            _hoverTimer.Stop();

            if (element.DataContext is not CameraSlot slot || DataContext is not LiveViewModel vm)
                return;

            SetHoverState(vm, slot, element);
        }

        private void SetHoverState(LiveViewModel vm, CameraSlot slot, FrameworkElement placementTarget)
        {
            if (vm.ActiveHoverSlot != null && vm.ActiveHoverSlot != slot)
            {
                vm.ActiveHoverSlot.IsHovered = false;
            }

            vm.ActiveHoverSlot = slot;
            slot.IsHovered = true;
            OpenTileOverlay(placementTarget, slot);
        }

        private void ClearHoverState()
        {
            if (DataContext is LiveViewModel vm && vm.ActiveHoverSlot != null)
            {
                vm.ActiveHoverSlot.IsHovered = false;
                vm.ActiveHoverSlot = null;
            }
        }

        private void OpenTileOverlay(FrameworkElement placementTarget, CameraSlot slot)
        {
            if (TileOverlayPopup == null) return;

            TileOverlayPopup.IsOpen = false;
            TileOverlayPopup.DataContext = slot;
            TileOverlayPopup.PlacementTarget = placementTarget;
            TileOverlayPopup.IsOpen = true;
        }

        private void CloseTileOverlay()
        {
            if (TileOverlayPopup != null)
            {
                TileOverlayPopup.IsOpen = false;
            }
        }

        private CameraSlot? GetMenuSlot(object? sender)
        {
            if (sender is not FrameworkElement element)
                return null;

            if (element.DataContext is CameraSlot slot)
                return slot;

            if (element.Parent is MenuItem parentItem && parentItem.DataContext is CameraSlot parentSlot)
                return parentSlot;

            if (element.Parent is ContextMenu menu && menu.DataContext is CameraSlot menuSlot)
                return menuSlot;

            if (element.Parent is ContextMenu menu2 &&
                menu2.PlacementTarget is FrameworkElement target &&
                target.DataContext is CameraSlot targetSlot)
                return targetSlot;

            return null;
        }

        private MainViewModel? GetMainViewModel() => App.Current.Services.GetRequiredService<MainViewModel>();

        private PlaybackViewModel? GetPlaybackViewModel()
        {
            var mainVm = GetMainViewModel();
            return mainVm?.CurrentView as PlaybackViewModel;
        }

        private LiveViewModel? GetLiveViewModel() => DataContext as LiveViewModel;

        private void SnapshotButton_Click(object sender, RoutedEventArgs e)
        {
            var vm = GetLiveViewModel();
            var slot = (sender as FrameworkElement)?.DataContext as CameraSlot;
            if (vm == null || slot == null) return;
            vm.SnapshotCommand.Execute(slot.CameraName);
        }

        private async void FullScreenButton_Click(object sender, RoutedEventArgs e)
        {
            var vm = GetLiveViewModel();
            var slot = (sender as FrameworkElement)?.DataContext as CameraSlot;
            if (vm == null || slot == null) return;
            await vm.EnterFullScreen(slot);
        }

        private async void ReconnectButton_Click(object sender, RoutedEventArgs e)
        {
            var vm = GetLiveViewModel();
            var slot = (sender as FrameworkElement)?.DataContext as CameraSlot;
            if (vm == null || slot == null) return;
            await vm.ReconnectStream(slot);
        }

        private void CameraDetailsMenuItem_Click(object sender, RoutedEventArgs e)
        {
            var slot = GetMenuSlot(sender);
            var mainVm = GetMainViewModel();
            if (slot == null || mainVm == null) return;

            mainVm.NavigateToCameraDetailsCommand.Execute(slot.Id);
        }

        private void OpenPlaybackMenuItem_Click(object sender, RoutedEventArgs e)
        {
            var slot = GetMenuSlot(sender);
            var mainVm = GetMainViewModel();
            if (slot == null || mainVm == null) return;

            mainVm.NavigateToPlaybackCommand.Execute(null);

            var playbackVm = mainVm.CurrentView as PlaybackViewModel;
            if (playbackVm == null) return;

            var camService = App.Current.Services.GetRequiredService<CameraService>();
            var cam = camService.AllCameras.FirstOrDefault(c => c.Id == slot.Id);
            if (cam != null)
            {
                playbackVm.SelectedCamera = cam;
            }
        }

        private void CopyRtspMenuItem_Click(object sender, RoutedEventArgs e)
        {
            var slot = GetMenuSlot(sender);
            if (slot == null || string.IsNullOrWhiteSpace(slot.RtspUrl))
                return;

            System.Windows.Clipboard.SetText(slot.RtspUrl);
        }

        private void StreamInfoMenuItem_Click(object sender, RoutedEventArgs e)
        {
            var slot = GetMenuSlot(sender);
            if (slot == null) return;

            string info =
                $"Camera: {slot.CameraName}\n" +
                $"ID: {slot.Id}\n" +
                $"RTSP URL: {slot.RtspUrl}\n" +
                $"Main URL: {slot.MainRtspUrl}\n" +
                $"Transport: {slot.RtspTransport}\n" +
                $"Codec: {slot.PreferredCodec}\n" +
                $"Audio: {(slot.HasAudioCapability ? "Yes" : "No")}";

            System.Windows.MessageBox.Show(info, "Stream Info", MessageBoxButton.OK, MessageBoxImage.Information);
        }

        private static CameraSlot? FindCameraSlot(DependencyObject? current)
        {
            while (current != null)
            {
                if (current is FrameworkElement element && element.DataContext is CameraSlot slot)
                {
                    return slot;
                }

                current = GetParent(current);
            }

            return null;
        }

        private static FrameworkElement? FindPopupTarget(DependencyObject? current)
        {
            while (current != null)
            {
                if (current is FrameworkElement element && element.DataContext is CameraSlot)
                {
                    return element;
                }

                current = GetParent(current);
            }

            return null;
        }

        private static DependencyObject? GetParent(DependencyObject current)
        {
            if (current is System.Windows.Media.Visual or System.Windows.Media.Media3D.Visual3D)
            {
                return VisualTreeHelper.GetParent(current);
            }

            if (current is FrameworkElement fe)
            {
                return fe.Parent ?? fe.TemplatedParent;
            }

            return null;
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
                // Guard: never stop a pipeline that was started less than 5 seconds ago.
                // Rapid hide/show cycles during grid re-layouts would otherwise destroy a
                // pipeline that hasn't had time to produce any frames.
                if ((DateTime.UtcNow - slot.PipelineStartedAt).TotalSeconds < 5)
                {
                    System.Diagnostics.Debug.WriteLine(
                        $"[TS-VMS] Skipped premature stop for {slot.CameraName} (pipeline age < 5s)");
                    return;
                }

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
                        VideoService.Log($"[TS-VMS] Hidden stop failed: {ex.Message}");
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

        // 3. The Actual Logic to Start the GStreamer Stream (RTSP tile)
        private async void StartVideo(VideoCanvas canvas)
        {
            if (canvas.DataContext is CameraSlot slot)
            {
                // CRITICAL: Block duplicate starts immediately before any async wait.
                // This prevents race conditions where multiple events (like grid layout changes)
                // trigger redundant pipelines.
                if (slot.IsPipelineStarting || slot.PipelineHandle != IntPtr.Zero)
                {
                    VideoService.Log($"[TS-VMS] Ignored redundant StartVideo request for {slot.CameraName}");
                    return;
                }

                try
                {
                    slot.IsPipelineStarting = true;

                    // Give the WPF surface a moment to finish settling before the
                    // first RTSP start. Manual reconnect already benefits from a
                    // similar pause; this makes the initial start follow the same
                    // path instead of racing the window creation.
                    await Task.Delay(350);

                    // Wait for Win32 handle.
                    int retries = 200;
                    while (canvas.Handle == IntPtr.Zero && retries-- > 0)
                        await Task.Delay(10);

                    if (canvas.Handle == IntPtr.Zero) return;

                    // Wait for non-zero layout size without blocking the dispatcher.
                    retries = 200;
                    while ((canvas.ActualWidth < 2 || canvas.ActualHeight < 2) && retries-- > 0)
                        await Task.Delay(10);

                    var videoService = App.Current.Services.GetRequiredService<VideoService>();

                    // RE-ATTACH if the window changed but the pipeline is already alive
                    if (slot.PipelineHandle != IntPtr.Zero)
                    {
                        if (slot.WindowHandle != canvas.Handle)
                        {
                            VideoService.Log(
                                $"[TS-VMS] Reattach requested old={slot.WindowHandle} new={canvas.Handle}");

                            videoService.Reattach(slot.PipelineHandle, canvas.Handle);
                            slot.WindowHandle = canvas.Handle;
                        }

                        return;
                    }

                    // Start on the current RTSP URL. The view model keeps this on the
                    // sub-stream first and switches to main only if the sub-stream fails.
                    string urlToPlay = slot.RtspUrl;
                    if (string.IsNullOrWhiteSpace(urlToPlay)) return;

                    VideoService.Log(
                        $"[TS-VMS] StartVideo tier={slot.ActiveTier} cam={slot.CameraName} url={urlToPlay}");

                    slot.WindowHandle = canvas.Handle;

                    Func<Task<(string Url, IntPtr Handle)>> getFreshContext = async () =>
                    {
                        // getFreshContext is called from RestartStreamAsync on a background thread.
                        // Dispatcher.InvokeAsync(async lambda) loses the dispatcher SynchronizationContext
                        // after the first await inside FetchCredentialsForSlot, causing "The calling thread
                        // cannot access this object" on ObservableCollection access.
                        // Fix: use BeginInvoke with async-void + TaskCompletionSource so the full
                        // async chain (including all awaits) runs on the dispatcher thread.
                        var tcs = new TaskCompletionSource<(string Url, IntPtr Handle)>();
                        _ = System.Windows.Application.Current.Dispatcher.BeginInvoke(new Action(async () =>
                        {
                            try
                            {
                                if (this.DataContext is LiveViewModel vm)
                                    await vm.FetchCredentialsForSlot(slot);
                                string url = slot.RtspUrl;
                                tcs.TrySetResult((url, canvas.Handle));
                            }
                            catch (Exception ex)
                            {
                                tcs.TrySetException(ex);
                            }
                        }));
                        return await tcs.Task;
                    };

                    slot.PipelineHandle = await Task.Run(() =>
                        videoService.StartStream(canvas.Handle, urlToPlay,
                                                 slot.Username, slot.Password, slot.HasAudioCapability, getFreshContext, slot.RtspTransport, slot.CameraName));

                    if (slot.PipelineHandle != IntPtr.Zero)
                    {
                        slot.PipelineStartedAt = DateTime.UtcNow;
                        if (slot.IsReconnectInProgress && DataContext is LiveViewModel vm)
                        {
                            _ = vm.CompleteReconnectAsync(slot, $"restarted on {slot.ActiveTier}");
                        }
                    }
                }
                finally
                {
                    slot.IsPipelineStarting = false;
                }
            }
        }

        // ── WebRTC Surface Handlers ─────────────────────────────────────────────

        private void WebRtcSurface_IsVisibleChanged(object sender, DependencyPropertyChangedEventArgs e)
        {
            if (!LiveViewModel.WebRtcEnabled)
                return;

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

        // Shared WebView2 environment: keep the video pipeline as close to the
        // browser defaults as possible. The stream is already isolated in a
        // dedicated WebView, so we avoid forcing software decode flags here.
        private static Microsoft.Web.WebView2.Core.CoreWebView2Environment? _sharedWv2Env;
        private static readonly System.Threading.SemaphoreSlim _wv2EnvLock = new(1, 1);
        private const string WebView2LiveVideoArgs =
            "--autoplay-policy=no-user-gesture-required";

        private static async Task<Microsoft.Web.WebView2.Core.CoreWebView2Environment> GetSharedWv2EnvAsync()
        {
            if (_sharedWv2Env != null) return _sharedWv2Env;
            await _wv2EnvLock.WaitAsync();
            try
            {
                _sharedWv2Env ??= await Microsoft.Web.WebView2.Core.CoreWebView2Environment.CreateAsync(
                    options: new Microsoft.Web.WebView2.Core.CoreWebView2EnvironmentOptions(
                        WebView2LiveVideoArgs));
            }
            finally { _wv2EnvLock.Release(); }
            return _sharedWv2Env!;
        }

        private async Task StartWebRtcStream(WebView2 webView, CameraSlot slot)
        {
            if (!LiveViewModel.WebRtcEnabled)
                return;

            // Guard: only start if still on WebRtc tier and connected
            if (slot.ActiveTier != StreamTier.WebRtc || !slot.IsConnected) return;

            // Restart-loop guard: if we already navigated for this slot, don't do it again.
            // IsWebRtcStarted is reset in WebRtcSurface_IsVisibleChanged only when the
            // slot genuinely leaves the WebRtc tier, so this prevents repeated
            // NavigateToString calls caused by brief IsConnected toggles from status polling.
            if (slot.IsWebRtcStarted) return;
            slot.IsWebRtcStarted = true; // set before any await to block concurrent calls

            VideoService.Log($"[TS-VMS] WebRTC: starting for cam={slot.CameraName} sfuUrl={slot.WebRtcSfuUrl} roomId={slot.WebRtcRoomId}");

            try
            {
                var app            = (App)System.Windows.Application.Current;
                var sessionService = app.Services.GetRequiredService<ISessionService>();
                var settings       = app.Services.GetRequiredService<SettingsService>();
                string token       = sessionService.AccessToken ?? "";
                string webRtcApiUrl = ResolveWebRtcApiUrl(slot.WebRtcSfuUrl, settings.CurrentSettings.BaseUrl);

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
                    webRtcApiUrl,
                    slot.WebRtcRoomId,
                    slot.SessionId,
                    token,
                    slot.WebRtcCodecPreference,
                    slot.WebRtcTimeoutMs,
                    slot.WebRtcTrackTimeoutMs);

                webView.NavigateToString(html);
                VideoService.Log($"[TS-VMS] WebRTC: HTML loaded for cam={slot.CameraName}");
            }
            catch (Exception ex)
            {
                VideoService.Log($"[TS-VMS] WebRTC: WebView2 init failed for cam={slot.CameraName}: {ex.Message}");
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
                using var doc = JsonDocument.Parse(json);
                var root = doc.RootElement;

                string? type = root.TryGetProperty("type", out var t) ? t.GetString() : null;
                string? reason = root.TryGetProperty("reason", out var r) ? r.GetString() : null;
                string? debugMessage = root.TryGetProperty("message", out var m) ? m.GetString() : null;
                string extra = root.TryGetProperty("extra", out var e) ? e.ToString() : "";

                VideoService.Log($"[TS-VMS] WebRTC: message type={type} reason={reason} message={debugMessage} extra={extra} cam={slot.CameraName}");

                Dispatcher.BeginInvoke(() =>
                {
                    var vm = DataContext as LiveViewModel;
                    if (type == "webrtc-connected")
                    {
                        VideoService.Log($"[TS-VMS] WebRTC: CONNECTED cam={slot.CameraName}");
                        slot.IsStreamFailed    = false;
                        slot.StreamErrorMessage = "";
                        if (vm != null)
                        {
                            _ = vm.CompleteReconnectAsync(slot, "WebRTC connected");
                        }
                    }
                    else if (type == "webrtc-failed")
                    {
                        VideoService.Log($"[TS-VMS] WebRTC: FAILED cam={slot.CameraName} reason={reason}");
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
            string preferredCodec,
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
                preferredCodec,
                timeoutMs,
                trackTimeoutMs
            });

            return template.Replace("%%PARAMS%%", paramsJson);
        }

        private static string ResolveWebRtcApiUrl(string advertisedUrl, string? configuredBaseUrl)
        {
            static string NormalizeApiBase(string value)
            {
                var trimmed = value.Trim().TrimEnd('/');
                return trimmed.EndsWith("/api/v1/sfu", StringComparison.OrdinalIgnoreCase)
                    ? trimmed
                    : trimmed + "/api/v1/sfu";
            }

            if (!string.IsNullOrWhiteSpace(configuredBaseUrl))
            {
                return NormalizeApiBase(configuredBaseUrl);
            }

            if (!string.IsNullOrWhiteSpace(advertisedUrl))
            {
                return NormalizeApiBase(advertisedUrl);
            }

            return "/api/v1/sfu";
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
            if (_isFullScreenStarting || _fullScreenPipeline != IntPtr.Zero)
            {
                VideoService.Log("[TS-VMS] Ignored duplicate StartFullScreenStream request.");
                return;
            }

            VideoService? videoService = null;
            Action<IntPtr>? readyHandler = null;

            try
            {
                _isFullScreenStarting = true;
                SetFullScreenLoading(true);

                // Wait briefly for the HWND to exist.
                int retries = 20;
                while (canvas.Handle == IntPtr.Zero && retries-- > 0)
                {
                    await System.Threading.Tasks.Task.Delay(10);
                }

                if (canvas.Handle == IntPtr.Zero) return;

                // The control already has a minimum size; do not wait for a full layout pass.

                var vm = DataContext as LiveViewModel;
                if (vm == null || string.IsNullOrEmpty(vm.FullScreenUrl)) return;

                videoService = App.Current.Services.GetRequiredService<VideoService>();
                var targetHandle = canvas.Handle;
                vm.FullScreenWindowHandle = targetHandle;
                var readyTcs = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
                readyHandler = handle =>
                {
                    if (handle == targetHandle)
                    {
                        readyTcs.TrySetResult(true);
                    }
                };
                videoService.StreamReady += readyHandler;
                
                VideoService.Log($"[TS-VMS] Starting Full Screen Stream: {vm.FullScreenUrl}");

                var activeSlot = vm.CameraGrid.FirstOrDefault(s => s.CameraName == vm.SelectedCameraName);
                Func<Task<(string Url, IntPtr Handle)>> getFreshContext = async () =>
                {
                    var tcs = new TaskCompletionSource<(string Url, IntPtr Handle)>();
                    _ = System.Windows.Application.Current.Dispatcher.BeginInvoke(new Action(async () =>
                    {
                        try
                        {
                            if (activeSlot != null)
                            {
                                await vm.FetchCredentialsForSlot(activeSlot);
                                string url = string.IsNullOrWhiteSpace(vm.FullScreenUrl)
                                    ? (string.IsNullOrWhiteSpace(activeSlot.MainRtspUrl) ? activeSlot.RtspUrl : activeSlot.MainRtspUrl)
                                    : vm.FullScreenUrl;
                                tcs.TrySetResult((url, canvas.Handle));
                            }
                            else
                            {
                                tcs.TrySetResult((vm.FullScreenUrl, canvas.Handle));
                            }
                        }
                        catch (Exception ex)
                        {
                            tcs.TrySetException(ex);
                        }
                    }));
                    return await tcs.Task;
                };

                _fullScreenPipeline = await System.Threading.Tasks.Task.Run(() =>
                    videoService.StartStream(canvas.Handle, vm.FullScreenUrl, "", "", vm.FullScreenHasAudio, getFreshContext, activeSlot?.RtspTransport ?? "tcp", vm.SelectedCameraName));

                var completed = await Task.WhenAny(readyTcs.Task, Task.Delay(3500));
                await Dispatcher.InvokeAsync(() =>
                {
                    SetFullScreenLoading(false);
                    if (completed == readyTcs.Task)
                    {
                        SetDashboardVisible(false);
                        DashboardGrid.Opacity = 0;

                        var fadeInFullScreen = new DoubleAnimation(1, TimeSpan.FromMilliseconds(180))
                        {
                            EasingFunction = new CubicEase { EasingMode = EasingMode.EaseOut }
                        };
                        FullScreenGrid.BeginAnimation(OpacityProperty, fadeInFullScreen);
                        if (FullScreenGrid != null)
                        {
                            FullScreenGrid.Visibility = Visibility.Visible;
                            FullScreenGrid.Opacity = 1;
                        }
                    }
                    else
                    {
                        // Keep the dashboard visible rather than showing a black
                        // fullscreen shell when preroll is slow or the ready
                        // signal does not arrive in time.
                        SetDashboardVisible(true);
                        DashboardGrid.Opacity = 1;
                        FullScreenGrid.Opacity = 0;
                    }
                });
            }
            finally
            {
                if (videoService != null && readyHandler != null)
                {
                    videoService.StreamReady -= readyHandler;
                }
                _isFullScreenStarting = false;
                SetFullScreenLoading(false);
            }
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
                if (DataContext is LiveViewModel vm)
                {
                    vm.FullScreenWindowHandle = IntPtr.Zero;
                }
                
                VideoService.Log("[TS-VMS] Full Screen Stream Stopped.");
            }
        }


        // 5. Cleanup when leaving the view
        private async void UserControl_Unloaded(object sender, RoutedEventArgs e)
        {
            _hoverTimer.Stop();
            ClearHoverState();

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
                            VideoService.Log($"[TS-VMS] Background stop failed: {ex.Message}");
                        }
                    }
                });
            }
        }
    }
}
