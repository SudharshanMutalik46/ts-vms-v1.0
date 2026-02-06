package media

import (
	"context"
	"fmt"

	mediav1 "github.com/technosupport/ts-vms/gen/go/media/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	conn   *grpc.ClientConn
	client mediav1.MediaServiceClient
}

func NewClient(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to dial media plane: %w", err)
	}

	return &Client{
		conn:   conn,
		client: mediav1.NewMediaServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) StartIngest(ctx context.Context, cameraID, rtspURL string, preferTCP bool) error {
	req := &mediav1.StartIngestRequest{
		CameraId:  cameraID,
		RtspUrl:   rtspURL,
		PreferTcp: preferTCP,
	}
	// Use Background context - ingest is a long-running operation that must NOT be
	// cancelled when the initiating HTTP request completes
	_, err := c.client.StartIngest(context.Background(), req)
	return err
}

func (c *Client) GetIngestStatus(ctx context.Context, cameraID string) (*mediav1.GetIngestStatusResponse, error) {
	req := &mediav1.GetIngestStatusRequest{
		CameraId: cameraID,
	}
	return c.client.GetIngestStatus(ctx, req)
}

func (c *Client) StartSfuRtpEgress(ctx context.Context, cameraID, roomID string, ssrc, pt uint32, dstIP string, dstPort int32) (bool, error) {
	req := &mediav1.StartSfuRtpEgressRequest{
		CameraId: cameraID,
		RoomId:   roomID,
		Ssrc:     ssrc,
		Pt:       pt,
		DstIp:    dstIP,
		DstPort:  dstPort,
	}

	// Use Background context - SFU egress is long-running and must persist beyond HTTP request
	resp, err := c.client.StartSfuRtpEgress(context.Background(), req)
	if err != nil {
		return false, err
	}

	return resp.AlreadyRunning, nil
}

func (c *Client) StopSfuRtpEgress(ctx context.Context, cameraID string) error {
	req := &mediav1.StopSfuRtpEgressRequest{
		CameraId: cameraID,
	}

	resp, err := c.client.StopSfuRtpEgress(ctx, req)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("failed to stop SFU egress in media plane")
	}

	return nil
}

func (c *Client) Health(ctx context.Context) (bool, string, error) {
	resp, err := c.client.Health(ctx, &mediav1.HealthRequest{})
	if err != nil {
		return false, "", err
	}
	return resp.Ok, resp.Status, nil
}

func (c *Client) GRPC() mediav1.MediaServiceClient {
	return c.client
}
