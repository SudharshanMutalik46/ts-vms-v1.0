package health

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
)

type AlertManager struct {
	repo data.HealthRepository
}

func NewAlertManager(repo data.HealthRepository) *AlertManager {
	return &AlertManager{repo: repo}
}

// ProcessState evaluates if an alert should be opened or closed
func (a *AlertManager) ProcessState(ctx context.Context, tenantID, cameraID uuid.UUID, status data.CameraHealthStatus, consecutiveFailures int, lastSuccessAt *time.Time) error {
	alertType := "offline_over_5m"

	// 1. Check for Open Alert
	activeAlert, err := a.repo.GetOpenAlert(ctx, cameraID, alertType)
	if err != nil {
		return err
	}

	// 2. Logic: Open Alert if Offline > 5m
	if activeAlert == nil {
		if status == data.HealthStatusOffline {
			// Calculate duration since last success
			// If lastSuccessAt is nil, assume it's been offline forever?
			// Or check consecutive failures?
			// Spec: "OFFLINE continuously for > 5 minutes"
			// If consecutive failures > 5 (assuming 1 check/min), that's approximate.
			// But time is better.

			offlineDuration := time.Duration(0)
			if lastSuccessAt != nil {
				offlineDuration = time.Since(*lastSuccessAt)
			} else {
				// Never succeeded? If consecutive > 5, assume > 5m
				if consecutiveFailures >= 5 {
					offlineDuration = 6 * time.Minute
				}
			}

			if offlineDuration > 5*time.Minute {
				// Open Alert
				newAlert := &data.CameraAlert{
					TenantID:  tenantID,
					CameraID:  cameraID,
					Type:      alertType,
					State:     "open",
					StartedAt: time.Now(),
				}
				if err := a.repo.UpsertAlert(ctx, newAlert); err != nil {
					return err
				}
				// Emit Audit/Metric here (handled by Service)
			}
		}
	} else {
		// 3. Logic: Close Alert if Online
		if status == data.HealthStatusOnline {
			if err := a.repo.CloseAlert(ctx, activeAlert.ID); err != nil {
				return err
			}
			// Emit Audit/Metric
		}
	}
	return nil
}
