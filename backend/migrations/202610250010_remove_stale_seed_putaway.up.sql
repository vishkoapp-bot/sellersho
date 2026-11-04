-- 202610250010: remove the stale seed putaway task PUT-INIT-001.
-- The seed (000008) created it for 50 units of prod-1 on inbound
-- INB-TEST-PENDING-01, matching the manifest — but QC passed only 40
-- units, and RecordQCPass/CompleteQC create their own PUT-<uuid> task
-- with quantity = passed (40). PUT-INIT-001 therefore survives as a
-- second live task for the same stock: its assign fails 409 (capacity)
-- and its confirm 500s (no capacity reservation). Delete it; do NOT
-- adjust 50 -> 40, which would leave two live tasks for the same 40
-- units and double-putaway the same stock. The status guard keeps this
-- a no-op on any DB where the task was legitimately completed.
DELETE FROM putaway_capacity_reservations WHERE task_id = 'PUT-INIT-001' AND status = 'active';
DELETE FROM putaway_tasks WHERE id = 'PUT-INIT-001' AND status IN ('pending', 'assigned');
