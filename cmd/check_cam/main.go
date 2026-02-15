package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("DB_USER")
	if user == "" {
		user = "postgres"
	}
	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		password = "ts1234"
	}
	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "ts_vms"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, dbname)
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
