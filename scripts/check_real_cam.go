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
		log.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()

	cameraID := "6ed6cf65-a421-4f5f-bfa3-363f3dbf235a"

	fmt.Println("--- Cameras Table ---")
	var name, ip string
	var port int
	err = db.QueryRow("SELECT name, ip_address, port FROM cameras WHERE id = $1", cameraID).Scan(&name, &ip, &port)
	if err != nil {
		fmt.Printf("Error fetching camera: %v\n", err)
	} else {
		fmt.Printf("ID: %s, Name: %s, IP: %s, Port: %d\n", cameraID, name, ip, port)
	}

	fmt.Println("\n--- Stream Selections Table ---")
	var mainURL sql.NullString
	err = db.QueryRow("SELECT main_rtsp_url_sanitized FROM camera_stream_selections WHERE camera_id = $1", cameraID).Scan(&mainURL)
	if err != nil {
		fmt.Printf("Error fetching stream selection: %v\n", err)
	} else {
		fmt.Printf("Main URL: %s\n", mainURL.String)
	}
}
