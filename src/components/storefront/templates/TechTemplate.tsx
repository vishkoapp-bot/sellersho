import React from 'react';
import type { StorefrontTemplateProps, TemplateConfig } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  heroSlidesOf,
  templateCustom,
  customText,
  storefrontProducts,
} from './templateTypes';
import {
  AnnouncementSection,
  HeroSlider,
  CategoryRail,
  BannerRow,
  DealsSection,
  TimedDealsSection,
  BrandsRail,
  FeaturedSection,
  BestsellersSection,
  TrustStripSection,
  DiscountedSection,
  AboutSection,
  TopRatedSection,
  CampaignSection,
  NewsletterSection,
  FaqSection,
  TestimonialsSection,
  LookbookSection,
  ProductsGridSection,
  groupProductsByCategory,
} from './StoreSections';
import type { CategoryGroup } from './StoreSections';
import { toPersianDigits } from '../../../utils/formatters';
import { Star, Package, LayoutGrid, BadgeCheck, ChevronLeft } from 'lucide-react';
import type { Product } from '../../../types';

// Config-driven storefront: sections render in the order and
// enablement stored in the resolved template config (template
// blueprint merged under the seller's saved config); every text,
// image and link comes from the seller-editable custom config or
// the store's own data — nothing is hardcoded per store.
//
// Tech identity: a clean, precise, SaaS-grade catalogue — slate
// surfaces, hairline borders, violet accent, real-data stat chips.
// Every number on this page (counts, ratings, brand tallies) is
// computed from the store's actual products; prices are only ever
// rendered by the shared ProductCard-based sections, never
// hand-formatted here.

const toFa = (v: string | number): string => toPersianDigits(v);

// Five-star display from a REAL rating value only.
const Stars: React.FC<{ rating: number; className?: string }> = ({ rating, className }) => {
  const filled = Math.max(0, Math.min(5, Math.round(rating)));
  return (
    <span className={`inline-flex items-center gap-0.5 ${className || ''}`} aria-label={`امتیاز ${toFa(rating)} از ۵`}>
      {[1, 2, 3, 4, 5].map((i) => (
        <Star
          key={i}
          className={`h-3.5 w-3.5 ${i <= filled ? 'fill-amber-400 text-amber-400' : 'fill-slate-200 text-slate-200'}`}
        />
      ))}
    </span>
  );
};

// White content panel with a hairline slate border — the frame the
// shared sections sit in on this template.
const Panel: React.FC<{ children: React.ReactNode; className?: string }> = ({ children, className }) => (
  <div className={`rounded-2xl border border-slate-200 bg-white p-4 shadow-[0_1px_2px_rgba(15,23,42,0.04)] lg:p-5 ${className || ''}`}>
    {children}
  </div>
);

// Precise category tiles: real category image (falling back to the
// category's first product image), name and live product count.
const CategoryTiles: React.FC<{
  groups: CategoryGroup[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ groups, config, accentColor }) => {
  if (groups.length === 0) return null;
  return (
    <section data-testid="store-category-tiles" className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="h-4 w-1 rounded-full" style={{ backgroundColor: accentColor }} />
        <h3 className="text-sm font-black text-slate-900">{customText(config, 'categoriesTitle', 'دسته‌بندی‌ها')}</h3>
      </div>
      <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-3">
        {groups.slice(0, 6).map((g) => {
          const img = g.image || g.products[0]?.image || '';
          return (
            <a
              key={g.slug}
              href="#products"
              className="group flex items-center gap-3 rounded-xl border border-slate-200 bg-white p-3 transition hover:border-[var(--tile-accent)] hover:shadow-sm"
              style={{ ['--tile-accent' as string]: accentColor } as React.CSSProperties}
            >
              <span className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-slate-100 bg-slate-50">
                {img ? (
                  <img src={img} alt={g.title} className="h-full w-full object-cover" loading="lazy" />
                ) : (
                  <LayoutGrid className="h-5 w-5 text-slate-300" />
                )}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-black text-slate-900">{g.title}</span>
                <span className="mt-0.5 block text-[11px] font-bold text-slate-400">{toFa(g.count)} کالا</span>
              </span>
              <ChevronLeft className="h-4 w-4 shrink-0 text-slate-300 transition group-hover:text-slate-500" />
            </a>
          );
        })}
      </div>
    </section>
  );
};

