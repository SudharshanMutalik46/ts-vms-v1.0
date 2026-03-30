package cameras

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"
	"github.com/google/uuid"
	"net/url"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type MosaicConfig struct {
	URLs []string `yaml:"urls"`
}

func StartMosaicUpdater(ctx context.Context, db *sql.DB, credService CredentialProvider, configPath string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		
		// Run once initially
		if err := updateMosaicConfig(ctx, db, credService, configPath); err != nil {
			log.Printf("Warning: Initial mosaic config update failed: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := updateMosaicConfig(ctx, db, credService, configPath); err != nil {
					log.Printf("Failed to update mosaic config: %v", err)
				}
			}
		}
	}()
}

func updateMosaicConfig(ctx context.Context, db *sql.DB, credService CredentialProvider, configPath string) error {
	query := `
		SELECT c.id, c.tenant_id, s.sub_rtsp_url_sanitized 
		FROM cameras c
		JOIN camera_stream_selections s ON c.id = s.camera_id
		WHERE c.is_enabled = true AND s.sub_rtsp_url_sanitized != '' AND c.deleted_at IS NULL
		ORDER BY c.created_at ASC
		LIMIT 64
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var (
			camID    uuid.UUID
			tenantID uuid.UUID
			rawURL   string
		)
		if err := rows.Scan(&camID, &tenantID, &rawURL); err != nil {
			return err
		}
		
		if rawURL != "" {
			// Fetch credentials
			creds, found, err := credService.GetCredentials(ctx, tenantID, camID, true)
			finalURL := rawURL
			if err == nil && found && creds != nil && creds.Data.Username != "" {
				user := url.QueryEscape(creds.Data.Username)
				pass := url.QueryEscape(creds.Data.Password)
				finalURL = fmt.Sprintf("rtsp://%s:%s@%s", user, pass, strings.TrimPrefix(rawURL, "rtsp://"))
			}
			urls = append(urls, finalURL)
		}
	}

	// Just in case there are no URLs, we still write an empty urls array so yaml is valid
	if urls == nil {
		urls = []string{}
	}

	config := MosaicConfig{URLs: urls}
	
	// Create yaml data
	data, err := yaml.Marshal(&config)
	if err != nil {
		return err
	}

	header := []byte("# TS-VMS Phase 3.10 Mosaic Configuration\n# Add up to 64 SUB-STREAM URLs below.\n")
	finalData := append(header, data...)

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(configPath, finalData, 0644)
}
