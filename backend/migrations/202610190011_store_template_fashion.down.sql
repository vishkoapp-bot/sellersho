-- Rollback: remove the Fashion storefront template registration.
DELETE FROM store_templates WHERE id = 'tpl-fashion-1';
