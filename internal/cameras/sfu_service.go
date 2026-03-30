package cameras

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/media"
	"github.com/technosupport/ts-vms/internal/platform/paths"
	"github.com/technosupport/ts-vms/internal/sfu"
)

type SfuService struct {
	sfuClient     *sfu.Client
	mediaClient   *media.Client
	cameraRepo    Repository
	mediaRepo     *data.MediaModel
	credService   *CredentialService
	mediaSelector *MediaService
	transcodePool *WebRtcPool
}

const hlsReadyProbeTimeout = 8 * time.Second
const ingestReadyProbeTimeout = 5 * time.Second

func NewSfuService(sfuClient *sfu.Client, mediaClient *media.Client, repo Repository, mediaRepo *data.MediaModel, credService *CredentialService, mediaSelector *MediaService, pool *WebRtcPool) *SfuService {
	return &SfuService{
		sfuClient:     sfuClient,
		mediaClient:   mediaClient,
		cameraRepo:    repo,
		mediaRepo:     mediaRepo,
		credService:   credService,
		mediaSelector: mediaSelector,
		transcodePool: pool,
	}
}

func (s *SfuService) GetRtpCapabilities(ctx context.Context, tenantID, cameraID uuid.UUID) (json.RawMessage, error) {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	// Query SFU
	caps, err := s.sfuClient.GetRouterRtpCapabilities(ctx, roomID)
	if err != nil {
		return nil, NewSfuError("sfu_caps", "ERR_SFU_CAPS", "Failed to get router caps", err)
	}
	return caps, nil
}

