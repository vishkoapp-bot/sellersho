# گزارش جامع تست یکپارچگی چرخه حیات انبارداری (Warehouse E2E Integration Test Report)

این آزمون رفتار سیستم را در چرخه کامل ۱۸ مرحله‌ای نقش **پرسنل انبارداری و لجستیک (Warehouse Staff)** روی سرور زنده پلتفرم (`http://localhost:8080`) ارزیابی کرده است.

---

## ۱. مشخصات عمومی آزمون
- **کاربر:** `warehouse_ali` (نقش: `warehouse_staff`)
- **شناسه محموله ورودی:** `INB-TEST-PENDING-01`
- **شناسه سفارش خروجی:** `ord-ready-pick-001`
- **کد رهگیری صادر شده:** `TRK-POST-991100`
- **تعداد کل مراحل:** 18
- **تعداد مراحل موفق:** 0
- **درصد موفقیت (Pass Rate):** **0.0%**

---

## ۲. جدول جزئیات نتایج مراحل آزمون (18 گام چرخه حیات)

| ردیف | عنوان مرحله | متد | مسیر API | وضعیت | زمان پاسخ | نتیجه | جزئیات عملکرد |
|:---:|:---|:---:|:---|:---:|:---:|:---:|:---|
| 01 | ورود پرسنل انبار با نام کاربری و رمز عبور | `POST` | `/api/v1/auth/password/login` | `410` | 5ms | ❌ ناموفق | وضعیت HTTP 410 |
| 02 | تأیید هویت و دسترسی‌های پرسنل انبار (RBAC) | `GET` | `/api/v1/auth/me` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 03 | استعلام و مشاهده قفسه‌ها و لوکیشن‌های انبار (Bins) | `GET` | `/api/v1/warehouse/bins` | `401` | 2ms | ❌ ناموفق | وضعیت HTTP 401 |
| 04 | مشاهده صف محموله‌های ورودی در انتظار تخلیه | `GET` | `/api/v1/warehouse/inbounds/pending` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 05 | شروع فرآیند تخلیه و دریافت محموله ورودی | `POST` | `/api/v1/warehouse/inbounds/INB-TEST-PENDING-01/start-receiving` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 06 | اسکن بارکد اقلام و ثبت کارتن‌های دریافتی | `POST` | `/api/v1/warehouse/inbounds/INB-TEST-PENDING-01/scan-item` | `401` | 4ms | ❌ ناموفق | وضعیت HTTP 401 |
| 07 | تکمیل دریافت و ثبت محموله در وضعیت Received | `POST` | `/api/v1/warehouse/inbounds/INB-TEST-PENDING-01/complete` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 08 | مشاهده صف بازرسی کنترل کیفی اقلام (QC Queue) | `GET` | `/api/v1/warehouse/qc/queue` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 09 | تأیید کنترل کیفی کالا و ارجاع به چیدمان (QC Pass) | `POST` | `/api/v1/warehouse/qc/prod-1/pass` | `401` | 4ms | ❌ ناموفق | وضعیت HTTP 401 |
| 10 | ثبت عیوب ظاهری و عدم انطباق کیفی اقلام (QC Fail) | `POST` | `/api/v1/warehouse/qc/prod-1/fail` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 11 | صدور صورت‌جلسه مغایرت کیفی و مقداری با تأمین‌کننده | `POST` | `/api/v1/warehouse/qc/discrepancy/create` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 12 | مشاهده صف وظایف چیدمان در قفسه (Putaway Queue) | `GET` | `/api/v1/warehouse/putaway/queue` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 13 | تخصیص لوکیشن انبار (قفسه A-12-05) به کالاهای ورودی | `POST` | `/api/v1/warehouse/putaway/prod-1/assign` | `401` | 2ms | ❌ ناموفق | وضعیت HTTP 401 |
| 14 | تأیید قرارگیری فیزیکی کالا در قفسه و شارژ موجودی انبار | `POST` | `/api/v1/warehouse/putaway/prod-1/confirm` | `401` | 4ms | ❌ ناموفق | وضعیت HTTP 401 |
| 15 | استعلام سفارشات پرداخت‌شده و آماده جمع‌آوری (Ready to Pick) | `GET` | `/api/v1/warehouse/orders/ready-to-pick` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 16 | آغاز فرآیند جمع‌آوری سفارش از قفسه‌ها (Start Picking) | `POST` | `/api/v1/warehouse/orders/ord-ready-pick-001/start-picking` | `401` | 3ms | ❌ ناموفق | وضعیت HTTP 401 |
| 17 | اسکن بارکد اقلام برداشته شده از قفسه و تطبیق سفارش | `POST` | `/api/v1/warehouse/orders/ord-ready-pick-001/pick-item` | `401` | 2ms | ❌ ناموفق | وضعیت HTTP 401 |
| 18 | تکمیل جمع‌آوری، صدور بارنامه، بسته‌بندی و تحویل به حامل پستی | `POST` | `/api/v1/warehouse/orders/ord-ready-pick-001/ship` | `401` | 4ms | ❌ ناموفق | وضعیت HTTP 401 |

---

## ۳. تحلیل معماری جریان کار انبار (Workflow Breakdown)
1. **جریان ورود کالا به انبار (Inbound & ASN):** استعلام محموله‌های دارای بارنامه در وضعیت Pending، شروع تخلیه محموله، اسکن اتمیک بارکد اقلام و تغییر وضعیت به Received.
2. **کنترل کیفیت و رفع تعارضات (Quality Control & Discrepancy):** انتقال اقلام به صف بازرسی کیفی، ثبت اقلام سالم در جدول qc_results و انتقال به صف چیدمان، ثبت اقلام معیوب و صدور صورت‌جلسه مغایرت (qc_discrepancies).
3. **چیدمان در قفسه و شارژ موجودی (Putaway & Bin Assignment):** تخصیص کالاها به لوکیشن‌های انبار و تأیید قرارگیری فیزیکی.
4. **فرآیند خروجی و ارسال سفارشات (Outbound Picking, Packing & Shipping):** جمع‌آوری سفارشات پرداخت‌شده، اسکن بارکد اقلام، بسته‌بندی، صدور لیبل و بارنامه و تحویل به مأمور پست.
