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

	var ip string
	err = db.QueryRow("SELECT ip_address FROM cameras WHERE id = '6ed6cf65-a421-4f5f-bfa3-363f33dbf23a'").Scan(&ip)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("IP: %s\n", ip)
}
