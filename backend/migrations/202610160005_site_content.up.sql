-- Site content management: footer, contact info, trust features, static pages
-- Makes hardcoded customer-facing content editable via super-admin panel

CREATE TABLE IF NOT EXISTS site_content (
    key VARCHAR(100) PRIMARY KEY,
    value TEXT NOT NULL DEFAULT '',
    value_type VARCHAR(20) NOT NULL DEFAULT 'text',
    title VARCHAR(200) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS static_pages (
    key VARCHAR(50) PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed footer/contact defaults (from current hardcoded values)
INSERT INTO site_content (key, value, value_type, title) VALUES
('footer.tagline', 'فروشگاه آنلاین کالا و خدمات', 'text', 'زیرعنوان فوتر'),
('footer.about_text', 'خرید آنلاین انواع کالای دیجیتال، مد، طلا و لوازم خانگی با ضمانت اصالت فیزیکی و تحویل سریع اکسپرس به سراسر ایران.', 'text', 'متن درباره فوتر'),
('footer.phone', '021 - 9100 8080', 'text', 'تلفن تماس'),
('footer.email', 'support@sellershow.ir', 'text', 'ایمیل پشتیبانی'),
('footer.address', 'تهران، مجتمع فناوری و لجستیک مرکزی', 'text', 'آدرس'),
('footer.copyright', '© ۱۴۰۵ سلرشو. کلیه حقوق محفوظ است.', 'text', 'متن کپی‌رایت'),
('footer.show_dev_credit', 'false', 'boolean', 'نمایش اعتبار توسعه‌دهنده'),
('header.search_placeholders', '["هندزفری، ساعت هوشمند، ایرپاد...", "پلاک طلا ۱۸ عیار، گوشواره...", "سرخ‌کن، قهوه‌ساز...", "کیف چرم، هودی اورسایز...", "کرم ضدچروک، جوانساز صورت..."]', 'json', 'پیشنهادهای جستجو در هدر')
ON CONFLICT (key) DO NOTHING;

-- Seed trust features as JSON (from current hardcoded values)
INSERT INTO site_content (key, value, value_type, title) VALUES
('trust_features', '[
  {"icon": "CreditCard", "title": "پرداخت امن درگاه مرکزی", "description": "تسویه مطمئن شتابی با صدور آنی فاکتور رسمی", "color": "violet"},
  {"icon": "Truck", "title": "ارسال سریع از انبار مرکزی", "description": "پردازش مکانیزه و تحویل به سراسر کشور", "color": "violet"},
  {"icon": "ShieldCheck", "title": "تضمین اصالت و کیفیت", "description": "تأمین مستقیم از برندها با گارانتی معتبر", "color": "emerald"},
  {"icon": "RotateCcw", "title": "۷ روز ضمانت بازگشت", "description": "امکان عودت و تعویض در صورت مغایرت", "color": "amber"},
  {"icon": "Headphones", "title": "پشتیبانی ۲۴ ساعته", "description": "همراهی شما در تمام مراحل خرید و تحویل", "color": "rose"}
]', 'json', 'ویژگی‌های اعتماد (۴ مورد نمایش داده می‌شود)')
ON CONFLICT (key) DO NOTHING;

-- Seed static pages (from current hardcoded values)
INSERT INTO static_pages (key, title, content, sort_order) VALUES
('about', 'درباره فروشگاه سلرشو', 'سلرشو بازارگاه آنلاین کالا و خدمات با شبکه گسترده تأمین، انبارداری استاندارد و ارسال مستقیم به سراسر کشور است. هدف ما فراهم آوردن بستری امن و مطمئن با بهترین قیمت، اصالت قطعی فیزیکی کالاها و ضمانت بازگشت وجه برای خریداران محترم می‌باشد.', 10),
('terms', 'شرایط و قوانین خدمات', '۱. بازرسی در انبار: کلیه اقلام قبل از ارسال در ایستگاه QC بارکدگذاری و بازرسی می‌شوند.\n\n۲. مهلت تست ۷ روزه: خریدار تا ۷ روز پس از دریافت مرسوله حق عودت در صورت مغایرت دارد.', 20),
('privacy', 'حریم خصوصی و امنیت', 'اطلاعات خریداران صرفاً جهت پردازش سفارش، صدور بارنامه پستی و ارائه خدمات پشتیبانی استفاده می‌شود. تمامی داده‌ها با رعایت اصول امنیتی نگهداری و رمزنگاری می‌شود.', 30),
('faq', 'سوالات متداول', '[{"q": "چگونه می‌توانم وضعیت و کد رهگیری سفارش خود را پیگیری کنم؟", "a": "پس از ثبت سفارش، کد رهگیری پستی صادر شده و از طریق آیکون پیگیری مرسوله در بالای سایت، با وارد کردن شماره رهگیری در هر لحظه وضعیت فیزیکی بسته قابل مشاهده است."}, {"q": "شرایط مهلت تست ۷ روزه و ضمانت سلامت فیزیکی چیست؟", "a": "کلیه کالاها قبل از بسته‌بندی تحت کنترل کیفی فیزیکی قرار می‌گیرند و خریدار تا ۷ روز کاری پس از تحویل، در صورت هرگونه نقص یا عدم تطابق می‌تواند درخواست عودت وجه یا تعویض ثبت کند."}, {"q": "شیوه ارسال و زمان تحویل سفارش‌ها چگونه است؟", "a": "سفارش‌ها با بسته‌بندی ایمن پستی و باربری معتبر اکسپرس ارسال می‌شوند؛ مدت زمان تحویل برای تهران ۲۴ ساعت و برای سایر استان‌ها ۲ الی ۳ روز کاری است."}]', 40),
('license', 'مجوزها', 'مجوزهای رسمی سلرشو در این بخش نمایش داده می‌شود.', 50)
ON CONFLICT (key) DO NOTHING;
