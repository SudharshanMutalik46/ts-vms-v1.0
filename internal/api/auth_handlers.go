package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/audit"
	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/middleware"
	"github.com/technosupport/ts-vms/internal/session"
	"github.com/technosupport/ts-vms/internal/tokens"
)

type AuthHandler struct {
	DB      *sql.DB
	Tokens  *tokens.Manager
	Session *session.Manager
	Hasher  *auth.Params
	Audit   *audit.Service
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TenantID string `json:"tenant_id"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RegisterRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	TenantID    string `json:"tenant_id"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"` // Seconds
}

type RegisterResponse struct {
	ID string `json:"id"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Password = strings.TrimSpace(req.Password)
	req.TenantID = strings.TrimSpace(req.TenantID)

	// 1. Check Lockout
	locked, err := h.Session.CheckLockout(r.Context(), req.TenantID, req.Email)
	if err != nil {
		h.genericError(w)
		return
	}
	if locked {
		h.genericError(w)
		return
	}

	// 2. Transaction Scope
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		h.genericError(w)
		return
	}
	defer tx.Rollback()

	// 3. Set Tenant Context for RLS
	if _, err := tx.ExecContext(r.Context(), "SELECT set_tenant_context($1)", req.TenantID); err != nil {
		h.failWithLockout(w, r, req.TenantID, req.Email)
		return
	}

	// 4. Retrieve User
	// users.go UserModel uses *sql.DB, but we are in a transaction *sql.Tx.
	// We need to fix UserModel to accept DBTX or handle Tx.
	// For now, let's cast Tx (This won't work directly if type is *sql.DB).
	// Required refactor: Change UserModel in users.go to use DBTX interface too.
	// Or pass tx if supported.
	// Checking users.go... it uses *sql.DB.
	// We MUST refactor users.go to use DBTX logic from repositories.go (which we deleted the interface for, wait).
	// repositories.go defined DBTX interface in lines 39-43.
	// We should move DBTX to users.go or shared.
	// ... For this step, I will execute the change to AuthHandler assuming DBTX is available, then I will update users.go to use DBTX.
	// Actually, I can't assign *sql.Tx to *sql.DB.
	// So step 4a: Update users.go to use DBTX.
	// 4. Retrieve User
	// 4. Retrieve User
	usersRepo := data.UserModel{DB: tx}
	tID, err := uuid.Parse(req.TenantID)
	if err != nil {
		fmt.Printf("Login Debug: Invalid Tenant UUID: %v\n", err)
		h.genericError(w)
		return
	}

	user, err := usersRepo.GetByEmailOrDisplayName(r.Context(), tID, req.Email)
	if err == data.ErrUserNotFound {
		// Dummy Verify for timing safety
		auth.CheckPassword("dummy", "$argon2id$v=19$m=65536,t=1,p=4$c2FsdHNhbHQ$hashhashhashhashhashhashhashhashhash")
		h.failWithLockout(w, r, req.TenantID, req.Email)
		return
	} else if err != nil {
		h.genericError(w)
		return
	}

	// 5. Verify Password
	match, err := auth.CheckPassword(req.Password, user.PasswordHash)
	if err != nil || !match {
		h.failWithLockout(w, r, req.TenantID, req.Email)
		return
	}

	// 6. Check Disabled
	if user.IsDisabled {
		h.failWithLockout(w, r, req.TenantID, req.Email)
		return
	}

	// 7. Successful Login - Issue Tokens
	sessionID := uuid.New().String()

	// Access Token (User ID and TenantID to String)
	accessToken, err := h.Tokens.GenerateAccessToken(user.ID.String(), req.TenantID)
	if err != nil {
		h.genericError(w)
		return
	}

	// Refresh Token
	tokensRepo := data.TokenModel{DB: tx}
	// TokenModel expects string UserID.
	refreshToken, _, err := tokensRepo.New(r.Context(), user.ID.String(), req.TenantID, sessionID, 7*24*time.Hour)
	if err != nil {
		h.genericError(w)
		return
	}

	// 8. Create Redis Session (async-ish, but safe to fail? Prompt says MUST)
	if err := h.Session.CreateSession(r.Context(), user.ID.String(), req.TenantID, sessionID); err != nil {
		// If redis fails, we should probably fail login or at least log error
		// Fail safe: user logs in but stateless? No, refresh relies on Redis optional?
		// "Redis session layer ... MUST"
		h.genericError(w)
		return
	}

	// Commit Tx
	if err := tx.Commit(); err != nil {
		h.genericError(w)
		return
	}

	// Audit Log
	go func() {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}

		evt := audit.AuditEvent{
			EventID:     uuid.New(),
			TenantID:    tID,
			ActorUserID: &user.ID,
			Action:      "USER_LOGIN",
			TargetType:  "auth",
			TargetID:    "login",
			Result:      "success",
			ClientIP:    ip,
			UserAgent:   r.UserAgent(),
			CreatedAt:   time.Now(),
		}

		// Use a detached background context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h.Audit.WriteEvent(ctx, evt)
	}()

	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    900, // 15 min
	})
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Password = strings.TrimSpace(req.Password)
	req.TenantID = strings.TrimSpace(req.TenantID)

	if req.Email == "" || req.Password == "" || req.TenantID == "" {
		http.Error(w, "missing_fields", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		http.Error(w, "password_too_short", http.StatusBadRequest)
		return
	}
	if !strings.Contains(req.Email, "@") {
		http.Error(w, "invalid_email", http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid_tenant", http.StatusBadRequest)
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), "SELECT set_tenant_context($1)", req.TenantID); err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	usersRepo := data.UserModel{DB: tx}
	_, err = usersRepo.GetByEmail(r.Context(), tenantID, req.Email)
	if err == nil {
		http.Error(w, "email_exists", http.StatusConflict)
		return
	}
	if !errors.Is(err, data.ErrUserNotFound) {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	user := &data.User{
		TenantID:     tenantID,
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		PasswordHash: passwordHash,
		IsDisabled:   false,
	}
	if err := usersRepo.Create(r.Context(), user); err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	// Keep signup role assignment deterministic under concurrency:
	// first 4 signup-assigned users become admin, then everyone gets viewer.
	if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext($1))`, tenantID.String()+":signup-role-allocation"); err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	ensureRoleID := func(roleName string) (uuid.UUID, error) {
		var id uuid.UUID
		err := tx.QueryRowContext(
			r.Context(),
			`SELECT id FROM roles WHERE tenant_id = $1 AND LOWER(name) = LOWER($2) LIMIT 1`,
			tenantID, roleName,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return uuid.Nil, err
		}

		if _, err := tx.ExecContext(
			r.Context(),
			`INSERT INTO roles (id, tenant_id, name, created_at, updated_at)
			 VALUES (gen_random_uuid(), $1, $2, NOW(), NOW())
			 ON CONFLICT DO NOTHING`,
			tenantID, strings.ToLower(roleName),
		); err != nil {
			return uuid.Nil, err
		}

		err = tx.QueryRowContext(
			r.Context(),
			`SELECT id FROM roles WHERE tenant_id = $1 AND LOWER(name) = LOWER($2) LIMIT 1`,
			tenantID, roleName,
		).Scan(&id)
		if err != nil {
			return uuid.Nil, err
		}
		return id, nil
	}

	var adminUsersCount int
	if err := tx.QueryRowContext(
		r.Context(),
		`SELECT COUNT(DISTINCT ur.user_id)
		 FROM user_roles ur
		 JOIN roles r ON r.id = ur.role_id
		 JOIN users u ON u.id = ur.user_id
		 WHERE r.tenant_id = $1
		   AND LOWER(r.name) = 'admin'
		   AND u.deleted_at IS NULL`,
		tenantID,
	).Scan(&adminUsersCount); err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	targetRoleName := "viewer"
	if adminUsersCount < 4 {
		targetRoleName = "admin"
	}

	targetRoleID, err := ensureRoleID(targetRoleName)
	if err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	if _, err := tx.ExecContext(
		r.Context(),
		`INSERT INTO user_roles (user_id, role_id, scope_type, scope_id)
		 VALUES ($1, $2, 'tenant', $3)
		 ON CONFLICT (user_id, role_id, scope_type, scope_id) DO NOTHING`,
		user.ID, targetRoleID, tenantID,
	); err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "register_failed", http.StatusInternalServerError)
		return
	}

	if h.Audit != nil {
		go func() {
			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.Audit.WriteEvent(ctx, audit.AuditEvent{
				EventID:    uuid.New(),
				TenantID:   tenantID,
				Action:     "USER_REGISTER",
				TargetType: "user",
				TargetID:   user.ID.String(),
				Result:     "success",
				ClientIP:   ip,
				UserAgent:  r.UserAgent(),
				CreatedAt:  time.Now(),
			})
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(RegisterResponse{ID: user.ID.String()})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.genericError(w)
		return
	}

	// 1. Validate JWT Format & Sig
	claims, err := h.Tokens.ValidateToken(req.RefreshToken)
	if err != nil || claims.TokenType != tokens.Refresh {
		h.genericError(w)
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		h.genericError(w)
		return
	}
	defer tx.Rollback()

	// 2. Set Tenant Context
	if _, err := tx.ExecContext(r.Context(), "SELECT set_tenant_context($1)", claims.TenantID); err != nil {
		h.genericError(w)
		return
	}

	tokensRepo := data.TokenModel{DB: tx}

	// 3. Lookup in DB (Hash check)
	dbToken, err := tokensRepo.GetByHash(r.Context(), req.RefreshToken)
	if err != nil {
		h.genericError(w)
		return
	}

	// 4. Reuse Detection
	if !dbToken.RevokedAt.IsZero() || dbToken.ReplacedByTokenID != nil {
		// ALARM: Reuse Detected!
		// Revoke ALL tokens for this user
		tokensRepo.RevokeAllForUser(r.Context(), dbToken.UserID)
		h.Session.RevokeAllUserSessions(r.Context(), dbToken.UserID)
		tx.Commit() // Commit the revocation
		h.genericError(w)
		return
	}

	// 5. Rotate
	newSessionID := dbToken.SessionID
	newRefreshToken, newID, err := tokensRepo.New(r.Context(), dbToken.UserID, dbToken.TenantID, newSessionID, 7*24*time.Hour)
	if err != nil {
		h.genericError(w)
		return
	}

	// Link Old -> New
	if err := tokensRepo.Rotate(r.Context(), dbToken.ID, newID); err != nil {
		// Logic mismatch: If rotation fails, should we revoke new?
		// Transaction will rollback everything, so perfectly safe.
		h.genericError(w)
		return
	}

	// 6. Issue Access Token
	newAccess, _ := h.Tokens.GenerateAccessToken(dbToken.UserID, dbToken.TenantID)

	if err := tx.Commit(); err != nil {
		h.genericError(w)
		return
	}

	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  newAccess,
		RefreshToken: newRefreshToken,
		ExpiresIn:    900,
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// 1. Audit Log (Async)
	// Try to get user from context (requires auth middleware)
	user, err := middleware.GetUserFromContext(r.Context())
	if err == nil {
		// Capture values for async goroutine
		uid := user.ID
		tid := user.TenantID
		ip := r.RemoteAddr
		agent := r.UserAgent()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			h.Audit.WriteEvent(ctx, audit.AuditEvent{
				EventID:     uuid.New(),
				TenantID:    tid,
				ActorUserID: &uid,
				Action:      "USER_LOGOUT",
				TargetType:  "auth",
				TargetID:    "logout",
				Result:      "success",
				ClientIP:    ip,
				UserAgent:   agent,
				CreatedAt:   time.Now(),
			})
		}()
	}

	// 2. Clear Session
	// Ideally we blacklist the token or remove it from Redis.
	// For now, client-side discard is sufficient for Phase 1, but let's try to be clean.
	// TODO: Add TokenRevocation check in middleware if not present.

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"logged_out"}`))
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	// 1. Get User from Context (Auth Middleware Required)
	ac, ok := middleware.GetAuthContext(r.Context())
	if !ok {
		h.genericError(w)
		return
	}
	userID, err := uuid.Parse(ac.UserID)
	if err != nil {
		h.genericError(w)
		return
	}
	tenantID, err := uuid.Parse(ac.TenantID) // Should match user's tenant
	if err != nil {
		h.genericError(w)
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.genericError(w)
		return
	}

	if req.NewPassword == "" {
		http.Error(w, "New password required", http.StatusBadRequest)
		return
	}

	// 2. Get User to verify Old Password
	usersRepo := data.UserModel{DB: h.DB}
	user, err := usersRepo.GetByID(r.Context(), userID)
	if err != nil {
		h.genericError(w)
		return
	}

	// Verify Tenant
	if user.TenantID != tenantID {
		h.genericError(w)
		return
	}

	// Verify Old Password
	match, err := auth.CheckPassword(req.OldPassword, user.PasswordHash)
	if err != nil || !match {
		http.Error(w, "Invalid old password", http.StatusUnauthorized)
		return
	}

	// 3. Hash New Password
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	// 4. Update Password
	user.PasswordHash = newHash
	if err := usersRepo.Update(r.Context(), user); err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"password_changed"}`))
}

func (h *AuthHandler) genericError(w http.ResponseWriter) {
	http.Error(w, "Invalid credential or request", http.StatusUnauthorized)
}

func (h *AuthHandler) failWithLockout(w http.ResponseWriter, r *http.Request, tenantID, email string) {
	h.Session.RecordFailedAttempt(r.Context(), tenantID, email)

	// Audit Failure
	go func() {
		tid, _ := uuid.Parse(tenantID)
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h.Audit.WriteEvent(ctx, audit.AuditEvent{
			EventID:    uuid.New(),
			TenantID:   tid,
			Action:     "USER_LOGIN",
			TargetType: "auth",
			TargetID:   "login",
			Result:     "failure",
			ReasonCode: "invalid_credentials",
			ClientIP:   ip,
			UserAgent:  r.UserAgent(),
			CreatedAt:  time.Now(),
		})
	}()

	h.genericError(w)
}
