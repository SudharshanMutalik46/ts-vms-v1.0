package adapters

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestProbeRTSPUsesDescribe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	reqCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		reqCh <- line

		_, _ = fmt.Fprint(conn, "RTSP/1.0 404 Not Found\r\nCSeq: 1\r\n\r\n")
	}()

	rtspURL := fmt.Sprintf("rtsp://%s/ch01.264", ln.Addr().String())
	if err := ProbeRTSP(context.Background(), rtspURL); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}

	select {
	case req := <-reqCh:
		if !strings.HasPrefix(req, "DESCRIBE rtsp://") {
			t.Fatalf("expected DESCRIBE request, got %q", req)
		}
	case err := <-errCh:
		t.Fatalf("server error: %v", err)
	}
}

func TestProbeRTSPCodecParsesSdp(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}

		body := "v=0\r\nm=video 0 RTP/AVP 96\r\na=rtpmap:96 H265/90000\r\n"
		_, _ = fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: 1\r\nContent-Length: %d\r\nContent-Type: application/sdp\r\n\r\n%s", len(body), body)
	}()

	rtspURL := fmt.Sprintf("rtsp://%s/ch01.265", ln.Addr().String())
	codec, err := ProbeRTSPCodecWithTimeout(context.Background(), rtspURL, 2*time.Second)
	if err != nil {
		t.Fatalf("ProbeRTSPCodecWithTimeout failed: %v", err)
	}
	if codec != "H265" {
		t.Fatalf("ProbeRTSPCodecWithTimeout() = %q, want H265", codec)
	}
}
