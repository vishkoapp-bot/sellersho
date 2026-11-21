-- Down migration for 202611210001 (FIN-014 repair batches).
-- Drops the three FIN-014 bookkeeping tables and their triggers.
-- These tables hold only FIN-014 proposal/apply bookkeeping (no
-- ledger, receipt, obligation, or order history), so dropping them
-- loses no posted history — but any proposed/applied batch record is
-- lost with them. Only run on a disposable database.
DROP TRIGGER IF EXISTS fin014_repair_operation_immutable ON fin014_repair_operations;
DROP TRIGGER IF EXISTS fin014_repair_batch_immutable ON fin014_repair_batches;
DROP TRIGGER IF EXISTS fin014_report_run_immutable ON fin014_report_runs;
DROP FUNCTION IF EXISTS fin014_immutable_operation();
DROP FUNCTION IF EXISTS fin014_immutable_batch_once_applied();
DROP FUNCTION IF EXISTS fin014_immutable_report_run();
DROP TABLE IF EXISTS fin014_repair_operations;
DROP TABLE IF EXISTS fin014_repair_batches;
DROP TABLE IF EXISTS fin014_report_runs;
