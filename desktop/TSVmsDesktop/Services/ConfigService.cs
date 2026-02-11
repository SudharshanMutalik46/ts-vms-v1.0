using System;
using System.IO;
using System.Text.Json;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public interface IConfigService
    {
        AppConfig Settings { get; }
        void Load();
        void Save();
    }

    public class ConfigService : IConfigService
    {
        private readonly string _configFolder;
        private readonly string _configFile;
        
        public AppConfig Settings { get; private set; } = new AppConfig();

        public ConfigService()
        {
            // Save to: C:\Users\[You]\AppData\Local\TS-VMS\config.json
            _configFolder = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "TS-VMS");
            _configFile = Path.Combine(_configFolder, "config.json");
            
            Load(); // Load immediately on startup
        }

        public void Load()
        {
            try
            {
                if (File.Exists(_configFile))
                {
                    string json = File.ReadAllText(_configFile);
                    var loaded = JsonSerializer.Deserialize<AppConfig>(json);
                    if (loaded != null) Settings = loaded;
                }
            }
            catch 
            {
                // If load fails, stick to defaults
            }
        }

        public void Save()
        {
            try
            {
                if (!Directory.Exists(_configFolder)) Directory.CreateDirectory(_configFolder);
                
                string json = JsonSerializer.Serialize(Settings, new JsonSerializerOptions { WriteIndented = true });
                File.WriteAllText(_configFile, json);
            }
            catch
            {
                // Handle save error silently or log it
            }
        }
    }
}
