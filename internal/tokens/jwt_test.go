package tokens_test

import (
	"testing"

	"github.com/technosupport/ts-vms/internal/tokens"
)

func TestTokenGeneration(t *testing.T) {
	mgr := tokens.NewManager("test-secret-key")
	userID := "user-123"
	tenantID := "tenant-abc"

	token, err := mgr.GenerateAccessToken(userID, tenantID)
	if err != nil {
		t.Fatalf("Failed to generate access token: %v", err)
	}

	claims, err := mgr.ValidateToken(token)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, claims.UserID)
	}
	if claims.TenantID != tenantID {
		t.Errorf("Expected TenantID %s, got %s", tenantID, claims.TenantID)
	}
	if claims.TokenType != tokens.Access {
		t.Errorf("Expected TokenType %s, got %s", tokens.Access, claims.TokenType)
	}
}

func TestInvalidSignature(t *testing.T) {
	mgr1 := tokens.NewManager("secret-1")
	mgr2 := tokens.NewManager("secret-2")

	token, _ := mgr1.GenerateAccessToken("u1", "t1")
	_, err := mgr2.ValidateToken(token)
	if err == nil {
		t.Error("Expected validation error for wrong signature")
	}
}
