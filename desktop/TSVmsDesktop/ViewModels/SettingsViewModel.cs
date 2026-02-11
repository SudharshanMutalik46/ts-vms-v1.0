using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Windows;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class SettingsViewModel : ObservableObject
    {
        private readonly IConfigService _configService;

        [ObservableProperty] private string _storagePath;
        [ObservableProperty] private bool _enableHardwareAcceleration;
        [ObservableProperty] private bool _enableDarkMode;
        [ObservableProperty] private string _logLevel;

        public SettingsViewModel(IConfigService configService)
        {
            _configService = configService;
            
            // 1. LOAD: Pull values from the persistent service into the UI
            _storagePath = _configService.Settings.StoragePath;
            _enableHardwareAcceleration = _configService.Settings.EnableHardwareAcceleration;
            _enableDarkMode = _configService.Settings.EnableDarkMode;
            _logLevel = _configService.Settings.LogLevel;
        }

        [RelayCommand]
        public void Save()
        {
            // 2. SAVE: Push UI values back to the service
            _configService.Settings.StoragePath = StoragePath;
            _configService.Settings.EnableHardwareAcceleration = EnableHardwareAcceleration;
            _configService.Settings.EnableDarkMode = EnableDarkMode;
            _configService.Settings.LogLevel = LogLevel;

            // 3. PERSIST: Write to disk
            _configService.Save();

            System.Windows.MessageBox.Show("Configuration Saved Successfully!", "Settings", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Information);
        }

        [RelayCommand]
        public void BrowseFolder()
        {
            // Using WinForms folder dialog for simplicity, or just a placeholder
            var dialog = new System.Windows.Forms.FolderBrowserDialog();
            if (dialog.ShowDialog() == System.Windows.Forms.DialogResult.OK)
            {
                StoragePath = dialog.SelectedPath;
            }
        }
    }
}
