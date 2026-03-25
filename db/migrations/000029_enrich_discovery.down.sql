-- 000029_enrich_discovery.down.sql

ALTER TABLE onvif_discovered_devices
  DROP COLUMN IF EXISTS mac_address,
  DROP COLUMN IF EXISTS snapshot_uri,
  DROP COLUMN IF EXISTS clock_offset_sec,
  DROP COLUMN IF EXISTS supports_audio,
  DROP COLUMN IF EXISTS supports_events,
  DROP COLUMN IF EXISTS supports_ptz,
  DROP COLUMN IF EXISTS event_topics;
