package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}
	dbPass := os.Getenv("DB_PASSWORD")
	if dbPass == "" {
		dbPass = "ts1234"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "ts_vms"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, dbPass, dbHost, dbName)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// IDs
	tenantID := "00000000-0000-0000-0000-000000000001"
	siteID := "00000000-0000-0000-0000-000000000001"
	cameraID := "00000000-0000-0000-0000-000000000001"
	userID := "00000000-0000-0000-0000-000000000001"
	roleID := "00000000-0000-0000-0000-000000000001"

	// 1. Upsert Tenant
	_, err = db.Exec(`
		INSERT INTO tenants (id, name, created_at, updated_at) 
		VALUES ($1, 'Default Tenant', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, tenantID)
	if err != nil {
		log.Fatalf("Tenant Insert Failed: %v", err)
	}

	// 1.1 Upsert Site
	_, err = db.Exec(`
		INSERT INTO sites (id, tenant_id, name, created_at, updated_at) 
		VALUES ($1, $2, 'Default Site', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, siteID, tenantID)
	if err != nil {
		log.Fatalf("Site Insert Failed: %v", err)
	}

	// 1.2 Upsert Camera
	_, err = db.Exec(`
		INSERT INTO cameras (id, tenant_id, site_id, name, ip_address, port, is_enabled, manufacturer, model, serial_number, mac_address, created_at, updated_at)
		VALUES ($1, $2, $3, 'Seeded Camera', '127.0.0.1', 8554, true, 'Generic', 'Virtual', 'SN12345', '00:00:00:00:00:00', NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET 
			ip_address = EXCLUDED.ip_address,
			name = EXCLUDED.name,
			manufacturer = EXCLUDED.manufacturer,
			model = EXCLUDED.model,
			serial_number = EXCLUDED.serial_number,
			mac_address = EXCLUDED.mac_address,
			updated_at = NOW()`, cameraID, tenantID, siteID)
	// Note: We need to update the URL in Internal Logic?
	// The DB schema for cameras uses `ip_address` and `port`.
	// Control Plane constructs RTSP URL from IP/Port usually?
	// Or does it allow custom URL?
	// The `cameras` table in `seed-admin` used `ip_address` and `port`.
	// The `ingest_manager` uses `rtsp_url`.
	// Where does `vms-control` construct `rtsp_url`?
	// `sfu_handlers` or `stream_manager`.
	// If `vms-control` constructs it as `rtsp://<ip>:<port>/...`, I can't inject `mock://`.
	// I need to check how `vms-control` determines the URL passed to `StartIngest`.
	if err != nil {
		log.Fatalf("Camera Insert Failed: %v", err)
	}

	// 2. Upsert User
	_, err = db.Exec(`
		INSERT INTO users (id, tenant_id, email, display_name, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'admin@example.com', 'System Admin', 'hash_placeholder', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING`, userID, tenantID)
	if err != nil {
		log.Fatalf("User Insert Failed: %v", err)
	}

	// 3. Upsert Role
	// First check if exists by name to avoid unique constraint if ID differs
	var existingRoleID string
	err = db.QueryRow("SELECT id FROM roles WHERE tenant_id = $1 AND name = 'System Admin'", tenantID).Scan(&existingRoleID)
	if err == nil {
		roleID = existingRoleID // Use existing ID
	} else {
		_, err = db.Exec(`
			INSERT INTO roles (id, tenant_id, name, created_at, updated_at)
			VALUES ($1, $2, 'System Admin', NOW(), NOW())
			ON CONFLICT (id) DO NOTHING`, roleID, tenantID)
		if err != nil {
			// Fallback check if race
			if strings.Contains(err.Error(), "unique constraint") {
				db.QueryRow("SELECT id FROM roles WHERE tenant_id = $1 AND name = 'System Admin'", tenantID).Scan(&roleID)
			} else {
				log.Fatalf("Role Insert Failed: %v", err)
			}
		}
	}

	// 4. Assign User Role
	_, err = db.Exec(`
		INSERT INTO user_roles (user_id, role_id, scope_type, scope_id)
		VALUES ($1, $2, 'tenant', $3)
		ON CONFLICT (user_id, role_id, scope_type, scope_id) DO NOTHING`, userID, roleID, tenantID)
	if err != nil {
		log.Fatalf("User Role Assignment Failed: %v", err)
	}

	// 5. Grant Permissions
	perms := []string{
		"cameras.list", "cameras.create", "cameras.manage", "camera.view",
		"camera.media.read", "camera.health.read",
		"nvr.discovery.read", "cameras.read", "nvr.read", "nvr.channel.write",
		"audit.read", "license.read", "user.read",
	}

	for _, p := range perms {
		// Ensure Permission Exists (using name as ID for simplicity or auto-gen)
		// Assuming UUID ID, let's look it up or insert
		var permID string
		err = db.QueryRow("SELECT id FROM permissions WHERE name = $1", p).Scan(&permID)
		if err != nil {
			// Insert
			// Generate deterministic ID or random? Random is fine if we link by ID immediately.
			// Or we can use p (names are unique?) No, ID is UUID.
			// Let's use a subquery/CTE or just select after insert
			// Simplified: Insert if not exists, then select.
			_, err := db.Exec(`INSERT INTO permissions (id, name, description) 
				VALUES (gen_random_uuid(), $1, 'Auto-Seeded')
				ON CONFLICT (name) DO NOTHING`, p)
			if err != nil {
				log.Fatalf("Permission Insert Failed for %s: %v", p, err)
			}
			err = db.QueryRow("SELECT id FROM permissions WHERE name = $1", p).Scan(&permID)
			if err != nil {
				log.Fatalf("Permission lookup failed for %s: %v", p, err)
			}
		}

		// Link to Role
		_, err = db.Exec(`
			INSERT INTO role_permissions (role_id, permission_id) 
			VALUES ($1, $2)
			ON CONFLICT (role_id, permission_id) DO NOTHING`, roleID, permID)
		if err != nil {
			log.Fatalf("Link Role-Permission Failed for %s: %v", p, err)
		}
	}

	fmt.Println("SUCCESS: DB Seeded with Admin User and Permissions.")
}
