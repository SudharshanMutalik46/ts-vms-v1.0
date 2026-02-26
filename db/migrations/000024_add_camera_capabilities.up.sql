-- Add a JSONB capabilities column to the cameras table
ALTER TABLE cameras 
ADD COLUMN capabilities JSONB NOT NULL DEFAULT '{"has_audio": false, "ptz": false}'::jsonb;

-- Create a GIN index for fast querying later
CREATE INDEX idx_cameras_capabilities ON cameras USING GIN (capabilities);
