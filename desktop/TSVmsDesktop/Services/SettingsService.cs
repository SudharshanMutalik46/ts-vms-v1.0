using System;
using System.IO;
using System.Text.Json;

namespace TSVmsDesktop.Services
{
    public class AppSettings
    {
        public string StoragePath { get; set; } = @"C:\TS-VMS\Storage";
        public bool EnableGpu { get; set; } = true;
        public string LogLevel { get; set; } = "Info";
        public string NasPath { get; set; } = "";
        public string BaseUrl { get; set; } = "http://127.0.0.1:8080";
        public string AuthTokenEncrypted { get; set; } = ""; // Stored securely
        public string SavedUsername { get; set; } = "";
        public string SavedPasswordEncrypted { get; set; } = ""; 
    }

    public class SettingsService
    {
        private readonly string _configPath;
        public AppSettings CurrentSettings { get; private set; }

        public SettingsService()
        {
            CurrentSettings = new AppSettings();
            // Use AppData for user-specific config
            string folder = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "TS-VMS");
            Directory.CreateDirectory(folder);
            _configPath = Path.Combine(folder, "desktop-config.json");

            Load();
        }

        public void Load()
        {
            if (File.Exists(_configPath))
            {
                try
                {
                    string json = File.ReadAllText(_configPath);
                    CurrentSettings = JsonSerializer.Deserialize<AppSettings>(json) ?? new AppSettings();
                    return;
                }
                catch { }
            }
            CurrentSettings = new AppSettings();
        }

        public void Save()
        {
            try
            {
                string json = JsonSerializer.Serialize(CurrentSettings, new JsonSerializerOptions { WriteIndented = true });
                File.WriteAllText(_configPath, json);
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"Failed to save settings: {ex.Message}");
            }
        }
    }
}
