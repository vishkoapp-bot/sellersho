-- Rollback migration 000008
DROP TABLE IF EXISTS pick_tasks CASCADE;
DROP TABLE IF EXISTS putaway_tasks CASCADE;
DROP TABLE IF EXISTS qc_results CASCADE;
DROP TABLE IF EXISTS inbound_receiving_logs CASCADE;

