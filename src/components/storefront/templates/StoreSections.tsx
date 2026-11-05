import React, { useEffect, useMemo, useState } from 'react';
import type { Store, Product, Category } from '../../../types';
import { TemplateProductGrid } from './TemplateProductGrid';
import { ProductCard } from '../../ProductCard';
import {
  sectionEnabled,
  templateCustom,
  heroSlidesOf,
  bannersOf,
  customText,
  customCount,
} from './templateTypes';
import type { TemplateConfig, TemplateCustom, TemplateHeroSlide } from './templateTypes';
import { toPersianDigits } from '../../../utils/formatters';
import { siteContentApi } from '../../../services/siteContentApi';
import { apiRequest } from '../../../services/apiClient';

// FIX3 — shared storefront section components, rebuilt from live
// behaviour (live tpl-* chunk evidence), not copied live code.
// Every section is self-hiding: disabled in config OR without data
// renders nothing, so stores without configured slides/banners never
// show empty shells. No section here reads pricing, settlement,
// discount, auth or admin state; deals are derived from the product
// list the storefront already renders (discountPercent / specialDeal
// flags on the catalogue product), and timed deals from the store's
// already-public deals widget config.

export const SectionTitle: React.FC<{ children: React.ReactNode; accentColor?: string }> = ({
  children,
  accentColor,
}) => (
  <div className="flex items-center gap-2.5">
    <span className="h-5 w-1.5 rounded-full shadow-sm" style={{ backgroundColor: accentColor || '#5d44f8' }} />
    <h3 className="text-base font-black tracking-tight text-slate-900">{children}</h3>
    <span className="h-px flex-1 bg-slate-200/80" />
  </div>
);

// --- Announcement bar (editable announcement / store tagline) -----
export const AnnouncementSection: React.FC<{
  store: Store;
  config: TemplateConfig;
  accentColor: string;
}> = ({ store, config, accentColor }) => {
  if (!sectionEnabled(config, 'announcement')) return null;
  const custom = templateCustom(config);
  const text = (custom.announcementText || '').trim() || (store.tagline || '').trim();
  if (!text) return null;
  const link = (custom.announcementLink || '').trim();
  const bar = (
    <div className="rounded-full px-4 py-2.5 text-center shadow-sm" style={{ backgroundColor: accentColor }}>
      <p className="text-[13px] font-extrabold leading-5 text-white">{text}</p>
    </div>
  );
  return (
    <section data-testid="store-announcement">
      {link ? (
        <a href={link} target="_blank" rel="noreferrer" className="block">
          {bar}
        </a>
      ) : (
        bar
      )}
    </section>
  );
};

