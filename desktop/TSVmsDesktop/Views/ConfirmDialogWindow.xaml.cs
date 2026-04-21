using System.Windows;

namespace TSVmsDesktop.Views
{
    public partial class ConfirmDialogWindow : Window
    {
        public ConfirmDialogWindow(string title, string message)
        {
            InitializeComponent();
            TitleText.Text = title;
            MessageText.Text = message;
        }

        private void Yes_Click(object sender, RoutedEventArgs e)
        {
            DialogResult = true;
            Close();
        }
    }
}
