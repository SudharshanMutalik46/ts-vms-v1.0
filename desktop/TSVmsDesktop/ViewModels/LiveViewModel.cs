using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using System.Linq;
using System;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    // FIX: Ensure this class is 'partial' and inherits 'ObservableObject'
    public partial class CameraSlot : ObservableObject
    {
        [ObservableProperty] private string _overlayText = "";
        
        // THIS is the property that triggers the visibility change
        [ObservableProperty] private bool _isConnected = false; 
        
        [ObservableProperty] private string _cameraName = "";

        public IntPtr PipelineHandle { get; set; } = IntPtr.Zero;
        public string RtspUrl { get; set; } = ""; 
    }

    public partial class LiveViewModel : ObservableObject
    {
        private readonly VideoService _videoService;
        private readonly CameraService _cameraService;

        public ObservableCollection<CameraSlot> CameraGrid { get; } = new();

        public LiveViewModel(VideoService videoService, CameraService cameraService) 
        {
            _videoService = videoService;
            _cameraService = cameraService;
            _videoService.Initialize();

            RefreshGrid();
        }

        [RelayCommand]
        public void ConnectDemo()
        {
            System.Diagnostics.Debug.WriteLine("[TS-VMS] 'View All' Clicked - Updating Slots...");
            
            foreach (var slot in CameraGrid)
            {
                // Force the UI to update
                slot.IsConnected = true;
                slot.CameraName = string.IsNullOrEmpty(slot.CameraName) ? "Live Stream" : slot.CameraName;
            }
        }

        private void RefreshGrid()
        {
            CameraGrid.Clear();
            var realCameras = _cameraService.AllCameras;

            for (int i = 0; i < 12; i++)
            {
                var slot = new CameraSlot { OverlayText = $"CAM-{i+1:D2}", IsConnected = false };

                if (i < realCameras.Count)
                {
                    var cam = realCameras[i];
                    slot.CameraName = cam.Name;
                    slot.RtspUrl = cam.RtspUrl;
                    slot.OverlayText = cam.Name;
                    // Note: We keep IsConnected false initially so the button click triggers the change
                }

                CameraGrid.Add(slot);
            }
        }
    }
}
