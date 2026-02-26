package health

import "time"

type State string

const (
	StateStopped            State = "STOPPED"
	StateActive             State = "ACTIVE"
	StatePaused             State = "PAUSED"
	StateFailed             State = "FAILED"
	StateThrottledByLicense State = "THROTTLED_BY_LICENSE"
	StateRecovering         State = "RECOVERING"
)

type Config struct {
	Enabled           bool
	SampleIntervalSec int
	FrameDrop         struct {
		Enabled         bool
		WarnDropRatePct float64
		CritDropRatePct float64
		WindowSec       int
	}
	DiskWriteRate struct {
		Enabled     bool
		WindowSec   int
		WarnMinMBps float64
		CritMinMBps float64
	}
	Alerts struct {
		CooldownSec                 int
		SustainedWindowsForCritical int
	}
}

// CameraStatus represents the exposed API state of a camera
type CameraStatus struct {
	CameraID               string    `json:"camera_id"`
	State                  State     `json:"state"`
	SinceTS                time.Time `json:"since_ts"`
	LastError              string    `json:"last_error,omitempty"`
	RestartCount           int       `json:"restart_count"`
	LastSegmentFinalizedTS time.Time `json:"last_segment_finalized_ts"`
	LastWriteBytes         uint64    `json:"last_write_bytes"` // Total written
	WriteMBpsAvg           float64   `json:"write_mbps_avg"`   // Over sliding window
	PeakMBps               float64   `json:"peak_mbps"`
	FrameDropCountWindow   uint64    `json:"frame_drop_count_window"`
	FrameDropRatePctWindow float64   `json:"frame_drop_rate_pct_window"`
	LastFramePTSTS         time.Time `json:"last_frame_pts_ts"`
}

type GlobalStatus struct {
	TotalWriteMBps float64                  `json:"total_write_mbps"`
	ActiveCameras  int                      `json:"active_cameras"`
	FailedCameras  int                      `json:"failed_cameras"`
	AlertsActive   int                      `json:"alerts_active"`
	Cameras        map[string]*CameraStatus `json:"cameras"`
}
