using System.Windows;

namespace TSVmsDesktop.Views
{
    public partial class CredentialPromptWindow : Window
    {
        public string Username => TxtUsername.Text.Trim();
        public string Password => TxtPassword.Password.Trim();

        public CredentialPromptWindow(string ipAddress)
        {
            InitializeComponent();
            TxtTitle.Text = $"Authenticate {ipAddress}";
            TxtPassword.Focus();
        }

        private void Adopt_Click(object sender, RoutedEventArgs e)
        {
            this.DialogResult = true;
            this.Close();
        }
    }
}
