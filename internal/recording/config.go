package recording

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Config struct {
	Global struct {
		SegmentDurationSec int    `yaml:"segment_duration_sec"`
		HealthPort         int    `yaml:"health_port"`
		ApiBaseURL         string `yaml:"api_base_url"`
		DevRbacBypass      bool   `yaml:"dev_rbac_bypass"`
		StorageRoot        string `yaml:"storage_root"`
		ExportRoot         string `yaml:"export_root"`
		FFmpegPath         string `yaml:"ffmpeg_path"`
		GstLaunchPath      string `yaml:"gst_launch_path"`
		DefaultTenantID    string `yaml:"default_tenant_id"`
		DefaultSiteID      string `yaml:"default_site_id"`
	} `yaml:"global"`
	Limits struct {
		MaxRecordingCameras int `yaml:"max_recording_cameras"`
	} `yaml:"limits"`
	Cameras []CameraConfig `yaml:"cameras"`

	Schedules []ScheduleConfig `yaml:"schedules"`

	HealthMonitoring struct {
		Enabled           bool `yaml:"enabled"`
		SampleIntervalSec int  `yaml:"sample_interval_sec"`
		FrameDrop         struct {
			Enabled         bool    `yaml:"enabled"`
			WarnDropRatePct float64 `yaml:"warn_drop_rate_pct"`
			CritDropRatePct float64 `yaml:"crit_drop_rate_pct"`
			WindowSec       int     `yaml:"window_sec"`
			Method          string  `yaml:"method"`
		} `yaml:"frame_drop"`
		DiskWriteRate struct {
			Enabled     bool    `yaml:"enabled"`
			WindowSec   int     `yaml:"window_sec"`
			WarnMinMBps float64 `yaml:"warn_min_mbps"`
			CritMinMBps float64 `yaml:"crit_min_mbps"`
		} `yaml:"disk_write_rate"`
		Alerts struct {
			CooldownSec                 int `yaml:"cooldown_sec"`
			SustainedWindowsForCritical int `yaml:"sustained_windows_for_critical"`
		} `yaml:"alerts"`
	} `yaml:"health_monitoring"`

	FailoverRecovery struct {
		Enabled             bool   `yaml:"enabled"`
		RestartBackoffSec   int    `yaml:"restart_backoff_sec"`
		DBRequiredForReady  bool   `yaml:"db_required_for_ready"`
		OrphanReconcileMode string `yaml:"orphan_reconcile_mode"`
	} `yaml:"failover_recovery"`

	CircuitBreaker struct {
		Enabled          bool `yaml:"enabled"`
		WarnFreeGB       int  `yaml:"warn_free_gb"`
		CritFreeGB       int  `yaml:"crit_free_gb"`
		WarnUsagePercent int  `yaml:"warn_usage_percent"`
		CritUsagePercent int  `yaml:"crit_usage_percent"`
		CheckIntervalSec int  `yaml:"check_interval_sec"`
		CooldownSec      int  `yaml:"cooldown_sec"`
	} `yaml:"circuit_breaker"`

	Performance struct {
		Pipeline struct {
			QueueMaxTimeNs     int64 `yaml:"queue_max_time_ns"`
			FragmentDurationMs int   `yaml:"fragment_duration_ms"`
			Faststart          bool  `yaml:"faststart"`
			RTSPSrcLatencyMs   int   `yaml:"rtspsrc_latency_ms"`
		} `yaml:"pipeline"`
		IO struct {
			SegmentWriterBatchBytes int  `yaml:"segment_writer_batch_bytes"`
			PreallocateFiles        bool `yaml:"preallocate_files"`
		} `yaml:"io"`
	} `yaml:"performance"`
}

type CameraConfig struct {
	ID            string `yaml:"id" json:"id"`
	RtspURL       string `yaml:"rtsp_url" json:"rtsp_url"`
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Codec         string `yaml:"codec" json:"codec,omitempty"`                   // h264 | h265
	SegmentFormat string `yaml:"segment_format" json:"segment_format,omitempty"` // mp4 | mkv
	RTSPTransport string `yaml:"rtsp_transport" json:"rtsp_transport,omitempty"` // tcp | udp
	Username      string `yaml:"-" json:"-"`                                     // Decrypted internal use
	Password      string `yaml:"-" json:"-"`                                     // Decrypted internal use
}

type ScheduleConfig struct {
	CameraID  string   `yaml:"camera_id" json:"camera_id"`
	Type      string   `yaml:"type" json:"type"`
	Days      []string `yaml:"days" json:"days"`
	StartTime string   `yaml:"start_time" json:"start_time"`
	EndTime   string   `yaml:"end_time" json:"end_time"`
}

