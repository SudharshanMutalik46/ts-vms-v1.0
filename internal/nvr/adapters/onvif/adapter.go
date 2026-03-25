package onvif

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/technosupport/ts-vms/internal/nvr/adapters"
	"github.com/technosupport/ts-vms/internal/onvif"
)

type Adapter struct {
	// Reusing logic from discovery, but here we don't store one client 
	// because each target might have different credentials.
	// The factory provides target and cred.
}

func NewAdapter() *Adapter {
	return &Adapter{}
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
	xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", target.IP, target.Port)
	if target.Port == 0 {
		xaddr = fmt.Sprintf("http://%s/onvif/device_service", target.IP)
	}

	cli, err := onvif.NewOnvifClient(xaddr, cred.Username, cred.Password)
	if err != nil {
		return adapters.NvrDeviceInfo{}, err
	}

	info, err := cli.GetDeviceInformation(ctx)
	if err != nil {
		return adapters.NvrDeviceInfo{}, err
	}

	// Also get capabilities to check for Profile S/G/T hint
	_, _, _, _, _ = cli.GetCapabilities(ctx)

	return adapters.NvrDeviceInfo{
		Manufacturer:    info.Manufacturer,
		Model:           info.Model,
		FirmwareVersion: info.FirmwareVersion,
		SerialNumber:    info.SerialNumber,
		CapabilitiesSummary: "onvif_generic", // Could be more specific if we check caps
	}, nil
}

func (a *Adapter) ListChannels(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential) ([]adapters.NvrChannel, error) {
	xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", target.IP, target.Port)
	if target.Port == 0 {
		xaddr = fmt.Sprintf("http://%s/onvif/device_service", target.IP)
	}

	cli, err := onvif.NewOnvifClient(xaddr, cred.Username, cred.Password)
	if err != nil {
		return nil, err
	}

	features, mediaURI, _, media2URI, err := cli.GetCapabilities(ctx)
	if err != nil {
		return nil, err
	}

	bestMediaURI := mediaURI
	useMedia2 := false
	if features["Media2"] && media2URI != "" {
		bestMediaURI = media2URI
		useMedia2 = true
	} else if bestMediaURI == "" {
		bestMediaURI = xaddr
	}

	profiles, err := cli.GetProfiles(ctx, bestMediaURI)
	if err != nil {
		return nil, err
	}

	var channels []adapters.NvrChannel
	for _, p := range profiles {
		// Get Stream URI for each profile to have them ready
		mainURI, _ := cli.GetStreamUri(ctx, bestMediaURI, p.Token, useMedia2)
		
		ch := adapters.NvrChannel{
			ChannelRef:        p.Token,
			Name:              p.Name,
			RTSPMain:          mainURI,
			SupportsSubStream: false, // Default, could be refined by looking for second profile
		}
		channels = append(channels, ch)
	}

	return channels, nil
}

func (a *Adapter) GetRtspUrls(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential, channelRef string) (string, string, error) {
	xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", target.IP, target.Port)
	if target.Port == 0 {
		xaddr = fmt.Sprintf("http://%s/onvif/device_service", target.IP)
	}

	cli, err := onvif.NewOnvifClient(xaddr, cred.Username, cred.Password)
	if err != nil {
		return "", "", err
	}

	features, mediaURI, _, media2URI, err := cli.GetCapabilities(ctx)
	if err != nil {
		return "", "", err
	}

	bestMediaURI := mediaURI
	useMedia2 := false
	if features["Media2"] && media2URI != "" {
		bestMediaURI = media2URI
		useMedia2 = true
	} else if bestMediaURI == "" {
		bestMediaURI = xaddr
	}

	uri, err := cli.GetStreamUri(ctx, bestMediaURI, channelRef, useMedia2)
	if err != nil {
		return "", "", err
	}

	return adapters.SanitizeRtspUrl(uri), "", nil
}

func (a *Adapter) FetchEvents(ctx context.Context, target adapters.NvrTarget, cred adapters.NvrCredential, since time.Time, limit int) ([]adapters.NvrEvent, int, error) {
	return nil, 0, errors.New("not_supported")
}
