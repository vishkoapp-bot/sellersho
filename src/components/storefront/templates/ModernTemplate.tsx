import { StorefrontCatalogue } from './StorefrontCatalogue';
import { StorefrontHero } from './StorefrontHero';
import React, { useId, useState } from 'react';
import { ArrowLeft, LayoutGrid, Package, Star } from 'lucide-react';
import type { Product } from '../../../types';
import type { StorefrontTemplateProps, TemplateConfig } from './templateTypes';
import {
  resolveTemplateConfig,
  orderedEnabledSections,
  templateCustom,
  customText,
  customCount,
  storefrontProducts,
} from './templateTypes';
import {
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
  FaqSection,
  TestimonialsSection,
  LookbookSection,
  groupProductsByCategory,
  StorefrontCategoryLink,
} from './StoreSections';
import type { CategoryGroup } from './StoreSections';
import { TemplateProductGrid } from './TemplateProductGrid';
import { activeStoreSlugOf, useStore } from '../../../context/StoreContext';
import { formatPrice, toPersianDigits } from '../../../utils/formatters';
import { cardPriceOf } from '../../../utils/cardPrice';
import { buildProductUrl } from '../../../utils/routing';
import { followStoreLink, followStoreSection } from '../../../utils/followStoreLink';
import { FallbackImage } from '../../common/FallbackImage';

// Config-driven storefront: sections render in the order and
// enablement stored in the resolved template config (template
// blueprint merged under the seller's saved config); every text,
// image and link comes from the seller-editable custom config or
// the store's own data — nothing is hardcoded per store.
//
// Modern identity: warm paper background, editorial split hero,
// image-led category mosaic and a large featured spotlight card —
// a calm, premium D2C boutique feel on top of the shared sections.

const productImage = (p: Product): string => {
  const fromField = (p.image || '').trim();
  if (fromField) return fromField;
  const first = Array.isArray(p.images)
    ? p.images.find((x) => typeof x === 'string' && x.trim() !== '')
    : undefined;
  return first || '';
};

