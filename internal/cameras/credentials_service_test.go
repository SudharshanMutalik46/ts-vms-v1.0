package cameras_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/cameras"
	"github.com/technosupport/ts-vms/internal/crypto"
	"github.com/technosupport/ts-vms/internal/data"
)

type MockCredRepo struct {
	Store map[string]*data.CameraCredential
}

func (m *MockCredRepo) Upsert(ctx context.Context, c *data.CameraCredential) error {
	m.Store[c.CameraID.String()] = c
	return nil
}

func (m *MockCredRepo) Get(ctx context.Context, cameraID uuid.UUID) (*data.CameraCredential, error) {
	c, ok := m.Store[cameraID.String()]
	if !ok {
		return nil, data.ErrCredentialNotFound
	}
	return c, nil
}

func (m *MockCredRepo) Delete(ctx context.Context, cameraID uuid.UUID) error {
	delete(m.Store, cameraID.String())
	return nil
}

func TestSetCredentials(t *testing.T) {
	// Setup Helper
	repo := &MockCredRepo{Store: make(map[string]*data.CameraCredential)}
	aud := &MockCredAuditor{} // Assuming MockAuditor structure from other tests or redefine

	// Setup Keyring
	key, _ := crypto.GenerateDEK()
	keyStr := base64.StdEncoding.EncodeToString(key)
	t.Setenv("MASTER_KEYS", `[{"kid":"test-v1","material":"`+keyStr+`"}]`)
	t.Setenv("ACTIVE_MASTER_KID", "test-v1")
	kr := crypto.NewKeyring()
	kr.LoadFromEnv()

	svc := cameras.NewCredentialService(repo, kr, aud)

	// Case 1: Success
	tenantID := uuid.New()
	camID := uuid.New()
	input := cameras.CredentialInput{Username: "admin", Password: "secretPassword"}

	if err := svc.SetCredentials(context.Background(), tenantID, camID, input); err != nil {
		t.Fatalf("SetCredentials failed: %v", err)
	}

	// Verify DB Store
	stored, _ := repo.Get(context.Background(), camID)
	if stored == nil {
		t.Fatal("Credential not stored")
	}
	if stored.MasterKID != "test-v1" {
		t.Error("Wrong Master KID used")
	}

	// Case 2: Payload too large
	longInput := cameras.CredentialInput{Password: string(make([]byte, 5000))} // Exceeds 4KB
	if err := svc.SetCredentials(context.Background(), tenantID, camID, longInput); err != cameras.ErrCredentialTooLarge {
		t.Errorf("Expected too large error, got %v", err)
	}
}

func TestGetCredentials(t *testing.T) {
	repo := &MockCredRepo{Store: make(map[string]*data.CameraCredential)}
	aud := &MockCredAuditor{}
	key, _ := crypto.GenerateDEK()
	keyStr := base64.StdEncoding.EncodeToString(key)
	t.Setenv("MASTER_KEYS", `[{"kid":"test-v1","material":"`+keyStr+`"}]`)
	t.Setenv("ACTIVE_MASTER_KID", "test-v1")
	kr := crypto.NewKeyring()
	kr.LoadFromEnv()
	svc := cameras.NewCredentialService(repo, kr, aud)

	tenantID := uuid.New()
	camID := uuid.New()

	// Pre-seed
	svc.SetCredentials(context.Background(), tenantID, camID, cameras.CredentialInput{Username: "u", Password: "p"})

	// Case 1: Get Redacted
	out, found, err := svc.GetCredentials(context.Background(), tenantID, camID, false)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found || out.Data != nil {
		t.Error("Should be found but redacted")
	}
	if !out.Exists {
		t.Error("Should report exists")
	}

	// Case 2: Get Revealed
	out, found, err = svc.GetCredentials(context.Background(), tenantID, camID, true)
	if err != nil {
		t.Fatalf("Get Reveal failed: %v", err)
	}
	if out.Data == nil || out.Data.Username != "u" {
		t.Error("Reveal failed decreption")
	}

	// Case 3: Wrong Tenant (Isolation)
	otherTenant := uuid.New()
	repo.Store[camID.String()].TenantID = otherTenant // Tamper DB record ownership
	_, _, err = svc.GetCredentials(context.Background(), tenantID, camID, false)
	if err != data.ErrCredentialNotFound {
		t.Error("Should return not found for wrong tenant")
	}
}

// Minimal Mock Auditor (if not shared)
type MockCredAuditor struct {
	Events []audit.AuditEvent
}

func (m *MockCredAuditor) WriteEvent(ctx context.Context, evt audit.AuditEvent) error {
	m.Events = append(m.Events, evt)
	return nil
}
