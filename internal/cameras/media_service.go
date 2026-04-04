package cameras

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/media"
	"github.com/technosupport/ts-vms/internal/nvr/adapters"
	"github.com/technosupport/ts-vms/internal/onvif"
)

type MediaRepository interface {
	UpsertProfile(ctx context.Context, p *data.CameraMediaProfile) error
	UpsertSelection(ctx context.Context, s *data.CameraStreamSelection) error
	GetSelection(ctx context.Context, tenantID, cameraID uuid.UUID) (*data.CameraStreamSelection, error)
	GetValidationResults(ctx context.Context, tenantID, cameraID uuid.UUID) ([]*data.RTSPValidationResult, error)
	UpsertValidationResult(ctx context.Context, res *data.RTSPValidationResult) error
	ListProfiles(ctx context.Context, tenantID, cameraID uuid.UUID) ([]*data.CameraMediaProfile, error)
}

type CredentialProvider interface {
	GetCredentials(ctx context.Context, tenantID, cameraID uuid.UUID, reveal bool) (*CredentialOutput, bool, error)
}

type OnvifClient interface {
	GetCapabilities(ctx context.Context) (map[string]bool, string, string, string, error)
	GetProfiles(ctx context.Context, mediaURI string) ([]onvif.MediaProfile, error)
	GetStreamUri(ctx context.Context, mediaURI, token string, useMedia2 bool) (string, error)
}

type OnvifClientFactory func(xaddr, username, password string) (OnvifClient, error)

type MediaService struct {
	MediaRepo     MediaRepository
	CameraRepo    Repository
	CredService   CredentialProvider
	Validator     *media.Validator
	Auditor       Auditor
	ClientFactory OnvifClientFactory
}

func NewMediaService(mRepo MediaRepository, cRepo Repository, credSvc CredentialProvider, aud Auditor) *MediaService {
	// Initialize Validator with persistence callback
	validator := media.NewValidator(func(job media.ValidationJob, res media.ValidationResult) {
		// Async Callback: Persist Result
		ctx := context.Background() // TODO: Context with timeout?
		dbRes := &data.RTSPValidationResult{
			TenantID:      job.TenantID,
			CameraID:      job.CameraID,
			Variant:       job.Variant,
			Status:        string(res.Status),
			LastErrorCode: res.LastErrorCode,
			RTT:           res.RTT,
		}
		// Note: We ignore error in async callback, or log it
		mRepo.UpsertValidationResult(ctx, dbRes)
	})

	return &MediaService{
		MediaRepo:   mRepo,
		CameraRepo:  cRepo,
		CredService: credSvc,
		Validator:   validator,
		Auditor:     aud,
		ClientFactory: func(x, u, p string) (OnvifClient, error) {
			return onvif.NewOnvifClient(x, u, p)
		},
	}
}

