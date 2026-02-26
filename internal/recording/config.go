package recording

type Config struct {
	Global struct {
		SegmentDurationSec int    `yaml:"segment_duration_sec"`
		HealthPort         int    `yaml:"health_port"`
		ApiBaseUrl         string `yaml:"api_base_url"`
		DevRbacBypass      bool   `yaml:"dev_rbac_bypass"`
	} `yaml:"global"`
	Limits struct {
		MaxRecordingCameras int `yaml:"max_recording_cameras"`
	} `yaml:"limits"`
	Cameras   []CameraConfig   `yaml:"cameras"`
	Schedules []ScheduleConfig `yaml:"schedules"`
}

type CameraConfig struct {
	ID      string `yaml:"id"`
	RtspURL string `yaml:"rtsp_url"`
	Enabled bool   `yaml:"enabled"`
}

type ScheduleConfig struct {
	CameraID  string   `yaml:"camera_id"`
	Type      string   `yaml:"type"` // "24x7", "time_window", "event_triggered"
	Days      []string `yaml:"days"`
	StartTime string   `yaml:"start_time"` // "HH:MM"
	EndTime   string   `yaml:"end_time"`   // "HH:MM"
}
