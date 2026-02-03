package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/session"
	"github.com/technosupport/ts-vms/internal/tokens"
)

// Mock DBTX
type MockDB struct {
	// Simple mock or use sqlmock
}

func TestLoginHandler(t *testing.T) {
	// Setup Dependencies
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	defer db.Close()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	sessionMgr := session.NewManager(mr.Addr(), "")
	tokenMgr := tokens.NewManager("test-key")
	handler := &api.AuthHandler{
		DB:      db,
		Session: sessionMgr,
		Tokens:  tokenMgr,
	}

	// Prepare Request
	reqBody := map[string]string{
		"email":     "test@example.com",
		"password":  "password123",
		"tenant_id": "00000000-0000-0000-0000-000000000001",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	// Mock Expectations
	// 1. Set Tenant Context
	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_tenant_context").WithArgs("00000000-0000-0000-0000-000000000001").WillReturnResult(sqlmock.NewResult(0, 0))

	// 2. Get User
	// 2. Get User
	hashedPassword, _ := auth.HashPassword("password123")
	rows := sqlmock.NewRows([]string{"id", "tenant_id", "email", "display_name", "password_hash", "is_disabled", "created_at", "updated_at", "deleted_at"}).
		AddRow("00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000001", "test@example.com", "Test User", hashedPassword, false, time.Now(), time.Now(), nil)
	mock.ExpectQuery("SELECT id, tenant_id, email").WithArgs("00000000-0000-0000-0000-000000000001", "test@example.com").WillReturnRows(rows)

	// 3. Insert Refresh Token
	mock.ExpectExec("INSERT INTO refresh_tokens").WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	// Execute
	handler.Login(w, req)

	// Verify
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var tokenResp api.TokenResponse
	json.NewDecoder(resp.Body).Decode(&tokenResp)
	if tokenResp.AccessToken == "" {
		t.Error("Expected Access Token")
	}
}
