namespace TSVmsDesktop.Models
{
    public class CameraModel
    {
        public string Name { get; set; } = "";
        public string IpAddress { get; set; } = "";
        public string Status { get; set; } = "Offline"; // Online, Offline, Error
        public string Model { get; set; } = "Generic RTSP";
        public string Thumbnail { get; set; } = "/Images/cam_placeholder.png"; // We'll use a generic icon
    }
}
