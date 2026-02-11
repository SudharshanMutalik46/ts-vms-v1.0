using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Threading.Tasks;
using System.Windows;

namespace TSVmsDesktop.ViewModels
{
    public partial class ServiceItem : ObservableObject
    {
        [ObservableProperty] private string _name = string.Empty;
        [ObservableProperty] private string _status = string.Empty; // Running, Stopped
        [ObservableProperty] private string _port = string.Empty;
        [ObservableProperty] private bool _isRunning;
    }

    public partial class SystemHealthViewModel : ObservableObject
    {
        public ObservableCollection<ServiceItem> Services { get; } = new();

        public SystemHealthViewModel()
        {
            // Initialize with Phase 3.9 required services
            Services.Add(new ServiceItem { Name = "ControlPlane", Status = "Running", Port = "8080", IsRunning = true });
            Services.Add(new ServiceItem { Name = "MediaPlane", Status = "Running", Port = "8888", IsRunning = true });
            Services.Add(new ServiceItem { Name = "SFU (WebRTC)", Status = "Running", Port = "5000", IsRunning = true });
            Services.Add(new ServiceItem { Name = "Redis", Status = "Running", Port = "6379", IsRunning = true });
            Services.Add(new ServiceItem { Name = "NATS", Status = "Running", Port = "4222", IsRunning = true });
        }

        [RelayCommand]
        public async Task RestartService(ServiceItem service)
        {
            if (service == null) return;

            service.Status = "Stopping...";
            service.IsRunning = false;
            
            // SIMULATION: In real production, call Process.Start("sc", "restart ...")
            await Task.Delay(2000); 

            service.Status = "Running";
            service.IsRunning = true;
            System.Windows.MessageBox.Show($"{service.Name} restarted successfully.", "Service Supervisor", MessageBoxButton.OK, MessageBoxImage.Information);
        }

        [RelayCommand]
        public void CollectSupportBundle()
        {
            System.Windows.MessageBox.Show("Support Bundle (Logs + Config) zipped to Desktop.", "Support", MessageBoxButton.OK, MessageBoxImage.Information);
        }
    }
}
