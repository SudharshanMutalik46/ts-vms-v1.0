package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
)

const (
	MaxChannels    = 512
	MaxEvents      = 200
	MaxPayloadSize = 8192
	DefaultTimeout = 5 // Seconds
)

var (
	// Regex to identify user:pass in RTSP URLs
	// matches rtsp://user:pass@host...
	rtspCredsRegex = regexp.MustCompile(`(?i)^(rtsp|rtsps)://([^@]+)@`)
)

// SanitizeRtspUrl strips credentials and dangerous query parameters from RTSP URLs.
// It ensures the output is safe for logging and frontend display.
func SanitizeRtspUrl(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		// Fallback regex for non-standard URLs
		return rtspCredsRegex.ReplaceAllString(rawURL, "$1://")
	}

	// Clear User Info
	u.User = nil

	// Clear dangerous query params (tokens, passwords)
	q := u.Query()
	for k := range q {
		kl := strings.ToLower(k)
		if strings.Contains(kl, "token") || strings.Contains(kl, "pass") || strings.Contains(kl, "auth") || strings.Contains(kl, "secret") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()

	return u.String()
}

// ConstrainLimits ensures requested limits do not exceed hard caps.
func ConstrainLimits(limit, max int) int {
	if limit <= 0 || limit > max {
		return max
	}
	return limit
}

// RedactMap creates a copy of the map with redacted values for sensitive keys.
func RedactMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		kl := strings.ToLower(k)
		if strings.Contains(kl, "password") || strings.Contains(kl, "token") || strings.Contains(kl, "secret") {
			out[k] = "[REDACTED]"
		} else {
			out[k] = v
		}
	}
	return out
}

// HashCredential creates a safe fingerprint of a credential for debugging (not storage).
func HashCredential(username, password string) string {
	h := sha256.Sum256([]byte(username + ":" + password))
	return hex.EncodeToString(h[:4]) + "..." // Only first 4 bytes
}
