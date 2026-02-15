using System.Windows;

namespace TSVmsDesktop.Views
{
    public partial class CreateUserWindow : Window
    {
        public string Email => TxtEmail.Text;
        public string Password => TxtPassword.Text;

        public CreateUserWindow()
        {
            InitializeComponent();
        }

        private void Create_Click(object sender, RoutedEventArgs e)
        {
            if (string.IsNullOrWhiteSpace(Email) || string.IsNullOrWhiteSpace(Password))
            {
                System.Windows.MessageBox.Show("Email and Password required.", "Validation", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            DialogResult = true;
        }
    }
}
