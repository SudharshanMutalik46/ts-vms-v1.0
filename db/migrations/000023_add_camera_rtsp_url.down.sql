-- Migration: Add rtsp_url column to cameras table (rollback)
-- 000023_add_camera_rtsp_url.down.sql

ALTER TABLE cameras DROP COLUMN IF EXISTS rtsp_url;
