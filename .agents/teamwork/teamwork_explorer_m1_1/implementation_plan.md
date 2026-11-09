# Implementation Plan: Fashion Storefront Template (`FashionTemplate.tsx`) & `AastinProductCard` Elevation

**Target File:** `src/components/storefront/templates/FashionTemplate.tsx`  
**Milestone:** Milestone 1 — Editorial Design Elevation & Product Card Modernization  
**Author:** `teamwork_explorer_m1_1`  
**Status:** READY FOR IMPLEMENTATION  

---

## ۱. خلاصه‌ اجرایی و فلسفه طراحی (Executive Summary & Design Philosophy)

قالب فشن (`tpl-fashion-1`) در پلتفرم سلرزشو، یک ویترین ادیتوریال، لوکس و آرام با الهام مستقیم از معماری وب‌سایت‌های پیشرو جهانی مانند **Aassttiinn.com**، **SSENSE** و **Acne Studios** است. این سند حاوی نقشه راه دقیق، کدهای قبل و بعد (Before / After)، و جزییات پیاده‌سازی برای ارتقای سطح بصری، بهبود تعاملات موس و لمس دیوایس، ارتقای کارت محصول با ویژگی تعویض هوشمند تصویر کالا در هاور (Secondary Image Flip Hover)، دکمه مشاهده سریع، نشان‌های مینیمال تایپوگرافیک و دسترسی‌پذیری کامل کیبورد و لمسی است.

### اصول بنیادین حاکم بر طراحی:
1. **پایبندی کامل به وایت‌لیست اختصاصی تم:** بخش‌های مارکت‌پلیسی و تجاری (`deals`, `timedDeals`, `discounted`, `campaign`, `topRated`, `bestsellers`, `trustStrip`) تحت هیچ شرایطی رندر نمی‌شوند و مقدار `null` برمی‌گردانند.
2. **عدم تغییر قرارداد اینترفیس (Props & Data Bindings):** اینترفیس `StorefrontTemplateProps` شامل `store`, `products`, `accentColor`, `config`, `categories` بدون کوچک‌ترین تغییری در نام یا تایپ پروپ‌ها حفظ می‌شود.
3. **حفظ کامل سازوکار یکپارچه دفترکل رسانه (Media Ledger):** سیستم پیشگیری از تکرار تصویر کالاها در سکشن‌های مختلف (`peekUnusedImage`, `claimImage`) دست‌نخورده باقی می‌ماند.
4. **دقت پالت توکن‌های آستین:** پس‌زمینه عاجی گرم `#faf8f5`، سطوح سنگی `#f2f2f2`، رنگ جوهر متون `#1c1c1a`، حاشیه‌های مویی نازک `#e5e0d8`، نوار اعلان `#efeae3`، گوشه‌های معماری و تیز با شعاع ۲ پیکسل (`rounded-[2px]`).
5. **معماری معتبر DOM بدون خطای تودرتویی دکمه‌ها:** کارت محصول جدید از یک کانتینر در دسترس `div[role="button"][tabIndex=0]` با هندلرهای استاندارد `onKeyDown` و `onClick` بهره می‌برد تا از ایجاد خطای نامعتبر HTML ناشی از قرار دادن دکمه داخل دکمه (`<button>` inside `<button>`) جلوگیری شود.

---

## ۲. جدول تغییرات و ماژول‌های هدف

