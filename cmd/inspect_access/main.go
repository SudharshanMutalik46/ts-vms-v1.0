package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	connStr := "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	camID := "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
	userID := "00000000-0000-0000-0000-000000000002"

	// 1. Check Camera Status
	var deletedAt *time.Time
	var tenantID string
	err = db.QueryRow("SELECT tenant_id, deleted_at FROM cameras WHERE id = $1", camID).Scan(&tenantID, &deletedAt)
	if err != nil {
		fmt.Printf("Camera Lookup Error: %v\n", err)
	} else {
		fmt.Printf("Camera: Tenant=%s, DeletedAt=%v\n", tenantID, deletedAt)
	}

	// 2. Check User Status
	var userTenant string
	var role string
	err = db.QueryRow("SELECT tenant_id, role FROM users WHERE id = $1", userID).Scan(&userTenant, &role)
	if err != nil {
		fmt.Printf("User Lookup Error: %v\n", err)
	} else {
		fmt.Printf("User: Tenant=%s, Role=%s\n", userTenant, role)
	}
}
