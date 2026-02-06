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
		cameraID = "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
	}

	// Connect to Media Plane gRPC
	conn, err := grpc.NewClient("127.0.0.1:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to Media Plane: %v", err)
	}
	defer conn.Close()

	client := mediav1.NewMediaServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start SFU Egress
	fmt.Printf("Starting SFU egress for camera %s\n", cameraID)

	req := &mediav1.StartSfuEgressRequest{
		CameraId: cameraID,
	}

	resp, err := client.StartSfuEgress(ctx, req)
	if err != nil {
		log.Fatalf("StartSfuEgress failed: %v", err)
	}

	fmt.Printf("SFU Egress started successfully!\n")
	fmt.Printf("Response: %+v\n", resp)
}
//go:build ignore


