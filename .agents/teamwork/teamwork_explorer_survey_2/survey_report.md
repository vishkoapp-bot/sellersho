# Comprehensive Survey Report: Styling Architecture, Responsive Patterns & Visual Design Standards for Fashion Template Redesign

**Author:** `teamwork_explorer_survey_2`  
**Date:** 2026-11-07  
**Target:** Fashion Storefront Redesign (`tpl-fashion-1` / `FashionTemplate.tsx` & associated storefront chrome)  
**Codebase:** Sellersho / Drup Dropship Platform  

---

## ۱. خلاصه‌ مدیریتی (Executive Summary)

این گزارش به بررسی ساختار استایل‌دهی، فریم‌ورک‌ها و کتابخانه‌های UI، رفتار واکنش‌گرا (Responsive) در ابعاد موبایل، تبلت و دسکتاپ، و تدوین مشخصات دقیق بصری برای بازطراحی لوکس، ادیتوریال و مدرن قالب مد و فشن (`tpl-fashion-1`) در پلتفرم سلرزشو می‌پردازد.

### دستاوردهای کلیدی تحلیل:
1. **معماری مدرن Tailwind CSS v4:** پروژه از Tailwind CSS نسخه `4.0.7` مبتنی بر افزونه رسمی `@tailwindcss/vite` بدون فایل سنتی `tailwind.config.js` استفاده می‌کند. تمامی تنظیمات، توکن‌های `@theme` و لایه‌ها در `src/index.css` پیاده‌سازی شده‌اند.
2. **فونت بومی فارسی و ساختار ارقام:** فونت اصلی کل پلتفرم `Vazirmatn` (نسخه `33.0.3`) با لود محلی است. اعداد و مبالغ پلتفرم از استایل ویژه `.font-mono { font-variant-numeric: tabular-nums; }` هدایت‌شده از طریق وزیرمتن بهره می‌برند تا تراز ستونی ارقام در تمام جداول و کارت‌های قیمت تضمین شود.
3. **موتور انیمیشن و آیکون‌ها:** از پکیج سبک و مدرن `motion` نسخه `12.4.7` (نسخه جدید Framer Motion برای React 19 با ایمپورت از `motion/react`) و آیکون‌های خطی مینیمال `lucide-react` (نسخه `0.475.0` با قانون سراسری `stroke-width: 1.5px`) استفاده می‌شود. کامپوننت‌های رادیکس یا شادسن (Radix / Shadcn) به عنوان وابستگی مستقیم نصب نیستند و تمامی المان‌های UI کامپوننت‌های دست‌ساز، سریع و تایپ‌سیف ری‌اکت ۱۹ هستند.
4. **مسئله حیاتی کانتینر در تبلت (Breakpoint Bottleneck):** در `src/App.tsx` لایه خارجی ویترین فروشگاه در سایزهای زیر `lg:` (زیر ۱۰۲۴ پیکسل) با کلاس `max-w-[504px]` محدود شده است. این امر باعث می‌شود که در مانیتورهای تبلت (مانند iPad با عرض 768px)، قالب به جای باز شدن در پهنای کامل تبلت، به صورت ستونی باریک در وسط صفحه با حاشیه‌های خاکستری اطراف رندر شود.
5. **هویت بصری لوکس/استریت‌ویر قالب مد (Aastin Editorial):** قالب بر پایه پالت ارگانیک عاجی و کرم `#faf8f5`، سطوح سنگی `#f2f2f2`، رنگ جوهر پرکربن `#1c1c1a`، حاشیه‌های مویی `#e5e0d8` و گوشه‌های معماری و تیز (`rounded-none` یا `rounded-[2px]`) بنا شده است. برای ارتقای آن به سطح استانداردهای جهانی (نظیر SSENSE، Acne Studios و Aassttiinn)، ویژگی‌هایی نظیر تعویض تصویر کالا در هاور (Secondary Image Flip)، کشوی بررسی سریع، نشان‌های تایپوگرافیک مینیمال، و ادغام هدر و فوتر چسبان با پالت ادیتوریال تدوین گردید.

