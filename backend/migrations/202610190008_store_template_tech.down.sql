-- Rollback for tpl-tech-1 registration.
-- Stores using tpl-tech-1 keep their template_id (SET NULL on delete); reset
-- them to the modern template before deleting the row.
UPDATE stores SET template_id = 'tpl-modern-1' WHERE template_id = 'tpl-tech-1';
DELETE FROM store_templates WHERE id = 'tpl-tech-1';
