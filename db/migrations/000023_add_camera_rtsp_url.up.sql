-- Migration: Add rtsp_url column to cameras table
-- 000023_add_camera_rtsp_url.up.sql

ALTER TABLE cameras ADD COLUMN rtsp_url TEXT;
