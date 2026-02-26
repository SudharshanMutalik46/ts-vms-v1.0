package recording

import (
	"context"
	"database/sql"
	"time"
)

type Segment struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	SiteID      string    `json:"site_id"`
	CameraID    string    `json:"camera_id"`
	StartTS     time.Time `json:"start_ts"`
	EndTS       time.Time `json:"end_ts"`
	DurationMs  int64     `json:"duration_ms"`
	Path        string    `json:"path"`
	SizeBytes   int64     `json:"size_bytes"`
	IsProtected bool      `json:"is_protected"`
}

type Event struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	SiteID    string    `json:"site_id"`
	CameraID  string    `json:"camera_id"`
	EventType string    `json:"event_type"`
	EventTS   time.Time `json:"event_ts"`
}

type IMetadataDB interface {
	WriteSegment(ctx context.Context, seg *Segment) error
	GetSegments(ctx context.Context, camID string, from, to time.Time) ([]Segment, error)
	CreateEvent(ctx context.Context, ev *Event) error
	LinkSegmentToEvent(ctx context.Context, eventID, segmentID string) error
}

type PostgresMetadataDB struct {
	DB *sql.DB
}

func NewPostgresMetadataDB(db *sql.DB) *PostgresMetadataDB {
	return &PostgresMetadataDB{DB: db}
}

func (m *PostgresMetadataDB) WriteSegment(ctx context.Context, seg *Segment) error {
	query := `
		INSERT INTO recording_segments (tenant_id, site_id, camera_id, start_ts, end_ts, duration_ms, path, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (path) DO UPDATE SET 
			size_bytes = EXCLUDED.size_bytes, 
			end_ts = EXCLUDED.end_ts
		RETURNING id;`

	return m.DB.QueryRowContext(ctx, query,
		seg.TenantID, seg.SiteID, seg.CameraID, seg.StartTS, seg.EndTS,
		seg.DurationMs, seg.Path, seg.SizeBytes).Scan(&seg.ID)
}

func (m *PostgresMetadataDB) GetSegments(ctx context.Context, camID string, from, to time.Time) ([]Segment, error) {
	query := `
		SELECT s.id, s.tenant_id, s.site_id, s.camera_id, s.start_ts, s.end_ts, s.duration_ms, s.path, s.size_bytes,
			EXISTS(SELECT 1 FROM recording_event_segments e WHERE e.segment_id = s.id) AS is_protected
		FROM recording_segments s
		WHERE s.camera_id = $1 AND s.start_ts >= $2 AND s.start_ts <= $3
		ORDER BY s.start_ts ASC;`

	rows, err := m.DB.QueryContext(ctx, query, camID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Segment
	for rows.Next() {
		var s Segment
		if err := rows.Scan(&s.ID, &s.TenantID, &s.SiteID, &s.CameraID, &s.StartTS, &s.EndTS, &s.DurationMs, &s.Path, &s.SizeBytes, &s.IsProtected); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, nil
}

func (m *PostgresMetadataDB) CreateEvent(ctx context.Context, ev *Event) error {
	query := `INSERT INTO recording_events (tenant_id, site_id, camera_id, event_type, event_ts) VALUES ($1, $2, $3, $4, $5) RETURNING id;`
	return m.DB.QueryRowContext(ctx, query, ev.TenantID, ev.SiteID, ev.CameraID, ev.EventType, ev.EventTS).Scan(&ev.ID)
}

func (m *PostgresMetadataDB) LinkSegmentToEvent(ctx context.Context, eventID, segmentID string) error {
	query := `INSERT INTO recording_event_segments (event_id, segment_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;`
	_, err := m.DB.ExecContext(ctx, query, eventID, segmentID)
	return err
}
