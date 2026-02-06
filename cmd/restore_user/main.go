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

	// Create/Restore User
	_, err = db.Exec(`
		INSERT INTO users (id, tenant_id, email, password_hash, role, created_at, updated_at)
		VALUES ($1, $2, 'admin@ts.com', 'hash', 'admin', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, userID, tenantID)

	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("User restored.")
}
