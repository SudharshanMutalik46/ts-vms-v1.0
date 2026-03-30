using System.Windows;

namespace TSVmsDesktop.Views
{
    public partial class ManualAdoptionWindow : Window
    {
        public string Url => TxtUrl.Text.Trim();
        public string Username => TxtUsername.Text.Trim();
        public string Password => TxtPassword.Password.Trim();

        public ManualAdoptionWindow(string ipAddress)
        {
            InitializeComponent();
            TxtTitle.Text = $"Manual Configuration ({ipAddress})";
            TxtUrl.Text = $"rtsp://{ipAddress}:554/";
            TxtUrl.Focus();
            TxtUrl.SelectAll();
        }

        private void Save_Click(object sender, RoutedEventArgs e)
        {
            if (string.IsNullOrWhiteSpace(Url))
            {
                System.Windows.MessageBox.Show("Please enter an RTSP URL.", "Validation Error", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            this.DialogResult = true;
            this.Close();
        }
    }
}