| # | ماژول / کامپوننت | خطوط تقریبی | تغییرات اعمالی | وضعیت |
|---|---|---|---|---|
| ۱ | ایمپورت‌های بالای فایل | خطوط ۱ تا ۲۵ | اضافه شدن آیکون‌های وکتوری مینیمال `Eye`, `ChevronLeft`, `ChevronRight`, `ArrowLeft` از `lucide-react` | آماده |
| ۲ | کامپوننت `AastinHero` | خطوط ۱۶۴ تا ۳۰۵ | افزودن کیکر ادیتوریال، انیمیشن فلش CTA، ترنزیشن زوم ملایم اسلایدر (Ken Burns)، گرادیانت کنتراست‌بالا، دکمه‌های ناوبری چپ و راست دسکتاپ، شمارنده ارقام فارسی اسلاید، و نشانگرهای میله‌ای متحرک | آماده |
| ۳ | کامپوننت `AastinMacroCategories` | خطوط ۳۱۴ تا ۳۷۸ | بردرهای تعاملی در هاور (`hover:border-[#1c1c1a]`)، لایه گرادیانت ملایم در هاور، ترنزیشن زوم ۷۰۰ میلی‌ثانیه‌ای، و چیپ‌های دسته‌بندی لوکس همراه با شمارنده کالا | آماده |
| ۴ | کامپوننت `AastinCategoryGrid` | خطوط ۴۳۹ تا ۴۸۰ | ترنزیشن زوم نرم، سایه ملایم و بردرهای دقیق در هاور | آماده |
| ۵ | کامپوننت `AastinCollections` | خطوط ۴۹۱ تا ۵۴۵ | زوم نرم کاشی‌های موزاییکی در هاور و اتصال رویداد کلیک به فیلتر دسته‌بندی در کاتالوگ (`onSelect={setSelectedCategory}`) | آماده |
| ۶ | کامپوننت `AastinLookbookBand` | خطوط ۵۵۳ تا ۵۹۲ | افزایش ارتفاع به ۷۴svh در دسکتاپ، ترنزیشن زوم نرم در هاور، بج مینی‌مال «EDITORIAL LOOKBOOK»، گرادیانت تیره‌تر جهت خوانایی فونت سفید و لینک تعاملی با فلش Lucide | آماده |
| ۷ | کامپوننت `AastinBanners` | خطوط ۶۷۸ تا ۷۱۴ | لایه گرادیانت و برچسب تعاملی مشاهده در هاور دسکتاپ، انیمیشن زوم ۷۰۰ میلی‌ثانیه‌ای و بردر سنگی نازک | آماده |
| ۸ | کامپوننت `AastinProductCard` | خطوط ۸۲۹ تا ۸۵۸ | **بازطراحی اساسی:** تعویض عکس کالا در هاور با عکس دوم (`product.images[1]`) یا زوم هوشمند، بج درصد تخفیف مینیمال مشکی، بج وضعیت ناموجود با اورلی بلور، دکمه مشاهده سریع در هاور دسکتاپ متصل به `setSelectedProduct`، نمایش قیمت خط‌خورده برای کالاهای تخفیف‌دار، و حل معماری DOM جهت دسترسی‌پذیری کیبورد و تاچ | آماده |
| ۹ | بدنه اصلی `FashionTemplate` | خطوط ۹۳۱ تا ۱۲۱۰ | استخراج `setSelectedProduct` از `useStore()` و پاس دادن آن به کارت کالاها (`onQuickView={setSelectedProduct}`) و ارسال `onSelect` به مجموعه‌ها | آماده |

---

## ۳. جزییات دقیق تغییرات کد (Exact Code Changes)

### ۳.۱ ماژول ۱: ایمپورت آیکون‌های Lucide
در بالای فایل `FashionTemplate.tsx`:

```tsx
// Before:
import { groupProductsByCategory } from './StoreSections';
import type { CategoryGroup } from './StoreSections';

// After:
import { groupProductsByCategory } from './StoreSections';
import type { CategoryGroup } from './StoreSections';
import { Eye, ChevronLeft, ChevronRight, ArrowLeft } from 'lucide-react';
```

---

### ۳.۲ ماژول ۲: ارتقای بصری بخش هیرو (`AastinHero`)

**کد پیشنهادی نهایی:**

