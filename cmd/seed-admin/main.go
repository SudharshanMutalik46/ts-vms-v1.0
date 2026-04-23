package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/technosupport/ts-vms/internal/auth"

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

	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", dbUser, dbPass, dbHost, dbPort, dbName)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// IDs
	tenantID := "00000000-0000-0000-0000-000000000001"
	siteID := "00000000-0000-0000-0000-000000000001"
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

	// 2. Upsert User
	passwordHash, err := auth.HashPassword("ts1234")
	if err != nil {
		log.Fatalf("Password Hash Failed: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO users (id, tenant_id, email, display_name, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'admin@technosupport.com', 'System Admin', $3, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET
			email = EXCLUDED.email,
			password_hash = EXCLUDED.password_hash,
			updated_at = NOW()`, userID, tenantID, passwordHash)
	if err != nil {
		log.Fatalf("User Insert Failed: %v", err)
	}

	// 3. Upsert Role (must be "admin" to match RBAC checks)
	// First check if exists by name to avoid unique constraint if ID differs
	var existingRoleID string
	err = db.QueryRow("SELECT id FROM roles WHERE tenant_id = $1 AND LOWER(name) = 'admin'", tenantID).Scan(&existingRoleID)
	if err == nil {
		roleID = existingRoleID // Use existing ID
	} else {
		_, err = db.Exec(`
			INSERT INTO roles (id, tenant_id, name, created_at, updated_at)
			VALUES ($1, $2, 'admin', NOW(), NOW())
			ON CONFLICT (id) DO NOTHING`, roleID, tenantID)
		if err != nil {
			// Fallback check if race
			if strings.Contains(err.Error(), "unique constraint") {
				db.QueryRow("SELECT id FROM roles WHERE tenant_id = $1 AND LOWER(name) = 'admin'", tenantID).Scan(&roleID)
			} else {
				log.Fatalf("Role Insert Failed: %v", err)
			}
		}
	}

	// 3.1 Ensure standard non-admin roles exist for assignment/self-signup.
	_, err = db.Exec(`
		INSERT INTO roles (id, tenant_id, name, created_at, updated_at)
		SELECT gen_random_uuid(), $1, v.name, NOW(), NOW()
		FROM (VALUES ('viewer'), ('operator')) AS v(name)
		WHERE NOT EXISTS (
			SELECT 1 FROM roles r WHERE r.tenant_id = $1 AND LOWER(r.name) = v.name
		)
	`, tenantID)
	if err != nil {
		log.Fatalf("Role bootstrap failed: %v", err)
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
		"audit.read", "audit.export", "license.read", "user.read",
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
