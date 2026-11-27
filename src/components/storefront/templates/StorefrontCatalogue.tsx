import { useId, useMemo, useState } from 'react';
import { Search, SlidersHorizontal, X } from 'lucide-react';
import type { Product } from '../../../types';
import { useStore } from '../../../context/StoreContext';
import { toPersianDigits } from '../../../utils/formatters';
import { normalizePersianText } from '../../../utils/routing';
import { cardPriceOf } from '../../../utils/cardPrice';
import { RouteLoading } from '../../common/RouteLoading';
import { FashionProductCard } from '../FashionProductCard';
import type { StorefrontTemplateProps } from './templateTypes';
import { TemplateProductGrid } from './TemplateProductGrid';
import type { CategoryGroup } from './StoreSections';

const SORT_OPTIONS = {
  featured: 'محبوب‌ترین',
  cheapest: 'کمترین قیمت',
  expensive: 'بیشترین قیمت',
  discount: 'بیشترین تخفیف',
} as const;

function normalizeSearch(value: string) {
  return normalizePersianText(value).replace(/[\s\u200c]+/g, ' ').trim().toLocaleLowerCase('fa-IR');
}

export function StorefrontCatalogue({ items, categoryGroups, productsTitle, productFeed, editorial = false }: {
  items: Product[];
  editorial?: boolean;
  categoryGroups: CategoryGroup[];
  productsTitle: string;
  productFeed?: StorefrontTemplateProps['productFeed'];
}) {
  const { openProductDetail, selectedCategory, setSelectedCategory,
    catalogBrandRequest, clearCatalogBrand, searchQuery, setSearchQuery } = useStore();
  const [floorSort, setFloorSort] = useState<keyof typeof SORT_OPTIONS>('featured');
  const catalogueHeadingId = useId();
  const selectedGroup = categoryGroups.find((g) => g.slug === selectedCategory);
  const brandFilter =
    catalogBrandRequest && items.some((p) => (p.brand || '').trim() === catalogBrandRequest.brand)
      ? catalogBrandRequest.brand
      : null;
  const categoryItems = useMemo(() => items.filter((product) =>
    (!selectedGroup || product.category === selectedGroup.slug) &&
    (!brandFilter || (product.brand || '').trim() === brandFilter)), [items, selectedGroup, brandFilter]);
  const query = normalizeSearch(searchQuery);
  const searching = query.length > 0;
  const floorItems = useMemo(() => searching
    ? categoryItems.filter(p => normalizeSearch([p.title, p.brand, p.description].filter(Boolean).join(' ')).includes(query))
    : categoryItems, [categoryItems, query, searching]);
  const sortedFloorItems = useMemo(() => [...floorItems].sort((a, b) => {
    if (floorSort === 'cheapest') return cardPriceOf(a).price - cardPriceOf(b).price;
    if (floorSort === 'expensive') return cardPriceOf(b).price - cardPriceOf(a).price;
    if (floorSort === 'discount') return cardPriceOf(b).discountPercent - cardPriceOf(a).discountPercent;
    return (b.salesCount || 0) - (a.salesCount || 0);
  }), [floorItems, floorSort]);
  const floorTitle = searching ? `جستجو: «${searchQuery}»` : brandFilter || (selectedGroup ? selectedGroup.title : productsTitle);
  const selectCategory = (slug: string): void => {
    clearCatalogBrand();
    setSelectedCategory(slug);
  };
  const clearFloorFilter = (): void => {
    if (brandFilter) clearCatalogBrand();
    if (selectedGroup) setSelectedCategory('all');
    if (searching) setSearchQuery('');
  };

  return (
    <section id="products" data-testid="store-products" aria-labelledby={catalogueHeadingId} className="fashion-catalogue">
      <div className="fashion-catalogue-heading">
        <div className="fashion-catalogue-title"><h2 id={catalogueHeadingId}>{floorTitle}</h2>
          {!productFeed?.loading && <span className="fashion-result-count" role="status">{toPersianDigits(floorItems.length)} کالا{productFeed?.hasMore ? ' تا اینجا' : ''}</span>}
        </div>
        {brandFilter || selectedGroup || searching ? (
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
        <div className="fashion-search-field">
          <Search size={18} aria-hidden="true" />
          <input id="fashion-search" type="search" name="q" autoComplete="off" aria-label="جست‌وجو در ویترین" placeholder="نام کالا یا برند…" value={searchQuery}
            onChange={event => setSearchQuery(event.target.value)} />
          {searchQuery && <button type="button" aria-label="پاک کردن جست‌وجو" onClick={() => setSearchQuery('')}><X size={17} aria-hidden="true" /></button>}
        </div>
        <label className="fashion-sort-field">
          <SlidersHorizontal size={16} aria-hidden="true" />
          <span className="sr-only">مرتب‌سازی محصولات</span>
          <select value={floorSort} onChange={event => setFloorSort(event.target.value as typeof floorSort)}>
            {Object.entries(SORT_OPTIONS).map(([id, label]) => <option key={id} value={id}>{label}</option>)}
          </select>
        </label>
      </div>
      {categoryGroups.length > 1 && <nav className="fashion-catalogue-filters" aria-label="فیلتر دسته‌بندی محصولات">
        <button type="button" aria-pressed={!selectedGroup && !brandFilter} onClick={() => selectCategory('all')}>همه</button>
        {categoryGroups.map(group => <button key={group.slug} type="button" aria-pressed={selectedGroup?.slug === group.slug && !brandFilter}
          onClick={() => selectCategory(group.slug)}>{group.title}</button>)}
      </nav>}
      {productFeed?.loading ? <RouteLoading label="در حال دریافت کالاها…" /> : floorItems.length === 0 ? (
        !productFeed?.error && <div className="fashion-empty">
          <p>{productFeed?.hasMore ? 'در کالاهای نمایش‌داده‌شده نتیجه‌ای پیدا نشد.' : searching || selectedGroup || brandFilter
            ? 'کالایی با این مشخصات یافت نشد.' : 'هنوز کالایی در این ویترین ثبت نشده است.'}</p>
          {(searching || selectedGroup || brandFilter) && <button type="button" className="fashion-text-action" onClick={clearFloorFilter}>پاک کردن فیلترها</button>}
        </div>
      ) : editorial ? (
          <div className="fashion-product-grid">
            {sortedFloorItems.map((p) => (
              <FashionProductCard key={p.id} product={p} onOpen={openProductDetail} />
            ))}
          </div>
      ) : <TemplateProductGrid products={sortedFloorItems} columnsClass="fashion-product-grid" />}
      {productFeed?.error && <div className="fashion-empty" role="status">
        <p>{productFeed.error}</p>
        <button type="button" className="fashion-text-action" onClick={productFeed.retry}>تلاش مجدد</button>
      </div>}
      {productFeed?.hasMore && !productFeed.error && <button type="button" className="fashion-button fashion-load-more" disabled={productFeed.loadingMore} onClick={productFeed.loadMore}>
        {productFeed.loadingMore ? 'در حال دریافت…' : 'نمایش کالاهای بیشتر'}
      </button>}
    </section>
  );
}
