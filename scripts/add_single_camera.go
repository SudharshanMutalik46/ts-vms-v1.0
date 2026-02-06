package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable"
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	siteID := uuid.MustParse("950fe99a-a4eb-4d7c-ac58-aaff6ea03d1d")
	cameraID := uuid.New()

	name := "Main IP Camera"
	rtspURL := "rtsp://192.168.1.7:554/live/stream1"
	ip := "192.168.1.7"

	fmt.Printf("Adding camera: %s (%s) ID: %s\n", name, rtspURL, cameraID)

	// 1. Upsert Camera
	err = db.QueryRowContext(ctx, `
		INSERT INTO cameras (id, tenant_id, site_id, name, ip_address, port, is_enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		cameraID, tenantID, siteID, name, ip, 554, true).Scan(&cameraID)
	if err != nil {
		log.Fatalf("Failed to insert camera: %v", err)
	}

	// 2. Upsert Media Profile
	profileID := uuid.New()
	_, err = db.ExecContext(ctx, `
		INSERT INTO camera_media_profiles (
			id, tenant_id, camera_id, profile_token, profile_name, 
			video_codec, width, height, fps, bitrate_kbps, 
			rtsp_url_sanitized
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		profileID, tenantID, cameraID, "main", "Main Stream",
		"H264", 1920, 1080, 25, 4096, rtspURL)
	if err != nil {
		log.Fatalf("Failed to insert media profile: %v", err)
	}

	// 3. Upsert Selection
	_, err = db.ExecContext(ctx, `
		INSERT INTO camera_stream_selections (
			tenant_id, camera_id, 
			main_profile_token, main_rtsp_url_sanitized, main_supported,
			sub_profile_token, sub_rtsp_url_sanitized, sub_supported,
			sub_is_same_as_main
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tenantID, cameraID,
		"main", rtspURL, true,
		"main", rtspURL, true, true)
	if err != nil {
		log.Fatalf("Failed to insert stream selection: %v", err)
	}

	fmt.Println("Camera added successfully!")
	fmt.Printf("CAMERA_ID=%s\n", cameraID)
}