// --- Hero slider (live heroSlides) ---------------------------------
export const HeroSlider: React.FC<{
  slides: TemplateHeroSlide[];
  accentColor: string;
  autoplay?: boolean;
}> = ({ slides, accentColor, autoplay }) => {
  const [index, setIndex] = useState(0);
  const count = slides.length;
  useEffect(() => {
    setIndex(0);
  }, [count]);
  useEffect(() => {
    if (!autoplay || count <= 1) return undefined;
    const timer = setInterval(() => setIndex((i) => (i + 1) % count), 5000);
    return () => clearInterval(timer);
  }, [autoplay, count]);
  if (count === 0) return null;
  const slide = slides[index % count];
  const body = (
    <div className="relative overflow-hidden rounded-[1.75rem] bg-slate-100 shadow-lg shadow-slate-900/10 ring-1 ring-black/5">
      {slide.image ? (
        <img
          src={slide.image}
          alt={slide.title || ''}
          className="h-52 w-full object-cover lg:h-80"
          loading="lazy"
        />
      ) : (
        <div className="flex h-52 w-full items-center justify-center lg:h-80" style={{ backgroundColor: accentColor }}>
          <span className="px-4 text-center text-xl font-black text-white lg:text-2xl">{slide.title || ''}</span>
        </div>
      )}
      {(slide.title || slide.subtitle) && slide.image ? (
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 via-black/30 to-transparent p-5 pt-16">
          {slide.title ? <p className="text-lg font-black leading-8 text-white drop-shadow-md lg:text-2xl lg:leading-10">{slide.title}</p> : null}
          {slide.subtitle ? <p className="mt-1 text-sm leading-6 text-white/90">{slide.subtitle}</p> : null}
        </div>
      ) : null}
    </div>
  );
  return (
    <div data-testid="store-hero-slider" className="group relative">
      {slide.link ? (
        <a href={slide.link} target="_blank" rel="noreferrer" className="block">
          {body}
        </a>
      ) : (
        body
      )}
      {count > 1 ? (
        <>
          <button
            type="button"
            aria-label="اسلاید قبلی"
            onClick={() => setIndex((i) => (i - 1 + count) % count)}
            className="absolute right-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-lg text-white shadow-lg ring-1 ring-white/30 backdrop-blur-md transition hover:bg-white/30 lg:opacity-0 lg:group-hover:opacity-100"
          >
            ›
          </button>
          <button
            type="button"
            aria-label="اسلاید بعدی"
            onClick={() => setIndex((i) => (i + 1) % count)}
            className="absolute left-3 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-lg text-white shadow-lg ring-1 ring-white/30 backdrop-blur-md transition hover:bg-white/30 lg:opacity-0 lg:group-hover:opacity-100"
          >
            ‹
          </button>
          <div className="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1.5 rounded-full bg-black/25 px-2 py-1.5 backdrop-blur-sm">
            {slides.map((s, i) => (
              <button
                key={`${s.image || s.title || ''}-${i}`}
                type="button"
                aria-label={`اسلاید ${toPersianDigits(i + 1)}`}
                onClick={() => setIndex(i)}
                className={`h-2 rounded-full shadow-sm transition-all duration-300 ${i === index ? 'w-7' : 'w-2 bg-white/60 hover:bg-white/85'}`}
                style={i === index ? { backgroundColor: accentColor } : undefined}
              />
            ))}
          </div>
        </>
      ) : null}
    </div>
  );
};

// --- Digikala hero fallback banners (live heroFallbackBanner*) ----
// Live defines these only for tpl-digikala-1: three banner texts plus
// a subtitle, shown when the store has no hero slides. Defaults are
// the placeholders from the live chunk evidence.
export const HeroFallbackBanners: React.FC<{ config: TemplateConfig; accentColor: string }> = ({
  config,
  accentColor,
}) => {
  const custom = templateCustom(config);
  const banners = [
    custom.heroFallbackBanner1 || 'پیشنهادهای شگفت‌انگیز',
    custom.heroFallbackBanner2 || 'جدیدترین محصولات',
    custom.heroFallbackBanner3 || 'تخفیف‌های ویژه',
  ];
  const subtitle = custom.heroFallbackSubtitle || 'هر روز، پیشنهادهای تازه';
  return (
    <div data-testid="store-hero-fallback" className="grid grid-cols-3 gap-3">
      {banners.map((text, i) => (
        <div
          key={text}
          className="relative overflow-hidden rounded-3xl p-4 text-white shadow-md"
          style={{ backgroundColor: accentColor, opacity: 1 - i * 0.15 }}
        >
          <span className="pointer-events-none absolute inset-0 bg-gradient-to-br from-white/25 via-white/5 to-black/10" />
          <span className="relative mb-2.5 flex h-8 w-8 items-center justify-center rounded-xl bg-white/20 shadow-sm ring-1 ring-white/30">
            <span className="h-2 w-2 rounded-full bg-white" />
          </span>
          <p className="relative text-sm font-black leading-6">{text}</p>
          {i === 0 ? <p className="relative mt-1 text-[11px] leading-5 opacity-90">{subtitle}</p> : null}
        </div>
      ))}
    </div>
  );
};

// --- Category helpers ----------------------------------------------
export interface CategoryGroup {
  slug: string;
  title: string;
  image: string;
  gradient: string;
  count: number;
  products: Product[];
}

