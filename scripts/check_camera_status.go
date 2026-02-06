//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	mediav1 "github.com/technosupport/ts-vms/gen/go/media/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cameraID := os.Getenv("CAM_ID")
	if cameraID == "" {
		log.Fatal("CAM_ID environment variable required")
	}

	// Connect to Media Plane gRPC
	conn, err := grpc.NewClient("127.0.0.1:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to Media Plane: %v", err)
	}
	defer conn.Close()

	client := mediav1.NewMediaServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get camera status
	req := &mediav1.GetIngestStatusRequest{
		CameraId: cameraID,
	}

	resp, err := client.GetIngestStatus(ctx, req)
	if err != nil {
		fmt.Printf("  ✗ Camera NOT running: %v\n", err)
		return
	}

	if resp.Running {
		fmt.Printf("  ✓ Camera is RUNNING\n")
		fmt.Printf("  - State: %s\n", resp.State)
		fmt.Printf("  - FPS: %d\n", resp.Fps)
		fmt.Printf("  - Session ID: %s\n", resp.SessionId)
		fmt.Printf("  - Last Frame Age: %dms\n", resp.LastFrameAgeMs)
		if resp.HlsState != "" {
			fmt.Printf("  - HLS State: %s\n", resp.HlsState)
		}
	} else {
		fmt.Printf("  ✗ Camera is NOT running\n")
		fmt.Printf("  - State: %s\n", resp.State)
		if resp.RecentErrorCode != "" {
			fmt.Printf("  - Error: %s\n", resp.RecentErrorCode)
		}
	}
}
//go:build ignore


