# Standard Operating Procedure (SOP): Database Setup & Migration
**Document ID:** SOP-OPS-001
**Version:** 1.0

## 1. Objective
To initialize the Techno Support VMS PostgreSQL database, apply schema migrations, and seed initial data for development.

## 2. Prerequisites
- **PostgreSQL 14+** installed and running.
- **Go 1.22+** installed.
- **PowerShell** terminal.

## 3. Procedure

### Step 1: Configuration (Environment Variables)
Run the following in PowerShell. **Replace `YOUR_PASSWORD` with your actual Postgres superuser password.**

```powershell
$env:DB_HOST = "localhost"
$env:DB_PORT = "5432"
$env:DB_USER = "postgres"
$env:DB_PASSWORD = "ts1234"  # <--- UPDATE THIS
$env:DB_NAME = "ts_vms"
$env:DB_SSLMODE = "disable"
```

### Step 2: Build Migration Tool
Compile the custom migration utility.

```powershell
go build -o migrator.exe ./cmd/migrator
```

### Step 3: Create Database
If the database does not exist yet:

```powershell
# You might need to provide password again if prompted
createdb -U postgres ts_vms
```

### Step 4: Run Migrations
Apply the schema changes and seed data.

```powershell
./migrator.exe -up
```

**Expected Output:**
```text
Running UP migrations...
Migration UP completed.
```

### Step 5: Verify Installation
Run the verification script to confirm tables and RLS policies are active.

```powershell
$env:PGPASSWORD = $env:DB_PASSWORD
psql -U postgres -d ts_vms -f verification.sql
```

**Expected Output:**
Should show `CHECK 1... PASS`, `CHECK 2... PASS`, etc.
(Note: CHECK 4 might say SKIPPED if running as superuser, this is normal).

## 4. Troubleshooting

**Error: `password authentication failed`**
- **Fix:** Ensure `$env:DB_PASSWORD` is set correctly. Check by running `psql -U postgres -W` and typing it manually.

**Error: `database "ts_vms" already exists`**
- **Fix:** Skip the `createdb` step, or run `dropdb -U postgres ts_vms` to start fresh (WARNING: Data Loss).

**Error: `connection refused`**
- **Fix:** Ensure PostgreSQL service is running (`Get-Service postgresql*`).
