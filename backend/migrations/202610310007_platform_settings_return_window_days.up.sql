-- 202610310007: platform_settings.return_window_days (live-parity gap).
--
-- P3a found no platform_settings.return_window_days column in the
-- migration chain or in live-base migrations 0001-0006, although the
-- Oct 30 dist PUTs {return_window_days} to /admin/settings/platform
-- and the public handler already reads the column opportunistically
-- (falling back to 7 when it is absent). The P3a platform handler
-- likewise persists the value only when the column exists.
-- This migration closes that gap so the admin-set value is actually
-- stored and read back.
--
-- Column style follows the chain's platform_settings conventions
-- (singleton row id = 1; additive ADD COLUMN IF NOT EXISTS; integer
-- day counts with a NOT NULL DEFAULT, as in 202610310003's
-- platform_pct/central_profit_pct/increase_cap_pct additions and the
-- initial schema's delivery_hold_period_days INT NOT NULL DEFAULT).
-- The default 7 preserves the current fallback behaviour for rows
-- written before this migration, and the CHECK keeps the P3a
-- handler's 1-365 validation honest at the database level.
ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS return_window_days INTEGER NOT NULL DEFAULT 7;
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'platform_settings_return_window_days_range'
  ) THEN
    ALTER TABLE platform_settings
      ADD CONSTRAINT platform_settings_return_window_days_range
      CHECK (return_window_days BETWEEN 1 AND 365);
  END IF;
END $$;
