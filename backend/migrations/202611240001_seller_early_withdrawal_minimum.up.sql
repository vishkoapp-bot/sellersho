ALTER TABLE platform_settings
  ADD COLUMN early_withdrawal_minimum_toman BIGINT NOT NULL DEFAULT 1000000,
  ADD CONSTRAINT platform_settings_early_withdrawal_minimum_toman_check
    CHECK (early_withdrawal_minimum_toman BETWEEN 1 AND 9007199254740991);
