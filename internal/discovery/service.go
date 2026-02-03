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
	Keyring *crypto.Keyring
	Auditor Auditor
}

func NewService(repo DiscoveryRepository, keyring *crypto.Keyring, auditor Auditor) *Service {
	return &Service{Repo: repo, Keyring: keyring, Auditor: auditor}
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
			EndpointRef:      dev.EndpointRef,
			SupportsProfileS: dev.SupportsProfileS,
			SupportsProfileT: dev.SupportsProfileT,
			SupportsProfileG: dev.SupportsProfileG,
			// Others empty until probed
		}

		if err := s.Repo.UpsertDevice(ctx, dbDev); err != nil {
			log.Printf("Failed to persist device %s: %v", dev.IPAddress, err)
			errCount++
		} else {
			count++
		}
	}

	s.Repo.UpdateRunStatus(ctx, runID, "completed", true, count, errCount)

	// Audit Complete (Optional, or just check DB)
}

func (s *Service) GetRun(ctx context.Context, runID uuid.UUID) (*data.DiscoveryRun, error) {
	return s.Repo.GetRun(ctx, runID)
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

	cli, err := NewOnvifClient(xaddr, username, password)
	if err != nil {
		return s.failProbe(ctx, dev, "client_init_error")
	}

	// 4. Execute Calls (Parallel logic omitted for simplicity, sequential is safer for stability)
	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	// A. Device Info
	info, err := cli.GetDeviceInformation(probeCtx)
	if err != nil {
		return s.failProbe(ctx, dev, "onvif_unauthorized_or_timeout") // simplified
	}

	dev.Manufacturer = info.Manufacturer
	dev.Model = info.Model
	dev.FirmwareVersion = info.FirmwareVersion
	dev.SerialNumber = info.SerialNumber

	// B. Capabilities (Profiles Hint)
	capsMap, mediaURI, err := cli.GetCapabilities(probeCtx)
	if err != nil {
		// Non-fatal?
	}
	dev.Capabilities, _ = json.Marshal(capsMap) // Cap size check needed logically

	// C. Profiles (Authoritative)
	if mediaURI != "" {
		profiles, err := cli.GetProfiles(probeCtx, mediaURI)
		if err == nil {
			// Infer S/T/G from profiles?
			// Profiles usually just list token/config.
			// Real check is if GetProfiles SUCCEEDS usually implies S.
			// Detailed check would inspect config types.
			// Let's assume Success = Profile S supported at least.
			dev.SupportsProfileS = true

			// Store raw profiles summary
			dev.MediaProfiles, _ = json.Marshal(profiles)

			// D. RTSP URIs
			var uris []string
			for _, p := range profiles {
				uri, err := cli.GetStreamUri(probeCtx, mediaURI, p.Token)
				if err == nil {
					// Strip Credentials from URI
					// e.g., rtsp://user:pass@IP...
					safeURI := stripCredentials(uri)
					uris = append(uris, fmt.Sprintf("%s|%s", p.Token, safeURI))
				}
			}
			dev.RTSP_URIs, _ = json.Marshal(uris)
		}
	}

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

	return s.Repo.UpdateDeviceProbe(ctx, dev)
}

func (s *Service) resolveCredential(ctx context.Context, credID, tenantID uuid.UUID) (string, string, error) {
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
	return nil
}

// Helpers
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