---

## ۲. تحلیل معماری استایل‌دهی و CSS (Styling Architecture Analysis)

### ۲.۱ پیکربندی موتور Tailwind CSS v4
در `package.json` و `vite.config.ts`:
- `@tailwindcss/vite: ^4.0.7`
- `tailwindcss: ^4.0.7`
- هیچ فایل `tailwind.config.js` یا `tailwind.config.ts` وجود ندارد. در نسخه ۴ تیل‌ویند، تنظیمات تماماً به داخل شیوه جدید CSS-first با کلیدواژه `@theme` منتقل شده است.
- فایل ورودی اصلی استایل در مسیر `src/index.css` قرار دارد:
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

### ۲.۲ سیستم تایپوگرافی فارسی و ارقام یکپارچه
- **فونت خانواده:** تمامی المان‌ها (`body`, `button`, `input`, `select`, `textarea`) تحت فونت `Vazirmatn` با چیدمان راست‌به‌چپ (`direction: rtl; text-align: right;`) هدایت می‌شوند.
- **حل معضل ارقام فارسی مونو (Tabular Numbers):** به دلیل اینکه در فونت‌های پیش‌فرض مونو اسپیس ارقام فارسی به درستی رندر نمی‌شوند، در `@theme` متغیر `--font-mono` به وزیرمتن هدایت شده و کلاس زیر اعمال گشته است:
```css
.font-mono { font-variant-numeric: tabular-nums; }
```
این امر مانع از لرزش عرضی قیمت‌ها و درصدهای تخفیف هنگام آپدیت یا لود می‌شود.

### ۲.۳ پالت رنگی قالب مد (Fashion/Editorial Token Set)
قالب مد بر اساس مدل وایت‌لیست (`tpl-fashion-1`) از رنگ‌های پلتفرم عمومی استفاده نمی‌کند، بلکه دارای یک پالت ثابت، باکلاس و بدون نویز بصری است:

| نقش توکن | متغیر / مقدار Hex | کلاس Tailwind پیاده‌شده | کاربرد در قالب مد |
| :--- | :--- | :--- | :--- |
| **Page Canvas** | `#faf8f5` | `bg-[#faf8f5]` | پس‌زمینه سراسری صفحه ویترین مد (رنگ عاجی بسیار گرم و طبیعی) |
| **Tile Surface** | `#f2f2f2` | `bg-[#f2f2f2]` | کانتینر خنثی تصویر محصولات، دسته‌بندی‌ها، اسلایدر و بنرها |
| **Announcement Bar** | `#efeae3` | `bg-[#efeae3]` | نوار اعلان باریک بالای صفحه چسبیده به هیرو |
| **Ink / Primary Text** | `#1c1c1a` | `text-[#1c1c1a]` | تیترها، عناوین محصولات، متون اصلی، بردرهای هاور |
| **Muted Meta** | `#737373` | `text-[#737373]` | نام برندها، توضیحات، پرسش‌ها و برچسب‌های ثانویه |
| **Hairline Border** | `#e5e0d8` | `border-[#e5e0d8]` | خطوط تفکیک بسیار باریک و تمیز، خطوط جداکننده FAQ و چیپ‌ها |
| **Dark Editorial Band** | `#3a4441` | `bg-[#3a4441]` | نوار عضویت در خبرنامه ادیتوریال (سبز/طوسی زغالی تیره) |
| **Card White** | `#ffffff` | `bg-white` | پس‌زمینه کارت‌های ترکیبی مجموعه‌ها و فرم‌های ورودی |
| **Seller Accent** | `var(--tpl-accent)` | `ring-[var(--tpl-accent)]` | رنگ تاکیدی فروشگاه (برای فوکوس، بوردر انتخابی و غیره) |

