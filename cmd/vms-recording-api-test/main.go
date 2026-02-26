package main

import (
	"database/sql"
	"log"
	"net/http"

	_ "github.com/lib/pq"
	"github.com/technosupport/ts-vms/internal/api"
	"github.com/technosupport/ts-vms/internal/recording"
)

func main() {
	// Connect and Migrate
	db, err := sql.Open("postgres", "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}

	// Hardcode schema creation to avoid missing golang-migrate CLI dependency in PowerShell test
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS recording_segments (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id VARCHAR(255) NOT NULL, site_id VARCHAR(255) NOT NULL, camera_id VARCHAR(255) NOT NULL, start_ts TIMESTAMPTZ NOT NULL, end_ts TIMESTAMPTZ NOT NULL, duration_ms BIGINT NOT NULL, path TEXT NOT NULL UNIQUE, size_bytes BIGINT NOT NULL, is_corrupt BOOLEAN DEFAULT false, created_at TIMESTAMPTZ DEFAULT NOW());`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS recording_events (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id VARCHAR(255) NOT NULL, site_id VARCHAR(255) NOT NULL, camera_id VARCHAR(255) NOT NULL, event_type VARCHAR(50) NOT NULL, event_ts TIMESTAMPTZ NOT NULL, payload JSONB, created_at TIMESTAMPTZ DEFAULT NOW());`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS recording_event_segments (event_id UUID REFERENCES recording_events(id) ON DELETE CASCADE, segment_id UUID REFERENCES recording_segments(id) ON DELETE CASCADE, PRIMARY KEY (event_id, segment_id));`)

	metaDB := recording.NewPostgresMetadataDB(db)
	handler := &api.RecordingAPI{DB: metaDB}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/recordings/segments", handler.HandleGetSegments)
	mux.HandleFunc("/api/v1/recordings/events", handler.HandleCreateEvent)
	mux.HandleFunc("/api/v1/recordings/events/link", handler.HandleLinkSegment)

	log.Println("Test API running on :8083")
	http.ListenAndServe(":8083", mux)
}
