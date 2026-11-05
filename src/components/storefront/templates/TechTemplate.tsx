import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
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
} from './StoreSections';
import { toPersianDigits } from '../../../utils/formatters';

// Config-driven storefront: sections render in the order and
// enablement stored in the resolved template config (template
// blueprint merged under the seller's saved config); every text,
// image and link comes from the seller-editable custom config or
// the store's own data — nothing is hardcoded per store.
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
  const brandCount = new Set(items.map((p) => (p.supplierName || '').trim()).filter(Boolean)).size;
  const categoryCount = categories
    ? categories.length
    : new Set(items.map((p) => p.category).filter(Boolean)).size;
  const stats = [
    { label: 'تعداد کالاها', value: items.length },
    { label: 'دسته‌ها', value: categoryCount },
    { label: 'برندها', value: brandCount },
  ].filter((s) => s.value > 0);

  const renderHero = (): React.ReactNode => {
    if (slides.length > 0) {
      return <HeroSlider slides={slides} accentColor={accentColor} autoplay={custom.heroAutoplay} />;
    }
    return (
      <section className="rounded-3xl bg-slate-900 p-6 text-white lg:p-8">
        <h2 className="text-xl font-black leading-snug lg:text-3xl">{heroTitle}</h2>
        {heroSubtitle ? <p className="mt-2 max-w-2xl text-sm leading-8 text-slate-300">{heroSubtitle}</p> : null}
        {stats.length > 0 ? (
          <div className="mt-5 flex flex-wrap gap-2">
            {stats.map((s) => (
              <span key={s.label} className="rounded-full bg-white/10 px-3 py-1.5 text-xs font-bold">
                {s.label}: {toPersianDigits(s.value)}
              </span>
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
        return <CategoryRail products={items} categories={categories} config={cfg} accentColor={accentColor} />;
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
          <ProductsGridSection products={items} config={cfg} accentColor={accentColor} columnsClass="grid-cols-2 lg:grid-cols-3 xl:grid-cols-4" />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="flex-1 flex flex-col bg-white" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-3 py-2 text-[11px] text-slate-400">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      <div className="bg-slate-900 text-white">
        <div className="mx-auto w-full max-w-7xl px-4 py-4 lg:flex lg:items-center lg:justify-between">
          <h1 className="text-base font-black lg:text-lg">{store.name}</h1>
          {store.tagline ? <p className="mt-1 text-xs opacity-80 lg:mt-0">{store.tagline}</p> : null}
        </div>
      </div>
      <div className="mx-auto w-full max-w-7xl space-y-6 p-4">
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
export default TechTemplate;