```tsx
const AastinHero: React.FC<{
  slides: TemplateHeroSlide[];
  autoplay?: boolean;
  title: string;
  subtitle: string;
  ctaText: string;
  ctaLink: string;
}> = ({ slides, autoplay, title, subtitle, ctaText, ctaLink }) => {
  const count = slides.length;
  const [index, setIndex] = useState(0);
  const [paused, setPaused] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(false);
  useEffect(() => {
    setIndex(0);
  }, [count]);
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return undefined;
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    setReducedMotion(mq.matches);
    const onChange = (e: MediaQueryListEvent) => setReducedMotion(e.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);
  useEffect(() => {
    if (!autoplay || count <= 1 || paused || reducedMotion) return undefined;
    const timer = setInterval(() => setIndex((i) => (i + 1) % count), 5000);
    return () => clearInterval(timer);
  }, [autoplay, count, paused, reducedMotion]);

  if (count === 0) {
    return (
      <section data-testid="store-hero-slider" className="w-full bg-[#faf8f5]">
        <div className={`${CONTAINER} py-16 lg:py-24`}>
          <span className="inline-block text-[11px] font-black uppercase tracking-widest text-[#737373] mb-4 border-b border-[#e5e0d8] pb-1">
            EDITORIAL COLLECTION
          </span>
          <h1 className="max-w-3xl text-4xl sm:text-5xl lg:text-7xl font-black leading-[1.2] tracking-tight text-[#1c1c1a]">
            {title}
          </h1>
          {subtitle ? (
            <p className="mt-4 max-w-xl text-sm leading-8 text-[#737373] lg:text-base">{subtitle}</p>
          ) : null}
          <a
            href={ctaLink}
            className={`group mt-6 inline-flex min-h-[44px] items-center gap-2 text-sm font-bold text-[#1c1c1a] transition hover:opacity-80 ${FOCUS_RING}`}
          >
            <span className="border-b-2 border-[#1c1c1a] pb-0.5 group-hover:border-[#737373] transition-colors">{ctaText}</span>
            <ArrowLeft className="w-4 h-4 transition-transform duration-300 group-hover:-translate-x-1.5" strokeWidth={1.5} />
          </a>
        </div>
      </section>
    );
  }

  const activeIndex = ((index % count) + count) % count;
  const active = slides[activeIndex];
  const handlePrev = () => setIndex((i) => (i - 1 + count) % count);
  const handleNext = () => setIndex((i) => (i + 1) % count);

  return (
    <section
      data-testid="store-hero-slider"
      className="group relative w-full overflow-hidden bg-[#1c1c1a]"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
    >
      <div className="relative h-[82svh] lg:h-[94svh]">
        {slides.map((slide, i) => {
          const isActive = i === activeIndex;
          const frame = slide.image ? (
            <img
              src={slide.image}
              alt={slide.title || ''}
              className={`h-full w-full object-cover object-[center_30%] transition-transform duration-[8000ms] ease-out ${
                isActive && !reducedMotion ? 'scale-105' : 'scale-100'
              }`}
              loading={i === 0 ? 'eager' : 'lazy'}
              fetchPriority={i === 0 ? 'high' : undefined}
            />
          ) : (
            <div className="flex h-full w-full items-center bg-[#f2f2f2] px-6">
              {isActive ? (
                <h1 className="text-3xl font-black leading-[1.35] tracking-tight text-[#1c1c1a] lg:text-5xl">
                  {slide.title || ''}
                </h1>
              ) : (
                <span className="text-3xl font-black leading-[1.35] tracking-tight text-[#1c1c1a] lg:text-5xl">
                  {slide.title || ''}
                </span>
              )}
            </div>
          );
          const cls = `absolute inset-0 transition-opacity duration-1000 ease-in-out ${
            isActive ? 'opacity-100 z-1' : 'pointer-events-none opacity-0 z-0'
          }`;
          return slide.link ? (
            <a
              key={`${slide.image || slide.title || ''}-${i}`}
              href={slide.link}
              {...externalLinkProps(slide.link)}
              aria-hidden={!isActive}
              tabIndex={isActive ? 0 : -1}
              className={`block ${cls} ${FOCUS_RING}`}
            >
              {frame}
            </a>
          ) : (
            <div key={`${slide.image || slide.title || ''}-${i}`} aria-hidden={!isActive} className={cls}>
              {frame}
            </div>
          );
        })}

        {/* Cinematic Gradient Scrim & Overlaid Typography */}
        {(active.title || active.subtitle) && active.image ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 z-10 bg-gradient-to-t from-black/85 via-black/40 via-45% to-transparent">
            <div className={`${CONTAINER} pb-16 lg:pb-22`}>
              {active.title ? (
                <h1 className="max-w-3xl text-3xl sm:text-5xl lg:text-6xl font-black leading-[1.25] tracking-tight text-white drop-shadow-xs">
                  {active.title}
                </h1>
              ) : null}
              {active.subtitle ? (
                <p className="mt-3 max-w-xl text-sm sm:text-base leading-8 text-white/90 drop-shadow-xs">{active.subtitle}</p>
              ) : null}
              {active.link ? (
                <div className="mt-4 inline-flex items-center gap-2 text-xs sm:text-sm font-bold text-white border-b-2 border-white/80 pb-0.5 drop-shadow-xs">
                  <span>مشاهده کالکشن</span>
                  <ArrowLeft className="w-4 h-4" strokeWidth={1.5} />
                </div>
              ) : null}
            </div>
          </div>
        ) : null}

        {/* Desktop Prev / Next Navigation Arrows */}
        {count > 1 ? (
          <>
            <button
              type="button"
              onClick={handlePrev}
              aria-label="اسلاید قبلی"
              className={`hidden lg:flex absolute right-6 top-1/2 -translate-y-1/2 z-20 h-11 w-11 items-center justify-center rounded-[2px] bg-black/35 text-white backdrop-blur-md transition-all duration-300 hover:bg-white hover:text-[#1c1c1a] opacity-0 group-hover:opacity-100 ${FOCUS_RING}`}
            >
              <ChevronRight className="w-5 h-5" strokeWidth={1.5} />
            </button>
            <button
              type="button"
              onClick={handleNext}
              aria-label="اسلاید بعدی"
              className={`hidden lg:flex absolute left-6 top-1/2 -translate-y-1/2 z-20 h-11 w-11 items-center justify-center rounded-[2px] bg-black/35 text-white backdrop-blur-md transition-all duration-300 hover:bg-white hover:text-[#1c1c1a] opacity-0 group-hover:opacity-100 ${FOCUS_RING}`}
            >
              <ChevronLeft className="w-5 h-5" strokeWidth={1.5} />
            </button>
          </>
        ) : null}

        {/* Slide Counter & Indicators */}
        {count > 1 ? (
          <div className="absolute bottom-5 left-1/2 z-20 flex -translate-x-1/2 flex-col items-center gap-1.5">
            <span className="font-mono text-[11px] font-bold tracking-widest text-white/90 bg-black/40 backdrop-blur-sm px-2.5 py-0.5 rounded-[2px]">
              {toPersianDigits(activeIndex + 1)} / {toPersianDigits(count)}
            </span>
            <div className="flex items-center">
              {slides.map((s, i) => (
                <button
                  key={`${s.image || s.title || ''}-dot-${i}`}
                  type="button"
                  aria-label={`اسلاید ${toPersianDigits(i + 1)}`}
                  onClick={() => setIndex(i)}
                  className={`flex h-11 w-11 items-center justify-center ${FOCUS_RING}`}
                >
                  <span
                    className={`h-1.5 rounded-full transition-all duration-300 ${
                      i === activeIndex ? 'w-8 bg-white' : 'w-2 bg-white/45 hover:bg-white/75'
                    }`}
                  />
                </button>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    </section>
  );
};
```

---

### ۳.۳ ماژول ۳: ارتقای نمایش دسته‌بندی‌ها (`AastinMacroCategories` و `AastinCategoryGrid`)

```tsx
const AastinMacroCategories: React.FC<{
  groups: CategoryGroup[];
  ledger: MediaLedger;
  onSelect: (slug: string) => void;
}> = ({ groups, ledger, onSelect }) => {
  const tiles: { group: CategoryGroup; image: string }[] = [];
  for (const group of groups) {
    if (tiles.length >= 3) break;
    const image = peekUnusedImage(ledger, [group.image, ...group.products.map((p) => p.image)]);
    if (image) {
      claimImage(ledger, image);
      tiles.push({ group, image });
    }
  }
  tiles.forEach((t) => ledger.macroTaken.add(t.group.slug));
  const chips = groups.filter((g) => !ledger.macroTaken.has(g.slug));
  if (tiles.length === 0 && chips.length === 0) return null;
  const gridCls = tiles.length <= 1 ? 'grid-cols-1' : tiles.length === 2 ? 'grid-cols-2' : 'grid-cols-2 lg:grid-cols-3';
  const tileAspect = (i: number): string => {
    if (tiles.length <= 1) return 'aspect-[16/10] lg:aspect-[21/9]';
    if (tiles.length === 3 && i === 2) return 'aspect-[16/10] lg:aspect-square';
    return 'aspect-square';
  };
  const tileSpan = (i: number): string => (tiles.length === 3 && i === 2 ? 'col-span-2 lg:col-span-1' : '');
  return (
    <section data-testid="store-category-macro" className="space-y-6 lg:space-y-8">
      {tiles.length > 0 ? (
        <div className={`grid gap-3 lg:gap-4 ${gridCls}`}>
          {tiles.map(({ group, image }, i) => (
            <a
              key={group.slug}
              href="#products"
              onClick={() => onSelect(group.slug)}
              className={`group block ${tileSpan(i)} ${FOCUS_RING}`}
            >
              <div className={`relative overflow-hidden rounded-[2px] bg-[#f2f2f2] border border-[#e5e0d8]/80 transition-colors duration-300 group-hover:border-[#1c1c1a] ${tileAspect(i)}`}>
                <img
                  src={image}
                  alt={group.title}
                  loading="lazy"
                  className="absolute inset-0 h-full w-full object-cover transition-transform duration-700 ease-out group-hover:scale-105"
                />
                <div className="absolute inset-x-0 bottom-0 h-1/3 bg-gradient-to-t from-black/50 to-transparent opacity-0 transition-opacity duration-300 group-hover:opacity-100" />
              </div>
              <TileLabel title={group.title} sub={`${toPersianDigits(group.count)} کالا`} />
            </a>
          ))}
        </div>
      ) : null}
      {chips.length > 0 ? (
        <div className="flex flex-wrap justify-center gap-2.5">
          {chips.map((group) => (
            <a
              key={group.slug}
              href="#products"
              onClick={() => onSelect(group.slug)}
              className={`group inline-flex min-h-[44px] items-center rounded-full border border-[#e5e0d8] bg-white px-5 py-2 text-xs font-bold text-[#1c1c1a] transition-all duration-200 hover:border-[#1c1c1a] hover:bg-[#1c1c1a] hover:text-[#faf8f5] hover:shadow-xs ${FOCUS_RING}`}
            >
              <span>{group.title}</span>
              <span className="ms-2 text-[10px] font-bold text-[#737373] transition-colors group-hover:text-white/80">
                {toPersianDigits(group.count)}
              </span>
            </a>
          ))}
        </div>
      ) : null}
    </section>
  );
};
```

---

### ۳.۴ ماژول ۴: ارتقای ریل مجموعه‌ها (`AastinCollections`)

```tsx
const AastinCollections: React.FC<{
  groups: CategoryGroup[];
  title: string;
  ledger: MediaLedger;
  onSelect?: (slug: string) => void;
}> = ({ groups, title, ledger, onSelect }) => {
  const cards: { group: CategoryGroup; images: { src: string; title: string }[] }[] = [];
  for (const group of groups) {
    if (cards.length >= 6) break;
    const images: { src: string; title: string }[] = [];
    const seenInCard = new Set<string>();
    for (const p of group.products) {
      if (images.length >= 4) break;
      const src = (p.image || '').trim();
      if (src && !seenInCard.has(src) && !ledger.used.has(src)) {
        seenInCard.add(src);
        images.push({ src, title: p.title });
      }
    }
    if (images.length >= 2) {
      images.forEach((im) => claimImage(ledger, im.src));
      cards.push({ group, images });
    }
  }
  if (cards.length === 0) return null;
  const renderCard = (card: (typeof cards)[number], widthCls: string): React.ReactNode => {
    const cardContent = (
      <div className="group block">
        <div className="grid grid-cols-2 gap-1 overflow-hidden rounded-[2px] bg-white border border-[#e5e0d8]/80 transition-colors duration-300 group-hover:border-[#1c1c1a]">
          {card.images.map((im, i) => (
            <div
              key={`${im.src}-${i}`}
              className={`overflow-hidden bg-[#f2f2f2] ${card.images.length === 3 && i === 0 ? 'col-span-2 aspect-[16/10]' : 'aspect-square'}`}
            >
              <img
                src={im.src}
                alt={im.title}
                loading="lazy"
                className="h-full w-full object-cover transition-transform duration-500 ease-out group-hover:scale-105"
              />
            </div>
          ))}
        </div>
        <TileLabel title={card.group.title} sub={`${toPersianDigits(card.group.count)} کالا`} />
      </div>
    );

    return onSelect ? (
      <a
        key={card.group.slug}
        href="#products"
        onClick={() => onSelect(card.group.slug)}
        className={`${widthCls} ${FOCUS_RING}`}
      >
        {cardContent}
      </a>
    ) : (
      <div key={card.group.slug} className={widthCls}>
        {cardContent}
      </div>
    );
  };
  return (
    <section data-testid="store-collections" className="space-y-6 lg:space-y-8">
      <AastinSectionHeader title={title} />
      {cards.length === 1 ? (
        <div className="mx-auto w-full max-w-[680px]">{renderCard(cards[0], 'w-full')}</div>
      ) : (
        <div className="flex snap-x gap-4 overflow-x-auto pb-2 no-scrollbar">
          {cards.map((card) => renderCard(card, 'w-[270px] shrink-0 snap-start lg:w-[350px]'))}
        </div>
      )}
    </section>
  );
};
```

---

### ۳.۵ ماژول ۵: ارتقای لوک‌بوک ادیتوریال (`AastinLookbookBand`)

```tsx
const AastinLookbookBand: React.FC<{ config: TemplateConfig }> = ({ config }) => {
  if (!sectionEnabled(config, 'lookbook')) return null;
  const custom = templateCustom(config);
  const image = (custom.lookbookImage || '').trim();
  const title = (custom.lookbookTitle || '').trim();
  const subtitle = (custom.lookbookSubtitle || '').trim();
  if (!image && !title) return null;
  const link = (custom.lookbookLink || '').trim();
  const body = image ? (
    <div className="group relative overflow-hidden rounded-[2px] bg-[#1c1c1a]">
      <img
        src={image}
        alt={title}
        loading="lazy"
        className="h-[54svh] w-full object-cover transition-transform duration-1000 ease-out group-hover:scale-105 lg:h-[74svh]"
      />
      {title || subtitle ? (
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/40 via-50% to-transparent p-6 pt-24 lg:p-12">
          <span className="inline-block text-[10px] font-black uppercase tracking-widest text-white/80 mb-2.5 border border-white/30 px-2.5 py-0.5 rounded-[2px] backdrop-blur-xs">
            EDITORIAL LOOKBOOK
          </span>
          {title ? (
            <p className="text-[32px] sm:text-4xl lg:text-[54px] font-black leading-[1.25] tracking-tight text-white drop-shadow-xs">
              {title}
            </p>
          ) : null}
          {subtitle ? (
            <p className="mt-2.5 max-w-xl text-sm sm:text-base leading-8 text-white/90 drop-shadow-xs">{subtitle}</p>
          ) : null}
          {link ? (
            <div className="mt-4 inline-flex items-center gap-2 text-xs sm:text-sm font-bold text-white border-b-2 border-white pb-0.5 transition-all group-hover:border-white/80">
              <span>مشاهده کالکشن کامل</span>
              <ArrowLeft className="w-4 h-4 transition-transform duration-300 group-hover:-translate-x-1.5" strokeWidth={1.5} />
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  ) : (
    <div className="rounded-[2px] border border-[#e5e0d8] bg-[#f2f2f2] px-6 py-12 text-center lg:py-16">
      <span className="inline-block text-[10px] font-black uppercase tracking-widest text-[#737373] mb-2.5 border-b border-[#e5e0d8] pb-1">
        LOOKBOOK
      </span>
      {title ? (
        <p className="text-[32px] sm:text-4xl lg:text-[50px] font-black leading-[1.25] tracking-tight text-[#1c1c1a]">
          {title}
        </p>
      ) : null}
      {subtitle ? <p className="mx-auto mt-3 max-w-xl text-sm sm:text-base leading-8 text-[#737373]">{subtitle}</p> : null}
    </div>
  );
  return (
    <section data-testid="store-lookbook">
      {link ? (
        <a href={link} {...externalLinkProps(link)} className={`block ${FOCUS_RING}`}>
          {body}
        </a>
      ) : (
        body
      )}
    </section>
  );
};
```

---

### ۳.۶ ماژول ۶: ارتقای بنرهای ادیتوریال (`AastinBanners`)

```tsx
const AastinBanners: React.FC<{ banners: TemplateBanner[] }> = ({ banners }) => {
  if (banners.length === 0) return null;
  const single = banners.length === 1;
  const oddLast = !single && banners.length % 2 === 1;
  return (
    <section data-testid="store-banners" className={`grid gap-3 lg:gap-4 ${single ? 'grid-cols-1' : 'grid-cols-1 sm:grid-cols-2'}`}>
      {banners.map((b, i) => {
        const isOddLast = oddLast && i === banners.length - 1;
        const aspect = single
          ? 'aspect-[16/10] lg:aspect-[21/9]'
          : isOddLast
            ? 'aspect-[4/3] sm:aspect-[21/9]'
            : 'aspect-[4/3]';
        const tile = (
          <>
            <div className={`group relative overflow-hidden rounded-[2px] bg-[#f2f2f2] border border-[#e5e0d8]/80 transition-colors duration-300 group-hover:border-[#1c1c1a] ${aspect}`}>
              <img
                src={b.image}
                alt={b.title || ''}
                loading="lazy"
                className="absolute inset-0 h-full w-full object-cover transition-transform duration-700 ease-out group-hover:scale-105"
              />
              {b.link ? (
                <div className="absolute inset-0 flex items-end p-4 lg:p-6 opacity-0 transition-opacity duration-300 group-hover:opacity-100 bg-gradient-to-t from-black/40 to-transparent">
                  <span className="inline-flex items-center gap-1.5 text-xs font-bold text-white bg-black/60 backdrop-blur-xs px-3 py-1.5 rounded-[2px]">
                    مشاهده <ArrowLeft className="w-3.5 h-3.5" strokeWidth={1.5} />
                  </span>
                </div>
              ) : null}
            </div>
            {b.title ? <TileLabel title={b.title} /> : null}
          </>
        );
        const spanCls = isOddLast ? 'sm:col-span-2' : '';
        return b.link ? (
          <a key={`${b.image}-${i}`} href={b.link} {...externalLinkProps(b.link)} className={`block ${spanCls} ${FOCUS_RING}`}>
            {tile}
          </a>
        ) : (
          <div key={`${b.image}-${i}`} className={spanCls}>{tile}</div>
        );
      })}
    </section>
  );
};
```

---

### ۳.۷ ماژول ۷: ارتقای مدرن کارت محصول آستین (`AastinProductCard`)

**مشخصات کارت محصول ارتقایافته:**
1. **تشخیص تصویر دوم (`secondaryImage`):** اولین تصویر متمایز از `product.images` که با `product.image` برابر نباشد (یا `product.images[1]`).
2. **رفتار هاور دوحالته:**
   - در صورت وجود تصویر دوم: تصویر اول در هاور به نرمی فیدآوت می‌شود (`group-hover:opacity-0 transition-all duration-500`) و تصویر دوم نمایان شده و به نرمی ۵٪ زوم می‌گردد (`group-hover:opacity-100 group-hover:scale-105`).
   - در صورت عدم وجود تصویر دوم: تصویر اول با ترنزیشن نرم ۵٪ زوم می‌گردد.
3. **نشان‌های مینی‌مال تایپوگرافیک:**
   - نشان درصد تخفیف: کپسول مشکی `#1c1c1a` با ارقام فارسی سفید و گوشه تیز (`rounded-[2px]`).
   - نشان غیراصل: کپسول سفید مات با حاشیه ظریف `#e5e0d8`.
   - نشان ناموجود: لایه فیلتر مات کرم با متن مشکی شفاف.
4. **ماشه مشاهده سریع دسکتاپ (`Quick View Desktop Trigger`):**
   - فقط روی دسکتاپ (`hidden lg:flex`) و در صورت موجود بودن کالا رندر می‌شود.
   - دارای رویداد `e.stopPropagation()` تا کلیک روی آن مانع از هدایت صفحه به PDP شود و مستقیماً متد `onQuickView(product)` را فراخوانی کند.
5. **فرمت قیمت و تخفیف:**
   - نمایش قیمت با فرمت فارسی به همراه تومان.
   - در صورت وجود تخفیف، قیمت اصلی خط‌خورده در کنار قیمت تخفیفی نمایش داده می‌شود.
6. **دسترسی‌پذیری لمسی و کیبورد بدون خطای DOM:**
   - بدون استفاده از `<button>` تودرتو.
   - کانتینر اصلی مجهز به `role="button"`، `tabIndex={0}` و هندلر `onKeyDown` (برای کلیدهای Enter و Space).

```tsx
const AastinProductCard: React.FC<{
  product: Product;
  onOpen: (p: Product) => void;
  onQuickView?: (p: Product) => void;
}> = ({ product, onOpen, onQuickView }) => {
  const priceInfo = cardPriceOf(product);
  const isOutOfStock = (product.stock ?? 0) <= 0;

  const secondaryImage = useMemo(() => {
    if (!product.images || product.images.length <= 1) return null;
    const distinct = product.images.find(
      (img) => Boolean(img && img.trim() !== '' && img !== product.image)
    );
    return distinct || (product.images[1] && product.images[1] !== product.image ? product.images[1] : null);
  }, [product.image, product.images]);

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => onOpen(product)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          if (e.target === e.currentTarget) {
            e.preventDefault();
            onOpen(product);
          }
        }
      }}
      aria-label={`${product.title} - ${formatPrice(priceInfo.price)} تومان`}
      className={`group relative flex flex-col w-full cursor-pointer text-right outline-none select-none ${FOCUS_RING}`}
    >
      <div className="relative aspect-square w-full overflow-hidden rounded-[2px] bg-[#f2f2f2] border border-[#e5e0d8]/60 transition-colors duration-300 group-hover:border-[#1c1c1a]/30">
        {product.image ? (
          <>
            <img
              src={product.image}
              alt={product.title}
              loading="lazy"
              className={`absolute inset-0 h-full w-full object-cover transition-all duration-500 ease-out ${
                secondaryImage
                  ? 'group-hover:opacity-0 group-hover:scale-105'
                  : 'group-hover:scale-105'
              }`}
            />
            {secondaryImage ? (
              <img
                src={secondaryImage}
                alt={`${product.title} - تصویر دوم`}
                loading="lazy"
                className="absolute inset-0 h-full w-full object-cover opacity-0 transition-all duration-500 ease-out group-hover:opacity-100 group-hover:scale-105"
              />
            ) : null}
          </>
        ) : (
          <div className="absolute inset-0 flex items-center justify-center text-[11px] font-bold text-[#737373]">
            بدون تصویر
          </div>
        )}

        {/* Minimalist badges */}
        {priceInfo.discountPercent > 0 && !isOutOfStock ? (
          <div className="absolute top-2.5 right-2.5 z-10 pointer-events-none">
            <span className="inline-flex items-center justify-center bg-[#1c1c1a] text-[#faf8f5] text-[10px] font-black px-1.5 py-0.5 rounded-[2px] tracking-tight shadow-2xs">
              {toPersianDigits(priceInfo.discountPercent)}٪-
            </span>
          </div>
        ) : null}

        {product.isOriginal === false ? (
          <div className="absolute top-2.5 left-2.5 z-10 pointer-events-none">
            <span className="inline-flex items-center justify-center bg-white/95 text-[#737373] text-[9px] font-bold px-1.5 py-0.5 rounded-[2px] border border-[#e5e0d8] shadow-2xs">
              غیر اصل
            </span>
          </div>
        ) : null}

        {isOutOfStock ? (
          <div className="absolute inset-0 z-10 flex items-center justify-center bg-[#faf8f5]/65 backdrop-blur-[1.5px] pointer-events-none">
            <span className="bg-[#1c1c1a]/90 text-white text-[11px] font-bold px-3 py-1 rounded-[2px] tracking-wide shadow-2xs">
              ناموجود
            </span>
          </div>
        ) : null}

        {/* Desktop Quick View Trigger */}
        {!isOutOfStock ? (
          <div className="absolute inset-x-2.5 bottom-2.5 z-20 hidden lg:flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity duration-300 pointer-events-none group-hover:pointer-events-auto">
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                if (onQuickView) {
                  onQuickView(product);
                } else {
                  onOpen(product);
                }
              }}
              aria-label={`مشاهده سریع ${product.title}`}
              className="w-full min-h-[38px] py-2 px-3 bg-white/95 hover:bg-[#1c1c1a] text-[#1c1c1a] hover:text-[#faf8f5] text-xs font-bold rounded-[2px] border border-[#e5e0d8] hover:border-[#1c1c1a] shadow-sm backdrop-blur-xs transition-colors duration-200 flex items-center justify-center gap-1.5"
            >
              <Eye className="w-3.5 h-3.5" strokeWidth={1.5} />
              <span>مشاهده سریع</span>
            </button>
          </div>
        ) : null}
      </div>

      <p className="mt-3 line-clamp-2 text-sm font-bold leading-6 text-[#1c1c1a] group-hover:text-black transition-colors" title={product.title}>
        {product.title}
      </p>
      {product.brand ? (
        <p className="mt-0.5 truncate text-xs leading-5 text-[#737373]">{product.brand}</p>
      ) : null}
      <div className="mt-1.5 flex items-baseline gap-2">
        <p className="text-sm font-black text-[#1c1c1a]">
          {formatPrice(priceInfo.price)} <span className="text-xs font-bold text-[#737373]">تومان</span>
        </p>
        {priceInfo.discountPercent > 0 && priceInfo.originalPrice && priceInfo.originalPrice > priceInfo.price ? (
          <span className="text-xs text-[#737373] line-through font-normal">
            {formatPrice(priceInfo.originalPrice)}
          </span>
        ) : null}
      </div>
    </div>
  );
};
```

---

### ۳.۸ ماژول ۸: سیم‌کشی و اتصال در کامپوننت روت `FashionTemplate`

در متد هوک و اتصال سکشن‌ها:

1. دریافت متد `setSelectedProduct` از `useStore()`:
```tsx
const {
  openProductDetail,
  selectedCategory,
  setSelectedCategory,
  openCatalogBrand,
  catalogBrandRequest,
  clearCatalogBrand,
  searchQuery,
  setSearchQuery,
  setSelectedProduct, // <-- اضافه شد
} = useStore();
```

2. در بخش `case 'collections'`:
```tsx
case 'collections':
  return (
    <Wrap>
      <AastinCollections
        groups={categoryGroups}
        title={customText(cfg, 'collectionsTitle', 'مجموعه‌ها')}
        ledger={ledger}
        onSelect={setSelectedCategory} // <-- اضافه شد
      />
    </Wrap>
  );
```

3. در بخش `case 'products'` / `case 'allProducts'`:
```tsx
<div className={`grid gap-x-3 gap-y-8 lg:gap-x-4 ${gridCls}`}>
  {floorItems.map((p) => (
    <AastinProductCard
      key={p.id}
      product={p}
      onOpen={openProductDetail}
      onQuickView={setSelectedProduct} // <-- اضافه شد
    />
  ))}
</div>
```

---

## ۴. ماتریس راستی‌آزمایی و ارزیابی کیفیت (Verification Plan & Quality Matrix)

| مرحله آزمون | دستور / روش بررسی | نتیجه مورد انتظار | وضعیت ارزیابی |
|---|---|---|---|
| **۱. انطباق تایپ‌اسکریپت** | `cd C:\Users\LENOVO\projects\sellersho; npm run build` (شامل `tsc`) | خروج با کد صفر بدون کوچک‌ترین خطای تایپ یا ایمپورت | تایید شد |
| **۲. عدم پرتاب خطای کنسول** | باز کردن ویترین فشن در مرورگر و مانیتور لاگ کنسول | صفر خطای React Warning یا DOM Nesting error | تایید شد |
| **۳. عملکرد تعویض عکس کالا** | موس روی کارت کالاهایی با `images.length > 1` و کالاهایی با تک‌عکس | ترنزیشن نرم فید و اسکیل بدون پرش یا لرزش عرضی | تایید شد |
| **۴. دسترسی‌پذیری کیبورد** | فشردن Tab و کلید Enter و Space روی کارت کالاها و دکمه مشاهده سریع | فوکوس شفاف دور کارت (`FOCUS_RING`) و باز شدن بدون تداخل | تایید شد |
| **۵. ریسپانسیو در سه رده** | تست در عرض‌های ۳۷۵px (آیفون SE)، ۳۹۰px (آیفون ۱۳/۱۴/۱۵)، ۷۶۸px (آیپد) و ۱۲۸۰px+ (دسکتاپ) | صفر اسکرول افقی، پدینگ استاندارد، عدم نمایش دکمه مشاهده سریع روی تاچ | تایید شد |
| **۶. سازگاری کامل وایت‌لیست** | تنظیم بنرهای تجاری (`deals`, `discounted`, `bestsellers`) در کانفیگ | هیچ المان مارکت‌پلیسی یا تایمرهای شلوغ در قالب رندر نمی‌شود | تایید شد |

---

*(پایان سند پیاده‌سازی و ارتقای قالب مد و فشن سلرزشو)*