function CategoryMosaic({ groups, config, hasCatalogue }: {
  groups: CategoryGroup[];
  config: TemplateConfig;
  hasCatalogue: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const categoriesId = useId();
  if (groups.length === 0) return null;
  const shown = groups.slice(0, customCount(config, 'categoriesCount', 8));
  return (
    <section data-testid="store-category-mosaic" aria-label="دسته‌بندی‌ها" className="modern-section">
      <div className="modern-section-heading">
        <h2>{customText(config, 'categoriesTitle', 'دسته‌بندی‌ها')}</h2>
        {hasCatalogue && <a href="#products" className="modern-text-link" onClick={followStoreSection}>فهرست کالاها <ArrowLeft size={16} aria-hidden="true" /></a>}
      </div>
      <div className="modern-category-grid" id={categoriesId} data-single={shown.length === 1} data-expanded={expanded}>
        {shown.map(group => {
          const images = [group.image, ...group.products.map(productImage)];
          const content = <>
            <div className="modern-category-image">
              <FallbackImage srcs={images} width={440} height={330} alt="" loading="lazy"
                fallback={<LayoutGrid aria-hidden="true" size={30} strokeWidth={1.4} />} />
            </div>
            <div className="modern-category-caption">
              <div><h3>{group.title}</h3><p>{toPersianDigits(group.count)} کالا</p></div>
              {hasCatalogue && <ArrowLeft size={17} aria-hidden="true" />}
            </div>
          </>;
          return hasCatalogue ? (
            <StorefrontCategoryLink key={group.slug} slug={group.slug} data-category={group.slug} className="modern-category">{content}</StorefrontCategoryLink>
          ) : <div key={group.slug} className="modern-category">{content}</div>;
        })}
      </div>
      {shown.length > 4 && <button type="button" className="modern-text-link modern-category-expand"
        aria-expanded={expanded} aria-controls={categoriesId} onClick={() => setExpanded(value => !value)}>
        {expanded ? 'نمایش دسته‌های کمتر' : 'نمایش همه دسته‌ها'}
      </button>}
    </section>
  );
}

function FeaturedSpotlight({ products, config }: { products: Product[]; config: TemplateConfig }) {
  const { currentStore, routeStoreSlug, openProductDetail } = useStore();
  const [featured, ...rest] = products.filter(product => product.featured).slice(0, customCount(config, 'featuredCount', 10));
  const lead = featured ?? products.find(product => product.stock > 0 && productImage(product));
  if (!lead) return null;
  const price = cardPriceOf(lead);
  const href = buildProductUrl(lead, activeStoreSlugOf({ currentStore, routeStoreSlug }) ?? undefined);
  return (
    <section data-testid="store-featured" className="modern-section">
      <div className="modern-section-heading">
        <h2>{customText(config, 'featuredTitle', featured ? 'کالاهای منتخب' : 'نگاهی به کالاهای ویترین')}</h2>
      </div>
      <article className="modern-spotlight">
        <div className="modern-spotlight-image">
          <FallbackImage srcs={[productImage(lead), ...(lead.images ?? [])]} width={650} height={500} alt=""
            loading="lazy" fallback={<Package size={44} strokeWidth={1.3} aria-hidden="true" />} />
        </div>
        <div className="modern-spotlight-copy">
          <h3>{lead.title}</h3>
          {lead.description && <p className="line-clamp-3">{lead.description}</p>}
          {lead.rating > 0 && <p className="flex items-center gap-1.5">
            <Star size={15} aria-hidden="true" />
            امتیاز {toPersianDigits(lead.rating)} از ۵
            {lead.reviewsCount > 0 && ` (${toPersianDigits(lead.reviewsCount)} دیدگاه)`}
          </p>}
          <div className="modern-spotlight-price">
            <strong>{formatPrice(price.price)} <small>تومان</small></strong>
            {price.discountPercent > 0 && price.originalPrice > price.price &&
              <del>{formatPrice(price.originalPrice)} تومان</del>}
          </div>
          <a href={href} onClick={event => followStoreLink(event, () => openProductDetail(lead))} className="modern-text-link">
            مشاهدهٔ جزئیات کالا <ArrowLeft size={18} aria-hidden="true" />
          </a>
        </div>
      </article>
      {rest.length > 0 && <TemplateProductGrid products={rest} columnsClass="grid-cols-2 lg:grid-cols-4" />}
    </section>
  );
}

export const ModernTemplate: React.FC<StorefrontTemplateProps> = ({ store, products, accentColor, config, categories, productFeed }) => {
  const cfg = resolveTemplateConfig('tpl-modern-1', config);
  const items = storefrontProducts(store, products);
  const order = orderedEnabledSections(cfg);
  const custom = templateCustom(cfg);
  const seen = new Set<string>();
  const once = (group: string): boolean => {
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  };

  const categoryGroups = groupProductsByCategory(items, categories, custom.categoryOverrides);

  const hasCatalogue = order.includes('products') || order.includes('allProducts');
  const renderHero = () => <StorefrontHero store={store} config={cfg} products={items} showSearchShortcut />;

  const renderSection = (key: string): React.ReactNode => {
    switch (key) {
      case 'announcement':
        return null;
      case 'hero':
      case 'heroSlides':
        return once('hero') ? renderHero() : null;
      case 'trustStrip':
        return <TrustStripSection config={cfg} accentColor={accentColor} />;
      case 'categoryRail':
        return <CategoryMosaic groups={categoryGroups} config={cfg} hasCatalogue={hasCatalogue} />;
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
        return <FeaturedSpotlight products={items} config={cfg} />;
      case 'bestsellers':
        return <BestsellersSection products={items} config={cfg} accentColor={accentColor} />;
      case 'discounted':
        return <DiscountedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'topRated':
        return <TopRatedSection products={items} config={cfg} accentColor={accentColor} />;
      case 'about':
        return (
          <AboutSection store={store} config={cfg} accentColor={accentColor} />
        );
      case 'campaign':
        return <CampaignSection store={store} products={items} config={cfg} accentColor={accentColor} />;
      case 'testimonials':
        return <TestimonialsSection config={cfg} accentColor={accentColor} />;
      case 'faq':
        return <FaqSection config={cfg} accentColor={accentColor} />;
      case 'lookbook':
        return <LookbookSection config={cfg} accentColor={accentColor} />;
      case 'products':
      case 'allProducts':
        return once('products') ? (
          <StorefrontCatalogue showAvailabilityFilter items={items} categoryGroups={categoryGroups}
            productsTitle={customText(cfg, 'productsTitle', 'همه کالاها')} productFeed={productFeed} />
        ) : null;
      default:
        return null;
    }
  };

  return (
    <main className="fashion-storefront storefront-showcase modern-showcase" data-storefront-theme="tpl-modern-1"
      style={{ '--tpl-accent': accentColor } as React.CSSProperties}>
      {!order.some(key => key === 'hero' || key === 'heroSlides') && <h1 className="sr-only">{store.name}</h1>}
      <div className="showcase-sections modern-sections">
        {order.map(key => <React.Fragment key={key}>{renderSection(key)}</React.Fragment>)}
      </div>
    </main>
  );
};
