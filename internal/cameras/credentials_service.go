package cameras

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
)

var (
	ErrCredentialTooLarge = errors.New("credential payload exceeds 4KB limit")
	ErrCredentialInvalid  = errors.New("invalid credential format")
	ErrCredentialCrypto   = errors.New("credential encryption/decryption failed") // Generic error
)

const (
	MaxCredentialSize = 4096
	AADPurpose        = "camera_credential_v1"
)

// CredentialUpdater defines dependency on data layer
type CredentialUpdater interface {
	Upsert(ctx context.Context, c *data.CameraCredential) error
	Get(ctx context.Context, cameraID uuid.UUID) (*data.CameraCredential, error)
	Delete(ctx context.Context, cameraID uuid.UUID) error
}

type CredentialService struct {
	repo    CredentialUpdater
	keyring *crypto.Keyring
	auditor Auditor // Using existing Auditor interface from service.go
}

func NewCredentialService(repo CredentialUpdater, keyring *crypto.Keyring, aud Auditor) *CredentialService {
	return &CredentialService{
		repo:    repo,
		keyring: keyring,
		auditor: aud,
	}
}

// CredentialInput is the plaintext payload
type CredentialInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	AuthType string `json:"auth_type,omitempty"` // Default basic
}

// CredentialOutput is the response payload (redacted or revealed)
type CredentialOutput struct {
	Exists    bool             `json:"exists"`
	Refreshed bool             `json:"refreshed,omitempty"` // If re-wrap happened on read (future)
	Data      *CredentialInput `json:"data,omitempty"`      // Only if revealed
	CreatedAt time.Time        `json:"created_at,omitempty"`
}

// SetCredentials encrypts and stores credentials
func (s *CredentialService) SetCredentials(ctx context.Context, tenantID, cameraID uuid.UUID, input CredentialInput) error {
	// 1. Validate Payload Size
	plaintext, err := json.Marshal(input)
	if err != nil {
		return ErrCredentialInvalid
	}
	if len(plaintext) > MaxCredentialSize {
		return ErrCredentialTooLarge
	}

	// 2. Prepare AAD (Binding Context)
	// Bind to Tenant + Camera + Purpose
	// Format: "tenant_uuid:camera_uuid:purpose"
	aad := []byte(fmt.Sprintf("%s:%s:%s", tenantID.String(), cameraID.String(), AADPurpose))

	// 3. Envelope Encryption
	// a. Generate DEK
	dek, err := crypto.GenerateDEK()
	if err != nil {
		return fmt.Errorf("dek gen failed: %w", err)
	}

	// b. Encrypt Data with DEK
	dNonce, dCipher, dTag, err := crypto.EncryptGCM(dek, plaintext, aad)
	if err != nil {
		return fmt.Errorf("data encrypt failed: %w", err)
	}

	// c. Wrap DEK with Active Master Key
	// Wrap AAD: Should match data AAD? Or exclude something?
	// Prompt says: "AAD should include at minimum: tenant_id, camera_id, constant purpose"
	// "AAD should include ... (optional) master_kid for DEK wrap context"
	// Let's use the SAME AAD for both Data and DEK wrapping to force strong binding.
	// This prevents DEK from being moved to another camera context even if unwrapped.
	kid, kNonce, kCipher, kTag, err := s.keyring.WrapDEK(dek, aad)
	if err != nil {
		return fmt.Errorf("key wrap failed: %w", err)
	}

	// 4. Store
	cred := &data.CameraCredential{
		TenantID:       tenantID,
		CameraID:       cameraID,
		MasterKID:      kid,
		DEKNonce:       kNonce,
		DEKCiphertext:  kCipher,
		DEKTag:         kTag,
		DataNonce:      dNonce,
		DataCiphertext: dCipher,
		DataTag:        dTag,
	}

	if err := s.repo.Upsert(ctx, cred); err != nil {
		return err
	}

	// 5. Audit
	s.auditor.WriteEvent(ctx, audit.AuditEvent{
		TenantID:   tenantID,
		EventID:    uuid.New(),
		Action:     "camera.credential.write",
		Result:     "success",
		TargetID:   cameraID.String(),
		TargetType: "camera",
		CreatedAt:  time.Now(),
		Metadata:   toMeta(map[string]any{"kid": kid}), // Safe metadata
	})

	return nil
}

