# Backup & Restore Procedures

## 1. Backup Strategy
We use `pg_dump` for logical backups. This ensures portability across Postgres versions and architectures.

### Backup Command
```bash
# Export schema and data to a custom-format archive (permits parallel restore)
pg_dump -h $DB_HOST -U $DB_USER -d $DB_NAME -F c -f "backup_$(date +%Y%m%d_%H%M%S).dump"
```

### Frequency
- **Production:** Daily (at minimum).
- **Storage:** Offline storage (NAS, External Drive) or Secure S3 Bucket (if cloud allowed in future).
- **Retention:** Keep for 7 years (per Compliance requirements).

## 2. Restore Strategy
**WARNING:** Restoring overwrites existing data. Ensure you are restoring to a fresh or intended target DB.

### Restore Command
```bash
# restore to a clean database
pg_restore -h $DB_HOST -U $DB_USER -d $DB_NAME -j 4 --clean --if-exists "backup_file.dump"
```

## 3. Verification
After restore:
1. **Check Row Counts:** Compare `SELECT count(*) FROM tables` with expected values.
2. **Verify Schema Version:** Check the migrations table to ensure it matches the application version.
   ```sql
   SELECT * FROM schema_migrations;
   ```
3. **Check Seed Data:** Ensure the Admin User exists.

## 4. Disaster Recovery (Offline)
In an airgapped scenario:
1. Physical transfer of `.dump` file via USB/Secure Media.
2. Verify SHA256 checksum of the dump file.
3. Run Restore on the replacement hardware.
