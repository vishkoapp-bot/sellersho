import React from 'react';
import type { Store, Product } from '../../../types';
import type { StorefrontTemplateProps, TemplateConfig, TemplateBanner } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  sectionEnabled,
  heroSlidesOf,
  bannersOf,
  templateCustom,
  customText,
  customCount,
  storefrontProducts,
} from './templateTypes';
import {
  AnnouncementSection,
  HeroSlider,
  CategoryRail,
  BannerRow,
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
  SectionTitle,
  groupProductsByCategory,
  discountedProducts,
} from './StoreSections';
import type { CategoryGroup } from './StoreSections';
import { TemplateProductGrid } from './TemplateProductGrid';
import { toPersianDigits } from '../../../utils/formatters';
import {
  Flame,
  Zap,
  Store as StoreIcon,
  BadgeCheck,
  BadgePercent,
  MapPin,
  Package,
  LayoutGrid,
  TrendingUp,
  ChevronLeft,
  ArrowLeft,
} from 'lucide-react';

// Digikala-style storefront: a dense, professional marketplace.
// Every section is a white panel on the light-grey marketplace canvas,
// sections render in the order and enablement stored in the resolved
// template config (blueprint merged under the seller's saved config),
// and every text, image and number comes from the seller-editable
// custom config or the store's own live data — nothing is fabricated.

const PANEL = 'rounded-2xl bg-white p-3 shadow-sm lg:p-4';

// White panel wrapper. `empty:hidden` keeps the panel invisible while
// the wrapped section self-hides (no data yet / async content still
// loading), so a store never shows empty white shells.
const Panel: React.FC<{ children: React.ReactNode; className?: string; id?: string }> = ({
  children,
  className = '',
  id,
}) => (
  <div id={id} className={`${PANEL} empty:hidden ${className}`}>
    {children}
  </div>
);

// First real image of a category group: the category's own image wins,
// otherwise the first product's image — never a placeholder.
function categoryImage(group: CategoryGroup): string {
  if (group.image) return group.image;
  const first = group.products[0];
  if (!first) return '';
  return first.image || (first.images && first.images[0]) || '';
}

// --- Category tiles (Digikala-style round tiles) ---------------------
// Circular image tiles with name + real product count. Mobile: a
// snap-scroll rail; desktop: a 6-column grid inside the panel.
const CategoryTiles: React.FC<{ groups: CategoryGroup[]; title: string; accentColor: string }> = ({
  groups,
  title,
  accentColor,
}) => (
  <section data-testid="store-category-tiles" className="space-y-3">
    <div className="flex items-center justify-between gap-2">
      <SectionTitle accentColor={accentColor}>{title}</SectionTitle>
      <span className="shrink-0 text-[11px] font-bold text-slate-400">
        {toPersianDigits(groups.length)} دسته‌بندی
      </span>
    </div>
    <div className="no-scrollbar -mx-1 flex snap-x gap-3 overflow-x-auto px-1 pb-1 lg:mx-0 lg:grid lg:grid-cols-6 lg:gap-4 lg:overflow-visible lg:px-0 lg:pb-0">
      {groups.slice(0, 12).map((group) => (
        <div
          key={group.slug}
          className="group flex w-[74px] shrink-0 snap-start flex-col items-center gap-1.5 text-center lg:w-auto"
        >
          <div
            className={`flex h-16 w-16 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br ring-1 ring-slate-200 transition duration-300 group-hover:shadow-md group-hover:ring-2 lg:h-20 lg:w-20 ${group.gradient}`}
            style={{ ['--tw-ring-color' as string]: accentColor } as React.CSSProperties}
          >
            <img
              src={categoryImage(group)}
              alt={group.title}
              className="h-full w-full object-cover transition duration-300 group-hover:scale-110"
              loading="lazy"
            />
          </div>
          <span className="w-full truncate text-[11px] font-bold text-slate-700 transition group-hover:text-slate-950">
            {group.title}
          </span>
          <span className="-mt-1 text-[10px] text-slate-400">{toPersianDigits(group.count)} کالا</span>
        </div>
      ))}
    </div>
  </section>
);

