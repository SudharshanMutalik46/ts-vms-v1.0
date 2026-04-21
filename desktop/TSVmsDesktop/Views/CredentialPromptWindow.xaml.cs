using System.Windows;
using System.Windows.Input;

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

        private void Header_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
        {
            if (e.ButtonState == MouseButtonState.Pressed)
            {
                DragMove();
            }
        }

        private void Close_Click(object sender, RoutedEventArgs e)
        {
            DialogResult = false;
            Close();
        }
    }
}
