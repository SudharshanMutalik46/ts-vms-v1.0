-- 000010_camera_inventory.down.sql

DROP TABLE IF EXISTS camera_group_members;
DROP TABLE IF EXISTS cameras;
DROP TABLE IF EXISTS camera_groups;
-- We do not drop pg_trgm as it might be used by other features unrelated to this migration eventually.
