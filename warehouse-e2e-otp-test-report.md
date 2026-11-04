# گزارش تست انبار با OTP (نسخه بازنویسی‌شده)

- موبایل تست (ماسک‌شده): `0900****007`
- محموله ورودی: `INB-TEST-PENDING-01` — سفارش خروجی: `ord-ready-pick-001`
- کد رهگیری صادر شده: `TRK-POST-991100`
- نتیجه: 13 موفق، 2 ناموفق، 3 ردشده (فیكسچر موجود نبود) از 18 مرحله
- نکته: این فایل جایگزین گزارش قدیمی نیست؛ گزارش قدیمی دست نخورده باقی مانده است.

| ردیف | عنوان مرحله | متد | مسیر API | وضعیت | نتیجه | جزئیات |
|:---:|:---|:---:|:---|:---:|:---:|:---|
| 01 | ورود پرسنل انبار با OTP موبایل | `POST` | `/api/v1/auth/otp/verify` | `200` | PASS | ورود موفق برای 0900****007، نقش warehouse_staff |
| 02 | تأیید هویت و دسترسی‌های پرسنل انبار (RBAC) | `GET` | `/api/v1/auth/me` | `200` | PASS | نقش: warehouse_staff |
| 03 | استعلام و مشاهده قفسه‌ها و لوکیشن‌های انبار (Bins) | `GET` | `/api/v1/warehouse/bins` | `200` | PASS | قفسه‌ها دریافت شد |
| 04 | مشاهده صف محموله‌های ورودی در انتظار تخلیه | `GET` | `/api/v1/warehouse/inbounds/pending` | `200` | PASS | تعداد در انتظار: 1، محموله تست موجود است |
| 05 | شروع فرآیند تخلیه و دریافت محموله ورودی | `POST` | `/api/v1/warehouse/inbounds/INB-TEST-PENDING-01/start-receiving` | `200` | PASS | HTTP 200 |
| 06 | اسکن بارکد اقلام و ثبت کارتن‌های دریافتی | `POST` | `/api/v1/warehouse/inbounds/INB-TEST-PENDING-01/scan-item` | `200` | PASS | HTTP 200 |
| 07 | تکمیل دریافت و ثبت محموله در وضعیت Received | `POST` | `/api/v1/warehouse/inbounds/INB-TEST-PENDING-01/complete` | `200` | PASS | HTTP 200 |
| 08 | مشاهده صف بازرسی کنترل کیفی اقلام (QC Queue) | `GET` | `/api/v1/warehouse/qc/queue` | `200` | PASS | HTTP 200 |
| 09 | تأیید کنترل کیفی کالا و ارجاع به چیدمان (QC Pass) | `POST` | `/api/v1/warehouse/qc/prod-1/pass` | `200` | PASS | HTTP 200 |
| 10 | ثبت عیوب ظاهری و عدم انطباق کیفی اقلام (QC Fail) | `POST` | `/api/v1/warehouse/qc/prod-1/fail` | `200` | PASS | HTTP 200 |
| 11 | صدور صورت‌جلسه مغایرت کیفی و مقداری با تأمین‌کننده | `POST` | `/api/v1/warehouse/qc/discrepancy/create` | `201` | PASS | HTTP 201 |
| 12 | مشاهده صف وظایف چیدمان در قفسه (Putaway Queue) | `GET` | `/api/v1/warehouse/putaway/queue` | `200` | PASS | HTTP 200 |
| 13 | تخصیص لوکیشن انبار (قفسه A-12-05) به کالاهای ورودی | `POST` | `/api/v1/warehouse/putaway/prod-1/assign` | `200` | PASS | HTTP 200  |
| 14 | تأیید قرارگیری فیزیکی کالا در قفسه و شارژ موجودی انبار | `POST` | `/api/v1/warehouse/putaway/PUT-TSK-d0a60dc1-60d1-4ff9-9fb3-7488e6ecfb5a/confirm` | `500` | FAIL | HTTP 500 |
| 15 | استعلام سفارشات پرداخت‌شده و آماده جمع‌آوری (Ready to Pick) | `GET` | `/api/v1/warehouse/orders/ready-to-pick` | `500` | FAIL | HTTP 500 |
| 16 | آغاز فرآیند جمع‌آوری سفارش از قفسه‌ها (Start Picking) | `POST` | `/api/v1/warehouse/orders/ord-ready-pick-001/start-picking` | `-` | SKIP | سفارش تست در صف آماده‌جمع‌آوری نیست |
| 17 | اسکن بارکد اقلام برداشته شده از قفسه و تطبیق سفارش | `POST` | `/api/v1/warehouse/orders/ord-ready-pick-001/pick-item` | `-` | SKIP | سفارش تست در صف آماده‌جمع‌آوری نیست |
| 18 | تکمیل جمع‌آوری، صدور بارنامه، بسته‌بندی و تحویل به حامل پستی | `POST` | `/api/v1/warehouse/orders/ord-ready-pick-001/ship` | `-` | SKIP | سفارش تست در صف آماده‌جمع‌آوری نیست |
