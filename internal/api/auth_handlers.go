package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/technosupport/ts-vms/internal/auth"
	"github.com/technosupport/ts-vms/internal/data"
	"github.com/technosupport/ts-vms/internal/session"
	"github.com/technosupport/ts-vms/internal/tokens"
)

type AuthHandler struct {
	DB      *sql.DB
	Tokens  *tokens.Manager
	Session *session.Manager
	Hasher  *auth.Params
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TenantID string `json:"tenant_id"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"` // Seconds
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

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
	usersRepo := data.UserModel{DB: tx}
	// Note: Auth Handler uses string TenantID, User Repo expects UUID.
	tID, err := uuid.Parse(req.TenantID)
	if err != nil {
		h.genericError(w)
		return
	}

	user, err := usersRepo.GetByEmail(r.Context(), tID, req.Email)
	if err == data.ErrUserNotFound { // Should probably use ErrUserNotFound from users.go
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

	json.NewEncoder(w).Encode(TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    900, // 15 min
	})
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
	// ... Logic ...
}

func (h *AuthHandler) genericError(w http.ResponseWriter) {
	http.Error(w, "Invalid credential or request", http.StatusUnauthorized)
}

func (h *AuthHandler) failWithLockout(w http.ResponseWriter, r *http.Request, tenantID, email string) {
	h.Session.RecordFailedAttempt(r.Context(), tenantID, email)
	h.genericError(w)
}
