-- CMS-editable footer link labels + social links (bug army: footer labels hardcoded, social links not editable)
INSERT INTO site_content (key, value, value_type, title) VALUES
('footer.link_about', 'درباره فروشگاه سلرشو', 'text', 'فوتر: برچسب لینک درباره ما'),
('footer.link_terms', 'قوانین و ضمانت ۷ روزه', 'text', 'فوتر: برچسب لینک قوانین'),
('footer.link_privacy', 'حریم خصوصی و امنیت', 'text', 'فوتر: برچسب لینک حریم خصوصی'),
('footer.link_faq', 'پرسش‌های متداول', 'text', 'فوتر: برچسب لینک سوالات متداول'),
('footer.link_license', 'مجوزها و نمادها', 'text', 'فوتر: برچسب لینک مجوزها'),
('footer.link_supplier', 'همکاری تأمین‌کننده', 'text', 'فوتر: برچسب لینک تأمین‌کننده'),
('footer.link_dropship', 'راه‌اندازی دراپ‌شیپینگ', 'text', 'فوتر: برچسب لینک دراپ‌شیپینگ'),
('footer.link_warehouse', 'ورود عملیات انبار', 'text', 'فوتر: برچسب لینک انبار'),
('footer.social_instagram', '', 'text', 'فوتر: لینک اینستاگرام (خالی = مخفی)'),
('footer.social_telegram', '', 'text', 'فوتر: لینک تلگرام (خالی = مخفی)'),
('footer.social_whatsapp', '', 'text', 'فوتر: لینک واتس‌اپ (خالی = مخفی)')
ON CONFLICT (key) DO NOTHING;
