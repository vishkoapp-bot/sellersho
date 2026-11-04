-- Rollback for tpl-electronic-1 registration.
-- Stores using tpl-electronic-1 fall back to tpl-modern-1.
UPDATE stores SET template_id = 'tpl-modern-1' WHERE template_id = 'tpl-electronic-1';
DELETE FROM store_templates WHERE id = 'tpl-electronic-1';
