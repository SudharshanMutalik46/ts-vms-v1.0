package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrRecordNotFound = errors.New("record not found")
)

type Token struct {
	ID                string
	TenantID          string
	UserID            string
	TokenHash         string
	SessionID         string
	ExpiresAt         time.Time
	RevokedAt         time.Time
	ReplacedByTokenID *string
}

// DBTX is a common interface for *sql.DB and *sql.Tx
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type TokenModel struct {
	DB DBTX
}

func (m TokenModel) New(ctx context.Context, userID, tenantID, sessionID string, ttl time.Duration) (string, string, error) {
	tokenPlain := uuid.New().String()
	hash := sha256.Sum256([]byte(tokenPlain))
	hashString := hex.EncodeToString(hash[:])

	id := uuid.New().String()
	expiresAt := time.Now().Add(ttl).UTC()

	query := `
		INSERT INTO refresh_tokens (id, tenant_id, user_id, token_hash, session_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := m.DB.ExecContext(ctx, query, id, tenantID, userID, hashString, sessionID, expiresAt)
	if err != nil {
		return "", "", err
	}
	return tokenPlain, id, nil
}

func (m TokenModel) GetByHash(ctx context.Context, tokenPlain string) (*Token, error) {
	hash := sha256.Sum256([]byte(tokenPlain))
	hashString := hex.EncodeToString(hash[:])

	query := `
		SELECT id, tenant_id, user_id, token_hash, session_id, expires_at, revoked_at, replaced_by_token_id
		FROM refresh_tokens
		WHERE token_hash = $1`

	var t Token
	var revokedAt sql.NullTime
	var replacedBy sql.NullString

	err := m.DB.QueryRowContext(ctx, query, hashString).Scan(
		&t.ID, &t.TenantID, &t.UserID, &t.TokenHash, &t.SessionID, &t.ExpiresAt, &revokedAt, &replacedBy,
	)

	if err == sql.ErrNoRows {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}

	if revokedAt.Valid {
		t.RevokedAt = revokedAt.Time
	}
	if replacedBy.Valid {
		t.ReplacedByTokenID = &replacedBy.String
	}

	return &t, nil
}

func (m TokenModel) Rotate(ctx context.Context, oldTokenID, newTokenID string) error {
	query := `
		UPDATE refresh_tokens
		SET revoked_at = (NOW() AT TIME ZONE 'UTC'), replaced_by_token_id = $1
		WHERE id = $2`
	_, err := m.DB.ExecContext(ctx, query, newTokenID, oldTokenID)
	return err
}

func (m TokenModel) RevokeAllForUser(ctx context.Context, userID string) error {
	query := `
		UPDATE refresh_tokens
		SET revoked_at = (NOW() AT TIME ZONE 'UTC')
		WHERE user_id = $1 AND revoked_at IS NULL`
	_, err := m.DB.ExecContext(ctx, query, userID)
	return err
}
