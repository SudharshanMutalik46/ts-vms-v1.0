using System;
using System.IO;
using System.Windows;
using System.Windows.Media.Imaging;
using System.Threading.Tasks;
using System.Windows.Threading;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.Views
{
    public partial class AdoptionPreviewWindow : Window
    {
        public sealed class RetryResult
        {
            public bool Success { get; set; }
            public string Message { get; set; } = "";
            public string? SnapshotPath { get; set; }
            public string? RtspUrl { get; set; }
            public string? Username { get; set; }
            public string? Password { get; set; }
        }

        public bool OpenLiveAfterClose { get; private set; }
        private readonly Func<string, Task<RetryResult>>? _retryHandler;
        private readonly VideoService? _videoService;
        private readonly DispatcherTimer _previewStartTimer;
        private string _previewRtspUrl;
        private string _previewUsername;
        private string _previewPassword;
        private IntPtr _previewPipeline = IntPtr.Zero;

        public AdoptionPreviewWindow(string details, string rtspUrl, string? snapshotPath, string? username = null, string? password = null, Func<string, Task<RetryResult>>? retryHandler = null)
        {
            InitializeComponent();
            DetailsText.Text = details;
            RtspUrlBox.Text = NormalizeRtspUrl(rtspUrl ?? "");
            _previewRtspUrl = RtspUrlBox.Text;
            _previewUsername = username ?? "";
            _previewPassword = password ?? "";
            ParseRtspCredentials(_previewRtspUrl, out var sanitizedUrl, out var urlUser, out var urlPass);
            _previewRtspUrl = sanitizedUrl;
            if (!string.IsNullOrWhiteSpace(urlUser))
            {
                _previewUsername = urlUser;
                _previewPassword = urlPass;
            }
            _retryHandler = retryHandler;
            RetryButton.IsEnabled = _retryHandler != null;
            _videoService = App.Current?.Services?.GetService<VideoService>();
            _previewStartTimer = new DispatcherTimer { Interval = TimeSpan.FromMilliseconds(300) };
            _previewStartTimer.Tick += (_, __) =>
            {
                _previewStartTimer.Stop();
                StartLivePreview();
            };
            Loaded += (_, __) => { if (PreviewPlaceholderText.Visibility == Visibility.Visible) ScheduleLivePreview(); };
            Closed += (_, __) => StopLivePreview();
            LoadSnapshot(snapshotPath);
        }

        private void LoadSnapshot(string? snapshotPath)
        {
            try
            {
                if (string.IsNullOrWhiteSpace(snapshotPath) || !File.Exists(snapshotPath))
                {
                    ShowPreviewPlaceholder("Loading live preview...");
                    ScheduleLivePreview();
                    return;
                }

                if (!IsLikelyValidJpeg(snapshotPath))
                {
                    ShowPreviewPlaceholder("Loading live preview...");
                    ScheduleLivePreview();
                    return;
                }

                StopLivePreview();
                var img = new BitmapImage();
                img.BeginInit();
                img.CacheOption = BitmapCacheOption.OnLoad;
                img.UriSource = new Uri(snapshotPath, UriKind.Absolute);
                img.EndInit();
                PreviewImage.Source = img;
                PreviewImage.Visibility = Visibility.Visible;
                PreviewVideoCanvas.Visibility = Visibility.Collapsed;
                PreviewPlaceholderText.Visibility = Visibility.Collapsed;
            }
            catch
            {
                ShowPreviewPlaceholder("Loading live preview...");
                ScheduleLivePreview();
            }
        }

        private void Stay_Click(object sender, RoutedEventArgs e)
        {
            OpenLiveAfterClose = false;
            DialogResult = true;
            Close();
        }

        private void OpenLive_Click(object sender, RoutedEventArgs e)
        {
            OpenLiveAfterClose = true;
            DialogResult = true;
            Close();
        }

        private async void Retry_Click(object sender, RoutedEventArgs e)
        {
            if (_retryHandler == null) return;

            string newUrl = RtspUrlBox.Text?.Trim() ?? "";
            newUrl = NormalizeRtspUrl(newUrl);
            RtspUrlBox.Text = newUrl;
            if (string.IsNullOrWhiteSpace(newUrl))
            {
                RetryStatusText.Text = "RTSP URL cannot be empty.";
                return;
            }

            RetryButton.IsEnabled = false;
            RetryStatusText.Text = "Retrying with updated URL...";

            try
            {
                var result = await _retryHandler(newUrl);
                RetryStatusText.Text = result.Message;
                if (result.Success)
                {
                    if (!string.IsNullOrWhiteSpace(result.RtspUrl))
                    {
                        _previewRtspUrl = NormalizeRtspUrl(result.RtspUrl!);
                    }
                    if (!string.IsNullOrWhiteSpace(result.Username))
                    {
                        _previewUsername = result.Username!;
                        _previewPassword = result.Password ?? "";
                    }
                    LoadSnapshot(result.SnapshotPath);
                }
            }
            finally
            {
                RetryButton.IsEnabled = true;
            }
        }

        private void ShowPreviewPlaceholder(string text)
        {
            PreviewImage.Source = null;
            PreviewImage.Visibility = Visibility.Collapsed;
            PreviewPlaceholderText.Text = text;
            PreviewPlaceholderText.Visibility = Visibility.Visible;
            PreviewVideoCanvas.Visibility = Visibility.Collapsed;
        }

        private void ScheduleLivePreview()
        {
            if (string.IsNullOrWhiteSpace(_previewRtspUrl))
            {
                ShowPreviewPlaceholder("Preview not available.");
                return;
            }

            _previewStartTimer.Stop();
            _previewStartTimer.Start();
        }

        private void StartLivePreview()
        {
            try
            {
                if (_videoService == null || string.IsNullOrWhiteSpace(_previewRtspUrl))
                {
                    ShowPreviewPlaceholder("Preview not available.");
                    return;
                }

                if (PreviewVideoCanvas.Handle == IntPtr.Zero)
                {
                    ScheduleLivePreview();
                    return;
                }

                StopLivePreview();
                _previewPipeline = _videoService.StartStream(
                    PreviewVideoCanvas.Handle,
                    _previewRtspUrl,
                    _previewUsername,
                    _previewPassword,
                    false,
                    null,
                    "tcp",
                    "adoption-preview");

                if (_previewPipeline != IntPtr.Zero)
                {
                    PreviewImage.Source = null;
                    PreviewImage.Visibility = Visibility.Collapsed;
                    PreviewPlaceholderText.Visibility = Visibility.Collapsed;
                    PreviewVideoCanvas.Visibility = Visibility.Visible;
                    return;
                }
            }
            catch
            {
            }

            ShowPreviewPlaceholder("Preview not available.");
        }

        private void StopLivePreview()
        {
            if (_previewPipeline == IntPtr.Zero || _videoService == null)
                return;

            try
            {
                _videoService.StopStream(_previewPipeline);
            }
            catch
            {
            }
            finally
            {
                _previewPipeline = IntPtr.Zero;
            }
        }

        private static string NormalizeRtspUrl(string url)
        {
            if (string.IsNullOrWhiteSpace(url)) return "";
            string n = System.Net.WebUtility.HtmlDecode(url.Trim());
            if (!n.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase) &&
                !n.StartsWith("rtsps://", StringComparison.OrdinalIgnoreCase))
            {
                n = "rtsp://" + n.TrimStart('/');
            }
            return n;
        }

        private static bool IsLikelyValidJpeg(string path)
        {
            try
            {
                var bytes = File.ReadAllBytes(path);
                if (bytes.Length < 4) return false;
                // SOI marker
                if (bytes[0] != 0xFF || bytes[1] != 0xD8) return false;

                int sofIndex = -1;
                int sosIndex = -1;
                for (int i = 0; i < bytes.Length - 1; i++)
                {
                    if (bytes[i] != 0xFF) continue;
                    byte marker = bytes[i + 1];
                    if (marker == 0xDA && sosIndex < 0) sosIndex = i; // Start of scan
                    if ((marker == 0xC0 || marker == 0xC1 || marker == 0xC2 || marker == 0xC3) && sofIndex < 0) sofIndex = i; // SOF
                }
                if (sosIndex >= 0 && (sofIndex < 0 || sofIndex > sosIndex)) return false;
                return true;
            }
            catch
            {
                return false;
            }
        }

        private static void ParseRtspCredentials(string url, out string sanitizedUrl, out string username, out string password)
        {
            username = "";
            password = "";
            sanitizedUrl = NormalizeRtspUrl(url);

            try
            {
                if (!Uri.TryCreate(sanitizedUrl, UriKind.Absolute, out var uri))
                    return;

                if (string.IsNullOrWhiteSpace(uri.UserInfo))
                    return;

                var parts = uri.UserInfo.Split(':', 2);
                username = Uri.UnescapeDataString(parts[0]);
                password = parts.Length > 1 ? Uri.UnescapeDataString(parts[1]) : "";

                var builder = new UriBuilder(uri)
                {
                    UserName = "",
                    Password = ""
                };
                sanitizedUrl = builder.Uri.ToString();
            }
            catch
            {
                username = "";
                password = "";
            }
        }
    }
}
