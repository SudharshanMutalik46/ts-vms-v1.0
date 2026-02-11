using System;
using System.Collections.ObjectModel;
using System.IO;
using System.Text.Json;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class CameraService
    {
        private readonly string _filePath;
        public ObservableCollection<CameraModel> AllCameras { get; private set; } = new();

        public CameraService()
        {
            // Save to %AppData%\TS-VMS\cameras.json
            string folder = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "TS-VMS");
            if (!Directory.Exists(folder)) Directory.CreateDirectory(folder);
            _filePath = Path.Combine(folder, "cameras.json");

            LoadCameras();
        }

        public void AddCamera(CameraModel cam)
        {
            AllCameras.Add(cam);
            SaveCameras(); // Auto-save on add
        }

        public void RemoveCamera(CameraModel cam)
        {
            if (AllCameras.Contains(cam))
            {
                AllCameras.Remove(cam);
                SaveCameras(); // Auto-save on remove
            }
        }

        private void SaveCameras()
        {
            try
            {
                string json = JsonSerializer.Serialize(AllCameras, new JsonSerializerOptions { WriteIndented = true });
                File.WriteAllText(_filePath, json);
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"[Error] Failed to save cameras: {ex.Message}");
            }
        }

        private void LoadCameras()
        {
            try
            {
                if (File.Exists(_filePath))
                {
                    string json = File.ReadAllText(_filePath);
                    var loaded = JsonSerializer.Deserialize<ObservableCollection<CameraModel>>(json);
                    
                    if (loaded != null)
                    {
                        foreach (var cam in loaded) AllCameras.Add(cam);
                    }
                }
                else
                {
                    // DEFAULT DATA (Only if no file exists)
                    // We keep these defaults so your app isn't empty on first run
                    AllCameras.Add(new CameraModel { Name = "Main Gate", RtspUrl = "test", Status = "Online" });
                    AllCameras.Add(new CameraModel { Name = "Lobby", RtspUrl = "test", Status = "Online" });
                    AllCameras.Add(new CameraModel { Name = "Parking", RtspUrl = "test", Status = "Online" });
                    SaveCameras(); // Create the initial file
                }
            }
            catch 
            {
                // Fallback if file is corrupt
            }
        }
    }
}