func (s *SfuService) ensureIngestForWebRtc(ctx context.Context, tenantID, cameraID uuid.UUID, targetCodec string) error {
	targetCodec = strings.ToUpper(strings.TrimSpace(targetCodec))

	type ingestCandidate struct {
		url   string
		codec string
	}

	var candidates []ingestCandidate
	addCandidate := func(raw string, codec string) {
		raw = strings.TrimSpace(raw)
		codec = strings.ToUpper(strings.TrimSpace(codec))
		if raw == "" {
			return
		}
		for _, existing := range candidates {
			if existing.url == raw {
				return
			}
		}
		candidates = append(candidates, ingestCandidate{url: raw, codec: codec})
	}

	tx, err := s.mediaRepo.DB.BeginTx(ctx, nil)
	if err != nil {
		return NewSfuError("ingest_ensure", "ERR_DB_TX", "Failed to start DB transaction", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL app.tenant_id = '%s'", tenantID)); err != nil {
		return NewSfuError("ingest_ensure", "ERR_TENANT_CONTEXT_MISSING", "Failed to set tenant context", err)
	}

	cam, err := s.cameraRepo.GetByID(ctx, cameraID)
	if err != nil {
		return NewSfuError("ingest_ensure", "ERR_CAMERA_NOT_FOUND", "Camera not found", err)
	}

	var mainRTSP, subRTSP, mainCodec, subCodec string
	err = tx.QueryRowContext(ctx, `
		SELECT
			COALESCE(s.main_rtsp_url_sanitized, ''),
			COALESCE(s.sub_rtsp_url_sanitized, ''),
			COALESCE(mp.video_codec, ''),
			COALESCE(sp.video_codec, '')
		FROM camera_stream_selections s
		LEFT JOIN camera_media_profiles mp ON s.camera_id = mp.camera_id AND s.main_profile_token = mp.profile_token
		LEFT JOIN camera_media_profiles sp ON s.camera_id = sp.camera_id AND s.sub_profile_token = sp.profile_token
		WHERE s.camera_id = $1`, cameraID).Scan(&mainRTSP, &subRTSP, &mainCodec, &subCodec)
	if err != nil && err != sql.ErrNoRows {
		return NewSfuError("ingest_ensure", "ERR_DB_QUERY", "Failed to fetch media selection", err)
	}
	tx.Commit()

	if cam.IPAddress.String() == "127.0.0.1" || cam.IPAddress.String() == "localhost" {
		addCandidate("mock://"+cameraID.String(), "H264")
	} else {
		// Prefer a native source that matches the codec the SFU asked for.
		// Keep the legacy sub->main ordering as a fallback because ONVIF codec
		// metadata can be stale on some cameras.
		if targetCodec == "H264" {
			if subCodec == "H264" {
				addCandidate(subRTSP, subCodec)
			}
			if mainCodec == "H264" {
				addCandidate(mainRTSP, mainCodec)
			}
		} else if targetCodec == "H265" {
			if subCodec == "H265" {
				addCandidate(subRTSP, subCodec)
			}
			if mainCodec == "H265" {
				addCandidate(mainRTSP, mainCodec)
			}
		}

		addCandidate(subRTSP, subCodec)
		addCandidate(mainRTSP, mainCodec)
		addCandidate(cam.RtspUrl, "")
	}
	if len(candidates) == 0 {
		return NewSfuError("ingest_ensure", "ERR_NO_RTSP_URL", "No RTSP URL found in database (ONVIF or Manual required)", nil)
	}

	credentialize := func(rtspURL string) string {
		if strings.HasPrefix(rtspURL, "mock://") {
			return rtspURL
		}
		rtspURL = html.UnescapeString(rtspURL)
		if s.credService == nil {
			return rtspURL
		}
		if creds, found, err := s.credService.GetCredentials(ctx, tenantID, cameraID, true); err == nil && found && creds.Data != nil {
			u := creds.Data.Username
			p := creds.Data.Password
			if u != "" && !strings.Contains(rtspURL, "@") {
				return fmt.Sprintf("rtsp://%s:%s@%s",
					strings.ReplaceAll(u, "@", "%40"),
					strings.ReplaceAll(p, "@", "%40"),
					strings.TrimPrefix(rtspURL, "rtsp://"))
			}
		}
		return rtspURL
	}


	status, err := s.mediaClient.GetIngestStatus(ctx, cameraID.String())
	if err == nil {
		state := strings.ToUpper(strings.TrimSpace(status.GetState()))
		running := status.Running || state == "STARTING" || state == "RECONNECTING"
		if running {
			return nil
		}
	}

	var lastErr error
	for idx, candidate := range candidates {
		rtspURL := credentialize(candidate.url)
		fmt.Printf("[DEBUG] ingest_ensure: trying candidate %d/%d rtspURL=%s codec=%s target=%s for camera=%s\n", idx+1, len(candidates), rtspURL, candidate.codec, targetCodec, cameraID)
		_ = s.mediaClient.StopIngest(context.Background(), cameraID.String())
		if err := s.mediaClient.StartIngest(ctx, cameraID.String(), rtspURL, true); err != nil {
			lastErr = err
			continue
		}

		deadline := time.Now().Add(ingestReadyProbeTimeout)
		for time.Now().Before(deadline) {
			resp, err := s.mediaClient.GetIngestStatus(ctx, cameraID.String())
			if err == nil {
				state := strings.ToUpper(strings.TrimSpace(resp.GetState()))
				if resp.Running || state == "STARTING" || state == "RECONNECTING" {
					return nil
				}
			}
			time.Sleep(250 * time.Millisecond)
		}
		lastErr = fmt.Errorf("ingest did not reach RUNNING state")
	}

	return NewSfuError("ingest_ensure", "ERR_INGEST_NOT_READY", "Ingest did not start for WebRTC", lastErr)
}

// EnsureHlsSession ensures that the media plane ingest is running and returns the active HLS session ID and playlist URL.
func (s *SfuService) EnsureHlsSession(ctx context.Context, tenantID, cameraID uuid.UUID) (string, string, error) {
	// Task B.3: Ensure tenant context for RLS
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

	// Refactored Selection Fetch with RLS — read both main and sub with codecs
	var mainRTSP, subRTSP, mainCodec, subCodec string
	err = tx.QueryRowContext(ctx, `
		SELECT
			COALESCE(s.main_rtsp_url_sanitized, ''),
			COALESCE(s.sub_rtsp_url_sanitized, ''),
			COALESCE(mp.video_codec, ''),
			COALESCE(sp.video_codec, '')
		FROM camera_stream_selections s
		LEFT JOIN camera_media_profiles mp ON s.camera_id = mp.camera_id AND s.main_profile_token = mp.profile_token
		LEFT JOIN camera_media_profiles sp ON s.camera_id = sp.camera_id AND s.sub_profile_token = sp.profile_token
		WHERE s.camera_id = $1`, cameraID).Scan(&mainRTSP, &subRTSP, &mainCodec, &subCodec)

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

	var candidateURLs []string
	addCandidate := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range candidateURLs {
			if existing == raw {
				return
			}
		}
		candidateURLs = append(candidateURLs, raw)
	}

	if cam.IPAddress.String() == "127.0.0.1" || cam.IPAddress.String() == "localhost" {
		addCandidate("mock://" + cameraID.String())
	} else if err == nil && (mainRTSP != "" || subRTSP != "") {
		// Prefer the profile that claims H264, but keep the alternate profile as a
		// fallback because some cameras advertise stale codec metadata.
		if subCodec == "H264" {
			addCandidate(subRTSP)
		}
		if mainCodec == "H264" {
			addCandidate(mainRTSP)
		}
		addCandidate(subRTSP)
		addCandidate(mainRTSP)
		if len(candidateURLs) > 0 {
			fmt.Printf("[DEBUG] hls_ensure: candidate streams for camera=%s main=%s sub=%s count=%d\n", cameraID, mainCodec, subCodec, len(candidateURLs))
		}
	} else if cam.RtspUrl != "" {
		addCandidate(cam.RtspUrl)
	} else {
		// NO hard-coded path fallbacks as requested
		return "", "", NewSfuError("hls_ensure", "ERR_NO_RTSP_URL", "No RTSP URL found in database (ONVIF or Manual required)", nil)
	}
	tx.Commit() // Done with DB

	credentialize := func(rtspURL string) string {
		if strings.HasPrefix(rtspURL, "mock://") {
			return rtspURL
		}
		rtspURL = html.UnescapeString(rtspURL)
		if s.credService == nil {
			return rtspURL
		}
		if creds, found, err := s.credService.GetCredentials(ctx, tenantID, cameraID, true); err == nil && found && creds.Data != nil {
			u := creds.Data.Username
			p := creds.Data.Password
			if u != "" && !strings.Contains(rtspURL, "@") {
				return fmt.Sprintf("rtsp://%s:%s@%s",
					strings.ReplaceAll(u, "@", "%40"),
					strings.ReplaceAll(p, "@", "%40"),
					strings.TrimPrefix(rtspURL, "rtsp://"))
			}
		}
		return rtspURL
	}

	var lastErr error
	for idx, candidate := range candidateURLs {
		rtspURL := credentialize(candidate)
		fmt.Printf("[DEBUG] hls_ensure: trying candidate %d/%d rtspURL=%s for camera=%s\n", idx+1, len(candidateURLs), rtspURL, cameraID)

		// Stop any stale ingest first so media plane picks up the selected URL.
		_ = s.mediaClient.StopIngest(context.Background(), cameraID.String())

		err = s.mediaClient.StartIngest(ctx, cameraID.String(), rtspURL, true)
		if err != nil {
			lastErr = NewSfuError("hls_ensure", "ERR_INGEST_FAILED", "Failed to start ingest", err)
			continue
		}

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := s.mediaClient.GetIngestStatus(ctx, cameraID.String())
			if err == nil && resp.GetHlsState() == "DEGRADED" {
				lastErr = NewSfuError("hls_ensure", "ERR_HLS_UNSUPPORTED_CODEC", resp.GetRecentErrorCode(), nil)
				fmt.Printf("[DEBUG] hls_ensure: candidate %d/%d degraded for camera=%s reason=%s\n", idx+1, len(candidateURLs), cameraID, resp.GetRecentErrorCode())
				break
			}
			if err == nil && resp.Running && resp.SessionId != "" {
				if s.waitForHlsSegments(ctx, cameraID, resp.SessionId) {
					playlistURL := fmt.Sprintf("/hls/live/%s/%s/%s/playlist.m3u8", tenantID, cameraID, resp.SessionId)
					return resp.SessionId, playlistURL, nil
				}
				lastErr = NewSfuError("hls_ensure", "ERR_HLS_NOT_READY", "HLS session started but first segment is not ready", nil)
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	if lastErr != nil {
		return "", "", lastErr
	}
	fmt.Printf("[REQ:unknown] hls_ensure failed code=ERR_HLS_NOT_READY camera=%s tenant=%s err=timeout\n", cameraID, tenantID)
	return "", "", NewSfuError("hls_ensure", "ERR_HLS_NOT_READY", "HLS session not ready after timeout", nil)
}

func (s *SfuService) waitForHlsSegments(ctx context.Context, cameraID uuid.UUID, sessionID string) bool {
	deadline := time.Now().Add(hlsReadyProbeTimeout)
	segmentDir := filepath.Join(paths.ResolveDataRoot(), "hls", "live", cameraID.String(), sessionID)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		entries, err := os.ReadDir(segmentDir)
		if err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if !entry.IsDir() && strings.HasPrefix(name, "segment_") && strings.HasSuffix(name, ".mp4") {
					return true
				}
			}
		}

		time.Sleep(250 * time.Millisecond)
	}

	return false
}

