using System.Windows;

namespace TSVmsDesktop.Views
{
    public partial class InfoDialogWindow : Window
    {
        public InfoDialogWindow(string title, string message)
        {
            InitializeComponent();
            Title = title;
            TitleText.Text = title;
            MessageText.Text = message;
        }

        private void Ok_Click(object sender, RoutedEventArgs e)
        {
            DialogResult = true;
            Close();
        }
    }
}