export function groupProductsByCategory(
  products: Product[],
  categories: Category[] | undefined,
  overrides?: { slug: string; label?: string; name?: string; count?: number; image?: string }[],
): CategoryGroup[] {
  const bySlug = new Map<string, Product[]>();
  products.forEach((p) => {
    const slug = (p.category || '').trim();
    if (!slug) return;
    const list = bySlug.get(slug) || [];
    list.push(p);
    bySlug.set(slug, list);
  });
  const groups: CategoryGroup[] = [];
  bySlug.forEach((list, slug) => {
    const cat = (categories || []).find((c) => c.slug === slug);
    const override = (overrides || []).find((o) => o.slug === slug);
    groups.push({
      slug,
      title: override?.label || override?.name || cat?.title || slug,
      image: override?.image || cat?.image || '',
      gradient: cat?.gradient || 'from-slate-50 to-blue-50',
      count: override?.count ?? list.length,
      products: list,
    });
  });
  groups.sort((a, b) => b.count - a.count);
  return groups;
}

const CategoryThumb: React.FC<{ group: CategoryGroup }> = ({ group }) => (
  <div className={`flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br ${group.gradient} shadow-md ring-1 ring-slate-900/5 transition duration-300 group-hover:scale-105 group-hover:shadow-lg`}>
    {group.image ? (
      <img src={group.image} alt={group.title} className="h-full w-full object-cover" loading="lazy" />
    ) : (
      <span className="text-lg font-black text-slate-500">{group.title.slice(0, 1)}</span>
    )}
  </div>
);

// --- Category rail (live categoryRails: most-product categories) ---
export const CategoryRail: React.FC<{
  products: Product[];
  categories?: Category[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, categories, config, accentColor }) => {
  const custom = templateCustom(config);
  const groups = useMemo(
    () => groupProductsByCategory(products, categories, custom.categoryOverrides),
    [products, categories, config],
  );
  if (!sectionEnabled(config, 'categoryRail') || groups.length === 0) return null;
  const maxRails = customCount(config, 'categoryRailsMax', 3);
  const perRail = customCount(config, 'categoryRailsCount', 8);
  const rails = groups.slice(0, maxRails);
  const title = customText(config, 'categoriesTitle', 'دسته‌بندی‌ها'); // live Digikala + Electronic render default
  return (
    <section data-testid="store-category-rail" className="space-y-5">
      <SectionTitle accentColor={accentColor}>{title}</SectionTitle>
      {rails.map((group) => (
        <div key={group.slug} className="group space-y-3">
          <div className="flex items-center gap-3 px-1">
            <CategoryThumb group={group} />
            <div>
              <p className="text-sm font-black text-slate-900">{group.title}</p>
              <p className="mt-0.5 text-xs text-slate-500">{toPersianDigits(group.count)} کالا</p>
            </div>
          </div>
          <div className="flex gap-3 overflow-x-auto pb-1.5 no-scrollbar">
            {group.products.slice(0, perRail).map((p) => (
              <div key={p.id} className="w-[160px] shrink-0 lg:w-[180px]">
                <ProductCard product={p} />
              </div>
            ))}
          </div>
        </div>
      ))}
    </section>
  );
};

// --- Category cards grid (Fashion's categories block: no rails) ----
export const CategoryCards: React.FC<{
  products: Product[];
  categories?: Category[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, categories, config, accentColor }) => {
  const custom = templateCustom(config);
  const groups = useMemo(
    () => groupProductsByCategory(products, categories, custom.categoryOverrides),
    [products, categories, config],
  );
  if (!sectionEnabled(config, 'categoryRail') || groups.length === 0) return null;
  const limit = customCount(config, 'categoriesCount', 8);
  const title = customText(config, 'categoriesTitle', 'دسته‌بندی‌ها');
  return (
    <section data-testid="store-category-cards" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{title}</SectionTitle>
      <div className="grid grid-cols-4 gap-3 lg:grid-cols-8">
        {groups.slice(0, limit).map((group) => (
          <div key={group.slug} className="group flex flex-col items-center gap-2 text-center">
            <CategoryThumb group={group} />
            <span className="w-full truncate text-xs font-extrabold text-slate-800">{group.title}</span>
            <span className="text-[11px] text-slate-400">{toPersianDigits(group.count)} کالا</span>
          </div>
        ))}
      </div>
    </section>
  );
};

