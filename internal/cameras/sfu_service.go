package cameras

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/media"
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

const ingestReadyProbeTimeout = 20 * time.Second
const ingestReadyProbeTimeoutH265 = 90 * time.Second
const sfuHealthProbeTimeout = 15 * time.Second
const sfuHealthProbeInterval = 500 * time.Millisecond
const ingestWarmupWindow = 15 * time.Second

func isHealthyIngestState(state string, lastFrameAgeMs int64, reconnectAttempts int32) bool {
	state = strings.ToUpper(strings.TrimSpace(state))
	if reconnectAttempts > 0 {
		return false
	}
	if state == "RUNNING" {
		return true
	}
	if state == "STARTING" || state == "RECONNECTING" {
		return lastFrameAgeMs < int64(ingestWarmupWindow/time.Millisecond)
	}
	return false
}

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

func (s *SfuService) ensureSfuHealthy(ctx context.Context, cameraID uuid.UUID) error {
	deadline := time.Now().Add(sfuHealthProbeTimeout)
	var lastErr error

	for {
		ok, err := s.sfuClient.Health(ctx)
		if err == nil && ok {
			fmt.Printf("[DEBUG] sfu_health: SFU reachable for camera=%s\n", cameraID)
			return nil
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("SFU is not healthy")
		}

		if time.Now().After(deadline) {
			return NewSfuError("sfu_health", "ERR_SFU_DOWN", "SFU health check failed", lastErr)
		}

		select {
		case <-ctx.Done():
			return NewSfuError("sfu_health", "ERR_SFU_DOWN", "SFU health check failed", ctx.Err())
		case <-time.After(sfuHealthProbeInterval):
		}
	}
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
		running := status.Running || isHealthyIngestState(state, status.GetLastFrameAgeMs(), status.GetReconnectAttempts())
		fmt.Printf("[DEBUG] ingest_ensure: initial check camera=%s running=%v state=%s lastFrameAgeMs=%d reconnectAttempts=%d\n",
			cameraID,
			running,
			state,
			status.GetLastFrameAgeMs(),
			status.GetReconnectAttempts())
		if running {
			return nil
		}
		if state == "RECONNECTING" || state == "STARTING" {
			fmt.Printf("[DEBUG] ingest_ensure: existing ingest is %s, will try fallback candidates for camera=%s\n", state, cameraID)
		}
	} else {
		fmt.Printf("[DEBUG] ingest_ensure: initial check camera=%s error=%v\n", cameraID, err)
	}

	var lastErr error
	for idx, candidate := range candidates {
		rtspURL := credentialize(candidate.url)
		fmt.Printf("[DEBUG] ingest_ensure: trying candidate %d/%d rtspURL=%s codec=%s target=%s for camera=%s\n", idx+1, len(candidates), rtspURL, candidate.codec, targetCodec, cameraID)

		fmt.Printf("[DEBUG] ingest_ensure: stopping existing ingest for camera=%s\n", cameraID)
		_ = s.mediaClient.StopIngest(context.Background(), cameraID.String())
		if err := s.mediaClient.StartIngest(ctx, cameraID.String(), rtspURL, true); err != nil {
			fmt.Printf("[DEBUG] ingest_ensure: StartIngest failed for camera=%s err=%v\n", cameraID, err)
			lastErr = err
			continue
		}

		fmt.Printf("[DEBUG] ingest_ensure: ingest started or refreshed for camera=%s codec=%s; SFU egress will queue until RUNNING\n",
			cameraID,
			targetCodec)
		return nil
	}

	return NewSfuError("ingest_ensure", "ERR_INGEST_NOT_READY", "Ingest did not start for WebRTC", lastErr)
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

	if err := s.ensureSfuHealthy(ctx, cameraID); err != nil {
		return nil, err
	}

	err := s.sfuClient.JoinRoom(ctx, roomID, sessionID)
	if err != nil {
		if err.Error() == "room at capacity" {
			return nil, NewSfuError("sfu_join", "ERR_ROOM_FULL", "Room at capacity", err)
		}
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "connection refused") ||
			strings.Contains(errText, "no connection could be made") ||
			strings.Contains(errText, "dial tcp") ||
			strings.Contains(errText, "timeout") {
			return nil, NewSfuError("sfu_join", "ERR_SFU_DOWN", "SFU join endpoint is not reachable", err)
		}

		fmt.Printf("[ERROR] sfu.JoinRoom failed for camera=%s tenant=%s err=%v\n", cameraID, tenantID, err)

		return nil, &SfuStepError{
			Step:           "sfu_join",
			ErrorCode:      "ERR_SFU_FAILURE",
			SafeMessage:    "SFU Join failed",
			RequiredAction: "Check SFU and Media Plane status",
			Err:            err,
		}
	}

	if err := s.ensureIngestForWebRtc(ctx, tenantID, cameraID, targetCodec); err != nil {
		// Ingest warm-up should not hard-fail the WebRTC join path. The media plane
		// already queues SFU egress until the pipeline is RUNNING, so we log the
		// condition and let the session continue.
		fmt.Printf("[WARN] media_prepare_ingest deferred for camera=%s target=%s err=%v\n", cameraID, targetCodec, err)
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
			errText := strings.ToLower(err.Error())
			if strings.Contains(errText, "connection refused") ||
				strings.Contains(errText, "no connection could be made") ||
				strings.Contains(errText, "dial tcp") ||
				strings.Contains(errText, "timeout") {
				return nil, NewSfuError("sfu_ingest_alloc", "ERR_SFU_DOWN", "SFU ingest endpoint is not reachable", err)
			}
			return nil, NewSfuError("sfu_ingest_alloc", "ERR_SFU_ALLOC", "SFU ingest alloc failed", err)
		}
	}

	var egressErr error
	for attempt := 1; attempt <= 3; attempt++ {
		_, egressErr = s.mediaClient.StartSfuRtpEgress(ctx, cameraID.String(), roomID, ingest.SSRC, ingest.PT, ingest.IP, ingest.Port, sfuIngestCodec)
		if egressErr == nil {
			break
		}
		fmt.Printf("[WARN] media_start_egress attempt %d failed for camera=%s room=%s err=%v\n", attempt, cameraID, roomID, egressErr)
		time.Sleep(time.Duration(attempt) * 250 * time.Millisecond)
	}
	if egressErr != nil {
		if h265Acquired {
			s.transcodePool.Release(cameraID.String())
		}
		fmt.Printf("[WARN] media_start_egress continuing without hard failure for camera=%s room=%s err=%v\n", cameraID, roomID, egressErr)
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
