//go:build ignore

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

	rows, err := db.Query("SELECT id, tenant_id, name, ip_address FROM cameras")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("ID | TenantID | Name | IP")
	fmt.Println("------------------------------------------------------------------")
	for rows.Next() {
		var id, tid, name, ip string
		rows.Scan(&id, &tid, &name, &ip)
		fmt.Printf("%s | %s | %s | %s\n", id, tid, name, ip)
	}
}
//go:build ignore