### ۲.۴ کلاس‌های کاربردی و بهینه‌سازی‌های پرفورمنس
در `src/index.css`:
- **Content Visibility (`content-visibility: auto`):**
  - `.cv-card`: با اندازه مجازی `contain-intrinsic-size: auto 360px` (در دسکتاپ 400px) برای روان ماندن اسکرول کاتالوگ‌های سنگین با صدها محصول.
  - `.cv-auto` و `.cv-list`.
- **Skeleton Shimmer Animation:**
  - انیمیشن ملایم لودینگ به جای پالس ساده:
  ```css
  @keyframes skeleton-shimmer { 0% { background-position: -400px 0; } to { background-position: 400px 0; } }
  .skeleton-shimmer { background: linear-gradient(90deg, #e2e8f0bf 25%, #f1f5f9f2, #e2e8f0bf 75%) 0 0 / 800px 100%; animation: 1.6s ease-in-out infinite skeleton-shimmer; }
  @media (prefers-reduced-motion: reduce) { .skeleton-shimmer { animation: none; } }
  ```
- **استروک آیکون‌ها:**
  - قاعده سراسری لایه بیس: `svg.lucide { stroke-width: 1.5px }` که تضمین می‌کند تمامی آیکون‌های Lucide با ضخامت خط ۱.۵ پیکسل و متناسب با سبک مینیمال رندر شوند.
- **اسکرول‌بارهای محو:**
  - `.no-scrollbar`: مخفی‌سازی کامل اسکرول‌بار در کاروسل‌ها و ریل‌های افقی همراه با پشتیبانی `scrollbar-width: none`.

---

## ۳. اکوسیستم کامپوننت‌های رابط کاربری و انیمیشن (UI Component & Icon Ecosystem)

### ۳.۱ بررسی پکیج‌های نصب‌شده (`package.json`)
- **React 19:** پلتفرم روی آخرین نسخه ری‌اکت ۱۹ کار می‌کند (`"react": "^19.0.0"`, `"react-dom": "^19.0.0"`).
- **Motion v12 (`"motion": "^12.4.7"`):**
  - موتور انیمیشن رسمی Framer Motion برای ری‌اکت ۱۹ که تحت نام پکیج جدید `motion` منتشر شده است.
  - نحوه ایمپورت استاندارد در پروژه:
    ```tsx
    import { motion, AnimatePresence } from 'motion/react';
    ```
  - در حال حاضر در `CartDrawer.tsx`، `ProductDetailModal.tsx`، `NotificationDrawer.tsx`، `AuthModal.tsx` و `ToastContext.tsx` با فنرهای روان (`type: 'spring', damping: 28, stiffness: 240`) به کار رفته است.
- **Lucide React (`"lucide-react": "^0.475.0"`):**
  - آیکون‌های وکتوری مدرن و منسجم مورد استفاده در هدر، فوتر، دکمه‌های ناوبری، سبد خرید، جستجو و نشان‌ها.
- **عدم وجود Radix UI / Shadcn UI در Dependencies:**
  - هیچ‌کدام از پکیج‌های حجیم شخص ثالث یا کتابخانه‌های Headless سنگین نظیر Radix نصب نیستند. این یک مزیت بزرگ برای سرعت لود و عملکرد است، چرا که کامپوننت‌های پروژه سبک، تمیز و کاملاً سفارشی برای معماری PWA و وب ساخته شده‌اند.

### ۳.۲ رفتار مودال‌ها، کشوها و فریم‌های تعاملی
1. **کشوی سبد خرید (`CartDrawer.tsx`):**
   - از راست به چپ باز نمی‌شود، بلکه به صورت یک مودال/شیت مرکزی در موبایل (`max-w-[480px]`) با پس‌زمینه محو تیره (`bg-slate-900/60 backdrop-blur-xs`) و ورودی مرحله‌ای کار می‌کند.
2. **کشوی اعلان‌ها (`NotificationDrawer.tsx`):**
   - به صورت باتم‌شیت با انیمیشن اسپرینگ بالا می‌آید (`y: '100%'` به `y: 0`).
