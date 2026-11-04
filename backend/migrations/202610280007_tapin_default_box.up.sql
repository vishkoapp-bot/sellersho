-- 202610280007: configurable default Tapin box for the quote path
-- (Oct 28, 2026 — Tapin quote v2 refinement, user spec ~21:36 Tehran).
--
--   platform_settings.tapin_default_box_id TEXT NOT NULL DEFAULT '4'
--
-- The default box the Tapin quote assumes when it cannot auto-pick the
-- nearest box from the shipment's volume (see pickTapinBoxForLeg in
-- cmd/server/shipping_tapin_quote.go). Super-admin tunable from the
-- commission tab's shipping section (PUT /admin/settings/platform,
-- key `tapin_default_box_id` — the single writer for this setting; the
-- shipping-rates endpoint deliberately does NOT carry it); current
-- value 4 — «سایز ۴ (30×20×20 cm)»,
-- the user-confirmed «کارتون پستی شماره ۴» (Tapin PackingBoxes pk=4,
-- live-verified Oct 28, 2026).
--
-- The code fallback tapin.DefaultBoxID ("4", internal/tapin — the
-- shared constant the quote path consumes) stays as the value used
-- when this setting is
-- unset, blank, or not a plausible Tapin box id — the setting is the
-- source of truth, the constant is the fail-safe, never the sole source.
--
-- DEPLOY NOTE: GetPlatformSettings selects this column explicitly, so
-- the binary built with this change REQUIRES this migration in the same
-- deploy window (same pattern as return_window_days / volumetric_divisor
-- before it). A binary without the migration against a DB without the
-- column fails every settings read.
--
-- IDEMPOTENCY: ADD COLUMN IF NOT EXISTS, so re-running UP is a no-op.
ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS tapin_default_box_id TEXT NOT NULL DEFAULT '4';

-- 202610280007 (part 2 — Tapin return settlement, Oct 28, 2026):
--
--   (a) platform_settings.tapin_platform_token / tapin_platform_shop_id:
--       the PLATFORM Tapin account ("the Tapin API connected to the
--       central system"). CORRECTION (Oct 29, 2026, deploy-all
--       assembly — the original header predated the tariff pivot):
--       the creation-time return quote is priced from the national
--       post tariff 1405 (migration 202610280009) and does NOT need
--       this token; the platform account is only the check-price
--       FALLBACK/cross-check when the tariff cannot cover a lane.
--       The token is stored AES-256-GCM "enc:v1:" with TAPIN_TOKEN_KEY,
--       exactly like suppliers.tapin_token; it is NEVER selected into
--       PlatformSettings or any DTO — only GetPlatformTapinCredentials
--       reads it, and only the masked form is ever displayed (super-
--       admin only, PUT /admin/settings/tapin-platform). NULL = not
--       configured: the check-price fallback is then unavailable —
--       never a fare, never zero, never a silently substituted
--       supplier token. Settlement NEVER reads these columns.
--
--   (b) customer_returns return_shipping_quote_*: the creation-time
--       quote snapshot (O-12): return_shipping_quote_toman (BIGINT,
--       NULL when no quote was available at creation),
--       return_shipping_quote_at, return_shipping_quote_source
--       ('tariff' | 'checkprice'), and return_shipping_quote_groups (JSONB:
--       the per-group destinations + fares, i.e. the same `groups`
--       array the quote endpoint returns). The groups JSON is also
--       the record of each return's per-group destinations (a
--       companion table was rejected: destinations are a snapshot
--       read only ever together with the return row, never queried
--       across returns).
ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS tapin_platform_token TEXT,
  ADD COLUMN IF NOT EXISTS tapin_platform_shop_id TEXT;
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS return_shipping_quote_toman BIGINT,
  ADD COLUMN IF NOT EXISTS return_shipping_quote_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS return_shipping_quote_source TEXT,
  ADD COLUMN IF NOT EXISTS return_shipping_quote_groups JSONB;
