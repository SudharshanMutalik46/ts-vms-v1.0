# Database Migrations

We use a Go-based migration tool compliant with `golang-migrate` standards.

## 1. Migration Structure
Migrations are stored in `db/migrations/`.
Naming convention: `NNNNNN_name.up.sql` and `NNNNNN_name.down.sql`.
- `NNNNNN`: 6-digit sequence number (e.g., `000001`).
- `name`: Descriptive snake_case name.

## 2. Running Migrations
We provide a helper tool `cmd/migrator`.

### Build
```bash
go build -o migrator.exe ./cmd/migrator
```

### Usage
```bash
# Apply all up migrations
./migrator.exe -up

# Rollback one step
./migrator.exe -down
```

### Environment Config
The tool reads `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` from environment variables.

## 3. Creating a New Migration
1. Create new files in `db/migrations/`:
   - `00000X_description.up.sql`
   - `00000X_description.down.sql`
2. Add DDL statements.
3. **Rules:**
   - **Additive Only:** Do not drop columns used by previous versions.
   - **Idempotent:** Use `IF NOT EXISTS` where possible.
   - **Safe:** No long-running locks on huge tables (use concurrent index creation if needed).

## 4. Verification
Check the `schema_migrations` table in the database to see the currently applied version.
