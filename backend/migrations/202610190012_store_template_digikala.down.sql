-- Rollback for tpl-digikala-1 registration.
-- NOTE: stores currently using this template keep their template_id value;
-- reassign them before rolling back in production.
DELETE FROM store_templates WHERE id = 'tpl-digikala-1';
