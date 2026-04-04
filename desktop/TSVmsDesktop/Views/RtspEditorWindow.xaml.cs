using System;
using System.Windows;
using MessageBox = System.Windows.MessageBox;

namespace TSVmsDesktop.Views
{
    public partial class RtspEditorWindow : Window
    {
        public RtspEditorWindow(
            string currentRtspUrl,
            string mainRtspUrl,
            string subRtspUrl,
            int port,
            string username,
            string password)
        {
            InitializeComponent();

            TxtCurrentRtspUrl.Text = string.IsNullOrWhiteSpace(currentRtspUrl) ? "rtsp://" : currentRtspUrl;
            TxtMainRtspUrl.Text = string.IsNullOrWhiteSpace(mainRtspUrl) ? "" : mainRtspUrl;
            TxtSubRtspUrl.Text = string.IsNullOrWhiteSpace(subRtspUrl) ? "" : subRtspUrl;
            TxtPort.Text = port > 0 ? port.ToString() : "554";
            TxtUsername.Text = username ?? "";
            TxtPassword.Password = password ?? "";

            TxtMainRtspUrl.Focus();
            TxtMainRtspUrl.SelectAll();
        }

        public string CurrentRtspUrl => TxtCurrentRtspUrl.Text.Trim();
        public string MainRtspUrl => TxtMainRtspUrl.Text.Trim();
        public string SubRtspUrl => TxtSubRtspUrl.Text.Trim();
        public int Port => int.TryParse(TxtPort.Text.Trim(), out var port) ? port : 0;
        public string Username => TxtUsername.Text.Trim();
        public string Password => TxtPassword.Password.Trim();

        private void Save_Click(object sender, RoutedEventArgs e)
        {
            var current = CurrentRtspUrl;
            var main = MainRtspUrl;
            var sub = SubRtspUrl;

            if (string.IsNullOrWhiteSpace(current) && string.IsNullOrWhiteSpace(main) && string.IsNullOrWhiteSpace(sub))
            {
                MessageBox.Show("Please enter at least one RTSP URL.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            if (!string.IsNullOrWhiteSpace(current) && !current.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                MessageBox.Show("Please enter a valid current RTSP URL.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            if (!string.IsNullOrWhiteSpace(main) && !main.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                MessageBox.Show("Please enter a valid main RTSP URL.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            if (!string.IsNullOrWhiteSpace(sub) && !sub.StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                MessageBox.Show("Please enter a valid sub RTSP URL.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            if (!string.IsNullOrWhiteSpace(TxtPort.Text))
            {
                if (!int.TryParse(TxtPort.Text.Trim(), out var port) || port <= 0)
                {
                    MessageBox.Show("Please enter a valid port number.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                    return;
                }
            }

            DialogResult = true;
            Close();
        }
    }
}
