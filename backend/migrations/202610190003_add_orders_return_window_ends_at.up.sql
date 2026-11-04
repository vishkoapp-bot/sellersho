-- Fresh-DB fix: 202610190004_profit_lock_view references orders.return_window_ends_at,
-- but no earlier migration creates it. This file sorts BEFORE 202610190004
-- (202610190003_add_... < 202610190003_supplier_approval alphabetically, and both
-- are before 202610190004), so a fresh DB gets the column before the view is created.
-- On live this is a no-op: the column already exists there.
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS return_window_ends_at TIMESTAMPTZ;