// Brand logo-wall: supplier names as precise text-logos in bordered
// cells, each with its real product count in this store.
const BrandWall: React.FC<{
  brands: { name: string; count: number }[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ brands, config, accentColor }) => {
  if (brands.length === 0) return null;
  return (
    <section data-testid="store-brand-wall" className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="h-4 w-1 rounded-full" style={{ backgroundColor: accentColor }} />
        <h3 className="text-sm font-black text-slate-900">{customText(config, 'brandsTitle', 'برندها')}</h3>
      </div>
      <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-4">
        {brands.map(({ name, count }) => (
          <div key={name} className="flex flex-col items-center justify-center gap-1 rounded-xl border border-slate-200 bg-white px-3 py-4 text-center transition hover:border-slate-300 hover:shadow-sm">
            <span className="max-w-full truncate text-sm font-black tracking-tight text-slate-700">{name}</span>
            <span className="text-[10px] font-bold text-slate-400">{toFa(count)} کالا در این فروشگاه</span>
          </div>
        ))}
      </div>
    </section>
  );
};

// Rating summary strip for the top-rated block: honest aggregate
// math over products that actually carry a rating.
const RatingSummary: React.FC<{ products: Product[] }> = ({ products }) => {
  const rated = products.filter((p) => (p.rating || 0) > 0);
  if (rated.length < 3) return null;
  const avg = rated.reduce((sum, p) => sum + (p.rating || 0), 0) / rated.length;
  const reviews = rated.reduce((sum, p) => sum + (p.reviewsCount || 0), 0);
  return (
    <div className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-amber-100 bg-amber-50/70 px-4 py-3">
      <span className="text-2xl font-black leading-none text-slate-900">{toFa(avg.toFixed(1))}</span>
      <span className="flex flex-col gap-1">
        <Stars rating={avg} />
        <span className="text-[11px] font-bold text-slate-500">
          میانگین امتیاز {toFa(rated.length)} کالای دارای امتیاز
          {reviews > 0 ? ` — بر پایهٔ ${toFa(reviews)} دیدگاه` : ''}
        </span>
      </span>
    </div>
  );
};

