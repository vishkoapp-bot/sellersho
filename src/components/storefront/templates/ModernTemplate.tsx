import React from 'react';
import type { Product } from '../../../types';
import type { StorefrontTemplateProps, TemplateConfig } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  heroSlidesOf,
  templateCustom,
  customText,
  customCount,
  storefrontProducts,
} from './templateTypes';
import {
  AnnouncementSection,
  HeroSlider,
  CategoryCards,
  BannerRow,
  DealsSection,
  TimedDealsSection,
  BrandsRail,
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
import { TemplateProductGrid } from './TemplateProductGrid';
import { useStore } from '../../../context/StoreContext';
import { formatPrice, toPersianDigits } from '../../../utils/formatters';

// Config-driven storefront: sections render in the order and
// enablement stored in the resolved template config (template
// blueprint merged under the seller's saved config); every text,
// image and link comes from the seller-editable custom config or
// the store's own data — nothing is hardcoded per store.
//
// Modern identity: warm paper background, editorial split hero,
// image-led category mosaic and a large featured spotlight card —
// a calm, premium D2C boutique feel on top of the shared sections.

const productImage = (p: Product): string => {
  const fromField = (p.image || '').trim();
  if (fromField) return fromField;
  const first = Array.isArray(p.images)
    ? p.images.find((x) => typeof x === 'string' && x.trim() !== '')
    : undefined;
  return first || '';
};

// Local section header: small accent eyebrow (real, data-derived
// label), big black title and a thin accent rule — consistent
// across every Modern-local block.
const ModernSectionHeader: React.FC<{
  eyebrow?: string;
  title: string;
  accentColor: string;
}> = ({ eyebrow, title, accentColor }) => (
  <div>
    {eyebrow ? (
      <p className="flex items-center gap-2 text-[11px] font-black" style={{ color: accentColor }}>
        <span className="h-px w-6" style={{ backgroundColor: accentColor }} />
        {eyebrow}
      </p>
    ) : null}
    <h2 className="mt-1.5 text-xl font-black leading-snug text-slate-900 lg:text-2xl">{title}</h2>
    <div className="mt-2.5 h-[3px] w-10 rounded-full" style={{ backgroundColor: accentColor }} />
  </div>
);

// --- Category mosaic (image-led tiles; CategoryCards fallback) ----
const CategoryMosaic: React.FC<{
  products: Product[];
  categories?: StorefrontTemplateProps['categories'];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, categories, config, accentColor }) => {
  const custom = templateCustom(config);
  const groups = groupProductsByCategory(products, categories, custom.categoryOverrides);
  if (groups.length === 0) return null;
  const coverOf = (g: CategoryGroup): string => {
    const first = g.products.find((p) => productImage(p) !== '');
    return (first ? productImage(first) : '') || g.image || '';
  };
  // No imagery anywhere -> the shared cards grid is the honest
  // fallback (initial-letter thumbs instead of broken covers).
  if (!groups.some((g) => coverOf(g) !== '')) {
    return <CategoryCards products={products} categories={categories} config={config} accentColor={accentColor} />;
  }
  const shown = groups.slice(0, customCount(config, 'categoriesCount', 8));
  const title = customText(config, 'categoriesTitle', 'دسته‌بندی‌ها');
  return (
    <section data-testid="store-category-mosaic" className="space-y-4 lg:space-y-5">
      <ModernSectionHeader
        eyebrow={`${toPersianDigits(shown.length)} دسته‌بندی`}
        title={title}
        accentColor={accentColor}
      />
      <div className="grid grid-cols-2 gap-3 lg:auto-rows-[190px] lg:grid-cols-4">
        {shown.map((g, i) => {
          const cover = coverOf(g);
          const lead = i === 0;
          return (
            <div
              key={g.slug}
              className={`group relative overflow-hidden rounded-[1.75rem] bg-slate-100 ring-1 ring-slate-900/5 transition duration-300 hover:-translate-y-0.5 hover:shadow-lg ${
                lead ? 'col-span-2 h-48 lg:h-auto lg:row-span-2' : 'h-36 lg:h-auto'
              }`}
            >
              {cover ? (
                <img
                  src={cover}
                  alt={g.title}
                  loading="lazy"
                  className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
                />
              ) : (
                <div className={`absolute inset-0 flex items-center justify-center bg-gradient-to-br ${g.gradient}`}>
                  <span className="text-4xl font-black text-slate-400">{g.title.slice(0, 1)}</span>
                </div>
              )}
              <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 via-black/25 to-transparent p-4 pt-10">
                <p className={`font-black leading-6 text-white ${lead ? 'text-base lg:text-xl' : 'text-sm'}`}>{g.title}</p>
                <p className="mt-0.5 text-[11px] font-bold text-white/85">{toPersianDigits(g.count)} کالا</p>
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
};

// --- Featured spotlight (large lead card + grid of the rest) -------
const FeaturedSpotlight: React.FC<{
  products: Product[];
  config: TemplateConfig;
  accentColor: string;
}> = ({ products, config, accentColor }) => {
  const { openProductDetail } = useStore();
  const featured = products.filter((p) => p.featured).slice(0, customCount(config, 'featuredCount', 10));
  if (featured.length === 0) return null;
  const lead = featured[0];
  const rest = featured.slice(1);
  const title = customText(config, 'featuredTitle', 'کالاهای منتخب');
  const leadImage = productImage(lead);
  return (
    <section data-testid="store-featured" className="space-y-4 lg:space-y-5">
      <ModernSectionHeader
        eyebrow={`${toPersianDigits(featured.length)} کالای منتخب`}
        title={title}
        accentColor={accentColor}
      />
      <article
        onClick={() => openProductDetail(lead)}
        className="group grid cursor-pointer overflow-hidden rounded-[2rem] bg-white shadow-sm ring-1 ring-slate-900/5 transition duration-300 hover:-translate-y-0.5 hover:shadow-xl lg:grid-cols-2"
      >
        <div className="relative aspect-[4/3] overflow-hidden bg-slate-50 lg:aspect-auto lg:min-h-[320px]">
          {leadImage ? (
            <img
              src={leadImage}
              alt={lead.title}
              loading="lazy"
              className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
            />
          ) : (
            <div className="absolute inset-0 flex items-center justify-center bg-slate-100">
              <span className="px-6 text-center text-lg font-black text-slate-400">{lead.title}</span>
            </div>
          )}
          {lead.discountPercent > 0 ? (
            <span className="absolute right-4 top-4 rounded-full bg-[#fb2c2c] px-2.5 py-1 text-xs font-black text-white shadow">
              {toPersianDigits(lead.discountPercent)}٪ تخفیف
            </span>
          ) : null}
        </div>
        <div className="flex flex-col justify-center gap-3 p-5 lg:p-8">
          <span
            className="self-start rounded-full px-3 py-1 text-[11px] font-black"
            style={{ color: accentColor, backgroundColor: `${accentColor}14` }}
          >
            منتخب فروشگاه
          </span>
          <h3 className="line-clamp-2 text-lg font-black leading-8 text-slate-900 lg:text-2xl lg:leading-10">{lead.title}</h3>
          {lead.description ? (
            <p className="line-clamp-3 text-sm leading-7 text-slate-500">{lead.description}</p>
          ) : null}
          {lead.rating > 0 ? (
            <p className="flex items-center gap-1.5 text-xs font-bold text-slate-600">
              <span className="text-base leading-none text-amber-500">★</span>
              {toPersianDigits(lead.rating)}
              {lead.reviewsCount > 0 ? (
                <span className="font-normal text-slate-400">({toPersianDigits(lead.reviewsCount)} دیدگاه)</span>
              ) : null}
            </p>
          ) : null}
          <div className="mt-1 flex flex-wrap items-end justify-between gap-3 border-t border-dashed border-slate-200 pt-4">
            <div>
              {lead.discountPercent > 0 && lead.originalPrice > lead.price ? (
                <p className="text-xs text-slate-400 line-through">{formatPrice(lead.originalPrice)}</p>
              ) : null}
              <p className="text-xl font-black text-slate-900 lg:text-2xl">
                {formatPrice(lead.price)}{' '}
                <span className="text-xs font-bold text-slate-500">تومان</span>
              </p>
            </div>
            <span
              className="inline-flex min-h-[42px] items-center rounded-full px-5 py-2.5 text-xs font-black text-white transition group-hover:opacity-90"
              style={{ backgroundColor: accentColor }}
            >
              مشاهده و خرید
            </span>
          </div>
        </div>
      </article>
      {rest.length > 0 ? <TemplateProductGrid products={rest} columnsClass="grid-cols-2 lg:grid-cols-4" /> : null}
    </section>
  );
};

export const ModernTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const cfg = resolveTemplateConfig('tpl-modern-1', config);
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
  const heroCtaText = customText(cfg, 'heroCtaText', 'مشاهده کالاها');
  const heroCtaLink = custom.heroCtaLink || '#products';
  const heroImage = custom.heroImage || store.logo;
  const aboutInOrder = order.includes('about');

  const renderHero = (): React.ReactNode => {
    if (slides.length > 0) {
      return (
        <div className="[&>div>div]:rounded-[2rem] [&_img]:lg:h-80">
          <HeroSlider slides={slides} accentColor={accentColor} autoplay={custom.heroAutoplay} />
        </div>
      );
    }
    const collage = items
      .filter((p) => productImage(p) !== '')
      .slice(0, 4)
      .map((p) => ({ src: productImage(p), title: p.title }));
    const hasMedia = Boolean(heroImage) || collage.length > 0;
    return (
      <section className={`grid items-center gap-8 lg:gap-12 ${hasMedia ? 'lg:grid-cols-2' : ''}`}>
        <div className={hasMedia ? '' : 'max-w-2xl'}>
          {store.tagline ? (
            <p
              className="inline-flex items-center gap-2 rounded-full bg-white px-3.5 py-1.5 text-[11px] font-black ring-1 ring-slate-900/5"
              style={{ color: accentColor }}
            >
              <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: accentColor }} />
              {store.tagline}
            </p>
          ) : null}
          <h1 className="mt-4 text-3xl font-black leading-[1.3] text-slate-900 lg:mt-5 lg:text-5xl lg:leading-[1.25]">
            {heroTitle}
          </h1>
          {heroSubtitle ? (
            <p className="mt-4 max-w-xl text-sm leading-8 text-slate-500 lg:text-base lg:leading-9">{heroSubtitle}</p>
          ) : null}
          <div className="mt-6 flex flex-wrap items-center gap-3 lg:mt-8">
            <a
              href={heroCtaLink}
              className="inline-flex min-h-[44px] items-center rounded-full px-6 py-3 text-sm font-black text-white shadow-md transition duration-300 hover:-translate-y-0.5 hover:shadow-lg"
              style={{ backgroundColor: accentColor }}
            >
              {heroCtaText}
            </a>
            {aboutInOrder ? (
              <a
                href="#about"
                className="inline-flex min-h-[44px] items-center rounded-full bg-white px-6 py-3 text-sm font-black text-slate-800 ring-1 ring-slate-200 transition duration-300 hover:-translate-y-0.5 hover:shadow-md"
              >
                درباره فروشگاه
              </a>
            ) : null}
          </div>
        </div>
        {heroImage ? (
          <div className="relative">
            <div
              aria-hidden="true"
              className="absolute inset-0 -rotate-2 rounded-[2rem]"
              style={{ backgroundColor: `${accentColor}1f` }}
            />
            <img
              src={heroImage}
              alt={store.name}
              className="relative h-64 w-full rounded-[2rem] object-cover shadow-xl ring-1 ring-slate-900/10 sm:h-80 lg:h-[26rem]"
            />
          </div>
        ) : collage.length > 0 ? (
          <div className="grid grid-cols-2 gap-3">
            {collage.map((tile, i) => (
              <div
                key={`${tile.src}-${i}`}
                className={`group overflow-hidden rounded-[1.75rem] ring-1 ring-slate-900/5 ${
                  collage.length === 1 ? 'col-span-2 aspect-[2/1]' : 'aspect-square'
                } ${i % 2 === 1 ? 'lg:translate-y-6' : ''}`}
              >
                <img
                  src={tile.src}
                  alt={tile.title}
                  loading="lazy"
                  className="h-full w-full object-cover transition duration-500 group-hover:scale-105"
                />
              </div>
            ))}
          </div>
        ) : null}
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
      case 'trustStrip':
        return <TrustStripSection config={cfg} accentColor={accentColor} />;
      case 'categoryRail':
        return <CategoryMosaic products={items} categories={categories} config={cfg} accentColor={accentColor} />;
      case 'banners':
      case 'promo':
        return once('banners') ? <BannerRow config={cfg} /> : null;
      case 'deals':
        return <DealsSection products={items} config={cfg} accentColor={accentColor} />;
      case 'timedDeals':
        return <TimedDealsSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'brands':
        return <BrandsRail products={items} config={cfg} accentColor={accentColor} />;
      case 'featured':
        return <FeaturedSpotlight products={items} config={cfg} accentColor={accentColor} />;
      case 'bestsellers':
        return <BestsellersSection products={items} config={cfg} accentColor={accentColor} />;
      case 'discounted':
        return <DiscountedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'topRated':
        return <TopRatedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'about':
        return (
          <div id="about" className="scroll-mt-24">
            <AboutSection store={store} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'campaign':
        return <CampaignSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'newsletter':
        return <NewsletterSection config={cfg} accentColor={accentColor} storeSlug={store.slug} />;
      case 'testimonials':
        return <TestimonialsSection config={cfg} accentColor={accentColor} />;
      case 'faq':
        return <FaqSection config={cfg} accentColor={accentColor} />;
      case 'lookbook':
        return <LookbookSection config={cfg} accentColor={accentColor} />;
      case 'products':
      case 'allProducts':
        return once('products') ? (
          <div id="products" className="scroll-mt-24">
            <ProductsGridSection products={items} config={cfg} accentColor={accentColor} columnsClass="grid-cols-2 lg:grid-cols-4" />
          </div>
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="flex-1 flex flex-col bg-[#fdfcf9]" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-4 pt-4 text-[11px] text-slate-400 lg:px-6">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      <div className="mx-auto w-full max-w-6xl space-y-10 p-4 lg:space-y-14 lg:p-6">
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
