using CommunityToolkit.Mvvm.ComponentModel;
using CommunityToolkit.Mvvm.Input;
using System;
using System.Collections.ObjectModel;
using System.Threading.Tasks;
using TSVmsDesktop.Models;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class AuditViewModel : ObservableObject
    {
        private readonly AuditService _auditService;
        
        [ObservableProperty] private ObservableCollection<AuditEvent> _events = new();
        [ObservableProperty] private string _filterAction = "";
        [ObservableProperty] private bool _isLoading = false;

        public AuditViewModel(AuditService auditService)
        {
            _auditService = auditService;
            // FIX: Automatically load logs when the view model is created
            _ = LoadEvents(); 
        }

        [RelayCommand]
        public async Task LoadEvents()
        {
            IsLoading = true;
            try 
            {
                var list = await _auditService.GetEventsAsync(FilterAction);
                
                // UI thread safe update
                System.Windows.Application.Current.Dispatcher.Invoke(() => {
                    Events.Clear();
                    if (list != null)
                    {
                        foreach (var ev in list) Events.Add(ev);
                    }
                });
            }
            catch (Exception ex)
            {
                Console.WriteLine($"[Audit] Load Failed: {ex.Message}");
                if(ex.InnerException != null) Console.WriteLine($"[Audit] Inner: {ex.InnerException.Message}");
            }
            finally { IsLoading = false; }
        }

        [RelayCommand]
        public async Task Export()
        {
            var dialog = new Microsoft.Win32.SaveFileDialog { Filter = "CSV Files|*.csv", FileName = $"audit_log_{DateTime.Now:yyyyMMdd}.csv" };
            if (dialog.ShowDialog() == true)
            {
                IsLoading = true;
                bool success = await _auditService.ExportLogsAsync(dialog.FileName, null, null);
                IsLoading = false;
                
                if(success) System.Windows.MessageBox.Show("Export complete.", "Audit", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Information);
                else System.Windows.MessageBox.Show("Export failed.", "Error", System.Windows.MessageBoxButton.OK, System.Windows.MessageBoxImage.Error);
            }
        }
    }
}
