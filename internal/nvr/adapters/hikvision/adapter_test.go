package hikvision

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/technosupport/ts-vms/internal/nvr/adapters"
)

func TestHikvisionDeviceInfo(t *testing.T) {
	// Mock Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ISAPI/System/deviceInfo" {
			t.Errorf("Unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<DeviceInfo>
	<deviceName>DS-7608NI-K2</deviceName>
	<model>DS-7608NI-K2</model>
	<serialNumber>DS-7608NI-K20820200830AAWRD12345678WCVU</serialNumber>
	<firmwareVersion>V4.30.000 build 200508</firmwareVersion>
	<manufacturer>Hikvision</manufacturer>
</DeviceInfo>`)
	}))
	defer ts.Close()

	adapter := NewAdapter()

	// Robust parse of ts.URL
	u, _ := url.Parse(ts.URL)
	target := adapters.NvrTarget{
		IP: u.Hostname(),
	}
	fmt.Sscanf(u.Port(), "%d", &target.Port)

	info, err := adapter.GetDeviceInfo(context.Background(), target, adapters.NvrCredential{})
	if err != nil {
		t.Fatalf("GetDeviceInfo failed: %v", err)
	}

	if info.Manufacturer != "Hikvision" {
		t.Errorf("Expected Hikvision, got %s", info.Manufacturer)
	}
	if info.Model != "DS-7608NI-K2" {
		t.Errorf("Expected model DS-7608NI-K2, got %s", info.Model)
	}
}

func TestHikvisionRtspUrls(t *testing.T) {
	adapter := NewAdapter()
	target := adapters.NvrTarget{IP: "1.2.3.4", Port: 80}

	main, sub, err := adapter.GetRtspUrls(context.Background(), target, adapters.NvrCredential{}, "101")
	if err != nil {
		t.Fatalf("GetRtspUrls failed: %v", err)
	}

	expectedMain := "rtsp://1.2.3.4:554/Streaming/Channels/10101"
	if main != expectedMain {
		t.Errorf("Expected %s, got %s", expectedMain, main)
	}

	expectedSub := "rtsp://1.2.3.4:554/Streaming/Channels/10102"
	if sub != expectedSub {
		t.Errorf("Expected %s, got %s", expectedSub, sub)
	}
}
