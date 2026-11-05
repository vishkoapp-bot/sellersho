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
  HeroFallbackBanners,
} from './StoreSections';

// Config-driven storefront: sections render in the order and
// enablement stored in the resolved template config (template
// blueprint merged under the seller's saved config); every text,
// image and link comes from the seller-editable custom config or
// the store's own data — nothing is hardcoded per store.
export const DigikalaTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const cfg = resolveTemplateConfig('tpl-digikala-1', config);
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

  const renderHero = (): React.ReactNode => (
    <div className="grid gap-3 lg:grid-cols-3">
      <div className="lg:col-span-2">
        {slides.length > 0 ? (
          <HeroSlider slides={slides} accentColor={accentColor} autoplay={custom.heroAutoplay} />
        ) : (
          <HeroFallbackBanners config={cfg} accentColor={accentColor} />
        )}
      </div>
      <div className="flex flex-col items-center justify-center rounded-2xl border border-slate-200 bg-white p-4 text-center">
        {store.logo ? (
          <img src={store.logo} alt={store.name} className="h-16 w-auto rounded-xl object-contain" />
        ) : null}
        <h1 className="mt-2 text-base font-black text-slate-900">{store.name}</h1>
        {store.tagline ? <p className="mt-1 text-xs leading-6 text-slate-500">{store.tagline}</p> : null}
      </div>
    </div>
  );

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
          <ProductsGridSection products={items} config={cfg} accentColor={accentColor} columnsClass="grid-cols-2 lg:grid-cols-5" />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="flex-1 flex flex-col bg-[#f0f0f1]" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-3 pt-3 text-[11px] text-slate-400 lg:px-4">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      <div className="mx-auto w-full max-w-[1400px] space-y-4 p-3 lg:p-4">
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
export default DigikalaTemplate;
