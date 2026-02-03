package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type NVRModel struct {
	DB *sql.DB
}

// --- NVR CRUD ---

func (m NVRModel) Create(ctx context.Context, nvr *NVR) error {
	query := `
		INSERT INTO nvrs (tenant_id, site_id, name, vendor, ip_address, port, is_enabled, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`

	err := m.DB.QueryRowContext(ctx, query,
		nvr.TenantID, nvr.SiteID, nvr.Name, nvr.Vendor, nvr.IPAddress, nvr.Port, nvr.IsEnabled, nvr.Status,
	).Scan(&nvr.ID, &nvr.CreatedAt, &nvr.UpdatedAt)
	return err
}

func (m NVRModel) GetByID(ctx context.Context, id uuid.UUID) (*NVR, error) {
	query := `
		SELECT id, tenant_id, site_id, name, vendor, ip_address::text, port, is_enabled, status, last_status_at, created_at, updated_at
		FROM nvrs
		WHERE id = $1 AND deleted_at IS NULL`

	var n NVR
	var lastStatus sql.NullTime

	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&n.ID, &n.TenantID, &n.SiteID, &n.Name, &n.Vendor, &n.IPAddress, &n.Port, &n.IsEnabled, &n.Status, &lastStatus, &n.CreatedAt, &n.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	if lastStatus.Valid {
		n.LastStatusAt = &lastStatus.Time
	}
	return &n, nil
}

