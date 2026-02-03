package rtsp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/technosupport/ts-vms/internal/nvr/adapters"
)

type Adapter struct {
	target adapters.NvrTarget
	conf   Config // Could contain URL templates
}

type Config struct {
	UrlTemplate string // e.g. "rtsp://{ip}:{port}/Streaming/Channels/{channel}01"
}

func NewAdapter(target adapters.NvrTarget, tmpl string) *Adapter {
	if tmpl == "" {
		tmpl = "rtsp://{ip}:{port}/Streaming/Channels/{channel}01" // Default Hikvision-like pattern
	}
	return &Adapter{
		target: target,
		conf:   Config{UrlTemplate: tmpl},
	}
}

func init() {
	adapters.Register("rtsp_fallback", func(target adapters.NvrTarget, cred adapters.NvrCredential) (adapters.Adapter, error) {
		// In a real system, we'd fetch the template from NVR metadata or global config.
		// For now, use the default Hikvision template.
		return NewAdapter(target, ""), nil
	})
}

func (a *Adapter) Kind() string {
	return "rtsp_fallback"
}

func (a *Adapter) GetDeviceInfo(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential) (adapters.NvrDeviceInfo, error) {
	// RTSP fallback cannot fetch device info. Sanitize output.
	return adapters.NvrDeviceInfo{
		Manufacturer:        "Generic",
		Model:               "RTSP Device",
		FirmwareVersion:     "unknown",
		SerialNumber:        "unknown",
		CapabilitiesSummary: "basic_rtsp",
	}, nil
}

func (a *Adapter) ListChannels(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential) ([]adapters.NvrChannel, error) {
	// RTSP fallback cannot enumerate channels.
	return nil, errors.New("rtsp_fallback: list_channels not supported")
}

func (a *Adapter) FetchEvents(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential, since time.Time, limit int) ([]adapters.NvrEvent, int, error) {
	return nil, 0, errors.New("rtsp_fallback: events not supported")
}

func (a *Adapter) GetRtspUrls(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential, channelRef string) (string, string, error) {
	// Manual template substitution
	// Validates channelRef is alphanumeric to prevent injection
	if !isAlphanumeric(channelRef) {
		return "", "", errors.New("rtsp_fallback: invalid channel ref")
	}

	url := a.conf.UrlTemplate
	url = strings.ReplaceAll(url, "{ip}", target.IP)
	url = strings.ReplaceAll(url, "{port}", fmt.Sprintf("%d", target.Port))
	url = strings.ReplaceAll(url, "{channel}", channelRef)

	// Inject credentials? Phase 2.7 prompt says "SanitizeRtspUrl".
	// BUT, if we want to USE the URL, we might need creds.
	// However, the interface returns `rtsp_main` which `NvrChannel` description says "Sanitized".
	// The prompt goal is "Produce sanitized RTSP URLs per channel (no embedded creds)".
	// Wait, if it's sanitized, how does the VMS *use* it?
	// The VMS (Media Plane) will inject credentials at runtime using the `NvrCredential` object from secure storage.
	// The ADAPTER's job is to return the RESOURCE URL.
	// So we generally return `rtsp://1.2.3.4/path` WITHOUT `user:pass`.
	// Correct.

	sanitized := adapters.SanitizeRtspUrl(url)
	return sanitized, "", nil // Sub-stream unknown/unsupported in basic template
}

func isAlphanumeric(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
