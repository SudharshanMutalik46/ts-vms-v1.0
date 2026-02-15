using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System.Collections.ObjectModel;
using System.Linq;
using System.Threading.Tasks;
using System.Windows;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class NvrDetailsViewModel : ObservableObject
    {
        private readonly NvrService _nvrService;
        private readonly MainViewModel _mainVm;
        private string _nvrId = "";

        [ObservableProperty] private NvrModel _nvr = new();
        [ObservableProperty] private string _credentialPassword = "";
        
        // Channel Discovery
        [ObservableProperty] private ObservableCollection<NvrChannel> _channels = new();
        [ObservableProperty] private string _discoveryStatus = "Ready";
        [ObservableProperty] private bool _isDiscoveryRunning;

        // Events
        [ObservableProperty] private ObservableCollection<NvrEvent> _events = new();

        public ObservableCollection<string> AdapterTypes { get; } = new() { "hikvision_isapi", "dahua_json", "onvif", "rtsp" };

        public NvrDetailsViewModel(NvrService nvrService, MainViewModel mainVm)
        {
            _nvrService = nvrService;
            _mainVm = mainVm;
        }

        public async void Load(string id)
        {
            _nvrId = id;
            var list = await _nvrService.GetNvrsAsync();
            var found = list.FirstOrDefault(n => n.Id == id);
            if (found != null) Nvr = found;
            else { System.Windows.MessageBox.Show("NVR not found"); Close(); }
        }

        [RelayCommand]
        public async Task SaveConfig()
        {
            // Save basic info
            await _nvrService.UpdateNvrAsync(Nvr);
            
            // Save credentials if typed
            if (!string.IsNullOrEmpty(CredentialPassword))
            {
                await _nvrService.SetCredentialsAsync(_nvrId, Nvr.Username, CredentialPassword);
            }
            System.Windows.MessageBox.Show("Configuration Saved.", "Success", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Information);
        }

        [RelayCommand]
        public async Task TestConnection()
        {
            DiscoveryStatus = "Testing Connection...";
            bool ok = await _nvrService.TestConnectionAsync(_nvrId);
            DiscoveryStatus = ok ? "Connection Successful" : "Connection Failed";
        }

        [RelayCommand]
        public async Task DiscoverChannels()
        {
            IsDiscoveryRunning = true;
            DiscoveryStatus = "Scanning Channels...";
            
            // Trigger backend scan
            await _nvrService.StartDiscoveryAsync(_nvrId);
            
            // Poll for results (simple delay for now)
            await Task.Delay(2000); 
            var results = await _nvrService.GetChannelsAsync(_nvrId);
            
            Channels.Clear();
            foreach (var c in results) Channels.Add(c);
            
            DiscoveryStatus = $"Found {Channels.Count} channels.";
            IsDiscoveryRunning = false;
        }

        [RelayCommand]
        public async Task ProvisionSelected()
        {
            var selectedIds = Channels.Where(c => c.IsSelected).Select(c => c.Id).ToList();
            if (selectedIds.Count == 0) return;

            if (await _nvrService.ProvisionCamerasAsync(_nvrId, selectedIds))
            {
                System.Windows.MessageBox.Show("Cameras Provisioned Successfully! Check Camera Inventory.", "Success");
                await DiscoverChannels(); // Refresh status
            }
            else
            {
                System.Windows.MessageBox.Show("Provisioning Failed.", "Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
            }
        }

        [RelayCommand]
        public async Task LoadEvents()
        {
            var ev = await _nvrService.GetEventsAsync(_nvrId);
            Events.Clear();
            foreach(var e in ev) Events.Add(e);
        }

        [RelayCommand]
        public void Close() => _mainVm.NavigateToNvrs();
    }
}