3. **مودال جزئیات محصول و خرید سریع (`ProductDetailModal.tsx` و `BuyNowAddressModal.tsx`):**
   - دارای استایل اختصاصی برای مد است (`theme="fashion"` یا `fashion={true}`). در حالت فشن، فرم‌ها و دکمه‌ها گوشه‌های تیز (`rounded-none`)، اینپوت‌های تمیز روی پس‌زمینه عاجی (`#faf8f5`) و بردرهای سنگی نازک به خود می‌گیرند.

---

## ۴. بررسی نیازمندی‌ها و الگوهای ریسپانسیو (Responsive Design Breakdown)

تحلیل رفتار صفحه در سه رده ابعاد حیاتی:

### ۴.۱ موبایل (عرض ۳۷۵ پیکسل و ۳۹۰ پیکسل)
- **کانتینر و گاترها:**
  - مقدار `px-4` (16 پیکسل از هر طرف).
  - در عرض ۳۷۵ پیکسل: فضای مفید محتوا ۳۴۳ پیکسل است.
  - در عرض ۳۹۰ پیکسل: فضای مفید محتوا ۳۵۸ پیکسل است.
- **هیرو اسلایدر (`AastinHero`):**
  - ارتفاع در موبایل `h-[80svh]` (استفاده هوشمندانه از واحدهای جدید `svh` مرورگرها جهت پیشگیری از پرش به دلیل نوار آدرس موبایل).
  - نقاط پیجینیشن اسلایدر دارای مساحت لمسی ۴۴×۴۴ پیکسل (`h-11 w-11`) هستند که با استانداردهای Human Interface Guidelines اپل کاملاً منطبق است.
- **ورودی دسته‌ها (`AastinMacroCategories`):**
  - در موبایل اگر ۳ دسته وجود داشته باشد، به صورت دو کاشی مربعی در بالا و یک کاشی عریض افقی (`aspect-[16/10] col-span-2`) در پایین چیده می‌شود که تعادل بصری بسیار جذابی روی صفحه کوچک گوشی ایجاد می‌کند.
- **گرید کاتالوگ کالاها (`AastinProductCard`):**
  - دو ستون متقارن با فاصله باریک ۱۲ پیکسل (`grid-cols-2 gap-x-3 gap-y-8`).
  - هر کارت کالا به صورت مربع (`aspect-square`) همراه با تایپوگرافی دوخطی کلمپ‌شده و قیمت با خط درشت مشکی در زیر آن قرار می‌گیرد.
- **ناوبری چسبان پایینی (`StorefrontBottomNav`):**
  - روی موبایل فعال است (`lg:hidden`)، ارتفاع استاندارد، دارای ۵۰۴ پیکسل حداکثر عرض در مرکز، و مجهز به `paddingBottom: 'env(safe-area-inset-bottom)'` برای آیفون‌های دارای Home Bar.

### ۴.۲ تبلت (عرض ۷۶۸ پیکسل - آیپد / دیوایس‌های متوسط) — *یافته بحرانی*
- **مشاهده معماری در `src/App.tsx` (سطر ۲۱۴):**
  ```tsx
  isStorefront ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : ...
  ```
- **تحلیل و پیامد:**
  - کلاس ریسپانسیو `lg:` در تیل‌ویند در پهنای `1024px` به بالا فعال می‌شود.
  - در نتیجه در عرض `768px` (تبلت‌های استاندارد عمودی نظیر iPad Mini و iPad Pro 11)، شرط `max-w-[504px]` حاکم است.
  - این امر باعث می‌شود کاربر تبلت یک اپ موبایل با عرض ۵۰۴ پیکسل را در وسط مانیتور تبلت ببیند و در دو طرف آن ۱۳۲ پیکسل فضای خالی به رنگ خاکستری وجود داشته باشد.
