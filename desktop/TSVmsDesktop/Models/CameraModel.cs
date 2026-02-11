namespace TSVmsDesktop.Models
{
    public class CameraModel
    {
        public string Name { get; set; } = "New Camera";
        public string IpAddress { get; set; } = "0.0.0.0";
        public string Status { get; set; } = "Offline"; 
        public string Model { get; set; } = "Generic";
        public string Thumbnail { get; set; } = "/Images/cam_placeholder.png"; // We'll use a generic icon
        public string RtspUrl { get; set; } = "test"; // NEW: The actual video link
    }
}
