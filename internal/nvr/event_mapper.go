package nvr

import "strings"

// MapVendorEvent maps raw vendor strings to VMS types
func MapVendorEvent(vendor, rawType string) (string, string) {
	raw := strings.ToLower(rawType)

	switch vendor {
	case "hikvision":
		if strings.Contains(raw, "motion") || strings.Contains(raw, "vmd") {
			return "motion", "info"
		}
		if strings.Contains(raw, "tamper") || strings.Contains(raw, "shelter") {
			return "tamper", "warn"
		}
		if strings.Contains(raw, "diskfull") || strings.Contains(raw, "hddfull") {
			return "disk_full", "critical"
		}
	case "dahua":
		if strings.Contains(raw, "motiondetected") || strings.Contains(raw, "videomotion") {
			return "motion", "info"
		}
		if strings.Contains(raw, "videoloss") || strings.Contains(raw, "videotamper") || strings.Contains(raw, "blind") {
			return "tamper", "warn"
		}
		if strings.Contains(raw, "storagef") || strings.Contains(raw, "diskfull") {
			return "disk_full", "critical"
		}
	}

	return "unknown", "info"
}
