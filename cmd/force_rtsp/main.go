package main

import (
	"flag"
	"fmt"
	"os"
)

// NOTE:
// This tool is intentionally minimal so it NEVER breaks the build.
// Later, you can extend it to call your Control Plane API to force-update a camera RTSP URL.

func main() {
	cameraID := flag.String("camera", "", "camera id (required)")
	rtspURL := flag.String("rtsp", "", "rtsp url (required)")
	flag.Parse()

	if *cameraID == "" || *rtspURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: force_rtsp --camera <id> --rtsp <rtsp_url>")
		os.Exit(2)
	}

	fmt.Printf("OK: request prepared (camera=%s rtsp=%s)\n", *cameraID, *rtspURL)
	fmt.Println("TODO: wire this to Control Plane endpoint (offline/local).")
}
