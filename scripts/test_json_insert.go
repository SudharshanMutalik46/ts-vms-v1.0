package main

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func main() {
	connStr := "postgres://postgres:ts1234@localhost:5432/ts_vms?sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	// Just try to insert a dummy record or test query on the table structure
	// We'll try to insert a new dummy device status

	// We'll mimic the service.go logic
	// caps := json.RawMessage("{}")

	// We need a valid run ID first? No, foreign key.
	// Let's just create a run quickly.
	var runID uuid.UUID
	err = db.QueryRow("INSERT INTO onvif_discovery_runs (tenant_id, status) VALUES ($1, 'running') RETURNING id", uuid.New()).Scan(&runID)
	if err != nil {
		log.Fatalf("Failed to create run: %v", err)
	}
	fmt.Printf("Created Run: %s\n", runID)

	// Now insert device
	query := `
		INSERT INTO onvif_discovered_devices (
			tenant_id, discovery_run_id, ip_address, endpoint_ref,
			manufacturer, model, 
			-- capabilities, media_profiles, rtsp_uris,
			xaddrs
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	// TEST 5: xaddrs as JSON string
	devID := uuid.New()
	err = db.QueryRow(query,
		uuid.New(), runID, "1.2.3.4", "urn:uuid:"+devID.String(),
		"TestFactory", "TestModel",
		`["http://1.2.3.4/onvif"]`, // Passing as JSON string
	).Scan(&devID)

	if err != nil {
		log.Fatalf("Insert failed: %v", err)
	}

	fmt.Printf("Successfully inserted device: %s\n", devID)
}
