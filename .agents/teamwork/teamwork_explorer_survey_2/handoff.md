# Handoff Report: Styling Architecture, Responsive Patterns & Visual Standards Survey

**Agent:** `teamwork_explorer_survey_2`  
**Working Directory:** `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\teamwork_explorer_survey_2`  
**Parent:** `d098698e-4ecb-42f8-a3af-a43e80f31d6d`  
**Date:** 2026-11-07  
**Artifact:** `survey_report.md`  

---

## ۱. Observation (مشاهدات مستقیم)

1. **پیکربندی استایل و وابستگی‌ها:**
   - در `package.json` (خطوط ۱۲-۳۴):
     ```json
     "dependencies": {
       "cors": "^2.8.5",
       "dotenv": "^16.4.7",
       "express": "^4.21.2",
       "lucide-react": "^0.475.0",
       "motion": "^12.4.7",
       "react": "^19.0.0",
       "react-dom": "^19.0.0",
       "vazirmatn": "^33.0.3"
     },
     "devDependencies": {
       "@tailwindcss/vite": "^4.0.7",
       "tailwindcss": "^4.0.7",
       "typescript": "^5.7.3",
       "vite": "^6.1.0"
     }
     ```
   - هیچ فایل `tailwind.config.js` وجود ندارد. در `vite.config.ts` (خطوط ۳ و ۹)، افزونه `@tailwindcss/vite` مستقیماً تزریق شده است:
     ```ts
     import tailwindcss from '@tailwindcss/vite';
     // ...
     plugins: [react(), tailwindcss()],
     ```

2. **فونت، توکن‌ها و لایه‌های CSS در `src/index.css`:**
   - خطوط ۱-۱۹:
     ```css
     @import "vazirmatn/Vazirmatn-font-face.css";
     @import "tailwindcss";

     @theme {
       --font-sans: 'Vazirmatn', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
       --font-vazir: 'Vazirmatn', sans-serif;
       --font-mono: 'Vazirmatn', ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
       --color-brand: #5d44f8;
       --color-brand-deep: #4a33d8;
       --color-brand-soft: #ede9fe;
       --color-brand-bg: #f5f3fe;
       --color-brand-border: #d9d2fb;
     }
     ```
   - خطوط ۴۹ و ۶۱:
     ```css
     svg.lucide{stroke-width:1.5px}
     .font-mono { font-variant-numeric: tabular-nums; }
     ```

3. **کانتینر ریسپانسیو در `src/App.tsx` (خطوط ۲۱۴-۲۱۵):**
   ```tsx
   <div className={(isHomepage ? 'homepage-shell ...' : isProductDetail ? '...' : `w-full bg-white min-h-screen shadow-2xl relative flex flex-col border-x border-slate-200/80 pb-20 ${isStorefront ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isStaticPage ? '...' : isWideChrome ? '...' : 'max-w-[504px]'}`)}>
   ```
   - کلاس `max-w-[504px]` تا قبل از `lg` (1024px) اعمال می‌شود، بنابراین در تبلت‌ها (مانند 768px)، ویترین فروشگاه در ستون ۵۰۴ پیکسلی محدود است.

4. **معماری فعلی قالب مد در `src/components/storefront/templates/FashionTemplate.tsx`:**
   - خطوط ۴۸-۵۵:
     ```tsx
     const CONTAINER = 'mx-auto w-full max-w-[1440px] px-4 lg:px-10';
     const FOCUS_RING =
       'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--tpl-accent)]';
     ```
   - خطوط ۳۸-۳۹:
     ```
     Palette is fixed: page #faf8f5, tile #f2f2f2, announcement #efeae3, card #ffffff, ink #1c1c1a, muted #737373, line #e5e0d8, band #3a4441.
     ```
   - خطوط ۸۳۴-۸۵۸ (`AastinProductCard`):
     کارت‌های محصول دارای گوشه تیز ۲ پیکسل، تصویر مربعی، بدون نمایش دکمه هاور خرید سریع، بدون هاور تعویض به عکس دوم (`product.images[1]`)، و تنها نمایش‌دهنده قیمت در کل صفحه هستند.

