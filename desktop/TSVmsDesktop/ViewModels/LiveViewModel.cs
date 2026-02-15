using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using System.Linq;
using System;
using System.Threading.Tasks;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class CameraSlot : ObservableObject
    {
        [ObservableProperty] private string _overlayText = "";
        
        [ObservableProperty] 
        [NotifyPropertyChangedFor(nameof(StatusColor))]
        private bool _isConnected = false; 
        
        public string StatusColor => IsConnected ? "#10B981" : "#EF4444";

        [ObservableProperty] private string _cameraName = "";
        
        [ObservableProperty] private string _ipAddress = "Offline"; // For UI display

        // Added ID to link back to Backend
        public string Id { get; set; } = "";
        public IntPtr PipelineHandle { get; set; } = IntPtr.Zero;
        public string RtspUrl { get; set; } = ""; 
    }

    public partial class LiveViewModel : ObservableObject
    {
        private readonly VideoService _videoService;
        private readonly CameraService _cameraService;
        private readonly ApiClient _apiClient;
        private readonly MainViewModel _mainViewModel; // New Dependency

        public ObservableCollection<CameraSlot> CameraGrid { get; } = new();

        public LiveViewModel(VideoService videoService, CameraService cameraService, ApiClient apiClient, MainViewModel mainViewModel) 
        {
            _videoService = videoService;
            _cameraService = cameraService;
            _apiClient = apiClient;
            _mainViewModel = mainViewModel;

            _videoService.Initialize();

            RefreshGrid();
        }

        [RelayCommand]
        public async Task ConnectAll()
        {
            System.Diagnostics.Debug.WriteLine("[TS-VMS] Connecting all cameras...");
            
            foreach (var slot in CameraGrid)
            {
                // Only connect valid slots that aren't already connected
                if (!string.IsNullOrEmpty(slot.Id) && !slot.IsConnected)
                {
                    try
                    {
                        // 1. Tell Backend we are starting (Audit/Resource allocation)
                        // POST /api/v1/cameras/{id}/live/start
                        string url = $"/api/v1/cameras/{slot.Id}/live/start";
                        
                        // We send a generic body, or empty. 
                        // The backend expects a POST to trigger the session.
                        var body = new { stream = "main" }; 
                        
                        // We don't block heavily on this, if it fails we might still try RTSP
                        // but strictly we should wait.
                        await _apiClient.PostAsync(url, body);
                        System.Diagnostics.Debug.WriteLine($"[Live] Session started for {slot.CameraName}");
                    }
                    catch (Exception ex)
                    {
                        System.Diagnostics.Debug.WriteLine($"[Live] Backend session start failed: {ex.Message}");
                        // Optional: Continue anyway so local playback works even if backend API is glitchy
                    }

                    // 2. Trigger UI to start GStreamer (Via bindings)
                    slot.IsConnected = true;
                    slot.CameraName = string.IsNullOrEmpty(slot.CameraName) ? "Live Stream" : slot.CameraName;
                }
            }
            OnPropertyChanged(nameof(ActiveStreamCount));
        }

        // Renaming the old command to point to the new Async logic
        [RelayCommand]
        public async Task ConnectDemo() => await ConnectAll();

        private void RefreshGrid()
        {
            CameraGrid.Clear();
            var realCameras = _cameraService.AllCameras;

            // Create a 12-grid layout
            for (int i = 0; i < 12; i++)
            {
                var slot = new CameraSlot { OverlayText = $"CAM-{i+1:D2}", IsConnected = false };

                if (i < realCameras.Count)
                {
                    var cam = realCameras[i];
                    slot.Id = cam.Id; // Store ID for API calls
                    slot.CameraName = cam.Name;
                    slot.RtspUrl = cam.EffectiveRtspUrl;
                    slot.OverlayText = cam.Name;
                    
                    // Extract IP for display
                    try 
                    {
                        if (Uri.TryCreate(cam.EffectiveRtspUrl, UriKind.Absolute, out var uri))
                        {
                            slot.IpAddress = uri.Host;
                        }
                    }
                    catch { /* Ignore */ }
                }

                CameraGrid.Add(slot);
            }
        }

        // --- Full Screen Mode ---
        [ObservableProperty]
        private bool _isFullScreen = false;

        [ObservableProperty]
        private string _fullScreenUrl = "";

        // --- Dashboard Stats ---
        public int ActiveStreamCount => CameraGrid.Count(c => c.IsConnected);

        [RelayCommand]
        public void EnterFullScreen(string url)
        {
            if (string.IsNullOrEmpty(url)) return;
            FullScreenUrl = url;
            IsFullScreen = true;
            _mainViewModel.IsKioskMode = true; // Hide Chrome & Maximize
        }

        [RelayCommand]
        public void ExitFullScreen()
        {
            IsFullScreen = false;
            FullScreenUrl = "";
            _mainViewModel.IsKioskMode = false; // Restore Chrome & Normal Window
        }

        [RelayCommand]
        public void Snapshot(string cameraName)
        {
            // Placeholder for Snapshot Logic
            System.Diagnostics.Debug.WriteLine($"[Snapshot] Taking snapshot of {cameraName}");
        }


    }
}