// --- Banners (live banners: seller-configured, self-hide) ----------
export const BannerRow: React.FC<{ config: TemplateConfig }> = ({ config }) => {
  const banners = bannersOf(config);
  if (!sectionEnabled(config, 'banners') || banners.length === 0) return null;
  return (
    <section data-testid="store-banners" className="grid grid-cols-2 gap-3">
      {banners.map((b, i) => {
        const tile = (
          <div className="group relative overflow-hidden rounded-3xl shadow-sm ring-1 ring-slate-900/5">
            <img src={b.image} alt={b.title || ''} className="h-32 w-full object-cover transition duration-500 group-hover:scale-105 lg:h-44" loading="lazy" />
            {b.title ? (
              <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 via-black/25 to-transparent p-3 pt-10">
                <p className="text-sm font-black text-white drop-shadow">{b.title}</p>
              </div>
            ) : null}
          </div>
        );
        return (
          <div key={`${b.image}-${i}`}>
            {b.link ? (
              <a href={b.link} target="_blank" rel="noreferrer" className="block">
                {tile}
              </a>
            ) : (
              tile
            )}
          </div>
        );
      })}
    </section>
  );
};

// --- Deals (live deals: discounted products of this store) ---------
export function discountedProducts(products: Product[]): Product[] {
  return products
    .filter((p) => p.discountPercent > 0 || p.specialDeal)
    .sort((a, b) => b.discountPercent - a.discountPercent);
}

export const DealsSection: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, config, accentColor }) => {
  if (!sectionEnabled(config, 'deals')) return null;
  const deals = discountedProducts(products).slice(0, customCount(config, 'dealsCount', 10));
  if (deals.length === 0) return null;
  const title = customText(config, 'dealsTitle', 'پیشنهاد شگفت‌انگیز'); // live Digikala + Electronic render default
  return (
    <section data-testid="store-deals" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{title}</SectionTitle>
      <TemplateProductGrid products={deals} columnsClass="grid-cols-2 lg:grid-cols-4" />
    </section>
  );
};

// --- Timed deals (live timedDeals: public deals widget + countdown) -
export const TimedDealsSection: React.FC<{
  store: Store;
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ store, products, config, accentColor }) => {
  const widget = store.sellerDealsWidgetConfig;
  const endsAt = widget?.endsAt ? Date.parse(widget.endsAt) : NaN;
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (Number.isNaN(endsAt)) return undefined;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [endsAt]);
  if (!sectionEnabled(config, 'timedDeals') || !widget) return null;
  const items = products.filter((p) => (widget.productIds || []).includes(p.id));
  if (items.length === 0) return null;
  // An expired timed offer hides itself (live countdown behaviour).
  if (!Number.isNaN(endsAt) && endsAt <= now) return null;
  let countdown: string | null = null;
  if (!Number.isNaN(endsAt)) {
    const total = Math.max(0, Math.floor((endsAt - now) / 1000));
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    const s = total % 60;
    countdown = `${toPersianDigits(String(h).padStart(2, '0'))}:${toPersianDigits(String(m).padStart(2, '0'))}:${toPersianDigits(String(s).padStart(2, '0'))}`;
  }
  return (
    <section data-testid="store-timed-deals" className="space-y-4 rounded-3xl border border-slate-200/60 bg-gradient-to-b from-slate-50 to-white p-4 shadow-sm">
      <div className="flex items-center justify-between">
        <SectionTitle accentColor={accentColor}>{widget.title || 'پیشنهاد زمان‌دار'}</SectionTitle>
        {countdown ? (
          <span className="rounded-full bg-slate-900 px-2.5 py-1 text-[11px] font-black text-white shadow-sm" dir="ltr">
            {countdown}
          </span>
        ) : null}
      </div>
      <TemplateProductGrid products={items} columnsClass="grid-cols-2 lg:grid-cols-4" />
    </section>
  );
};

