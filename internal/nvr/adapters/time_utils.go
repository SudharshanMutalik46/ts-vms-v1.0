package adapters

import (
	"strings"
	"time"
)

// ParseVendorTime attempts to parse timestamps from various NVR vendors.
// Hikvision ISAPI: 2023-10-27T10:00:00Z or 2023-10-27T10:00:00+08:00
// Dahua JSON-RPC: 2023-10-27 10:00:00
func ParseVendorTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}

	// Try RFC3339 (Hikvision)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}

	// Try Dahua/Standard SQL format
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return t
	}

	// Try Hikvision without Z or Zone
	if t, err := time.Parse("2006-01-02T15:04:05", raw); err == nil {
		return t
	}

	// Fallback: strip timezone manually if needed or return zero
	clean := strings.Split(raw, ".")[0] // strip sub-seconds
	if t, err := time.Parse("2006-01-02 15:04:05", clean); err == nil {
		return t
	}

	return time.Time{}
}
