package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCredentialNotFound = errors.New("credentials not found")
)

type CameraCredential struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	CameraID       uuid.UUID
	MasterKID      string
	DEKNonce       []byte
	DEKCiphertext  []byte
	DEKTag         []byte
	DataNonce      []byte
	DataCiphertext []byte
	DataTag        []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CredentialModel struct {
	DB *sql.DB // Can verify if DBTX interface is needed, for now standard DB
}

func (m CredentialModel) Get(ctx context.Context, cameraID uuid.UUID) (*CameraCredential, error) {
	query := `
		SELECT id, tenant_id, camera_id, master_kid, 
		       dek_nonce, dek_ciphertext, dek_tag, 
		       data_nonce, data_ciphertext, data_tag, 
		       created_at, updated_at
		FROM camera_credentials
		WHERE camera_id = $1
	`
	var c CameraCredential
	err := m.DB.QueryRowContext(ctx, query, cameraID).Scan(
		&c.ID, &c.TenantID, &c.CameraID, &c.MasterKID,
		&c.DEKNonce, &c.DEKCiphertext, &c.DEKTag,
		&c.DataNonce, &c.DataCiphertext, &c.DataTag,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (m CredentialModel) Upsert(ctx context.Context, c *CameraCredential) error {
	// Assumes RLS is active on session (tenant_id check enforced by DB or Middleware context setting)
	// We pass tenant_id explicitly for INSERT, and check it matches constraint.
	query := `
		INSERT INTO camera_credentials (
			tenant_id, camera_id, master_kid, 
			dek_nonce, dek_ciphertext, dek_tag, 
			data_nonce, data_ciphertext, data_tag,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (camera_id) DO UPDATE SET
			master_kid = EXCLUDED.master_kid,
			dek_nonce = EXCLUDED.dek_nonce,
			dek_ciphertext = EXCLUDED.dek_ciphertext,
			dek_tag = EXCLUDED.dek_tag,
			data_nonce = EXCLUDED.data_nonce,
			data_ciphertext = EXCLUDED.data_ciphertext,
			data_tag = EXCLUDED.data_tag,
			updated_at = NOW()
		RETURNING id, created_at, updated_at
	`
	return m.DB.QueryRowContext(ctx, query,
		c.TenantID, c.CameraID, c.MasterKID,
		c.DEKNonce, c.DEKCiphertext, c.DEKTag,
		c.DataNonce, c.DataCiphertext, c.DataTag,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (m CredentialModel) Delete(ctx context.Context, cameraID uuid.UUID) error {
	query := `DELETE FROM camera_credentials WHERE camera_id = $1`
	res, err := m.DB.ExecContext(ctx, query, cameraID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrCredentialNotFound
	}
	return nil
}
