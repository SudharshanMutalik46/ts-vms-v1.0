package adapters

import (
	"testing"
)

func TestSanitizeRtspUrl(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Basic URL",
			input:    "rtsp://192.168.1.100:554/live",
			expected: "rtsp://192.168.1.100:554/live",
		},
		{
			name:     "URL with credentials",
			input:    "rtsp://admin:pass123@192.168.1.100:554/live",
			expected: "rtsp://192.168.1.100:554/live",
		},
		{
			name:     "URL with credentials and query",
			input:    "rtsp://admin:pass123@192.168.1.100:554/live?param=1",
			expected: "rtsp://192.168.1.100:554/live?param=1",
		},
		{
			name:     "URL with sensitive query param",
			input:    "rtsp://192.168.1.100/live?token=secret123&user=admin",
			expected: "rtsp://192.168.1.100/live?user=admin",
		},
		{
			name:     "Invalid URL fallback",
			input:    "rtsp://admin:pass@invalid@host",
			expected: "rtsp://host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeRtspUrl(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeRtspUrl() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConstrainLimits(t *testing.T) {
	if got := ConstrainLimits(1000, 512); got != 512 {
		t.Errorf("Expected 512, got %d", got)
	}
	if got := ConstrainLimits(0, 512); got != 512 {
		t.Errorf("Expected 512, got %d", got)
	}
	if got := ConstrainLimits(10, 512); got != 10 {
		t.Errorf("Expected 10, got %d", got)
	}
}

func TestRedactMap(t *testing.T) {
	m := map[string]interface{}{
		"username": "admin",
		"password": "secretpassword",
		"token":    "abc-123",
		"id":       1,
	}
	redacted := RedactMap(m)

	if redacted["password"] != "[REDACTED]" {
		t.Errorf("Expected password to be redacted")
	}
	if redacted["token"] != "[REDACTED]" {
		t.Errorf("Expected token to be redacted")
	}
	if redacted["username"] != "admin" {
		t.Errorf("Username should not be redacted")
	}
}
