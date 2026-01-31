-- 000008_audit_event_id.down.sql
ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS uq_audit_event_id;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS event_id;
