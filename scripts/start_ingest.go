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

	// Start Ingest for the camera
	cameraID := "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
	rtspURL := "rtsp://192.168.1.181:554/live/0/MAIN"

	fmt.Printf("Starting ingest for camera %s with URL %s\n", cameraID, rtspURL)

	req := &mediav1.StartIngestRequest{
		CameraId:  cameraID,
		RtspUrl:   rtspURL,
		PreferTcp: true,
	}

	resp, err := client.StartIngest(ctx, req)
	if err != nil {
		log.Fatalf("StartIngest failed: %v", err)
	}

	fmt.Printf("StartIngest response: %+v\n", resp)

	// Wait and check status
	time.Sleep(2 * time.Second)

	statusReq := &mediav1.GetIngestStatusRequest{
		CameraId: cameraID,
	}
	statusResp, err := client.GetIngestStatus(ctx, statusReq)
	if err != nil {
		log.Fatalf("GetIngestStatus failed: %v", err)
	}

	fmt.Printf("Ingest Status: Running=%v, SessionId=%s\n", statusResp.Running, statusResp.SessionId)
}
