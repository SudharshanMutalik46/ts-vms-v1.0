-- 000029_enrich_discovery.up.sql

ALTER TABLE onvif_discovered_devices
  ADD COLUMN mac_address TEXT,
  ADD COLUMN snapshot_uri TEXT,
  ADD COLUMN clock_offset_sec INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN supports_audio BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN supports_events BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN supports_ptz BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN event_topics JSONB NOT NULL DEFAULT '[]'::jsonb;
