//go:build ignore

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/technosupport/ts-vms/internal/recording"
)

func main() {
	defaultDB := os.Getenv("DB_URL")
	if defaultDB == "" {
		defaultDB = "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable"
	}
	dbConn := flag.String("db", defaultDB, "DB Connection String (overrides $DB_URL)")
	scanDir := flag.String("dir", "", "Root directory to scan (e.g. ./data/recordings)")
	flag.Parse()

	if *scanDir == "" {
		log.Fatal("Must provide -dir to scan")
	}

	db, err := sql.Open("postgres", *dbConn)
	if err != nil {
		log.Fatalf("DB Connection Failed: %v", err)
	}
	metaDB := recording.NewPostgresStore(db)

	scanned, inserted := 0, 0

	filepath.Walk(*scanDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".mp4") {
			return nil
		}

		scanned++
		rel, _ := filepath.Rel(*scanDir, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		// Expected layout: tenant / site / camera / YYYY-MM-DD / HH / filename.mp4
		if len(parts) < 6 {
			return nil
		}

		tenant, site, cam := parts[0], parts[1], parts[2]

		// Parse Phase 4.3 filename: {CameraID}_{YYYYMMDDTHHMMSSZ}_{Duration}_{Seq}.mp4
		nameParts := strings.Split(strings.TrimSuffix(info.Name(), ".mp4"), "_")
		if len(nameParts) < 4 {
			return nil
		}

		startTs, pErr := time.Parse("20060102T150405Z", nameParts[1])
		if pErr != nil {
			startTs = info.ModTime()
		}

		durSec, _ := strconv.ParseInt(nameParts[2], 10, 64)

		seg := &recording.Segment{
			TenantID:   tenant,
			SiteID:     site,
			CameraID:   cam,
			StartTS:    startTs,
			EndTS:      startTs.Add(time.Duration(durSec) * time.Second),
			DurationMs: durSec * 1000,
			Path:       path,
			SizeBytes:  info.Size(),
		}

		if err := metaDB.UpsertFinalizedSegment(context.Background(), seg); err == nil {
			inserted++
		} else {
			log.Printf("Failed to insert %s: %v", path, err)
		}
		return nil
	})

	fmt.Printf("\n=== BACKFILL COMPLETE ===\nScanned: %d | Inserted/Updated: %d\n", scanned, inserted)
}
