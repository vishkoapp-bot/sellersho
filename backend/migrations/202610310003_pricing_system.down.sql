-- 202610310003 DOWN: checked 2026-10-31 against the pre-0003 chain —
-- no pre-existing definition was found for any object dropped below:
-- product_price_exceptions, product_market_ceilings and
-- supplier_price_increases are 0003-only tables; support_tickets.section
-- and platform_settings.{platform_pct, central_profit_pct,
-- increase_cap_pct, increase_locked} are 0003-only columns (the older
-- chain creates support_tickets without section and never adds those
-- platform_settings columns). The drops are therefore safe as written;
-- no object is withheld here, unlike 0002's down.
DROP TABLE IF EXISTS product_price_exceptions;
DROP TABLE IF EXISTS product_market_ceilings;
DROP TABLE IF EXISTS supplier_price_increases;
ALTER TABLE support_tickets DROP COLUMN IF EXISTS section;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS increase_locked;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS increase_cap_pct;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS central_profit_pct;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS platform_pct;