// --- Brands rail (live brands: distinct suppliers of the store) ----
export const BrandsRail: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, config, accentColor }) => {
  if (!sectionEnabled(config, 'brands')) return null;
  const brands: string[] = [];
  products.forEach((p) => {
    const name = (p.supplierName || '').trim();
    if (name && !brands.includes(name)) brands.push(name);
  });
  if (brands.length === 0) return null;
  return (
    <section data-testid="store-brands" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{customText(config, 'brandsTitle', 'برندها')}</SectionTitle>
      <div className="flex gap-3 overflow-x-auto pb-1.5 no-scrollbar">
        {brands.map((brand) => (
          <div
            key={brand}
            className="flex shrink-0 items-center gap-2.5 rounded-2xl border border-slate-200/60 bg-white px-3.5 py-2.5 shadow-sm transition duration-300 hover:-translate-y-0.5 hover:shadow-md"
          >
            <span className="flex h-8 w-8 items-center justify-center rounded-xl bg-slate-100 text-xs font-black text-slate-600 ring-1 ring-slate-900/5">
              {brand.slice(0, 2)}
            </span>
            <span className="text-xs font-bold text-slate-800">{brand}</span>
          </div>
        ))}
      </div>
    </section>
  );
};

// --- Featured / bestsellers -----------------------------------------
export const FeaturedSection: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, config, accentColor }) => {
  if (!sectionEnabled(config, 'featured')) return null;
  const featured = products.filter((p) => p.featured).slice(0, customCount(config, 'featuredCount', 10));
  if (featured.length === 0) return null;
  const title = customText(config, 'featuredTitle', 'کالاهای منتخب');
  return (
    <section data-testid="store-featured" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{title}</SectionTitle>
      <TemplateProductGrid products={featured} columnsClass="grid-cols-2 lg:grid-cols-4" />
    </section>
  );
};

export const BestsellersSection: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, config, accentColor }) => {
  if (!sectionEnabled(config, 'bestsellers')) return null;
  const best = [...products]
    .sort((a, b) => (b.salesCount || 0) - (a.salesCount || 0))
    .slice(0, customCount(config, 'bestsellersCount', 10));
  if (best.length === 0 || best.every((p) => (p.salesCount || 0) <= 0)) return null;
  const title = customText(config, 'bestSellersTitle', 'پرفروش‌ترین‌ها');
  return (
    <section data-testid="store-bestsellers" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{title}</SectionTitle>
      <div className="flex gap-3 overflow-x-auto pb-1.5 no-scrollbar">
        {best.map((p) => (
          <div key={p.id} className="w-[160px] shrink-0 lg:w-[180px]">
            <ProductCard product={p} />
          </div>
        ))}
      </div>
    </section>
  );
};

export { heroSlidesOf };

// --- FIX 14: missing Modern sections (live Modern order) ---------------
export const TrustStripSection: React.FC<{
  config: TemplateConfig;
  accentColor: string;
}> = ({ config, accentColor }) => {
  // Seller-configured trust items win: when the template config carries
  // its own trustItems, the platform-wide siteContent fetch is skipped.
  const configItems = (templateCustom(config).trustItems || []).filter(
    (t) => t && typeof t.title === 'string' && t.title.trim() !== '',
  );
  const [fetched, setFetched] = useState<{ icon: string; title: string; subtitle: string }[]>([]);
  useEffect(() => {
    if (configItems.length > 0) return undefined;
    let on = true;
    siteContentApi.getTrustFeatures().then((list) => { if (on) setFetched(list.slice(0, 4).map((f) => ({ icon: f.icon, title: f.title, subtitle: f.description }))); }).catch(() => {});
    return () => { on = false; };
  }, [config]);
  const items: { title: string; subtitle: string }[] =
    configItems.length > 0
      ? configItems.slice(0, 4).map((t) => ({ title: t.title, subtitle: t.subtitle || '' }))
      : fetched;
  if (!sectionEnabled(config, 'trustStrip') || items.length === 0) return null;
  return (
    <section data-testid="store-trust-strip" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {items.map((f) => (
        <div key={f.title} className="rounded-3xl border border-slate-200/60 bg-white p-4 shadow-sm">
          <span className="mb-2.5 flex h-9 w-9 items-center justify-center rounded-xl text-sm font-black text-white shadow-sm" style={{ backgroundColor: accentColor }}>
            {f.title.slice(0, 1)}
          </span>
          <p className="text-[13px] font-black text-slate-900">{f.title}</p>
          {f.subtitle ? <p className="text-[11px] text-slate-500 leading-6 mt-0.5">{f.subtitle}</p> : null}
        </div>
      ))}
    </section>
  );
};