export const TechTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const cfg = resolveTemplateConfig('tpl-tech-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const slides = heroSlidesOf(cfg);
  const custom = templateCustom(cfg);
  const seen = new Set<string>();
  const once = (group: string): boolean => {
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  };

  const heroTitle = customText(cfg, 'heroTitle', store.name);
  const heroSubtitle = customText(cfg, 'heroSubtitle', store.description || store.shortDescription || store.tagline || '');
  const heroImage = (custom.heroImage || '').trim();
  const brandCount = new Set(items.map((p) => (p.brand || '').trim()).filter(Boolean)).size;
  const categoryCount = categories
    ? categories.length
    : new Set(items.map((p) => p.category).filter(Boolean)).size;
  const ratedItems = items.filter((p) => (p.rating || 0) > 0);
  const avgRating = ratedItems.length > 0
    ? ratedItems.reduce((sum, p) => sum + (p.rating || 0), 0) / ratedItems.length
    : 0;
  const hasProductsSection = order.includes('products') || order.includes('allProducts');
  const categoryGroups = groupProductsByCategory(items, categories, custom.categoryOverrides);
  const brandTallies = (() => {
    const counts = new Map<string, number>();
    items.forEach((p) => {
      const name = (p.brand || '').trim();
      if (name) counts.set(name, (counts.get(name) || 0) + 1);
    });
    return [...counts.entries()]
      .map(([name, count]) => ({ name, count }))
      .sort((a, b) => b.count - a.count);
  })();

  const statChips: { icon: React.ReactNode; value: string; label: string }[] = [
    ...(items.length > 0 ? [{ icon: <Package className="h-4 w-4 text-slate-400" />, value: toFa(items.length), label: 'کالا' }] : []),
    ...(categoryCount > 0 ? [{ icon: <LayoutGrid className="h-4 w-4 text-slate-400" />, value: toFa(categoryCount), label: 'دسته‌بندی' }] : []),
    ...(brandCount > 0 ? [{ icon: <BadgeCheck className="h-4 w-4 text-slate-400" />, value: toFa(brandCount), label: 'برند' }] : []),
    ...(avgRating > 0 ? [{ icon: <Star className="h-4 w-4 fill-amber-400 text-amber-400" />, value: toFa(avgRating.toFixed(1)), label: 'میانگین امتیاز' }] : []),
  ];

  const renderHero = (): React.ReactNode => {
    if (slides.length > 0) {
      return <HeroSlider slides={slides} accentColor={accentColor} autoplay={custom.heroAutoplay} />;
    }
    return (
      <section className="relative overflow-hidden rounded-3xl bg-slate-950 text-white">
        {/* subtle blueprint grid-lines over the slate panel */}
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0"
          style={{
            backgroundImage:
              'linear-gradient(to left, rgba(148,163,184,0.09) 1px, transparent 1px), linear-gradient(to bottom, rgba(148,163,184,0.09) 1px, transparent 1px)',
            backgroundSize: '28px 28px',
          }}
        />
        <div
          aria-hidden="true"
          className="pointer-events-none absolute -top-24 left-0 h-64 w-64 rounded-full opacity-25 blur-3xl"
          style={{ backgroundColor: accentColor }}
        />
        <div className={`relative grid gap-6 p-6 lg:p-10 ${heroImage ? 'lg:grid-cols-[1fr_340px] lg:items-center' : ''}`}>
          <div>
            <span className="inline-flex items-center gap-1.5 rounded-full border border-white/15 bg-white/5 px-3 py-1 text-[11px] font-bold text-slate-300">
              <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: accentColor }} />
              {store.name}
            </span>
            <h2 className="mt-4 text-2xl font-black leading-snug lg:text-4xl lg:leading-tight">{heroTitle}</h2>
            {heroSubtitle ? <p className="mt-3 max-w-2xl text-sm leading-8 text-slate-300">{heroSubtitle}</p> : null}
            {(custom.heroCtaText || hasProductsSection) ? (
              <div className="mt-5 flex flex-wrap items-center gap-2">
                {custom.heroCtaText && custom.heroCtaLink ? (
                  <a
                    href={custom.heroCtaLink}
                    className="rounded-xl px-5 py-2.5 text-sm font-black text-white transition hover:opacity-90"
                    style={{ backgroundColor: accentColor }}
                  >
                    {custom.heroCtaText}
                  </a>
                ) : null}
                {hasProductsSection && items.length > 0 ? (
                  <a href="#products" className="rounded-xl border border-white/20 bg-white/5 px-5 py-2.5 text-sm font-black text-white transition hover:bg-white/10">
                    مشاهده کاتالوگ
                  </a>
                ) : null}
              </div>
            ) : null}
            {statChips.length > 0 ? (
              <div className="mt-6 flex flex-wrap gap-2">
                {statChips.map((chip) => (
                  <span key={chip.label} className="flex items-center gap-2.5 rounded-xl border border-white/10 bg-white/5 px-3 py-2">
                    {chip.icon}
                    <span className="flex flex-col">
                      <span className="text-sm font-black leading-4">{chip.value}</span>
                      <span className="text-[10px] font-bold text-slate-400">{chip.label}</span>
                    </span>
                  </span>
                ))}
              </div>
            ) : null}
          </div>
          {heroImage ? (
            <img
              src={heroImage}
              alt={heroTitle}
              className="hidden h-56 w-full rounded-2xl border border-white/10 object-cover lg:block lg:h-64"
              loading="lazy"
            />
          ) : null}
        </div>
      </section>
    );
  };

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return <AnnouncementSection store={store} config={cfg} accentColor={accentColor} />;
      case 'hero':
      case 'heroSlides':
        return once('hero') ? renderHero() : null;
      case 'trustStrip': {
        // Panel only when the seller configured trust items; the
        // platform-fetched fallback renders bare so an empty fetch
        // never leaves an empty panel shell.
        const hasTrustItems = (custom.trustItems || []).some(
          (t) => t && typeof t.title === 'string' && t.title.trim() !== '',
        );
        return hasTrustItems ? (
          <Panel>
            <TrustStripSection config={cfg} accentColor={accentColor} />
          </Panel>
        ) : (
          <TrustStripSection config={cfg} accentColor={accentColor} />
        );
      }
      case 'categoryRail':
        // Precise local tiles when categories carry products;
        // the shared rail remains the fallback presentation.
        return categoryGroups.length > 0 ? (
          <CategoryTiles groups={categoryGroups} config={cfg} accentColor={accentColor} />
        ) : (
          <CategoryRail products={items} categories={categories} config={cfg} accentColor={accentColor} />
        );
      case 'banners':
      case 'promo':
        return once('banners') ? <BannerRow config={cfg} /> : null;
      case 'deals':
        return <DealsSection products={items} config={cfg} accentColor={accentColor} />;
      case 'timedDeals':
        return <TimedDealsSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'brands':
        // Logo-wall for 2+ real brands; single-brand stores keep
        // the shared rail (which self-hides when empty).
        return brandTallies.length >= 2 ? (
          <BrandWall brands={brandTallies} config={cfg} accentColor={accentColor} />
        ) : (
          <BrandsRail products={items} config={cfg} accentColor={accentColor} />
        );
      case 'featured':
        // Panel approach for prices: the shared FeaturedSection
        // renders real ProductCards (store-resolved pricing) —
        // this template never formats a price itself. The panel is
        // gated on real featured products so it never renders empty.
        if (!items.some((p) => p.featured)) return null;
        return (
          <Panel>
            <FeaturedSection products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'bestsellers':
        return <BestsellersSection products={items} config={cfg} accentColor={accentColor} />;
      case 'discounted':
        return <DiscountedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'topRated':
        if (ratedItems.length === 0) return null;
        return (
          <Panel>
            <RatingSummary products={items} />
            <TopRatedSection products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'about': {
        const aboutText = (custom.aboutText || '').trim()
          || (store.description || store.shortDescription || store.tagline || '').trim();
        if (!aboutText) return null;
        return (
          <Panel>
            <AboutSection store={store} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      }
      case 'campaign':
        return <CampaignSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'newsletter':
        // NewsletterSection carries its own bordered card chrome;
        // rendering it bare avoids a doubled frame.
        return <NewsletterSection config={cfg} accentColor={accentColor} storeSlug={store.slug} />;
      case 'testimonials': {
        const hasTestimonials = (custom.testimonials || []).some(
          (t) => t && typeof t.text === 'string' && t.text.trim() !== '',
        );
        if (!hasTestimonials) return null;
        return (
          <Panel>
            <TestimonialsSection config={cfg} accentColor={accentColor} />
          </Panel>
        );
      }
      case 'faq': {
        // Panel only for seller-configured FAQ items; the platform
        // /static-pages fallback renders bare (same shell rule as
        // the trust strip).
        const hasFaqItems = (custom.faqItems || []).some(
          (f) => f && typeof f.q === 'string' && f.q.trim() !== '' && typeof f.a === 'string' && f.a.trim() !== '',
        );
        return hasFaqItems ? (
          <Panel>
            <FaqSection config={cfg} accentColor={accentColor} />
          </Panel>
        ) : (
          <FaqSection config={cfg} accentColor={accentColor} />
        );
      }
      case 'lookbook':
        return <LookbookSection config={cfg} accentColor={accentColor} />;
      case 'products':
      case 'allProducts':
        if (!once('products') || items.length === 0) return null;
        return (
          <div id="products" className="scroll-mt-4">
            <Panel className="p-3 lg:p-5">
              <ProductsGridSection products={items} config={cfg} accentColor={accentColor} columnsClass="grid-cols-2 lg:grid-cols-3 xl:grid-cols-4" />
            </Panel>
          </div>
        );
      default:
        return null;
    }
  };

  return (
    <div className="flex-1 flex flex-col bg-slate-50" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-3 py-2 text-[11px] text-slate-400">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      <div className="bg-slate-900 text-white">
        <div className="mx-auto flex w-full max-w-7xl items-center gap-3 px-4 py-4">
          {store.logo ? (
            <img src={store.logo} alt={store.name} className="h-11 w-11 shrink-0 rounded-xl border border-white/20 object-cover" loading="lazy" />
          ) : null}
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-black lg:text-xl">{store.name}</h1>
            {store.tagline ? <p className="mt-0.5 truncate text-xs text-slate-300">{store.tagline}</p> : null}
          </div>
          {items.length > 0 ? (
            <span className="hidden shrink-0 items-center gap-1.5 rounded-full border border-white/15 bg-white/5 px-3 py-1.5 text-[11px] font-bold text-slate-200 sm:inline-flex">
              <Package className="h-3.5 w-3.5" />
              {toFa(items.length)} کالا
            </span>
          ) : null}
        </div>
      </div>
      <div className="mx-auto w-full max-w-7xl space-y-8 p-4 lg:p-6">
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
export default TechTemplate;
