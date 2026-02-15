using System.Windows;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Views
{
    public partial class AddCameraWindow : Window
    {
        // Property to hold result
        public CameraModel? CreatedCamera { get; private set; }
        public string CameraUsername { get; private set; } = "";
        public string CameraPassword { get; private set; } = "";

        public AddCameraWindow()
        {
            InitializeComponent();
            TxtName.Focus(); // Auto-focus the name field
        }

        private void Save_Click(object sender, RoutedEventArgs e)
        {
            // 1. Validate Input
            if (string.IsNullOrWhiteSpace(TxtName.Text))
            {
                System.Windows.MessageBox.Show("Please enter a Camera Name.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            if (string.IsNullOrWhiteSpace(TxtUrl.Text) || TxtUrl.Text.Length < 7)
            {
                System.Windows.MessageBox.Show("Please enter a valid RTSP URL.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            // 2. Capture credentials
            CameraUsername = TxtUsername.Text.Trim();
            CameraPassword = TxtPassword.Password.Trim();

            // 3. Create Model
            CreatedCamera = new CameraModel
            {
                Name = TxtName.Text.Trim(),
                RtspUrl = TxtUrl.Text.Trim(),
                IpAddress = string.IsNullOrWhiteSpace(TxtIp.Text) ? "Unknown IP" : TxtIp.Text.Trim(),
                Status = "Online",
                Model = "RTSP Source",
                Thumbnail = "",
                SiteId = "00000000-0000-0000-0000-000000000001", // Default Site ID
                IsEnabled = true
            };

            // Auto-detect Port and IP from URL if possible
            try
            {
                if (System.Uri.TryCreate(CreatedCamera.RtspUrl, System.UriKind.Absolute, out var uri))
                {
                    if (uri.Port > 0) CreatedCamera.Port = uri.Port;
                    if (CreatedCamera.IpAddress == "Unknown IP" && !string.IsNullOrWhiteSpace(uri.Host))
                    {
                        CreatedCamera.IpAddress = uri.Host;
                    }
                }
            }
            catch { /* Best effort parsing */ }

            // 4. Close with Success
            this.DialogResult = true;
            this.Close();
        }
    }
}
