package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/onvif"
)

const (
	MaxScanDuration         = 5 * time.Second
	MaxDevicesPerRun        = 4096
	MaxProbeWorkers         = 16
	ProbeTimeout            = 10 * time.Second
	OnvifCredentialsPurpose = "onvif_bootstrap_v1" // AAD Purpose
)

type Auditor interface {
	WriteEvent(ctx context.Context, evt audit.AuditEvent) error
}

type DiscoveryRepository interface {
	CreateRun(ctx context.Context, run *data.DiscoveryRun) error
	UpdateRunStatus(ctx context.Context, id uuid.UUID, status string, finished bool, deviceCount, errorCount int) error
	GetRun(ctx context.Context, id uuid.UUID) (*data.DiscoveryRun, error)
	UpsertDevice(ctx context.Context, d *data.DiscoveredDevice) error
	UpdateDeviceProbe(ctx context.Context, d *data.DiscoveredDevice) error
	GetDevice(ctx context.Context, id uuid.UUID) (*data.DiscoveredDevice, error)
	ListDevices(ctx context.Context, runID uuid.UUID, limit, offset int) ([]*data.DiscoveredDevice, error)
	StoreBootstrapCred(ctx context.Context, c *data.OnvifCredential) error
	GetBootstrapCred(ctx context.Context, id uuid.UUID) (*data.OnvifCredential, error)
}

type Service struct {
	Repo    DiscoveryRepository
	NvrRepo data.NVRRepository
	Keyring *crypto.Keyring
	Auditor Auditor
}

func NewService(repo DiscoveryRepository, nvrRepo data.NVRRepository, keyring *crypto.Keyring, auditor Auditor) *Service {
	return &Service{Repo: repo, NvrRepo: nvrRepo, Keyring: keyring, Auditor: auditor}
}

// StartDiscovery (Async)
func (s *Service) StartDiscovery(ctx context.Context, tenantID uuid.UUID, siteID *uuid.UUID) (uuid.UUID, error) {
	// Create Run
	run := &data.DiscoveryRun{
		TenantID: tenantID,
		SiteID:   siteID,
		Status:   "running",
	}
	if err := s.Repo.CreateRun(ctx, run); err != nil {
		return uuid.Nil, err
	}

	// Audit Start
	meta, _ := json.Marshal(map[string]interface{}{"site_id": siteID})
	s.Auditor.WriteEvent(ctx, audit.AuditEvent{
		EventID:    uuid.New(),
		Action:     "onvif.discovery.run",
		TargetID:   run.ID.String(),
		TargetType: "discovery_run",
		TenantID:   tenantID,
		Result:     "success",
		Metadata:   meta,
	})

	// Launch Background Scan
	// Note: We need a detached context for background work, but we want to carry TraceID if possible.
	// For now, simple Background.
	go s.runScan(context.Background(), run.ID, tenantID)

	return run.ID, nil
}

func (s *Service) runScan(ctx context.Context, runID, tenantID uuid.UUID) {
	client, err := NewWSDiscoveryClient()
	if err != nil {
		log.Printf("Discovery Init Failed: %v", err)
		s.Repo.UpdateRunStatus(ctx, runID, "failed", true, 0, 1)
		return
	}
	defer client.Close()

	results, err := client.Scan(ctx, MaxScanDuration)
	if err != nil {
		log.Printf("Discovery Scan Failed: %v", err)
		s.Repo.UpdateRunStatus(ctx, runID, "failed", true, 0, 1)
		return
	}

	// Persist Results (capped)
	count := 0
	errCount := 0
	for i, dev := range results {
		if i >= MaxDevicesPerRun {
			break
		}

		// Map simplified struct to DB struct
		dbDev := &data.DiscoveredDevice{
			TenantID:         tenantID,
			DiscoveryRunID:   runID,
			IPAddress:        dev.IPAddress,
			Manufacturer:     dev.Manufacturer,
			Model:            dev.Model,
			EndpointRef:      dev.EndpointRef,
			SupportsProfileS: dev.SupportsProfileS,
			SupportsProfileT: dev.SupportsProfileT,
			SupportsProfileG: dev.SupportsProfileG,
			XAddrs:           dev.XAddrs,
			// Initialize JSON fields to valid empty values to avoid 22P02 DB error
			Capabilities:  json.RawMessage("{}"),
			MediaProfiles: json.RawMessage("[]"),
			RTSP_URIs:     json.RawMessage("[]"),
		}

		if err := s.Repo.UpsertDevice(ctx, dbDev); err != nil {
			log.Printf("Failed to persist device %s: %v", dev.IPAddress, err)
			errCount++
		} else {
			count++
			// Best-effort enrichment: Fetch Info/Profiles in background if possible
			// We use anonymous probing since we don't have credentials yet
			go func(d *data.DiscoveredDevice) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				// Run Probe with empty creds
				_ = s.ProbeDevice(bgCtx, d.ID, uuid.Nil, d.TenantID)
			}(dbDev)
		}
	}

	s.Repo.UpdateRunStatus(ctx, runID, "completed", true, count, errCount)

	// Audit Complete (Optional, or just check DB)
}

