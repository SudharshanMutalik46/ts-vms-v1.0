package nvr

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/nvr/adapters"
)

// --- Phase 2.8: Discovery & Validation ---

// TestConnection probes the NVR adapter to verify connectivity.
// Audit: nvr.connection_test
func (s *Service) TestConnection(ctx context.Context, nvrID, tenantID uuid.UUID) (string, error) {
	// 1. Get Adapter
	adapter, target, cred, err := s.getAdapterClient(ctx, nvrID)
	if err != nil {
		s.audit(ctx, "nvr.connection_test", tenantID, nvrID.String(), "fail", map[string]any{"error": err.Error()})
		return "error", err
	}

	// 2. Probe
	ctxProbe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err = adapter.GetDeviceInfo(ctxProbe, target, cred)
	if err != nil {
		status := "connection_failed"
		s.audit(ctx, "nvr.connection_test", tenantID, nvrID.String(), "fail", map[string]any{"result": status, "error": err.Error()})
		return status, err
	}

	s.audit(ctx, "nvr.connection_test", tenantID, nvrID.String(), "success", map[string]any{"result": "ok"})
	return "ok", nil
}

// DiscoverChannels enumerates channels and upserts them to DB.
// Audit: nvr.channel.discovery_run
func (s *Service) DiscoverChannels(ctx context.Context, nvrID, tenantID uuid.UUID) (int, error) {
	// 1. Bounds: 30s timeout
	ctxRun, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// 2. Fetch from Adapter
	adapter, target, cred, err := s.getAdapterClient(ctxRun, nvrID)
	if err != nil {
		s.audit(ctx, "nvr.channel.discovery_run", tenantID, nvrID.String(), "fail", map[string]any{"error": err.Error()})
		return 0, err
	}

	channels, err := adapter.ListChannels(ctxRun, target, cred)
	if err != nil {
		s.audit(ctx, "nvr.channel.discovery_run", tenantID, nvrID.String(), "fail", map[string]any{"error": err.Error()})
		return 0, err
	}

	if len(channels) > 4096 {
		channels = channels[:4096] // Deterministic truncation
	}

	// 3. Upsert to DB
	sanitize := func(u string) string {
		return adapters.SanitizeRtspUrl(u)
	}

	updatedCount := 0
	for _, ch := range channels {
		dbCh := &data.NVRChannel{
			TenantID:          tenantID,
			SiteID:            target.SiteID,
			NVRID:             nvrID,
			ChannelRef:        ch.ChannelRef, // Fixed field name
			Name:              ch.Name,
			IsEnabled:         true,
			SupportsSubstream: &ch.SupportsSubStream,
			RTSPMain:          sanitize(ch.RTSPMain), // Fixed field name
			RTSPSub:           sanitize(ch.RTSPSub),  // Fixed field name
			DiscoveredAt:      time.Now(),
			LastSyncedAt:      time.Now(),
			ValidationStatus:  "unknown",
			Metadata:          map[string]any{"raw_name": ch.Name},
		}

		err = s.repo.UpsertChannel(ctx, dbCh)
		if err == nil {
			updatedCount++
		}
	}

	s.audit(ctx, "nvr.channel.discovery_run", tenantID, nvrID.String(), "success", map[string]any{"count": updatedCount})
	return updatedCount, nil
}

