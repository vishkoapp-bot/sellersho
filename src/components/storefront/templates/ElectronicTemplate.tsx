import { StorefrontCatalogue } from './StorefrontCatalogue';
import { StorefrontHero } from './StorefrontHero';
import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  templateCustom,
  customText,
  bannersOf,
  sectionEnabled,
  storefrontProducts,
} from './templateTypes';
import type { TemplateConfig } from './templateTypes';
import type { Product } from '../../../types';
import {
  CategoryRail,
  CategoryCards,
  BannerRow,
  DealsSection,
  TimedDealsSection,
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
  SectionTitle,
  discountedProducts,
  groupProductsByCategory,
  StorefrontCategoryLink,
  StorefrontBrandLink,
} from './StoreSections';
import type { CategoryGroup } from './StoreSections';
import { toPersianDigits } from '../../../utils/formatters';

// Config-driven storefront: sections render in the order and
// enablement stored in the resolved template config (template
// blueprint merged under the seller's saved config); every text,
// image and link comes from the seller-editable custom config or
// the store's own data — nothing is hardcoded per store.
//
// Electronic identity: an energetic gadget retailer — bold orange
// gradient hero with a dot-grid pattern, a sticky category-chip
// rail under the hero, an accent-tinted deals panel, brand tiles
// and big promo tiles, all on a light slate canvas.

// --- Sticky category chips (top categories by product count) -------
const CategoryChips: React.FC<{ groups: CategoryGroup[]; accentColor: string }> = ({ groups, accentColor }) => {
  const top = groups.slice(0, 10);
  if (top.length < 2) return null;
  return (
    <div className="showcase-rail" aria-label="دسته‌بندی‌ها">
      <div className="flex snap-x gap-2 overflow-x-auto rounded-2xl border border-slate-200/70 bg-white/85 p-2 shadow-sm backdrop-blur no-scrollbar">
        {top.map((g) => (
          <StorefrontCategoryLink key={g.slug} slug={g.slug}
            className="flex shrink-0 snap-start items-center gap-2 rounded-xl px-3 py-2 text-xs font-bold text-slate-700 transition duration-300 hover:-translate-y-0.5 hover:shadow-sm"
            style={{ backgroundColor: `${accentColor}14` }}
          >
            <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: accentColor }} />
            <span className="whitespace-nowrap">{g.title}</span>
            <span className="text-[11px] font-black" style={{ color: accentColor }}>
              {toPersianDigits(g.count)}
            </span>
          </StorefrontCategoryLink>
        ))}
      </div>
    </div>
  );
};

// --- Brand tiles (same derivation as the shared BrandsRail:
// distinct real brand names carried by the store's own products) -----
const BrandTiles: React.FC<{ products: Product[]; config: TemplateConfig; accentColor: string }> = ({
  products,
  config,
  accentColor,
}) => {
  if (!sectionEnabled(config, 'brands')) return null;
  const counts = new Map<string, number>();
  products.forEach((p) => {
    const name = (p.brand || '').trim();
    if (name) counts.set(name, (counts.get(name) || 0) + 1);
  });
  const brands = Array.from(counts.entries());
  if (brands.length < 2) return null;
  return (
    <section data-testid="store-brands" className="space-y-3">
      <SectionTitle accentColor={accentColor}>{customText(config, 'brandsTitle', 'برندها')}</SectionTitle>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-6">
        {brands.map(([brand, count]) => (
          <StorefrontBrandLink brand={brand} key={brand}
            className="group rounded-2xl border border-slate-100 bg-white p-3 text-center shadow-2xs transition duration-300 hover:-translate-y-0.5 hover:shadow-md"
          >
            <span
              className="mx-auto flex h-11 w-11 items-center justify-center rounded-full text-sm font-black transition duration-300 group-hover:scale-110"
              style={{ backgroundColor: `${accentColor}1a`, color: accentColor }}
            >
              {brand.slice(0, 2)}
            </span>
            <p className="mt-2 truncate text-xs font-bold text-slate-800">{brand}</p>
            <p className="mt-0.5 text-[11px] text-slate-400">{toPersianDigits(count)} کالا</p>
          </StorefrontBrandLink>
        ))}
      </div>
    </section>
  );
};

