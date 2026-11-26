import React from 'react';
import { useStore } from '../context/StoreContext';
import { getCentralDeals } from '../utils/centralDeals';
import { Product } from '../types';
import { DealProductCard } from './DealProductCard';
import { ProductCarousel } from "./ProductCarousel";
import { ArrowLeft, Tag } from 'lucide-react';
import { followStoreLink } from '../utils/followStoreLink';
import { pathForView } from '../utils/routing';

interface SpecialDealsSliderProps {
  // Optional pre-scoped list. A dropshipper storefront passes its
  // store-scoped list so the deal prices are the STORE's (the context
  // catalogue is central-priced while the order path charges store
  // prices). The central home passes nothing and keeps the context
  // catalogue, exactly as before.
  products?: Product[];
  // Store branding, passed by a dropshipper storefront's fallback
  // layout: when present the band speaks in the store's name, and
  // in the store's accent when one is configured, instead of the
  // platform's name and purple livery. Absent (central home) the
  // band renders exactly as before.
  storeBrand?: { name: string; accentColor?: string };
}

export const SpecialDealsSlider: React.FC<SpecialDealsSliderProps> = ({ products: productsProp, storeBrand }) => {
  const { products: contextProducts, currentStore, openCatalogCategory } = useStore();
  const products = productsProp ?? contextProducts;

  const dealProducts = getCentralDeals(products, currentStore);
  if (dealProducts.length === 0) return null;

  if (!productsProp && !storeBrand) {
    return (
      <section id="Campaign_Home" data-testid="personalized-carousel-banner" className="central-deals" aria-labelledby="central-deals-title">
        <div className="central-section-title">
          <h2 id="central-deals-title">کالاهای تخفیف‌دار</h2>
        </div>
        <ProductCarousel products={dealProducts} />
      </section>
    );
  }

  const browseDeals = (className: string) => (
    <a href={pathForView('catalog')} onClick={event => followStoreLink(event, () => openCatalogCategory('all'))} className={className}>
      <span className="mb-2 grid size-10 place-items-center rounded-full bg-white text-brand-deep" style={storeBrand?.accentColor ? { color: storeBrand.accentColor } : undefined}>
        <ArrowLeft className="size-5" aria-hidden="true" />
      </span>
      <span className="text-xs font-bold text-white">مشاهده همه کالاها</span>
    </a>
  );

  return (
    <section id="Campaign_Home" data-testid="personalized-carousel-banner" className="relative mx-3.5 lg:mx-8 xl:mx-10 2xl:mx-12 my-4 rounded-2xl overflow-hidden shadow-xs">
    {/* FIX 16: live band is a rounded card over a deep-purple gradient
        (#5d44f8 -> #4e30d6 -> #3f1ca3) plus blur circles. Its width
        is capped to 1240px centred by the #Campaign_Home rule in
        index.css (live CSS patch) -- the mx-8/10/12 margins below are
        overridden at >=1280px by that ID rule, so the band fills the
        1240px main content box exactly like live's (340..1580 @1920).
        Below 1280px the mx values are live's own. */}
      {storeBrand?.accentColor ? (
        <div className="absolute inset-0 z-0" style={{ backgroundColor: storeBrand.accentColor }} />
      ) : (
        <div className="absolute inset-0 z-0 bg-gradient-to-l from-[#5d44f8] via-[#4e30d6] to-[#3f1ca3]" />
      )}
      <div className="absolute inset-0 z-0 opacity-20">
        <div className="absolute -top-10 -right-10 w-48 h-48 rounded-full bg-white/20 blur-2xl" />
        <div className="absolute -bottom-12 -left-8 w-40 h-40 rounded-full bg-white/10 blur-2xl" />
      </div>
      <div className="relative z-10 page-gutter py-4 lg:py-5">
      <div className="mb-3 flex items-center gap-2">
        <div className="w-8 h-8 lg:w-10 lg:h-10 rounded-xl bg-white/20 backdrop-blur-sm flex items-center justify-center">
          <Tag className="w-4 h-4 lg:w-5 lg:h-5 text-white"  strokeWidth={1.5}/>
        </div>
        <div>
          <h2 className="text-base lg:text-xl font-extrabold text-white tracking-tight">تخفیف‌های امروز</h2>
          <p className="text-xs lg:text-sm text-white/85 mt-0.5">{storeBrand ? `پیشنهادهای ویژه ${storeBrand.name}` : 'پیشنهادهای ویژه سلرشو'}</p>
        </div>
      </div>

      {/* FIX 27: live deals band (bundle component z5) has count-dependent rows.
          Common case (>3 deals): the shared carousel of vertical cards plus
          the full-size end tile. 1 deal: the horizontal card plus the
          110x120 tile in a flex row. At most 3 deals: the horizontal card
          on top plus the carousel of the rest. */}
      {dealProducts.length === 1 ? (
        <div className="relative z-10 flex items-stretch gap-2.5 page-gutter py-3">
          <div className="flex-1 min-w-0">
            <DealProductCard product={dealProducts[0]} />
          </div>
          {browseDeals('w-[110px] h-[120px] self-center shrink-0 bg-white/15 hover:bg-white/25 backdrop-blur-md rounded-xl flex flex-col items-center justify-center p-3 text-center cursor-pointer transition-colors border border-white/20')}
        </div>
      ) : dealProducts.length <= 3 ? (
        <>
          <div className="relative z-10 page-gutter pt-3">
            <DealProductCard product={dealProducts[0]} />
          </div>
          <div className="relative z-10 page-gutter py-3">
            <ProductCarousel
              products={dealProducts.slice(1)}
              endTile={
                browseDeals('h-full w-full bg-white/15 hover:bg-white/25 backdrop-blur-md rounded-xl flex flex-col items-center justify-center p-3 text-center cursor-pointer transition-colors border border-white/20')
              }
            />
          </div>
        </>
      ) : (
        <div className="relative z-10 page-gutter py-3">
          <ProductCarousel
            products={dealProducts}
            endTile={
              browseDeals('h-full w-full bg-white/15 hover:bg-white/25 backdrop-blur-md rounded-xl flex flex-col items-center justify-center p-3 text-center cursor-pointer transition-colors border border-white/20')
            }
          />
        </div>
      )}
      </div>
    </section>
  );
};
