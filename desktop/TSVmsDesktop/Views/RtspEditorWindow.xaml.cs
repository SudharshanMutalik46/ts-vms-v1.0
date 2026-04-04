using System;
using System.Collections.Generic;
using System.Linq;
using System.Windows;
using MessageBox = System.Windows.MessageBox;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Views
{
    public partial class RtspEditorWindow : Window
    {
        private readonly List<MediaProfile> _profiles;

        public RtspEditorWindow(
            string rtspUrl,
            int port,
            string username,
            string password,
            IEnumerable<MediaProfile> profiles,
            string selectedMainToken,
            string selectedSubToken)
        {
            InitializeComponent();

            _profiles = profiles?.ToList() ?? new List<MediaProfile>();
            CmbMainProfile.ItemsSource = _profiles;
            CmbSubProfile.ItemsSource = _profiles;

            TxtRtspUrl.Text = string.IsNullOrWhiteSpace(rtspUrl) ? "rtsp://" : rtspUrl;
            TxtPort.Text = port > 0 ? port.ToString() : "554";
            TxtUsername.Text = username ?? "";
            TxtPassword.Password = password ?? "";

            SelectProfile(CmbMainProfile, selectedMainToken);
            SelectProfile(CmbSubProfile, selectedSubToken);

            TxtRtspUrl.Focus();
            TxtRtspUrl.SelectAll();
        }

        public string RtspUrl => TxtRtspUrl.Text.Trim();
        public int Port => int.TryParse(TxtPort.Text.Trim(), out var port) ? port : 0;
        public string Username => TxtUsername.Text.Trim();
        public string Password => TxtPassword.Password.Trim();
        public MediaProfile? SelectedMainProfile => CmbMainProfile.SelectedItem as MediaProfile;
        public MediaProfile? SelectedSubProfile => CmbSubProfile.SelectedItem as MediaProfile;

        private void SelectProfile(System.Windows.Controls.ComboBox comboBox, string token)
        {
            if (string.IsNullOrWhiteSpace(token))
            {
                comboBox.SelectedIndex = -1;
                return;
            }

            var match = _profiles.FirstOrDefault(p => string.Equals(p.Token, token, StringComparison.OrdinalIgnoreCase));
            comboBox.SelectedItem = match;
        }

        private void Save_Click(object sender, RoutedEventArgs e)
        {
            if (string.IsNullOrWhiteSpace(TxtRtspUrl.Text) || !TxtRtspUrl.Text.Trim().StartsWith("rtsp://", StringComparison.OrdinalIgnoreCase))
            {
                MessageBox.Show("Please enter a valid RTSP URL.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
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