// GetCredentials retrieves (and optionally decrypts) credentials
// Returns (output, found, error). If not found, output is nil, found is false, err is nil.
func (s *CredentialService) GetCredentials(ctx context.Context, tenantID, cameraID uuid.UUID, reveal bool) (*CredentialOutput, bool, error) {
	// 1. Retrieve
	c, err := s.repo.Get(ctx, cameraID)
	if err != nil {
		if errors.Is(err, data.ErrCredentialNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	// 2. Validate Tenant Isolation (Model should handle RLS, but double check)
	if c.TenantID != tenantID {
		// Log potential breach attempt?
		return nil, false, data.ErrCredentialNotFound // Pretend not found
	}

	out := &CredentialOutput{
		Exists:    true,
		CreatedAt: c.CreatedAt,
	}

	// 3. Decrypt if Revealed
	if reveal {
		aad := []byte(fmt.Sprintf("%s:%s:%s", tenantID.String(), cameraID.String(), AADPurpose))

		// a. Unwrap DEK
		dek, err := s.keyring.UnwrapDEK(c.MasterKID, c.DEKNonce, c.DEKCiphertext, c.DEKTag, aad)
		if err != nil {
			// Do not leak crypto error details
			s.logCryptoError("unwrap", c.MasterKID, err)
			return nil, true, ErrCredentialCrypto
		}

		// b. Decrypt Data
		plaintext, err := crypto.DecryptGCM(dek, c.DataNonce, c.DataCiphertext, c.DataTag, aad)
		if err != nil {
			s.logCryptoError("decrypt_data", c.MasterKID, err)
			return nil, true, ErrCredentialCrypto
		}

		var input CredentialInput
		if err := json.Unmarshal(plaintext, &input); err != nil {
			return nil, true, ErrCredentialCrypto
		}
		out.Data = &input
	}

	// 4. Audit
	action := "camera.credential.read"
	meta := map[string]any{"revealed": reveal}

	s.auditor.WriteEvent(ctx, audit.AuditEvent{
		TenantID:   tenantID,
		EventID:    uuid.New(),
		Action:     action,
		Result:     "success",
		TargetID:   cameraID.String(),
		TargetType: "camera",
		CreatedAt:  time.Now(),
		Metadata:   toMeta(meta),
	})

	return out, true, nil
}

// DeleteCredentials removes the record
func (s *CredentialService) DeleteCredentials(ctx context.Context, tenantID, cameraID uuid.UUID) error {
	// Check existence first? Or just delete?
	// Delete is idempotent usually.
	// But we need to audit ONLY if it existed? Or attempts?
	// Spec says "All credential accesses are audited".
	// DELETE usually implies "Delete if exists".
	// Let's try Delete.
	err := s.repo.Delete(ctx, cameraID)
	found := true
	if err != nil {
		if errors.Is(err, data.ErrCredentialNotFound) {
			found = false
			err = nil // Not an error to delete missing
		} else {
			return err
		}
	}

	if found {
		s.auditor.WriteEvent(ctx, audit.AuditEvent{
			TenantID:   tenantID,
			EventID:    uuid.New(),
			Action:     "camera.credential.delete",
			Result:     "success",
			TargetID:   cameraID.String(),
			TargetType: "camera",
			CreatedAt:  time.Now(),
		})
	}

	return nil
}

func (s *CredentialService) logCryptoError(stage, kid string, err error) {
	// TODO: log info via logger interface if available.
	// fmt.Printf("Crypto Error [%s] KID=%s: %v\n", stage, kid, err)
}