- **راهکار پیشنهادی برای ارتقای رده تبلت:**
  - برای صفحات ویترین (`isStorefront`)، شکست کانتینر باید از سایز تبلت یعنی `md:` (یا با افزودن `md:max-w-none`) آزاد شود، یا گرید به صورت ارگانیک در ابعاد بالای ۶۴۰ یا ۷۶۸ پیکسل به ۳ ستون تبدیل گردد.
  - ناوبری پایینی (`StorefrontBottomNav`) نیز باید در تبلت هماهنگ با طراحی هدر تصمیم‌گیری شود.

### ۴.۳ دسکتاپ و مانیتورهای عریض (۱۲۸۰ پیکسل و بالاتر)
- **کانتینر ادیتوریال:**
  - `mx-auto w-full max-w-[1440px] px-4 lg:px-10`
  - عرض صفحه تا ۱۴۴۰ پیکسل با پدینگ ۴۰ پیکسل در هر طرف به شکلی باشکوه باز می‌شود.
- **هیرو اسلایدر دسکتاپ:**
  - ارتفاع `h-[92svh]`، تصویر بسیار پرکشش و سینمایی با فوکوس `object-[center_30%]`.
  - فونت عنوان هیرو: `lg:text-6xl` (سایز ۶۰ پیکسل ضخیم و با وقار).
- **گرید کاتالوگ کالاها:**
  - به ۴ ستون شیک و باز تبدیل می‌شود (`lg:grid-cols-4 gap-x-4 gap-y-8`).
- **اسپات‌لایت ادیتوریال (بنتو گرید محصولات منتخب):**
  - کادر اول به صورت ۲ سطر و ۲ ستون با ارتفاع کامل (`lg:row-span-2 lg:aspect-auto lg:h-full`) خودنمایی کرده و در کنار آن ۴ محصول مربعی قرار می‌گیرند.
- **ریتم عمودی و فاصله‌گذاری سکشن‌ها:**
  - فواصل سکشن‌ها در دسکتاپ `gap-24` (معادل ۹۶ پیکسل) است که حس یک مجله مد بین‌المللی لوکس را القا می‌کند.

---

## ۵. مشخصات و استانداردهای طراحی بصری مد لوکس / استریت‌ویر (Luxury/Streetwear Visual Design Specs)

برای اینکه قالب مد فراتر از یک قالب فروشگاهی عادی باشد و هویت یک برند اصیل، مینیمال و معتبر را متجلی سازد، اصول و کامپوننت‌های زیر باید استانداردسازی شوند:

### ۵.۱ معماری هدر چسبان لوکس و اختصاصی (Editorial Sticky Header)
**وضعیت فعلی:**
- در حال حاضر `SellerStorefrontView.tsx` در صورت انتخاب قالب مد، از همان هدر عمومی مدرن (`SellerStorefrontHeader` با واریانت `modern`) با پس‌زمینه کرم `#FAF8F4`، دایره لوگوی سبزآبی تیره `#0D7377` و چهار دکمه استفاده می‌کند.
**طراحی هدف برای مد (Fashion Header Variant):**
1. **پس‌زمینه ترنسپرنت شیشه‌ای:**
   - `bg-[#faf8f5]/90 backdrop-blur-md border-b border-[#e5e0d8]/80`
2. **تایپوگرافی لوگو و نام برند:**
   - نام فروشگاه با فونت سیاه ۹۰۰ یا بلک (`font-black text-xl lg:text-2xl tracking-tight text-[#1c1c1a]`).
   - به جای دایره‌های بزرگ رنگی پلتفرمی، نمایش مونوگرام مینیمال با حاشیه ظریف یا لوگوی فروشنده.
3. **منوی متنی ظریف در دسکتاپ:**
   - ردیف لینک‌های ناوبری سریع در دسکتاپ: «کالکشن جدید»، «دسته‌بندی‌ها»، «برندها»، «درباره ما» با فونت ۱۲ پیکسل پررنگ و هاور آندرلاین ادیتوریال.
4. **اکشن‌های سمت چپ:**
   - سرچ باکس ظریف با آیکون Search، بج سبد خرید مینی‌مال با دایره مشکی و عدد سفید (به جای دایره‌های قرمز/آبی پررنگ تجاری).

