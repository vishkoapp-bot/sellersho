-- User decision (Oct 25 2026 — «برنگردد، صریح اعلام شود»):
-- shipping is NON-REFUNDABLE on customer returns, and the returns
-- policy text must state it explicitly. The 'terms' static page is
-- the policy text customers read (قوانین و ضمانت / مهلت تست و عودت
-- کالا); the refund flow itself never included shipping (unit price
-- + per-unit add-ons only) — this migration only makes the policy
-- visible, it changes no refund behavior.
--
-- Append-only and idempotent: the sentence is added only when the
-- page does not already contain it, so re-runs never duplicate it
-- and any admin edits to the page are preserved. Unnumbered
-- paragraph on purpose: the admin may have renumbered the list
-- since the seed, and a wrong «۳.» would be worse than none.
UPDATE static_pages
SET content = content || E'\n\nهزینه ارسال غیرقابل‌استرداد است: هزینه ارسال پرداخت‌شده در سفارش، در صورت مرجوعی کالا بازگردانده نمی‌شود؛ مبلغ بازپرداخت فقط شامل بهای کالا (و هزینه‌های افزوده‌ی همان قلم) است.'
WHERE key = 'terms'
  AND content NOT LIKE '%غیرقابل‌استرداد%';
