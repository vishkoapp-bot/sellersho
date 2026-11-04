-- Drup Platform - Seed Data Migration
-- Part 1: Platform Settings, Categories, Niches, Suppliers

-- 1. Platform Settings
INSERT INTO platform_settings (
    id, base_fee_f, min_profit_margin, max_profit_margin, default_margin,
    attribution_model, cookie_days, delivery_hold_period_days, delivery_followup_trigger_days,
    referral_commission_percent, referral_is_enabled, referral_subject_to_delivery_lock,
    seller_contract_version, seller_contract_text, supplier_contract_version, supplier_contract_text
) VALUES (
    1, 100000, 5.00, 35.00, 20.00,
    'last_touch', 30, 20, 10,
    2.00, TRUE, TRUE,
    2, 'قرارداد رسمی همکاری تجاری دراپ‌شیپینگ دروپ (سند شماره ۵۰۴)',
    2, 'قرارداد رسمی تأمین و توزیع کالای دروپ (سند شماره ۵۰۵)'
) ON CONFLICT (id) DO UPDATE SET updated_at = NOW();

-- 2. Categories
INSERT INTO categories (id, title, slug, icon_name, gradient, image_url) VALUES
('cat-digital', 'کالای دیجیتال', 'digital', 'Smartphone', 'from-blue-500 to-indigo-600', 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/digital.png'),
('cat-fashion', 'مد و پوشاک', 'fashion', 'Shirt', 'from-pink-500 to-rose-600', 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/fashion.png'),
('cat-beauty', 'زیبایی و سلامت', 'beauty', 'Sparkles', 'from-purple-500 to-violet-600', 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/beauty.png'),
('cat-appliances', 'لوازم خانگی', 'appliances', 'Home', 'from-amber-500 to-orange-600', 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/appliances.png'),
('cat-gold', 'طلا و زیورآلات', 'gold', 'Coins', 'from-yellow-400 to-amber-500', 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/gold.png')
ON CONFLICT (id) DO NOTHING;

-- 3. Niches (9 Official Niches)
INSERT INTO niches (id, title, slug, category_slug, badge, description) VALUES
('niche-smartwatch', 'ساعت هوشمند و گجت‌های پوشیدنی', 'smartwatch', 'digital', 'پرفروش‌ترین', 'ساعت‌های هوشمند با پشتیبانی اعلان فارسی'),
('niche-powerbank', 'پاوربانک و تجهیزات شارژ سریع', 'powerbank', 'digital', 'ارسال اکسپرس', 'شارژرها و پاوربانک‌های فست‌شارژ استاندارد'),
('niche-headphone', 'هدفون، هندزفری و اسپیکر بلوتوثی', 'headphone', 'digital', 'تضمین اصالت', 'هندزفری‌های بلوتوثی با نویزکنسلینگ'),
('niche-gold18', 'طلا و زیورآلات ۱۸ عیار استاندارد', 'gold-jewelry', 'gold', 'ارسال بیمه‌شده', 'زیورآلات طلای ۱۸ عیار با کد رهگیری پستی'),
('niche-leather', 'کیف و اکسسوری چرم طبیعی', 'leather-bags', 'fashion', 'دست‌دوز تبریز', 'محصولات چرم طبیعی گاوی و تبریز'),
('niche-home-appliances', 'لوازم برقی و پخت‌وپز خانه', 'home-appliances', 'appliances', 'گارانتی معتبر', 'سرخ‌کن بدون روغن، قهوه‌ساز و غذاساز'),
('niche-skincare', 'مراقبت پوست، مو و زیبایی', 'skincare', 'beauty', 'ضمانت بازگشت', 'کرم‌ها و سرم‌های بهداشتی با برچسب اصالت'),
('niche-books', 'کتاب کنکور، رمان و نوشت‌افزار', 'stationery-books', 'digital', 'ارسال ارزان', 'کتاب‌های دانشگاهی و لوازم‌التحریر فانتزی'),
('niche-auto', 'لوازم فنی، نظم‌دهنده و نگهداری خودرو', 'auto-accessories', 'digital', 'تجهیزات سفر', 'جامپ‌استارتر، پمپ باد پرتابل و هولدر')
ON CONFLICT (id) DO NOTHING;

-- 4. Suppliers
INSERT INTO suppliers (id, name, logo_url, rating, total_products, delivery_score, warehouse_city, description, badge) VALUES
('sup-khanoumi', 'خانومی', 'https://web-cdn.snapp.ir/fintech/merchant-growth/img/khanoumi-large.png', 4.90, 420, 99, 'تهران', 'مرجع تخصصی آرایشی، بهداشتی و مراقبت پوست با ضمانت اصالت کالا', '٪۳ بازگشت وجه'),
('sup-technolife', 'تکنولایف', 'https://web-cdn.snapp.ir/fintech/merchant-growth/img/technolife-large.png', 4.90, 850, 99, 'تهران', 'تأمین‌کننده معتبر گوشی، لپ‌تاپ، لوازم جانبی و گجت‌های هوشمند', 'ارسال فوری'),
('sup-filimo', 'فیلیمو', 'https://web-cdn.snapp.ir/fintech/merchant-growth/img/filimo-large.png', 4.80, 15, 98, 'تهران', 'بزرگترین پلتفرم تماشای آنلاین فیلم و سریال در ایران', 'تخفیف اشتراک')
ON CONFLICT (id) DO NOTHING;


-- 5. Stores
INSERT INTO stores (id, name, slug, owner_type, owner_name, profit_margin_percent, active_plan_id, signed_contract_version, total_sales) VALUES
('store-central', 'فروشگاه رسمی مرکزی سلرشو', 'central', 'central', 'مدیریت مرکزی سلرشو', 0.00, 'vip', 2, 184500000),
('store-bibi', 'فروشگاه بی‌بی استایل (Bibi Style)', 'bibi', 'dropshipper', 'بهاره رضوانی', 12.00, 'trial', 2, 44100000),
('store-sepehr', 'موبایل و دیجیتال سپهر', 'sepehr-digital', 'dropshipper', 'سپهر زمانی', 15.00, 'standard', 2, 52000000),
('store-parsian', 'گالری پوشاک و چرم پارسیان', 'parsian-style', 'dropshipper', 'نیما پارسا', 14.00, 'pro', 2, 38000000)
ON CONFLICT (id) DO NOTHING;

-- Set referral relationships
UPDATE stores SET referrer_seller_id = 'store-bibi' WHERE id = 'store-sepehr';
UPDATE stores SET referrer_seller_id = 'store-sepehr' WHERE id = 'store-parsian';

-- 6. Reference Products
INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost, platform_margin, is_original, stock, image_url, description) VALUES
('prod-1', 'ساعت هوشمند HK11 Ultra 3 همراه با بند سیلیکونی', 'hk11-ultra-3', 'digital', 'sup-technolife', 5200000, 4100000, 100000, TRUE, 45, 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/hk11-watch.png', 'ساعت هوشمند با کیفیت صفحه نمایش AMOLED و قابلیت مکالمه بلوتوثی'),
('prod-2', 'پاوربانک شیائومی Redmi PB200LZM ظرفیت ۲۰۰۰۰ میلی‌آمپر', 'redmi-powerbank-20000', 'digital', 'sup-technolife', 2450000, 1950000, 100000, TRUE, 80, 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/redmi-powerbank.png', 'پاوربانک با دو درگاه خروجی و پشتیبانی از شارژ سریع ۱۸ واتی'),
('prod-3', 'هدفون بی‌سیم انکر Soundcore Life P2i با نویزکنسلینگ', 'anker-soundcore-p2i', 'digital', 'sup-technolife', 4500000, 3885000, 100000, TRUE, 30, 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/anker-p2i.png', 'هدفون بی‌سیم انکر با بیس قدرتمند و شارژدهی تا ۲۸ ساعت'),
('prod-4', 'شارژر دیواری انکر Anker 313 مدل Ace 45W فست شارژ', 'anker-313-charger-45w', 'digital', 'sup-technolife', 4650000, 3985000, 100000, TRUE, 55, 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/anker-charger.png', 'شارژر دیواری ۴۵ واتی سازگار با انواع گوشی‌ها و تبلت‌ها'),
('prod-5', 'سرم روشن‌کننده ویتامین C ویتالیر مدل Time Vit', 'vitalayer-vitamin-c-serum', 'beauty', 'sup-khanoumi', 1480000, 1150000, 100000, TRUE, 120, 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/vitalayer-serum.png', 'سرم جوان‌ساز و آنتی‌اکسیدان قوی با جذب سریع'),
('prod-6', 'کرم ضدآفتاب رنگی لافارر مدل SPF 40 مناسب پوست چرب', 'la-farrerr-sunscreen-spf40', 'beauty', 'sup-khanoumi', 1150000, 890000, 100000, TRUE, 90, 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/lafarrerr-sunscreen.png', 'ضدآفتاب با پوشش مات و محافظت کامل در برابر UVA و UVB')
ON CONFLICT (id) DO NOTHING;

-- Map Products to Niches
INSERT INTO product_niches (product_id, niche_id) VALUES
('prod-1', 'niche-smartwatch'),
('prod-2', 'niche-powerbank'),
('prod-3', 'niche-headphone'),
('prod-4', 'niche-powerbank'),
('prod-5', 'niche-skincare'),
('prod-6', 'niche-skincare')
ON CONFLICT (product_id, niche_id) DO NOTHING;