### ۵.۲ کارت محصول هوشمند ادیتوریال با تعویض تصویر در هاور (Product Card Flip Hover Interaction)
یکی از امضاهای اصلی سایت‌های پوشاک و لوکس جهانی (مانند Zara، Aassttiinn، Farfetch، SSENSE)، نمایش عکس دوم لباس (مثلاً نمای پشت یا بر تن مدل) در هنگام قرارگیری موس است:
- **داده‌های موجود:** آبجکت `Product` در تایپ‌اسکریپت پروژه دارای هر دو فیلد `image: string` و `images: string[]` است!
- **مکانیزم هاور پیشنهادی:**
```tsx
<div className="relative aspect-square overflow-hidden rounded-[2px] bg-[#f2f2f2] group">
  {/* Primary Image */}
  <img
    src={product.image}
    alt={product.title}
    className={`absolute inset-0 h-full w-full object-cover transition-opacity duration-500 ${
      product.images && product.images.length > 1 ? 'group-hover:opacity-0' : 'group-hover:scale-105'
    }`}
  />
  {/* Secondary Image (Lifestyle / Model angle) */}
  {product.images && product.images.length > 1 && (
    <img
      src={product.images[1]}
      alt={`${product.title} - زاویه دیگر`}
      loading="lazy"
      className="absolute inset-0 h-full w-full object-cover opacity-0 transition-all duration-500 group-hover:opacity-100 group-hover:scale-105"
    />
  )}

  {/* Quick Action Floating Bar on Desktop Hover */}
  <div className="absolute inset-x-2 bottom-2 z-10 hidden lg:flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity duration-300">
    <button
      type="button"
      onClick={(e) => { e.stopPropagation(); onQuickView(product); }}
      className="w-full py-2 bg-white/95 backdrop-blur-xs text-[#1c1c1a] text-xs font-bold border border-[#e5e0d8] shadow-sm hover:bg-[#1c1c1a] hover:text-[#faf8f5] transition-colors"
    >
      مشاهده سریع و انتخاب سایز
    </button>
  </div>
</div>
```

### ۵.۳ بج‌ها و نشان‌های بصری لوکس (Refined Luxury Badges)
در فروشگاه‌های فشن سطح‌بالا، نباید از برچسب‌های نئونی قرمز و شلوغ مارکت‌پلیسی استفاده کرد. سیستم بج‌های فشن باید شامل موارد زیر باشد:
1. **نشان تخفیف / قیمت ویژه:**
   - کپسول ظریف مشکی با متن سفید یا کپسول کرم تیره:
   - `text-[10px] font-bold text-[#1c1c1a] bg-[#efeae3] px-2 py-0.5 rounded-[2px]`
2. **نشان «جدید» یا «کالکشن پاییز / بهار»:**
   - بج با حروف‌نگاری نازک بدون پس‌زمینه زننده: `border border-[#1c1c1a] text-[#1c1c1a] text-[9px] px-1.5 py-0.5 uppercase`
3. **نشان وضعیت موجودی:**
   - در صورت ناموجود بودن: اورلی مات ظریف طوسی روی عکس همراه با متن ادیتوریال «ناموجود در انبار».

### ۵.۴ کامپوننت لوک‌بوک و مجموعه‌های ژورنالی (Lookbook & Collections Rail)
- **لوک‌بوک بنر سینمایی:**
  - تصویر افقی بسیار عریض با نسبت‌های سینمایی ۲۱/۹ در دسکتاپ و ارتفاع `72svh`.
  - گرادیانت متن رو به پایین بسیار ملایم (`from-black/60 via-black/20 to-transparent`) با تایپوگرافی درشت سفید و لینک کالکشن با ترنزیشن هاور خط زیرین (`underline underline-offset-8`).