func (m NVRModel) List(ctx context.Context, tenantID uuid.UUID, filter NVRFilter, limit, offset int) ([]*NVR, int, error) {
	where := "WHERE tenant_id = $1 AND deleted_at IS NULL"
	args := []any{tenantID}
	nextArg := 2

	if filter.SiteID != nil {
		where += fmt.Sprintf(" AND site_id = $%d", nextArg)
		args = append(args, *filter.SiteID)
		nextArg++
	}
	if filter.Vendor != nil {
		where += fmt.Sprintf(" AND vendor = $%d", nextArg)
		args = append(args, *filter.Vendor)
		nextArg++
	}
	if filter.Status != nil {
		where += fmt.Sprintf(" AND status = $%d", nextArg)
		args = append(args, *filter.Status)
		nextArg++
	}
	if filter.IsEnabled != nil {
		where += fmt.Sprintf(" AND is_enabled = $%d", nextArg)
		args = append(args, *filter.IsEnabled)
		nextArg++
	}
	if filter.Query != "" {
		where += fmt.Sprintf(" AND (name ILIKE '%%' || $%d || '%%' OR ip_address::text ILIKE '%%' || $%d || '%%')", nextArg, nextArg)
		args = append(args, filter.Query)
		nextArg++
	}

	// Count
	var total int
	countQuery := "SELECT count(*) FROM nvrs " + where
	if err := m.DB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Select
	query := fmt.Sprintf(`
		SELECT id, tenant_id, site_id, name, vendor, ip_address::text, port, is_enabled, status, last_status_at, created_at, updated_at
		FROM nvrs
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, nextArg, nextArg+1)

	args = append(args, limit, offset)

	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var nvrs []*NVR
	for rows.Next() {
		var n NVR
		var lastStatus sql.NullTime
		if err := rows.Scan(&n.ID, &n.TenantID, &n.SiteID, &n.Name, &n.Vendor, &n.IPAddress, &n.Port, &n.IsEnabled, &n.Status, &lastStatus, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if lastStatus.Valid {
			n.LastStatusAt = &lastStatus.Time
		}
		nvrs = append(nvrs, &n)
	}
	return nvrs, total, nil
}

func (m NVRModel) ListAllNVRs(ctx context.Context) ([]*NVR, error) {
	// For background jobs only. No RLS.
	query := `SELECT id, tenant_id, site_id, name, vendor, ip_address::text, port, is_enabled, status, last_status_at FROM nvrs WHERE deleted_at IS NULL`
	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nvrs []*NVR
	for rows.Next() {
		var n NVR
		var lastStatus sql.NullTime
		if err := rows.Scan(&n.ID, &n.TenantID, &n.SiteID, &n.Name, &n.Vendor, &n.IPAddress, &n.Port, &n.IsEnabled, &n.Status, &lastStatus); err != nil {
			return nil, err
		}
		if lastStatus.Valid {
			n.LastStatusAt = &lastStatus.Time
		}
		nvrs = append(nvrs, &n)
	}
	return nvrs, nil
}

func (m NVRModel) Update(ctx context.Context, nvr *NVR) error {
	query := `
		UPDATE nvrs
		SET name = $1, vendor = $2, ip_address = $3, port = $4, is_enabled = $5, status = $6, last_status_at = $7, updated_at = NOW()
		WHERE id = $8 AND tenant_id = $9 AND deleted_at IS NULL
		RETURNING updated_at`

	err := m.DB.QueryRowContext(ctx, query,
		nvr.Name, nvr.Vendor, nvr.IPAddress, nvr.Port, nvr.IsEnabled, nvr.Status, nvr.LastStatusAt, nvr.ID, nvr.TenantID,
	).Scan(&nvr.UpdatedAt)

	if err == sql.ErrNoRows {
		return ErrRecordNotFound
	}
	return err
}

func (m NVRModel) Delete(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE nvrs SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
	res, err := m.DB.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// --- Linking ---

func (m NVRModel) UpsertLink(ctx context.Context, link *NVRLink) error {
	// Deterministic Move:
	// 1. Delete filtering by tenant+camera (prevents cross-tenant hijacking if RLS fails, but RLS should handle it).
	// 2. Insert new.
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete existing link for this camera (enforce 1:1)
	delQuery := `DELETE FROM camera_nvr_links WHERE camera_id = $1 AND tenant_id = $2`
	_, err = tx.ExecContext(ctx, delQuery, link.CameraID, link.TenantID)
	if err != nil {
		return err
	}

	// 2. Insert new
	insQuery := `
		INSERT INTO camera_nvr_links (tenant_id, camera_id, nvr_id, nvr_channel_ref, recording_mode, is_enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at`

	err = tx.QueryRowContext(ctx, insQuery,
		link.TenantID, link.CameraID, link.NVRID, link.NVRChannelRef, link.RecordingMode, link.IsEnabled,
	).Scan(&link.ID, &link.CreatedAt, &link.UpdatedAt)

	if err != nil {
		// Possibly FK Violation (NVR not found or not in Tenant)
		// Or Unique constraint if race condition.
		return err
	}

	return tx.Commit()
}

func (m NVRModel) GetLinkByCameraID(ctx context.Context, cameraID uuid.UUID) (*NVRLink, error) {
	query := `
		SELECT id, tenant_id, camera_id, nvr_id, nvr_channel_ref, recording_mode, is_enabled, created_at, updated_at
		FROM camera_nvr_links
		WHERE camera_id = $1`

	var l NVRLink
	var ref sql.NullString
	err := m.DB.QueryRowContext(ctx, query, cameraID).Scan(
		&l.ID, &l.TenantID, &l.CameraID, &l.NVRID, &ref, &l.RecordingMode, &l.IsEnabled, &l.CreatedAt, &l.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	if ref.Valid {
		l.NVRChannelRef = &ref.String
	}
	return &l, nil
}

func (m NVRModel) ListLinks(ctx context.Context, nvrID uuid.UUID, limit, offset int) ([]*NVRLink, error) {
	query := `
		SELECT id, tenant_id, camera_id, nvr_id, nvr_channel_ref, recording_mode, is_enabled, created_at, updated_at
		FROM camera_nvr_links
		WHERE nvr_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := m.DB.QueryContext(ctx, query, nvrID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []*NVRLink
	for rows.Next() {
		var l NVRLink
		var ref sql.NullString
		if err := rows.Scan(&l.ID, &l.TenantID, &l.CameraID, &l.NVRID, &ref, &l.RecordingMode, &l.IsEnabled, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		if ref.Valid {
			l.NVRChannelRef = &ref.String
		}
		links = append(links, &l)
	}
	return links, nil
}

func (m NVRModel) UnlinkCamera(ctx context.Context, cameraID uuid.UUID) error {
	query := `DELETE FROM camera_nvr_links WHERE camera_id = $1`
	res, err := m.DB.ExecContext(ctx, query, cameraID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		// Not considered error if already unlinked?
		// Usually idempotent DELETE returns nil
		return nil
	}
	return nil
}

// --- Credentials ---

func (m NVRModel) UpsertCredential(ctx context.Context, cred *NVRCredential) error {
	query := `
		INSERT INTO nvr_credentials (tenant_id, nvr_id, master_kid, dek_nonce, dek_ciphertext, dek_tag, data_nonce, data_ciphertext, data_tag)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, nvr_id) DO UPDATE SET
			master_kid = EXCLUDED.master_kid,
			dek_nonce = EXCLUDED.dek_nonce,
			dek_ciphertext = EXCLUDED.dek_ciphertext,
			dek_tag = EXCLUDED.dek_tag,
			data_nonce = EXCLUDED.data_nonce,
			data_ciphertext = EXCLUDED.data_ciphertext,
			data_tag = EXCLUDED.data_tag,
			updated_at = NOW()
		RETURNING id, created_at, updated_at`

	err := m.DB.QueryRowContext(ctx, query,
		cred.TenantID, cred.NVRID, cred.MasterKID,
		cred.DekNonce, cred.DekCiphertext, cred.DekTag,
		cred.DataNonce, cred.DataCiphertext, cred.DataTag,
	).Scan(&cred.ID, &cred.CreatedAt, &cred.UpdatedAt)
	return err
}

func (m NVRModel) GetCredential(ctx context.Context, nvrID uuid.UUID) (*NVRCredential, error) {
	query := `
		SELECT id, tenant_id, nvr_id, master_kid, dek_nonce, dek_ciphertext, dek_tag, data_nonce, data_ciphertext, data_tag, created_at, updated_at
		FROM nvr_credentials
		WHERE nvr_id = $1`

	var c NVRCredential
	err := m.DB.QueryRowContext(ctx, query, nvrID).Scan(
		&c.ID, &c.TenantID, &c.NVRID, &c.MasterKID,
		&c.DekNonce, &c.DekCiphertext, &c.DekTag,
		&c.DataNonce, &c.DataCiphertext, &c.DataTag,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrRecordNotFound
	}
	return &c, err
}

func (m NVRModel) DeleteCredential(ctx context.Context, nvrID uuid.UUID) error {
	query := `DELETE FROM nvr_credentials WHERE nvr_id = $1`
	_, err := m.DB.ExecContext(ctx, query, nvrID)
	return err
}

// --- Phase 2.8 Discovery ---

func (m NVRModel) UpsertChannel(ctx context.Context, ch *NVRChannel) error {
	query := `
		INSERT INTO nvr_channels (
			tenant_id, site_id, nvr_id, channel_ref, name, 
			is_enabled, supports_substream, rtsp_main_url_sanitized, rtsp_sub_url_sanitized,
			discovered_at, last_synced_at, validation_status, last_validation_at, last_error_code,
			provision_state, metadata
		) VALUES (
			$1, $2, $3, $4, $5, 
			$6, $7, $8, $9, 
			$10, $11, $12, $13, $14,
			$15, $16
		)
		ON CONFLICT (tenant_id, nvr_id, channel_ref) DO UPDATE SET
			name = EXCLUDED.name,
			site_id = EXCLUDED.site_id,
			supports_substream = EXCLUDED.supports_substream,
			rtsp_main_url_sanitized = EXCLUDED.rtsp_main_url_sanitized,
			rtsp_sub_url_sanitized = EXCLUDED.rtsp_sub_url_sanitized,
			last_synced_at = EXCLUDED.last_synced_at,
			metadata = EXCLUDED.metadata
		RETURNING id`

	if ch.DiscoveredAt.IsZero() {
		ch.DiscoveredAt = time.Now()
	}
	if ch.LastSyncedAt.IsZero() {
		ch.LastSyncedAt = time.Now()
	}

	metaJSON, _ := json.Marshal(ch.Metadata)

	return m.DB.QueryRowContext(ctx, query,
		ch.TenantID, ch.SiteID, ch.NVRID, ch.ChannelRef, ch.Name,
		ch.IsEnabled, ch.SupportsSubstream, ch.RTSPMain, ch.RTSPSub,
		ch.DiscoveredAt, ch.LastSyncedAt, ch.ValidationStatus, ch.LastValidationAt, ch.LastErrorCode,
		ch.ProvisionState, metaJSON,
	).Scan(&ch.ID)
}

func (m NVRModel) ListChannels(ctx context.Context, nvrID uuid.UUID, filter NVRChannelFilter, limit, offset int) ([]*NVRChannel, int, error) {
	query := `
		SELECT 
			id, tenant_id, site_id, nvr_id, channel_ref, name, 
			is_enabled, supports_substream, rtsp_main_url_sanitized, rtsp_sub_url_sanitized,
			discovered_at, last_synced_at, validation_status, last_validation_at, last_error_code,
			provision_state, metadata
		FROM nvr_channels
		WHERE nvr_id = $1`

	args := []interface{}{nvrID}
	nextArg := 2

	if filter.IsEnabled != nil {
		query += fmt.Sprintf(" AND is_enabled = $%d", nextArg)
		args = append(args, *filter.IsEnabled)
		nextArg++
	}
	if filter.ProvisionState != nil {
		query += fmt.Sprintf(" AND provision_state = $%d", nextArg)
		args = append(args, *filter.ProvisionState)
		nextArg++
	}
	if filter.Validation != nil {
		query += fmt.Sprintf(" AND validation_status = $%d", nextArg)
		args = append(args, *filter.Validation)
		nextArg++
	}
	if filter.Query != "" {
		query += fmt.Sprintf(" AND (name ILIKE '%%' || $%d || '%%' OR channel_ref ILIKE '%%' || $%d || '%%')", nextArg, nextArg)
		args = append(args, filter.Query)
		nextArg++
	}

	// Count
	var total int
	// Crude count replace for speed
	countQuery := "SELECT COUNT(*) FROM nvr_channels WHERE nvr_id = $1" // Simplified base, fix filter app
	// Actually, let's reuse query builder logic properly or wrapper.
	// For simplicity in this edit:
	countQuery = "SELECT COUNT(*) FROM (" + query + ") as c"
	if err := m.DB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query += fmt.Sprintf(" ORDER BY channel_ref ASC LIMIT $%d OFFSET $%d", nextArg, nextArg+1)
	args = append(args, limit, offset)

	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var channels []*NVRChannel
	for rows.Next() {
		ch := &NVRChannel{}
		var metaJSON []byte
		var lastVal sql.NullTime
		var subStream sql.NullBool
		err := rows.Scan(
			&ch.ID, &ch.TenantID, &ch.SiteID, &ch.NVRID, &ch.ChannelRef, &ch.Name,
			&ch.IsEnabled, &subStream, &ch.RTSPMain, &ch.RTSPSub,
			&ch.DiscoveredAt, &ch.LastSyncedAt, &ch.ValidationStatus, &lastVal, &ch.LastErrorCode,
			&ch.ProvisionState, &metaJSON,
		)
		if err != nil {
			return nil, 0, err
		}
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &ch.Metadata)
		}
		if lastVal.Valid {
			ch.LastValidationAt = &lastVal.Time
		}
		if subStream.Valid {
			ch.SupportsSubstream = &subStream.Bool
		}
		channels = append(channels, ch)
	}

	return channels, total, nil
}

func (m NVRModel) GetChannel(ctx context.Context, id uuid.UUID) (*NVRChannel, error) {
	query := `
		SELECT 
			id, tenant_id, site_id, nvr_id, channel_ref, name, 
			is_enabled, supports_substream, rtsp_main_url_sanitized, rtsp_sub_url_sanitized,
			discovered_at, last_synced_at, validation_status, last_validation_at, last_error_code,
			provision_state, metadata
		FROM nvr_channels WHERE id = $1`

	ch := &NVRChannel{}
	var metaJSON []byte
	var lastVal sql.NullTime
	var subStream sql.NullBool
	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&ch.ID, &ch.TenantID, &ch.SiteID, &ch.NVRID, &ch.ChannelRef, &ch.Name,
		&ch.IsEnabled, &subStream, &ch.RTSPMain, &ch.RTSPSub,
		&ch.DiscoveredAt, &ch.LastSyncedAt, &ch.ValidationStatus, &lastVal, &ch.LastErrorCode,
		&ch.ProvisionState, &metaJSON,
	)
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > 0 {
		json.Unmarshal(metaJSON, &ch.Metadata)
	}
	if lastVal.Valid {
		ch.LastValidationAt = &lastVal.Time
	}
	if subStream.Valid {
		ch.SupportsSubstream = &subStream.Bool
	}
	return ch, nil
}

func (m NVRModel) GetChannelByRef(ctx context.Context, nvrID uuid.UUID, ref string) (*NVRChannel, error) {
	query := `
		SELECT 
			id, tenant_id, site_id, nvr_id, channel_ref, name, 
			is_enabled, supports_substream, rtsp_main_url_sanitized, rtsp_sub_url_sanitized,
			discovered_at, last_synced_at, validation_status, last_validation_at, last_error_code,
			provision_state, metadata
		FROM nvr_channels WHERE nvr_id = $1 AND channel_ref = $2`

	ch := &NVRChannel{}
	var metaJSON []byte
	var lastVal sql.NullTime
	var subStream sql.NullBool
	err := m.DB.QueryRowContext(ctx, query, nvrID, ref).Scan(
		&ch.ID, &ch.TenantID, &ch.SiteID, &ch.NVRID, &ch.ChannelRef, &ch.Name,
		&ch.IsEnabled, &subStream, &ch.RTSPMain, &ch.RTSPSub,
		&ch.DiscoveredAt, &ch.LastSyncedAt, &ch.ValidationStatus, &lastVal, &ch.LastErrorCode,
		&ch.ProvisionState, &metaJSON,
	)
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > 0 {
		json.Unmarshal(metaJSON, &ch.Metadata)
	}
	if lastVal.Valid {
		ch.LastValidationAt = &lastVal.Time
	}
	if subStream.Valid {
		ch.SupportsSubstream = &subStream.Bool
	}
	return ch, nil
}

func (m NVRModel) UpdateChannelStatus(ctx context.Context, id uuid.UUID, validationStatus string, errCode *string) error {
	query := `UPDATE nvr_channels SET validation_status = $1, last_validation_at = NOW(), last_error_code = $2 WHERE id = $3`
	_, err := m.DB.ExecContext(ctx, query, validationStatus, errCode, id)
	return err
}

func (m NVRModel) UpdateChannelProvisionState(ctx context.Context, id uuid.UUID, state string) error {
	query := `UPDATE nvr_channels SET provision_state = $1 WHERE id = $2`
	_, err := m.DB.ExecContext(ctx, query, state, id)
	return err
}

func (m NVRModel) BulkEnableChannels(ctx context.Context, ids []uuid.UUID, enable bool) error {
	query := `UPDATE nvr_channels SET is_enabled = $1 WHERE id = ANY($2)`
	_, err := m.DB.ExecContext(ctx, query, enable, pq.Array(ids))
	return err
}

// --- Phase 2.9 Health ---

func (m NVRModel) UpsertNVRHealth(ctx context.Context, h *NVRHealth) error {
	query := `
		INSERT INTO nvr_health_current (
			tenant_id, nvr_id, status, last_checked_at, last_success_at, consecutive_failures, last_error_code, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (tenant_id, nvr_id) DO UPDATE SET
			status = EXCLUDED.status,
			last_checked_at = EXCLUDED.last_checked_at,
			last_success_at = COALESCE(EXCLUDED.last_success_at, nvr_health_current.last_success_at),
			consecutive_failures = EXCLUDED.consecutive_failures,
			last_error_code = EXCLUDED.last_error_code,
			updated_at = NOW()`
	_, err := m.DB.ExecContext(ctx, query,
		h.TenantID, h.NVRID, h.Status, h.LastCheckedAt, h.LastSuccessAt, h.ConsecutiveFailures, h.LastErrorCode,
	)
	return err
}

func (m NVRModel) UpsertChannelHealth(ctx context.Context, h *NVRChannelHealth) error {
	query := `
		INSERT INTO nvr_channel_health_current (
			tenant_id, nvr_id, channel_id, status, last_checked_at, last_success_at, consecutive_failures, last_error_code, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (tenant_id, nvr_id, channel_id) DO UPDATE SET
			status = EXCLUDED.status,
			last_checked_at = EXCLUDED.last_checked_at,
			last_success_at = COALESCE(EXCLUDED.last_success_at, nvr_channel_health_current.last_success_at),
			consecutive_failures = EXCLUDED.consecutive_failures,
			last_error_code = EXCLUDED.last_error_code,
			updated_at = NOW()`
	_, err := m.DB.ExecContext(ctx, query,
		h.TenantID, h.NVRID, h.ChannelID, h.Status, h.LastCheckedAt, h.LastSuccessAt, h.ConsecutiveFailures, h.LastErrorCode,
	)
	return err
}

func (m NVRModel) GetNVRHealthSummary(ctx context.Context, tenantID uuid.UUID, siteIDs []uuid.UUID) (*NVRHealthSummary, error) {
	// 1. NVR Counts
	// NVR Status sources from `nvr_health_current`. If missing, assume 'unknown' or join.
	// We really should join `nvrs` with `nvr_health_current`.

	// Base Query for NVRs
	nvrWhere := "WHERE n.tenant_id = $1 AND n.deleted_at IS NULL"
	args := []interface{}{tenantID}

	if len(siteIDs) > 0 {
		nvrWhere += " AND n.site_id = ANY($2)"
		args = append(args, pq.Array(siteIDs))
	}

	nvrQuery := fmt.Sprintf(`
		SELECT 
			count(*) as total,
			count(*) FILTER (WHERE h.status = 'online') as online,
			count(*) FILTER (WHERE h.status = 'offline') as offline,
			count(*) FILTER (WHERE h.status = 'auth_failed') as auth_failed,
			count(*) FILTER (WHERE h.status = 'error') as error
		FROM nvrs n
		LEFT JOIN nvr_health_current h ON n.id = h.nvr_id
		%s
	`, nvrWhere)

	s := &NVRHealthSummary{}
	err := m.DB.QueryRowContext(ctx, nvrQuery, args...).Scan(
		&s.TotalNVRs, &s.NVRsOnline, &s.NVRsOffline, &s.NVRsAuthFailed, &s.NVRsError,
	)
	if err != nil {
		return nil, err
	}

	// 2. Channel Counts
	// Complex logic:
	// - If NVR is NOT online -> Channel is UnreachableDueToNVR (regardless of channel health DB entry)
	// - If NVR IS online -> Channel status comes from channel_health DB entry.
	// - Only count ENABLED channels.

	chanWhere := "WHERE c.tenant_id = $1 AND c.is_enabled = true"
	// We need to verify site access via NVR or directly if channels have site_id (they do).
	// Channels have `site_id`, so we can filter directly on channels.
	if len(siteIDs) > 0 {
		chanWhere += " AND c.site_id = ANY($2)"
	}
	// Args same as above ($1, $2)

	chanQuery := fmt.Sprintf(`
		SELECT
			count(*) as total_enabled,
			
			-- Unreachable: NVR is not online (missing, offline, error, auth_failed)
			count(*) FILTER (WHERE nh.status IS DISTINCT FROM 'online') as unreachable_nvr,
			
			-- Online: NVR is online AND Channel is online
			count(*) FILTER (WHERE nh.status = 'online' AND ch.status = 'online') as online,
			
			-- Offline: NVR is online AND Channel is offline
			count(*) FILTER (WHERE nh.status = 'online' AND ch.status = 'offline') as offline,
			
			-- Auth Failed: NVR is online AND Channel auth failed
			count(*) FILTER (WHERE nh.status = 'online' AND ch.status = 'auth_failed') as auth_failed,
			
			-- Stream Error
			count(*) FILTER (WHERE nh.status = 'online' AND ch.status = 'stream_error') as error
			
		FROM nvr_channels c
		JOIN nvrs n ON c.nvr_id = n.id AND n.deleted_at IS NULL
		LEFT JOIN nvr_health_current nh ON n.id = nh.nvr_id
		LEFT JOIN nvr_channel_health_current ch ON c.id = ch.channel_id
		%s
	`, chanWhere)

	err = m.DB.QueryRowContext(ctx, chanQuery, args...).Scan(
		&s.TotalChannelsEnabled,
		&s.ChannelsUnreachableDueToNVR,
		&s.ChannelsOnline,
		&s.ChannelsOffline,
		&s.ChannelsAuthFailed,
		&s.ChannelsStreamError,
	)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (m NVRModel) ListChannelHealth(ctx context.Context, nvrID uuid.UUID, limit, offset int) ([]*NVRChannelHealth, error) {
	// This returns the stored health rows.
	// The API layer will do the "Effective Status" join or computation?
	// The prompt Plan said: "API ... Computes effective_status for response".
	// So we just return the raw DB rows here.
	query := `
		SELECT tenant_id, nvr_id, channel_id, status, last_checked_at, last_success_at, consecutive_failures, last_error_code, updated_at
		FROM nvr_channel_health_current
		WHERE nvr_id = $1
		ORDER BY channel_id
		LIMIT $2 OFFSET $3`

	rows, err := m.DB.QueryContext(ctx, query, nvrID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []*NVRChannelHealth
	for rows.Next() {
		h := &NVRChannelHealth{}
		err := rows.Scan(
			&h.TenantID, &h.NVRID, &h.ChannelID, &h.Status, &h.LastCheckedAt, &h.LastSuccessAt, &h.ConsecutiveFailures, &h.LastErrorCode, &h.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		res = append(res, h)
	}
	return res, nil
}

// --- Event Polling ---

func (m NVRModel) UpsertEventPollState(ctx context.Context, state *NVREventPollState) error {
	query := `
		INSERT INTO nvr_event_poll_state (
			tenant_id, nvr_id, last_success_at, cursor, since_ts, consecutive_failures, last_error_code, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (tenant_id, nvr_id) DO UPDATE SET
			last_success_at = EXCLUDED.last_success_at,
			cursor = EXCLUDED.cursor,
			since_ts = EXCLUDED.since_ts,
			consecutive_failures = EXCLUDED.consecutive_failures,
			last_error_code = EXCLUDED.last_error_code,
			updated_at = NOW()
	`
	_, err := m.DB.ExecContext(ctx, query,
		state.TenantID,
		state.NVRID,
		state.LastSuccessAt,
		state.Cursor,
		state.SinceTS,
		state.ConsecutiveFailures,
		state.LastErrorCode,
	)
	return err
}

func (m NVRModel) GetEventPollState(ctx context.Context, nvrID uuid.UUID) (*NVREventPollState, error) {
	query := `
		SELECT tenant_id, nvr_id, last_success_at, cursor, since_ts, consecutive_failures, last_error_code, updated_at
		FROM nvr_event_poll_state
		WHERE nvr_id = $1`

	var s NVREventPollState
	err := m.DB.QueryRowContext(ctx, query, nvrID).Scan(
		&s.TenantID,
		&s.NVRID,
		&s.LastSuccessAt,
		&s.Cursor,
		&s.SinceTS,
		&s.ConsecutiveFailures,
		&s.LastErrorCode,
		&s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Return nil if no state tracking yet (first run)
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
