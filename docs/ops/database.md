# PostgreSQL Database Infrastructure

## 1. Requirements
- **Version:** PostgreSQL 14 or higher.
- **Extensions:** `pgcrypto` (for UUID generation), `uuid-ossp` (optional if using pgcrypto gen_random_uuid()).

## 2. Configuration
The application expects the database connection string to be provided via environment variables.

### Environment Variables
| Variable | Description | Example |
| :--- | :--- | :--- |
| `DB_HOST` | Database Hostname | `localhost` |
| `DB_PORT` | Database Port | `5432` |
| `DB_USER` | Database Username | `postgres` |
| `DB_PASSWORD` | Database Password | `securepassword` |
| `DB_NAME` | Database Name | `ts_vms` |
| `DB_SSLMODE` | SSL Mode | `disable` (dev), `verify-full` (prod) |

### Recommended Settings (postgresql.conf)
- **Timezone:** `UTC` (Crucial for audit logs and retention).
- **Max Connections:** 100 (Adjust based on Service architecture).
- **Statement Timeout:** `30000` (30s) to prevent runaway queries.
- **Log Statement:** `ddl` (Log schema changes).

## 3. Naming Conventions
- **Tables:** `sneak_case`, plural (e.g., `users`, `audit_logs`).
- **Columns:** `sneak_case` (e.g., `created_at`, `tenant_id`).
- **Primary Keys:** `id` (UUID).
- **Foreign Keys:** `<table>_id` (e.g., `tenant_id`).
- **Indexes:** `idx_<table>_<column>` (e.g., `idx_users_email`).

## 4. Multi-Tenancy Strategy
We use **Row-Level Security (RLS)**.
- Every tenant-scoped table MUST have an RLS policy.
- The application MUST set the current tenant context at the start of a transaction:
  ```sql
  SET LOCAL app.tenant_id = 'uuid-of-tenant';
  ```
- **Superuser/Admin:** Can bypass RLS (BYPASSRLS trait), but application roles should not.
