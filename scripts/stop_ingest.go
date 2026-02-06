package main

import (
	"context"
	"fmt"
	"log"
	"time"

	mediav1 "github.com/technosupport/ts-vms/gen/go/media/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Connect to Media Plane gRPC
	conn, err := grpc.NewClient("127.0.0.1:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to Media Plane: %v", err)
	}
	defer conn.Close()

	client := mediav1.NewMediaServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop Ingest for the camera
	cameraID := "e1855a70-233d-4ead-a3dd-4a8b670c00e9"

	fmt.Printf("Stopping ingest for camera %s\n", cameraID)

	req := &mediav1.StopIngestRequest{
		CameraId: cameraID,
	}

	resp, err := client.StopIngest(ctx, req)
	if err != nil {
		log.Fatalf("StopIngest failed: %v", err)
	}

	fmt.Printf("StopIngest response: %+v\n", resp)
}