5. **فقدان واریانت هدر و ناهماهنگی فوتر برای مد:**
   - در `SellerStorefrontView.tsx` سطر ۲۲۳ و ۲۷۰:
     ```tsx
     variant={resolvedTemplateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
     ```
     برای `tpl-fashion-1` هدر اختصاصی وجود ندارد و هدر مدرن با پس‌زمینه `#FAF8F4` و آیکون دایره‌ای سبزآبی `#0D7377` نمایش می‌یابد.
   - در `SellerStorefrontFooter.tsx` سطر ۵۶:
     ```tsx
     <footer className="rounded-t-[32px] mt-2" style={{ backgroundColor: '#1C1917' }}>
     ```
     گوشه‌های گرد ۳۲ پیکسل و پس‌زمینه مشکی/قهوه‌ای متناقض با نوار خبرنامه تخت `#3a4441` فشن رندر می‌شود.

6. **مدل داده کالا برای هاور عکس دوم در `src/types/index.ts` (سطور ۵۳-۵۴):**
   ```ts
   image: string;
   images: string[];
   ```
   محصولات دارای آرایه چندعکسی `images` هستند که امکان نمایش تصویر دوم در حالت Hover را کاملاً فراهم می‌سازد.

---

## ۲. Logic Chain (زنجیره استدلال)

1. **از مشاهدات ۱ و ۲:**
   پروژه روی استک فوق‌العاده مدرن React 19 + Tailwind CSS v4 + Motion v12 توسعه یافته است. در نتیجه هیچ نیازی به نصب پکیج‌های حجیم اضافی نظیر Radix UI یا تغییر در کانفیگ بیلد نیست؛ تمامی قابلیت‌های بصری و استایل‌ها مستقیماً با کلاس‌های کاربردی تیل‌ویند و انیمیشن‌های `motion/react` قابل توسعه است.
2. **از مشاهده ۳:**
   کلاس `isStorefront ? 'max-w-[504px] lg:max-w-none ...'` در `src/App.tsx` نقطه شکست تجربه کاربری در تبلت (768px) است. چون تبلت‌ها زیر breakpoint تیل‌ویند `lg` (1024px) قرار دارند، کل قالب مد داخل ۵۰۴ پیکسل فشرده می‌شود. رهاسازی این قید در سطح `md:` یا باز کردن پهنای ویترین برای تبلت‌ها، فضای تنفس و گرید ۳ ستونه تبلت را احیا خواهد کرد.
3. **از مشاهدات ۴ و ۶:**
   کارت کالا در قالب مد (`AastinProductCard`) مینیمال و شیک است، اما دو تعامل استاندارد فروشگاه‌های جهانی فشن (مانند SSENSE / Aassttiinn / Zara) را کم دارد: (الف) تعویض به عکس دوم در هاور از طریق `product.images[1]`، و (ب) نوار شناور هاور روی دسکتاپ برای مشاهده سریع / انتخاب سایز بدون ترک صفحه. با توجه به وجود داده‌های `images` در مدل محصول، این امکان بدون هیچ تغییری در بک‌اند مهیا است.
4. **از مشاهده ۵:**
   هدر و فوتر فعلی ویترین از نظر هویت بصری با بدنه ادیتوریال قالب مد ناهمخوان هستند (استفاده از پس‌زمینه و آیکون سبزآبی در هدر، و گوشه‌های گرد ۳۲ پیکسلی در فوتر). افزودن `variant="fashion"` به `SellerStorefrontHeader` و هماهنگ‌سازی اتصال فوتر به نوار خبرنامه `#3a4441`، هارمونی لوکس ۱۰۰٪ را تکمیل می‌کند.

