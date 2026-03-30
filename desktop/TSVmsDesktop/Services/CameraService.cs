using System;
using System.Collections.ObjectModel;
using System.IO;
using System.Net.Http;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class CameraService
    {
        private readonly ApiClient _api;

        public ObservableCollection<CameraModel> AllCameras { get; private set; } = new();

        public CameraService(ApiClient api)
        {
            _api = api;
        }

        public async Task LoadCamerasAsync()
        {
            // GET /api/v1/cameras -> Returns { "data": [...], "meta": ... }
            var result = await _api.GetAsync<PaginatedResponse<CameraModel>>("/api/v1/cameras");

            void Apply()
            {
                AllCameras.Clear();
                if (result != null && result.Data != null)
                {
                    foreach (var c in result.Data)
                        AllCameras.Add(c);
                }
            }

            var dispatcher = System.Windows.Application.Current?.Dispatcher;
            if (dispatcher == null || dispatcher.CheckAccess())
            {
                Apply();
            }
            else
            {
                await dispatcher.InvokeAsync(Apply);
            }
        }

        private class PaginatedResponse<T>
        {
            [JsonPropertyName("data")]
            public List<T> Data { get; set; } = new();
        }

        public class HealthStatusDto 
        {
            [JsonPropertyName("camera_id")]
            public string CameraId { get; set; } = "";
            
            [JsonPropertyName("status")]
            public string Status { get; set; } = "OFFLINE";

            [JsonPropertyName("last_checked_at")]
            public DateTime LastCheckedAt { get; set; }
        }

        public async Task LoadHealthAsync()
        {
             // GET /api/v1/cameras/health -> Returns List<HealthStatusDto> directly
             try 
             {
                 var healthData = await _api.GetAsync<List<HealthStatusDto>>("/api/v1/cameras/health");
                 
                 // Handle null response (e.g. backend returns null for empty list)
                 if (healthData == null)
                 {
                     // VideoService.Log("[CameraService] Health data was null (treating as empty).");
                     healthData = new List<HealthStatusDto>();
                 }

                 if (healthData != null)
                 {
                     void Apply()
                     {
                         foreach (var h in healthData)
                         {
                             var cam = AllCameras.FirstOrDefault(c => c.Id == h.CameraId);
                             if (cam != null)
                             {
                                 string oldStatus = cam.Status;
                                 string newVal = h.Status?.ToUpper() ?? "OFFLINE";
                                 
                                 // VideoService.Log($"[CameraService] Cam {cam.Name} ({cam.Id}) Status: {h.Status} -> {newVal}");

                                 switch (newVal)
                                 {
                                     case "ONLINE":
                                         cam.Status = "Online";
                                         break;
                                     default:
                                         cam.Status = "Offline"; 
                                         break;
                                 }
                                 
                                 if (oldStatus != cam.Status)
                                 {
                                     VideoService.Log($"[CameraService] Updated {cam.Name} status to {cam.Status}");
                                 }
                             }
                             else
                             {
                                 // Silence during initial load (if AllCameras is empty)
                                 if (AllCameras.Any())
                                 {
                                     System.Diagnostics.Debug.WriteLine($"[CameraService] Warning: Health record for unknown camera ID: {h.CameraId}");
                                 }
                             }
                         }
                     }

                     var dispatcher = System.Windows.Application.Current?.Dispatcher;
                     if (dispatcher == null || dispatcher.CheckAccess())
                     {
                         Apply();
                     }
                     else
                     {
                         await dispatcher.InvokeAsync(Apply);
                     }
                 }
                 else
                 {
                     VideoService.Log("[CameraService] Health data was null.");
                 }
             }
             catch (Exception ex)
             {
                 VideoService.Log($"[CameraService] LoadHealthAsync Error: {ex.Message}");
             }
        }

        public async Task<bool> CreateCameraAsync(CameraModel cam)
        {
            // POST /api/v1/cameras
            var success = await _api.PostAsync("/api/v1/cameras", cam);
            if (success) await LoadCamerasAsync();
            return success;
        }

        public async Task<CameraModel?> GetCameraAsync(string id)
        {
            return await _api.GetAsync<CameraModel>($"/api/v1/cameras/{id}");
        }

        public async Task<bool> UpdateCameraAsync(CameraModel cam)
        {
             // PUT /api/v1/cameras/{id}
             var success = await _api.PutAsync($"/api/v1/cameras/{cam.Id}", cam);
             if (success) await LoadCamerasAsync();
             return success;
        }

        public async Task<bool> DeleteCameraAsync(string id)
        {
            var success = await _api.DeleteAsync($"/api/v1/cameras/{id}");
            if (success) await LoadCamerasAsync();
            return success;
        }

        public async Task<bool> EnableCameraAsync(string id) => await _api.PostAsync($"/api/v1/cameras/{id}/enable", new {});
        public async Task<bool> DisableCameraAsync(string id) => await _api.PostAsync($"/api/v1/cameras/{id}/disable", new {});
        
        public async Task<bool> BulkOpAsync(List<string> ids, string operation)
        {
            // POST /api/v1/cameras/bulk
            var payload = new { camera_ids = ids, action = operation };
            var success = await _api.PostAsync("/api/v1/cameras/bulk", payload);
            if(success) await LoadCamerasAsync();
            return success;
        }

        public async Task<List<CameraGroup>> GetGroupsAsync() => await _api.GetAsync<List<CameraGroup>>("/api/v1/camera-groups") ?? new();
        
        public async Task<bool> CreateGroupAsync(string name) 
        {
            return await _api.PostAsync("/api/v1/camera-groups", new { name = name });
        }
        
        public async Task<bool> UpdateGroupMembersAsync(string groupId, List<string> memberIds)
        {
            // PUT /api/v1/camera-groups/{id}/members
            return await _api.PutAsync($"/api/v1/camera-groups/{groupId}/members", new { member_ids = memberIds });
        }

        public async Task<bool> DeleteGroupAsync(string groupId) => await _api.DeleteAsync($"/api/v1/camera-groups/{groupId}");

        public async Task<HealthStatusDto?> GetSingleCameraHealthAsync(string id) => 
            await _api.GetAsync<HealthStatusDto>($"/api/v1/cameras/{id}/health");

        // Assuming a dynamic list of history records
        public async Task<object?> GetCameraHealthHistoryAsync(string id) => 
            await _api.GetAsync<object>($"/api/v1/cameras/{id}/health/history");

        public async Task<bool> ManualHealthRecheckAsync(string id) => 
            await _api.PostAsync($"/api/v1/cameras/{id}/health-recheck", new { });

        public async Task CheckServerHealthAsync()
        {
             // This might be redundant if we just reload cameras, but keeping for compatibility if needed or logic adjustments
             // For now, implementing as a no-op or simple reload if really needed, but the original requirement implies
             // health is part of the camera object now. 
             // If specific health check endpoint exists, use it. 
             // Original code had specific /health logic.
             // For Phase 2, we can just reload cameras to get status.
             await LoadCamerasAsync(); 
        }

        // Keep for backward compatibility if ViewModels call it, or update ViewModels
        public async void AddCamera(CameraModel cam)
        {
             await CreateCameraAsync(cam);
        }

        public async void RemoveCamera(CameraModel cam)
        {
             await DeleteCameraAsync(cam.Id);
        }
    }
}
