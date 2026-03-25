package onvif

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnvifClient_GetDeviceInformation(t *testing.T) {
	// Mock SOAP Response
	resp := `<?xml version="1.0" encoding="UTF-8"?>
	<SOAP-ENV:Envelope xmlns:SOAP-ENV="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl">
		<SOAP-ENV:Body>
			<tds:GetDeviceInformationResponse>
				<tds:Manufacturer>TestManufacturer</tds:Manufacturer>
				<tds:Model>TestModel</tds:Model>
				<tds:FirmwareVersion>1.2.3</tds:FirmwareVersion>
				<tds:SerialNumber>SN12345</tds:SerialNumber>
				<tds:HardwareId>HW123</tds:HardwareId>
			</tds:GetDeviceInformationResponse>
		</SOAP-ENV:Body>
	</SOAP-ENV:Envelope>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml")
		w.Write([]byte(resp))
	}))
	defer server.Close()

	client, _ := NewOnvifClient(server.URL, "admin", "password")
	info, err := client.GetDeviceInformation(context.Background())
	if err != nil {
		t.Fatalf("Failed to get device info: %v", err)
	}

	if info.Manufacturer != "TestManufacturer" {
		t.Errorf("Expected TestManufacturer, got %s", info.Manufacturer)
	}
	if info.SerialNumber != "SN12345" {
		t.Errorf("Expected SN12345, got %s", info.SerialNumber)
	}
}

func TestDetermineCapabilities(t *testing.T) {
	// Simple test for the helper moved to shared package
	cases := []struct {
		name     string
		profiles []MediaProfile
		audio    bool
		ptz      bool
	}{
		{
			"Profile S (No Audio/PTZ)",
			[]MediaProfile{{Token: "p1", Name: "ProfileS"}},
			false,
			false,
		},
		{
			"Audio Profile",
			[]MediaProfile{{Token: "p1", Name: "AudioEnc", AudioEncoderConfiguration: &struct{}{}}},
			true,
			false,
		},
		{
			"PTZ Profile",
			[]MediaProfile{{Token: "p2", Name: "PTZ", PTZConfiguration: &struct{}{}}},
			false,
			true,
		},
	}

	for _, c := range cases {
		got := DetermineCapabilities(c.profiles)
		if got.HasAudio != c.audio {
			t.Errorf("%s: Expected HasAudio %v, got %v", c.name, c.audio, got.HasAudio)
		}
		if got.PTZ != c.ptz {
			t.Errorf("%s: Expected PTZ %v, got %v", c.name, c.ptz, got.PTZ)
		}
	}
}
