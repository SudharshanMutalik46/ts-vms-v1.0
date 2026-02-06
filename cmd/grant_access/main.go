package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func main() {
	connStr := "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	userID := "00000000-0000-0000-0000-000000000002"
	tenantID := "00000000-0000-0000-0000-000000000001"

	// 1. Ensure User exists
	_, err = db.Exec(`
		INSERT INTO users (id, tenant_id, email, display_name, password_hash, is_disabled, created_at, updated_at)
		VALUES ($1, $2, 'admin@ts.com', 'Admin', 'hash', false, NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, userID, tenantID)
	if err != nil {
		panic(err)
	}

	// 2. Get Admin Role ID
	var adminRoleID string
	err = db.QueryRow("SELECT id FROM roles WHERE name = 'Admin'").Scan(&adminRoleID)
	if err != nil {
		panic(err)
	}

	// 3. Link User to Admin Role
	_, err = db.Exec(`
		INSERT INTO user_roles (user_id, role_id, scope_type, scope_id)
		VALUES ($1, $2, 'tenant', $3)
		ON CONFLICT (user_id, role_id, scope_type, scope_id) DO NOTHING
	`, userID, adminRoleID, tenantID)
	if err != nil {
		panic(err)
	}

	fmt.Println("User linked to Admin role successfully.")

	// 4. Verify camera.view permission
	var count int
	db.QueryRow("SELECT count(*) FROM permissions WHERE name = 'camera.view'").Scan(&count)
	if count == 0 {
		fmt.Println("WARNING: camera.view permission missing!")
	} else {
		fmt.Println("camera.view permission exists.")
	}
}