func (s *SfuService) JoinRoom(ctx context.Context, tenantID, cameraID uuid.UUID, sessionID string, codecPreferences []string) (json.RawMessage, error) {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	prefs := normalizeCodecPrefs(codecPreferences)
	targetCodec := "H264"
	for _, c := range prefs {
		if c == "H265" {
			targetCodec = "H265"
			break
		}
	}

	getHlsFallback := func() (string, bool) {
		_, playlistURL, hlsErr := s.EnsureHlsSession(ctx, tenantID, cameraID)
		if hlsErr == nil && strings.TrimSpace(playlistURL) != "" {
			return playlistURL, true
		}
		var sfuErr *SfuStepError
		if errors.As(hlsErr, &sfuErr) {
			fmt.Printf("[DEBUG] JoinRoom: HLS fallback unavailable for camera=%s code=%s err=%v\n", cameraID, sfuErr.ErrorCode, hlsErr)
		} else if hlsErr != nil {
			fmt.Printf("[DEBUG] JoinRoom: HLS fallback unavailable for camera=%s err=%v\n", cameraID, hlsErr)
		}
		return "", false
	}

	err := s.sfuClient.JoinRoom(ctx, roomID, sessionID)
	if err != nil {
		if err.Error() == "room at capacity" {
			return nil, NewSfuError("sfu_join", "ERR_ROOM_FULL", "Room at capacity", err)
		}
		if playlistURL, ok := getHlsFallback(); ok {
			sfErr := NewSfuErrorWithFallback("sfu_join", "ERR_SFU_FAILURE", "SFU Join failed, use HLS", playlistURL, err)
			sfErr.RequiredAction = "Switch to HLS player"
			return nil, sfErr
		}
		return nil, &SfuStepError{
			Step:           "sfu_join",
			ErrorCode:      "ERR_SFU_FAILURE",
			SafeMessage:    "SFU Join failed",
			RequiredAction: "Check SFU and Media Plane status",
			Err:            err,
		}
	}

	// SFU Ingest Allocation with H.265 Transcode Fallback
	var ingest *sfu.IngestResponse
	h265Acquired := false
	sfuIngestCodec := "H264" // mediasoup H265 WebRTC not universally supported; media plane transcodes

	if targetCodec == "H265" {
		if err := s.transcodePool.Acquire(ctx, cameraID.String()); err == nil {
			h265Acquired = true
		} else {
			fmt.Printf("[JoinRoom] H265 transcode pool full for camera=%s, falling back to H264\n", cameraID)
			targetCodec = "H264"
		}
	}

	ingest, err = s.sfuClient.PrepareIngest(ctx, roomID, sfuIngestCodec)
	if err != nil {
		// Detect H265_NOT_SUPPORTED from SFU
		if targetCodec == "H265" && strings.Contains(err.Error(), "501") {
			fmt.Printf("[JoinRoom] SFU does not support H265, retrying with H264 for camera=%s\n", cameraID)
			if h265Acquired {
				s.transcodePool.Release(cameraID.String())
				h265Acquired = false
			}
			targetCodec = "H264"
			ingest, err = s.sfuClient.PrepareIngest(ctx, roomID, sfuIngestCodec)
		}

		if err != nil {
			if playlistURL, ok := getHlsFallback(); ok {
				return nil, NewSfuErrorWithFallback("sfu_ingest_alloc", "ERR_SFU_ALLOC", "SFU ingest alloc failed", playlistURL, err)
			}
			return nil, NewSfuError("sfu_ingest_alloc", "ERR_SFU_ALLOC", "SFU ingest alloc failed", err)
		}
	}

	if err := s.ensureIngestForWebRtc(ctx, tenantID, cameraID, targetCodec); err != nil {
		if h265Acquired {
			s.transcodePool.Release(cameraID.String())
		}
		if playlistURL, ok := getHlsFallback(); ok {
			return nil, NewSfuErrorWithFallback("media_prepare_ingest", "ERR_MEDIA_INGEST", "Media ingest not ready for WebRTC", playlistURL, err)
		}
		return nil, NewSfuError("media_prepare_ingest", "ERR_MEDIA_INGEST", "Media ingest not ready for WebRTC", err)
	}

	_, err = s.mediaClient.StartSfuRtpEgress(ctx, cameraID.String(), roomID, ingest.SSRC, ingest.PT, ingest.IP, ingest.Port, sfuIngestCodec)
	if err != nil {
		if h265Acquired {
			s.transcodePool.Release(cameraID.String())
		}
		if playlistURL, ok := getHlsFallback(); ok {
			return nil, NewSfuErrorWithFallback("media_start_egress", "ERR_MEDIA_EGRESS", "Media egress failed", playlistURL, err)
		}
		return nil, NewSfuError("media_start_egress", "ERR_MEDIA_EGRESS", "Media egress failed", err)
	}

	caps, err := s.sfuClient.GetRouterRtpCapabilities(ctx, roomID)
	if err != nil {
		return nil, NewSfuError("sfu_caps", "ERR_SFU_CAPS", "Failed to get router caps", err)
	}
	return caps, nil
}

func (s *SfuService) LeaveRoom(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	_ = s.mediaClient.StopSfuRtpEgress(ctx, cameraID.String())
	s.transcodePool.Release(cameraID.String())
	return s.sfuClient.LeaveRoom(ctx, roomID)
}

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
	producerKey := roomID + ":video"
	return s.sfuClient.Consume(ctx, roomID, transportID, producerKey, rtpCaps)
}

func (s *SfuService) ResumeConsumer(ctx context.Context, tenantID, cameraID, transportID, consumerID string) error {
	roomID := fmt.Sprintf("%s:%s", tenantID, cameraID)
	return s.sfuClient.ResumeConsumer(ctx, roomID, transportID, consumerID)
}

func normalizeCodecPrefs(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		s := strings.ToUpper(strings.TrimSpace(v))
		if s == "HEVC" {
			s = "H265"
		}
		if s != "H265" && s != "H264" {
			continue
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = []string{"H264"}
	}
	return out
}
