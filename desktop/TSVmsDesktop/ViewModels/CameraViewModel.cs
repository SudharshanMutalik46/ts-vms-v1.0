using CommunityToolkit.Mvvm.ComponentModel;
using System;
using System.Threading.Tasks;
using TSVmsDesktop.Services;

namespace TSVmsDesktop.ViewModels
{
    public partial class CameraViewModel : ObservableObject
    {
        private readonly RecordingService _recordingService;

        public string CameraId { get; set; } = "";

        [ObservableProperty] private string _recordingBadgeText = "";
        [ObservableProperty] private bool _isRecordingBadgeVisible;

        public CameraViewModel(RecordingService recordingService)
        {
            _recordingService = recordingService;
        }

        public async Task PollRecordingStatusAsync()
        {
            try
            {
                var state = await _recordingService.GetCameraStatusAsync(CameraId);

                RecordingBadgeText = state.ToUpperInvariant() switch
                {
                    "RECORDING" => "REC",
                    "RETRYING" => "RETRYING...",
                    _ => ""
                };

                IsRecordingBadgeVisible = !string.IsNullOrWhiteSpace(RecordingBadgeText);
            }
            catch (Exception ex)
            {
                System.Diagnostics.Debug.WriteLine($"Error polling recording status: {ex.Message}");
                RecordingBadgeText = "";
                IsRecordingBadgeVisible = false;
            }
        }
    }
}
