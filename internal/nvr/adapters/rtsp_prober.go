package adapters

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
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
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	u, err := url.Parse(rtspURL)
	if err != nil {
		return fmt.Errorf("invalid url: %v", err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":554" // Default RTSP port
	}

	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return err
	}
	defer conn.Close()

	// RTSP DESCRIBE is a better signal than OPTIONS for whether the media
	// path itself exists and can produce SDP.
	// CSeq: 1
	// User-Agent: TS-VMS-Health
	msg := fmt.Sprintf("DESCRIBE %s RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: TS-VMS-Health\r\nAccept: application/sdp\r\n\r\n", rtspURL)

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}

	if _, err := conn.Write([]byte(msg)); err != nil {
		return err
	}

	// Read Response
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return err
	}

	// Expect "RTSP/1.0 200 OK"
	parts := strings.Fields(statusLine)
	if len(parts) < 2 {
		return fmt.Errorf("malformed response: %s", statusLine)
	}

	code := parts[1]
	if code == "401" || code == "403" {
		return fmt.Errorf("auth_failed: %s", code)
	}
	if !strings.HasPrefix(code, "2") {
		return fmt.Errorf("stream_error: %s", code)
	}

	return nil
}
