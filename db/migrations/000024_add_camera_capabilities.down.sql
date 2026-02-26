-- Remove the GIN index
DROP INDEX IF EXISTS idx_cameras_capabilities;

-- Remove the capabilities column from the cameras table
ALTER TABLE cameras 
DROP COLUMN IF EXISTS capabilities;
