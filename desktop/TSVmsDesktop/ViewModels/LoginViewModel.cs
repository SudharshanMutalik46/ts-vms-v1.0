using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;

namespace TSVmsDesktop.ViewModels
{
    public partial class LoginViewModel : ObservableObject
    {
        private readonly MainViewModel _mainViewModel;
        
        // FIX: Initialize to avoid null warnings
        [ObservableProperty] private string _username = string.Empty;
        [ObservableProperty] private string _password = string.Empty;

        public LoginViewModel(MainViewModel mainViewModel)
        {
            _mainViewModel = mainViewModel;
        }

        [RelayCommand]
        public void Login()
        {
            _mainViewModel.IsLoggedIn = true;
            _mainViewModel.NavigateToLive();
        }
    }
}
