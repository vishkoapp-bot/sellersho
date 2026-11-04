import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import { storefrontProducts, sectionEnabled, heroSlidesOf, templateCustom } from './templateTypes';
import { TemplateProductGrid } from './TemplateProductGrid';
import {
  HeroSlider,
  HeroFallbackBanners,
  CategoryRail,
  BannerRow,
  DealsSection,
  TimedDealsSection,
  BrandsRail,
  FeaturedSection,
  BestsellersSection,
  SectionTitle,
} from './StoreSections';

// FIX3 (live parity, tpl-digikala-1): live block order, rebuilt from
// live chunk evidence — heroSlides (+ heroFallbackBanner when there
// are no slides) → categoryRail → deals → timedDeals → brands →
// bestsellers → products. Sections self-hide when disabled or empty,
// so a store without slides/config still shows name + products.
export const DigikalaTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const items = storefrontProducts(store, products);
  const slides = heroSlidesOf(config);
  const showSlides = sectionEnabled(config, 'heroSlides') && slides.length > 0;
  return (
    <div className="flex-1 flex flex-col bg-white" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-3 py-2 text-[11px] text-slate-400">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      <div className="px-3 pb-2 lg:flex lg:items-center lg:justify-between">
        <h1 className="text-base font-black text-slate-900">{store.name}</h1>
        {sectionEnabled(config, 'announcement') && store.tagline ? (
          <p className="mt-1 text-xs font-bold lg:mt-0" style={{ color: accentColor }}>{store.tagline}</p>
        ) : null}
      </div>
      <div className="space-y-4 p-3">
        {showSlides ? (
          <HeroSlider slides={slides} accentColor={accentColor} autoplay={templateCustom(config).heroAutoplay} />
        ) : sectionEnabled(config, 'hero') ? (
          <>
            <HeroFallbackBanners config={config} accentColor={accentColor} />
            {store.logo ? (
              <img src={store.logo} alt={store.name} className="mx-auto h-28 rounded-2xl object-contain bg-slate-50 border border-slate-100" />
            ) : null}
          </>
        ) : null}
        <BannerRow config={config} />
        <CategoryRail products={items} categories={categories} config={config} accentColor={accentColor} />
        <DealsSection products={items} config={config} accentColor={accentColor} />
        <TimedDealsSection store={store} products={items} config={config} accentColor={accentColor} />
        <BrandsRail products={items} config={config} accentColor={accentColor} />
        <FeaturedSection products={items} config={config} accentColor={accentColor} />
        <BestsellersSection products={items} config={config} accentColor={accentColor} />
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
export default DigikalaTemplate;
