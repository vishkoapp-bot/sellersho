DROP TABLE checkout_attempts;
DROP FUNCTION guard_checkout_attempt();
DROP TRIGGER checkout_item_title_guard ON order_items;
DROP FUNCTION guard_checkout_item_title();
ALTER TABLE order_items DROP COLUMN product_title_snapshot;
