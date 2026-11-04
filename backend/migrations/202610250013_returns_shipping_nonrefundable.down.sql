-- Reverts 202610250013: removes the appended non-refundable-shipping
-- paragraph from the live 'terms' page (only the exact sentence this
-- migration added; admin edits elsewhere in the page are untouched).
UPDATE static_pages
SET content = REPLACE(content, E'\n\nهزینه ارسال غیرقابل‌استرداد است: هزینه ارسال پرداخت‌شده در سفارش، در صورت مرجوعی کالا بازگردانده نمی‌شود؛ مبلغ بازپرداخت فقط شامل بهای کالا (و هزینه‌های افزوده‌ی همان قلم) است.', '')
WHERE key = 'terms';
