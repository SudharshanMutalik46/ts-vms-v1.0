using System.Windows;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Views
{
    public partial class AddCameraWindow : Window
    {
        // Property to hold result
        public CameraModel? CreatedCamera { get; private set; }

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

            // 2. Create Model
            CreatedCamera = new CameraModel
            {
                Name = TxtName.Text.Trim(),
                RtspUrl = TxtUrl.Text.Trim(),
                IpAddress = string.IsNullOrWhiteSpace(TxtIp.Text) ? "Unknown IP" : TxtIp.Text.Trim(),
                Status = "Online", // Default to Online for now
                Model = "RTSP Source",
                Thumbnail = ""
            };

            // 3. Close with Success
            this.DialogResult = true;
            this.Close();
        }
    }
}