export const DiscountedSection: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, config, accentColor }) => {
  if (!sectionEnabled(config, 'discounted')) return null;
  const items = discountedProducts(products).slice(0, customCount(config, 'discountedCount', 10));
  if (items.length === 0) return null;
  return (
    <section data-testid="store-discounted" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{customText(config, 'discountedTitle', 'کالاهای تخفیف‌دار')}</SectionTitle>
      <TemplateProductGrid products={items} columnsClass="grid-cols-2 lg:grid-cols-4" />
    </section>
  );
};

export const AboutSection: React.FC<{
  store: Store;
  config: TemplateConfig;
  accentColor: string;
}> = ({ store, config, accentColor }) => {
  if (!sectionEnabled(config, 'about')) return null;
  const custom = templateCustom(config);
  const customAbout = (custom.aboutText || '').trim();
  const text = customAbout || (store.description || store.shortDescription || store.tagline || '').trim();
  if (!text) return null;
  // aboutImage is read defensively: the key is part of the editable
  // content contract and may land in TemplateCustom separately.
  const aboutImage = ((custom as TemplateCustom & { aboutImage?: string }).aboutImage || '').trim();
  return (
    <section data-testid="store-about" className="space-y-3">
      <SectionTitle accentColor={accentColor}>{customText(config, 'aboutTitle', 'درباره فروشگاه')}</SectionTitle>
      {aboutImage ? <img src={aboutImage} alt="" className="h-48 w-full rounded-3xl object-cover shadow-sm lg:h-56" loading="lazy" /> : null}
      <p className="text-[13px] leading-7 text-slate-500">{text}</p>
    </section>
  );
};

export const TopRatedSection: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, config, accentColor }) => {
  if (!sectionEnabled(config, 'topRated')) return null;
  const items = [...products]
    .filter((p) => (p.rating || 0) > 0)
    .sort((a, b) => (b.rating || 0) - (a.rating || 0))
    .slice(0, customCount(config, 'topRatedCount', 10));
  if (items.length === 0) return null;
  return (
    <section data-testid="store-top-rated" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{customText(config, 'topRatedTitle', 'پرامتیازترین‌ها')}</SectionTitle>
      <TemplateProductGrid products={items} columnsClass="grid-cols-2 lg:grid-cols-4" />
    </section>
  );
};

export const CampaignSection: React.FC<{
  store: Store;
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ store, products, config, accentColor }) => {
  if (!sectionEnabled(config, 'campaign')) return null;
  const widget = store.sellerDealsWidgetConfig;
  if (!widget) return null;
  const items = products.filter((p) => (widget.productIds || []).includes(p.id));
  if (items.length === 0) return null;
  return (
    <section data-testid="store-campaign" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{widget.title || 'پیشنهاد ویژه'}</SectionTitle>
      <TemplateProductGrid products={items} columnsClass="grid-cols-2 lg:grid-cols-4" />
    </section>
  );
};