// --- Banner mosaic (1 wide + 2 small, asymmetric) --------------------
const BannerTile: React.FC<{ banner: TemplateBanner }> = ({ banner }) => {
  const body = (
    <div className="group relative block h-full w-full overflow-hidden rounded-2xl bg-slate-100 shadow-sm">
      <img
        src={banner.image}
        alt={banner.title || ''}
        className="h-full w-full object-cover transition duration-500 group-hover:scale-105"
        loading="lazy"
      />
      {banner.title ? (
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/70 via-black/25 to-transparent p-3 pt-8">
          <p className="text-xs font-black text-white lg:text-sm">{banner.title}</p>
        </div>
      ) : null}
    </div>
  );
  return banner.link ? (
    <a href={banner.link} target="_blank" rel="noreferrer" className="block h-full">
      {body}
    </a>
  ) : (
    body
  );
};

const BannerMosaic: React.FC<{ banners: TemplateBanner[] }> = ({ banners }) => {
  if (banners.length === 1) {
    return (
      <section data-testid="store-banners">
        <div className="h-40 lg:h-56">
          <BannerTile banner={banners[0]} />
        </div>
      </section>
    );
  }
  if (banners.length === 2) {
    return (
      <section data-testid="store-banners" className="grid grid-cols-2 gap-3">
        {banners.map((banner, i) => (
          <div key={`${banner.image}-${i}`} className="h-32 lg:h-44">
            <BannerTile banner={banner} />
          </div>
        ))}
      </section>
    );
  }
  return (
    <section data-testid="store-banners" className="grid grid-cols-2 gap-3 lg:grid-cols-3">
      <div className="col-span-2 h-44 lg:row-span-2 lg:h-auto lg:min-h-[300px]">
        <BannerTile banner={banners[0]} />
      </div>
      <div className="h-32 lg:h-[144px]">
        <BannerTile banner={banners[1]} />
      </div>
      <div className="h-32 lg:h-[144px]">
        <BannerTile banner={banners[2]} />
      </div>
    </section>
  );
};

// --- Amazing deals (accent panel) ------------------------------------
// Same data path as the shared DealsSection (discountedProducts +
// TemplateProductGrid), rendered inside the signature red-tinted panel
// with a local pill header so the title never renders twice.
const DealsPanel: React.FC<{ products: Product[]; config: TemplateConfig; accentColor: string }> = ({
  products,
  config,
  accentColor,
}) => {
  if (!sectionEnabled(config, 'deals')) return null;
  const deals = discountedProducts(products).slice(0, customCount(config, 'dealsCount', 10));
  if (deals.length === 0) return null;
  const title = customText(config, 'dealsTitle', 'پیشنهاد شگفت‌انگیز');
  return (
    <section
      data-testid="store-deals"
      className="rounded-2xl border border-rose-100 p-3 shadow-sm lg:p-4"
      style={{ backgroundColor: '#fff5f6', borderColor: `${accentColor}40` }}
    >
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <span
          className="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-black text-white shadow-sm"
          style={{ backgroundColor: accentColor }}
        >
          <Flame size={14} strokeWidth={2.2} />
          {title}
        </span>
        <span className="inline-flex items-center gap-1 text-[11px] font-bold" style={{ color: accentColor }}>
          <Zap size={12} />
          {toPersianDigits(deals.length)} کالای تخفیف‌دار
        </span>
      </div>
      <TemplateProductGrid products={deals} columnsClass="grid-cols-2 lg:grid-cols-5" />
    </section>
  );
};

