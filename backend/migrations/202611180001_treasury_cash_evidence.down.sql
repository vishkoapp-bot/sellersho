-- 202611180001_treasury_cash_evidence.down.sql
-- FIN-008: opening cash is retained evidence; refuse to discard it while
-- any evidenced row exists (same pattern as the refund-principal down
-- migration, 202611150004).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM treasury_opening_cash) THEN
        RAISE EXCEPTION 'cannot discard retained treasury opening-cash evidence';
    END IF;
END $$;
DROP TABLE IF EXISTS treasury_opening_cash;
