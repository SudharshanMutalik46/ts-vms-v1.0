package live

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
)

type Service struct {
	Redis         *redis.Client
	CameraService *cameras.Service
	BaseURL       string
	HLSParams     HLSParams
}

type HLSParams struct {
	BaseURL string
}

const (
	SessionTTL        = 10 * time.Minute
	IdempotencyWindow = 10 * time.Second
)

func NewService(r *redis.Client, c *cameras.Service, baseUrl string, hlsParams HLSParams) *Service {
	return &Service{
		Redis:         r,
		CameraService: c,
		BaseURL:       baseUrl,
		HLSParams:     hlsParams,
	}
}

// StartLiveSession initiates a viewer session (idempotent)
func (s *Service) StartLiveSession(ctx context.Context, u *data.User, cameraID, viewMode, quality string) (*LiveSessionResponse, error) {
	// 1. Validate Camera Access (RBAC via service)
	_, err := s.CameraService.GetCamera(ctx, u.TenantID, cameraID)
	if err != nil {
		return nil, fmt.Errorf("camera access failed: %w", err)
	}

	// 2. Active Session Management (Limit 16)
	activeKey := fmt.Sprintf("live:active:%s:%s", u.TenantID, u.ID)

	// Scrubbing Logic: Verify existing members are actually alive
	members, err := s.Redis.SMembers(ctx, activeKey).Result()
	if err == nil {
		for _, sessID := range members {
			exists, _ := s.Redis.Exists(ctx, fmt.Sprintf("live:sess:%s", sessID)).Result()
			if exists == 0 {
				s.Redis.SRem(ctx, activeKey, sessID)
			}
		}
	}

	// Check Limit
	count, _ := s.Redis.SCard(ctx, activeKey).Result()
	if count >= 16 {
		// Stable JSON error payload expected by client
		// We return a specific error that the handler will map to 429 with JSON body
		return nil, fmt.Errorf("%s: limit=16 active=%d", ErrLiveLimitExceeded, count)
	}

	// 3. Check Idempotency (Prevent spam)
	// Key: live:idempotency:{user_id}:{camera_id} -> session_id
	idemKey := fmt.Sprintf("live:idempotency:%s:%s", u.ID.String(), cameraID)
	existingSessionID, err := s.Redis.Get(ctx, idemKey).Result()
	if err == nil && existingSessionID != "" {
		// Session exists and is recent - try to fetch it
		sessKey := fmt.Sprintf("live:sess:%s", existingSessionID)
		sessData, err := s.Redis.Get(ctx, sessKey).Result()
		if err == nil {
			var sess ViewerSession
			if err := json.Unmarshal([]byte(sessData), &sess); err == nil {
				return s.buildResponse(&sess, quality), nil
			}
		}
	}

	// 4. Create New Session
	sessionID := uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(SessionTTL)

	sess := &ViewerSession{
		ID:            sessionID,
		TenantID:      u.TenantID,
		UserID:        u.ID,
		CameraID:      cameraID,
		Mode:          "webrtc", // Start default
		CreatedAt:     now,
		LastSeenAt:    now,
		ExpiresAt:     expiresAt,
		FallbackCount: 0,
		LastError:     "",
	}

	// 5. Store in Redis
	sessJSON, _ := json.Marshal(sess)
	pipe := s.Redis.Pipeline()

	// Session record
	pipe.Set(ctx, fmt.Sprintf("live:sess:%s", sessionID), sessJSON, SessionTTL)

	// Idempotency key
	pipe.Set(ctx, idemKey, sessionID, IdempotencyWindow)

	// Active sessions index (Set)
	pipe.SAdd(ctx, activeKey, sessionID)
	pipe.Expire(ctx, activeKey, SessionTTL)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to store session: %w", err)
	}

	return s.buildResponse(sess, quality), nil
}

// --- Phase 3.8: Overlay & Detection ---

