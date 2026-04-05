package adapters

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ProbeRTSP performs a lightweight DESCRIBE handshake.
// Does NOT use complex libraries to keep dependency footprint low (boundedness).
func ProbeRTSP(ctx context.Context, rtspURL string) error {
	return ProbeRTSPWithTimeout(ctx, rtspURL, 5*time.Second)
}

// ProbeRTSPWithTimeout allows callers to tighten or loosen the probe deadline.
func ProbeRTSPWithTimeout(ctx context.Context, rtspURL string, timeout time.Duration) error {
	_, _, err := describeRTSP(ctx, rtspURL, timeout)
	return err
}

// ProbeRTSPCodecWithTimeout returns the codec advertised by the RTSP SDP payload.
// It prefers explicit H.264/H.265 mappings and returns an empty string when the
// server does not expose a recognizable codec in the SDP.
func ProbeRTSPCodecWithTimeout(ctx context.Context, rtspURL string, timeout time.Duration) (string, error) {
	_, sdp, err := describeRTSP(ctx, rtspURL, timeout)
	if err != nil {
		return "", err
	}
	return inferCodecFromSDP(sdp), nil
}

func describeRTSP(ctx context.Context, rtspURL string, timeout time.Duration) (int, string, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	u, err := url.Parse(rtspURL)
	if err != nil {
		return 0, "", fmt.Errorf("invalid url: %v", err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":554" // Default RTSP port
	}

	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()

	// RTSP DESCRIBE is a better signal than OPTIONS for whether the media
	// path itself exists and can produce SDP.
	// CSeq: 1
	// User-Agent: TS-VMS-Health
	msg := fmt.Sprintf("DESCRIBE %s RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: TS-VMS-Health\r\nAccept: application/sdp\r\n\r\n", rtspURL)

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return 0, "", err
	}

	if _, err := conn.Write([]byte(msg)); err != nil {
		return 0, "", err
	}

	// Read Response
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return 0, "", err
	}

	// Expect "RTSP/1.0 200 OK"
	parts := strings.Fields(statusLine)
	if len(parts) < 2 {
		return 0, "", fmt.Errorf("malformed response: %s", statusLine)
	}

	code := parts[1]
	if code == "401" || code == "403" {
		return atoiCode(code), "", fmt.Errorf("auth_failed: %s", code)
	}
	if !strings.HasPrefix(code, "2") {
		return atoiCode(code), "", fmt.Errorf("stream_error: %s", code)
	}

	headers, err := readRTSPHeaders(reader)
	if err != nil {
		return atoiCode(code), "", err
	}

	body, err := readRTSPBody(reader, headers)
	if err != nil {
		return atoiCode(code), "", err
	}

	return atoiCode(code), body, nil
}

func atoiCode(code string) int {
	if len(code) != 3 {
		return 0
	}
	n := 0
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func readRTSPHeaders(reader *bufio.Reader) (map[string]string, error) {
	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return headers, nil
		}
		if idx := strings.Index(line, ":"); idx != -1 {
			key := strings.ToLower(strings.TrimSpace(line[:idx]))
			val := strings.TrimSpace(line[idx+1:])
			headers[key] = val
		}
	}
}

func readRTSPBody(reader *bufio.Reader, headers map[string]string) (string, error) {
	cl := 0
	if v := strings.TrimSpace(headers["content-length"]); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &cl)
	}
	if cl <= 0 {
		return "", nil
	}

	body := make([]byte, cl)
	if _, err := io.ReadFull(reader, body); err != nil {
		return "", err
	}
	return string(body), nil
}

var rtspCodecLine = regexp.MustCompile(`(?im)^a=rtpmap:\d+\s+([^/\s]+)`)

func inferCodecFromSDP(sdp string) string {
	matches := rtspCodecLine.FindAllStringSubmatch(sdp, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(match[1])) {
		case "H264", "AVC":
			return "H264"
		case "H265", "HEVC":
			return "H265"
		}
	}
	return ""
}
