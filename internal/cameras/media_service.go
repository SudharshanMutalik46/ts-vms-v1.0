package cameras

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/discovery"
	"github.com/technosupport/ts-vms/internal/media"
)

type MediaRepository interface {
	UpsertProfile(ctx context.Context, p *data.CameraMediaProfile) error
	UpsertSelection(ctx context.Context, s *data.CameraStreamSelection) error
	GetSelection(ctx context.Context, cameraID uuid.UUID) (*data.CameraStreamSelection, error)
	GetValidationResults(ctx context.Context, cameraID uuid.UUID) ([]*data.RTSPValidationResult, error)
	UpsertValidationResult(ctx context.Context, res *data.RTSPValidationResult) error
	ListProfiles(ctx context.Context, cameraID uuid.UUID) ([]*data.CameraMediaProfile, error)
}

type CredentialProvider interface {
	GetCredentials(ctx context.Context, tenantID, cameraID uuid.UUID, reveal bool) (*CredentialOutput, bool, error)
}

type OnvifClient interface {
	GetCapabilities(ctx context.Context) (map[string]bool, string, error)
	GetProfiles(ctx context.Context, mediaURI string) ([]discovery.MediaProfile, error)
	GetStreamUri(ctx context.Context, mediaURI, token string) (string, error)
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
			return discovery.NewOnvifClient(x, u, p)
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
	// Phase 2.1 Camera struct has IPAddress (net.IP).
	// We need to construct URL from IP.
	host := cam.IPAddress.String()
	xaddr := fmt.Sprintf("http://%s/onvif/device_service", host)

	client, err := s.ClientFactory(xaddr, user, pass)
	if err != nil {
		return nil, fmt.Errorf("failed to init onvif client: %w", err)
	}

	// Get Capabilities/Media URI
	_, mediaURI, err := client.GetCapabilities(ctx)
	if err != nil {
		// Log warning, try default
	}
	if mediaURI == "" {
		mediaURI = xaddr
	}

	// Get Profiles
	onvifProfiles, err := client.GetProfiles(ctx, mediaURI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profiles: %w", err)
	}

	// 3. Normalize & Store
	var domainProfiles []media.Profile
	for _, op := range onvifProfiles {
		// Get Stream URI for each
		uri, err := client.GetStreamUri(ctx, mediaURI, op.Token)
		if err != nil {
			continue // Skip broken profiles
		}

		// Sanitize
		sanitizedURI := media.SanitizeRTSPURL(uri)

		// Codec Mapping
		codec := media.CodecUnknown
		enc := strings.ToUpper(op.VideoEncoderConfiguration.Encoding)
		if strings.Contains(enc, "H264") {
			codec = media.CodecH264
		} else if strings.Contains(enc, "H265") {
			codec = media.CodecH265
		} else if strings.Contains(enc, "JPEG") {
			codec = media.CodecMJPEG
		}

		p := media.Profile{
			Token:      op.Token,
			Name:       op.Name,
			VideoCodec: codec,
			Width:      op.VideoEncoderConfiguration.Resolution.Width,
			Height:     op.VideoEncoderConfiguration.Resolution.Height,
			RTSPURL:    uri, // Raw needed for Selector? No, Selector just passes it through.
			// But Selector should ideally work on objects.
			// Wait, we need to store Sanitized in DB.
			// The Validator needs Raw (with creds injected) OR Creds separately.
			// Validator takes (User, Pass, SanitizedURL).
			// So we store Sanitized.
		}
		p.RTSPURL = sanitizedURI // Store sanitized in struct used for selection

		domainProfiles = append(domainProfiles, p)

		// Persist Normalized
		dbP := &data.CameraMediaProfile{
			TenantID:         tenantID,
			CameraID:         cameraID,
			ProfileToken:     p.Token,
			ProfileName:      p.Name,
			VideoCodec:       string(p.VideoCodec),
			Width:            p.Width,
			Height:           p.Height,
			RTSPURLSanitized: sanitizedURI,
		}
		s.MediaRepo.UpsertProfile(ctx, dbP)
	}

	// 4. Run Selection
	selRes := media.SelectProfiles(domainProfiles)

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

func (s *MediaService) GetProfiles(ctx context.Context, cameraID uuid.UUID) ([]*data.CameraMediaProfile, error) {
	return s.MediaRepo.ListProfiles(ctx, cameraID)
}

func (s *MediaService) GetSelection(ctx context.Context, cameraID uuid.UUID) (*data.CameraStreamSelection, []*data.RTSPValidationResult, error) {
	sel, err := s.MediaRepo.GetSelection(ctx, cameraID)
	if err != nil {
		return nil, nil, err
	}
	val, err := s.MediaRepo.GetValidationResults(ctx, cameraID)
	return sel, val, err
}

func (s *MediaService) ValidateRTSP(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	// Re-run validation for current selection
	sel, err := s.MediaRepo.GetSelection(ctx, cameraID)
	if err != nil || sel == nil {
		return fmt.Errorf("no selection found")
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
