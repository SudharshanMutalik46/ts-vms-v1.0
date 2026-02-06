package live

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/license"
)

// Mock Repo
type dummyRepo struct{}

func (d *dummyRepo) GetAll(ctx context.Context, tenantID uuid.UUID) ([]*data.Camera, error) {
	return nil, nil
}
func (d *dummyRepo) GetByID(ctx context.Context, id uuid.UUID) (*data.Camera, error) {
	// Return minimal valid camera
	return &data.Camera{ID: id, TenantID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}, nil
}
func (d *dummyRepo) Create(ctx context.Context, c *data.Camera) error { return nil }
func (d *dummyRepo) Update(ctx context.Context, c *data.Camera) error { return nil }
func (d *dummyRepo) Delete(ctx context.Context, id uuid.UUID) error   { return nil }
func (d *dummyRepo) SetStatus(ctx context.Context, id, tenantID uuid.UUID, enabled bool) error {
	return nil
}                                                                                  // Renamed in service?
func (d *dummyRepo) SoftDelete(ctx context.Context, id, tenantID uuid.UUID) error  { return nil }
func (d *dummyRepo) CountAll(ctx context.Context, tenantID uuid.UUID) (int, error) { return 0, nil }
func (d *dummyRepo) BulkUpdateStatus(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, enabled bool) error {
	return nil
}
func (d *dummyRepo) BulkAddTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	return nil
}
func (d *dummyRepo) BulkRemoveTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	return nil
}
func (d *dummyRepo) List(ctx context.Context, tenantID uuid.UUID, filter data.CameraFilter, limit, offset int) ([]*data.Camera, int, error) {
	return nil, 0, nil
}
func (d *dummyRepo) CreateGroup(ctx context.Context, g *data.CameraGroup) error { return nil }
func (d *dummyRepo) ListGroups(ctx context.Context, tenantID uuid.UUID) ([]*data.CameraGroup, error) {
	return nil, nil
}
func (d *dummyRepo) DeleteGroup(ctx context.Context, id, tenantID uuid.UUID) error { return nil }
func (d *dummyRepo) SetGroupMembers(ctx context.Context, groupID, tenantID uuid.UUID, cameraIDs []uuid.UUID) error {
	return nil
}

// Mock Auditor
type dummyAuditor struct{}

func (d *dummyAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error { return nil }

// Mock LicenseChecker
type dummyLicense struct{}

func (d *dummyLicense) GetLimits(tenantID uuid.UUID) license.LicenseLimits {
	return license.LicenseLimits{}
}

func setupServiceWithCamera(t *testing.T) (*Service, *redis.Client, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	camSvc := cameras.NewService(&dummyRepo{}, &dummyLicense{}, &dummyAuditor{})

	svc := NewService(rdb, camSvc, "http://localhost:8080", HLSParams{BaseURL: "http://localhost:8080"})
	return svc, rdb, mr
}

func TestStartLiveSession_LimitEnforcement(t *testing.T) {
	svc, rdb, _ := setupServiceWithCamera(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	user := &data.User{ID: userID, TenantID: tenantID}

	// 1. Manually populate 16 active sessions
	activeKey := fmt.Sprintf("live:active:%s:%s", tenantID, userID)

	for i := 0; i < 16; i++ {
		sessID := fmt.Sprintf("sess-%d", i)
		rdb.SAdd(ctx, activeKey, sessID)
		rdb.Set(ctx, fmt.Sprintf("live:sess:%s", sessID), "{}", time.Minute)
	}

	// 2. Try 17th -> Should Fail
	_, err := svc.StartLiveSession(ctx, user, uuid.New().String(), "grid", "sub")
	assert.Error(t, err)
	// Check specific error string constant or partial match
	// ErrLiveLimitExceeded might be unexported or variable, checking substring
	assert.Contains(t, err.Error(), "limit=16")
}

func TestStartLiveSession_Scrubbing(t *testing.T) {
	svc, rdb, _ := setupServiceWithCamera(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	user := &data.User{ID: userID, TenantID: tenantID}

	activeKey := fmt.Sprintf("live:active:%s:%s", tenantID, userID)

	// 1. Populate 16 sessions in Set
	for i := 0; i < 16; i++ {
		rdb.SAdd(ctx, activeKey, fmt.Sprintf("sess-%d", i))
	}

	// 2. ONLY populate 15 valid key-values
	for i := 1; i < 16; i++ {
		rdb.Set(ctx, fmt.Sprintf("live:sess:sess-%d", i), "{}", time.Minute)
	}

	// 3. Try 17th (effectively 16th valid) -> Should Succeed
	camID := uuid.New().String()
	resp, err := svc.StartLiveSession(ctx, user, camID, "grid", "sub")
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	// Verify sess-0 scrubbed
	exists, _ := rdb.SIsMember(ctx, activeKey, "sess-0").Result()
	assert.False(t, exists)

	count, _ := rdb.SCard(ctx, activeKey).Result()
	assert.Equal(t, int64(16), count)
}

func TestStartLiveSession_QualitySelection(t *testing.T) {
	svc, _, _ := setupServiceWithCamera(t)
	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	user := &data.User{ID: userID, TenantID: tenantID}

	// 1. Request "sub"
	resp, err := svc.StartLiveSession(ctx, user, uuid.New().String(), "grid", "sub")
	assert.NoError(t, err)
	assert.Equal(t, "sub", resp.SelectedQuality)

	// 2. Request "auto"
	resp2, err := svc.StartLiveSession(ctx, user, uuid.New().String(), "fullscreen", "auto")
	assert.NoError(t, err)
	assert.Equal(t, "main", resp2.SelectedQuality)
}

func TestOverlay_Demand(t *testing.T) {
	svc, _, _ := setupServiceWithCamera(t)
	ctx := context.Background()

	// 1. Increment Demand
	err := svc.IncrementOverlayDemand(ctx, "cam-1")
	assert.NoError(t, err)

	// 2. Check Active List
	list, err := svc.GetCamerasWithOverlayEnabled(ctx)
	assert.NoError(t, err)
	assert.Contains(t, list, "cam-1")

	// 3. Decrement
	err = svc.DecrementOverlayDemand(ctx, "cam-1")
	assert.NoError(t, err)

	// 4. Verify Empty
	list2, err := svc.GetCamerasWithOverlayEnabled(ctx)
	assert.NoError(t, err)
	assert.NotContains(t, list2, "cam-1")
}

func TestDetection_Storage(t *testing.T) {
	svc, _, _ := setupServiceWithCamera(t)
	ctx := context.Background()
	tid := uuid.New()
	cid := "cam-1"

	payload := &DetectionPayload{
		CameraID: cid,
		TSUnixMS: time.Now().UnixMilli(),
		Stream:   "basic",
		Objects: []Object{
			{Label: "person", Confidence: 0.9, BBox: BBox{X: 0.1, Y: 0.1, W: 0.2, H: 0.2}},
		},
	}

	// 1. Save
	err := svc.SaveDetection(ctx, tid, payload)
	assert.NoError(t, err)

	// 2. Get (now requires stream param)
	got, err := svc.GetLatestDetection(ctx, tid, cid, "basic")
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, payload.CameraID, got.CameraID)
	assert.Equal(t, 1, len(got.Objects))
}
