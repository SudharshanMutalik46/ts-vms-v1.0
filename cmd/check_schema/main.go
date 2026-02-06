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

	rows, err := db.Query("SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'users'")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("Users table columns:")
	for rows.Next() {
		var name, dtype string
		if err := rows.Scan(&name, &dtype); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("- %s (%s)\n", name, dtype)
	}
}
