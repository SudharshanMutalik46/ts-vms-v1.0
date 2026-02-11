using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.ViewModels
{
    public partial class CamerasViewModel : ObservableObject
    {
        [ObservableProperty] private ObservableCollection<CameraModel> _cameras = new();

        public CamerasViewModel()
        {
            // Load Dummy Data for Design Verification
            Cameras.Add(new CameraModel { Name = "Main Gate Entry", IpAddress = "192.168.1.101", Status = "Online", Model = "Hikvision 4K" });
            Cameras.Add(new CameraModel { Name = "Lobby Wide", IpAddress = "192.168.1.102", Status = "Online", Model = "Axis P32" });
            Cameras.Add(new CameraModel { Name = "Parking North", IpAddress = "192.168.1.105", Status = "Offline", Model = "Dahua Bullet" });
            Cameras.Add(new CameraModel { Name = "Server Room", IpAddress = "192.168.1.110", Status = "Online", Model = "Uniview Dome" });
            Cameras.Add(new CameraModel { Name = "Warehouse Dock", IpAddress = "192.168.1.112", Status = "Error", Model = "Generic RTSP" });
        }

        [RelayCommand]
        public void AddCamera()
        {
            // Placeholder for "Add Camera" Dialog
            Cameras.Add(new CameraModel { Name = "New Camera", IpAddress = "0.0.0.0", Status = "Offline" });
        }

        [RelayCommand]
        public void DeleteCamera(CameraModel cam)
        {
            if (Cameras.Contains(cam)) Cameras.Remove(cam);
        }
    }
}