export const NewsletterSection: React.FC<{
  config: TemplateConfig;
  accentColor: string;
  // ISO-E: the store this form belongs to. Without it the subscribe
  // POST carried no store identity at all, so a subscription entered
  // on store B could not be attributed to B (the unsubscribe endpoint
  // is store-aware: mobile+storeId). The success copy promises
  // "this store's" notifications, so the request must say which store.
  storeSlug?: string;
}> = ({ config, accentColor, storeSlug }) => {
  const [mobile, setMobile] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => { setMobile(''); setDone(false); setError(''); }, [storeSlug]);
  if (!sectionEnabled(config, 'newsletter')) return null;
  const newsletterSubtitle = (templateCustom(config).newsletterSubtitle || '').trim();
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!/^09\d{9}$/.test(mobile.trim())) { setError('شماره موبایل معتبر وارد کنید.'); return; }
    setError('');
    try {
      await apiRequest('/newsletter/subscribe', { method: 'POST', body: JSON.stringify(storeSlug ? { mobile: mobile.trim(), storeSlug } : { mobile: mobile.trim() }) });
      setDone(true);
    } catch {
      setError('ثبت انجام نشد؛ دوباره تلاش کنید.');
    }
  };
  return (
    <section data-testid="store-newsletter" className="space-y-3 rounded-3xl border border-slate-200/60 bg-slate-50 p-5 shadow-sm">
      <SectionTitle accentColor={accentColor}>{customText(config, 'newsletterTitle', 'عضویت در اطلاع‌رسانی')}</SectionTitle>
      {newsletterSubtitle ? <p className="text-xs leading-6 text-slate-500">{newsletterSubtitle}</p> : null}
      {done ? (
        // ISO-A: POST /newsletter/subscribe carries no store slug, so
        // the old text ("registered for THIS store's notifications")
        // claimed a per-store subscription the backend never records.
        <p className="text-emerald-600 font-bold text-xs">شماره شما ثبت شد.</p>
      ) : (
        <form onSubmit={submit} className="flex gap-2">
          <input
            type="tel"
            dir="ltr"
            value={mobile}
            onChange={(e) => setMobile(e.target.value)}
            placeholder="09xxxxxxxxx"
            className="flex-1 min-w-0 rounded-2xl border border-slate-200 bg-white px-3 py-2.5 text-xs text-left shadow-sm outline-hidden focus:border-brand"
          />
          <button type="submit" className="rounded-2xl px-5 py-2.5 text-xs font-bold text-white shadow-sm transition hover:opacity-90" style={{ backgroundColor: accentColor }}>
            عضویت
          </button>
        </form>
      )}
      {error !== '' && <p className="text-rose-500 font-bold text-[11px]">{error}</p>}
    </section>
  );
};

