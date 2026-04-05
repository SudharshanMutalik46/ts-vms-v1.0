-- Add persistent codec preference for recording startup failover
ALTER TABLE cameras
ADD COLUMN IF NOT EXISTS preferred_recording_codec VARCHAR(16);
