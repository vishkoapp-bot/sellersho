-- Warehouse pack flow: persist the packaging box size the staff selected at
-- the packing station. The packing form collects it and the pack API already
-- parses it — previously it was silently dropped, so the choice staff made
-- was never recorded (Team 4 warehouse audit, 2026-10-19).

ALTER TABLE orders ADD COLUMN IF NOT EXISTS box_size VARCHAR(80);