// DetectionPayload represents the AI service output
type DetectionPayload struct {
	CameraID string   `json:"camera_id"`
	TSUnixMS int64    `json:"ts_unix_ms"`
	AgeMS    int64    `json:"age_ms,omitempty"` // Computed on read
	Stream   string   `json:"stream"`           // "basic" or "weapon"
	Objects  []Object `json:"objects"`
}

type Object struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	BBox       BBox    `json:"bbox"`
}

type BBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// ValidLabels defines the 10-class basic + 3-class weapon enum
var ValidBasicLabels = map[string]bool{
	"person": true, "car": true, "truck": true, "bus": true, "motorcycle": true,
	"bicycle": true, "cat": true, "dog": true, "bird": true, "bag": true,
}
var ValidWeaponLabels = map[string]bool{
	"handgun": true, "rifle": true, "knife": true,
}

const (
	DetectionTTL     = 10 * time.Second
	MaxPayloadSize   = 8 * 1024 // 8KB
	MaxObjectsPerMsg = 50
	OverlayDemandTTL = 20 * time.Second
)

// ValidateDetection checks payload constraints per Phase 3.8 spec
func ValidateDetection(p *DetectionPayload) error {
	if len(p.Objects) > MaxObjectsPerMsg {
		return fmt.Errorf("too many objects: %d > %d", len(p.Objects), MaxObjectsPerMsg)
	}

	labelSet := ValidBasicLabels
	if p.Stream == "weapon" {
		labelSet = ValidWeaponLabels
	}

	for i, obj := range p.Objects {
		// Label enum check
		if !labelSet[obj.Label] {
			return fmt.Errorf("invalid label at index %d: %s", i, obj.Label)
		}
		// Confidence range
		if obj.Confidence < 0 || obj.Confidence > 1 {
			return fmt.Errorf("confidence out of range at index %d: %f", i, obj.Confidence)
		}
		// BBox validation: x,y,w,h ∈ [0..1], w>0, h>0, x+w≤1, y+h≤1
		b := obj.BBox
		if b.X < 0 || b.X > 1 || b.Y < 0 || b.Y > 1 {
			return fmt.Errorf("bbox x/y out of range at index %d", i)
		}
		if b.W <= 0 || b.H <= 0 {
			return fmt.Errorf("bbox w/h must be > 0 at index %d", i)
		}
		if b.X+b.W > 1 || b.Y+b.H > 1 {
			return fmt.Errorf("bbox exceeds bounds at index %d", i)
		}
	}
	return nil
}

// SaveDetection stores the latest detection for 10s with stream support
func (s *Service) SaveDetection(ctx context.Context, tenantID uuid.UUID, payload *DetectionPayload) error {
	if err := ValidateDetection(payload); err != nil {
		return err
	}
	// Key: det:latest:{tenant}:{camera}:{stream}
	stream := payload.Stream
	if stream == "" {
		stream = "basic"
	}
	key := fmt.Sprintf("det:latest:%s:%s:%s", tenantID.String(), payload.CameraID, stream)
	data, _ := json.Marshal(payload)
	return s.Redis.Set(ctx, key, data, DetectionTTL).Err()
}

// GetLatestDetection retrieves detection for client with age_ms
func (s *Service) GetLatestDetection(ctx context.Context, tenantID uuid.UUID, cameraID, stream string) (*DetectionPayload, error) {
	if stream == "" {
		stream = "basic"
	}
	key := fmt.Sprintf("det:latest:%s:%s:%s", tenantID.String(), cameraID, stream)
	data, err := s.Redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // 204 No Content equivalent
	}
	if err != nil {
		return nil, err
	}
	var payload DetectionPayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil, err
	}
	// Compute age_ms
	now := time.Now().UnixMilli()
	payload.AgeMS = now - payload.TSUnixMS
	return &payload, nil
}

