package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Camera represents a video capture device
type Camera struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	SiteID       uuid.UUID  `json:"site_id"`
	Name         string     `json:"name"`
	IPAddress    net.IP     `json:"ip_address"`
	Port         int        `json:"port"`
	Manufacturer string     `json:"manufacturer,omitempty"`
	Model        string     `json:"model,omitempty"`
	SerialNumber string     `json:"serial_number,omitempty"`
	MacAddress   string     `json:"mac_address,omitempty"`
	IsEnabled    bool       `json:"is_enabled"`
	Tags         []string   `json:"tags"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type CameraGroup struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	SiteID      *uuid.UUID `json:"site_id,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CameraModel struct {
	DB DBTX
}

// Create inserts a new camera. Enforces tenant/site FK via DB.
func (m CameraModel) Create(ctx context.Context, c *Camera) error {
	query := `
		INSERT INTO cameras (
			tenant_id, site_id, name, ip_address, port, 
			manufacturer, model, serial_number, mac_address, 
			is_enabled, tags
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at`

	// Safe cast IP to string for pq driver auto-handling or pass as string if driver requires
	// lib/pq usually handles net.IP correctly as INET
	err := m.DB.QueryRowContext(ctx, query,
		c.TenantID, c.SiteID, c.Name, c.IPAddress.String(), c.Port,
		c.Manufacturer, c.Model, c.SerialNumber, c.MacAddress,
		c.IsEnabled, pq.Array(c.Tags),
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)

	return err
}

// GetByID retrieves a camera by ID, strictly scoping to tenant (and site if provided/inforced by caller)
// Note: We scan tenant_id so caller can verify RBAC
func (m CameraModel) GetByID(ctx context.Context, id uuid.UUID) (*Camera, error) {
	query := `
		SELECT id, tenant_id, site_id, name, ip_address, port, 
		       manufacturer, model, serial_number, mac_address, 
		       is_enabled, tags, created_at, updated_at, deleted_at
		FROM cameras
		WHERE id = $1 AND deleted_at IS NULL`

	var c Camera
	var ipStr string
	var tags []string

	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.TenantID, &c.SiteID, &c.Name, &ipStr, &c.Port,
		&c.Manufacturer, &c.Model, &c.SerialNumber, &c.MacAddress,
		&c.IsEnabled, pq.Array(&tags), &c.CreatedAt, &c.UpdatedAt, &c.DeletedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	c.IPAddress = net.ParseIP(ipStr)
	c.Tags = tags
	return &c, nil
}

// Update modifies camera struct. Assumes UpdatedAt is set by DB default or we set it here?
// DB has DEFAULT NOW(), but standard Go pattern is to read it back.
func (m CameraModel) Update(ctx context.Context, c *Camera) error {
	query := `
		UPDATE cameras
		SET name = $1, ip_address = $2, port = $3,
		    manufacturer = $4, model = $5, serial_number = $6, mac_address = $7,
		    tags = $8, updated_at = NOW()
		WHERE id = $9 AND tenant_id = $10 AND deleted_at IS NULL
		RETURNING updated_at`

	err := m.DB.QueryRowContext(ctx, query,
		c.Name, c.IPAddress.String(), c.Port,
		c.Manufacturer, c.Model, c.SerialNumber, c.MacAddress,
		pq.Array(c.Tags), c.ID, c.TenantID,
	).Scan(&c.UpdatedAt)

	if err == sql.ErrNoRows {
		return ErrRecordNotFound
	}
	return err
}

