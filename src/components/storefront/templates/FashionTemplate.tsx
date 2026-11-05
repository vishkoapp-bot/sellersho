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
  CategoryCards,
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

// Config-driven storefront: sections render in the order and
// enablement stored in the resolved template config (template
// blueprint merged under the seller's saved config); every text,
// image and link comes from the seller-editable custom config or
// the store's own data — nothing is hardcoded per store.
export const FashionTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const cfg = resolveTemplateConfig('tpl-fashion-1', config);
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
  const heroSubtitle = customText(cfg, 'heroSubtitle', store.tagline || store.description || store.shortDescription || '');
  const heroCtaText = customText(cfg, 'heroCtaText', 'مشاهده کالکشن');
  const heroCtaLink = custom.heroCtaLink || '#products';
  const heroImage = custom.heroImage || (slides[0] && slides[0].image) || store.logo;

  const renderHero = (): React.ReactNode => {
    if (slides.length > 0) {
      return <HeroSlider slides={slides} accentColor={accentColor} autoplay={custom.heroAutoplay} />;
    }
    return (
      <section className="relative overflow-hidden rounded-3xl bg-neutral-900">
        {heroImage ? (
          <img src={heroImage} alt={store.name} className="h-[380px] w-full object-cover lg:h-[480px]" />
        ) : (
          <div className="h-[380px] w-full lg:h-[480px]" />
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-black/40 to-black/20" />
        <div className="absolute inset-0 flex flex-col items-center justify-center px-4 text-center text-white">
          <h1 className="text-3xl font-black tracking-tight lg:text-5xl">{heroTitle}</h1>
          {heroSubtitle ? <p className="mt-3 max-w-lg text-sm leading-8 text-white/85">{heroSubtitle}</p> : null}
          <a
            href={heroCtaLink}
            className="mt-6 rounded-full border border-white px-6 py-2.5 text-sm font-black text-white"
          >
            {heroCtaText}
          </a>
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
      case 'trustStrip':
        return <TrustStripSection config={cfg} accentColor={accentColor} />;
      case 'categoryRail':
        return <CategoryCards products={items} categories={categories} config={cfg} accentColor={accentColor} />;
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
          <ProductsGridSection products={items} config={cfg} accentColor={accentColor} columnsClass="grid-cols-2 lg:grid-cols-4" />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="flex-1 flex flex-col bg-neutral-950 text-neutral-100" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-4 pt-3 text-[11px] text-neutral-400 lg:px-6">
        <span>خانه</span><span className="mx-1">/</span><span className="text-neutral-100 font-bold">{store.name}</span>
      </nav>
      <div className="mx-auto w-full max-w-6xl space-y-10 p-4 lg:p-6">
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
export default FashionTemplate;
