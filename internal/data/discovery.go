package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrRunNotFound    = errors.New("discovery run not found")
	ErrDeviceNotFound = errors.New("discovered device not found")
)

type DiscoveryRun struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	SiteID      *uuid.UUID `json:"site_id,omitempty"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	DeviceCount int        `json:"device_count"`
	ErrorCount  int        `json:"error_count"`
}

type DiscoveredDevice struct {
	ID              uuid.UUID `json:"id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	DiscoveryRunID  uuid.UUID `json:"discovery_run_id"`
	IPAddress       string    `json:"ip_address"`
	EndpointRef     string    `json:"endpoint_ref,omitempty"`
	Manufacturer    string    `json:"manufacturer,omitempty"`
	Model           string    `json:"model,omitempty"`
	FirmwareVersion string    `json:"firmware_version,omitempty"`
	SerialNumber    string    `json:"serial_number,omitempty"`

	SupportsProfileS bool `json:"supports_profile_s"`
	SupportsProfileT bool `json:"supports_profile_t"`
	SupportsProfileG bool `json:"supports_profile_g"`

	Capabilities  json.RawMessage `json:"capabilities,omitempty"`
	MediaProfiles json.RawMessage `json:"media_profiles,omitempty"`
	RTSP_URIs     json.RawMessage `json:"rtsp_uris,omitempty"`

	LastProbeAt   *time.Time `json:"last_probe_at,omitempty"`
	LastErrorCode string     `json:"last_error_code,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DiscoveryModel struct {
	DB *sql.DB
}

// Runs
func (m *DiscoveryModel) CreateRun(ctx context.Context, run *DiscoveryRun) error {
	query := `
		INSERT INTO onvif_discovery_runs (tenant_id, site_id, status)
		VALUES ($1, $2, $3)
		RETURNING id, started_at
	`
	return m.DB.QueryRowContext(ctx, query, run.TenantID, run.SiteID, run.Status).Scan(&run.ID, &run.StartedAt)
}

func (m *DiscoveryModel) UpdateRunStatus(ctx context.Context, id uuid.UUID, status string, finished bool, dCount, eCount int) error {
	query := `
		UPDATE onvif_discovery_runs 
		SET status = $2, device_count = $3, error_count = $4, finished_at = CASE WHEN $5 THEN NOW() ELSE finished_at END
		WHERE id = $1
	`
	_, err := m.DB.ExecContext(ctx, query, id, status, dCount, eCount, finished)
	return err
}

func (m *DiscoveryModel) GetRun(ctx context.Context, id uuid.UUID) (*DiscoveryRun, error) {
	query := `
		SELECT id, tenant_id, site_id, status, started_at, finished_at, device_count, error_count
		FROM onvif_discovery_runs WHERE id = $1
	`
	var r DiscoveryRun
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&r.ID, &r.TenantID, &r.SiteID, &r.Status, &r.StartedAt, &r.FinishedAt, &r.DeviceCount, &r.ErrorCount,
	)
	if err == sql.ErrNoRows {
		return nil, ErrRunNotFound
	}
	return &r, err
}

// Devices
func (m *DiscoveryModel) UpsertDevice(ctx context.Context, d *DiscoveredDevice) error {
	// De-dupe on RunID + IP: Usually we probe once per run, OR we want latest for that run.
	// Actually schema doesn't have unique constraint on (discovery_run_id, ip_address), only index.
	// But logically we should update if exists in THIS run?
	// Or just INSERT?
	// Recommendation: Upsert to avoid dupes if scan finds same device multiple times (e.g. multiple XAddrs).
	// But `onvif_discovered_devices` primary key is ID.
	// We'll rely on service to check existence or just INSERT new.
	// Let's implement Upsert based on DiscoveryRunID + IP.
	// If schema lacks unique constraint, ON CONFLICT won't work well without it.
	// We'll trust the User requirement "De-dupe by endpoint reference OR IP".
	// The service layer handles de-dupe memory-side per scan.
	// So DB can just INSERT.

	query := `
		INSERT INTO onvif_discovered_devices (
			tenant_id, discovery_run_id, ip_address, endpoint_ref,
			manufacturer, model, firmware_version, serial_number,
			supports_profile_s, supports_profile_t, supports_profile_g,
			capabilities, media_profiles, rtsp_uris,
			last_probe_at, last_error_code
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id, created_at, updated_at
	`
	return m.DB.QueryRowContext(ctx, query,
		d.TenantID, d.DiscoveryRunID, d.IPAddress, d.EndpointRef,
		d.Manufacturer, d.Model, d.FirmwareVersion, d.SerialNumber,
		d.SupportsProfileS, d.SupportsProfileT, d.SupportsProfileG,
		d.Capabilities, d.MediaProfiles, d.RTSP_URIs,
		d.LastProbeAt, d.LastErrorCode,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

func (m *DiscoveryModel) UpdateDeviceProbe(ctx context.Context, d *DiscoveredDevice) error {
	query := `
		UPDATE onvif_discovered_devices
		SET manufacturer=$2, model=$3, firmware_version=$4, serial_number=$5,
		    supports_profile_s=$6, supports_profile_t=$7, supports_profile_g=$8,
		    capabilities=$9, media_profiles=$10, rtsp_uris=$11,
		    last_probe_at=$12, last_error_code=$13, updated_at=NOW()
		WHERE id=$1
	`
	res, err := m.DB.ExecContext(ctx, query,
		d.ID, d.Manufacturer, d.Model, d.FirmwareVersion, d.SerialNumber,
		d.SupportsProfileS, d.SupportsProfileT, d.SupportsProfileG,
		d.Capabilities, d.MediaProfiles, d.RTSP_URIs,
		d.LastProbeAt, d.LastErrorCode,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

func (m *DiscoveryModel) GetDevice(ctx context.Context, id uuid.UUID) (*DiscoveredDevice, error) {
	query := `
		SELECT id, tenant_id, discovery_run_id, ip_address, endpoint_ref,
		       manufacturer, model, firmware_version, serial_number,
		       supports_profile_s, supports_profile_t, supports_profile_g,
		       capabilities, media_profiles, rtsp_uris,
		       last_probe_at, last_error_code, created_at, updated_at
		FROM onvif_discovered_devices WHERE id = $1
	`
	var d DiscoveredDevice
	// Need to handle nulls if we use them
	// Start with simple scan
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&d.ID, &d.TenantID, &d.DiscoveryRunID, &d.IPAddress, &d.EndpointRef,
		&d.Manufacturer, &d.Model, &d.FirmwareVersion, &d.SerialNumber,
		&d.SupportsProfileS, &d.SupportsProfileT, &d.SupportsProfileG,
		&d.Capabilities, &d.MediaProfiles, &d.RTSP_URIs,
		&d.LastProbeAt, &d.LastErrorCode, &d.CreatedAt, &d.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrDeviceNotFound
	}
	return &d, err
}

func (m *DiscoveryModel) ListDevices(ctx context.Context, runID uuid.UUID, limit, offset int) ([]*DiscoveredDevice, error) {
	query := `
		SELECT id, tenant_id, discovery_run_id, ip_address, endpoint_ref,
		       manufacturer, model,
		       supports_profile_s, supports_profile_t, supports_profile_g,
		       last_error_code
		FROM onvif_discovered_devices
		WHERE discovery_run_id = $1
		ORDER BY ip_address
		LIMIT $2 OFFSET $3
	`
	rows, err := m.DB.QueryContext(ctx, query, runID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devs []*DiscoveredDevice
	for rows.Next() {
		d := &DiscoveredDevice{}
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.DiscoveryRunID, &d.IPAddress, &d.EndpointRef,
			&d.Manufacturer, &d.Model,
			&d.SupportsProfileS, &d.SupportsProfileT, &d.SupportsProfileG,
			&d.LastErrorCode,
		); err != nil {
			return nil, err
		}
		devs = append(devs, d)
	}
	return devs, nil
}

// Bootstrap Creds (Option A)
// Logic reused from camera_credentials? No, they are separate tables.
// We need a store method.
type OnvifCredential struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	MasterKID      string
	DEKNonce       []byte
	DEKCiphertext  []byte
	DEKTag         []byte
	DataNonce      []byte
	DataCiphertext []byte
	DataTag        []byte
}

func (m *DiscoveryModel) StoreBootstrapCred(ctx context.Context, c *OnvifCredential) error {
	query := `
		INSERT INTO onvif_credentials (
			tenant_id, master_kid, 
			dek_nonce, dek_ciphertext, dek_tag,
			data_nonce, data_ciphertext, data_tag
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`
	return m.DB.QueryRowContext(ctx, query,
		c.TenantID, c.MasterKID,
		c.DEKNonce, c.DEKCiphertext, c.DEKTag,
		c.DataNonce, c.DataCiphertext, c.DataTag,
	).Scan(&c.ID)
}

func (m *DiscoveryModel) GetBootstrapCred(ctx context.Context, id uuid.UUID) (*OnvifCredential, error) {
	// RLS checked by DB
	query := `SELECT id, tenant_id, master_kid, dek_nonce, dek_ciphertext, dek_tag, data_nonce, data_ciphertext, data_tag FROM onvif_credentials WHERE id = $1`
	var c OnvifCredential
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.TenantID, &c.MasterKID,
		&c.DEKNonce, &c.DEKCiphertext, &c.DEKTag,
		&c.DataNonce, &c.DataCiphertext, &c.DataTag,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("credential not found")
	}
	return &c, err
}