// SelectMediaProfiles Orchestrates Sync -> Select -> Store -> Validate
func (s *MediaService) SelectMediaProfiles(ctx context.Context, tenantID, cameraID uuid.UUID) (*data.CameraStreamSelection, error) {
	// 1. Fetch Credentials (Decrypt) to Probe
	// Use GetCredentials with reveal=true
	out, found, err := s.CredService.GetCredentials(ctx, tenantID, cameraID, true)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve credentials: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("credentials not found")
	}
	user := out.Data.Username
	pass := out.Data.Password

	// 2. Fetch Profiles via ONVIF (Probe)
	cam, err := s.CameraRepo.GetByID(ctx, cameraID)
	if err != nil {
		return nil, err
	}
	if cam.TenantID.String() != tenantID.String() {
		return nil, fmt.Errorf("unauthorized")
	}

	// Construct XAddr
	// Phase 2.1 Camera struct has IPAddress (net.IP) and Port.
	// We use the port if it's defined, otherwise default to 80 for ONVIF.
	host := cam.IPAddress.String()
	port := cam.Port
	if port <= 0 || port == 554 {
		port = 80 // Default ONVIF
	}
	xaddr := fmt.Sprintf("http://%s:%d/onvif/device_service", host, port)
	if port == 80 {
		xaddr = fmt.Sprintf("http://%s/onvif/device_service", host)
	}

	client, err := s.ClientFactory(xaddr, user, pass)
	if err != nil {
		return nil, fmt.Errorf("failed to init onvif client: %w", err)
	}

	// Get Capabilities/Media URI
	features, mediaURI, _, media2URI, err := client.GetCapabilities(ctx)
	if err != nil {
		// Log warning, try default
	}

	bestMediaURI := mediaURI
	useMedia2 := false
	if features["Media2"] && media2URI != "" {
		bestMediaURI = media2URI
		useMedia2 = true
	} else if bestMediaURI == "" {
		bestMediaURI = xaddr
	}

	// Get Profiles
	onvifProfiles, err := client.GetProfiles(ctx, bestMediaURI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profiles: %w", err)
	}

	// 3. Normalize & Store
	var domainProfiles []media.Profile
	for _, op := range onvifProfiles {
		// Get Stream URI for each
		uri, err := client.GetStreamUri(ctx, bestMediaURI, op.Token, useMedia2)
		if err != nil {
			continue // Skip broken profiles
		}

		// Sanitize
		sanitizedURI := media.SanitizeRTSPURL(uri)

		// Codec Mapping
		codec := media.CodecUnknown
		if op.VideoEncoderConfiguration != nil {
			enc := strings.ToUpper(op.VideoEncoderConfiguration.Encoding)
			if strings.Contains(enc, "H264") {
				codec = media.CodecH264
			} else if strings.Contains(enc, "H265") || strings.Contains(enc, "HEVC") {
				codec = media.CodecH265
			} else if strings.Contains(enc, "JPEG") {
				codec = media.CodecMJPEG
			}
		}

		// Audio Codec Mapping
		audioCodec := "—"
		if op.AudioEncoderConfiguration != nil {
			audioCodec = strings.ToUpper(op.AudioEncoderConfiguration.Encoding)
		}

		p := media.Profile{
			Token:      op.Token,
			Name:       op.Name,
			VideoCodec: codec,
			AudioCodec: audioCodec,
			RTSPURL:    sanitizedURI,
		}

		if op.VideoEncoderConfiguration != nil {
			p.Width = op.VideoEncoderConfiguration.Resolution.Width
			p.Height = op.VideoEncoderConfiguration.Resolution.Height
			p.FPS = op.VideoEncoderConfiguration.RateControl.FrameRateLimit
			p.BitrateKbps = op.VideoEncoderConfiguration.RateControl.BitrateLimit
		}

		domainProfiles = append(domainProfiles, p)

		// Persist Normalized
		dbP := &data.CameraMediaProfile{
			TenantID:         tenantID,
			CameraID:         cameraID,
			ProfileToken:     op.Token,
			ProfileName:      p.Name,
			VideoCodec:       string(p.VideoCodec),
			AudioCodec:       p.AudioCodec,
			Width:            p.Width,
			Height:           p.Height,
			FPS:              p.FPS,
			BitrateKbps:      p.BitrateKbps,
			RTSPURLSanitized: sanitizedURI,
		}
		if err := s.MediaRepo.UpsertProfile(ctx, dbP); err != nil {
			log.Printf("[ERROR] Failed to upsert profile %s: %v", op.Token, err)
		}
	}

	// 4. Run Selection
	if len(domainProfiles) == 0 {
		return nil, fmt.Errorf("no playable rtsp profiles discovered")
	}

	selRes := media.SelectProfilesForCodecs(domainProfiles, []media.Codec{media.CodecH264, media.CodecH265})
	if selRes.MainRTSP == "" && selRes.SubRTSP == "" {
		return nil, fmt.Errorf("no playable rtsp urls discovered")
	}

	// Persist Selection
	dbSel := &data.CameraStreamSelection{
		TenantID:         tenantID,
		CameraID:         cameraID,
		MainProfileToken: selRes.MainToken,
		MainRTSP:         selRes.MainRTSP,
		MainSupported:    selRes.MainSupported,
		SubProfileToken:  selRes.SubToken,
		SubRTSP:          selRes.SubRTSP,
		SubSupported:     selRes.SubSupported,
		SubIsSameAsMain:  selRes.SubIsSameAsMain,
	}
	s.MediaRepo.UpsertSelection(ctx, dbSel)

	// 5. Trigger Validation
	s.Validator.Enqueue(media.ValidationJob{
		TenantID: tenantID,
		CameraID: cameraID,
		Variant:  "main",
		RTSPURL:  selRes.MainRTSP, // Sanitized
		Username: user,
		Password: pass,
	})

	if !selRes.SubIsSameAsMain {
		s.Validator.Enqueue(media.ValidationJob{
			TenantID: tenantID,
			CameraID: cameraID,
			Variant:  "sub",
			RTSPURL:  selRes.SubRTSP,
			Username: user,
			Password: pass,
		})
	}

	// Audit
	meta, _ := json.Marshal(map[string]interface{}{
		"main": selRes.MainToken,
		"sub":  selRes.SubToken,
	})
	s.Auditor.WriteEvent(ctx, audit.AuditEvent{
		EventID:    uuid.New(),
		Action:     "camera.media.select",
		TenantID:   tenantID,
		TargetID:   cameraID.String(),
		TargetType: "camera",
		Result:     "success",
		Metadata:   meta,
	})

	return dbSel, nil
}

