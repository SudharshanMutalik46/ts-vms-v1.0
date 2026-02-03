package onvif

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/technosupport/ts-vms/internal/nvr/adapters"
)

type Adapter struct {
	client *http.Client
}

func NewAdapter() *Adapter {
	return &Adapter{
		client: &http.Client{
			Timeout: adapters.DefaultTimeout * time.Second,
		},
	}
}

func init() {
	adapters.Register("onvif", func(target adapters.NvrTarget, cred adapters.NvrCredential) (adapters.Adapter, error) {
		return NewAdapter(), nil
	})
}

func (a *Adapter) Kind() string {
	return "onvif"
}

func (a *Adapter) GetDeviceInfo(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential) (adapters.NvrDeviceInfo, error) {
	// Does ONVIF GetDeviceInformation
	// <tds:GetDeviceInformation/>
	// Requires SOAP client.
	return adapters.NvrDeviceInfo{
		Manufacturer:        "ONVIF_Generic",
		Model:               "ONVIF_Model",
		FirmwareVersion:     "unknown",
		SerialNumber:        "unknown",
		CapabilitiesSummary: "onvif_profile_s",
	}, nil
}

func (a *Adapter) ListChannels(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential) ([]adapters.NvrChannel, error) {
	// GetProfiles -> Extract VideoSourceConfiguration -> Map to Channel
	return nil, nil // Return empty list
}

func (a *Adapter) GetRtspUrls(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential, channelRef string) (string, string, error) {
	// GetStreamUri(ProfileToken=channelRef)
	// Stub:
	main := fmt.Sprintf("rtsp://%s:%d/onvif/live/main?token=%s", target.IP, 554, channelRef)
	return adapters.SanitizeRtspUrl(main), "", nil
}

func (a *Adapter) FetchEvents(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential, since time.Time, limit int) ([]adapters.NvrEvent, int, error) {
	return nil, 0, errors.New("not_supported")
}
