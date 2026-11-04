import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import { storefrontProducts, sectionEnabled, heroSlidesOf, templateCustom } from './templateTypes';
import { TemplateProductGrid } from './TemplateProductGrid';
import { HeroSlider, BannerRow, CategoryCards, DealsSection, BrandsRail, FeaturedSection, BestsellersSection, SectionTitle, TrustStripSection, DiscountedSection, AboutSection, TopRatedSection, CampaignSection, NewsletterSection, FaqSection } from './StoreSections';

// FIX3 (live parity, tpl-modern-1): Modern lives in live's main bundle
// (no separate chunk), so its full live section order was NOT
// recovered from bundle evidence (U44) and is not invented here.
// FIX6: vishkostor/sahara1 enable deals/brands/featured/bestsellers
// and a categories block in their dict config, so Modern now renders
// those enabled sections too (CategoryCards for the dict's
// categoryTiles key; rail-vs-cards for Modern is still U44-unproven,
// Cards matches the enabled key name). Order follows Digikala's
// proven order minus timedDeals (neither Modern store enables it);
// it is NOT claimed to be live's Modern order. TimedDeals is
// deliberately not added: neither Modern store enables it.
export const ModernTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const items = storefrontProducts(store, products);
  const slides = heroSlidesOf(config);
  const showSlides = sectionEnabled(config, 'heroSlides') && slides.length > 0;
  return (
    <div className="flex-1 flex flex-col bg-white" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-3 py-2 text-[11px] text-slate-400">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      {sectionEnabled(config, 'announcement') && store.tagline ? (
        <div className="mx-3 rounded-xl px-3 py-2 text-xs font-bold text-white" style={{ backgroundColor: accentColor }}>
          {store.tagline}
        </div>
      ) : null}
      <div className="space-y-4 p-3">
        {showSlides ? (
          <HeroSlider slides={slides} accentColor={accentColor} autoplay={templateCustom(config).heroAutoplay} />
        ) : sectionEnabled(config, 'hero') ? (
          <div className="lg:grid lg:grid-cols-2 lg:gap-4 lg:items-center">
            <div>
              <h1 className="text-base font-black text-slate-900">{store.name}</h1>
              <p className="mt-1 text-xs leading-6 text-slate-500">{store.description || store.shortDescription || ''}</p>
            </div>
            {store.logo ? <img src={store.logo} alt={store.name} className="mt-3 h-24 w-full rounded-2xl object-contain bg-slate-50 border border-slate-100 lg:mt-0" /> : null}
          </div>
        ) : null}
        {/* FIX 14: live Modern order — announcement, hero, trustStrip,
            categories, brands, deals, discounted, featured, about,
            bestsellers, topRated, promo, campaign, newsletter,
            testimonials (no data source, not rendered), faq. */}
        <TrustStripSection config={config} accentColor={accentColor} />
        <CategoryCards products={items} categories={categories} config={config} accentColor={accentColor} />
        <BrandsRail products={items} config={config} accentColor={accentColor} />
        <DealsSection products={items} config={config} accentColor={accentColor} />
        <DiscountedSection products={items} config={config} accentColor={accentColor} />
        <FeaturedSection products={items} config={config} accentColor={accentColor} />
        <AboutSection store={store} config={config} accentColor={accentColor} />
        <BestsellersSection products={items} config={config} accentColor={accentColor} />
        <TopRatedSection products={items} config={config} accentColor={accentColor} />
        <BannerRow config={config} />
        <CampaignSection store={store} products={items} config={config} accentColor={accentColor} />
        <NewsletterSection config={config} accentColor={accentColor} storeSlug={store.slug} />
        <FaqSection config={config} accentColor={accentColor} />
        {sectionEnabled(config, 'products') ? (
          <section className="space-y-3">
            <SectionTitle accentColor={accentColor}>همه کالاهای فروشگاه</SectionTitle>
            <TemplateProductGrid products={items} columnsClass="grid-cols-2 lg:grid-cols-4" />
          </section>
        ) : null}
      </div>
    </div>
  );
};
