package cameras

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/media"
	"github.com/technosupport/ts-vms/internal/sfu"
)

type SfuService struct {
	sfuClient   *sfu.Client
	mediaClient *media.Client
	cameraRepo  Repository
	mediaRepo   *data.MediaModel
}

func NewSfuService(sfuClient *sfu.Client, mediaClient *media.Client, repo Repository, mediaRepo *data.MediaModel) *SfuService {
	return &SfuService{
		sfuClient:   sfuClient,
		mediaClient: mediaClient,
		cameraRepo:  repo,
		mediaRepo:   mediaRepo,
	}
}

func (s *SfuService) GetRtpCapabilities(ctx context.Context, tenantID, cameraID uuid.UUID) (json.RawMessage, error) {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	return s.sfuClient.GetRouterRtpCapabilities(ctx, roomID)
}

func (s *SfuService) JoinRoom(ctx context.Context, tenantID, cameraID uuid.UUID, sessionID string) (json.RawMessage, error) {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)

	// 1. Ensure Room exists in SFU
	if err := s.sfuClient.JoinRoom(ctx, roomID, sessionID); err != nil {
		return nil, fmt.Errorf("failed to join room: %w", err)
	}

	// 1.5 Ensure Media Plane Ingest is Running
	// Retrieve Camera IP to decide on Mock vs Real
	// We need to fetch camera first.
	// But `JoinRoom` signature has `tenantID, cameraID`.
	// We use `s.cameraRepo`.
	cam, err := s.cameraRepo.GetByID(ctx, cameraID)
	if err != nil {
		// Log error but maybe proceed if we assume it's running? NO, we need it running.
		return nil, fmt.Errorf("failed to fetch camera for ingest: %w", err)
	}

	// Fetch selection to get the real RTSP URL
	selection, err := s.mediaRepo.GetSelection(ctx, cameraID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stream selection: %w", err)
	}

	var rtspURL string
	if cam.IPAddress.String() == "127.0.0.1" {
		rtspURL = "mock://" + cameraID.String()
	} else if selection != nil && selection.MainRTSP != "" {
		rtspURL = selection.MainRTSP
	} else {
		// Fallback to basic construction if selection is missing
		rtspURL = fmt.Sprintf("rtsp://%s:%d/live", cam.IPAddress, cam.Port)
	}

	// Trigger Ingest (Idempotent)
	fmt.Printf("[DEBUG] Starting ingest for camera %s with URL: %s\n", cameraID, rtspURL)
	if err := s.mediaClient.StartIngest(ctx, cameraID.String(), rtspURL, true); err != nil {
		// If error is "Already Running" (not returned by grpc usually), it's fine.
		// MediaService returns OK if running.
		// Returns RESOURCE_EXHAUSTED if cap hit.
		return nil, fmt.Errorf("failed to start ingest: %w", err)
	}

	// 2. Prepare SFU for Media Plane Ingest
	ingest, err := s.sfuClient.PrepareIngest(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare SFU ingest: %w", err)
	}

	// 3. Command Media Plane to start RTP egress
	// We'll use the cameraID string. In a real system, we'd ensure the camera exists and belongs to the tenant.
	alreadyRunning, err := s.mediaClient.StartSfuRtpEgress(ctx, cameraID.String(), roomID, ingest.SSRC, ingest.PT, ingest.IP, ingest.Port)
	if err != nil {
		return nil, fmt.Errorf("failed to start Media Plane egress: %w", err)
	}
	if alreadyRunning {
		// Log or handle idempotency
	}

	// 4. Return SFU router capabilities (needed by client)
	return s.sfuClient.GetRouterRtpCapabilities(ctx, roomID)
}

func (s *SfuService) LeaveRoom(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)

	// 1. Stop Media Plane Egress
	if err := s.mediaClient.StopSfuRtpEgress(ctx, cameraID.String()); err != nil {
		// Log but proceed? Media Plane might have already stopped if camera was deleted.
	}

	// 2. Cleanup SFU
	return s.sfuClient.LeaveRoom(ctx, roomID)
}

// Signaling Relays

func (s *SfuService) CreateTransport(ctx context.Context, tenantID, cameraID uuid.UUID) (json.RawMessage, error) {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	return s.sfuClient.CreateWebRtcTransport(ctx, roomID)
}

func (s *SfuService) ConnectTransport(ctx context.Context, tenantID, cameraID, transportID string, params json.RawMessage) error {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	return s.sfuClient.ConnectWebRtcTransport(ctx, roomID, transportID, params)
}

func (s *SfuService) Consume(ctx context.Context, tenantID, cameraID, transportID string, rtpCaps json.RawMessage) (json.RawMessage, error) {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)

	// We need to find the producer ID for this room.
	// The SFU should manage this. For simplicity, we assume "video" producer is what we want.
	// In a multi-camera SFU, the producerID would be the Media Plane's producer.

	// Let's assume SFU has a "getProducerId" or we just pass roomID to the consume call.
	return s.sfuClient.Consume(ctx, roomID, transportID, "video-producer-id", rtpCaps)
}
