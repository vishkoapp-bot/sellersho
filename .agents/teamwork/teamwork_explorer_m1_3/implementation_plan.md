# Comprehensive Implementation Plan: Responsive Layout Optimization across Viewports

**Milestone:** 1 — Responsive Layout Optimization  
**Agent:** `teamwork_explorer_m1_3`  
**Target Codebase:** Sellersho Dropship Platform (`src/App.tsx`, `FashionTemplate.tsx`, `SellerStorefrontHeader.tsx`, `SellerStorefrontFooter.tsx`, Modals & Drawers)  
**Date:** 2026-11-07  

---

## ۱. خلاصه‌ اجرایی (Executive Summary)

این سند، راهنمای جامع و نقشه پیاده‌سازی دقیق فنی برای **بهینه‌سازی کامل لایه‌های واکنش‌گرا (Responsive Layout Optimization)** در پلتفرم سلرزشو و ویترین مد و فشن (`tpl-fashion-1`) است. 

در معماری کنونی، ویترین‌های فروشگاه در ابعاد تبلت (`md:` معادل ۷۶۸ پیکسل نظیر iPad) در داخل یک کانتینر موبایلی ۵۰۴ پیکسلی (`max-w-[504px]`) محصور می‌شوند. این امر باعث می‌شود که کاربران تبلت به جای تجربه یک کاتالوگ غنی، چندستونه و ژورنالی، با یک ستون باریک موبایلی در مرکز نمایشگر و نوارهای عریض خاکستری در طرفین مواجه شوند.

### اهداف بنیادین طرح:
1. **رفع گلوگاه تبلت در `src/App.tsx` (سطر ۲۱۴):** آزادسازی کانتینر ویترین فروشگاه در پهنای تبلت با اعمال `md:max-w-none md:shadow-none md:border-x-0 md:pb-0`، ضمن حفظ کادربندی شکیل گوشی (Phone Framing) در سایزهای موبایلی مربوطه.
2. **پاسخگویی بی‌نقص در مقیاس‌های سه‌گانه ابعاد:**
   - **موبایل (۳۷۵ و ۳۹۰ پیکسل):** پیشگیری کامل از سرریز افقی (Zero Horizontal Overflow)، پدینگ استاندارد ۱۶ پیکسل (`px-4`)، و مساحت لمسی استاندارد (Touch Targets $\ge 44\text{px}$).
   - **تبلت (۷۶۸ پیکسل):** ارتقای گریدها به چندستونه (۳ ستون در کف کاتالوگ، ۳ ستون در ورودی ماکرو دسته‌بندی‌ها، چیدمان دونیمه در داستان برند).
   - **دسکتاپ (۱۲۸۰ پیکسل و عریض‌تر):** مهار تمیز در کانتینر استاندارد ادیتوریال (`max-w-[1440px] px-10`)، تایپوگرافی چشم‌نواز و هماهنگی کامل مرزهای هدر، بنرها و فوتر.
3. **همگام‌سازی المان‌های ناوبری، کشوها و مودال‌ها:** سازگاری رفتار ناوبری پایینی (`StorefrontBottomNav`)، مودال سبد خرید (`CartDrawer`)، مرکز اعلان‌ها (`NotificationDrawer`)، مودال مشخصات کالا (`ProductDetailModal`) و اورلی جستجو.

---

## ۲. تحلیل ماتریس ابعاد و آسیب‌شناسی وضع موجود (Viewport Matrix & Gap Analysis)

