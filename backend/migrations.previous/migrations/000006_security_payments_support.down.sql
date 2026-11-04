DROP TABLE IF EXISTS support_ticket_messages CASCADE;
DROP TABLE IF EXISTS support_tickets CASCADE;
DROP INDEX IF EXISTS payment_transactions_one_active_per_order;
DROP TABLE IF EXISTS payment_transactions CASCADE;
DROP INDEX IF EXISTS users_username_unique;
DROP INDEX IF EXISTS orders_customer_user_id_idx;
ALTER TABLE orders
    DROP COLUMN IF EXISTS customer_user_id,
    DROP COLUMN IF EXISTS payment_status;
ALTER TABLE suppliers DROP COLUMN IF EXISTS owner_user_id;
ALTER TABLE users
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS failed_login_attempts,
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS username;