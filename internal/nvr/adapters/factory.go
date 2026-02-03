package adapters

import (
	"fmt"
	"strings"
)

// Registry of adapter factories
var Registry = map[string]Factory{}

// Register adds a factory for a vendor
func Register(vendor string, f Factory) {
	Registry[strings.ToLower(vendor)] = f
}

// GetAdapter returns an initialized adapter for the target
func GetAdapter(target NvrTarget, cred NvrCredential) (Adapter, error) {
	kind := strings.ToLower(target.Vendor)

	// Normalize vendor names
	if strings.Contains(kind, "hikvision") {
		kind = "hikvision"
	} else if strings.Contains(kind, "dahua") {
		kind = "dahua"
	} else if strings.Contains(kind, "onvif") {
		kind = "onvif"
	} else if kind == "" || kind == "manual" || kind == "rtsp" {
		kind = "rtsp_fallback"
	}

	factory, ok := Registry[kind]
	if !ok {
		// Default to RTSP fallback if unknown, providing a safe generic interface
		// But maybe verify if we should error?
		// Plan says: "unknown vendor falls back to RTSP-only adapter deterministically"
		// We'll use "rtsp_fallback" factory if available, or error.
		fallback, ok := Registry["rtsp_fallback"]
		if ok {
			return fallback(target, cred)
		}
		return nil, fmt.Errorf("unknown vendor '%s' and no fallback available", target.Vendor)
	}

	return factory(target, cred)
}