func (s *MediaService) UpdateManualStreamUrls(ctx context.Context, tenantID, cameraID uuid.UUID, mainRTSP, subRTSP string) (*data.CameraStreamSelection, error) {
	mainRTSP = media.SanitizeRTSPURL(strings.TrimSpace(mainRTSP))
	subRTSP = media.SanitizeRTSPURL(strings.TrimSpace(subRTSP))

	cam, err := s.CameraRepo.GetByID(ctx, cameraID)
	if err != nil {
		return nil, err
	}
	if cam.TenantID.String() != tenantID.String() {
		return nil, fmt.Errorf("unauthorized")
	}

	existing, err := s.MediaRepo.GetSelection(ctx, tenantID, cameraID)
	if err != nil {
		return nil, err
	}

	sel := &data.CameraStreamSelection{
		TenantID:         tenantID,
		CameraID:         cameraID,
		MainProfileToken: "",
		MainRTSP:         mainRTSP,
		MainSupported:    strings.TrimSpace(mainRTSP) != "",
		SubProfileToken:  "",
		SubRTSP:          subRTSP,
		SubSupported:     strings.TrimSpace(subRTSP) != "",
		SubIsSameAsMain:  false,
	}

	if existing != nil {
		sel.ID = existing.ID
		if strings.TrimSpace(sel.SubRTSP) == "" && strings.TrimSpace(sel.MainRTSP) != "" {
			sel.SubIsSameAsMain = true
			sel.SubRTSP = sel.MainRTSP
			sel.SubSupported = sel.MainSupported
		} else {
			sel.SubIsSameAsMain = strings.EqualFold(strings.TrimSpace(sel.MainRTSP), strings.TrimSpace(sel.SubRTSP))
		}
	}

	if err := s.MediaRepo.UpsertSelection(ctx, sel); err != nil {
		return nil, err
	}

	if sel.MainRTSP != "" {
		s.Validator.Enqueue(media.ValidationJob{
			TenantID: tenantID,
			CameraID: cameraID,
			Variant:  "main",
			RTSPURL:  sel.MainRTSP,
		})
	}
	if sel.SubRTSP != "" && !sel.SubIsSameAsMain {
		s.Validator.Enqueue(media.ValidationJob{
			TenantID: tenantID,
			CameraID: cameraID,
			Variant:  "sub",
			RTSPURL:  sel.SubRTSP,
		})
	}

	s.Auditor.WriteEvent(ctx, audit.AuditEvent{
		EventID:    uuid.New(),
		Action:     "camera.media.manual_update",
		TenantID:   tenantID,
		TargetID:   cameraID.String(),
		TargetType: "camera",
		Result:     "success",
		Metadata:   toMeta(map[string]any{"main_rtsp_url_sanitized": sel.MainRTSP, "sub_rtsp_url_sanitized": sel.SubRTSP}),
	})

	return sel, nil
}

func (s *MediaService) GetProfiles(ctx context.Context, tenantID, cameraID uuid.UUID) ([]*data.CameraMediaProfile, error) {
	return s.MediaRepo.ListProfiles(ctx, tenantID, cameraID)
}

func (s *MediaService) GetSelection(ctx context.Context, tenantID, cameraID uuid.UUID) (*data.CameraStreamSelection, []*data.RTSPValidationResult, error) {
	sel, err := s.MediaRepo.GetSelection(ctx, tenantID, cameraID)
	if err != nil {
		return nil, nil, err
	}

	if sel == nil {
		log.Printf("[WARN] No cached RTSP selection for camera %s; discovering media profiles", cameraID)
		if refreshed, refreshErr := s.SelectMediaProfiles(ctx, tenantID, cameraID); refreshErr == nil && refreshed != nil {
			sel = refreshed
		} else if refreshErr != nil {
			log.Printf("[WARN] RTSP selection discovery failed for camera %s: %v", cameraID, refreshErr)
		}
	}

	if sel != nil && !s.isManualSelection(sel) && s.selectionNeedsRefresh(ctx, sel) {
		log.Printf("[WARN] RTSP selection probe failed for camera %s; refreshing media selection", cameraID)
		if refreshed, refreshErr := s.SelectMediaProfiles(ctx, tenantID, cameraID); refreshErr == nil && refreshed != nil {
			sel = refreshed
		} else if refreshErr != nil {
			log.Printf("[WARN] RTSP selection refresh failed for camera %s: %v", cameraID, refreshErr)
		}
	}

	val, err := s.MediaRepo.GetValidationResults(ctx, tenantID, cameraID)
	return sel, val, err
}

