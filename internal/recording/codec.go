package recording

import (
	"net/url"
	"strings"
)

func normalizeCodec(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "H264", "AVC", "H.264", "VIDEO/H264", "X264":
		return "h264"
	case "H265", "HEVC", "H.265", "VIDEO/H265", "X265":
		return "h265"
	default:
		return ""
	}
}

func inferCodecFromRTSPURL(rtspURL string) string {
	u := strings.ToLower(strings.TrimSpace(rtspURL))
	if u == "" {
		return ""
	}
	switch {
	case strings.Contains(u, ".264"), strings.Contains(u, "h264"), strings.Contains(u, "avc"):
		return "h264"
	case strings.Contains(u, ".265"), strings.Contains(u, "h265"), strings.Contains(u, "hevc"):
		return "h265"
	default:
		return ""
	}
}

func extractRTSPHost(rtspURL string) string {
	u, err := url.Parse(strings.TrimSpace(rtspURL))
	if err != nil || u == nil {
		return ""
	}
	return strings.TrimSpace(u.Host)
}
