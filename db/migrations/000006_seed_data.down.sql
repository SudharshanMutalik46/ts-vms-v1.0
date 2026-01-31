-- 000006_seed_data.down.sql
-- We generally don't delete data in down migrations unless strictly schema revert.
-- But for development ensuring clean state:
DELETE FROM tenants WHERE id = '00000000-0000-0000-0000-000000000001';
DELETE FROM permissions;
