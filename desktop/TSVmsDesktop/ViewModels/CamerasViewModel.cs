using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using System.Threading.Tasks;
using System.Windows; 
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;
using TSVmsDesktop.Views; 

namespace TSVmsDesktop.ViewModels
{
    public partial class CamerasViewModel : ObservableObject
    {
        private readonly CameraService _cameraService;

        // BINDING DIRECTLY to the Service's collection ensures updates are instant
        public ObservableCollection<CameraModel> Cameras => _cameraService.AllCameras;

        public CamerasViewModel(CameraService cameraService)
        {
            _cameraService = cameraService;
        }

        [RelayCommand]
        public void AddCamera()
        {
            // 1. Create and Configure the Window
            var dialog = new AddCameraWindow();
            dialog.Owner = System.Windows.Application.Current.MainWindow; 
            
            // 2. Show Modal
            bool? result = dialog.ShowDialog();

            // 3. Process Result
            if (result == true && dialog.CreatedCamera != null)
            {
                _cameraService.AddCamera(dialog.CreatedCamera);
            }
        }

        [RelayCommand]
        public void DeleteCamera(CameraModel cam)
        {
            if (cam == null) return;

            var result = System.Windows.MessageBox.Show($"Are you sure you want to delete '{cam.Name}'?", "Confirm Delete", MessageBoxButton.YesNo, MessageBoxImage.Warning);
            
            if (result == System.Windows.MessageBoxResult.Yes)
            {
                _cameraService.RemoveCamera(cam);
            }
        }

        [RelayCommand]
        public async Task RefreshHealth()
        {
            // If the list is empty, try to reload the whole list first
            if (_cameraService.AllCameras.Count == 0)
            {
                await _cameraService.LoadCamerasAsync();
            }
            else
            {
                // Otherwise, just update the status of existing cameras
                await _cameraService.CheckServerHealthAsync();
            }
            System.Diagnostics.Debug.WriteLine("[TS-VMS] Manual Health Refresh Requested.");
        }
    }
}
