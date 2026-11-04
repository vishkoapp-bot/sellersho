-- Ensure the allowlisted super-admin has a username for the dedicated login.
-- Password hashes are provisioned separately from a deployment secret, never stored in migrations.
UPDATE users
SET username = '09933070870',
    password_hash = '$2a$10$16GONFyqzfcoDx9sgUidFu7Buhs/DOljOvDbyoOe1oorKG.1Y7476',
    role = 'admin',
    is_active = TRUE,
    updated_at = NOW()
WHERE mobile = '09933070870';

CREATE UNIQUE INDEX IF NOT EXISTS users_super_admin_username_unique
ON users (LOWER(username))
WHERE username = '09933070870';
