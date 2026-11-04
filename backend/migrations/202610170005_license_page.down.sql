-- Rollback for 202610170005_license_page: restore the previous placeholder row.
INSERT INTO static_pages (key, title, content, sort_order, is_active) VALUES
('license', 'مجوزها', 'مجوزهای رسمی سلرشو در این بخش نمایش داده می‌شود.', 50, TRUE)
ON CONFLICT (key) DO UPDATE SET
    title = EXCLUDED.title,
    content = EXCLUDED.content,
    sort_order = EXCLUDED.sort_order,
    is_active = TRUE,
    updated_at = NOW();