| مشخصه / ابعاد | موبایل کوچک (375px - iPhone SE) | موبایل استاندارد (390px - iPhone 14/15) | تبلت عمودی (768px - iPad Mini / Air) | دسکتاپ عریض (1280px - 1440px+) |
| :--- | :--- | :--- | :--- | :--- |
| **عرض در دسترس** | ۳۴۳ پیکسل مفید (با پدینگ ۱۶) | ۳۵۸ پیکسل مفید (با پدینگ ۱۶) | ۷۲۰ پیکسل مفید (با پدینگ ۲۴) | تا ۱۴۴۰ پیکسل با پدینگ ۴۰ |
| **وضعیت فعلی `App.tsx`** | پهنای کامل در فریم ۵۰۴ پیکسل | پهنای کامل در فریم ۵۰۴ پیکسل | **محبوس در 504px با حاشیه خاکستری** | پهنای کامل با `lg:max-w-none` |
| **رفتار گرید کاتالوگ** | ۲ ستونه (عرض هر کارت ~۱۶۵ پیکسل) | ۲ ستونه (عرض هر کارت ~۱۷۱ پیکسل) | ۲ ستونه کش‌آمده در کادر ۵۰۴ پیکسلی | ۴ ستونه در قالب ادیتوریال |
| **ناوبری پایینی** | فعال در پایین صفحه | فعال در پایین صفحه | فعال با عرض ۵۰۴ پیکسل در وسط تبلت | مخفی (`lg:hidden`) |
| **مودال سبد خرید** | تمام صفحه / باتم‌شیت | تمام صفحه / باتم‌شیت | محبوس در عرض ۵۰۴ پیکسل | مودال وسیع ۸۸۰ پیکسل با ۲ ستون تسویه |
| **کشو اعلان‌ها** | عرض ۵۰۴ پیکسل | عرض ۵۰۴ پیکسل | عرض ۵۰۴ پیکسل در وسط صفحه | عرض ۵۰۴ پیکسل در وسط صفحه |
| **تارگت‌های لمسی** | برخی دکمه‌ها زیر ۴۴ پیکسل (۲۴-۳۲px) | برخی دکمه‌ها زیر ۴۴ پیکسل (۲۴-۳۲px) | لمس تبلت نیازمند اهداف بزرگ‌تر | کلیک با ماوس |

---

## ۳. تغییرات کدهای دقیق و مشخصات فنی (Concrete Code Specifications)

### ۳.۱ تغییر ریشه اپلیکیشن در `src/App.tsx` (گلوگاه اصلی تبلت)

#### مشاهده در کد کنونی (`src/App.tsx:214`):
```tsx
<div className={(isHomepage ? 'homepage-shell w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : isProductDetail ? 'w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : `w-full bg-white min-h-screen shadow-2xl relative flex flex-col border-x border-slate-200/80 pb-20 ${isStorefront ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isStaticPage ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isWideChrome ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : 'max-w-[504px]'}`)}>
```

#### تحلیل فنی:
- کلاس `max-w-[504px]` تا قبل از کلاس `lg:` (یعنی تا ۱۰۲۴ پیکسل) فعال می‌ماند.
- تبلت‌ها (`md:` معادل ۷۶۸ تا ۱۰۲۳ پیکسل) دارای عرض صفحه بیشتر از ۵۰۴ پیکسل هستند. در نتیجه بردرهای خاکستری `border-x` و سایه سنگین `shadow-2xl` بر روی تبلت رندر می‌شوند.
- همچنین فاصله پایینی `pb-20` باعث باقی ماندن یک فضای خالی ۸۰ پیکسلی زیر فوتر در تبلت می‌گردد.

