namespace TSVmsDesktop.Models
{
    public class AppConfig
    {
        public string StoragePath { get; set; } = @"C:\TS-VMS\Storage";
        public bool EnableHardwareAcceleration { get; set; } = true;
        public bool EnableDarkMode { get; set; } = false;
        public string LogLevel { get; set; } = "Error";
        
        // Window State Persistence
        public double WindowTop { get; set; } = 100;
        public double WindowLeft { get; set; } = 100;
        public double WindowWidth { get; set; } = 1280;
        public double WindowHeight { get; set; } = 720;
        public bool IsMaximized { get; set; } = false;
    }
}