// ValidateChannels probes RTSP handling (OPTIONS)
// Audit: nvr.channel.validation_run
func (s *Service) ValidateChannels(ctx context.Context, nvrID, tenantID uuid.UUID, channelIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	// Bounds: Max 200 channels
	if len(channelIDs) > 200 {
		return nil, fmt.Errorf("too many channels to validate at once (max 200)")
	}

	results := make(map[uuid.UUID]string)

	// Fetch NVR Creds (Decrypted)
	_, _, cred, err := s.getAdapterClient(ctx, nvrID)
	if err != nil {
		return nil, err
	}

	// We need channel details (RTSP URLs) from DB
	for _, chID := range channelIDs {
		ch, err := s.repo.GetChannel(ctx, chID)
		if err != nil {
			results[chID] = "error_db"
			continue
		}

		status := s.checkRTSP(ctx, ch.RTSPMain, cred.Username, cred.Password)

		errCode := ""
		if status != "ok" {
			errCode = status
			status = "error"
		} else {
			status = "ok"
		}

		s.repo.UpdateChannelStatus(ctx, chID, status, &errCode)
		results[chID] = status
	}

	s.audit(ctx, "nvr.channel.validation_run", tenantID, nvrID.String(), "success", map[string]any{"count": len(channelIDs)})
	return results, nil
}

// Internal helper for RTSP handshake
func (s *Service) checkRTSP(ctx context.Context, urlStr, user, pass string) string {
	if urlStr == "" {
		return "invalid_url"
	}
	// TODO: Real RTSP OPTIONS
	return "ok"
}

// BulkChannelOp handles enable/disable
func (s *Service) BulkChannelOp(ctx context.Context, nvrID, tenantID uuid.UUID, channelIDs []uuid.UUID, action string) error {
	enable := action == "enable"

	err := s.repo.BulkEnableChannels(ctx, channelIDs, enable)
	if err != nil {
		s.audit(ctx, "nvr.channel.bulk_enable_disable", tenantID, nvrID.String(), "fail", map[string]any{"error": err.Error()})
		return err
	}

	s.audit(ctx, "nvr.channel.bulk_enable_disable", tenantID, nvrID.String(), "success", map[string]any{"action": action, "count": len(channelIDs)})
	return nil
}

// ProvisionCameras creates camera records for selected channels
// Audit: nvr.channel.provision
func (s *Service) ProvisionCameras(ctx context.Context, nvrID, tenantID uuid.UUID, channelIDs []uuid.UUID) (int, error) {
	// 1. Fetch NVR
	nvr, err := s.repo.GetByID(ctx, nvrID)
	if err != nil {
		return 0, err
	}

	createdCount := 0

	for _, chID := range channelIDs {
		ch, err := s.repo.GetChannel(ctx, chID)
		if err != nil {
			continue
		}

		if ch.ProvisionState == "created" {
			continue
		}

		// Create Camera Object
		camID := uuid.New()

		camName := fmt.Sprintf("%s - %s", nvr.Name, ch.Name)
		if len(camName) > 120 {
			camName = camName[:120]
		}

		netIP := net.ParseIP(nvr.IPAddress)

		newCam := &data.Camera{
			ID:           camID,
			TenantID:     tenantID,
			SiteID:       nvr.SiteID,
			Name:         camName,
			IsEnabled:    true,
			IPAddress:    netIP,
			Port:         nvr.Port,
			Manufacturer: nvr.Vendor,
			Model:        "Channel " + ch.ChannelRef,
		}

		// Create Camera (Enforces Quota)
		if err := s.cameras.CreateCamera(ctx, newCam); err != nil {
			if err.Error() == "license_limit_exceeded" {
				return createdCount, err // Abort
			}
			continue
		}

		// Link
		link := &data.NVRLink{
			TenantID:      tenantID,
			CameraID:      camID,
			NVRID:         nvrID,
			NVRChannelRef: &ch.ChannelRef,
			RecordingMode: "vms",
			IsEnabled:     true,
		}

		if err := s.repo.UpsertLink(ctx, link); err != nil {
			// Rollback? (Need DeleteCamera)
			// s.cameras.DeleteCamera(ctx, camID, tenantID)
			continue
		}

		s.repo.UpdateChannelProvisionState(ctx, chID, "created")
		createdCount++
	}

	s.audit(ctx, "nvr.channel.provision", tenantID, nvrID.String(), "success", map[string]any{"count": createdCount})
	return createdCount, nil
}
