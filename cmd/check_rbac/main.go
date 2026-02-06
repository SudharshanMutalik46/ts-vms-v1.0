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

	checkTable := func(name string) {
		var count int
		err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", name)).Scan(&count)
		if err != nil {
			fmt.Printf("Table %s: Error %v\n", name, err)
			return
		}
		fmt.Printf("Table %s: %d rows\n", name, count)
	}

	checkTable("roles")
	checkTable("permissions")
	checkTable("role_permissions")
	checkTable("user_roles")

	// Print roles
	rows, _ := db.Query("SELECT id, name FROM roles")
	fmt.Println("\nRoles:")
	for rows.Next() {
		var id, name string
		rows.Scan(&id, &name)
		fmt.Printf("- %s (%s)\n", name, id)
	}
	rows.Close()

	// Print permissions
	rows, _ = db.Query("SELECT id, name FROM permissions")
	fmt.Println("\nPermissions:")
	for rows.Next() {
		var id, name string
		rows.Scan(&id, &name)
		fmt.Printf("- %s (%s)\n", name, id)
	}
	rows.Close()
}
