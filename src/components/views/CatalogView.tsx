import React, { useState, useEffect, useRef } from 'react';
import { catalogApi } from '../../services/catalogApi';
import { isMockMode } from '../../services/apiClient';
import { useStore, activeStoreSlugOf } from '../../context/StoreContext';
import { ProductCard } from '../ProductCard';
import { PriceRangeSlider } from '../PriceRangeSlider';
import {
  ArrowRight, Search, RotateCcw, ArrowUpDown,
  PackageX, ChevronDown, Check
} from 'lucide-react';

export const CatalogView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const {
    products,
    selectedCategory,
    setSelectedCategory,
    searchQuery,
    setSearchQuery,
    toPersianDigits,
    formatPrice,
    categories,
    catalogLoading,
    catalogError,
    loadMoreCatalog,
    hasMoreCatalog,
    catalogTotal,
    catalogLoadingMore,
    setCatalogFilter,
    openCatalogCategory,
    catalogBrandRequest,
    clearCatalogBrand,
    setActiveView,
    currentStore,
    routeStoreSlug,
  } = useStore();

  // If a store context is somehow active while this central catalog
  // is on screen, sidebar category clicks must not keep the shopper
  // in the central assortment: they take the same store-aware
  // channel as every other category entry point and land on the
  // store's own storefront with the category applied. With no store
  // context this is exactly the local filter it has always been.
  const storeSlug = activeStoreSlugOf({ routeStoreSlug, currentStore });
  const handleSelectCategory = (slug: string) => {
    if (storeSlug) openCatalogCategory(slug);
    else setSelectedCategory(slug);
  };

  // Self-guard: this view is the CENTRAL catalogue. If it mounts
  // while a store context is active (a history/URL leftover —
  // every in-app entry point now routes in-store shoppers to the
  // storefront), rendering it would show the central assortment
  // at central prices inside that store's session. Bounce once to
  // the store's own storefront instead. Central mounts (no store
  // context) never fire this.
  const storeGuardFiredRef = useRef(false);
  useEffect(() => {
    if (storeSlug && !storeGuardFiredRef.current) {
      storeGuardFiredRef.current = true;
      setActiveView('seller_storefront', { storeSlug });
    }
  }, [storeSlug, setActiveView]);

  const [sortBy, setSortBy] = useState<'featured' | 'cheapest' | 'expensive' | 'discount' | 'newest'>('featured');
  const [onlyInStock, setOnlyInStock] = useState(false);
  const [onlyOriginal, setOnlyOriginal] = useState(false);
  const [priceMin, setPriceMin] = useState('');
  const [priceMax, setPriceMax] = useState('');
  const liveMode = !isMockMode();
  // FIX 17: live's catalog tree expands per slug (bundle E(slug)),
  // and a fresh category page shows the current category's
  // subcategories EXPANDED (browser evidence). The old single
  // expandedCategory started null, so nothing was ever expanded
  // on load. The set below auto-expands the selected category
  // plus its ancestors whenever categories/selection change.
  const [expandedSlugs, setExpandedSlugs] = useState<Set<string>>(new Set());
  const toggleExpanded = (slug: string) => setExpandedSlugs((prev) => { const next = new Set(prev); if (next.has(slug)) next.delete(slug); else next.add(slug); return next; });
  useEffect(() => {
    if (selectedCategory === 'all') return;
    const path: string[] = [];
    const guard = new Set<string>();
    let cur = categories.find((c) => c.slug === selectedCategory);
    while (cur && !guard.has(cur.slug)) { guard.add(cur.slug); path.push(cur.slug); cur = cur.parentId ? categories.find((c) => c.id === cur!.parentId) : undefined; }
    if (path.length > 0) setExpandedSlugs((prev) => { const next = new Set(prev); path.forEach((s) => next.add(s)); return next; });
  }, [categories, selectedCategory]);
  // FIX 14b: live's catalog fetches category brands + attributes when
  // a category is selected (live bundle) and renders the sections
  // only when non-empty; selections reset on category change.
  const [facetBrands, setFacetBrands] = useState<{ brand: string; count: number }[]>([]);
  const [facetAttrs, setFacetAttrs] = useState<import('../../services/catalogApi').FacetAttribute[]>([]);
  const [selectedBrands, setSelectedBrands] = useState<string[]>([]);
  const [selectedAttrs, setSelectedAttrs] = useState<Record<number, number[]>>({});
  // PERF-01: in live mode the filters/sort/price go to the SERVER
  // (ProductContext page-1 + show-more); the list below renders the
  // loaded pages as-is. Mock mode keeps the old client-side path
  // (the whole mock list is local). setCatalogFilter is value-equal
  // guarded, so pushing identical values never refetches.
  useEffect(() => {
    if (!liveMode) return;
    setCatalogFilter({
      sort: sortBy,
      inStockOnly: onlyInStock,
      onlyOriginal,
      minPrice: priceMin === '' ? null : Number(priceMin),
      maxPrice: priceMax === '' ? null : Number(priceMax),
      brands: selectedBrands,
      attributes: selectedAttrs,
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [liveMode, sortBy, onlyInStock, onlyOriginal, priceMin, priceMax, selectedBrands, selectedAttrs]);

  useEffect(() => {
    // FIX 15: a custom price range must not stick across categories --
    // the slider defaults to the newly selected category's own bounds.
    setPriceMin('');
    setPriceMax('');
    setSelectedBrands([]);
    setSelectedAttrs({});
    if (selectedCategory === 'all') {
      setFacetBrands([]);
      setFacetAttrs([]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedCategory]);
  // FIX 71: a PDP brand click lands here with a fresh nonce; apply it as
  // the brand filter (all categories). Declared AFTER the category-reset
  // effect so it wins on the mount where both run.
  useEffect(() => {
    if (catalogBrandRequest) setSelectedBrands([catalogBrandRequest.brand]);
  }, [catalogBrandRequest]);
  useEffect(() => {
    if (selectedCategory === 'all') return;
    let on = true;
    catalogApi.getCategoryBrands(selectedCategory).then((brands) => {
      if (!on) return;
      const counts = new Map<string, number>();
      products.forEach((p) => {
        if (p.category === selectedCategory && p.brand) counts.set(p.brand, (counts.get(p.brand) || 0) + 1);
      });
      setFacetBrands(brands
        .map((b) => ({ brand: b.titleFa, count: counts.get(b.titleFa) || 0 }))
        .filter((b) => b.brand !== '' && b.brand !== 'بدون برند' && b.count > 0));
    }).catch(() => { if (on) setFacetBrands([]); });
    catalogApi.getCategoryAttributes(selectedCategory).then((res) => {
      if (!on) return;
      const attrs = res?.attributes ?? [];
      // Live keeps only values present in the category products'
      // attributeValues; candidate products carry none yet, so groups
      // stay hidden until approval picks exist -- exactly like live
      // hides empty groups.
      const present = new Map<number, Set<number>>();
      products.forEach((p) => {
        if (p.category !== selectedCategory) return;
        (p.attributeValues ?? []).forEach((av) => {
          if (!present.has(av.attributeId)) present.set(av.attributeId, new Set());
          present.get(av.attributeId)!.add(av.valueId);
        });
      });
      setFacetAttrs(attrs.map((a) => ({ ...a, values: a.values.filter((v) => present.get(a.id)?.has(v.id)) })).filter((a) => a.values.length > 0));
    }).catch(() => { if (on) setFacetAttrs([]); });
    return () => { on = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedCategory, products]);

  // FIX 14: live sidebar price controls default to the server
  // price bounds (GET /products/price-bounds), scoped to the selected
  // category (FIX 15: e.g. gold-silver: 5,280,000-63,800,000) while
  // search (category 'all') keeps the full global range. PERF-01: no
  // client fallback — the loaded pages are not the full range, so a
  // client min/max from 24 items would be wrong; the slider hides
  // until the server bounds arrive.
  const [serverBounds, setServerBounds] = useState<{ minPrice: number; maxPrice: number } | null>(null);
  useEffect(() => {
    let on = true;
    setServerBounds(null);
    catalogApi.getPriceBounds(selectedCategory === 'all' ? {} : { category: selectedCategory }).then((b) => { if (on) setServerBounds(b); }).catch(() => {});
    return () => { on = false; };
  }, [selectedCategory]);
  const dataMin = serverBounds?.minPrice ?? 0;
  const dataMax = serverBounds?.maxPrice ?? 0;
  const effMin = priceMin === '' ? dataMin : Number(priceMin);
  const effMax = priceMax === '' ? dataMax : Number(priceMax);

  // PERF-01: mock mode keeps the old client-side filter/sort over the
  // whole mock list. Live mode renders the server-filtered pages
  // as-is — re-filtering or re-sorting loaded pages client-side would
  // scramble page order (featured) and hide the server total.
  // Facet counts above still derive from the loaded pages (documented
  // limitation: a brand/attribute with no loaded product hides until
  // its page loads).
  let mockFiltered = products;
  if (!liveMode) mockFiltered = products.filter((p) => {
    const matchesCategory = selectedCategory === 'all' || p.category === selectedCategory;
    const matchesSearch = !searchQuery || 
      p.title.includes(searchQuery) || 
      p.description.includes(searchQuery);
    const matchesStock = !onlyInStock || (p.stock || 0) > 0;
    const matchesOriginal = !onlyOriginal || p.isOriginal === true;
    
    let matchesPrice = true;
    if (priceMin !== '' && p.price < Number(priceMin)) matchesPrice = false;
    if (priceMax !== '' && p.price > Number(priceMax)) matchesPrice = false;

    const matchesBrand = selectedBrands.length === 0 || (p.brand !== undefined && selectedBrands.includes(p.brand));
    const matchesAttrs = Object.entries(selectedAttrs).every(([attrId, valueIds]) => {
      if (valueIds.length === 0) return true;
      const aid = Number(attrId);
      return (p.attributeValues ?? []).some((av) => av.attributeId === aid && valueIds.includes(av.valueId));
    });
    return matchesCategory && matchesSearch && matchesStock && matchesOriginal && matchesPrice && matchesBrand && matchesAttrs;
  });

  if (!liveMode) {
    mockFiltered = [...mockFiltered].sort((a, b) => {
      if (sortBy === 'cheapest') return a.price - b.price;
      if (sortBy === 'expensive') return b.price - a.price;
      if (sortBy === 'discount') return b.discountPercent - a.discountPercent;
      // FIX 12: 'newest' keeps the API order, which is already
      // newest-first (list ids are creation timestamps, desc).
      if (sortBy === 'newest') return 0;
      return (b.salesCount || 0) - (a.salesCount || 0);
    });
  }
  const filtered = liveMode ? products : mockFiltered;
  const shownTotal = liveMode ? catalogTotal : filtered.length;

  const activeCategoryObj = categories.find(c => c.slug === selectedCategory);
  const activeBrand = catalogBrandRequest && selectedBrands.includes(catalogBrandRequest.brand)
    ? catalogBrandRequest.brand
    : null;
  const titleText = searchQuery
    ? `جستجو: «${searchQuery}»`
    : activeBrand
    ? `برند: «${activeBrand}»`
    : activeCategoryObj ? activeCategoryObj.title : 'کاتالوگ جامع کالاها';

  // Central-paint guard: the store-guard effect above bounces to the
  // storefront, but effects run AFTER the first paint -- without this
  // return the central assortment (at central prices) flashes for one
  // frame inside a dropshipper session, and a product the store does
  // not carry (e.g. a newly approved product in 0 assortments) appears
  // and then vanishes when the scoped storefront list replaces it.
  // Render nothing while a store context is active; the effect still
  // performs the navigation. This sits after every hook above -- an
  // earlier return would break hook order.
  if (storeSlug) return null;

  return (
    <div className="flex-1 flex flex-col bg-white select-none pb-20 lg:pb-8">
      <header className="sticky top-0 z-30 lg:static bg-white border-b border-slate-200 px-3 lg:px-8 xl:px-10 2xl:px-12 py-2 lg:py-3 flex items-center gap-2">
        <button onClick={onBack} className="p-1 rounded-lg hover:bg-slate-100 text-slate-700">
          <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
        </button>
        <div className="relative flex-1 flex items-center bg-slate-100 rounded-xl px-2.5 py-1.5 border border-slate-200 focus-within:border-brand focus-within:ring-2 focus-within:ring-brand/20 lg:hidden">
          <Search className="w-3.5 h-3.5 text-slate-400 shrink-0 ml-1.5"  strokeWidth={1.5}/>
          <input
            type="text"
            placeholder="جستجوی کالا یا برند…"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            // FIX 12 item 12: submitting from this box used to update
            // only the filter state, leaving the address bar on the
            // previous /?q= URL. Route through openCatalogCategory so
            // the URL is pushed (staying inside /preview/).
            onKeyDown={(e) => { if (e.key === 'Enter') openCatalogCategory('all', searchQuery); }}
            className="w-full bg-transparent text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
          />
        </div>
        <span className="hidden lg:block lg:text-lg xl:text-xl font-bold text-slate-900 truncate">{titleText}</span>
      </header>

      <div className="lg:grid lg:grid-cols-[240px_minmax(0,1fr)] lg:gap-6 lg:px-8 xl:px-10 2xl:px-12">
        <div className="p-3 lg:p-0 lg:pt-6 lg:col-span-2 border-b border-slate-100 lg:border-0 space-y-2">
        {catalogError && (
          <div className="rounded-xl bg-rose-50 border border-rose-100 px-3 py-2 text-[11px] text-rose-700">
            {catalogError}
          </div>
        )}
        {catalogLoading && (
          <div className="h-2 rounded-full bg-slate-100 animate-pulse" aria-label="در حال بارگذاری کاتالوگ" />
        )}
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1 text-xs lg:text-lg xl:text-xl text-slate-500">
            <span className="font-bold text-slate-900">{titleText}</span>
            <span className="text-[10px] lg:text-xs text-slate-400">({toPersianDigits(shownTotal)})</span>
          </div>
          {(onlyInStock || onlyOriginal || priceMin !== '' || priceMax !== '' || selectedCategory !== 'all' || searchQuery || selectedBrands.length > 0 || Object.keys(selectedAttrs).length > 0) && (
            <button
              onClick={() => {
                setSelectedCategory('all');
                setSearchQuery('');
                clearCatalogBrand();
                setOnlyInStock(false);
                setOnlyOriginal(false);
                setSortBy('featured');
              }}
              className="text-[10px] lg:text-xs text-rose-500 font-bold bg-rose-50 px-2 py-0.5 min-h-[36px] rounded-lg flex items-center gap-1"
            >
              <RotateCcw className="w-3 h-3"  strokeWidth={1.5}/>
              <span>حذف فیلترها</span>
            </button>
          )}
        </div>
      </div>

        {/* Desktop sidebar filters, like live's catalogue page */}
        {/* FIX 17: live catalog aside (bundle u3): continuous flow,
            NOT separate spaced cards. Aside: pt-2 lg:pt-0 lg:sticky
            lg:top-32 lg:self-start lg:space-y-5, NO hidden class (only
            headings hide on mobile, so filters render on mobile too),
            h2 "فیلترها", category flow with "همه", expandable tree,
            toggles + price box + sort in one flow row, facets plain.
            Grid column (240px) comes from the parent grid, not the
            aside -- do not add a width class to the aside. */}
        <aside className="pt-2 lg:pt-0 lg:sticky lg:top-32 lg:self-start lg:space-y-5">
          <h2 className="hidden lg:block text-sm font-extrabold text-slate-900">فیلترها</h2>
          <div>
            <h3 className="hidden lg:block text-xs font-bold text-slate-500 mb-2">دسته‌بندی</h3>
            <div className="flex gap-1.5 overflow-x-auto lg:overflow-visible lg:flex-wrap lg:flex-col lg:items-stretch no-scrollbar px-3 lg:px-0 py-0.5 text-[11px] lg:text-sm">
              <button
                onClick={() => handleSelectCategory('all')}
                className={`px-3 py-1 min-h-[36px] lg:min-h-[40px] inline-flex items-center justify-center lg:justify-start rounded-xl font-bold shrink-0 transition-all ${selectedCategory === 'all' ? 'bg-brand text-white shadow-2xs' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'}`}
              >
                همه
              </button>
              {categories.filter((cat) => !cat.parentId).map((cat) => (
                <CatalogCategoryNode
                  key={cat.id}
                  node={cat}
                  categories={categories}
                  selectedCategory={selectedCategory}
                  setSelectedCategory={handleSelectCategory}
                  expandedSlugs={expandedSlugs}
                  toggleExpanded={toggleExpanded}
                  onViewTree={() => setActiveView('categories_tree')}
                />
              ))}
            </div>
          </div>
          <div className="px-3 lg:px-0 py-2 lg:py-0 bg-slate-50 lg:bg-transparent border-b border-slate-200/80 lg:border-0 flex items-start justify-between gap-1 lg:flex-col lg:items-stretch lg:gap-4 text-[10px] lg:text-sm">
            <div className="flex flex-wrap items-center gap-1.5 overflow-x-auto lg:overflow-visible lg:flex-col lg:items-stretch lg:gap-2 no-scrollbar min-w-0">
              <button
                type="button"
                onClick={() => setOnlyInStock((v) => !v)}
                className={`px-2 py-1 min-h-[36px] lg:min-h-[40px] rounded-lg font-bold flex items-center gap-1 shrink-0 lg:w-full ${onlyInStock ? 'bg-brand text-white' : 'bg-white border border-slate-200 text-slate-600'}`}
              >
                {onlyInStock && <Check className="w-3 h-3"  strokeWidth={1.5}/>}
                <span>موجود</span>
              </button>
              <button
                type="button"
                onClick={() => setOnlyOriginal((v) => !v)}
                className={`px-2 py-1 min-h-[36px] lg:min-h-[40px] rounded-lg font-bold flex items-center gap-1 shrink-0 lg:w-full ${onlyOriginal ? 'bg-brand text-white shadow-2xs' : 'bg-white border border-slate-200 text-slate-600'}`}
              >
                {onlyOriginal && <Check className="w-3 h-3"  strokeWidth={1.5}/>}
                <span>فقط کالای اصل</span>
              </button>
              {dataMax > dataMin && (
                <div className="w-full bg-white border border-slate-200 rounded-lg px-2.5 py-2">
                  <PriceRangeSlider
                    boundMin={dataMin}
                    boundMax={dataMax}
                    value={[effMin, effMax]}
                    showHeader={false}
                    onChange={([nextMin, nextMax]) => {
                      if (nextMin === dataMin && nextMax === dataMax) {
                        setPriceMin('');
                        setPriceMax('');
                      } else {
                        setPriceMin(String(nextMin));
                        setPriceMax(String(nextMax));
                      }
                    }}
                  />
                </div>
              )}
            </div>
            <div className="flex items-center gap-1 shrink-0 lg:flex-col lg:items-stretch">
              <span className="hidden lg:block text-xs font-bold text-slate-500">مرتب‌سازی</span>
              <ArrowUpDown className="w-3 h-3 text-slate-400 lg:hidden"  strokeWidth={1.5}/>
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value as typeof sortBy)}
                className="px-2 py-1 min-h-[36px] lg:min-h-[40px] rounded-lg font-bold bg-white border border-slate-200 text-slate-600 text-[11px] lg:text-sm outline-hidden lg:w-full"
              >
                <option value="featured">پیشنهادی</option>
                <option value="newest">جدیدترین</option>
                <option value="discount">بیشترین تخفیف</option>
                <option value="cheapest">ارزان‌ترین</option>
                <option value="expensive">گران‌ترین</option>
              </select>
            </div>
          </div>
          {selectedCategory !== 'all' && (facetBrands.length > 0 || facetAttrs.length > 0) && (
            <div className="px-3 lg:px-0 py-2 lg:py-0 space-y-4">
              {facetBrands.length > 0 && (
                <div>
                  <h3 className="text-xs font-bold text-slate-500 mb-2">برند</h3>
                  <div className="space-y-1">
                    {facetBrands.map(({ brand, count }) => (
                      <label key={brand} className="flex items-center gap-2 min-h-[32px] px-2 py-1 rounded-lg hover:bg-slate-50 cursor-pointer text-[11px] lg:text-sm text-slate-600">
                        <input type="checkbox" checked={selectedBrands.includes(brand)} onChange={() => setSelectedBrands((prev) => prev.includes(brand) ? prev.filter((b) => b !== brand) : [...prev, brand])} className="w-4 h-4 shrink-0 accent-brand" />
                        <span className="flex-1 font-medium">{brand}</span>
                        <span className="text-[10px] text-slate-400">({toPersianDigits(count)})</span>
                      </label>
                    ))}
                  </div>
                </div>
              )}
              {facetAttrs.map((attr) => (
                <div key={attr.id}>
                  <h3 className="text-xs font-bold text-slate-500 mb-2">{attr.title}</h3>
                  <div className="space-y-1">
                    {attr.values.map((v) => (
                      <label key={v.id} className="flex items-center gap-2 min-h-[32px] px-2 py-1 rounded-lg hover:bg-slate-50 cursor-pointer text-[11px] lg:text-sm text-slate-600">
                        <input type="checkbox" checked={(selectedAttrs[attr.id] ?? []).includes(v.id)} onChange={() => setSelectedAttrs((prev) => {
                          const cur = prev[attr.id] ?? [];
                          const next = cur.includes(v.id) ? cur.filter((id) => id !== v.id) : [...cur, v.id];
                          const out = { ...prev };
                          if (next.length === 0) delete out[attr.id]; else out[attr.id] = next;
                          return out;
                        })} className="w-4 h-4 shrink-0 accent-brand" />
                        <span className="flex-1 font-medium">{v.titleFa || v.titleEn}</span>
                      </label>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </aside>
        <div className="p-3 lg:p-0">
        {filtered.length === 0 ? (
          <div className="bg-white rounded-2xl p-6 text-center border border-slate-100 space-y-2">
            <div className="w-16 h-16 rounded-full bg-slate-100 flex items-center justify-center mx-auto"><PackageX className="w-8 h-8 text-slate-400"  strokeWidth={1.5}/></div>
            <h3 className="text-xs font-bold text-slate-800">کالایی یافت نشد</h3>
            <div className="flex items-center justify-center gap-2 pt-1">
              <button
                onClick={() => {
                  setSelectedCategory('all');
                  setSearchQuery('');
                  setOnlyInStock(false);
                  setOnlyOriginal(false);
                    setPriceMin('');
                  setPriceMax('');
                  setSortBy('featured');
                }}
                className="py-1.5 px-3 bg-[#5d44f8] text-white font-bold text-[11px] rounded-xl inline-flex items-center gap-1.5"
              >
                <RotateCcw className="w-3 h-3"  strokeWidth={1.5}/>
                <span>حذف فیلترها</span>
              </button>
              <button
                onClick={() => {
                  setSelectedCategory('all');
                  setSearchQuery('');
                  setOnlyInStock(false);
                  setOnlyOriginal(false);
                    setPriceMin('');
                  setPriceMax('');
                  setSortBy('featured');
                }}
                className="py-1.5 px-3 bg-slate-100 text-slate-700 font-bold text-[11px] rounded-xl inline-flex items-center gap-1.5"
              >
                <span>مشاهده همه محصولات</span>
              </button>
            </div>
          </div>
        ) : (
          <>
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-2.5 lg:gap-4 xl:gap-5">
              {filtered.map((product) => (
                <ProductCard key={product.id} product={product} />
              ))}
            </div>
            {liveMode && hasMoreCatalog && (
              <button
                onClick={() => { void loadMoreCatalog(); }}
                disabled={catalogLoadingMore}
                className="w-full mt-3 py-2.5 bg-white border border-brand/30 text-brand-deep font-bold text-[11px] lg:text-sm rounded-xl shadow-xs hover:bg-brand-soft transition-colors disabled:opacity-60"
              >
                {catalogLoadingMore ? 'در حال بارگذاری…' : `نمایش بیشتر (${toPersianDigits(shownTotal)} کالا)`}
              </button>
            )}
          </>
        )}
        </div>
      </div>
    </div>
  );
};

// FIX 17: live catalog tree (bundle u3) -- depth-aware classes:
// L0 root chip (rounded-xl, slate-100) + chevron px-2/min-h-40;
// L1 child (rounded-lg, slate-50) + chevron px-1.5/min-h-36;
// L2 leaves are plain text-[12px] buttons; when a L2 node has
// children, a "مشاهده همهٔ زیردسته‌ها" button navigates to the
// categories tree view. Expansion is a per-slug set (bundle E).
const CatalogCategoryNode: React.FC<{
  node: any;
  categories: any[];
  selectedCategory: string;
  setSelectedCategory: (s: string) => void;
  expandedSlugs: Set<string>;
  toggleExpanded: (slug: string) => void;
  depth?: number;
  onViewTree: () => void;
}> = ({ node, categories, selectedCategory, setSelectedCategory, expandedSlugs, toggleExpanded, depth = 0, onViewTree }) => {
  const children = categories.filter((c) => c.parentId === node.id);
  const expanded = expandedSlugs.has(node.slug);
  if (depth >= 2) {
    return (
      <button onClick={() => setSelectedCategory(node.slug)} className={`block w-full px-2.5 py-1 min-h-[32px] text-right rounded-lg text-[12px] font-medium transition-all ${selectedCategory === node.slug ? 'bg-brand text-white font-bold' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'}`}>{node.title}</button>
    );
  }
  const isRoot = depth === 0;
  const hasGrandchildren = children.some((ch) => categories.some((c) => c.parentId === ch.id));
  return (
    <div className="shrink-0 lg:shrink lg:w-full">
      <div className="flex items-stretch gap-0.5">
        <button onClick={() => setSelectedCategory(node.slug)} className={isRoot ? `flex-1 px-2.5 lg:px-3.5 py-1 min-h-[36px] lg:min-h-[40px] inline-flex items-center justify-center lg:justify-start rounded-xl font-medium shrink-0 transition-all ${selectedCategory === node.slug ? 'bg-brand text-white font-bold' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'}` : `flex-1 px-2.5 py-1 min-h-[36px] inline-flex items-center justify-start rounded-lg font-medium transition-all text-right ${selectedCategory === node.slug ? 'bg-brand text-white font-bold' : 'bg-slate-50 text-slate-600 hover:bg-slate-100'}`}>{node.title}</button>
        {children.length > 0 && (
          <button
            type="button"
            aria-label={`زیردسته‌های ${node.title}`}
            aria-expanded={expanded}
            onClick={() => toggleExpanded(node.slug)}
            className={isRoot ? 'hidden lg:inline-flex items-center justify-center px-2 min-h-[40px] rounded-xl bg-slate-100 text-slate-600 hover:bg-slate-200 transition-all shrink-0' : 'hidden lg:inline-flex items-center justify-center px-1.5 min-h-[36px] rounded-lg bg-slate-50 text-slate-600 hover:bg-slate-100 transition-all shrink-0'}
          >
            <ChevronDown className={`${isRoot ? 'w-4 h-4' : 'w-3.5 h-3.5'} transition-transform ${expanded ? 'rotate-180' : ''}`}  strokeWidth={1.5}/>
          </button>
        )}
      </div>
      {expanded && children.length > 0 && (
        <div className={isRoot ? 'hidden lg:block mr-3 mt-1 space-y-1 border-r-2 border-slate-100 pr-2' : 'mr-3 mt-0.5 space-y-0.5 border-r-2 border-slate-100 pr-2'}>
          {children.map((child) => (
            <CatalogCategoryNode key={child.id} node={child} categories={categories} selectedCategory={selectedCategory} setSelectedCategory={setSelectedCategory} expandedSlugs={expandedSlugs} toggleExpanded={toggleExpanded} depth={depth + 1} onViewTree={onViewTree} />
          ))}
          {depth === 1 && hasGrandchildren && (
            <button type="button" onClick={onViewTree} className="block w-full px-2.5 py-1 min-h-[32px] text-right rounded-lg text-[12px] font-bold text-brand hover:bg-brand-soft transition-all">مشاهده همهٔ زیردسته‌ها</button>
          )}
        </div>
      )}
    </div>
  );
};
