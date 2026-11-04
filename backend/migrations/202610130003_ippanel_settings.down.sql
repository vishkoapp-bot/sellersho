ALTER TABLE platform_settings
  DROP COLUMN IF EXISTS ippanel_enabled,
  DROP COLUMN IF EXISTS ippanel_base_url,
  DROP COLUMN IF EXISTS ippanel_api_key,
  DROP COLUMN IF EXISTS ippanel_sender,
  DROP COLUMN IF EXISTS ippanel_pattern_code;
