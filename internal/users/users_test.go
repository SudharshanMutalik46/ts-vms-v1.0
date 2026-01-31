package users_test

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/users"
)

// Helper to provide a real test DB or skip
func getTestDB(t *testing.T) *sql.DB {
	dbURL := "postgres://postgres:postgres@localhost:5432/vms_test?sslmode=disable"
	db, err := sql.Open("postgres", dbURL)
	if err != nil || db.Ping() != nil {
		t.Skip("Skipping integration test: vms_test database not available")
	}
	return db
}

// Helper for Mock Auth
func withMockAuth(req *http.Request) *http.Request {
	ctx := middleware.WithAuthContext(req.Context(), &middleware.AuthContext{
		UserID:   uuid.NewString(),
		TenantID: uuid.NewString(),
		Permissions: map[string]data.PermissionGrant{
			"user.manage": {TenantWide: true},
		},
	})
	return req.WithContext(ctx)
}

// --- DAO Tests (12 Tests) ---

func TestDAO_CreateUser(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	u := &data.User{TenantID: uuid.New(), Email: uuid.NewString() + "@test.com", DisplayName: "Test"}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if u.ID == uuid.Nil {
		t.Error("ID not set")
	}
}

func TestDAO_GetByEmail_Success(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	tid := uuid.New()
	email := uuid.NewString() + "@test.com"
	repo.Create(context.Background(), &data.User{TenantID: tid, Email: email, DisplayName: "Test"})
	u, err := repo.GetByEmail(context.Background(), tid, email)
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != email {
		t.Error("Email mismatch")
	}
}

