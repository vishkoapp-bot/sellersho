ALTER TABLE products
ADD COLUMN IF NOT EXISTS supply_option VARCHAR(20) NOT NULL DEFAULT '1day'
CHECK (supply_option IN ('3hour_local', '1day', '3day'));

COMMENT ON COLUMN products.supply_option IS 'گزینه تامین: 3hour_local=سه ساعته شهر تامین‌کننده, 1day=یک روزه استاندارد, 3day=سه روزه';
