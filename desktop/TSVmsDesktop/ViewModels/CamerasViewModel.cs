using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using Microsoft.Extensions.DependencyInjection;
using System.Collections.ObjectModel;
using System.Linq;
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
        private readonly CredentialService _credentialService;
        private readonly MainViewModel _mainViewModel;

        // BINDING DIRECTLY to the Service's collection ensures updates are instant
        public ObservableCollection<CameraModel> Cameras => _cameraService.AllCameras;
        
        [ObservableProperty] private bool _isMultiSelectMode;
        [ObservableProperty] private ObservableCollection<CameraModel> _selectedCameras = new();

        public CamerasViewModel(CameraService cameraService, CredentialService credentialService, MainViewModel mainViewModel)
        {
            _cameraService = cameraService;
            _credentialService = credentialService;
            _mainViewModel = mainViewModel;
            _ = RefreshList(); // Auto-load and check status
        }

        [RelayCommand]
        public async Task RefreshList() 
        {
            await _cameraService.LoadCamerasAsync();
            _ = CheckAllStatuses();
        }

        private async Task CheckAllStatuses()
        {
            var tasks = Cameras.Select(async c => 
            {
                if (!c.IsEnabled) 
                {
                    c.Status = "Disabled";
                    return;
                }
                
                c.Status = "Checking...";
                try 
                {
                    using var client = new System.Net.Sockets.TcpClient();
                    var connectTask = client.ConnectAsync(c.IpAddress, c.Port > 0 ? c.Port : 554);
                    // 2 second timeout
                    if (await Task.WhenAny(connectTask, Task.Delay(2000)) == connectTask)
                    {
                        await connectTask; // Throw if failed
                        c.Status = "Online";
                    }
                    else
                    {
                        c.Status = "Offline";
                    }
                }
                catch
                {
                    c.Status = "Offline";
                }
            });
            await Task.WhenAll(tasks);
        }

        [RelayCommand]
        public async Task EnableSelected()
        {
            var ids = Cameras.Where(c => c.IsSelected).Select(c => c.Id).ToList();
            if (ids.Any()) 
            {
                await _cameraService.BulkOpAsync(ids, "enable");
                await RefreshList();
            }
        }

        [RelayCommand]
        public async Task DisableSelected()
        {
            var ids = Cameras.Where(c => c.IsSelected).Select(c => c.Id).ToList();
            if (ids.Any()) 
            {
                await _cameraService.BulkOpAsync(ids, "disable");
                await RefreshList();
            }
        }
        
        [RelayCommand]
        public void OpenDetails(CameraModel cam)
        {
            if (cam == null) return;
            ((App)App.Current).Services.GetRequiredService<MainViewModel>().NavigateToCameraDetails(cam.Id);
        }

        [RelayCommand]
        public void OpenDiscovery()
        {
            ((App)App.Current).Services.GetRequiredService<MainViewModel>().NavigateToDiscovery();
        }

        [RelayCommand]
        public async Task AddCamera()
        {
            var dialog = new AddCameraWindow();
            dialog.Owner = System.Windows.Application.Current.MainWindow; 
            
            bool? result = dialog.ShowDialog();

            if (result == true && dialog.CreatedCamera != null)
            {
                _cameraService.AddCamera(dialog.CreatedCamera);

                // Save credentials if provided
                if (!string.IsNullOrWhiteSpace(dialog.CameraUsername) && !string.IsNullOrWhiteSpace(dialog.CameraPassword))
                {
                    // Wait for the camera list to reload so we get the new camera's server-assigned ID
                    await Task.Delay(500);
                    var added = Cameras.FirstOrDefault(c => c.Name == dialog.CreatedCamera.Name);
                    if (added != null && !string.IsNullOrEmpty(added.Id))
                    {
                        var credSaved = await _credentialService.UpdateCredentialsAsync(added.Id, dialog.CameraUsername, dialog.CameraPassword);
                        if (credSaved)
                        {
                            System.Windows.MessageBox.Show("Camera added and credentials saved!", "Success", MessageBoxButton.OK, MessageBoxImage.Information);
                        }
                    }
                }
            }
        }

        [RelayCommand]
        public void DeleteCamera(CameraModel cam)
        {
            // Single delete (keep existing or map to API)
             if (cam == null) return;

             var result = System.Windows.MessageBox.Show($"Are you sure you want to delete '{cam.Name}'?", "Confirm Delete", MessageBoxButton.YesNo, MessageBoxImage.Warning);
             
             if (result == System.Windows.MessageBoxResult.Yes)
             {
                 _cameraService.RemoveCamera(cam); // Calls DeleteCameraAsync
             }
        }
    }
}
