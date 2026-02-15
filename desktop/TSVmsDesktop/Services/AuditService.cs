using System.Collections.Generic;
using System.IO;
using System.Threading.Tasks;
using System;
using TSVmsDesktop.Models;

namespace TSVmsDesktop.Services
{
    public class AuditService
    {
        private readonly ApiClient _api;

        public AuditService(ApiClient api)
        {
            _api = api;
        }

        public async Task<List<AuditEvent>> GetEventsAsync(string? filterAction = null)
        {
            // Simple query param construction
            string url = "/api/v1/audit/events";
            if (!string.IsNullOrEmpty(filterAction)) url += $"?action={filterAction}";
            
            var result = await _api.GetAsync<List<AuditEvent>>(url);
            return result ?? new List<AuditEvent>();
        }

        public async Task<bool> ExportLogsAsync(string filePath, DateTime? start, DateTime? end)
        {
            var req = new AuditExportRequest 
            { 
                Format = "csv",
                StartTime = start,
                EndTime = end
            };

            return await _api.DownloadFileAsync("/api/v1/audit/exports", req, filePath);
        }
    }
}
