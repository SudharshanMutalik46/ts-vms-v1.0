-- 000008_audit_event_id.up.sql

-- Add event_id column if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='audit_logs' AND column_name='event_id') THEN
        ALTER TABLE audit_logs ADD COLUMN event_id UUID;
    END IF;
END $$;

-- Populate existing rows with random UUIDs to allow unique constraint creation
UPDATE audit_logs SET event_id = gen_random_uuid() WHERE event_id IS NULL;

-- Make it NOT NULL
ALTER TABLE audit_logs ALTER COLUMN event_id SET NOT NULL;

-- Add Unique Constraint for Idempotency
ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS uq_audit_event_id;
ALTER TABLE audit_logs ADD CONSTRAINT uq_audit_event_id UNIQUE (event_id);
