import { useEffect, useId, useMemo, useState } from 'react';
import { Search, SlidersHorizontal, X } from 'lucide-react';
import type { Product } from '../../../types';
import { useStore } from '../../../context/StoreContext';
import { toPersianDigits } from '../../../utils/formatters';
import { normalizePersianText } from '../../../utils/routing';
import { cardPriceOf } from '../../../utils/cardPrice';
import { activeProductVariants, resolvePurchase } from '../../../utils/productPurchase';
import { RouteLoading } from '../../common/RouteLoading';
import { FashionProductCard } from '../FashionProductCard';
import { storefrontProducts, type StorefrontTemplateProps } from './templateTypes';
import { TemplateProductGrid } from './TemplateProductGrid';
import type { CategoryGroup } from './StoreSections';
import { useStorefrontProducts } from '../useStorefrontProducts';

const SORT_OPTIONS = {
  featured: 'پیشنهادی فروشگاه',
  cheapest: 'کمترین قیمت',
  expensive: 'بیشترین قیمت',
  discount: 'بیشترین تخفیف',
} as const;

function normalizeSearch(value: string) {
  return normalizePersianText(value).replace(/[\s\u200c]+/g, ' ').trim().toLocaleLowerCase('fa-IR');
}

export function StorefrontCatalogue({ items, categoryGroups, productsTitle, productFeed, editorial = false, showAvailabilityFilter = false }: {
  items: Product[];
  editorial?: boolean;
  showAvailabilityFilter?: boolean;
  categoryGroups: CategoryGroup[];
  productsTitle: string;
  productFeed?: StorefrontTemplateProps['productFeed'];
}) {
  const { openProductDetail, selectedCategory, setSelectedCategory, categories = [],
    catalogBrandRequest, clearCatalogBrand, searchQuery, setSearchQuery } = useStore();
  const [floorSort, setFloorSort] = useState<keyof typeof SORT_OPTIONS>('featured');
  const [availableOnly, setAvailableOnly] = useState(false);
  const [searchDraft, setSearchDraft] = useState(searchQuery);
  useEffect(() => setSearchDraft(searchQuery), [catalogBrandRequest, searchQuery, selectedCategory]);
  const catalogueHeadingId = useId();
  const categoryChoices = useMemo(() => {
    const choices = new Map(categoryGroups.map(({ slug, title }) => [slug, title]));
    for (const slug of productFeed?.categorySlugs ?? []) {
      if (!choices.has(slug)) choices.set(slug, productFeed?.categoryLabels?.[slug] ?? categories.find(category => category.slug === slug)?.title ?? slug);
    }
    return [...choices].map(([slug, title]) => ({ slug, title }));
  }, [categories, categoryGroups, productFeed?.categoryLabels, productFeed?.categorySlugs]);
  const selectedGroup = categoryChoices.find(group => group.slug === selectedCategory);
  const category = selectedCategory !== 'all' && (productFeed || selectedGroup) ? selectedCategory : undefined;
  const brandFilter = catalogBrandRequest?.brand;
  const term = searchQuery.trim();
  const searching = term.length > 0;
  const filtered = Boolean(brandFilter || category || searching || availableOnly || floorSort !== 'featured');
  const filteredStore = filtered ? productFeed?.store : undefined;
  const filters = useMemo(() => ({
    category, search: term, sort: floorSort, inStockOnly: availableOnly, brands: brandFilter ? [brandFilter] : undefined,
  }), [availableOnly, brandFilter, category, floorSort, term]);
  const filteredFeed = useStorefrontProducts(filteredStore?.slug, filters);
  const feed = filteredStore ? filteredFeed : productFeed;
  const floorItems = useMemo(() => {
    if (filteredStore) return storefrontProducts(filteredStore, filteredFeed.items);
    if (productFeed) return items;
    const query = normalizeSearch(term);
    const preview = items.filter(product => {
      if (category && product.category !== category) return false;
      if (brandFilter && (product.brand || '').trim() !== brandFilter) return false;
      if (query && !normalizeSearch([product.title, product.brand, product.description].filter(Boolean).join(' ')).includes(query)) return false;
      if (!availableOnly) return true;
      return product.variants?.length
        ? activeProductVariants(product).some(variant => Boolean(resolvePurchase(product, variant.id)))
        : Boolean(resolvePurchase(product));
    });
    if (floorSort === 'featured') return preview;
    return preview.sort((a, b) => {
      if (floorSort === 'discount') return cardPriceOf(b).discountPercent - cardPriceOf(a).discountPercent;
      const difference = cardPriceOf(a).price - cardPriceOf(b).price;
      return floorSort === 'cheapest' ? difference : -difference;
    });
  }, [availableOnly, brandFilter, category, filteredFeed.items, filteredStore, floorSort, items, productFeed, term]);
  const floorTitle = searching ? `جستجو: «${searchQuery}»` : brandFilter || (selectedGroup ? selectedGroup.title : productsTitle);
  const selectCategory = (slug: string): void => {
    clearCatalogBrand();
    setSelectedCategory(slug);
  };
  const clearSearch = () => {
    setSearchDraft('');
    setSearchQuery('');
  };
  const clearFloorFilter = () => {
    clearCatalogBrand();
    setSelectedCategory('all');
    clearSearch();
    setFloorSort('featured');
    setAvailableOnly(false);
  };

  return (
    <section id="products" data-testid="store-products" aria-labelledby={catalogueHeadingId} className="fashion-catalogue">
      <div className="fashion-catalogue-heading">
        <div className="fashion-catalogue-title"><h2 id={catalogueHeadingId}>{floorTitle}</h2>
          {!feed?.loading && !feed?.error && <span className="fashion-result-count" role="status">{toPersianDigits(feed?.total ?? floorItems.length)} کالا</span>}
        </div>
        {filtered ? (
          <button
            type="button"
            onClick={clearFloorFilter}
            className="fashion-text-action"
          >
            نمایش همه کالاها
          </button>
        ) : null}
      </div>
      <div className="fashion-catalogue-tools">
        <form role="search" className="fashion-search-field" onSubmit={event => { event.preventDefault(); setSearchQuery(searchDraft.trim()); }}>
          <button type="submit" aria-label="نمایش نتایج جست‌وجو"><Search size={18} aria-hidden="true" /></button>
          <input id="fashion-search" type="search" name="q" maxLength={256} autoComplete="off" enterKeyHint="search" aria-label="جست‌وجو در ویترین" placeholder="نام کالا یا برند…" value={searchDraft}
            onChange={event => {
              setSearchDraft(event.target.value);
              if (event.target.value === '') setSearchQuery('');
            }} />
          {searchDraft && <button type="button" aria-label="پاک کردن جست‌وجو" onClick={clearSearch}><X size={17} aria-hidden="true" /></button>}
        </form>
        <label className="fashion-sort-field">
          <SlidersHorizontal size={16} aria-hidden="true" />
          <span className="sr-only">مرتب‌سازی محصولات</span>
          <select value={floorSort} onChange={event => setFloorSort(event.target.value as typeof floorSort)}>
            {Object.entries(SORT_OPTIONS).map(([id, label]) => <option key={id} value={id}>{label}</option>)}
          </select>
        </label>
        {showAvailabilityFilter && <label className="modern-availability">
          <input type="checkbox" checked={availableOnly} onChange={event => setAvailableOnly(event.target.checked)} />
          فقط کالاهای موجود
        </label>}
      </div>
      {categoryChoices.length > 1 && <nav className="fashion-catalogue-filters" aria-label="فیلتر دسته‌بندی محصولات">
        <button type="button" aria-pressed={!category && !brandFilter} onClick={() => selectCategory('all')}>همه</button>
        {categoryChoices.map(group => <button key={group.slug} data-category={group.slug} type="button" aria-pressed={category === group.slug && !brandFilter}
          onClick={() => selectCategory(group.slug)}>{group.title}</button>)}
      </nav>}
      {feed?.loading ? <RouteLoading label="در حال دریافت کالاها…" /> : floorItems.length === 0 ? (
        !feed?.error && <div className="fashion-empty">
          <p>{filtered
            ? 'کالایی با این مشخصات یافت نشد.' : 'هنوز کالایی در این ویترین ثبت نشده است.'}</p>
          {filtered && <button type="button" className="fashion-text-action" onClick={clearFloorFilter}>پاک کردن فیلترها</button>}
        </div>
      ) : editorial ? (
          <div className="fashion-product-grid">
            {floorItems.map((p) => (
              <FashionProductCard key={p.id} product={p} onOpen={openProductDetail} />
            ))}
          </div>
      ) : <TemplateProductGrid products={floorItems} columnsClass="fashion-product-grid" />}
      {feed?.error && <div className="fashion-empty" role="status">
        <p>{feed.error}</p>
        <button type="button" className="fashion-text-action" onClick={feed.retry}>تلاش مجدد</button>
      </div>}
      {feed?.hasMore && !feed.error && <button type="button" className="fashion-button fashion-load-more" disabled={feed.loadingMore} onClick={feed.loadMore}>
        {feed.loadingMore ? 'در حال دریافت…' : 'نمایش کالاهای بیشتر'}
      </button>}
    </section>
  );
}