// SetOverlayState toggles overlay for a specific session
func (s *Service) SetOverlayState(ctx context.Context, sessID string, enabled bool) error {
	// We store a flag in Redis? Or update the Session JSON?
	// Updating Session JSON is better for state persistence but heavier (R-M-W).
	// Lightweight Flag: live:sess:{id}:overlay -> "1"
	key := fmt.Sprintf("live:sess:%s:overlay", sessID)
	if enabled {
		return s.Redis.Set(ctx, key, "1", SessionTTL).Err()
	}
	return s.Redis.Del(ctx, key).Err()
}

// GetResult for internal overlay logic if needed?
// Or better: GetCamerasWithOverlayEnabled relies on checking ALL sessions... expensive to scan?
// Optimized: "Active Cameras" list.
// When Overlay Enabled -> Add cameraID to "live:active_overlay:{camera_id}" Set?
// No, simpler: AI Service asks "Which cameras have overlay enabled?".
// Control Plane can maintain a SET of "cameras_with_overlay".
// SADD when enabled, SREM when disabled?
// But SREM tricky if multiple users view same camera. Reference Counting?
// Redis doesn't do ref counting natively on Sets.
// SCORING via Sorted Set? Score = count?
// ZINCRBY 1 when enable, ZINCRBY -1 when disable. ZREM if <= 0.
// Let's use ZSET "live:overlay_demand". Member=camera_id, Score=subscriber_count.

func (s *Service) IncrementOverlayDemand(ctx context.Context, cameraID string) error {
	key := "live:overlay_demand"
	return s.Redis.ZIncrBy(ctx, key, 1.0, cameraID).Err()
}

func (s *Service) DecrementOverlayDemand(ctx context.Context, cameraID string) error {
	key := "live:overlay_demand"
	// Decrement
	res, err := s.Redis.ZIncrBy(ctx, key, -1.0, cameraID).Result()
	if err != nil {
		return err
	}
	// Cleanup if <= 0
	if res <= 0 {
		s.Redis.ZRem(ctx, key, cameraID)
	}
	return nil
}

// GetCamerasWithOverlayEnabled returns list for AI service
func (s *Service) GetCamerasWithOverlayEnabled(ctx context.Context) ([]string, error) {
	// Return all members of ZSET
	return s.Redis.ZRange(ctx, "live:overlay_demand", 0, -1).Result()
}

func (s *Service) buildResponse(sess *ViewerSession, requestedQuality string) *LiveSessionResponse {
	// Quality Selection Logic (Deterministic)
	// If quality="sub", we try to verify/use sub stream.
	// For now, if "sub" requested, we map to sub. Otherwise "main".
	// Implementation gap: We don't check if sub actually exists in DB here strictly for MVP.
	// But per plan: "If quality=sub, use sub... else main".

	selectedQuality := "main"
	if requestedQuality == "sub" {
		selectedQuality = "sub"
	}
	// Note: In real world, we'd check s.CameraMonitor.HasSubStream(sess.CameraID)

	// Mock HLS Token
	hlsToken := fmt.Sprintf("sub=%s&sid=%s&scope=hls&q=%s&sig=mock_sig", sess.CameraID, sess.ID, selectedQuality)

	hlsURL := fmt.Sprintf("%s/hls/live/%s/%s/index.m3u8?token=%s",
		s.HLSParams.BaseURL, sess.CameraID, sess.ID, hlsToken)

	// WebRTC Config
	sfuURL := fmt.Sprintf("%s/api/v1/sfu", s.BaseURL)

	return &LiveSessionResponse{
		ViewerSessionID: sess.ID,
		ExpiresAt:       sess.ExpiresAt.UnixMilli(),
		Primary:         "webrtc",
		Fallback:        "hls",
		SelectedQuality: selectedQuality,
		WebRTC: &WebRTCBlock{
			SFUURL:           sfuURL,
			RoomID:           sess.CameraID, // In Phase 3.7+ this might be mapped to "room_id_sub"
			ConnectTimeoutMs: 5000,
		},
		HLS: &HLSBlock{
			PlaylistURL:     hlsURL,
			TargetLatencyMs: 4000,
		},
		FallbackPolicy: &FallbackPolicy{
			WebRTCConnectTimeoutMs: 5000,
			WebRTCTrackTimeoutMs:   2000,
			MaxAutoRetries:         2,
			RetryBackoffMs:         []int{1000, 3000},
		},
		TelemetryPolicy: &TelemetryPolicy{
			Endpoint: "/api/v1/live/events",
		},
	}
}

