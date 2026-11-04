import React from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import { storefrontProducts, sectionEnabled } from './templateTypes';
import { TemplateProductGrid } from './TemplateProductGrid';

// FIX3 (live parity, tpl-tech-1): deliberately NOT expanded. Live's
// Tech chunk has no heroSlides, categoryRails, banners, deals,
// timedDeals or brands blocks at all — its only section is the full
// product list (live id "products"), with about/contact rendered
// outside the template by the storefront view (unchanged here). Adding
// rails/deals to Tech would invent live behaviour, not rebuild it.
export const TechTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config }) => {
  const items = storefrontProducts(store, products);
  return (
    <div className="flex-1 flex flex-col bg-white" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-3 py-2 text-[11px] text-slate-400">
        <span>خانه</span><span className="mx-1">/</span><span className="text-slate-700 font-bold">{store.name}</span>
      </nav>
      <div className="bg-slate-900 text-white p-4 lg:flex lg:items-center lg:justify-between">
        <h1 className="text-base font-black">{store.name}</h1>
        {sectionEnabled(config, 'announcement') && store.tagline ? (
          <p className="mt-1 text-xs opacity-80 lg:mt-0">{store.tagline}</p>
        ) : null}
      </div>
      {sectionEnabled(config, 'hero') && store.description ? (
        <p className="p-3 text-xs leading-6 text-slate-500">{store.description}</p>
      ) : null}
      <div className="p-3">
        <TemplateProductGrid products={items} columnsClass="grid-cols-2 lg:grid-cols-3" />
      </div>
    </div>
  );
};
export default TechTemplate;
