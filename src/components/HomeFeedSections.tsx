import React, { useEffect, useState } from 'react';
import { useStore } from '../context/StoreContext';
import { homepageApi, HomepageSection } from '../services/homepageApi';
import { bannersApi, ApiBanner, cleanBannerTitle, isUsableBanner } from '../services/bannersApi';
import { ProductCarouselSection } from './ProductCarouselSection';
import { ProductGridSection } from './ProductGridSection';
import { TrustFeatures } from './TrustFeatures';


// Live homepage sections resolve their content from /banners by position
// (live bundle section switch Y5): secondary_slider renders the secondary
// banners, promo_banner with index 0 renders the promo banners, tile_grid
// with index 0 renders the tile banners. A section with a non-zero index
// renders nothing in live, and so does here -- that is live behaviour, not
// a missing section. Empty configs therefore never silently vanish: the
// index-0 / secondary sections below always render the real banners.
const useBannerClick = () => {
  const { openCatalogCategory } = useStore();
  return (banner: ApiBanner) => {
    if (banner.linkUrl && banner.linkUrl.startsWith('#')) {
      const el = document.querySelector(banner.linkUrl);
      if (el) {
        el.scrollIntoView({ behavior: 'smooth' });
        return;
      }
    }
    openCatalogCategory('all', cleanBannerTitle(banner.title));
  };
};

const SecondaryBannerSlider: React.FC<{ banners: ApiBanner[] }> = ({ banners }) => {
  const [index, setIndex] = useState(0);
  const handleClick = useBannerClick();
  const slides = banners.filter(isUsableBanner);
  useEffect(() => {
    if (slides.length <= 1) return;
    const timer = setInterval(() => setIndex((i) => (i + 1) % slides.length), 5000);
    return () => clearInterval(timer);
  }, [slides.length]);
  if (!slides.length) return null;
  return (
    <section id="SliderBanner" data-testid="slider-banner" className="px-3.5 my-3 lg:px-8 lg:my-5 xl:px-10 2xl:px-12">
      <div className="relative overflow-hidden rounded-2xl shadow-xs aspect-[2.3/1] lg:aspect-[4.5/1] lg:max-h-[300px] xl:aspect-[5/1] xl:max-h-[320px] w-full flex items-center bg-slate-100">
        {slides.map((banner, i) => {
          const title = cleanBannerTitle(banner.title);
          return (
            <div
              key={banner.id}
              onClick={() => handleClick(banner)}
              className={`absolute inset-0 w-full h-full cursor-pointer transition-opacity duration-500 ${
                i === index ? 'opacity-100 z-10' : 'opacity-0 z-0 pointer-events-none'
              }`}
            >
              <img src={banner.imageUrl} alt={title} loading={i === 0 ? 'eager' : 'lazy'} decoding="async" className="w-full h-full object-cover rounded-2xl" />
              {(title || banner.subtitle) && (
                <div className="absolute inset-0 z-10 flex flex-col justify-center pl-6 pr-14 lg:pr-16 bg-gradient-to-l from-black/45 via-black/10 to-transparent rounded-2xl pointer-events-none">
                  {title && <h3 className="text-white text-lg lg:text-2xl xl:text-3xl font-extrabold tracking-tight drop-shadow-md leading-snug max-w-[70%]">{title}</h3>}
                  {banner.subtitle && <p className="text-white/90 text-xs lg:text-sm xl:text-base font-medium mt-1.5 drop-shadow max-w-[65%]">{banner.subtitle}</p>}
                  {banner.linkUrl && (
                    <button type="button" onClick={(e) => { e.stopPropagation(); handleClick(banner); }} className="pointer-events-auto self-start mt-2.5 px-4 py-2 rounded-xl bg-[#5d44f8] hover:bg-[#4a33d8] text-white text-[11px] lg:text-sm lg:px-6 lg:py-3 font-bold shadow-md transition-colors">مشاهده</button>
                  )}
                </div>
              )}
            </div>
          );
        })}
        {slides.length > 1 && (
          <>
            <button onClick={() => setIndex((i) => (i - 1 + slides.length) % slides.length)} className="absolute right-2 z-20 w-11 h-11 rounded-full bg-black/25 hover:bg-black/50 backdrop-blur-sm text-white flex items-center justify-center transition-colors" title="قبلی" aria-label="اسلاید قبلی">&#8250;</button>
            <button onClick={() => setIndex((i) => (i + 1) % slides.length)} className="absolute left-2 z-20 w-11 h-11 rounded-full bg-black/25 hover:bg-black/50 backdrop-blur-sm text-white flex items-center justify-center transition-colors" title="بعدی" aria-label="اسلاید بعدی">&#8249;</button>
          </>
        )}
      </div>
    </section>
  );
};

