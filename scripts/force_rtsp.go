package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

func main() {
    connStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
        os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_HOST"), os.Getenv("DB_NAME"))
    
    db, err := sql.Open("postgres", connStr)
    if err != nil {
        panic(err)
    }
    defer db.Close()

    camID := "a4445b25-5ff0-43d5-9eed-935a91dface9"
    tenantID := "00000000-0000-0000-0000-000000000001"
    rtspURL := "rtsp://192.168.1.7:554/live/stream1"

    query := 
        INSERT INTO camera_stream_selections (
            tenant_id, camera_id, 
            main_profile_token, main_rtsp_url_sanitized, main_supported,
            updated_at
        ) VALUES (, , 'manual', , true, NOW())
        ON CONFLICT (tenant_id, camera_id) DO UPDATE SET
            main_rtsp_url_sanitized = EXCLUDED.main_rtsp_url_sanitized,
            updated_at = NOW()
    
    
    _, err = db.ExecContext(context.Background(), query, tenantID, camID, rtspURL)
    if err != nil {
        panic(err)
    }
    fmt.Println("Updated Media Selection for " + camID)
}
