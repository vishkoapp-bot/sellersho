-- Down migration for 202611190001 (FIN-009 seller-profit reversals).
-- Drops the reversal records table and its immutability trigger.
-- Only run on a disposable database: a down migration on prod would
-- destroy reversal evidence and must never run there.
DROP TRIGGER IF EXISTS fin009_seller_profit_reversal_immutable ON seller_profit_reversals;
DROP TABLE IF EXISTS seller_profit_reversals;
