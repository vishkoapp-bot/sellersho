-- 202610260001 (down): remove the stuck-order auto-expiry kill-switch.
ALTER TABLE platform_settings
    DROP COLUMN IF EXISTS stuck_order_expiry_enabled;