// ResolveCameraTenant looks up the TenantID for a CameraID (used by Internal Ingest)
// We rely on repository lookup. Optimizable via cache.
func (s *Service) ResolveCameraTenant(ctx context.Context, cameraID string) (uuid.UUID, error) {
	uid, err := uuid.Parse(cameraID)
	if err != nil {
		return uuid.Nil, err
	}
	// We use GetByID without knowing Tenant. If Repo enforces Tenant, we can't use it easily?
	// But cameras.Repository GetByID takes ID. LiveService wrapper takes TenantID.
	// Cameras Repo interface: GetByID(ctx, id) (*Camera, error)
	cam, err := s.CameraService.GetByIDUnsafe(ctx, uid)
	if err != nil {
		return uuid.Nil, err
	}
	return cam.TenantID, nil
}

// SaveDetectionFromNATS stores detection with TTL (called by NATS subscription handler)
func (s *Service) SaveDetectionFromNATS(ctx context.Context, data []byte) error {
	// Size check
	if len(data) > MaxPayloadSize {
		return fmt.Errorf("payload too large: %d > %d", len(data), MaxPayloadSize)
	}

	var payload DetectionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	if err := ValidateDetection(&payload); err != nil {
		return err
	}

	// Resolve tenant from camera (required for multi-tenant storage)
	tenantID, err := s.ResolveCameraTenant(ctx, payload.CameraID)
	if err != nil {
		return fmt.Errorf("camera not found: %s", payload.CameraID)
	}

	// Store in Redis: det:latest:{tenant}:{camera}:{stream}
	stream := payload.Stream
	if stream == "" {
		stream = "basic"
	}
	key := fmt.Sprintf("det:latest:%s:%s:%s", tenantID.String(), payload.CameraID, stream)
	return s.Redis.Set(ctx, key, data, DetectionTTL).Err()
}

// ActiveCamera is returned by GetActiveCamerasForAI
type ActiveCamera struct {
	CameraID string `json:"camera_id"`
	TenantID string `json:"tenant_id"`
}

// GetActiveCamerasForAI returns cameras with overlay demand seen within 20s
func (s *Service) GetActiveCamerasForAI(ctx context.Context) ([]ActiveCamera, error) {
	// Use ZSET with timestamp scores for demand expiry
	// Key: overlay:demand (score = last_seen_unix_ms)
	key := "overlay:demand"

	// Get all cameras with score > (now - 20s)
	cutoff := float64(time.Now().Add(-OverlayDemandTTL).UnixMilli())
	results, err := s.Redis.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("%f", cutoff),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, err
	}

	cameras := make([]ActiveCamera, 0, len(results))
	for _, z := range results {
		camID := z.Member.(string)
		// Resolve tenant for each camera
		tenantID, err := s.ResolveCameraTenant(ctx, camID)
		if err != nil {
			continue // Skip invalid cameras
		}
		cameras = append(cameras, ActiveCamera{
			CameraID: camID,
			TenantID: tenantID.String(),
		})
	}

	return cameras, nil
}

// RefreshOverlayDemand updates demand timestamp for a camera
func (s *Service) RefreshOverlayDemand(ctx context.Context, cameraID string) error {
	key := "overlay:demand"
	score := float64(time.Now().UnixMilli())
	return s.Redis.ZAdd(ctx, key, redis.Z{Score: score, Member: cameraID}).Err()
}

// ClearOverlayDemand removes a camera from demand tracking
func (s *Service) ClearOverlayDemand(ctx context.Context, cameraID string) error {
	return s.Redis.ZRem(ctx, "overlay:demand", cameraID).Err()
}