export const FaqSection: React.FC<{
  config: TemplateConfig;
  accentColor: string;
}> = ({ config, accentColor }) => {
  // Seller-configured FAQ items win: when the template config carries
  // its own faqItems, the platform-wide /static-pages fetch is skipped.
  const configItems = (templateCustom(config).faqItems || []).filter(
    (f) => f && typeof f.q === 'string' && f.q.trim() !== '' && typeof f.a === 'string' && f.a.trim() !== '',
  );
  const [faqs, setFaqs] = useState<{ q: string; a: string }[]>([]);
  useEffect(() => {
    if (configItems.length > 0) return undefined;
    let on = true;
    apiRequest<{ pages: { key: string; content: string }[] }>('/static-pages')
      .then((res) => {
        if (!on) return;
        const page = (res.pages || []).find((p) => p.key === 'faq');
        if (!page) return;
        try {
          const parsed = JSON.parse(page.content);
          if (Array.isArray(parsed)) setFaqs(parsed.filter((x) => x && x.q && x.a));
        } catch { /* non-JSON faq content renders nothing */ }
      })
      .catch(() => {});
    return () => { on = false; };
  }, [config]);
  const items = configItems.length > 0 ? configItems : faqs;
  if (!sectionEnabled(config, 'faq') || items.length === 0) return null;
  return (
    <section data-testid="store-faq" className="space-y-3">
      <SectionTitle accentColor={accentColor}>پرسش‌های متداول</SectionTitle>
      {items.map((f) => (
        <div key={f.q} className="rounded-3xl border border-slate-200/60 bg-white p-4 shadow-sm">
          <p className="text-[13px] font-black text-slate-900">{f.q}</p>
          <p className="text-xs text-slate-500 leading-6 mt-1.5">{f.a}</p>
        </div>
      ))}
    </section>
  );
};
// --- Testimonials (seller-configured customer quotes) ---------------
export const TestimonialsSection: React.FC<{
  config: TemplateConfig;
  accentColor: string;
}> = ({ config, accentColor }) => {
  if (!sectionEnabled(config, 'testimonials')) return null;
  const items = (templateCustom(config).testimonials || []).filter(
    (t) => t && typeof t.text === 'string' && t.text.trim() !== '',
  );
  if (items.length === 0) return null;
  return (
    <section data-testid="store-testimonials" className="space-y-4">
      <SectionTitle accentColor={accentColor}>نظرات مشتریان</SectionTitle>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        {items.map((t, i) => (
          <figure key={`${t.name || ''}-${i}`} className="rounded-3xl border border-slate-200/60 bg-white p-5 shadow-sm">
            <span className="text-4xl font-black leading-none" style={{ color: accentColor }}>“</span>
            <blockquote className="mt-2 text-[13px] leading-7 text-slate-600">{t.text}</blockquote>
            {t.name ? (
              <figcaption className="mt-3 flex items-center gap-2">
                <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs font-black text-white" style={{ backgroundColor: accentColor }}>
                  {t.name.slice(0, 1)}
                </span>
                <span className="text-xs font-bold text-slate-900">{t.name}</span>
              </figcaption>
            ) : null}
          </figure>
        ))}
      </div>
    </section>
  );
};

// --- Lookbook (wide collection banner with overlaid title) ----------
export const LookbookSection: React.FC<{
  config: TemplateConfig;
  accentColor: string;
}> = ({ config, accentColor }) => {
  if (!sectionEnabled(config, 'lookbook')) return null;
  const custom = templateCustom(config);
  const image = (custom.lookbookImage || '').trim();
  const title = (custom.lookbookTitle || '').trim();
  const subtitle = (custom.lookbookSubtitle || '').trim();
  if (!image && !title) return null;
  const link = (custom.lookbookLink || '').trim();
  const body = (
    <div className="relative overflow-hidden rounded-3xl bg-slate-100 shadow-lg shadow-slate-900/10 ring-1 ring-black/5">
      {image ? (
        <img src={image} alt={title} className="h-64 w-full object-cover lg:h-80" loading="lazy" />
      ) : (
        <div className="flex h-64 w-full items-center justify-center lg:h-80" style={{ backgroundColor: accentColor }}>
          <span className="px-4 text-center text-xl font-black text-white lg:text-2xl">{title}</span>
        </div>
      )}
      {image && (title || subtitle) ? (
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 via-black/30 to-transparent p-5 pt-16 text-right">
          {title ? <p className="text-2xl font-black leading-9 text-white drop-shadow-md">{title}</p> : null}
          {subtitle ? <p className="mt-1 text-sm leading-6 text-white/90">{subtitle}</p> : null}
        </div>
      ) : null}
    </div>
  );
  return (
    <section data-testid="store-lookbook">
      {link ? (
        <a href={link} target="_blank" rel="noreferrer" className="block">
          {body}
        </a>
      ) : (
        body
      )}
    </section>
  );
};

// --- Full products grid (custom-titled catalogue) -------------------
export const ProductsGridSection: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
  columnsClass?: string;
}> = ({ products, config, accentColor, columnsClass }) => {
  if (!sectionEnabled(config, 'products') && !sectionEnabled(config, 'allProducts')) return null;
  if (products.length === 0) return null;
  return (
    <section data-testid="store-products" className="space-y-4">
      <SectionTitle accentColor={accentColor}>{customText(config, 'productsTitle', 'همه کالاهای فروشگاه')}</SectionTitle>
      <TemplateProductGrid products={products} columnsClass={columnsClass || 'grid-cols-2 lg:grid-cols-4'} />
    </section>
  );
};