// --- Dual promo tiles (config banners as big cover tiles) ----------
const PromoTiles: React.FC<{ config: TemplateConfig }> = ({ config }) => {
  const banners = bannersOf(config);
  if (banners.length < 2) return null;
  return (
    <section data-testid="store-banners" className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {banners.map((b, i) => {
        const inner = (
          <>
            <img
              src={b.image}
              alt={b.title || ''}
              className="h-40 w-full object-cover transition duration-500 group-hover:scale-105 lg:h-52"
              loading="lazy"
            />
            {b.title ? (
              <span className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 via-black/25 to-transparent p-4 pt-12 text-sm font-black text-white">
                {b.title}
              </span>
            ) : null}
          </>
        );
        const cls = 'group relative block overflow-hidden rounded-3xl shadow-sm transition duration-300 hover:shadow-lg';
        return b.link ? (
          <a key={`${b.image}-${i}`} href={b.link} {...(/^https?:\/\//i.test(b.link) ? { target: '_blank', rel: 'noreferrer' } : {})} className={cls}>
            {inner}
          </a>
        ) : (
          <div key={`${b.image}-${i}`} className={cls}>
            {inner}
          </div>
        );
      })}
    </section>
  );
};

export const ElectronicTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories, productFeed }) => {
  const cfg = resolveTemplateConfig('tpl-electronic-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const custom = templateCustom(cfg);
  const banners = bannersOf(cfg);
  const seen = new Set<string>();
  const once = (group: string): boolean => {
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  };

  const categoryGroups = groupProductsByCategory(items, categories, custom.categoryOverrides);
  const renderHero = () => <StorefrontHero store={store} config={cfg} products={items} />;

  const renderDeals = (): React.ReactNode => {
    const deals = discountedProducts(items);
    if (!sectionEnabled(cfg, 'deals') || deals.length === 0) {
      // Let the shared section self-hide exactly as before.
      return <DealsSection products={items} config={cfg} accentColor={accentColor} />;
    }
    return (
      <section className="rounded-3xl border border-orange-100 bg-orange-50 p-3 lg:p-4">
        <div className="mb-3 flex flex-wrap items-center gap-2 px-1">
          <span
            className="flex h-8 w-8 items-center justify-center rounded-xl text-base shadow-sm"
            style={{ background: `linear-gradient(135deg, ${accentColor}, #ea580c)` }}
          >
            ⚡
          </span>
          <h3 className="text-sm font-black text-slate-900 lg:text-base">
            {customText(cfg, 'dealsTitle', 'پیشنهاد شگفت‌انگیز')}
          </h3>
          <span
            className="rounded-full px-2.5 py-1 text-[11px] font-black text-white"
            style={{ backgroundColor: accentColor }}
          >
            پیشنهادهای امروز
          </span>
          <span className="ms-auto text-[11px] font-bold text-slate-500">
            {toPersianDigits(deals.length)} کالا با تخفیف
          </span>
        </div>
        {/* The shared section renders its own title row first; the
            panel header above replaces it visually. */}
        <div className="[&>section>div:first-child]:hidden">
          <DealsSection products={items} config={cfg} accentColor={accentColor} />
        </div>
      </section>
    );
  };

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return null;
      case 'hero':
      case 'heroSlides':
        return once('hero') ? <>{renderHero()}{order.includes('categoryRail') && <CategoryChips groups={categoryGroups} accentColor={accentColor} />}</> : null;
      case 'trustStrip':
        return (
          <div className="rounded-3xl border border-slate-100 bg-white p-3 shadow-sm empty:hidden lg:p-4">
            <TrustStripSection config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'categoryRail':
        return <CategoryRail products={items} categories={categories} config={cfg} accentColor={accentColor} />;
      case 'categoryCards':
        return <CategoryCards products={items} categories={categories} config={cfg} accentColor={accentColor} />;
      case 'banners':
      case 'promo':
        return once('banners') ? (
          banners.length >= 2 ? (
            <PromoTiles config={cfg} />
          ) : (
            <BannerRow config={cfg} />
          )
        ) : null;
      case 'deals':
        return renderDeals();
      case 'timedDeals':
        return <TimedDealsSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'brands':
        return <BrandTiles products={items} config={cfg} accentColor={accentColor} />;
      case 'featured':
        return <FeaturedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'bestsellers':
        return <BestsellersSection products={items} config={cfg} accentColor={accentColor} />;
      case 'discounted':
        return <DiscountedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'topRated':
        return <TopRatedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'about':
        return <AboutSection store={store} config={cfg} accentColor={accentColor} />;
      case 'campaign':
        return <CampaignSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'newsletter':
        return (
          <div className="rounded-3xl bg-white p-2 shadow-sm empty:hidden lg:p-3">
            <NewsletterSection config={cfg} accentColor={accentColor} storeSlug={store.slug} />
          </div>
        );
      case 'testimonials':
        return (
          <div className="rounded-3xl border border-slate-100 bg-white p-4 shadow-sm empty:hidden lg:p-5">
            <TestimonialsSection config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'faq':
        return (
          <div className="rounded-3xl border border-slate-100 bg-white p-4 shadow-sm empty:hidden lg:p-5">
            <FaqSection config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'lookbook':
        return <LookbookSection config={cfg} accentColor={accentColor} />;
      case 'products':
      case 'allProducts':
        return once('products') ? (
          <StorefrontCatalogue items={items} categoryGroups={categoryGroups}
            productsTitle={customText(cfg, 'productsTitle', 'همه کالاها')} productFeed={productFeed} />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <main className="fashion-storefront storefront-showcase" data-storefront-theme="tpl-electronic-1"
      style={{ '--tpl-accent': accentColor } as React.CSSProperties}>
      {!order.some(key => key === 'hero' || key === 'heroSlides') && <h1 className="sr-only">{store.name}</h1>}
      <div className="showcase-sections">
        {order.map(key => <React.Fragment key={key}>{renderSection(key)}</React.Fragment>)}
      </div>
    </main>
  );
};
export default ElectronicTemplate;
