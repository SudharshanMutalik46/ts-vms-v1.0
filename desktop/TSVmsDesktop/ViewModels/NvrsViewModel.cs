using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using System.Threading.Tasks;
using System.Windows;
using Microsoft.Extensions.DependencyInjection;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class NvrsViewModel : ObservableObject
    {
        private readonly NvrService _nvrService;
        private readonly MainViewModel _mainVm;

        [ObservableProperty] private ObservableCollection<NvrModel> _nvrs = new();
        [ObservableProperty] private bool _isLoading;

        public NvrsViewModel(NvrService nvrService, MainViewModel mainVm)
        {
            _nvrService = nvrService;
            _mainVm = mainVm;
            _ = LoadNvrs();
        }

        [RelayCommand]
        public async Task LoadNvrs()
        {
            IsLoading = true;
            var list = await _nvrService.GetNvrsAsync();
            Nvrs.Clear();
            foreach (var n in list) Nvrs.Add(n);
            IsLoading = false;
        }

        [RelayCommand]
        public async Task AddNvr()
        {
            try
            {
                // Quick-add placeholder with random suffix to avoid conflicts
                var random = new System.Random().Next(100, 255);
                var newNvr = new NvrModel 
                { 
                    Name = $"New NVR {random}", 
                    IpAddress = $"10.0.0.{random}", 
                    AdapterType = "onvif" 
                };
                
                if (await _nvrService.CreateNvrAsync(newNvr))
                    await LoadNvrs();
                else
                    System.Windows.MessageBox.Show("Failed to create NVR. Check logs.", "Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
            }
            catch (System.Exception ex)
            {
                System.Windows.MessageBox.Show($"Error adding NVR: {ex.Message}", "Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public async Task DeleteNvr(object? parameter)
        {
            if (parameter is not NvrModel nvr) return;

            if (System.Windows.MessageBox.Show($"Delete NVR '{nvr.Name}'? This may orphan linked cameras.", "Confirm", System.Windows.MessageBoxButton.YesNo, System.Windows.MessageBoxImage.Warning) == System.Windows.MessageBoxResult.Yes)
            {
                try
                {
                    if (await _nvrService.DeleteNvrAsync(nvr.Id))
                        await LoadNvrs();
                }
                catch (System.Exception ex)
                {
                     System.Windows.MessageBox.Show($"Error deleting NVR: {ex.Message}", "Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
                }
            }
        }

        [RelayCommand]
        public void OpenDetails(object? parameter)
        {
            if (parameter is not NvrModel nvr) return;
            _mainVm.NavigateToNvrDetails(nvr.Id);
        }
    }
}
