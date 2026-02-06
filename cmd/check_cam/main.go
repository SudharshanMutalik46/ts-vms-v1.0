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

	camID := "6ed6cf65-a421-4f5f-bfa3-363f33dbf23a"
	var tenantID string
	err = db.QueryRow("SELECT tenant_id FROM cameras WHERE id = $1", camID).Scan(&tenantID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Camera Tenant ID: %s\n", tenantID)
}
