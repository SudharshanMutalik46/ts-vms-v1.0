package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrTokenNotFound  = errors.New("reset token not found")
	ErrEmailDuplicate = errors.New("email already exists")
	ErrTokenExpired   = errors.New("reset token expired")
	ErrTokenUsed      = errors.New("reset token already used")
	ErrOptimisticLock = errors.New("optimistic lock failure")
)

type User struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Email             string
	DisplayName       string
	PasswordHash      string
	IsDisabled        bool
	PasswordUpdatedAt time.Time // Legacy field for Auth
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

type PasswordResetToken struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	UserID          uuid.UUID
	TokenHash       string
	ExpiresAt       time.Time
	UsedAt          *time.Time
	CreatedByUserID *uuid.UUID
	CreatedAt       time.Time
}

type UserModel struct {
	DB DBTX
}

// GetByEmail retrieves a user by email, strictly respecting Soft Delete
func (m UserModel) GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*User, error) {
	query := `
		SELECT id, tenant_id, email, display_name, password_hash, is_disabled, created_at, updated_at, deleted_at
		FROM users
		WHERE tenant_id = $1 AND email = $2 AND deleted_at IS NULL
	`
	var u User
	err := m.DB.QueryRowContext(ctx, query, tenantID, email).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsDisabled, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

// GetByID retrieves a user by ID, strictly respecting Soft Delete
func (m UserModel) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	// Note: We don't filter by tenantID here, caller must enforce RBAC or check u.TenantID matches
	query := `
		SELECT id, tenant_id, email, display_name, password_hash, is_disabled, created_at, updated_at, deleted_at
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`
	var u User
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsDisabled, &u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

// Create inserts a new user
func (m UserModel) Create(ctx context.Context, u *User) error {
	query := `
		INSERT INTO users (tenant_id, email, display_name, password_hash, is_disabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`
	err := m.DB.QueryRowContext(ctx, query, u.TenantID, u.Email, u.DisplayName, u.PasswordHash, u.IsDisabled).Scan(
		&u.ID, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		// Postgres specific check for unique violation code 23505 could be done here
		return err
	}
	return nil
}

// Update changes user details
func (m UserModel) Update(ctx context.Context, u *User) error {
	query := `
		UPDATE users
		SET display_name = $1, is_disabled = $2, password_hash = $3, updated_at = NOW()
		WHERE id = $4 AND deleted_at IS NULL
		RETURNING updated_at
	`
	err := m.DB.QueryRowContext(ctx, query, u.DisplayName, u.IsDisabled, u.PasswordHash, u.ID).Scan(&u.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// SoftDelete sets deleted_at
func (m UserModel) SoftDelete(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE users
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`
	res, err := m.DB.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// List retrieves users with pagination
func (m UserModel) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*User, error) {
	query := `
		SELECT id, tenant_id, email, display_name, is_disabled, created_at
		FROM users
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := m.DB.QueryContext(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.IsDisabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, &u)
	}
	return users, nil
}

// --- Password Reset Tokens ---

func (m UserModel) CreateResetToken(ctx context.Context, t *PasswordResetToken) error {
	query := `
		INSERT INTO password_reset_tokens (tenant_id, user_id, token_hash, expires_at, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	return m.DB.QueryRowContext(ctx, query, t.TenantID, t.UserID, t.TokenHash, t.ExpiresAt, t.CreatedByUserID).Scan(&t.ID, &t.CreatedAt)
}

func (m UserModel) GetResetToken(ctx context.Context, hash string) (*PasswordResetToken, error) {
	query := `
		SELECT id, tenant_id, user_id, token_hash, expires_at, used_at
		FROM password_reset_tokens
		WHERE token_hash = $1
	`
	var t PasswordResetToken
	err := m.DB.QueryRowContext(ctx, query, hash).Scan(
		&t.ID, &t.TenantID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.UsedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (m UserModel) MarkTokenUsed(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE password_reset_tokens
		SET used_at = NOW()
		WHERE id = $1 AND used_at IS NULL
	`
	res, err := m.DB.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrTokenUsed // Or not found
	}
	return nil
}

// AssignRole sets a user role (Basic Insert, assuming idempotency check in Service)
func (m UserModel) AssignRole(ctx context.Context, userID, roleID, scopeID uuid.UUID, scopeType string) error {
	query := `
		INSERT INTO user_roles (user_id, role_id, scope_type, scope_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, role_id, scope_type, scope_id) DO NOTHING
	`
	_, err := m.DB.ExecContext(ctx, query, userID, roleID, scopeType, scopeID)
	return err
}
