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
            
            System.Windows.Application.Current.Dispatcher.Invoke(() => 
            {
                AllCameras.Clear();
                if (result != null && result.Data != null)
                {
                    foreach (var c in result.Data) AllCameras.Add(c);
                }
            });
        }

        private class PaginatedResponse<T>
        {
            [JsonPropertyName("data")]
            public List<T> Data { get; set; } = new();
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
