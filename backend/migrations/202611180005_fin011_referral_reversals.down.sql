-- Down migration for 202611180005 (FIN-011 referral-commission reversals).
-- Drops the reversal records table and its immutability trigger.
-- Only run on a disposable database: live has no referral history
-- (Q-07 probe: 0 referral rows), but a down migration on prod would
-- destroy reversal evidence and must never run there.
DROP TRIGGER IF EXISTS fin011_reversal_immutable ON referral_commission_reversals;
DROP TABLE IF EXISTS referral_commission_reversals;