func TestDAO_GetByEmail_NotFound(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	_, err := repo.GetByEmail(context.Background(), uuid.New(), "none@none.com")
	if err != data.ErrUserNotFound {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestDAO_GetByEmail_SoftDeleted(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	tid := uuid.New()
	email := uuid.NewString() + "@test.com"
	u := &data.User{TenantID: tid, Email: email, DisplayName: "Test"}
	repo.Create(context.Background(), u)
	repo.SoftDelete(context.Background(), u.ID)
	_, err := repo.GetByEmail(context.Background(), tid, email)
	if err != data.ErrUserNotFound {
		t.Error("Should not find soft deleted user")
	}
}

func TestDAO_GetByID_Success(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	u := &data.User{TenantID: uuid.New(), Email: uuid.NewString() + "@test.com", DisplayName: "Test"}
	repo.Create(context.Background(), u)
	u2, err := repo.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u2.ID != u.ID {
		t.Error("ID mismatch")
	}
}

func TestDAO_Update_DisplayName(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	u := &data.User{TenantID: uuid.New(), Email: uuid.NewString() + "@test.com", DisplayName: "Old"}
	repo.Create(context.Background(), u)
	u.DisplayName = "New"
	if err := repo.Update(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	u2, _ := repo.GetByID(context.Background(), u.ID)
	if u2.DisplayName != "New" {
		t.Error("Update failed")
	}
}

func TestDAO_Update_DisabledStatus(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	u := &data.User{TenantID: uuid.New(), Email: uuid.NewString() + "@test.com", IsDisabled: false}
	repo.Create(context.Background(), u)
	u.IsDisabled = true
	repo.Update(context.Background(), u)
	u2, _ := repo.GetByID(context.Background(), u.ID)
	if !u2.IsDisabled {
		t.Error("Status update failed")
	}
}

func TestDAO_List_TenantIsolation(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	t1 := uuid.New()
	t2 := uuid.New()
	repo.Create(context.Background(), &data.User{TenantID: t1, Email: uuid.NewString() + "@t1.com"})
	repo.Create(context.Background(), &data.User{TenantID: t2, Email: uuid.NewString() + "@t2.com"})
	list, _ := repo.List(context.Background(), t1, 10, 0)
	if len(list) < 1 {
		t.Fatal("No users found")
	}
	for _, l := range list {
		if l.TenantID != t1 {
			t.Errorf("Isolation breach: found tenant %v for %v", l.TenantID, t1)
		}
	}
}

func TestDAO_ResetToken_CreateAndGet(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	tid := uuid.New()
	uid := uuid.New()
	tkn := &data.PasswordResetToken{TenantID: tid, UserID: uid, TokenHash: "hash123", ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.CreateResetToken(context.Background(), tkn); err != nil {
		t.Fatal(err)
	}
	t2, err := repo.GetResetToken(context.Background(), "hash123")
	if err != nil {
		t.Fatal(err)
	}
	if t2.UserID != uid {
		t.Error("Token user mismatch")
	}
}

func TestDAO_ResetToken_MarkUsed(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	tkn := &data.PasswordResetToken{TenantID: uuid.New(), UserID: uuid.New(), TokenHash: "hash-used", ExpiresAt: time.Now().Add(time.Hour)}
	repo.CreateResetToken(context.Background(), tkn)
	if err := repo.MarkTokenUsed(context.Background(), tkn.ID); err != nil {
		t.Fatal(err)
	}
	t2, _ := repo.GetResetToken(context.Background(), "hash-used")
	if t2.UsedAt == nil {
		t.Error("Token not marked used")
	}
}

func TestDAO_AssignRole_Idempotency(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	uid := uuid.New()
	rid := uuid.New()
	sid := uuid.New()
	if err := repo.AssignRole(context.Background(), uid, rid, sid, "tenant"); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignRole(context.Background(), uid, rid, sid, "tenant"); err != nil {
		t.Error("Second assign failed (should DO NOTHING)")
	}
}

func TestDAO_GetByEmail_DeletedReuse(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	tid := uuid.New()
	email := "reuse@test.com"
	u1 := &data.User{TenantID: tid, Email: email}
	repo.Create(context.Background(), u1)
	repo.SoftDelete(context.Background(), u1.ID)

	u2 := &data.User{TenantID: tid, Email: email}
	if err := repo.Create(context.Background(), u2); err != nil {
		t.Errorf("Reuse of email after soft delete failed: %v", err)
	}
}

// --- Service Tests ---

func TestService_CreateUser_HashesPassword(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	svc := users.NewService(&repo, nil, nil, nil)
	user := &data.User{TenantID: uuid.New(), Email: uuid.NewString() + "@svc.com"}
	svc.CreateUser(context.Background(), user, "password123", uuid.New())
	if user.PasswordHash == "" || user.PasswordHash == "password123" {
		t.Error("Password not hashed")
	}
}

func TestService_CompleteReset_Success(t *testing.T) {
	db := getTestDB(t)
	repo := data.UserModel{DB: db}
	svc := users.NewService(&repo, nil, nil, nil)
	tid := uuid.New()
	user := &data.User{TenantID: tid, Email: uuid.NewString() + "@reset.com"}
	repo.Create(context.Background(), user)
	token, _ := svc.InitiateReset(context.Background(), user.ID, tid, uuid.New())
	err := svc.CompleteReset(context.Background(), token, "new-password")
	if err != nil {
		t.Fatal(err)
	}
	u2, _ := repo.GetByID(context.Background(), user.ID)
	match, _ := auth.CheckPassword("new-password", u2.PasswordHash)
	if !match {
		t.Error("Password update failed")
	}
}

// --- API Handler Tests ---

func TestHandler_DisableUser_SelfProtection(t *testing.T) {
	uid := uuid.New()
	handler := &api.UserHandler{Service: users.NewService(&data.UserModel{}, nil, nil, nil)}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/users/"+uid.String()+"/disable", nil)
	req.SetPathValue("id", uid.String())

	// Inject uid as actor
	ctx := middleware.WithAuthContext(req.Context(), &middleware.AuthContext{
		UserID:      uid.String(),
		TenantID:    uuid.NewString(),
		Permissions: map[string]data.PermissionGrant{"user.manage": {TenantWide: true}},
	})
	req = req.WithContext(ctx)

	handler.DisableUser(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Error("Should prevent self-disable")
	}
}

func TestHandler_CreateUser_Validation(t *testing.T) {
	handler := &api.UserHandler{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/users", bytes.NewBuffer([]byte(`{"email":""}`)))
	req = withMockAuth(req)
	handler.CreateUser(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Error("Expected 400 for missing email")
	}
}

func TestHandler_AssignRole_ScopeCheck(t *testing.T) {
	handler := &api.UserHandler{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/users/"+uuid.NewString()+"/roles", bytes.NewBuffer([]byte(`{"scope_type":"tenant","scope_id":"`+uuid.NewString()+`"}`)))
	req.SetPathValue("id", uuid.NewString())
	req = withMockAuth(req)

	handler.AssignRole(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Error("Expected 403 for cross-tenant role assignment")
	}
}
