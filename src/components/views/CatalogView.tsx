import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowLeft, ArrowRight, ArrowUpDown, Check, ChevronDown, ChevronLeft, LayoutGrid, PackageX,
  RotateCcw, SlidersHorizontal, X,
} from 'lucide-react';
import { useStore, activeStoreSlugOf } from '../../context/StoreContext';
import { DEFAULT_CATALOG_FILTER } from '../../context/ProductContext';
import { catalogApi, type FacetAttribute, type FacetBrand } from '../../services/catalogApi';
import { isMockMode } from '../../services/apiClient';
import type { Category } from '../../types';
import { remainingCatalogCount } from '../../utils/catalogRemaining';
import { followStoreLink } from '../../utils/followStoreLink';
import { normalizePersianText, pathForView } from '../../utils/routing';
import { ProductCard } from '../ProductCard';
import { PriceRangeSlider, PRICE_RANGE_ERROR_MESSAGE } from '../PriceRangeSlider';

const pageWidth = 'central-wrap';
const control = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-[var(--shop-line,#d5e0d7)] bg-white px-3 text-sm font-bold text-[var(--shop-forest,#164638)] hover:bg-[var(--shop-soft,#edf4ee)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--shop-forest,#164638)]';
const primary = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-[var(--shop-forest,#164638)] px-4 text-sm font-bold text-white hover:bg-[#215849] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--shop-forest,#164638)]';
const grid = 'central-catalog-grid grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4';
const categoryFilter = { minPrice: null, maxPrice: null, brands: [], attributes: {} };

const sortOptions = [
  ['featured', 'پیشنهادی'],
  ['newest', 'جدیدترین'],
  ['discount', 'بیشترین تخفیف'],
  ['cheapest', 'کمترین قیمت'],
  ['expensive', 'بیشترین قیمت'],
] as const;

export const CatalogView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const {
    products, selectedCategory, searchQuery, toPersianDigits, formatPrice, categories,
    catalogFilter = DEFAULT_CATALOG_FILTER, catalogLoading, catalogError, catalogPriceRangeError,
    fetchCatalogProducts, loadMoreCatalog, hasMoreCatalog, catalogTotal, catalogLoadingMore, catalogLoadMoreError,
    setCatalogFilter, openCatalogCategory, catalogBrandRequest, clearCatalogBrand,
    setActiveView, currentStore, routeStoreSlug,
  } = useStore();

  const storeSlug = activeStoreSlugOf({ routeStoreSlug, currentStore });
  const liveMode = !isMockMode();
  const [filtersOpen, setFiltersOpen] = useState(false);
  const filterButton = useRef<HTMLButtonElement>(null);
  const [expandedSlugs, setExpandedSlugs] = useState<Set<string>>(new Set());
  const [serverBounds, setServerBounds] = useState<{ category: string; minPrice: number; maxPrice: number } | null>(null);
  const [facetSchema, setFacetSchema] = useState<{ category: string; brands: FacetBrand[]; attrs: FacetAttribute[] } | null>(null);
  // Keep observed choices visible when a selected facet narrows the results.
  const [observed, setObserved] = useState<{ category: string; brands: Set<string>; values: Set<number> } | null>(null);

  const hierarchy = useMemo(() => {
    const byId = new Map(categories.map(category => [category.id, category]));
    const bySlug = new Map(categories.map(category => [category.slug, category]));
    const byParent = new Map<string | null, Category[]>();
    for (const category of categories) {
      const parent = category.parentId && byId.has(category.parentId) ? category.parentId : null;
      const children = byParent.get(parent) ?? [];
      children.push(category);
      byParent.set(parent, children);
    }
    const path: Category[] = [];
    const seen = new Set<string>();
    let current = bySlug.get(selectedCategory);
    while (current && !seen.has(current.id)) {
      seen.add(current.id);
      path.unshift(current);
      current = current.parentId ? byId.get(current.parentId) : undefined;
    }
    const selected = bySlug.get(selectedCategory);
    const descendants = new Set<string>();
    const walk = (category: Category) => {
      if (descendants.has(category.slug)) return;
      descendants.add(category.slug);
      byParent.get(category.id)?.forEach(walk);
    };
    if (selected) walk(selected);
    return { byParent, selected, path, descendants };
  }, [categories, selectedCategory]);

  useEffect(() => {
    if (storeSlug) setActiveView('seller_storefront', { storeSlug });
    // Only react to a scope transition. StoreContext recreates the navigation callback.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [storeSlug]);

  useEffect(() => {
    if (hierarchy.path.length) {
      setExpandedSlugs(previous => new Set([...previous, ...hierarchy.path.map(category => category.slug)]));
    }
  }, [hierarchy.path]);

  useEffect(() => {
    if (catalogBrandRequest) setCatalogFilter({ ...categoryFilter, brands: [catalogBrandRequest.brand] });
  }, [catalogBrandRequest, setCatalogFilter]);

  useEffect(() => {
    if (storeSlug || !liveMode) return;
    let active = true;
    catalogApi.getPriceBounds({ category: selectedCategory }).then(bounds => {
      if (active) setServerBounds({ ...bounds, category: selectedCategory });
    }).catch(() => {
      if (active) setServerBounds(null);
    });
    if (selectedCategory !== 'all') {
      void Promise.all([
        catalogApi.getCategoryBrands(selectedCategory).catch(() => []),
        catalogApi.getCategoryAttributes(selectedCategory).catch(() => null),
      ]).then(([brands, result]) => {
        if (active) setFacetSchema({ category: selectedCategory, brands: Array.isArray(brands) ? brands : [], attrs: Array.isArray(result?.attributes) ? result.attributes : [] });
      });
    }
    return () => { active = false; };
  }, [liveMode, selectedCategory, storeSlug]);

  useEffect(() => {
    if (selectedCategory === 'all' || catalogLoading || catalogError || catalogPriceRangeError) return;
    setObserved(previous => {
      const sameCategory = previous?.category === selectedCategory;
      const brands = new Set(sameCategory ? previous.brands : []);
      const values = new Set(sameCategory ? previous.values : []);
      for (const product of products) {
        if (!hierarchy.descendants.has(product.category)) continue;
        if (product.brand && product.brand !== 'بدون برند') brands.add(product.brand);
        for (const attribute of product.attributeValues ?? []) values.add(attribute.valueId);
      }
      if (sameCategory && brands.size === previous.brands.size && values.size === previous.values.size) return previous;
      return { category: selectedCategory, brands, values };
    });
  }, [products, selectedCategory, hierarchy.descendants, catalogLoading, catalogError, catalogPriceRangeError]);

  const { sort, inStockOnly, onlyOriginal, minPrice, maxPrice, brands, attributes } = catalogFilter;
  const priceSelected = minPrice !== null || maxPrice !== null;
  const filterCount = Number(inStockOnly) + Number(onlyOriginal) + Number(priceSelected) +
    brands.length + Object.values(attributes).reduce((sum, values) => sum + values.length, 0);
  const hasSelection = filterCount > 0 || sort !== 'featured' || selectedCategory !== 'all' || Boolean(searchQuery);
  const availableBrands = facetSchema?.category === selectedCategory && observed?.category === selectedCategory
    ? [...new Set(facetSchema.brands.map(brand => brand.titleFa || brand.titleEn).filter(brand => observed.brands.has(brand) || brands.includes(brand)))]
    : [];
  const availableAttrs = facetSchema?.category === selectedCategory && observed?.category === selectedCategory
    ? facetSchema.attrs.map(attr => ({ ...attr, values: attr.values.filter(value => observed.values.has(value.id) || attributes[attr.id]?.includes(value.id)) }))
      .filter(attr => attr.values.length > 0)
    : [];
  const currentBounds = serverBounds?.category === selectedCategory
    ? serverBounds
    : !liveMode && products.length
      ? { minPrice: products.reduce((price, product) => Math.min(price, product.price), Infinity), maxPrice: products.reduce((price, product) => Math.max(price, product.price), 0) }
      : null;

  const visible = useMemo(() => {
    if (liveMode) return products;
    const query = normalizePersianText(searchQuery.trim()).toLowerCase();
    const matches = products.filter(product => (
      (selectedCategory === 'all' || product.category === selectedCategory) &&
      (!query || normalizePersianText(`${product.title} ${product.description}`).toLowerCase().includes(query)) &&
      (!inStockOnly || product.stock > 0) && (!onlyOriginal || product.isOriginal === true) &&
      (minPrice === null || product.price >= minPrice) && (maxPrice === null || product.price <= maxPrice) &&
      (!brands.length || (product.brand !== undefined && brands.includes(product.brand))) &&
      Object.entries(attributes).every(([id, values]) => !values.length ||
        product.attributeValues?.some(attribute => attribute.attributeId === Number(id) && values.includes(attribute.valueId)))
    ));
    if (sort === 'newest') return matches;
    return matches.sort((a, b) => {
      if (sort === 'cheapest') return a.price - b.price;
      if (sort === 'expensive') return b.price - a.price;
      if (sort === 'discount') return b.discountPercent - a.discountPercent;
      return (b.salesCount || 0) - (a.salesCount || 0);
    });
  }, [products, liveMode, searchQuery, selectedCategory, inStockOnly, onlyOriginal, minPrice, maxPrice, brands, attributes, sort]);

  const showSkeleton = catalogLoading && visible.length === 0;
  const total = liveMode ? catalogTotal : visible.length;
  const remaining = remainingCatalogCount(catalogTotal, visible.length);
  const title = searchQuery.trim() ? `نتایج جستجوی «${searchQuery}»` : catalogBrandRequest && brands.includes(catalogBrandRequest.brand)
    ? `کالاهای برند ${catalogBrandRequest.brand}` : hierarchy.selected?.title ?? 'کاتالوگ جامع کالاها';
  const closeFilters = () => {
    if (!filtersOpen) return;
    setFiltersOpen(false);
    filterButton.current?.focus();
  };
  const chooseCategory = (slug: string) => {
    closeFilters();
    if (slug !== selectedCategory) openCatalogCategory(slug, searchQuery);
  };
  const resetFilters = () => {
    clearCatalogBrand();
    setCatalogFilter(DEFAULT_CATALOG_FILTER);
    if (selectedCategory !== 'all' || searchQuery) openCatalogCategory('all');
  };
  const removeBrand = (brand: string) => {
    if (catalogBrandRequest?.brand === brand) clearCatalogBrand();
    setCatalogFilter({ brands: brands.filter(value => value !== brand) });
  };
  const toggleAttribute = (id: number, value: number) => {
    const current = attributes[id] ?? [];
    const next = { ...attributes };
    const updated = current.includes(value) ? current.filter(item => item !== value) : [...current, value];
    if (updated.length) next[id] = updated;
    else delete next[id];
    setCatalogFilter({ attributes: next });
  };
  const toggleTree = (slug: string) => setExpandedSlugs(previous => {
    const next = new Set(previous);
    if (next.has(slug)) next.delete(slug);
    else next.add(slug);
    return next;
  });

  const renderCategory = (category: Category, depth = 0): React.ReactNode => {
    const children = hierarchy.byParent.get(category.id) ?? [];
    const isExpanded = expandedSlugs.has(category.slug);
    const selected = selectedCategory === category.slug;
    return (
      <li key={category.id}>
        <div className="flex items-stretch gap-1">
          <button type="button" aria-pressed={selected} onClick={() => chooseCategory(category.slug)}
            className={`min-h-11 min-w-0 flex-1 rounded-lg px-2 text-start text-sm transition-colors focus-visible:outline-[var(--shop-forest,#164638)] ${selected ? 'bg-[var(--shop-soft,#e5efe6)] font-bold text-[var(--shop-forest,#164638)]' : 'text-[var(--shop-muted,#40574d)] hover:bg-[#f2f5ef]'}`}>
            {category.title}
          </button>
          {children.length > 0 && depth < 2 && (
            <button type="button" aria-expanded={isExpanded} aria-label={`زیردسته‌های ${category.title}`}
              onClick={() => toggleTree(category.slug)} className={`${control} aspect-square px-0`}>
              <ChevronDown size={16} aria-hidden className={isExpanded ? 'rotate-180' : ''} />
            </button>
          )}
        </div>
        {isExpanded && children.length > 0 && depth < 2 && (
          <ul className="ms-2 mt-1 space-y-1 border-s border-[var(--shop-line,#d5e0d7)] ps-2">
            {children.map(child => renderCategory(child, depth + 1))}
          </ul>
        )}
      </li>
    );
  };

  // Prevent a central price/assortment flash while restoring a scoped store.
  if (storeSlug) return null;

  const subcategories = hierarchy.byParent.get(hierarchy.selected?.id ?? null) ?? [];
  const parentCategories = hierarchy.path.slice(0, -1);
  return (
    <main id="main-content" tabIndex={-1} dir="rtl" className="central-catalog flex-1 bg-[var(--shop-bg,#f8f6ef)] pb-12 text-[var(--shop-ink,#172d26)]">
      <header className="central-list-heading">
        <div className={`${pageWidth} central-list-heading-inner`}>
          <nav aria-label="مسیر صفحه" className="mb-2 flex flex-wrap items-center gap-x-1.5 text-xs font-bold text-[var(--shop-muted,#40574d)]">
            <a href="/" onClick={event => followStoreLink(event, onBack)}
              className="inline-flex min-h-11 items-center rounded-md px-1 hover:underline focus-visible:outline-[var(--shop-forest,#164638)]">
              <ArrowRight size={16} className="me-1.5" aria-hidden /> خانه
            </a>
            {hierarchy.selected && (
              <>
                <ChevronLeft size={13} aria-hidden />
                <button type="button" onClick={() => chooseCategory('all')} className="min-h-11 rounded-md px-1 hover:underline focus-visible:outline-[var(--shop-forest,#164638)]">همهٔ کالاها</button>
              </>
            )}
            {parentCategories.map(category => (
              <React.Fragment key={category.id}>
                <ChevronLeft size={13} aria-hidden />
                <button type="button" onClick={() => chooseCategory(category.slug)} className="min-h-11 rounded-md px-1 hover:underline focus-visible:outline-[var(--shop-forest,#164638)]">{category.title}</button>
              </React.Fragment>
            ))}
          </nav>
          <h1 className="wrap-anywhere text-2xl leading-snug font-black sm:text-4xl">{title}</h1>
          <p className="mt-1.5 min-h-5 text-sm font-medium text-[var(--shop-muted,#506357)]" role="status">
            {catalogLoading ? 'در حال بارگذاری کاتالوگ…' : catalogError ? 'به‌روزرسانی نتایج انجام نشد' : `${toPersianDigits(total)} کالا`}
          </p>
          {!!subcategories.length && (
            <nav aria-label={hierarchy.selected ? 'زیردسته‌ها' : 'دسته‌ها'} className="central-catalog-rail mt-3 -mb-1 flex gap-2 overflow-x-auto py-1">
              {subcategories.slice(0, 8).map(category => (
                <a key={category.id} href={pathForView('catalog', { searchQuery, categorySlug: category.slug })}
                  onClick={event => followStoreLink(event, () => chooseCategory(category.slug))}
                  className={`${control} shrink-0 whitespace-nowrap`}>{category.title}<ChevronLeft size={14} aria-hidden /></a>
              ))}
              {subcategories.length > 8 && <button type="button" onClick={() => setActiveView('categories_tree')} className={`${control} shrink-0`}>همهٔ دسته‌بندی‌ها</button>}
            </nav>
          )}
        </div>
      </header>

      <div className={`${pageWidth} pt-4 lg:grid lg:grid-cols-[240px_minmax(0,1fr)] lg:grid-rows-[max-content_1fr] lg:gap-x-6 lg:pt-7`}>
        <div className="central-catalog-bar mb-3 flex flex-wrap items-center gap-2 lg:col-start-2 lg:row-start-1">
          <button ref={filterButton} type="button" aria-expanded={filtersOpen} aria-controls="catalog-filters"
            onClick={() => setFiltersOpen(open => !open)} className={`${control} lg:hidden`}>
            <SlidersHorizontal size={17} aria-hidden /> فیلترها
            {filterCount > 0 && <span className="rounded-full bg-[var(--shop-gold,#efc771)] px-2 py-0.5 text-xs text-[var(--shop-ink,#172d26)]">{toPersianDigits(filterCount)}</span>}
          </button>
          <label className={`${control} min-w-0 flex-1 justify-start gap-1 lg:flex-none`}>
            <ArrowUpDown size={16} aria-hidden />
            <span className="sr-only">مرتب‌سازی کالاها</span>
            <select value={sort} onChange={event => setCatalogFilter({ sort: event.target.value as typeof sort })}
              className="min-h-11 min-w-0 flex-1 bg-transparent py-1 ps-1 pe-2 text-sm focus-visible:outline-[var(--shop-forest,#164638)]">
              {sortOptions.map(([value, text]) => <option key={value} value={value}>{text}</option>)}
            </select>
          </label>
          {hasSelection && (
            <button type="button" onClick={resetFilters} className={`${control} w-full sm:w-auto lg:ms-auto`}>
              <RotateCcw size={16} aria-hidden /> حذف فیلترها
            </button>
          )}
        </div>

        <aside id="catalog-filters" aria-label="فیلترهای کاتالوگ"
          onKeyDown={event => { if (event.key === 'Escape' && filtersOpen) closeFilters(); }}
          className={`${filtersOpen ? 'block' : 'hidden'} central-catalog-filters mb-4 min-w-0 self-start rounded-2xl border border-[var(--shop-line,#dce5d9)] bg-white p-3.5 lg:col-start-1 lg:row-span-2 lg:row-start-1 lg:mb-0 lg:block`}>
          <div className="central-catalog-filter-heading mb-2 flex items-center justify-between border-b border-[var(--shop-line,#e1e8de)] pb-2">
            <h2 className="flex items-center gap-2 text-base font-black"><SlidersHorizontal size={17} aria-hidden /> فیلترها</h2>
            <button type="button" aria-label="بستن فیلترها" className={`${control} lg:hidden`} onClick={closeFilters}><X size={18} aria-hidden /></button>
          </div>
          <fieldset className="space-y-1 border-b border-[var(--shop-line,#e1e8de)] pb-3">
            <legend className="py-2 text-sm font-extrabold">وضعیت کالا</legend>
            <label className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-2 text-sm hover:bg-[var(--shop-soft,#edf4ee)]">
              <input type="checkbox" checked={inStockOnly} onChange={event => setCatalogFilter({ inStockOnly: event.target.checked })} className="size-4 accent-[var(--shop-forest,#164638)]" />
              فقط کالاهای موجود
            </label>
            <label className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-2 text-sm hover:bg-[var(--shop-soft,#edf4ee)]">
              <input type="checkbox" checked={onlyOriginal} onChange={event => setCatalogFilter({ onlyOriginal: event.target.checked })} className="size-4 accent-[var(--shop-forest,#164638)]" />
              فقط کالاهای اصل
            </label>
          </fieldset>
          {currentBounds && currentBounds.maxPrice > currentBounds.minPrice && (
            <div className="border-b border-[var(--shop-line,#e1e8de)] py-3 [&_input[type=text]]:min-h-11">
              <h3 className="mb-2 text-sm font-extrabold">محدودهٔ قیمت</h3>
              <PriceRangeSlider
                key={selectedCategory}
                boundMin={currentBounds.minPrice}
                boundMax={currentBounds.maxPrice}
                value={minPrice !== null && maxPrice !== null && minPrice > maxPrice ? [currentBounds.minPrice, currentBounds.maxPrice] : [minPrice ?? currentBounds.minPrice, maxPrice ?? currentBounds.maxPrice]}
                showHeader={false}
                serverRangeError={catalogPriceRangeError && filtersOpen}
                onChange={([nextMin, nextMax]) => {
                  setCatalogFilter(nextMin === currentBounds.minPrice && nextMax === currentBounds.maxPrice
                    ? { minPrice: null, maxPrice: null }
                    : { minPrice: nextMin, maxPrice: nextMax });
                }}
              />
            </div>
          )}
          {availableBrands.length > 0 && (
            <fieldset className="border-b border-[var(--shop-line,#e1e8de)] pb-2">
              <legend className="py-2 text-sm font-extrabold">برند</legend>
              {availableBrands.map(brand => (
                <label key={brand} className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-2 text-sm hover:bg-[var(--shop-soft,#edf4ee)]">
                  <input type="checkbox" checked={brands.includes(brand)} className="size-4 accent-[var(--shop-forest,#164638)]" onChange={() => {
                    if (brands.includes(brand)) removeBrand(brand);
                    else setCatalogFilter({ brands: [...brands, brand] });
                  }} />
                  {brand}
                </label>
              ))}
            </fieldset>
          )}
          {availableAttrs.map(attr => (
            <fieldset key={attr.id} className="border-b border-[var(--shop-line,#e1e8de)] pb-2">
              <legend className="py-2 text-sm font-extrabold">{attr.title}</legend>
              {attr.values.map(value => (
                <label key={value.id} className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-2 text-sm hover:bg-[var(--shop-soft,#edf4ee)]">
                  <input type="checkbox" checked={attributes[attr.id]?.includes(value.id) ?? false}
                    className="size-4 accent-[var(--shop-forest,#164638)]" onChange={() => toggleAttribute(attr.id, value.id)} />
                  {value.titleFa || value.titleEn}
                </label>
              ))}
            </fieldset>
          ))}
          <div className="pt-3">
            <h3 className="mb-2 text-sm font-extrabold">دسته‌بندی کالاها</h3>
            <ul className="space-y-1">
              <li><button type="button" aria-pressed={selectedCategory === 'all'} onClick={() => chooseCategory('all')}
                className={`flex min-h-11 w-full items-center gap-1.5 rounded-lg px-2 text-start text-sm focus-visible:outline-[var(--shop-forest,#164638)] ${selectedCategory === 'all' ? 'bg-[var(--shop-soft,#e5efe6)] font-bold text-[var(--shop-forest,#164638)]' : 'text-[var(--shop-muted,#40574d)] hover:bg-[#f2f5ef]'}`}>
                {selectedCategory === 'all' && <Check size={15} aria-hidden />} همه
              </button></li>
              {(hierarchy.byParent.get(null) ?? []).map(category => renderCategory(category))}
            </ul>
            <button type="button" onClick={() => setActiveView('categories_tree')} className={`${control} mt-3 w-full`}>
              <LayoutGrid size={16} aria-hidden /> همهٔ دسته‌بندی‌ها
            </button>
          </div>
          <button type="button" onClick={closeFilters} className={`${primary} mt-3 w-full lg:hidden`}>نمایش کالاها</button>
        </aside>

        <section aria-label="نتایج کاتالوگ" aria-busy={catalogLoading || catalogLoadingMore}
          className="min-w-0 scroll-mt-28 lg:col-start-2 lg:row-start-2">
          {(inStockOnly || onlyOriginal || priceSelected || brands.length > 0 || Object.keys(attributes).length > 0) && (
            <ul aria-label="فیلترهای فعال" className="mb-3 flex flex-wrap gap-2 text-xs">
              {inStockOnly && <FilterChip label="کالاهای موجود" onRemove={() => setCatalogFilter({ inStockOnly: false })} />}
              {onlyOriginal && <FilterChip label="کالاهای اصل" onRemove={() => setCatalogFilter({ onlyOriginal: false })} />}
              {priceSelected && <FilterChip label={[minPrice != null ? `از ${formatPrice(minPrice)}` : '', maxPrice != null ? `تا ${formatPrice(maxPrice)}` : '', 'تومان'].filter(Boolean).join(' ')}
                onRemove={() => setCatalogFilter({ minPrice: null, maxPrice: null })} />}
              {brands.map(brand => <FilterChip key={brand} label={brand} onRemove={() => removeBrand(brand)} />)}
              {availableAttrs.flatMap(attr => attr.values.filter(value => attributes[attr.id]?.includes(value.id))
                .map(value => <FilterChip key={`${attr.id}:${value.id}`} label={`${attr.title}، ${value.titleFa || value.titleEn}`} onRemove={() => toggleAttribute(attr.id, value.id)} />))}
            </ul>
          )}
          {catalogError && (
            <div data-testid="catalog-load-error" role="alert" className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[#ead6c4] bg-[#fff5e9] p-3 text-sm font-bold text-[#743905]">
              <span>{visible.length ? 'بارگذاری نتایج تازه انجام نشد. نتایج قبلی نمایش داده می‌شود.' : 'بارگذاری کالاها انجام نشد.'}</span>
              <button type="button" onClick={() => void fetchCatalogProducts()} className={control}><RotateCcw size={16} aria-hidden /> تلاش دوباره</button>
            </div>
          )}
          {catalogPriceRangeError && (!filtersOpen || !currentBounds || currentBounds.maxPrice <= currentBounds.minPrice) && (
            <p data-testid="price-range-error" role="alert" className="mb-3 rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm font-bold text-rose-800">{PRICE_RANGE_ERROR_MESSAGE}</p>
          )}
          {showSkeleton ? (
            <div aria-hidden className={grid}>
              {Array.from({ length: 8 }, (_, index) => <div key={index} className="aspect-[3/4] rounded-2xl border border-[var(--shop-line,#dce5d9)] bg-[#e9eee3] motion-safe:animate-pulse" />)}
            </div>
          ) : visible.length === 0 && !catalogError && !catalogPriceRangeError ? (
            <div className="rounded-2xl border border-[var(--shop-line,#dce5d9)] bg-white px-4 py-12 text-center sm:py-20">
              <div className="mx-auto mb-3 grid size-14 place-items-center rounded-2xl bg-[var(--shop-soft,#edf4ee)] text-[var(--shop-forest,#164638)]"><PackageX size={26} aria-hidden /></div>
              <h2 className="text-lg font-black">کالایی یافت نشد</h2>
              <p className="mt-1 text-sm leading-7 text-[var(--shop-muted,#506357)]">{searchQuery ? 'عبارت دیگری جستجو کنید یا همهٔ کالاها را ببینید.' : filterCount ? 'فیلترها را تغییر دهید تا کالاهای بیشتری ببینید.' : 'برای پیدا کردن کالا، دستهٔ دیگری را انتخاب کنید.'}</p>
              <button type="button" onClick={resetFilters} className={`${primary} mt-4`}>مشاهده همه محصولات <ArrowLeft size={16} aria-hidden /></button>
            </div>
          ) : visible.length > 0 && (
            <>
              <div className={grid}>
                {visible.map(product => <ProductCard key={product.id} product={product} />)}
              </div>
              {liveMode && hasMoreCatalog && remaining > 0 && !catalogError && !catalogPriceRangeError && (
                <div className="mt-5 flex flex-col items-center gap-2">
                  {catalogLoadMoreError && <p role="alert" className="text-sm font-semibold text-[#743905]">بارگذاری کالاهای بیشتر انجام نشد.</p>}
                  <button type="button" onClick={() => void loadMoreCatalog()} disabled={catalogLoadingMore} className={`${control} w-full disabled:opacity-60 sm:w-auto`}>
                    {catalogLoadingMore ? 'در حال بارگذاری…' : catalogLoadMoreError ? 'تلاش دوباره برای کالاهای بیشتر' : `نمایش بیشتر (${toPersianDigits(remaining)} کالای دیگر)`}
                    <ChevronDown size={16} aria-hidden />
                  </button>
                </div>
              )}
            </>
          )}
        </section>
      </div>
    </main>
  );
};

const FilterChip: React.FC<{ label: string; onRemove: () => void }> = ({ label, onRemove }) => (
  <li>
    <button type="button" aria-label={`حذف فیلتر ${label}`} onClick={onRemove}
      className="inline-flex min-h-11 items-center gap-2 rounded-full border border-[#ccdccc] bg-[#e9f0e7] px-3 font-bold text-[var(--shop-forest,#164638)] hover:bg-white focus-visible:outline-2 focus-visible:outline-[var(--shop-forest,#164638)]">
      {label} <X size={14} aria-hidden />
    </button>
  </li>
);
