-- Seed verified logistics and support specialist identities with unique mobile numbers for unified phone+OTP authentication.
INSERT INTO users (id, mobile, full_name, role, username, is_active, created_at, updated_at)
VALUES
    ('a0000000-0000-0000-0000-000000000044', '09000000044', 'رضا افشار (سرپرست عملیات انبار و لجستیک)', 'logistics_specialist', 'logistics_reza', TRUE, NOW(), NOW()),
    ('a0000000-0000-0000-0000-000000000055', '09000000055', 'نگار احمدی (سرپرست پشتیبانی و نظارت)', 'support_specialist', 'support_negar', TRUE, NOW(), NOW())
ON CONFLICT (mobile) DO UPDATE SET
    role = EXCLUDED.role,
    full_name = EXCLUDED.full_name,
    username = EXCLUDED.username,
    is_active = TRUE,
    updated_at = NOW();