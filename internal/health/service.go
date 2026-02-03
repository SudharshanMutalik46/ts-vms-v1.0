package health

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
)

type Service struct {
	Repo    data.HealthRepository
	NVRRepo data.NVRRepository
	Prober  Prober
	History *HistoryManager
	Alerts  *AlertManager
}

func NewService(repo data.HealthRepository, nvrRepo data.NVRRepository, prober Prober) *Service {
	return &Service{
		Repo:    repo,
		NVRRepo: nvrRepo,
		Prober:  prober,
		History: NewHistoryManager(repo),
		Alerts:  NewAlertManager(repo),
	}
}

// ... unchanged ListTargets ...
// ... unchanged PerformCheck ...
// ... unchanged ManualCheck ... (omitted in tool call, will use StartLine/EndLine to preserve)

func (s *Service) GetStatus(ctx context.Context, cameraID uuid.UUID) (*data.CameraHealthCurrent, error) {
	// 1. Get Base Status
	status, err := s.Repo.GetStatus(ctx, cameraID)
	if err != nil {
		return nil, err
	}
	if status == nil {
		// Return empty/unknown if no health record yet
		return nil, nil
	}

	// 2. Check NVR Link
	// We need to resolve NVR status.
	// If NVRRepo is nil (testing?), skip? No, should be injected.
	if s.NVRRepo == nil {
		return status, nil
	}

	link, err := s.NVRRepo.GetLinkByCameraID(ctx, cameraID)
	if err == nil && link != nil && link.NVRID != uuid.Nil {
		status.NVRLinked = true
		status.NVRID = &link.NVRID

		// Fetch NVR Status
		nvr, err := s.NVRRepo.GetByID(ctx, link.NVRID)
		if err == nil {
			status.NVRStatus = nvr.Status

			// 3. Effective Status Logic
			// If NVR is offline/auth_failed/error, Camera is effectively OFF-LINE.
			if nvr.Status == "offline" || nvr.Status == "auth_failed" || nvr.Status == "error" {
				// Override
				status.EffectiveStatus = data.HealthStatusOffline // Map to OFFLINE
				status.EffectiveReason = fmt.Sprintf("nvr_%s", nvr.Status)
			} else {
				status.EffectiveStatus = status.Status
				status.EffectiveReason = ""
			}
		} else {
			status.NVRStatus = "unknown"
			status.EffectiveStatus = status.Status
		}
	} else {
		// Not linked
		status.NVRLinked = false
		status.EffectiveStatus = status.Status
	}

	return status, nil
}

// ListTargets fetches cameras with their selected RTSP profiles
// Returns minimal struct for scheduler
func (s *Service) ListTargets(ctx context.Context) ([]data.CameraHealthTarget, error) {
	// Implementation depends on data layer providing this optimization
	// For now assume Repo has it.
	// data.CameraHealthTarget needs definition in data package or here.
	// Let's assume it's in data.
	return s.Repo.ListTargets(ctx)
}

// PerformCheck is called by Scheduler Worker
func (s *Service) PerformCheck(ctx context.Context, tenantID, cameraID uuid.UUID, rtspURL string) {
	// 1. Probe
	status, reason, rtt := s.Prober.Probe(ctx, tenantID, cameraID, rtspURL)

	// 2. Fetch Current State (for Alert Logic)
	// We need consecutive failures count.
	current, err := s.Repo.GetStatus(ctx, cameraID)
	consecutive := 0
	var lastSuccess *time.Time

	if err == nil && current != nil {
		consecutive = current.ConsecutiveFailures
		lastSuccess = current.LastSuccessAt
	}

	// Update Consecutive
	if status == data.HealthStatusOnline {
		consecutive = 0
		now := time.Now()
		lastSuccess = &now
	} else {
		consecutive++
	}

	// 3. Update Current Status
	newStatus := &data.CameraHealthCurrent{
		TenantID:            tenantID,
		CameraID:            cameraID,
		Status:              status,
		LastCheckedAt:       time.Now(),
		LastSuccessAt:       lastSuccess,
		ConsecutiveFailures: consecutive,
		LastErrorCode:       reason,
		UpdatedAt:           time.Now(),
	}

	if err := s.Repo.UpsertStatus(ctx, newStatus); err != nil {
		// Log error
	}

	// 4. Record History
	// Optimization: Only record on transition? Or every check?
	// "Store a bounded history..."
	// If checking every 60s, 200 entries = 200 mins = 3 hours.
	// If we record ALL, we lose history fast.
	// "Keep 7 days rolling" OR "N events".
	// User Prompts: "Keep last N events (e.g., N=200)".
	// Implies record everything? Or significant events?
	// Standard practice: Record State Transitions + Periodic samples.
	// But simple implementation is record all.
	// Let's record all for now, as it aids debugging (e.g. RTT graph).
	if err := s.History.AddEntry(ctx, tenantID, cameraID, status, reason, rtt); err != nil {
		// Log
	}

	// 5. Alerting
	if err := s.Alerts.ProcessState(ctx, tenantID, cameraID, status, consecutive, lastSuccess); err != nil {
		// Log
	}

	// 6. Metrics (Prometheus hooks would go here)
	// metrics.HealthChecksTotal.WithLabelValues(string(status), reason).Inc()
}

// TrigggerCheck (Manual Operator)
func (s *Service) ManualCheck(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	// 1. Fetch Target Info (URL)
	target, err := s.Repo.GetTarget(ctx, cameraID)
	if err != nil {
		return err // e.g. Not Found
	}

	// 2. Perform Check (Sync? Or Async?)
	// "triggers an immediate probe".
	// Should be async to not block API?
	// But operator wants result? API usually returns "accepted".
	// Let's run async to avoid holding HTTP handler.
	go s.PerformCheck(context.Background(), tenantID, cameraID, target.RTSPURL)

	// Audit
	// s.Auditor.Log(...)

	return nil
}

func (s *Service) GetHistory(ctx context.Context, cameraID uuid.UUID, limit, offset int) ([]*data.CameraHealthHistory, error) {
	return s.Repo.GetHistory(ctx, cameraID, limit, offset)
}

func (s *Service) ListAlerts(ctx context.Context, tenantID uuid.UUID, state string) ([]*data.CameraAlert, error) {
	return s.Repo.ListAlerts(ctx, tenantID, state)
}
