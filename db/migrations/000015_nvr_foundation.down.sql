DROP TABLE IF EXISTS nvr_credentials;
DROP TABLE IF EXISTS camera_nvr_links;
DROP TABLE IF EXISTS nvrs;

-- Optional: Clean up permissions? Usually not strict requirement for down migration to clean data, but safe to leave or remove.
-- Leaving permissions is safer to avoid breaking if re-upping.
