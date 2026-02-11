using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;

namespace TSVmsDesktop.ViewModels
{
    public partial class CameraSlot : ObservableObject
    {
        [ObservableProperty] private string _cameraName = "No Signal";
        [ObservableProperty] private bool _isConnected = false;
        [ObservableProperty] private string _overlayText = "CAM-01";
    }

    public partial class LiveViewModel : ObservableObject
    {
        // 12 Slots for 4x3 Grid
        public ObservableCollection<CameraSlot> CameraGrid { get; } = new();

        public LiveViewModel()
        {
            // Reset and create exactly 12 slots to match the 4x3 Dashboard Grid
            CameraGrid.Clear();
            for(int i = 1; i <= 12; i++)
            {
                CameraGrid.Add(new CameraSlot { OverlayText = $"CAM-{i:D2}" });
            }
        }

        [RelayCommand]
        public void ConnectDemo()
        {
            // Simulates connecting a camera behavior
            if (CameraGrid.Count > 0)
            {
                CameraGrid[0].IsConnected = true;
                CameraGrid[0].CameraName = "Parking Lot Entry";
                
                // Light up a few more for the "View All" demo feel
                if (CameraGrid.Count > 4) {
                     CameraGrid[1].IsConnected = true;
                     CameraGrid[4].IsConnected = true;
                }
            }
        }
    }
}