const PromoBanners: React.FC<{ id: string; banners: ApiBanner[] }> = ({ id, banners }) => {
  const [errored, setErrored] = useState<Record<string, boolean>>({});
  const [loaded, setLoaded] = useState<Record<string, boolean>>({});
  const handleClick = useBannerClick();
  const list = banners.filter(isUsableBanner).filter((b) => (b.imageUrl || '').trim() && !errored[b.id]);
  if (!list.length) return null;
  const multi = list.length > 1;
  return (
    <section id={id} className="px-3.5 my-3 lg:px-8 lg:my-5 xl:px-10 2xl:px-12">
      <div className={`grid grid-cols-1 ${multi ? 'lg:grid-cols-2' : ''} gap-3 lg:gap-4`}>
        {list.map((banner) => (
          <div
            key={banner.id}
            onClick={() => handleClick(banner)}
            className={`relative w-full aspect-[2.3/1] ${multi ? 'lg:max-h-[300px] xl:max-h-[320px]' : 'lg:max-h-[420px] 2xl:max-h-[520px]'} rounded-2xl overflow-hidden bg-slate-100 shadow-2xs hover:opacity-95 transition-opacity cursor-pointer active:scale-98`}
          >
            {!loaded[banner.id] && <span aria-hidden="true" className="absolute inset-0 bg-slate-100" />}
            <img
              src={banner.imageUrl}
              alt={cleanBannerTitle(banner.title)}
              loading="lazy"
              onLoad={() => setLoaded((prev) => ({ ...prev, [banner.id]: true }))}
              onError={() => setErrored((prev) => ({ ...prev, [banner.id]: true }))}
              className={`absolute inset-0 w-full h-full object-cover transition-opacity duration-300 ${loaded[banner.id] ? 'opacity-100' : 'opacity-0'}`}
            />
          </div>
        ))}
      </div>
    </section>
  );
};

