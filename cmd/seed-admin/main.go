package main

import (
"database/sql"
"fmt"
"log"
"os"

"github.com/google/uuid"
_ "github.com/lib/pq"
)

func main() {
dbHost := os.Getenv("DB_HOST")
if dbHost == "" { dbHost = "localhost" }
dbUser := os.Getenv("DB_USER")
if dbUser == "" { dbUser = "postgres" }
dbPass := os.Getenv("DB_PASSWORD")
if dbPass == "" { dbPass = "ts1234" }
dbName := os.Getenv("DB_NAME")
if dbName == "" { dbName = "ts_vms" }

connStr := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", dbUser, dbPass, dbHost, dbName)
db, err := sql.Open("postgres", connStr)
if err != nil { log.Fatal(err) }
defer db.Close()

// 1. Ensure Tenant Exists
var tenantID string
err = db.QueryRow("SELECT id FROM tenants WHERE name = 'Default Tenant'").Scan(&tenantID)
if err != nil { err = nil; tenantID = uuid.New().String(); db.Exec("INSERT INTO tenants (id, name, created_at, updated_at) VALUES (, 'Default Tenant', NOW(), NOW())", tenantID) }

// 2. Ensure Role Exists
var roleID string
err = db.QueryRow("SELECT id FROM roles WHERE name = 'System Admin' AND tenant_id = ", tenantID).Scan(&roleID)
if err != nil { roleID = uuid.New().String(); db.Exec("INSERT INTO roles (id, tenant_id, name, created_at, updated_at) VALUES (, , 'System Admin', NOW(), NOW())", roleID, tenantID) }

// 3. Grant Permissions
permName := "nvr.health.read"
var permID string
err = db.QueryRow("SELECT id FROM permissions WHERE name = ", permName).Scan(&permID)
if err != nil { permID = uuid.New().String(); db.Exec("INSERT INTO permissions (id, name, description, created_at, updated_at) VALUES (, , 'Read NVR Health', NOW(), NOW())", permID, permName) }
db.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (, ) ON CONFLICT DO NOTHING", roleID, permID)

perms := []string{"nvr.discovery.read", "cameras.read", "nvr.read", "nvr.channel.write"}
for _, p := range perms {
var pid string
db.QueryRow("SELECT id FROM permissions WHERE name = ", p).Scan(&pid)
if pid == "" { pid = uuid.New().String(); db.Exec("INSERT INTO permissions (id, name, description, created_at, updated_at) VALUES (, , 'Auto', NOW(), NOW())", pid, p) }
db.Exec("INSERT INTO role_permissions (role_id, permission_id) VALUES (, ) ON CONFLICT DO NOTHING", roleID, pid)
}
    fmt.Println("Permissions updated.")
}
