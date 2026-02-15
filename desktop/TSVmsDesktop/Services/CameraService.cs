using System;
using System.Collections.ObjectModel;
using System.IO;
using System.Text.Json;
using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class CameraService
    {
        private readonly string _filePath;
        private readonly ApiClient _api;
        private System.Timers.Timer _refreshTimer;

        public ObservableCollection<CameraModel> AllCameras { get; private set; } = new();

        public CameraService(ApiClient api)
        {
            _api = api;
            string folder = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "TS-VMS");
            if (!Directory.Exists(folder)) Directory.CreateDirectory(folder);
            _filePath = Path.Combine(folder, "cameras.json");

            _ = LoadCamerasAsync();

            // Setup Timer to refresh health every 30 seconds
            _refreshTimer = new System.Timers.Timer(30000); 
            _refreshTimer.Elapsed += async (s, e) => await CheckServerHealthAsync();
            _refreshTimer.AutoReset = true;
            _refreshTimer.Enabled = true;
        }

        public async void AddCamera(CameraModel cam)
        {
            try { await _api.PostAsync("/api/v1/cameras", cam); } catch { }
            AllCameras.Add(cam);
            SaveCameras();
        }

        public void RemoveCamera(CameraModel cam)
        {
            if (AllCameras.Contains(cam)) { AllCameras.Remove(cam); SaveCameras(); }
        }

        private void SaveCameras()
        {
            try {
                string json = JsonSerializer.Serialize(AllCameras, new JsonSerializerOptions { WriteIndented = true });
                File.WriteAllText(_filePath, json);
            } catch { }
        }

        public async Task LoadCamerasAsync()
        {
            try
            {
                var root = await _api.GetAsync<JsonElement>("/api/v1/cameras");
                JsonSerializerOptions options = new JsonSerializerOptions { PropertyNameCaseInsensitive = true };
                ObservableCollection<CameraModel>? remoteCameras = null;

                if (root.ValueKind == JsonValueKind.Object && root.TryGetProperty("data", out var dataProp))
                    remoteCameras = JsonSerializer.Deserialize<ObservableCollection<CameraModel>>(dataProp.GetRawText(), options);
                else if (root.ValueKind == JsonValueKind.Array)
                    remoteCameras = JsonSerializer.Deserialize<ObservableCollection<CameraModel>>(root.GetRawText(), options);

                if (remoteCameras != null)
                {
                    System.Windows.Application.Current.Dispatcher.Invoke(() => {
                        AllCameras.Clear();
                        foreach(var c in remoteCameras) 
                        {
                            c.Status = "Checking...";
                            AllCameras.Add(c);
                        }
                    });
                    
                    await CheckServerHealthAsync();
                }
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"[Load Error] {ex.Message}");
            }
        }

        public Task CheckServerHealthAsync()
        {
            // We iterate through all cameras and update their status via the Backend API
            foreach (var cam in AllCameras)
            {
                _ = Task.Run(async () => 
                {
                    try
                    {
                        // Endpoint recently implemented in Go backend
                        string url = $"/api/v1/cameras/{cam.Id}/health";
                        var health = await _api.GetAsync<JsonElement>(url);
                        
                        string status = "Offline";
                        if (health.ValueKind == JsonValueKind.Object && health.TryGetProperty("status", out var s))
                        {
                            status = s.ToString();
                        }

                        System.Windows.Application.Current.Dispatcher.Invoke(() => {
                            cam.Status = status; 
                        });
                    }
                    catch
                    {
                        System.Windows.Application.Current.Dispatcher.Invoke(() => cam.Status = "Offline");
                    }
                });
            }
            return Task.CompletedTask;
        }
    }
}
