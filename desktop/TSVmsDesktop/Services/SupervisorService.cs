using System;
using System.Diagnostics;
using System.IO;
using System.Threading.Tasks;

namespace TSVmsDesktop.Services
{
    public class SupervisorService
    {
        private readonly IHealthService _healthService;
        private readonly string _logPath;

        public SupervisorService(IHealthService healthService)
        {
            _healthService = healthService;
            // Define local log path. Adjust if logs are stored elsewhere.
            // For now, assuming standard AppData or where the app writes logs + api_debug_log.txt on Desktop
            _logPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "TS-VMS");
        }

        public async Task<(bool Healthy, string Details)> GetSystemHealthAsync()
        {
            return await _healthService.CheckHealthAsync();
        }

        public void OpenLogFolder()
        {
            // Also try to open the Desktop log file if it exists, or just the folder
            string debugLog = @"C:\Users\sudha\Desktop\api_debug_log.txt";
            if (File.Exists(debugLog))
            {
                 Process.Start(new ProcessStartInfo
                {
                    FileName = debugLog,
                    UseShellExecute = true
                });
            }
            
            if (Directory.Exists(_logPath))
            {
                Process.Start(new ProcessStartInfo
                {
                    FileName = _logPath,
                    UseShellExecute = true,
                    Verb = "open"
                });
            }
        }

        public void OpenEventViewer()
        {
            try
            {
                Process.Start(new ProcessStartInfo
                {
                    FileName = "eventvwr.msc",
                    UseShellExecute = true
                });
            }
            catch { /* Ignore permission errors */ }
        }
    }
}