func (s *Service) GetRun(ctx context.Context, runID uuid.UUID) (*data.DiscoveryRun, error) {
	return s.Repo.GetRun(ctx, runID)
}

func (s *Service) GetDevice(ctx context.Context, deviceID uuid.UUID) (*data.DiscoveredDevice, error) {
	return s.Repo.GetDevice(ctx, deviceID)
}

func (s *Service) ListDevices(ctx context.Context, runID uuid.UUID) ([]*data.DiscoveredDevice, error) {
	return s.Repo.ListDevices(ctx, runID, 100, 0)
}

// Credential Management (Bootstrap)
func (s *Service) CreateBootstrapCredential(ctx context.Context, tenantID uuid.UUID, username, password string) (uuid.UUID, error) {
	// 1. Generate DEK
	dek, err := crypto.GenerateDEK()
	if err != nil {
		return uuid.Nil, err
	}

	// 2. Encrypt Data (Username:Password)
	payload := fmt.Sprintf("%s:%s", username, password)

	// AAD: Tenant + Purpose (No CameraID here, so simpler AAD)
	// Must match unwrap logic
	aad := []byte(fmt.Sprintf("tenant:%s:purpose:%s", tenantID, OnvifCredentialsPurpose))

	nonce, ciphertext, tag, err := crypto.EncryptGCM(dek, []byte(payload), aad)
	if err != nil {
		return uuid.Nil, err
	}

	// 3. Wrap DEK
	masterKID, wNonce, wCipher, wTag, err := s.Keyring.WrapDEK(dek, aad)
	if err != nil {
		return uuid.Nil, err
	}

	// 4. Store
	cred := &data.OnvifCredential{
		TenantID:       tenantID,
		MasterKID:      masterKID,
		DEKNonce:       wNonce,
		DEKCiphertext:  wCipher,
		DEKTag:         wTag,
		DataNonce:      nonce,
		DataCiphertext: ciphertext,
		DataTag:        tag,
	}

	if err := s.Repo.StoreBootstrapCred(ctx, cred); err != nil {
		return uuid.Nil, err
	}

	return cred.ID, nil
}