func (s *MediaService) isManualSelection(sel *data.CameraStreamSelection) bool {
	if sel == nil {
		return false
	}

	return strings.TrimSpace(sel.MainProfileToken) == "" &&
		strings.TrimSpace(sel.SubProfileToken) == "" &&
		(strings.TrimSpace(sel.MainRTSP) != "" || strings.TrimSpace(sel.SubRTSP) != "")
}

func (s *MediaService) selectionNeedsRefresh(ctx context.Context, sel *data.CameraStreamSelection) bool {
	if sel == nil {
		return false
	}

	mainOK := s.probeRTSPPath(ctx, sel.MainRTSP)
	if sel.SubIsSameAsMain {
		return !mainOK
	}

	subOK := s.probeRTSPPath(ctx, sel.SubRTSP)
	return !mainOK && !subOK
}

func (s *MediaService) probeRTSPPath(ctx context.Context, rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}

	probeErr := adapters.ProbeRTSPWithTimeout(ctx, rawURL, 2*time.Second)
	if probeErr == nil {
		return true
	}

	errText := strings.ToLower(probeErr.Error())
	if strings.Contains(errText, "auth_failed") || strings.Contains(errText, "401") || strings.Contains(errText, "403") {
		return true
	}

	return false
}

func (s *MediaService) ValidateRTSP(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	// Re-run validation for current selection
	sel, err := s.MediaRepo.GetSelection(ctx, tenantID, cameraID)
	if err != nil || sel == nil {
		// Attempt auto-selection if missing
		// This makes the validation endpoint robust against race conditions or missing init steps
		sel, err = s.SelectMediaProfiles(ctx, tenantID, cameraID)
		if err != nil {
			return fmt.Errorf("no selection found and auto-selection failed: %w", err)
		}
	}
	if sel.TenantID.String() != tenantID.String() {
		return fmt.Errorf("unauthorized")
	}

	// 2. Fetch Credentials
	out, found, err := s.CredService.GetCredentials(ctx, tenantID, cameraID, true)
	if err != nil {
		// Log but don't error? No, if we can't get creds, we can't validate secured streams.
		// If creds not found, maybe stream is public?
		// But if we have creds, we should use them.
		return fmt.Errorf("failed to get credentials: %w", err)
	}
	var user, pass string
	if found {
		user = out.Data.Username
		pass = out.Data.Password
	}

	s.Validator.Enqueue(media.ValidationJob{
		TenantID: tenantID,
		CameraID: cameraID,
		Variant:  "main",
		RTSPURL:  sel.MainRTSP,
		Username: user,
		Password: pass,
	})

	if !sel.SubIsSameAsMain {
		s.Validator.Enqueue(media.ValidationJob{
			TenantID: tenantID,
			CameraID: cameraID,
			Variant:  "sub",
			RTSPURL:  sel.SubRTSP,
			Username: user,
			Password: pass,
		})
	}

	s.Auditor.WriteEvent(ctx, audit.AuditEvent{
		EventID:    uuid.New(),
		Action:     "camera.media.validate",
		TenantID:   tenantID,
		TargetID:   cameraID.String(),
		TargetType: "camera",
		Result:     "success",
	})

	return nil
}

// Helper
func getHostFromURL(raw string) string {
	// Parse URL
	// Simple approach
	if strings.Contains(raw, "@") {
		// Has auth
		parts := strings.Split(raw, "@")
		if len(parts) > 1 {
			raw = parts[1]
		}
	} else if strings.Contains(raw, "://") {
		parts := strings.Split(raw, "://")
		if len(parts) > 1 {
			raw = parts[1]
		}
	}
	// now raw is host:port/path
	if idx := strings.Index(raw, "/"); idx != -1 {
		raw = raw[:idx]
	}
	return raw
}

func (s *MediaService) SelectProfilesForCodecs(ctx context.Context, tenantID, cameraID uuid.UUID, prefs []media.Codec) (*media.SelectionResult, error) {
	dbProfiles, err := s.GetProfiles(ctx, tenantID, cameraID)
	if err != nil {
		return nil, err
	}
	var profiles []media.Profile
	for _, p := range dbProfiles {
		profiles = append(profiles, media.Profile{
			Token:       p.ProfileToken,
			Name:        p.ProfileName,
			VideoCodec:  media.Codec(p.VideoCodec),
			Width:       p.Width,
			Height:      p.Height,
			FPS:         p.FPS,
			BitrateKbps: p.BitrateKbps,
			RTSPURL:     p.RTSPURLSanitized,
		})
	}
	res := media.SelectProfilesForCodecs(profiles, prefs)
	return &res, nil
}
