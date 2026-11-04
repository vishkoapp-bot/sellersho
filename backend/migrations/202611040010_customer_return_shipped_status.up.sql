-- 202611040010 (Fix69 O1): ShipCustomerReturn sets customer_returns
-- status='shipped', but the CHECK (defined in the candidate chain by
-- 202610160006 and redefined by 202610190005 to add 'cancelled')
-- admitted only requested, approved, rejected, received, refunded,
-- completed, cancelled — so every ship attempt failed with a 500 on
-- CHECK violation. Admit 'shipped'.
--
-- Evidence the state is intended, not invented: ReceiveCustomerReturn
-- already accepts a prior status of 'shipped' (the ship-then-receive
-- flow), and shipped_at/tracking_number/prepaid columns already exist
-- on customer_returns. This migration only widens the CHECK; it changes
-- no existing data and the Go code is unchanged (ShipCustomerReturn
-- already writes 'shipped').
ALTER TABLE customer_returns DROP CONSTRAINT IF EXISTS customer_returns_status_check;
ALTER TABLE customer_returns ADD CONSTRAINT customer_returns_status_check
  CHECK (status IN ('requested', 'approved', 'rejected', 'received', 'refunded', 'completed', 'cancelled', 'shipped'));
