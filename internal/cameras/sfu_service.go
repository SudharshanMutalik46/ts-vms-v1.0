package cameras

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
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
	caps, err := s.sfuClient.GetRouterRtpCapabilities(ctx, roomID)
	if err != nil {
		return nil, NewSfuError("sfu_reachable", "ERR_SFU_UNREACHABLE", "Failed to reach SFU", err)
	}
	return caps, nil
}

// EnsureHlsSession ensures that the media plane ingest is running and returns the active HLS session ID and playlist URL.
func (s *SfuService) EnsureHlsSession(ctx context.Context, tenantID, cameraID uuid.UUID) (string, string, error) {
	// Task B.3: Ensure tenant context for RLS
	// We need a transaction to ensure SET LOCAL persists for subsequent queries on the same connection.
	tx, err := s.mediaRepo.DB.BeginTx(ctx, nil)
	if err != nil {
		fmt.Printf("[REQ:unknown] hls_ensure failed code=ERR_DB_TX camera=%s tenant=%s err=%v\n", cameraID, tenantID, err)
		return "", "", NewSfuError("hls_ensure", "ERR_DB_TX", "Failed to start DB transaction", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL app.tenant_id = '%s'", tenantID))
	if err != nil {
		fmt.Printf("[REQ:unknown] hls_ensure failed code=ERR_TENANT_CONTEXT_MISSING camera=%s tenant=%s err=%v\n", cameraID, tenantID, err)
		return "", "", &SfuStepError{
			Step:           "hls_ensure",
			ErrorCode:      "ERR_TENANT_CONTEXT_MISSING",
			SafeMessage:    "Failed to set tenant context",
			RequiredAction: "Ensure tenant context is set for DB session / RLS",
			Err:            err,
		}
	}

	// 1. Get Camera and RTSP URL
	cam, err := s.cameraRepo.GetByID(ctx, cameraID)
	if err != nil {
		fmt.Printf("[REQ:unknown] hls_ensure failed code=ERR_CAMERA_NOT_FOUND camera=%s tenant=%s err=%v\n", cameraID, tenantID, err)
		return "", "", NewSfuError("hls_ensure", "ERR_CAMERA_NOT_FOUND", "Camera not found", err)
	}

	// Task B.1: Handle DB Selection Errors
	// For Phase 3.4, I'll inline the tx-scoped selection fetch or update MediaModel?
	// The requirement is specific about RLS.

	// Refactored Selection Fetch with RLS
	var mainRTSP string
	err = tx.QueryRowContext(ctx, "SELECT main_rtsp_url_sanitized FROM camera_stream_selections WHERE camera_id = $1", cameraID).Scan(&mainRTSP)

	sqlState := "unknown"
	if pqErr, ok := err.(*pq.Error); ok {
		sqlState = string(pqErr.Code)
	}

	if err != nil && err != sql.ErrNoRows {
		fmt.Printf("[REQ:unknown] hls_ensure failed code=ERR_DB_QUERY sqlstate=%s camera=%s tenant=%s err=%v\n", sqlState, cameraID, tenantID, err)
		return "", "", &SfuStepError{
			Step:           "hls_ensure",
			ErrorCode:      "ERR_DB_QUERY",
			SafeMessage:    "Failed to fetch media selection",
			RequiredAction: "Check DB connectivity and RLS policies",
			Err:            err,
		}
	}

	var rtspURL string
	if cam.IPAddress.String() == "127.0.0.1" {
		rtspURL = "mock://" + cameraID.String()
	} else if err == nil && mainRTSP != "" {
		rtspURL = mainRTSP
	} else {
		// Task B.1: Default selection
		// prefer H.264 stream for live if available -> we don't have profile info here easily without another query.
		// For now, choose "main" (which our default RTSP pattern represents).
		rtspURL = fmt.Sprintf("rtsp://%s:%d/live", cam.IPAddress, cam.Port)
	}

	fmt.Printf("[DEBUG] hls_ensure: selected rtspURL=%s for camera=%s\n", rtspURL, cameraID)
	tx.Commit() // Done with DB

	// 2. Check Status logic (Poll Loop optimization)
	status, err := s.mediaClient.GetIngestStatus(ctx, cameraID.String())
	if err == nil && status.Running && status.SessionId != "" {
		playlistURL := fmt.Sprintf("/hls/live/%s/%s/%s/playlist.m3u8", tenantID, cameraID, status.SessionId)
		return status.SessionId, playlistURL, nil
	}

	// 3. Trigger Start
	err = s.mediaClient.StartIngest(ctx, cameraID.String(), rtspURL, true)
	if err != nil {
		fmt.Printf("[REQ:unknown] hls_ensure failed code=ERR_INGEST_FAILED camera=%s tenant=%s err=%v\n", cameraID, tenantID, err)
		return "", "", NewSfuError("hls_ensure", "ERR_INGEST_FAILED", "Failed to start ingest", err)
	}

	// 4. Poll for Session ID (max 5s)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := s.mediaClient.GetIngestStatus(ctx, cameraID.String())
		if err == nil && resp.Running && resp.SessionId != "" {
			playlistURL := fmt.Sprintf("/hls/live/%s/%s/%s/playlist.m3u8", tenantID, cameraID, resp.SessionId)
			return resp.SessionId, playlistURL, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("[REQ:unknown] hls_ensure failed code=ERR_HLS_NOT_READY camera=%s tenant=%s err=timeout\n", cameraID, tenantID)
	return "", "", NewSfuError("hls_ensure", "ERR_HLS_NOT_READY", "HLS session not ready after timeout", nil)
}

func (s *SfuService) JoinRoom(ctx context.Context, tenantID, cameraID uuid.UUID, sessionID string) (json.RawMessage, error) {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	fmt.Printf("[DEBUG] JoinRoom: roomID=%s, sessionID=%s\n", roomID, sessionID)

	// Task A: Make hls_ensure NON-BLOCKING.
	// We attempt SFU Join FIRST.

	// Task A (Phase 3.4): Enforce H.264 Only.
	// We check the codec of the main main profile.
	codec, err := s.checkCodec(ctx, tenantID, cameraID)
	if err == nil && codec != "" && codec != "H264" {
		fmt.Printf("[DEBUG] JoinRoom: Codec is %s (not H264). Forcing HLS Fallback.\n", codec)
		_, playlistURL, hlsErr := s.EnsureHlsSession(ctx, tenantID, cameraID)
		if hlsErr != nil {
			return nil, NewSfuError("codec_check", "ERR_UNSUPPORTED_CODEC", fmt.Sprintf("Codec %s not supported for WebRTC (and HLS failed)", codec), hlsErr)
		}
		// Return Error with Fallback
		sfErr := NewSfuErrorWithFallback("codec_check", "ERR_UNSUPPORTED_CODEC", fmt.Sprintf("Codec %s not supported for WebRTC. Use HLS.", codec), playlistURL, nil)
		sfErr.RequiredAction = "Switch to HLS player"
		return nil, sfErr
	}

	// 0. Proactively ensure ingestion is running (needed for SFU egress)
	_, _, err = s.EnsureHlsSession(ctx, tenantID, cameraID)
	if err != nil {
		fmt.Printf("[DEBUG] JoinRoom: EnsureHlsSession failed for camera=%s: %v\n", cameraID, err)
		// We proceed anyway, but this usually means StartSfuRtpEgress will fail later.
		// However, it gives the ingestion 1-2 seconds to warm up during the SFU join.
	} else {
		fmt.Printf("[DEBUG] JoinRoom: EnsureHlsSession OK for camera=%s\n", cameraID)
	}

	// 1. Ensure Room exists in SFU
	err = s.sfuClient.JoinRoom(ctx, roomID, sessionID)
	if err != nil {
		fmt.Printf("[DEBUG] JoinRoom: SFU JoinRoom failed: %v\n", err)
		if err.Error() == "room at capacity" {
			return nil, NewSfuError("sfu_join", "ERR_ROOM_FULL", "Room at capacity", err)
		}

		// SFU Failed. NOW we try EnsureHlsSession to provide a fallback.
		// Result is ignored if it fails, we just want the URL if possible.
		_, playlistURL, hlsErr := s.EnsureHlsSession(ctx, tenantID, cameraID)
		if hlsErr != nil {
			// Both failed. Return SFU error WITHOUT fallback hint.
			// Task A: "If EnsureHlsSession() fails: ... Return Join error for the WebRTC failing step, and include fallback_hint: null plus required_action"
			return nil, &SfuStepError{
				Step:           "sfu_join",
				ErrorCode:      "ERR_SFU_FAILURE",
				SafeMessage:    "SFU Join failed (and HLS unavailable)",
				RequiredAction: "Check SFU and Media Plane status",
				Err:            err,
			}
		}

		// Return SFU error WITH fallback
		sfErr := NewSfuErrorWithFallback("sfu_join", "ERR_SFU_FAILURE", "SFU Join failed, use HLS", playlistURL, err)
		sfErr.RequiredAction = "Switch to HLS player"
		return nil, sfErr
	}

	// SFU Join OK.
	fmt.Printf("[DEBUG] JoinRoom: SFU JoinRoom OK\n")

	// 2. Prepare SFU for Media Plane Ingest
	ingest, err := s.sfuClient.PrepareIngest(ctx, roomID)
	if err != nil {
		fmt.Printf("[DEBUG] JoinRoom: PrepareIngest failed: %v\n", err)
		// If this fails, we again might want HLS fallback.
		_, playlistURL, _ := s.EnsureHlsSession(ctx, tenantID, cameraID)
		return nil, NewSfuErrorWithFallback("sfu_ingest_alloc", "ERR_SFU_ALLOC", "SFU ingest alloc failed", playlistURL, err)
	}

	// 3. Command Media Plane to start RTP egress
	fmt.Printf("[DEBUG] JoinRoom: PrepareIngest OK: IP=%s, Port=%d, SSRC=%d, PT=%d\n", ingest.IP, ingest.Port, ingest.SSRC, ingest.PT)

	// Note: We might want to run EnsureHlsSession in background here too if we want "seamless" switch later?
	// But requirements say "Only call ... when WebRTC fails".
	if s.mediaClient == nil {
		fmt.Printf("[DEBUG] JoinRoom: mediaClient is nil\n")
		_, playlistURL, _ := s.EnsureHlsSession(ctx, tenantID, cameraID)
		return nil, NewSfuErrorWithFallback("media_start_egress", "ERR_MEDIA_CLIENT_NIL", "Media client not initialized", playlistURL, nil)
	}
	alreadyRunning, err := s.mediaClient.StartSfuRtpEgress(ctx, cameraID.String(), roomID, ingest.SSRC, ingest.PT, ingest.IP, ingest.Port)
	if err != nil {
		_, playlistURL, _ := s.EnsureHlsSession(ctx, tenantID, cameraID)
		return nil, NewSfuErrorWithFallback("media_start_egress", "ERR_MEDIA_EGRESS", "Media egress failed", playlistURL, err)
	}
	if alreadyRunning {
		// OK
	}

	// 4. Return SFU router capabilities
	caps, err := s.sfuClient.GetRouterRtpCapabilities(ctx, roomID)
	if err != nil {
		return nil, NewSfuError("sfu_caps", "ERR_SFU_CAPS", "Failed to get router caps", err)
	}

	return caps, nil
}

func (s *SfuService) LeaveRoom(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)

	// 1. Stop Media Plane Egress
	if err := s.mediaClient.StopSfuRtpEgress(ctx, cameraID.String()); err != nil {
		// Log but proceed
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
	// For Mediasoup logic, the producer ID should ideally come from the SFU or mapping.
	// We'll pass an empty producerId if not found, let the SFU decide or use its room mapping.
	// Actually, Mediasoup's Consume REST API in main.ts currently doesn't even take producerId as a param,
	// it expects it to be determined by the manager.
	// So we'll pass an empty string or the key used in the manager map.
	producerKey := roomID + ":video"
	return s.sfuClient.Consume(ctx, roomID, transportID, producerKey, rtpCaps)
}

func (s *SfuService) checkCodec(ctx context.Context, tenantID, cameraID uuid.UUID) (string, error) {
	// We need a transaction to ensure RLS context (SET LOCAL) is respected
	tx, err := s.mediaRepo.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 1. Set RLS
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL app.tenant_id = '%s'", tenantID)); err != nil {
		return "", err
	}

	// 2. Query Codec from Selection + Profile
	// We trust that SelectMediaProfiles has populated these tables.
	query := `
		SELECT p.video_codec 
		FROM camera_stream_selections s
		JOIN camera_media_profiles p ON s.camera_id = p.camera_id AND s.main_profile_token = p.profile_token
		WHERE s.camera_id = $1
	`
	var codec string
	err = tx.QueryRowContext(ctx, query, cameraID).Scan(&codec)
	if err == sql.ErrNoRows {
		return "", nil // Unknown/No Selection
	}
	if err != nil {
		return "", err
	}

	return codec, nil
}
