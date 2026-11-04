import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import { storefrontProducts, sectionEnabled, heroSlidesOf, templateCustom } from './templateTypes';
import { TemplateProductGrid } from './TemplateProductGrid';
import {
  HeroSlider,
  CategoryCards,
  BannerRow,
  DealsSection,
  BrandsRail,
  BestsellersSection,
  SectionTitle,
} from './StoreSections';

// FIX3 (live parity, tpl-fashion-1): live block order, rebuilt from
// live chunk evidence — hero (editorial panel, replaced by heroSlides
// when the store configured slides) → categories (cards, NOT a
// product rail: live Fashion has a categories block but no
// categoryRails key) → deals → brands → bestsellers → products.
export const FashionTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const items = storefrontProducts(store, products);
  const slides = heroSlidesOf(config);
  const showSlides = sectionEnabled(config, 'heroSlides') && slides.length > 0;
  return (
    <div className="flex-1 flex flex-col bg-white" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-3 py-2 text-[11px] text-slate-400">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      <div className="space-y-4 p-3">
        {showSlides ? (
          <HeroSlider slides={slides} accentColor={accentColor} autoplay={templateCustom(config).heroAutoplay} />
        ) : sectionEnabled(config, 'hero') ? (
          <div className="lg:grid lg:grid-cols-2 lg:gap-6">
            <div className="rounded-2xl p-4 text-white" style={{ backgroundColor: accentColor }}>
              <h1 className="text-lg font-black">{store.name}</h1>
              <p className="mt-1 text-xs leading-6 opacity-90">{store.tagline || store.description || ''}</p>
            </div>
            {store.logo ? <img src={store.logo} alt={store.name} className="mt-3 h-32 w-full rounded-2xl object-contain bg-slate-50 border border-slate-100 lg:mt-0" /> : null}
          </div>
        ) : null}
        <BannerRow config={config} />
        <CategoryCards products={items} categories={categories} config={config} accentColor={accentColor} />
        <DealsSection products={items} config={config} accentColor={accentColor} />
        <BrandsRail products={items} config={config} accentColor={accentColor} />
        <BestsellersSection products={items} config={config} accentColor={accentColor} />
        {sectionEnabled(config, 'products') ? (
          <section className="space-y-3">
            <SectionTitle accentColor={accentColor}>همه کالاهای فروشگاه</SectionTitle>
            <TemplateProductGrid products={items} columnsClass="grid-cols-2 lg:grid-cols-3" />
          </section>
        ) : null}
      </div>
    </div>
  );
};
export default FashionTemplate;
