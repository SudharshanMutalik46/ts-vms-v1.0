package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func main() {
	upCmd := flag.Bool("up", false, "Run all up migrations")
	downCmd := flag.Bool("down", false, "Rollback all migrations")
	stepsCmd := flag.Int("steps", 0, "Run +/- steps")
	flag.Parse()

	// 1. Read Env Config
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	sslmode := os.Getenv("DB_SSLMODE")

	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "5432"
	}
	if sslmode == "" {
		sslmode = "disable"
	}

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, dbname, sslmode)

	// 2. Connect to DB
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	// 3. Init Migrate
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("Failed to create migrate driver: %v", err)
	}

	// Assume migrations are in local db/migrations folder relative to execution
	m, err := migrate.NewWithDatabaseInstance(
		"file://db/migrations",
		"postgres", driver)
	if err != nil {
		log.Fatalf("Failed to initialize migrate: %v", err)
	}

	// 4. Run Commands
	start := time.Now()
	if *upCmd {
		log.Println("Running UP migrations...")
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("Migration UP failed: %v", err)
		}
		log.Println("Migration UP completed.")
	} else if *downCmd {
		log.Println("Running DOWN migrations...")
		if err := m.Down(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("Migration DOWN failed: %v", err)
		}
		log.Println("Migration DOWN completed.")
	} else if *stepsCmd != 0 {
		log.Printf("Running %d steps...\n", *stepsCmd)
		if err := m.Steps(*stepsCmd); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("Migration Steps failed: %v", err)
		}
		log.Println("Migration Steps completed.")
	} else {
		log.Println("No command specified. Use -up, -down, or -steps.")
		version, dirty, err := m.Version()
		if err != nil {
			log.Println("No version found (empty db?).")
		} else {
			log.Printf("Current Version: %d, Dirty: %v\n", version, dirty)
		}
	}
	log.Printf("Duration: %v", time.Since(start))
}