const TileBanners: React.FC<{ id: string; banners: ApiBanner[] }> = ({ id, banners }) => {
  const [errored, setErrored] = useState<Record<string, boolean>>({});
  const [loaded, setLoaded] = useState<Record<string, boolean>>({});
  const handleClick = useBannerClick();
  const list = banners.filter(isUsableBanner).filter((b) => ((b.imageUrl || '').trim() && !errored[b.id]) || cleanBannerTitle(b.title));
  if (!list.length) return null;
  const cols = list.length >= 4 ? 'grid-cols-2 lg:grid-cols-4' : list.length === 3 ? 'grid-cols-2 lg:grid-cols-3' : list.length === 2 ? 'grid-cols-2' : 'grid-cols-1';
  return (
    <section id={id} className="px-3.5 my-3 lg:px-8 lg:my-5 xl:px-10 2xl:px-12">
      <div className={`grid ${cols} gap-3 lg:gap-4`}>
        {list.map((banner) => {
          const title = cleanBannerTitle(banner.title);
          const hasImage = !!(banner.imageUrl || '').trim() && !errored[banner.id];
          return (
            <div
              key={banner.id}
              onClick={() => handleClick(banner)}
              className="relative rounded-2xl overflow-hidden bg-slate-100 shadow-2xs hover:opacity-95 transition-opacity cursor-pointer active:scale-98 aspect-square lg:aspect-[3/2]"
            >
              {title && (
                <span className="absolute inset-0 flex items-center justify-center px-3 text-center">
                  <span className="text-[12px] lg:text-sm font-bold leading-5 text-slate-800">{title}</span>
                </span>
              )}
              {hasImage && (
                <img
                  src={banner.imageUrl}
                  alt={title}
                  loading="lazy"
                  onLoad={() => setLoaded((prev) => ({ ...prev, [banner.id]: true }))}
                  onError={() => setErrored((prev) => ({ ...prev, [banner.id]: true }))}
                  className={`absolute inset-0 w-full h-full object-cover transition-opacity duration-300 ${loaded[banner.id] ? 'opacity-100' : 'opacity-0'}`}
                />
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
};

export const HomeFeedSections: React.FC = () => {
  const { products, stores, openStorefront } = useStore();
  const [apiSections, setApiSections] = useState<HomepageSection[] | null>(null);
  const [promoBanners, setPromoBanners] = useState<ApiBanner[]>([]);
  const [tileBanners, setTileBanners] = useState<ApiBanner[]>([]);
  const [secondaryBanners, setSecondaryBanners] = useState<ApiBanner[]>([]);

  useEffect(() => {
    let cancelled = false;
    bannersApi.getBanners('promo').then((list) => { if (!cancelled) setPromoBanners(list); }).catch(() => {});
    bannersApi.getBanners('tile').then((list) => { if (!cancelled) setTileBanners(list); }).catch(() => {});
    bannersApi.getBanners('secondary').then((list) => { if (!cancelled) setSecondaryBanners(list); }).catch(() => {});
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    let cancelled = false;
    homepageApi.getSections()
      .then((sections) => {
        if (!cancelled) setApiSections(sections.filter((s) => s.isActive).sort((a, b) => a.sortOrder - b.sortOrder));
      })
      .catch(() => {
        if (!cancelled) setApiSections([]);
      });
    return () => { cancelled = true; };
  }, []);

  if (apiSections && apiSections.length > 0) {
    return (
      <>
        {apiSections.map((section) => {
          const cfg: Record<string, any> = section.config || {};
          switch (section.sectionType) {
            case 'trust_features':
              return <TrustFeatures key={section.id} />;
            case 'product_carousel': {
              const list = products.filter((pr) => !cfg.categorySlug || pr.category === cfg.categorySlug);
              if (!list.length) return null;
              return <ProductCarouselSection key={section.id} id={section.id} title={section.title || 'پیشنهادها'} categorySlug={cfg.categorySlug} products={list} />;
            }
            case 'product_grid':
              return <ProductGridSection key={section.id} />;
            case 'store_carousel': {
              const activeStores = stores.filter((st) => (st as any).status === 'active' || !(st as any).status);
              if (!activeStores.length) return null;
              return (
                <section key={section.id} className="py-2.5 px-3 lg:px-8 lg:py-4 xl:px-10 2xl:px-12 bg-white border-b border-slate-100/80">
                  <h2 className="text-[14px] lg:text-base font-bold text-slate-900 tracking-tight mb-3 px-1">{section.title || 'فروشگاه‌ها'}</h2>
                  <div className="flex items-stretch gap-3 overflow-x-auto no-scrollbar pb-1">
                    {activeStores.map((store) => (
                      <button key={store.id} onClick={() => openStorefront(store.slug)} className="w-[190px] shrink-0 bg-white border border-slate-200/90 rounded-2xl p-4 text-right hover:shadow-md transition-all">
                        {store.logo ? (
                          <img src={store.logo} alt="" className="w-10 h-10 rounded-xl object-cover mb-2" />
                        ) : null}
                        <span className="block text-xs font-bold text-slate-800">{store.name}</span>
                        {store.ownerType === 'central' && (
                          <span className="inline-block text-[10px] bg-blue-100 text-blue-700 px-1.5 py-0.5 rounded font-bold mt-1">مرکزی</span>
                        )}
                        {store.tagline && <span className="block text-[11px] text-slate-500 mt-1">{store.tagline}</span>}
                      </button>
                    ))}
                  </div>
                </section>
              );
            }
            case 'promo_banner': {
              // Live G5 renders promo banners only for index 0; the
              // section title is empty in live data, the image is the
              // content -- a missing title must not blank the section.
              if ((cfg.index || 0) !== 0) return null;
              return <PromoBanners key={section.id} id={`promo-${section.id}`} banners={promoBanners} />;
            }
            case 'tile_grid': {
              // Live $5 renders tile banners only for index 0 (the
              // layout:"quad" section); grid columns derive from the
              // banner count, not from cfg.tiles, which live data
              // never carries.
              if ((cfg.index || 0) !== 0) return null;
              return <TileBanners key={section.id} id={`tile-${section.id}`} banners={tileBanners} />;
            }
            case 'secondary_slider': {
              // Live V5 renders the secondary banners regardless of
              // the (empty) section config -- no slides array in config.
              return <SecondaryBannerSlider key={section.id} banners={secondaryBanners} />;
            }
            default:
              return null;
          }
        })}
        {/* FIX 12: live appends two carousels after the API sections
            (live HomeFeedSections, not sections data): discounted
            products (discountPercent > 0, price desc, first 5) and
            under-300k products. */}
        {/* FIX 14: live hides these two carousels below 1280px. */}
        <div className="hidden min-[1280px]:block">
          <ProductCarouselSection id="desktop-discount-band" title="کالاهای تخفیف‌دار" products={[...products].filter((p) => p.discountPercent > 0).sort((a, b) => b.price - a.price).slice(0, 5)} />
          <ProductCarouselSection id="desktop-under-300k" title="زیر ۳۰۰ هزار تومان" products={products.filter((p) => p.price < 300000)} />
        </div>
      </>
    );
  }
  // FIX 9: no hardcoded fallback feed. When the homepage-sections API
  // returns no sections or fails, the homepage renders nothing here
  // (neutral empty state) instead of old mock data.
  return null;
};