- **ریل مجموعه‌ها (Mosaic Collections):**
  - کاروسل با اسنپ افقی (`snap-x snap-start`) که هر کارت شامل ۴ تصویر از محصولات آن گروه در یک موزاییک ۲×۲ مینیمال با بردرهای سفید ۱ پیکسلی است.

### ۵.۵ کشوی انتخاب سریع سایز / رنگ (Fashion Quick View Drawer)
به جای باز کردن مودال عمومی پلتفرم، ویترین فشن می‌تواند یک کشوی مدرن از بغل یا پایین داشته باشد:
- **تنوع‌ها:** دکمه‌های کپسولی یا مربعی سایز (S, M, L, XL, 38, 40) با استایل Aastin Tokens (`border border-[#e5e0d8] bg-white text-[#1c1c1a]`, و در حالت انتخاب‌شده `bg-[#1c1c1a] text-white`).
- **دکمه اکشن خرید فوری:** دکمه با ارتفاع ۴۸ پیکسل، بدون گوشه گرد (`rounded-none`) با رنگ زغالی عمیق `#1c1c1a` و هاور مات.

### ۵.۶ یکپارچگی هدر و فوتر با بدنه قالب (Eliminating Visual Discontinuity)
**مشکل کنونی:**
- در پایین صفحه قالب مد، نوار خبرنامه دارای پس‌زمینه `#3a4441` است. بلافاصله پس از آن، فوتر عمومی پلتفرم با پس‌زمینه قهوه‌ای/مشکی `#1C1917`، گوشه‌های بالای گرد (`rounded-t-[32px]`) و آیکون سبزآبی `#0D7377` ظاهر می‌شود.
**اصلاح بصری مورد نیاز:**
- هماهنگ‌سازی استایل فوتر با تم فشن؛ حذف راندگی گرد ۳۲ پیکسل در همنشینی با خبرنامه یا انتقال تم رنگی فوتر به مشکی جوهری عمیق `#141412` با خط مویی تفکیک‌کننده و رنگ آیکون‌های هماهنگ با توکن‌های قالب مد.

---

## ۶. نقشه راه و توصیه‌های فنی برای تیم پیاده‌سازی (Implementation Specs for Workers)

| بخش / فایل | وضعیت فعلی | تغییرات پیشنهادی برای ورکرها |
| :--- | :--- | :--- |
| `src/components/storefront/templates/FashionTemplate.tsx` | گرید تک‌عکس بدون هاور ثانویه و بدون دکمه خرید سریع | افزودن هاور عکس دوم از `product.images[1]`، دکمه سریع هاور دسکتاپ، بج‌های مینیمال |
| `src/components/storefront/SellerStorefrontHeader.tsx` | استفاده از هدر یکسان مدرن با آیکون سبزآبی | افزودن `variant="fashion"` با استایل ادیتوریال بلور `#faf8f5/90`، آیکون‌های مشکی مینیمال و نشانگر تمیز سبد |
| `src/components/storefront/SellerStorefrontView.tsx` | پاس دادن هدر `variant="modern"` برای فشن | شرطی‌سازی: اگر `resolvedTemplateId === 'tpl-fashion-1'` باشد، `variant="fashion"` پاس داده شود |
| `src/App.tsx` | کانتینر `max-w-[504px]` تا قبل از ۱۰۲۴ پیکسل | اصلاح استایل شل ویترین: اعمال `md:max-w-none` یا `w-full` برای نمایش تمیز در تبلت‌ها |
| `src/components/ProductVariantSelector.tsx` | دارای `theme="fashion"` عالی با توکن‌های آستین | اتصال مستقیم به کشوی خرید سریع و مدال‌های ویترین فشن |
| `src/components/storefront/SellerStorefrontFooter.tsx` | فوتر عمومی با گوشه‌های گرد ۳۲ پیکسل و لهجه سبزآبی | سازگار کردن با خطوط صاف قالب فشن (`rounded-none` و رنگ‌های مشکی/کرم هماهنگ) |

---
**پایان گزارش ارزیابی معماری استایل و استانداردهای طراحی ویترین فشن.**
