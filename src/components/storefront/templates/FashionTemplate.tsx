import React, { useMemo } from 'react';
import type { StorefrontTemplateProps } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  heroSlidesOf,
  bannersOf,
  templateCustom,
  customText,
  customCount,
  storefrontProducts,
  sectionEnabled,
} from './templateTypes';
import type { TemplateBanner, TemplateConfig } from './templateTypes';
import type { Product, Store } from '../../../types';
import { useStore } from '../../../context/StoreContext';
import { formatPrice, toPersianDigits } from '../../../utils/formatters';
import {
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

// Fashion = dark luxury editorial. The page is neutral-950; blocks
// that are cheap to build natively dark (announcement, hero,
// categories, featured showcase, lookbook tiles) are template-local,
// while the light-themed shared sections render as deliberate
// contrast blocks — their white cards sit on the dark page and only
// their slate-900 titles are re-coloured through the `onDark` scope.
// Everything renders from the resolved config order; every text,
// image and price comes from the seller-editable custom config, the
// store's own fields or real product data — nothing is fabricated.

const DarkSectionHeader: React.FC<{ eyebrow: string; title: string; accentColor: string }> = ({
  eyebrow,
  title,
  accentColor,
}) => (
  <div className="space-y-2">
    <p className="text-[11px] font-black uppercase tracking-[0.35em]" style={{ color: accentColor }}>
      {eyebrow}
    </p>
    <h2 className="text-2xl font-black tracking-tight text-white lg:text-3xl">{title}</h2>
    <span className="block h-px w-16 bg-white/25" />
  </div>
);

// Marquee-style announcement bar: the announcement text (seller
// config, else the store tagline) scrolls on a thin accent pill.
// Letter-spacing is applied only to non-Arabic text — CSS tracking
// disconnects Persian script, so Persian announcements scroll
// untracked instead of rendering broken.
const FashionAnnouncement: React.FC<{ store: Store; config: TemplateConfig; accentColor: string }> = ({
  store,
  config,
  accentColor,
}) => {
  if (!sectionEnabled(config, 'announcement')) return null;
  const custom = templateCustom(config);
  const text = (custom.announcementText || '').trim() || (store.tagline || '').trim();
  if (!text) return null;
  const link = (custom.announcementLink || '').trim();
  const phrase = Array.from({ length: 6 }, () => text).join('  ✦  ');
  const spaced = /[\u0600-\u06FF]/.test(text) ? '' : ' tracking-[0.3em]';
  const bar = (
    <div className="overflow-hidden rounded-full py-2" style={{ backgroundColor: accentColor }}>
      <style>{'@keyframes tpl-fashion-marquee{from{transform:translateX(0)}to{transform:translateX(50%)}}'}</style>
      <div className="flex w-max [animation:tpl-fashion-marquee_24s_linear_infinite] motion-reduce:[animation:none]">
        {[0, 1].map((half) => (
          <span key={half} className={`whitespace-nowrap px-3 text-[11px] font-bold text-white${spaced}`}>
            {phrase}
          </span>
        ))}
      </div>
    </div>
  );
  return (
    <section data-testid="store-announcement">
      {link ? (
        <a href={link} target="_blank" rel="noreferrer" className="block">
          {bar}
        </a>
      ) : (
        bar
      )}
    </section>
  );
};

// Dark editorial category tiles: real category imagery (category
// image, else the group's first product image) under a black
// gradient, name in bold white at the bottom.
const FashionCategoryTiles: React.FC<{ groups: CategoryGroup[]; title: string; accentColor: string }> = ({
  groups,
  title,
  accentColor,
}) => (
  <section data-testid="store-category-cards" className="space-y-4">
    <DarkSectionHeader eyebrow="Collection" title={title} accentColor={accentColor} />
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
      {groups.map((group) => {
        const image = group.image || (group.products[0] && group.products[0].image) || '';
        return (
          <div key={group.slug} className="group relative h-44 overflow-hidden rounded-[1.5rem] bg-neutral-900 lg:h-56">
            {image ? (
              <img
                src={image}
                alt={group.title}
                loading="lazy"
                className="absolute inset-0 h-full w-full object-cover transition duration-500 group-hover:scale-105"
              />
            ) : (
              <span className="absolute inset-0 flex items-center justify-center text-6xl font-black text-white/10">
                {group.title.slice(0, 1)}
              </span>
            )}
            <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/25 to-transparent" />
            <div className="absolute inset-x-0 bottom-0 p-4">
              <p className="text-base font-black text-white lg:text-lg">{group.title}</p>
              <p className="mt-0.5 text-[11px] text-white/70">{toPersianDigits(group.count)} کالا</p>
            </div>
          </div>
        );
      })}
    </div>
  </section>
);

// Large lookbook/banner tiles: grayscale editorial stills that come
// to colour (and scale) on hover. An odd last banner spans the row.
const FashionDuoTiles: React.FC<{ banners: TemplateBanner[] }> = ({ banners }) => (
  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
    {banners.map((b, i) => {
      const oddLast = banners.length % 2 === 1 && i === banners.length - 1;
      const tile = (
        <div className="group relative h-72 w-full overflow-hidden rounded-[2rem] bg-neutral-900 lg:h-96">
          <img
            src={b.image}
            alt={b.title || ''}
            loading="lazy"
            className="absolute inset-0 h-full w-full object-cover grayscale transition duration-700 group-hover:scale-105 group-hover:grayscale-0"
          />
          <div className="absolute inset-0 bg-gradient-to-t from-black/75 via-black/15 to-transparent" />
          {b.title ? <p className="absolute inset-x-0 bottom-0 p-5 text-lg font-black text-white">{b.title}</p> : null}
        </div>
      );
      return b.link ? (
        <a
          key={`${b.image}-${i}`}
          href={b.link}
          target="_blank"
          rel="noreferrer"
          className={`block ${oddLast ? 'sm:col-span-2' : ''}`}
        >
          {tile}
        </a>
      ) : (
        <div key={`${b.image}-${i}`} className={oddLast ? 'sm:col-span-2' : ''}>
          {tile}
        </div>
      );
    })}
  </div>
);

// Featured showcase: the first featured product as a large cinematic
// tile (real image, brand, title and price; discount badge only when
// the product really carries one). Clicking opens the product page,
// exactly like the shared ProductCard.
const FashionFeaturedShowcase: React.FC<{
  product: Product;
  accentColor: string;
  onOpen: (p: Product) => void;
}> = ({ product, accentColor, onOpen }) => (
  <div
    onClick={() => onOpen(product)}
    className="group relative cursor-pointer overflow-hidden rounded-[2rem] bg-neutral-900"
  >
    {product.image ? (
      <img
        src={product.image}
        alt={product.title}
        loading="lazy"
        className="h-80 w-full object-cover transition duration-700 group-hover:scale-105 lg:h-[26rem]"
      />
    ) : (
      <div className="h-80 w-full lg:h-[26rem]" />
    )}
    <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/30 to-transparent" />
    {product.discountPercent > 0 ? (
      <span
        className="absolute right-4 top-4 rounded-full px-3 py-1 text-[11px] font-black text-white"
        style={{ backgroundColor: accentColor }}
      >
        {toPersianDigits(product.discountPercent)}٪ تخفیف
      </span>
    ) : null}
    <div className="absolute inset-x-0 bottom-0 p-5 lg:p-7">
      {product.brand ? (
        <p className="text-[11px] font-black uppercase" style={{ color: accentColor }}>
          {product.brand}
        </p>
      ) : null}
      <h3 className="mt-1 text-xl font-black text-white lg:text-2xl">{product.title}</h3>
      <div className="mt-2 flex items-baseline gap-2">
        {product.originalPrice > product.price ? (
          <span className="text-sm text-white/50 line-through">{formatPrice(product.originalPrice)}</span>
        ) : null}
        <span className="text-lg font-black text-white">
          {formatPrice(product.price)} <span className="text-xs font-bold text-white/70">تومان</span>
        </span>
      </div>
    </div>
  </div>
);

export const FashionTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const cfg = resolveTemplateConfig('tpl-fashion-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const slides = heroSlidesOf(cfg);
  const banners = bannersOf(cfg);
  const custom = templateCustom(cfg);
  const { openProductDetail } = useStore();
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
  const heroImage = custom.heroImage || (slides[0] && slides[0].image) || (items[0] && items[0].image) || store.logo;
  const productsTitle = customText(cfg, 'productsTitle', 'همه کالاها');

  const categoryGroups = useMemo(
    () => groupProductsByCategory(items, categories, custom.categoryOverrides),
    [items, categories, cfg],
  );
  const featuredList = useMemo(
    () => items.filter((p) => p.featured).slice(0, customCount(cfg, 'featuredCount', 10)),
    [items, cfg],
  );
  const bannersInOwnSection = order.includes('banners') || order.includes('promo');

  // Shared light sections keep their white cards as contrast blocks;
  // only their slate-900 section titles need re-colouring on dark.
  const onDark = '[&_h3]:text-neutral-50';

  const renderHero = (): React.ReactNode => {
    if (slides.length > 0) {
      // The shared slider's frame stays, but its imagery is stretched
      // to cinematic height so slides read as a fashion hero.
      return (
        <div className="[&_img]:h-[380px] [&_img]:lg:h-[520px]">
          <HeroSlider slides={slides} accentColor={accentColor} autoplay={custom.heroAutoplay} />
        </div>
      );
    }
    return (
      <section className="relative overflow-hidden rounded-[2rem] bg-neutral-900">
        {heroImage ? (
          <img src={heroImage} alt={store.name} className="h-[380px] w-full object-cover sm:h-[420px] lg:h-[540px]" />
        ) : (
          <div
            className="h-[380px] w-full sm:h-[420px] lg:h-[540px]"
            style={{ background: `radial-gradient(60% 90% at 80% 0%, ${accentColor}40 0%, transparent 60%)` }}
          />
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-black/85 via-black/35 to-black/10" />
        <div className="absolute inset-0 flex flex-col justify-end p-6 text-right lg:p-10">
          <span className="mb-4 block h-1 w-14 rounded-full" style={{ backgroundColor: accentColor }} />
          <h1 className="text-4xl font-black leading-tight tracking-tight text-white lg:text-6xl">{heroTitle}</h1>
          {heroSubtitle ? (
            <p className="mt-3 max-w-xl text-sm leading-8 text-white/80 lg:text-base">{heroSubtitle}</p>
          ) : null}
          <div className="mt-6 flex flex-wrap gap-3">
            <a
              href={heroCtaLink}
              className="rounded-full px-6 py-3 text-sm font-black text-white transition hover:opacity-90"
              style={{ backgroundColor: accentColor }}
            >
              {heroCtaText}
            </a>
            <a
              href="#products"
              className="rounded-full border border-white/70 px-6 py-3 text-sm font-black text-white backdrop-blur transition hover:bg-white/10"
            >
              {productsTitle}
            </a>
          </div>
        </div>
      </section>
    );
  };

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return <FashionAnnouncement store={store} config={cfg} accentColor={accentColor} />;
      case 'hero':
      case 'heroSlides':
        return once('hero') ? renderHero() : null;
      case 'trustStrip':
        return <TrustStripSection config={cfg} accentColor={accentColor} />;
      case 'categoryRail': {
        const tiles = categoryGroups.slice(0, customCount(cfg, 'categoriesCount', 6));
        const hasImagery = tiles.some((g) => g.image || (g.products[0] && g.products[0].image));
        if (tiles.length > 0 && hasImagery) {
          return (
            <FashionCategoryTiles
              groups={tiles}
              title={customText(cfg, 'categoriesTitle', 'دسته‌بندی‌ها')}
              accentColor={accentColor}
            />
          );
        }
        return (
          <div className={onDark}>
            <CategoryCards products={items} categories={categories} config={cfg} accentColor={accentColor} />
          </div>
        );
      }
      case 'banners':
      case 'promo':
        return once('banners') ? (
          banners.length >= 2 ? (
            <FashionDuoTiles banners={banners} />
          ) : (
            <div className="[&_p]:text-neutral-200">
              <BannerRow config={cfg} />
            </div>
          )
        ) : null;
      case 'deals':
        return (
          <div className={onDark}>
            <DealsSection products={items} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'timedDeals':
        return <TimedDealsSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'brands':
        return (
          <div className={onDark}>
            <BrandsRail products={items} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'featured': {
        if (featuredList.length === 0) return null;
        const first = featuredList[0];
        const rest = featuredList.slice(1);
        return (
          <section data-testid="store-featured" className="space-y-4">
            <DarkSectionHeader
              eyebrow="Featured"
              title={customText(cfg, 'featuredTitle', 'کالاهای منتخب')}
              accentColor={accentColor}
            />
            <FashionFeaturedShowcase product={first} accentColor={accentColor} onOpen={openProductDetail} />
            {rest.length > 0 ? (
              <TemplateProductGrid products={rest} columnsClass="grid-cols-2 lg:grid-cols-4" />
            ) : null}
          </section>
        );
      }
      case 'bestsellers':
        return (
          <div className={onDark}>
            <BestsellersSection products={items} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'discounted':
        return (
          <div className={onDark}>
            <DiscountedSection products={items} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'topRated':
        return (
          <div className={onDark}>
            <TopRatedSection products={items} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'about':
        return (
          <div className="[&_h3]:text-neutral-50 [&_p]:text-neutral-300">
            <AboutSection store={store} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'campaign':
        return (
          <div className={onDark}>
            <CampaignSection store={store} products={items} config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'newsletter':
        // The shared form keeps its own light panel; on the dark page
        // it is framed as a matted contrast card inside a glass shell.
        return (
          <div className="rounded-[2rem] border border-white/10 bg-white/5 p-2">
            <NewsletterSection config={cfg} accentColor={accentColor} storeSlug={store.slug} />
          </div>
        );
      case 'testimonials':
        return (
          <div className="rounded-[2rem] bg-neutral-900 p-4 [&_h3]:text-neutral-50 lg:p-6">
            <TestimonialsSection config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'faq':
        return (
          <div className={onDark}>
            <FaqSection config={cfg} accentColor={accentColor} />
          </div>
        );
      case 'lookbook':
        return (
          <div className="space-y-3">
            <LookbookSection config={cfg} accentColor={accentColor} />
            {!bannersInOwnSection && banners.length >= 2 ? <FashionDuoTiles banners={banners} /> : null}
          </div>
        );
      case 'products':
      case 'allProducts':
        return once('products') ? (
          <div id="products" className="[&_h3]:text-xl [&_h3]:text-neutral-50 [&_h3]:lg:text-2xl">
            <ProductsGridSection products={items} config={cfg} accentColor={accentColor} columnsClass="grid-cols-2 lg:grid-cols-4" />
          </div>
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="flex-1 flex flex-col bg-neutral-950 text-neutral-100" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <nav aria-label="مسیر صفحه" className="px-4 pt-3 text-[11px] text-neutral-500 lg:px-6">
        <span>خانه</span><span className="mx-1">/</span><span className="text-neutral-100 font-bold">{store.name}</span>
      </nav>
      <div className="mx-auto w-full max-w-6xl space-y-12 p-4 lg:p-6">
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
export default FashionTemplate;
