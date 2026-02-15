using System.Windows;
using System.Windows.Controls;
using TSVmsDesktop.ViewModels;

namespace TSVmsDesktop.Views
{
    public partial class LoginView : System.Windows.Controls.UserControl
    {
        private bool _isSyncing = false;
        public LoginView()
        {
            InitializeComponent();
            // 1. Initial Load Sync (ViewModel -> Boxes)
            this.DataContextChanged += (s, e) =>
            {
                if (this.DataContext is LoginViewModel vm)
                {
                    if (!string.IsNullOrEmpty(vm.Password))
                    {
                        if (SecretBox != null) SecretBox.Password = vm.Password;
                        if (VisibleBox != null) VisibleBox.Text = vm.Password;
                    }
                }
            };
        }

        // 2. Hidden Box Changed (User typing in dots mode)
        private void SecretBox_PasswordChanged(object sender, RoutedEventArgs e)
        {
            if (_isSyncing) return;
            
            if (this.DataContext is LoginViewModel vm)
            {
                _isSyncing = true;
                vm.Password = SecretBox.Password; // Update ViewModel
                
                if (VisibleBox != null) VisibleBox.Text = SecretBox.Password;
                _isSyncing = false;
            }
        }

        // 3. Visible Box Changed (User typing in text mode)
        private void VisibleBox_TextChanged(object sender, TextChangedEventArgs e)
        {
            if (_isSyncing) return;
            
            if (this.DataContext is LoginViewModel vm)
            {
                _isSyncing = true;
                
                // Sync to Hidden Box
                if (SecretBox != null) SecretBox.Password = VisibleBox.Text;
                _isSyncing = false;
            }
        }
    }
}