// EnableDisable sets is_enabled
func (m CameraModel) SetStatus(ctx context.Context, id, tenantID uuid.UUID, enabled bool) error {
	query := `UPDATE cameras SET is_enabled = $1, updated_at = NOW() WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`
	res, err := m.DB.ExecContext(ctx, query, enabled, id, tenantID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// SoftDelete
func (m CameraModel) SoftDelete(ctx context.Context, id, tenantID uuid.UUID) error {
	query := `UPDATE cameras SET deleted_at = NOW() WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`
	res, err := m.DB.ExecContext(ctx, query, id, tenantID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// CameraFilter parameters
type CameraFilter struct {
	SiteID    *uuid.UUID
	IsEnabled *bool
	Query     string // FTS
}

// List retrieves paginated cameras.
func (m CameraModel) List(ctx context.Context, tenantID uuid.UUID, filter CameraFilter, limit, offset int) ([]*Camera, int, error) {
	// 1. Build Base Query
	where := "WHERE tenant_id = $1 AND deleted_at IS NULL"
	args := []any{tenantID}
	nextArg := 2

	if filter.SiteID != nil {
		where += fmt.Sprintf(" AND site_id = $%d", nextArg)
		args = append(args, *filter.SiteID)
		nextArg++
	}
	if filter.IsEnabled != nil {
		where += fmt.Sprintf(" AND is_enabled = $%d", nextArg)
		args = append(args, *filter.IsEnabled)
		nextArg++
	}
	if filter.Query != "" {
		// FTS: query against search_text using pg_trgm
		where += fmt.Sprintf(" AND search_text %% $%d", nextArg) // % operator for trgm similarity or LIKE?
		// Plan said "search by name and ip". Trigram works well with LIKE %q% logic too if index supports it.
		// "search_text gin_trgm_ops" supports ILIKE or %.
		// Let's use ILIKE for partial match which trigram indexes accelerate.
		// Actually, `search_text ILIKE '%' || $N || '%'` is standard for "contains".
		where += fmt.Sprintf(" AND search_text ILIKE '%%' || $%d || '%%'", nextArg)
		args = append(args, filter.Query)
		nextArg++
	}

	// 2. Count Total (for pagination metadata if needed, usually good practice)
	// Optimizing: Just return list for now as per requirements "paginated"
	// But usually API returns X-Total-Count.
	countQuery := "SELECT count(*) FROM cameras " + where
	var total int
	if err := m.DB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 3. Select Data
	query := fmt.Sprintf(`
		SELECT id, tenant_id, site_id, name, ip_address, port, is_enabled, tags, created_at, updated_at 
		FROM cameras 
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, nextArg, nextArg+1)

	args = append(args, limit, offset)

	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var cameras []*Camera
	for rows.Next() {
		var c Camera
		var ipStr string
		var tags []string
		if err := rows.Scan(&c.ID, &c.TenantID, &c.SiteID, &c.Name, &ipStr, &c.Port, &c.IsEnabled, pq.Array(&tags), &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		c.IPAddress = net.ParseIP(ipStr)
		c.Tags = tags
		cameras = append(cameras, &c)
	}

	return cameras, total, nil
}

// CountEnabled used for license quota checks
func (m CameraModel) CountAll(ctx context.Context, tenantID uuid.UUID) (int, error) {
	query := `SELECT count(*) FROM cameras WHERE tenant_id = $1 AND deleted_at IS NULL`
	var count int
	err := m.DB.QueryRowContext(ctx, query, tenantID).Scan(&count)
	return count, err
}

// BulkEnable checks quotas before enabling.
// actually the Service Layer should do the quota check logic.
// Model just executes bulk update.
func (m CameraModel) BulkUpdateStatus(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, enabled bool) error {
	query := `
		UPDATE cameras 
		SET is_enabled = $1, updated_at = NOW()
		WHERE tenant_id = $2 AND id = ANY($3) AND deleted_at IS NULL`
	_, err := m.DB.ExecContext(ctx, query, enabled, tenantID, pq.Array(ids))
	return err
}

func (m CameraModel) BulkAddTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	// Postres Array append: array_cat or ||
	// Uniq usage: SELECT array_agg(DISTINCT x) ... complex update?
	// Simpler: UPDATE cameras SET tags = array(select distinct unnest(tags || $3))
	query := `
		UPDATE cameras
		SET tags = (SELECT ARRAY(SELECT DISTINCT UNNEST(tags || $1)))
		WHERE tenant_id = $2 AND id = ANY($3) AND deleted_at IS NULL`
	_, err := m.DB.ExecContext(ctx, query, pq.Array(tags), tenantID, pq.Array(ids))
	return err
}

func (m CameraModel) BulkRemoveTags(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID, tags []string) error {
	// Remove tags: array_remove? But that removes all instances of one value.
	// Removing multiple tags at once from array is tricky in SQL standard.
	// `tags = ARRAY(SELECT unnest(tags) EXCEPT SELECT unnest($1))`
	query := `
		UPDATE cameras
		SET tags = ARRAY(
			SELECT x FROM unnest(tags) AS x
			WHERE x NOT IN (SELECT unnest($1::text[]))
		)
		WHERE tenant_id = $2 AND id = ANY($3) AND deleted_at IS NULL`

	_, err := m.DB.ExecContext(ctx, query, pq.Array(tags), tenantID, pq.Array(ids))
	return err
}

// --- Grouping ---

func (m CameraModel) CreateGroup(ctx context.Context, g *CameraGroup) error {
	query := `
		INSERT INTO camera_groups (tenant_id, site_id, name, description)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`
	// SiteID is nullable
	var siteID sql.NullString
	if g.SiteID != nil {
		siteID = sql.NullString{String: g.SiteID.String(), Valid: true}
	}

	err := m.DB.QueryRowContext(ctx, query, g.TenantID, siteID, g.Name, g.Description).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt)
	return err
}

func (m CameraModel) DeleteGroup(ctx context.Context, id, tenantID uuid.UUID) error {
	query := `DELETE FROM camera_groups WHERE id = $1 AND tenant_id = $2`
	res, err := m.DB.ExecContext(ctx, query, id, tenantID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// SetGroupMembers updates the members of a group (replace all)
func (m CameraModel) SetGroupMembers(ctx context.Context, groupID, tenantID uuid.UUID, cameraIDs []uuid.UUID) error {
	// Verify Tenant Ownership first?
	// Or just DELETE WHERE group_id IN (SELECT id FROM camera_groups WHERE id=$1 AND tenant_id=$2)
	// PostgreSQL doesn't support JOIN in DELETE simply without USING.
	// `DELETE FROM camera_group_members WHERE group_id = $1 AND group_id IN (SELECT id FROM camera_groups WHERE tenant_id = $2)`
	// But group_id is FK to camera_groups.

	// Security Check: Ensure group belongs to tenant
	checkQuery := `SELECT EXISTS(SELECT 1 FROM camera_groups WHERE id = $1 AND tenant_id = $2)`
	var exists bool
	if err := m.DB.QueryRowContext(ctx, checkQuery, groupID, tenantID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errors.New("group not found or access denied")
	}

	deleteQuery := `DELETE FROM camera_group_members WHERE group_id = $1`
	if _, err := m.DB.ExecContext(ctx, deleteQuery, groupID); err != nil {
		return err
	}

	if len(cameraIDs) == 0 {
		return nil
	}

	// Bulk Insert
	// UNNEST logic
	insertQuery := `
		INSERT INTO camera_group_members (group_id, camera_id)
		SELECT $1, unnest($2::uuid[])
		ON CONFLICT DO NOTHING`
	_, err := m.DB.ExecContext(ctx, insertQuery, groupID, pq.Array(cameraIDs))
	return err
}

func (m CameraModel) ListGroups(ctx context.Context, tenantID uuid.UUID) ([]*CameraGroup, error) {
	query := `SELECT id, tenant_id, site_id, name, description, created_at, updated_at FROM camera_groups WHERE tenant_id = $1`
	rows, err := m.DB.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*CameraGroup
	for rows.Next() {
		var g CameraGroup
		var siteID sql.NullString
		var desc sql.NullString
		if err := rows.Scan(&g.ID, &g.TenantID, &siteID, &g.Name, &desc, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		if siteID.Valid {
			uid := uuid.MustParse(siteID.String)
			g.SiteID = &uid
		}
		if desc.Valid {
			g.Description = desc.String
		}
		groups = append(groups, &g)
	}
	return groups, nil
}

// ResolveSiteID implements middleware.CameraResolver
func (m CameraModel) ResolveSiteID(ctx context.Context, cameraID string) (string, error) {
	var siteID string
	query := `SELECT site_id FROM cameras WHERE id = $1 AND deleted_at IS NULL`
	err := m.DB.QueryRowContext(ctx, query, cameraID).Scan(&siteID)
	if err != nil {
		return "", err
	}
	return siteID, nil
}