// Probing
func (s *Service) ProbeDevice(ctx context.Context, deviceID, credID uuid.UUID, tenantID uuid.UUID) error {
	// 1. Get Device
	dev, err := s.Repo.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	if dev.TenantID != tenantID {
		return fmt.Errorf("unauthorized")
	}

	// 2. Resolve Credential
	username, password, err := s.resolveCredential(ctx, credID, tenantID)
	if err != nil {
		return fmt.Errorf("credential error: %w", err)
	}

	// 3. Init Client
	// XAddr might be missing if only IP found, assume http://IP/onvif/device_service if empty
	xaddr := dev.EndpointRef
	if xaddr == "" || !strings.Contains(xaddr, "http") {
		xaddr = fmt.Sprintf("http://%s/onvif/device_service", dev.IPAddress)
	}

	cli, err := onvif.NewOnvifClient(xaddr, username, password)
	if err != nil {
		fmt.Printf("[PROBE] Client Init Failed for %s: %v\n", dev.IPAddress, err)
		return s.failProbe(ctx, dev, "client_init_error")
	}

	fmt.Printf("[PROBE] Starting probe for %s (XAddr: %s)\n", dev.IPAddress, xaddr)

	// 4. Execute Calls (Parallel logic omitted for simplicity, sequential is safer for stability)
	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	// A. Device Info
	info, err := cli.GetDeviceInformation(probeCtx)
	if err != nil {
		fmt.Printf("[PROBE] GetDeviceInformation Failed for %s: %v\n", dev.IPAddress, err)
		return s.failProbe(ctx, dev, "onvif_unauthorized_or_timeout") // simplified
	}

	fmt.Printf("[PROBE] Detected: %s %s (%s)\n", info.Manufacturer, info.Model, dev.IPAddress)

	dev.Manufacturer = info.Manufacturer
	dev.Model = info.Model
	dev.FirmwareVersion = info.FirmwareVersion
	dev.SerialNumber = info.SerialNumber

	// B. Capabilities (Profiles Hint)
	capsMap, mediaURI, eventsURI, media2URI, err := cli.GetCapabilities(probeCtx)
	if err != nil {
		fmt.Printf("[PROBE] GetCapabilities Failed for %s (Non-Fatal): %v\n", dev.IPAddress, err)
	} else {
		fmt.Printf("[PROBE] Media URI: %s, Media2 URI: %s, Events URI: %s\n", mediaURI, media2URI, eventsURI)
	}
	// Temporarily marshal capsMap into dev.Capabilities in case GetProfiles fails.
	dev.Capabilities, _ = json.Marshal(capsMap)

	var chosenProfileToken string
	var mergedCaps onvif.CameraCapabilities

	bestMediaURI := mediaURI
	useMedia2 := false
	if capsMap["Media2"] && media2URI != "" {
		bestMediaURI = media2URI
		useMedia2 = true
	}

	if bestMediaURI != "" {
		profiles, err := cli.GetProfiles(probeCtx, bestMediaURI)
		if err == nil {
			dev.SupportsProfileS = true
			mergedCaps = onvif.DetermineCapabilities(profiles)
			dev.MediaProfiles, _ = json.Marshal(profiles)

			if len(profiles) > 0 {
				chosenProfileToken = profiles[0].Token
			}

			var uris []string
			for _, p := range profiles {
				uri, err := cli.GetStreamUri(probeCtx, bestMediaURI, p.Token, useMedia2)
				if err == nil {
					safeURI := stripCredentials(uri)
					uris = append(uris, fmt.Sprintf("%s|%s", p.Token, safeURI))
				}
			}
			dev.RTSP_URIs, _ = json.Marshal(uris)
		}
	}

	// D. Phase 1 enrichment
	if mac, err := cli.GetNetworkInterfaces(probeCtx); err == nil && mac != "" {
		dev.MACAddress = mac
	}

	if chosenProfileToken != "" && mediaURI != "" {
		if snap, err := cli.GetSnapshotURI(probeCtx, mediaURI, chosenProfileToken); err == nil && snap != "" {
			dev.SnapshotURI = stripCredentials(snap)
		}
	}

	if offsetSec, err := cli.GetSystemDateAndTime(probeCtx); err == nil {
		dev.ClockOffsetSec = offsetSec
	}

	if mediaURI != "" {
		if n, err := cli.GetAudioSources(probeCtx, mediaURI); err == nil && n > 0 {
			dev.SupportsAudio = true
		}
	}

	if eventsURI != "" {
		if topics, err := cli.GetEventProperties(probeCtx, eventsURI); err == nil {
			dev.EventTopics = topics
			dev.SupportsEvents = len(topics) > 0
		}
	}

	// Merge inferred + directly probed booleans
	mergedCaps.HasAudio = mergedCaps.HasAudio || dev.SupportsAudio
	mergedCaps.PTZ = mergedCaps.PTZ || dev.SupportsPTZ
	// Events support can be inferred if eventsURI is present, or if topics > 0
	mergedCapsHasEvents := dev.SupportsEvents || (eventsURI != "")

	dev.SupportsAudio = mergedCaps.HasAudio
	dev.SupportsPTZ = mergedCaps.PTZ
	dev.SupportsEvents = mergedCapsHasEvents

	dev.Capabilities, _ = json.Marshal(map[string]any{
		"HasAudio": dev.SupportsAudio,
		"PTZ":      dev.SupportsPTZ,
		"Events":   dev.SupportsEvents,
	})

	dev.LastProbeAt = timePtr(time.Now())
	dev.LastErrorCode = ""

	// Audit
	s.Auditor.WriteEvent(ctx, audit.AuditEvent{
		EventID:    uuid.New(),
		Action:     "onvif.discovery.probe",
		TargetID:   dev.ID.String(),
		TargetType: "discovered_device",
		TenantID:   tenantID,
		Result:     "success",
	})

	err = s.Repo.UpdateDeviceProbe(ctx, dev)
	if err == nil {
		// Sync to NVR channels if matching NVR exists
		go s.syncToNvr(context.Background(), dev)
	}
	return err
}