---

## ۳. Caveats (ملاحظات و حوزه‌های بررسی‌نشده)

1. **کدنویسی سورس کد در فاز بررسی:**
   به عنوان ایجنت کاوشگر (Explorer)، این مأموریت کاملاً تحلیلی و خواندنی (Read-Only) انجام شد و کدهای سورس تغییر داده نشدند.
2. **تصاویر دوم محصولات قدیمی:**
   برخی محصولات ممکن است فقط یک عکس در آرایه `images` داشته باشند. بنابراین در منطق هاور، شرط ایمنی `product.images && product.images.length > 1` ضروری است تا در صورت نبود عکس دوم، انیمیشن زوم ظریف روی همان عکس اول (`group-hover:scale-105`) اعمال شود.
3. **همپوشانی ناوبری پایینی در تبلت:**
   در صورت باز کردن عرض ویترین در تبلت (`md:max-w-none`)، باید اطمینان حاصل شود که `StorefrontBottomNav` روی تبلت چیدمان متناسب داشته باشد یا مانند دسکتاپ در مانیتورهای عریض از دید خارج شود.

---

## ۴. Conclusion (نتیجه‌گیری نهایی و مشخصات راهنما برای ورکرها)

معماری استایل‌دهی پروژه کاملاً آماده، منعطف و بدون اضافه بار پکیج‌های خارجی است. برای اجرای فاز بعدی بازطراحی قالب فشن (`tpl-fashion-1`):
1. **اصلاح شل تبلت:** در `src/App.tsx`، کانتینر ویترین با `md:max-w-none` برای پشتیبانی کامل از آیپد و تبلت‌های ۷۶۸ پیکسل باز شود.
2. **ارتقای کارت کالای فشن (`AastinProductCard`):** اضافه کردن افکت تعویض نرم به تصویر دوم (`product.images[1]`) در هاور، افزودن دکمه شناور دسکتاپ «مشاهده سریع» (`Quick View`)، و بج‌های متنی مینیمال (`#efeae3` و حاشیه‌های نازک).
3. **پیاده‌سازی هدر اختصاصی فشن (`variant="fashion"`):** در `SellerStorefrontHeader.tsx` با هدر شیشه‌ای بلور، لوگوی مونوگرام شیک و تایپوگرافی تمیز.
4. **تراز بصری فوتر:** حذف قوس ۳۲ پیکسل در انتهای نوار خبرنامه و همسان‌سازی رنگ‌ها و استروک‌های خطی.

---

## ۵. Verification Method (روش راستی‌آزمایی مستقل)

برای تایید صحت تمامی موارد گزارش‌شده، می‌توان مراحل زیر را اجرا کرد:

1. **بررسی پکیج‌ها:**
   - فایل `package.json` را بررسی کرده و حضور Tailwind v4 و Motion v12 را تایید کنید:
     `view_file` بر روی `C:\Users\LENOVO\projects\sellersho\package.json`
2. **بررسی استایل‌های بیس و توکن‌های CSS:**
   - فایل `src/index.css` را بررسی کرده و توکن‌های `@theme` و قوانین `svg.lucide` و `.font-mono` را ملاحظه نمایید:
     `view_file` بر روی `C:\Users\LENOVO\projects\sellersho\src\index.css`
3. **بررسی محدودیت کانتینر در `App.tsx`:**
   - سطر ۲۱۴ از `src/App.tsx` را بررسی نمایید تا شرط `isStorefront ? 'max-w-[504px] lg:max-w-none ...'` مشاهده شود.
4. **تست بیلد تایپ‌اسکریپت و ویت:**
   - اجرای فرمان بیلد پلتفرم بدون هیچ خطای استایل و تایپ:
     ```powershell
     npm run build
     ```
   - این دستور کامپایل `tsc && vite build` را اجرا کرده و سلامت پکیج‌ها را تضمین می‌کند.