func (c *Config) ApplyDefaults() {
	if c.Global.SegmentDurationSec <= 0 {
		c.Global.SegmentDurationSec = 60
	}
	if c.Global.HealthPort <= 0 {
		c.Global.HealthPort = 8082
	}
	if c.FailoverRecovery.RestartBackoffSec <= 0 {
		c.FailoverRecovery.RestartBackoffSec = 5
	}
	if c.HealthMonitoring.SampleIntervalSec <= 0 {
		c.HealthMonitoring.SampleIntervalSec = 5
	}
	if c.CircuitBreaker.CheckIntervalSec <= 0 {
		c.CircuitBreaker.CheckIntervalSec = 5
	}
	if c.Performance.Pipeline.RTSPSrcLatencyMs <= 0 {
		c.Performance.Pipeline.RTSPSrcLatencyMs = 200
	}
	if c.Performance.IO.SegmentWriterBatchBytes <= 0 {
		c.Performance.IO.SegmentWriterBatchBytes = 4 * 1024 * 1024
	}
	if c.Global.DefaultTenantID == "" {
		c.Global.DefaultTenantID = "tenant-default"
	}
	if c.Global.DefaultSiteID == "" {
		c.Global.DefaultSiteID = "site-default"
	}
	if c.Global.FFmpegPath == "" {
		if v := os.Getenv("TS_VMS_FFMPEG_PATH"); v != "" {
			c.Global.FFmpegPath = v
		} else {
			c.Global.FFmpegPath = "ffmpeg"
		}
	}
	if c.Global.GstLaunchPath == "" {
		if v := os.Getenv("TS_VMS_GST_LAUNCH_PATH"); v != "" {
			c.Global.GstLaunchPath = v
		} else if runtime.GOOS == "windows" {
			c.Global.GstLaunchPath = "gst-launch-1.0.exe"
		} else {
			c.Global.GstLaunchPath = "gst-launch-1.0"
		}
	}
	if c.Global.StorageRoot == "" {
		if v := os.Getenv("TS_VMS_RECORDING_ROOT"); v != "" {
			c.Global.StorageRoot = v
		} else if runtime.GOOS == "windows" {
			c.Global.StorageRoot = `C:\ProgramData\TechnoSupport\VMS\recordings`
		} else {
			c.Global.StorageRoot = "/var/lib/ts-vms/recordings"
		}
	}
	if c.Global.ExportRoot == "" {
		if v := os.Getenv("TS_VMS_EXPORT_ROOT"); v != "" {
			c.Global.ExportRoot = v
		} else if runtime.GOOS == "windows" {
			c.Global.ExportRoot = `C:\ProgramData\TechnoSupport\VMS\exports`
		} else {
			c.Global.ExportRoot = "/var/lib/ts-vms/exports"
		}
	}
	for i := range c.Cameras {
		c.Cameras[i].Codec = strings.ToLower(strings.TrimSpace(c.Cameras[i].Codec))
		c.Cameras[i].SegmentFormat = strings.ToLower(strings.TrimSpace(c.Cameras[i].SegmentFormat))
		c.Cameras[i].RTSPTransport = strings.ToLower(strings.TrimSpace(c.Cameras[i].RTSPTransport))

		if c.Cameras[i].Codec == "" {
			c.Cameras[i].Codec = "h265"
		}
		if c.Cameras[i].SegmentFormat == "" {
			c.Cameras[i].SegmentFormat = "mkv"
		}
		if c.Cameras[i].RTSPTransport == "" {
			c.Cameras[i].RTSPTransport = "tcp"
		}
	}
	_ = os.MkdirAll(c.Global.StorageRoot, 0o755)
	_ = os.MkdirAll(c.Global.ExportRoot, 0o755)
}

func (c *Config) Validate() error {
	if c.Global.SegmentDurationSec < 5 {
		return fmt.Errorf("segment_duration_sec must be >= 5")
	}
	for _, cam := range c.Cameras {
		if cam.ID == "" {
			return fmt.Errorf("camera id cannot be empty")
		}
		if cam.Enabled && cam.RtspURL == "" {
			return fmt.Errorf("camera %s enabled without rtsp_url", cam.ID)
		}
		switch cam.Codec {
		case "", "h264", "h265":
		default:
			return fmt.Errorf("camera %s has unsupported codec %q", cam.ID, cam.Codec)
		}
		switch cam.SegmentFormat {
		case "", "mp4", "mkv":
		default:
			return fmt.Errorf("camera %s has unsupported segment_format %q", cam.ID, cam.SegmentFormat)
		}
		switch cam.RTSPTransport {
		case "", "tcp", "udp":
		default:
			return fmt.Errorf("camera %s has unsupported rtsp_transport %q", cam.ID, cam.RTSPTransport)
		}
	}
	if !filepath.IsAbs(c.Global.StorageRoot) {
		return fmt.Errorf("storage_root must be absolute: %s", c.Global.StorageRoot)
	}
	if !filepath.IsAbs(c.Global.ExportRoot) {
		return fmt.Errorf("export_root must be absolute: %s", c.Global.ExportRoot)
	}
	return nil
}
