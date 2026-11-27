import { StorefrontCatalogue } from './StorefrontCatalogue';
import { StorefrontHero } from './StorefrontHero';
import React from 'react';
import type { Product } from '../../../types';
import type { StorefrontTemplateProps, TemplateConfig, TemplateBanner } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  sectionEnabled,
  bannersOf,
  templateCustom,
  customText,
  customCount,
  storefrontProducts,
} from './templateTypes';
import {
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
  SectionTitle,
  groupProductsByCategory,
  StorefrontCategoryLink,
  discountedProducts,
} from './StoreSections';
import type { CategoryGroup } from './StoreSections';
import { TemplateProductGrid } from './TemplateProductGrid';
import { toPersianDigits } from '../../../utils/formatters';
import {
  Flame,
  Zap,
  Store as StoreIcon,
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
  <section data-testid="store-category-tiles" aria-label="دسته‌بندی‌ها" className="space-y-3">
    <div className="flex items-center justify-between gap-2">
      <SectionTitle accentColor={accentColor}>{title}</SectionTitle>
      <span className="shrink-0 text-[11px] font-bold text-slate-400">
        {toPersianDigits(groups.length)} دسته‌بندی
      </span>
    </div>
    <div className="no-scrollbar -mx-1 flex snap-x gap-3 overflow-x-auto px-1 pb-1 lg:mx-0 lg:grid lg:grid-cols-6 lg:gap-4 lg:overflow-visible lg:px-0 lg:pb-0">
      {groups.slice(0, 12).map((group) => (
        <StorefrontCategoryLink key={group.slug} slug={group.slug}
          className="group flex w-[74px] shrink-0 snap-start flex-col items-center gap-1.5 text-center lg:w-auto"
        >
          <div
            className={`flex h-16 w-16 items-center justify-center overflow-hidden rounded-full bg-gradient-to-br ring-1 ring-slate-200 transition duration-300 group-hover:shadow-md group-hover:ring-2 lg:h-20 lg:w-20 ${group.gradient}`}
            style={{ ['--tw-ring-color' as string]: accentColor } as React.CSSProperties}
          >
            {categoryImage(group) ? <img
              src={categoryImage(group)}
              alt="" width={80} height={80}
              className="h-full w-full object-cover transition duration-300 group-hover:scale-110"
              loading="lazy"
            /> : <span className="text-lg font-bold">{group.title.slice(0, 1)}</span>}
          </div>
          <span className="w-full truncate text-[11px] font-bold text-slate-700 transition group-hover:text-slate-950">
            {group.title}
          </span>
          <span className="-mt-1 text-xs text-slate-600">{toPersianDigits(group.count)} کالا</span>
        </StorefrontCategoryLink>
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
    <a href={banner.link} {...(/^https?:\/\//i.test(banner.link) ? { target: '_blank', rel: 'noreferrer' } : {})} className="block h-full">
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

export const DigikalaTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories, productFeed }) => {
  const cfg = resolveTemplateConfig('tpl-digikala-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const custom = templateCustom(cfg);
  const banners = bannersOf(cfg);
  const categoryGroups = groupProductsByCategory(items, categories, custom.categoryOverrides);
  const seen = new Set<string>();
  const once = (group: string): boolean => {
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  };

  const renderHero = () => (
    <StorefrontHero store={store} config={cfg} products={items} aside={
      <aside className="marketplace-details">
        <div className="marketplace-seller">
          <StoreIcon size={20} aria-hidden="true" />
          <div><strong>{store.ownerName || store.name}</strong>{store.city && <p>{store.city}</p>}</div>
        </div>
        <dl>
          <div><dt>کالاهای ویترین</dt><dd>{toPersianDigits(items.length)}</dd></div>
          <div><dt>دسته‌بندی</dt><dd>{toPersianDigits(categoryGroups.length)}</dd></div>
          {store.totalSales > 0 && <div><dt>فروش ثبت‌شده</dt><dd>{toPersianDigits(store.totalSales)}</dd></div>}
        </dl>
        {order.includes('deals') && discountedProducts(items).length > 0 && (
          <a className="marketplace-promotion" href="#marketplace-deals">
            <Flame size={16} aria-hidden="true" />{custom.heroFallbackBanner1 || custom.heroFallbackBanner3 || 'مشاهده پیشنهادها'}
          </a>
        )}
      </aside>
    } />
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
        return null;
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
        return categoryGroups.length ? (
          <Panel>
            <CategoryTiles groups={categoryGroups}
              title={customText(cfg, 'categoriesTitle', 'دسته‌بندی‌ها')} accentColor={accentColor} />
          </Panel>
        ) : null;
      case 'banners':
      case 'promo':
        return once('banners') ? renderBanners() : null;
      case 'deals':
        return <div id="marketplace-deals" className="empty:hidden"><DealsPanel products={items} config={cfg} accentColor={accentColor} /></div>;
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
          <StorefrontCatalogue items={items} categoryGroups={categoryGroups}
            productsTitle={customText(cfg, 'productsTitle', 'همه کالاها')} productFeed={productFeed} />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <main className="fashion-storefront storefront-showcase" data-storefront-theme="tpl-digikala-1"
      style={{ '--tpl-accent': accentColor } as React.CSSProperties}>
      {!order.some(key => key === 'hero' || key === 'heroSlides') && <h1 className="sr-only">{store.name}</h1>}
      <div className="showcase-sections">
        {order.map(key => <React.Fragment key={key}>{renderSection(key)}</React.Fragment>)}
      </div>
    </main>
  );
};
export default DigikalaTemplate;
