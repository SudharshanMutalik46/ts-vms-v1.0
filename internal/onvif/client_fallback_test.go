package onvif

import (
	"testing"
)

func containsCandidate(cands []string, want string) bool {
	for _, c := range cands {
		if c == want {
			return true
		}
	}
	return false
}

func TestBuildRtspFallbackCandidates(t *testing.T) {
	cands := buildRtspFallbackCandidates("rtsp://192.168.1.46:554/ch01_sub.264?dev=1")

	if len(cands) == 0 {
		t.Fatal("expected fallback candidates")
	}

	if cands[0] != "rtsp://192.168.1.46:554/ch01_sub.264?dev=1" {
		t.Fatalf("unexpected first candidate: %s", cands[0])
	}

	if !containsCandidate(cands, "rtsp://192.168.1.46:554/ch01_sub.264") {
		t.Fatal("expected stripped-query candidate")
	}

	if !containsCandidate(cands, "rtsp://192.168.1.46:554/ch01.264") {
		t.Fatal("expected main-stream candidate")
	}

	if !containsCandidate(cands, "rtsp://192.168.1.46:554/Streaming/Channels/101") {
		t.Fatal("expected hikvision-style fallback candidate")
	}

	if !containsCandidate(cands, "rtsp://192.168.1.46:554/cam/realmonitor?channel=1&subtype=0") {
		t.Fatal("expected dahua-style fallback candidate")
	}
}