export const DigikalaTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories }) => {
  const cfg = resolveTemplateConfig('tpl-digikala-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const slides = heroSlidesOf(cfg);
  const custom = templateCustom(cfg);
  const banners = bannersOf(cfg);
  const categoryGroups = groupProductsByCategory(items, categories, custom.categoryOverrides);
  const imagedGroups = categoryGroups.filter((group) => categoryImage(group) !== '');
  const seen = new Set<string>();
  const once = (group: string): boolean => {
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  };

  // Store identity card: logo, name, tagline, seller line, city and
  // real storefront stats (product / category counts, sales when the
  // store has any, otherwise its discounted-product count).
  const renderStoreCard = (): React.ReactNode => {
    const stats: { icon: React.ReactNode; value: string; label: string }[] = [
      { icon: <Package size={13} />, value: toPersianDigits(items.length), label: 'کالا' },
      { icon: <LayoutGrid size={13} />, value: toPersianDigits(categoryGroups.length), label: 'دسته‌بندی' },
    ];
    if (store.totalSales > 0) {
      stats.push({ icon: <TrendingUp size={13} />, value: toPersianDigits(store.totalSales), label: 'فروش' });
    } else {
      const dealCount = discountedProducts(items).length;
      if (dealCount > 0) {
        stats.push({ icon: <BadgePercent size={13} />, value: toPersianDigits(dealCount), label: 'تخفیف‌دار' });
      }
    }
    const ownerLine = (store.ownerName || '').trim();
    return (
      <div className={`${PANEL} flex flex-col`}>
        <div className="flex items-start gap-3">
          <div className="flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden rounded-2xl border border-slate-100 bg-slate-50">
            {store.logo ? (
              <img src={store.logo} alt={store.name} className="h-full w-full object-contain p-1" />
            ) : (
              <StoreIcon size={26} style={{ color: accentColor }} />
            )}
          </div>
          <div className="min-w-0 flex-1">
            <h1 className="truncate text-base font-black text-slate-900">{store.name}</h1>
            {store.tagline ? (
              <p className="mt-0.5 line-clamp-2 text-[11px] leading-5 text-slate-500">{store.tagline}</p>
            ) : null}
            {ownerLine && ownerLine !== store.name.trim() ? (
              <p className="mt-1 inline-flex items-center gap-1 text-[11px] font-bold text-slate-600">
                <BadgeCheck size={13} style={{ color: accentColor }} />
                {ownerLine}
              </p>
            ) : null}
          </div>
          {store.city ? (
            <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-slate-100 px-2 py-1 text-[10px] font-bold text-slate-600">
              <MapPin size={11} />
              {store.city}
            </span>
          ) : null}
        </div>
        <div className="mt-3 grid gap-2" style={{ gridTemplateColumns: `repeat(${stats.length}, minmax(0, 1fr))` }}>
          {stats.map((stat) => (
            <div key={stat.label} className="rounded-xl bg-slate-50 px-2 py-2 text-center">
              <p className="flex items-center justify-center gap-1 text-sm font-black text-slate-900">
                <span style={{ color: accentColor }}>{stat.icon}</span>
                {stat.value}
              </p>
              <p className="mt-0.5 text-[10px] text-slate-400">{stat.label}</p>
            </div>
          ))}
        </div>
        <a
          href={custom.heroCtaLink || '#products'}
          className="mt-3 inline-flex items-center justify-center gap-1 rounded-xl py-2 text-xs font-black text-white transition hover:opacity-90"
          style={{ backgroundColor: accentColor }}
        >
          {custom.heroCtaText || 'مشاهده همه کالاها'}
          <ArrowLeft size={13} />
        </a>
      </div>
    );
  };

  // Second block of the hero side column: the seller's first configured
  // banner as an image tile, or — with no banner — a short excerpt of
  // the store's own description. Nothing renders when neither exists.
  const renderHeroSideExtra = (): React.ReactNode => {
    const firstBanner = banners[0];
    if (firstBanner && firstBanner.image) {
      return (
        <div className="min-h-[7rem] flex-1">
          <BannerTile banner={firstBanner} />
        </div>
      );
    }
    const excerpt = (store.description || store.shortDescription || '').trim();
    if (!excerpt) return null;
    return (
      <div className={`${PANEL} flex-1`}>
        <SectionTitle accentColor={accentColor}>{customText(cfg, 'aboutTitle', 'درباره فروشگاه')}</SectionTitle>
        <p className="mt-2 line-clamp-4 text-[11px] leading-5 text-slate-500">{excerpt}</p>
      </div>
    );
  };

  const renderHero = (): React.ReactNode => (
    <div className="grid gap-3 lg:grid-cols-3">
      <div className="min-w-0 overflow-hidden rounded-2xl shadow-sm lg:col-span-2">
        {slides.length > 0 ? (
          <HeroSlider slides={slides} accentColor={accentColor} autoplay={custom.heroAutoplay} />
        ) : (
          <HeroFallbackBanners config={cfg} accentColor={accentColor} />
        )}
      </div>
      <div className="flex flex-col gap-3">
        {renderStoreCard()}
        {renderHeroSideExtra()}
      </div>
    </div>
  );

  const renderBanners = (): React.ReactNode => {
    if (banners.length >= 1 && banners.length <= 3) {
      return <BannerMosaic banners={banners} />;
    }
    return <BannerRow config={cfg} />;
  };

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return <AnnouncementSection store={store} config={cfg} accentColor={accentColor} />;
      case 'hero':
      case 'heroSlides':
        return once('hero') ? renderHero() : null;
      case 'trustStrip':
        return (
          <Panel>
            <TrustStripSection config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'categoryRail':
        return imagedGroups.length > 0 ? (
          <Panel>
            <CategoryTiles
              groups={imagedGroups}
              title={customText(cfg, 'categoriesTitle', 'دسته‌بندی‌ها')}
              accentColor={accentColor}
            />
          </Panel>
        ) : (
          <Panel>
            <CategoryRail products={items} categories={categories} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'banners':
      case 'promo':
        return once('banners') ? renderBanners() : null;
      case 'deals':
        return <DealsPanel products={items} config={cfg} accentColor={accentColor} />;
      case 'timedDeals':
        return <TimedDealsSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'brands':
        return (
          <Panel>
            <BrandsRail products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'featured':
        return (
          <Panel>
            <FeaturedSection products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'bestsellers':
        return (
          <Panel>
            <BestsellersSection products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'discounted':
        return (
          <Panel>
            <DiscountedSection products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'topRated':
        return (
          <Panel>
            <TopRatedSection products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'about':
        return (
          <Panel>
            <AboutSection store={store} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'campaign':
        return (
          <Panel>
            <CampaignSection store={store} products={items} config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'newsletter':
        return (
          <Panel>
            <NewsletterSection config={cfg} accentColor={accentColor} storeSlug={store.slug} />
          </Panel>
        );
      case 'testimonials':
        return (
          <Panel>
            <TestimonialsSection config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'faq':
        return (
          <Panel>
            <FaqSection config={cfg} accentColor={accentColor} />
          </Panel>
        );
      case 'lookbook':
        return <LookbookSection config={cfg} accentColor={accentColor} />;
      case 'products':
      case 'allProducts':
        return once('products') ? (
          <Panel id="products">
            <ProductsGridSection products={items} config={cfg} accentColor={accentColor} columnsClass="grid-cols-2 lg:grid-cols-5" />
          </Panel>
        ) : null;
      default:
        return null;
    }
  };

  return (
    <div className="flex flex-1 flex-col bg-[#f0f0f1]" data-template-desktop="lg:grid" style={{ ['--tpl-accent' as string]: accentColor } as React.CSSProperties}>
      <div className="mx-auto w-full max-w-[1400px] space-y-3 p-3 lg:space-y-4 lg:p-4">
        <nav aria-label="مسیر صفحه" className="flex items-center gap-1 px-1 text-[11px] text-slate-400">
          <span>خانه</span>
          <ChevronLeft size={11} />
          <span className="font-bold text-slate-700">{store.name}</span>
        </nav>
        {order.map((key) => (
          <React.Fragment key={key}>{renderSection(key)}</React.Fragment>
        ))}
      </div>
    </div>
  );
};
export default DigikalaTemplate;