func (s *Service) syncToNvr(ctx context.Context, dev *data.DiscoveredDevice) {
	if s.NvrRepo == nil {
		return
	}

	// 1. Find NVRs with same IP
	// Repo.List handles RLS but we pass tenantID explicitly in filter if needed?
	// data.NVRFilter doesn't have TenantID, but List takes it.
	nvrs, _, err := s.NvrRepo.List(ctx, dev.TenantID, data.NVRFilter{Query: dev.IPAddress}, 10, 0)
	if err != nil {
		return
	}

	for _, nvr := range nvrs {
		// Strict IP match (repo might return partial match)
		if nvr.IPAddress != dev.IPAddress {
			continue
		}

		// 2. Map Profiles
		var profiles []onvif.MediaProfile
		if err := json.Unmarshal(dev.MediaProfiles, &profiles); err != nil {
			continue
		}

		var rawUris []string
		_ = json.Unmarshal(dev.RTSP_URIs, &rawUris)
		uriMap := make(map[string]string)
		for _, u := range rawUris {
			parts := strings.SplitN(u, "|", 2)
			if len(parts) == 2 {
				uriMap[parts[0]] = parts[1]
			}
		}

		// 3. Upsert Channels
		for _, p := range profiles {
			mainURI := uriMap[p.Token]
			sub := false

			ch := &data.NVRChannel{
				TenantID:          dev.TenantID,
				SiteID:            nvr.SiteID,
				NVRID:             nvr.ID,
				ChannelRef:        p.Token,
				Name:              p.Name,
				IsEnabled:         true,
				SupportsSubstream: &sub,
				RTSPMain:          mainURI,
				DiscoveredAt:      time.Now(),
				LastSyncedAt:      time.Now(),
				ValidationStatus:  "ok",
			}
			s.NvrRepo.UpsertChannel(ctx, ch)
		}
	}
}

func (s *Service) resolveCredential(ctx context.Context, credID, tenantID uuid.UUID) (string, string, error) {
	if credID == uuid.Nil {
		return "", "", nil // Anonymous
	}
	c, err := s.Repo.GetBootstrapCred(ctx, credID)
	if err != nil {
		return "", "", err
	}
	if c.TenantID != tenantID {
		return "", "", fmt.Errorf("unauthorized credential")
	}

	// Unwrap DEK
	aad := []byte(fmt.Sprintf("tenant:%s:purpose:%s", tenantID, OnvifCredentialsPurpose))
	dek, err := s.Keyring.UnwrapDEK(c.MasterKID, c.DEKNonce, c.DEKCiphertext, c.DEKTag, aad)
	if err != nil {
		return "", "", err
	}

	// Decrypt Payload
	payloadBytes, err := crypto.DecryptGCM(dek, c.DataNonce, c.DataCiphertext, c.DataTag, aad)
	if err != nil {
		return "", "", err
	}

	parts := strings.SplitN(string(payloadBytes), ":", 2)
	if len(parts) != 2 {
		return "", "", errors.New("invalid payload format")
	}
	return parts[0], parts[1], nil
}

func (s *Service) failProbe(ctx context.Context, dev *data.DiscoveredDevice, code string) error {
	dev.LastErrorCode = code
	dev.LastProbeAt = timePtr(time.Now())
	s.Repo.UpdateDeviceProbe(ctx, dev)

	meta, _ := json.Marshal(map[string]interface{}{"code": code})
	s.Auditor.WriteEvent(ctx, audit.AuditEvent{
		EventID:    uuid.New(),
		Action:     "onvif.discovery.probe",
		TargetID:   dev.ID.String(),
		TargetType: "discovered_device",
		TenantID:   dev.TenantID,
		Result:     "failure",
		Metadata:   meta,
	})
	return fmt.Errorf("probe failed: %s", code)
}

// stripCredentials removed, used from onvif package if needed? 
// Actually OnvifClient has stripCredentials? No it doesn't.
// Wait, I didn't move stripCredentials to onvif. 
// Let's keep it here or move it.
// I'll keep it here for now as it's a small helper.

func stripCredentials(uri string) string {
	// Parse as URL? RTSP isn't always standard URL parseable if quirky, but typically yes.
	// Manual string manip often safer for RTSP to preserve query params etc exactly.
	// rtsp://user:pass@host...
	if idx := strings.Index(uri, "://"); idx != -1 {
		proto := uri[:idx+3]
		rest := uri[idx+3:]
		if at := strings.Index(rest, "@"); at != -1 {
			// Check if user:pass before @
			// Find first slash to ensure @ is in authority section
			slash := strings.Index(rest, "/")
			if slash == -1 || at < slash {
				// We have credentials, remove them
				return proto + rest[at+1:]
			}
		}
	}
	return uri
}

func timePtr(t time.Time) *time.Time {
	return &t
}
