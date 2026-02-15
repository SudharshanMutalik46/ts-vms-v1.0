using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Threading.Tasks;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class LicenseViewModel : ObservableObject
    {
        private readonly LicenseService _licenseService;

        [ObservableProperty] private LicenseStatus? _status;
        [ObservableProperty] private bool _isLoading;
        [ObservableProperty] private string _message = "";
        [ObservableProperty] private string _quotaDisplay = "";

        // Default constructor for design-time (if needed) or DI
        public LicenseViewModel(LicenseService licenseService)
        {
            _licenseService = licenseService;
            _ = LoadStatus();
        }

        [RelayCommand]
        public async Task LoadStatus()
        {
            IsLoading = true;
            Status = await _licenseService.GetStatusAsync();
            UpdateQuotaDisplay();
            IsLoading = false;
        }

        [RelayCommand]
        public async Task Reload()
        {
            IsLoading = true;
            bool success = await _licenseService.ReloadLicenseAsync();
            if (success)
            {
                Message = "License reloaded successfully.";
                await LoadStatus();
            }
            else
            {
                Message = "Failed to reload license.";
            }
            IsLoading = false;
        }

        private void UpdateQuotaDisplay()
        {
            if (Status == null) return;
            var sb = new System.Text.StringBuilder();
            if (Status.Quotas != null)
            {
                foreach (var q in Status.Quotas)
                {
                    int used = (Status.Usage != null && Status.Usage.ContainsKey(q.Key)) ? Status.Usage[q.Key] : 0;
                    sb.AppendLine($"{q.Key}: {used} / {q.Value}");
                }
            }
            QuotaDisplay = sb.ToString();
        }
    }
}
