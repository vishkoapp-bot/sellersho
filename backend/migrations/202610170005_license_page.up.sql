-- License page (H9): publish REAL, honest license/trust content for /license.
-- No license numbers or eNamad codes are invented here: anything not yet issued
-- is stated as "in progress" so the page never makes a false trust claim.
-- This mirrors the CMS-managed /about and /terms pages and remains editable
-- from the super-admin "محتوای سایت و صفحات" tab afterwards.

INSERT INTO static_pages (key, title, content, sort_order, is_active) VALUES
('license', 'مجوزها و نمادهای اعتماد',
 'سلرشو بازارگاه آنلاین کالا و خدمات است و در حال طی مراحل قانونی دریافت مجوزهای لازم برای فعالیت رسمی می‌باشد.

۱. نماد اعتماد الکترونیکی (اینماد): درخواست اخذ نماد در دست اقدام است. پس از صدور، کد نماد در همین صفحه نمایش داده خواهد شد.

۲. عضویت در اتحادیه کسب‌وکارهای مجازی: فرآیند عضویت در حال پیگیری است و پس از نهایی شدن، اطلاعات آن در همین صفحه منتشر می‌شود.

۳. ضمانت‌های جاری سلرشو: ضمانت اصالت فیزیکی کالاها، ۷ روز مهلت تست و بازگشت کالا، و پرداخت امن از طریق درگاه مرکزی — مطابق قوانین مندرج در صفحه «قوانین و ضمانت».', 50, TRUE)
ON CONFLICT (key) DO UPDATE SET
    title = EXCLUDED.title,
    content = EXCLUDED.content,
    sort_order = EXCLUDED.sort_order,
    is_active = TRUE,
    updated_at = NOW();
