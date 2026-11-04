-- 202610230006: Customer address book + Tapin geo snapshots.
--
-- user_addresses: one row per saved customer address. Checkout either
-- references a saved address (addressId) or saves the typed address
-- (saveAddress), so later purchases can reuse it. Province/city TAPIN
-- codes are stored as opaque JSONB (numbers or strings — passed back to
-- Tapin verbatim); the display names are frozen alongside for rendering
-- and for the legacy name-based order columns.
--
-- At most one default address per user, enforced by a partial unique
-- index; the repository keeps the invariant inside a transaction and
-- promotes the oldest remaining address when the default is deleted.
--
-- tapin_geo_snapshots: durable copy of Tapin's geography payloads
-- (kind 'tree' for POST /state/tree/, 'cities:<province_code>' reserved
-- for per-province city lists). Refreshed best-effort whenever a supplier
-- connects/tests Tapin (the only moment a Tapin token is at hand) and
-- served to customers without any token via /api/v1/geo/*.

CREATE TABLE IF NOT EXISTS user_addresses (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id UUID NOT NULL REFERENCES users(id),
  label TEXT,
  receiver_full_name TEXT NOT NULL,
  receiver_phone TEXT,
  province_name TEXT NOT NULL,
  province_code JSONB,
  city_name TEXT NOT NULL,
  city_code JSONB,
  postal_code TEXT,
  address_detail TEXT NOT NULL,
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_addresses_user
  ON user_addresses (user_id, is_default DESC, created_at ASC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_addresses_one_default
  ON user_addresses (user_id) WHERE is_default;

CREATE TABLE IF NOT EXISTS tapin_geo_snapshots (
  kind TEXT PRIMARY KEY,
  payload JSONB NOT NULL,
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