#### قطعه کد اصلاحی پیشنهادی برای `src/App.tsx`:
```tsx
<<<<
      <div className={(isHomepage ? 'homepage-shell w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : isProductDetail ? 'w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : `w-full bg-white min-h-screen shadow-2xl relative flex flex-col border-x border-slate-200/80 pb-20 ${isStorefront ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isStaticPage ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isWideChrome ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : 'max-w-[504px]'}`)}>
====
      <div className={(isHomepage ? 'homepage-shell w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : isProductDetail ? 'w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : `w-full bg-white min-h-screen shadow-2xl relative flex flex-col border-x border-slate-200/80 pb-20 ${isStorefront ? 'max-w-[504px] md:max-w-none md:shadow-none md:border-x-0 md:pb-0 lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isStaticPage ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isWideChrome ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : 'max-w-[504px]'}`)}>
>>>>
```

#### اثر این تغییر:
- در گوشی‌ها (< 768px): کانتینر در ابعاد کوچک به صورت تمام‌عرض و در تبلت‌های کوچک یا حالت افقی در چارچوب امن ۵۰۴ پیکسلی سنپ‌پی محصور می‌ماند.
- در تبلت‌ها ($\ge 768px$): فریم ۵۰۴ پیکسل بی‌درنگ شکسته شده (`md:max-w-none`)، سایه و خطوط بردر حذف می‌شوند (`md:shadow-none md:border-x-0`)، و قالب فروشگاه فضای کل عرض تبلت را در اختیار می‌گیرد.

---

### ۳.۲ بهینه‌سازی کانتینر هدر، اورلی جستجو و فوتر

#### الف) هماهنگ‌سازی هدر در `SellerStorefrontHeader.tsx`
در فایل `src/components/storefront/SellerStorefrontHeader.tsx`، کانتینر داخلی نوار بالا باید درون ماکزیمم کانتینر ۱۴۴۰ پیکسل قرار گیرد تا با گرید بدنه صفحه در دسکتاپ و تبلت کاملاً تراز باشد:

```tsx
// برای واریانت فشن و مدرن:
<header className="sticky top-0 z-30 ...">
  <div className="mx-auto w-full max-w-[1440px] px-4 md:px-6 lg:px-10 flex items-center justify-between py-2.5">
    {/* محتوای هدر */}
  </div>
</header>
```

#### ب) اورلی جستجوی ویترین در `SellerStorefrontView.tsx` (سطور ۳۰۹ تا ۳۳۲)
در `SellerStorefrontView.tsx`، جعبه جستجو در حالت باز شده نباید در مانیتورهای عریض به صورت تمام‌عرض بدون مهار رها شود:

```tsx
<<<<
        <div className="bg-slate-50 border-b border-slate-200 p-3 flex items-center gap-2 animate-in fade-in duration-200">
          <div className="relative flex-1 flex items-center bg-white border border-slate-200 rounded-xl px-2.5 py-1.5 shadow-2xs">
            <Search className="w-4 h-4 text-slate-400 shrink-0 ml-1.5"  strokeWidth={1.5}/>
            <input
              type="text"
              autoFocus
              placeholder="جستجوی محصول در این فروشگاه..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
            />
          </div>
          <button
            onClick={() => {
              setIsSearchOpen(false);
              setSearchQuery('');
            }}
            className="p-1.5 rounded-xl hover:bg-slate-200 text-slate-500"
          >
            <X className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>
====
        <div className="bg-slate-50/95 backdrop-blur-md border-b border-slate-200 p-3 animate-in fade-in duration-200">
          <div className="mx-auto w-full max-w-[1440px] px-4 md:px-6 lg:px-10 flex items-center gap-2.5">
            <div className="relative flex-1 flex items-center bg-white border border-slate-200 rounded-xl px-3 py-2 shadow-2xs min-h-[44px]">
              <Search className="w-4 h-4 text-slate-400 shrink-0 ml-2" strokeWidth={1.5}/>
              <input
                type="text"
                autoFocus
                placeholder="جستجوی محصول در این فروشگاه..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="w-full text-xs md:text-sm text-slate-800 placeholder-slate-400 focus:outline-hidden"
              />
            </div>
            <button
              onClick={() => {
                setIsSearchOpen(false);
                setSearchQuery('');
              }}
              aria-label="بستن جستجو"
              className="w-11 h-11 shrink-0 rounded-xl hover:bg-slate-200 text-slate-500 flex items-center justify-center transition-colors"
            >
              <X className="w-5 h-5" strokeWidth={1.5}/>
            </button>
          </div>
        </div>
>>>>
```

#### ج) فوتر و ناوبری چسبان در `SellerStorefrontFooter.tsx`
در `src/components/storefront/SellerStorefrontFooter.tsx`:
1. کانتینر داخلی فوتر با حداکثر عرض ادیتوریال و پدینگ هماهنگ تراز می‌شود:
   ```tsx
   <div className="mx-auto w-full max-w-[1440px] px-6 md:px-8 lg:px-10 pt-8 pb-[84px] md:pb-8">
   ```
2. بهینه‌سازی `StorefrontBottomNav`:
   - در حال حاضر `StorefrontBottomNav` با `lg:hidden` پنهان می‌شود، اما در تبلت (`768px`) با استایل درون‌خطی `maxWidth: 504` در مرکز تبلت شناور می‌ماند.
   - برای تبلت‌ها و دسکتاپ، کنترل‌های ناوبری در هدر چسبان و فوتر متمرکز هستند. بهترین الگو برای تبلت:
     - پنهان‌سازی هوشمند در تبلت با تغییر به `md:hidden`، که کامپوننت را روی موبایل‌های ۳۷۵ و ۳۹۰ حفظ کرده و از اشغال فضای نمایشگر تبلت جلوگیری می‌کند.
     - تضمین ارتفاع لمسی استاندارد $\ge 44\text{px}$ برای تمام تب‌ها با کلاس `min-h-[44px]`.
     - هماهنگی رنگ پس‌زمینه و آیکون با تم فعال (عاجی و مشکی در قالب فشن).

---

### ۳.۳ بهینه‌سازی گریدها در `FashionTemplate.tsx` برای تبلت (768px)

در `src/components/storefront/templates/FashionTemplate.tsx`، کانتینر با ثابت زیر تعریف شده است:
```typescript
const CONTAINER = 'mx-auto w-full max-w-[1440px] px-4 md:px-6 lg:px-10';
```
با افزودن `md:px-6`، تبلت‌ها گاتر روان ۲۴ پیکسلی دریافت می‌کنند.

#### الف) گرید کف کاتالوگ (`products` / `allProducts`) در سطر ۱۱۴۰
در کد فعلی:
```typescript
const gridCls =
  count === 1 ? 'grid-cols-1' : count === 2 ? 'grid-cols-2' : count === 3 ? 'grid-cols-2 lg:grid-cols-3' : 'grid-cols-2 lg:grid-cols-4';
```
در عرض ۷۶۸ پیکسل، این گرید ۲ ستونه باقی می‌ماند و عرض هر کارت به بیش از ۳۶۰ پیکسل کش می‌آید که برای کارت‌های مربعی پوشاک نامتناسب است.
**کد اصلاحی:**
```typescript
const gridCls =
  count === 1
    ? 'grid-cols-1'
    : count === 2
      ? 'grid-cols-2'
      : count === 3
        ? 'grid-cols-2 md:grid-cols-3 lg:grid-cols-3'
        : 'grid-cols-2 md:grid-cols-3 lg:grid-cols-4';
```
با این اصلاح، در تبلت دقیقاً ۳ کارت متقارن و چشم‌نواز در هر سطر رندر می‌شوند.

#### ب) ورودی ماکرو دسته‌بندی‌ها (`AastinMacroCategories`) در سطر ۳۳۱
در کد فعلی ۳ کاشی در تبلت به صورت ۲ مربع در بالا و یک کاشی عریض در پایین چیده می‌شوند.
با ارتقا به:
```typescript
const gridCls =
  tiles.length <= 1
    ? 'grid-cols-1'
    : tiles.length === 2
      ? 'grid-cols-2'
      : 'grid-cols-2 md:grid-cols-3';

const tileAspect = (i: number): string => {
  if (tiles.length <= 1) return 'aspect-[16/10] lg:aspect-[21/9]';
  if (tiles.length === 3 && i === 2) return 'aspect-[16/10] md:aspect-square';
  return 'aspect-square';
};

const tileSpan = (i: number): string =>
  tiles.length === 3 && i === 2 ? 'col-span-2 md:col-span-1' : '';
```
در موبایل ترکیب ۲+۱ باقی می‌ماند، اما در تبلت بلافاصله به ۳ کاشی مربعی مجلل در یک ردیف تبدیل می‌شود.

#### ج) سکشن درباره فروشگاه (`AastinStory`) در سطر ۴۲۰
در کد فعلی:
```tsx
<div className="grid items-center gap-8 lg:grid-cols-2 lg:gap-16">
```
در تبلت ستون متن و عکس زیر هم قرار می‌گیرند. با تغییر به:
```tsx
<div className="grid items-center gap-8 md:grid-cols-2 md:gap-10 lg:gap-16">
```
در تبلت متن ادیتوریال در سمت راست و عکس ژورنالی در سمت چپ در کنار یکدیگر متوازن می‌شوند.

#### د) مقیاس تایپوگرافی عناوین بخش‌ها (`AastinSectionHeader`) در سطر ۷۳
برای روان ماندن ریتم قلم بین موبایل و دسکتاپ:
```tsx
<h2 className="text-right text-[26px] md:text-[32px] lg:text-[40px] font-extrabold leading-[1.35] tracking-normal text-[#1c1c1a]">
  {title}
</h2>
```

---

### ۳.۴ بهینه‌سازی واکنش‌گرای کشوها و مودال‌ها (Drawers & Modals)

#### الف) کشوی سبد خرید (`CartDrawer.tsx`)
در `src/components/CartDrawer.tsx` سطر ۳۲۵:
```tsx
<<<<
className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between lg:max-w-[880px] lg:h-auto lg:max-h-[90vh] lg:rounded-3xl lg:my-auto"
====
className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between md:max-w-[680px] md:h-auto md:max-h-[88vh] md:rounded-2xl md:my-auto lg:max-w-[880px] lg:rounded-3xl"
>>>>
```
و در سطر ۳۵۳ در مرحله تسویه:
```tsx
<<<<
<div className="lg:grid lg:grid-cols-[minmax(0,1fr)_320px] lg:gap-6">
====
<div className="md:grid md:grid-cols-[minmax(0,1fr)_280px] md:gap-4 lg:grid-cols-[minmax(0,1fr)_320px] lg:gap-6">
>>>>
```
**اثر:** کاربر تبلت هنگام باز کردن سبد خرید و ثبت نشانی، به جای یک ستون کشیده ۵۰۴ پیکسلی، فرم آدرس و خلاصه فاکتور را در دو ستون تمیز در کادری ۶۸۰ پیکسلی با گوشه‌های گرد مشاهده می‌کند.

#### ب) مرکز اعلان‌ها (`NotificationDrawer.tsx`)
در `src/components/common/NotificationDrawer.tsx`:
1. مساحت دکمه بستن (سطر ۱۳۳):
```tsx
<<<<
<button
  onClick={onClose}
  className="p-1 rounded-lg text-slate-400 hover:bg-slate-100"
>
  <X className="w-4 h-4" strokeWidth={1.5}/>
</button>
====
<button
  onClick={onClose}
  aria-label="بستن اعلان‌ها"
  className="w-11 h-11 rounded-xl text-slate-400 hover:bg-slate-100 flex items-center justify-center transition-colors"
>
  <X className="w-5 h-5" strokeWidth={1.5}/>
</button>
>>>>
```
2. کانتینر مودال (سطر ۱۰۷):
```tsx
<<<<
className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between"
====
className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between md:max-w-[560px] md:h-auto md:max-h-[85vh] md:rounded-2xl md:my-auto"
>>>>
```

#### ج) مودال مشخصات سریع کالا (`ProductDetailModal.tsx`)
در `src/components/ProductDetailModal.tsx`:
کانتینر مودال در سطر ۹۷:
```tsx
<<<<
className="bg-white rounded-2xl max-w-[480px] w-full overflow-hidden shadow-2xl border border-slate-100 flex flex-col max-h-[92vh]"
====
className="bg-white rounded-2xl max-w-[480px] md:max-w-[620px] lg:max-w-[700px] w-full overflow-hidden shadow-2xl border border-slate-100 flex flex-col max-h-[92vh]"
>>>>
```
دکمه بستن در سطر ۱۰۷:
```tsx
<<<<
<button
  onClick={() => setSelectedProduct(null)}
  className="p-1.5 rounded-full hover:bg-slate-100 text-slate-500 transition-colors"
>
  <X className="w-5 h-5" strokeWidth={1.5}/>
</button>
====
<button
  onClick={() => setSelectedProduct(null)}
  aria-label="بستن پنجره"
  className="w-11 h-11 rounded-full hover:bg-slate-100 text-slate-500 transition-colors flex items-center justify-center"
>
  <X className="w-5 h-5" strokeWidth={1.5}/>
</button>
>>>>
```

---

## ۴. ممیزی تارگت‌های لمسی (Touch Targets $\ge 44\text{px}$ Audit)

بر اساس استانداردهای بین‌المللی WCAG 2.2 Success Criterion 2.5.8 و Apple Human Interface Guidelines، حداقل مساحت لمسی تمامی دکمه‌ها و لینک‌های کنترلی در رابط کاربری لمسی باید $44 \times 44$ پیکسل باشد.

| کامپوننت / المان | مسیر فایل | کلاس قبلی (نامنطبق) | کلاس اصلاحی پیشنهادی (منطبق $\ge 44\text{px}$) | ابعاد نهایی | وضعیت |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **نقاط اسلایدر هیرو** | `FashionTemplate.tsx:291` | `h-11 w-11` | `h-11 w-11 flex items-center justify-center` | $44 \times 44\text{px}$ | ✅ از قبل منطبق |
| **چیپ‌های دسته‌بندی** | `FashionTemplate.tsx:369` | `min-h-[44px]` | `min-h-[44px] px-4 inline-flex items-center` | $\ge 44\text{px}$ | ✅ از قبل منطبق |
| **چیپ‌های برند** | `FashionTemplate.tsx:662` | `min-h-[44px]` | `min-h-[44px] px-4 inline-flex items-center` | $\ge 44\text{px}$ | ✅ از قبل منطبق |
| **دکمه بستن اعلان‌ها** | `NotificationDrawer.tsx:133` | `p-1 rounded-lg` | `w-11 h-11 rounded-xl flex items-center justify-center` | $44 \times 44\text{px}$ | 🔧 نیازمند اعمال |
| **دکمه بستن مشخصات کالا** | `ProductDetailModal.tsx:107` | `p-1.5 rounded-full` | `w-11 h-11 rounded-full flex items-center justify-center` | $44 \times 44\text{px}$ | 🔧 نیازمند اعمال |
| **دکمه بستن اورلی سرچ** | `SellerStorefrontView.tsx:327` | `p-1.5 rounded-xl` | `w-11 h-11 rounded-xl flex items-center justify-center` | $44 \times 44\text{px}$ | 🔧 نیازمند اعمال |
| **دکمه‌های تب ناوبری پایین** | `SellerStorefrontFooter.tsx:210`| `py-2 px-1` | `min-h-[44px] py-1.5 px-1 flex flex-col items-center` | $\ge 44\text{px}$ | 🔧 نیازمند اعمال |
| **دکمه استپر تعداد (+/-)** | `ProductDetailModal.tsx:147` | `px-2.5 py-1.5 text-sm` | `min-w-[44px] min-h-[44px] flex items-center justify-center` | $44 \times 44\text{px}$ | 🔧 نیازمند اعمال |
| **دکمه‌های هدر فشن** | `SellerStorefrontHeader.tsx` | `p-2 rounded-full` | `w-11 h-11 rounded-[2px] flex items-center justify-center` | $44 \times 44\text{px}$ | 🔧 هماهنگ با اکسپلورر ۲ |

---

## ۵. چک‌لیست جلوگیری از سرریز افقی (Horizontal Overflow Zero-Defect Checklist)

1. **موبایل ۳۷۵ و ۳۹۰ پیکسل:**
   - هیچ المانی دارای عرض ثابت بزرگ‌تر از ۳۲۰ پیکسل (`w-[>320px]`) بدون کلمپ یا کلاس ریسپانسیو نیست.
   - کاروسل‌ها و ریل‌های افقی دسته‌بندی و مجموعه‌ها دارای کلاس‌های `overflow-x-auto no-scrollbar` و `snap-x` هستند.
   - تمام متون و عناوین طولانی مجهز به `truncate` یا `line-clamp-1` / `line-clamp-2` هستند.
2. **تبلت ۷۶۸ پیکسل:**
   - با اعمال `md:max-w-none`، صفحه بدون هرگونه اسکرول افقی در عرض ۷۶۸ باز می‌شود.
   - گرید ۳ ستونه کاتالوگ با پدینگ `md:px-6` عرض کل صفحه را دقیقاً پر می‌کند.
3. **دسکتاپ ۱۲۸۰ پیکسل و بالاتر:**
   - کانتینر ادیتوریال `max-w-[1440px] mx-auto` در مرکز صفحه با حاشیه‌های متقارن قرار می‌گیرد.
   - بنرهای عریض نسبت ۲۱/۹ پیدا کرده و کل پهنا را پر می‌کنند.

---

## ۶. جدول ترتیبی و گام‌های اجرای ورکر پیاده‌سازی (Worker Action Plan)

| گام | فایل هدف | تغییرات دقیق | ابزار / دستور اعتبارسنجی |
| :---: | :--- | :--- | :--- |
| **۱** | `src/App.tsx` | ویرایش سطر ۲۱۴: افزودن `md:max-w-none md:shadow-none md:border-x-0 md:pb-0` به شرط `isStorefront` | `npm run build` |
| **۲** | `SellerStorefrontHeader.tsx` | افزودن کانتینر `mx-auto w-full max-w-[1440px] px-4 md:px-6 lg:px-10` به ردیف بالا | `npm run build` |
| **۳** | `SellerStorefrontView.tsx` | افزودن کانتینر مهارشده به اورلی سرچ و ارتقای دکمه بستن به `w-11 h-11` | `npm run build` |
| **۴** | `SellerStorefrontFooter.tsx` | ارتقای کانتینر فوتر به `max-w-[1440px]` و افزودن `min-h-[44px]` به دکمه‌های ناوبری | `npm run build` |
| **۵** | `FashionTemplate.tsx` | افزودن `md:px-6` به `CONTAINER`، اضافه کردن `md:grid-cols-3` به گرید کالاها و ماکرو دسته‌ها، و ۲ ستونه کردن داستان برند در تبلت | `npm run build` |
| **۶** | `CartDrawer.tsx` | افزودن ابعاد تبلت (`md:max-w-[680px] md:rounded-2xl`) و گرید دو ستونه تسویه در تبلت | `npm run build` |
| **۷** | `NotificationDrawer.tsx` | ارتقای دکمه بستن به `w-11 h-11` و ری‌ریسپانسیو کردن کانتینر در تبلت | `npm run build` |
| **۸** | `ProductDetailModal.tsx` | ارتقای دکمه بستن و استپر به مساحت ۴۴ پیکسل و عرض ریسپانسیو در تبلت | `npm run build` |

---
**پایان سند طرح پیاده‌سازی بهینه‌سازی لایه‌های واکنش‌گرا.**
