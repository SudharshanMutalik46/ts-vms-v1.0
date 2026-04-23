using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.IO;
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
            _storagePath = NormalizeStoragePath(_configService.Settings.StoragePath);
            _enableHardwareAcceleration = _configService.Settings.EnableHardwareAcceleration;
            _enableDarkMode = _configService.Settings.EnableDarkMode;
            _logLevel = _configService.Settings.LogLevel;
        }

        [RelayCommand]
        public void Save()
        {
            StoragePath = NormalizeStoragePath(StoragePath);

            try
            {
                Directory.CreateDirectory(StoragePath);
            }
            catch (Exception ex)
            {
                System.Windows.MessageBox.Show($"Invalid storage path.\n{ex.Message}", "Settings", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

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
                StoragePath = NormalizeStoragePath(dialog.SelectedPath);
            }
        }

        private static string NormalizeStoragePath(string? rawPath)
        {
            var defaultPath = Path.Combine(
                Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),
                "TS-VMS",
                "Storage");

            var candidate = string.IsNullOrWhiteSpace(rawPath) ? defaultPath : Environment.ExpandEnvironmentVariables(rawPath.Trim());

            try
            {
                return Path.GetFullPath(candidate);
            }
            catch
            {
                return defaultPath;
            }
        }
    }
}
