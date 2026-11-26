import React, { createContext, useContext, useState, useEffect, useLayoutEffect, useRef, useCallback, ReactNode } from 'react';
import { Product, Category, Niche } from '../types';
import { mockCategories, mockProducts } from '../data/mockData';
import { catalogApi, ProductDto } from '../services/catalogApi';
import { isMockMode, tokenStore } from '../services/apiClient';
import { useAuth } from './AuthContext';
import { wishlistApi } from '../services/wishlistApi';

// FIX 4 (team 04 G2): the backend serves gallery images, specs and
// variants; map them through instead of hardcoding specs: {} and a
// single image. discountPercent prefers the backend value (live
// derives it the same way, so this only avoids recompute drift).
const mapBackendSpecs = (specs: ProductDto['specs']): { [key: string]: string } => {
  if (!specs) return {};
  const out: { [key: string]: string } = {};
  for (const [key, value] of Object.entries(specs)) {
    if (typeof value === 'boolean') out[key] = value ? 'بله' : 'خیر';
    else if (value === null || value === undefined) continue;
    else out[key] = String(value);
  }
  return out;
};

// Variant systems P0: the backend serves variant images as images[]
// (images[0] = hero) with no separate imageUrl — derive imageUrl from
// images[0] so the PDP/selector can use one field.
const mapBackendVariant = (variant: NonNullable<ProductDto['variants']>[number]) => ({
  ...variant,
  imageUrl: variant.imageUrl ?? variant.images?.[0] ?? null,
});

// Admin-review fix (Nov 13): stale mock-era entries in localStorage
// (drup_custom_approved_products) used to be PREPENDED in live mode too,
// so they occupied the head of the live catalog and pushed real products
// down. In live mode the server list leads and customs only fill gaps
// at the end; customs lead only in mock mode, where they are the source
// of approved products. Pure helper so the ordering is unit-testable.
export const mergeCatalogLists = (baseCatalogList: Product[], customApprovedProducts: Product[], mockMode: boolean): Product[] => mockMode ? [
  ...customApprovedProducts,
  ...baseCatalogList.filter((b) => !customApprovedProducts.some((c) => c.id === b.id)),
] : [
  ...baseCatalogList,
  ...customApprovedProducts.filter((c) => !baseCatalogList.some((b) => b.id === c.id)),
];

export const mapBackendProduct = (product: ProductDto): Product => ({
  id: product.id,
  // P0 fix: product-level weight/dims were dropped here even though the
  // propose form requires them and the dossier shows them; variants
  // inherit from these, so they must survive the mapping.
  weightGrams: product.weightGrams,
  lengthMm: product.lengthMm,
  widthMm: product.widthMm,
  heightMm: product.heightMm,
  // P1(a) price range + P2(c) store default (omitted server-side when absent).
  priceMin: product.priceMin,
  priceMax: product.priceMax,
  storeDefaultVariantId: product.storeDefaultVariantId ?? null,
  productType: product.productType,
  schemaVersion: product.schemaVersion,
  productSchema: product.productSchema,
  title: product.title,
  slug: product.slug,
  spnNumber: product.spnNumber,
  category: product.category,
  supplierId: product.supplierId ?? '',
  supplierName: product.supplierName ?? '',
  originalPrice: product.originalPrice || product.price,
  price: product.price,
  // baseCostB is the raw supplier cost C on a store view (the backend
  // sets it to p.SupplierBaseCost); platformFeeF is the platform share
  // F = R - C, so C must NOT be reduced by F here. baseCostB is absent
  // on the central view (live parity); that yields 0, and any view
  // that needs costs must fetch store-scoped.
  supplierBaseCost: product.baseCostB != null
    ? Math.max(0, product.baseCostB)
    : 0,
  platformMargin: product.platformFeeF,
  discountPercent: product.discountPercent != null
    ? product.discountPercent
    : (product.originalPrice > 0
      ? Math.max(0, Math.round((1 - product.price / product.originalPrice) * 100))
      : 0),
  brand: product.brand,
  isActive: product.isActive,
  discountEligible: product.discountEligible,
  campaignName: product.campaignName,
  storeCount: product.storeCount,
  variants: product.variants?.map(mapBackendVariant),
  variantPrices: product.variantPrices,
  addons: product.addons,
  addonSectionTitle: product.addonSectionTitle,
  attributeValues: product.attributeValues,
  cashbackPercent: 0,
  rating: 0,
  reviewsCount: 0,
  salesCount: 0,
  stock: product.stock,
  image: product.image,
  images: product.images && product.images.length > 0
    ? product.images
    : (product.image ? [product.image] : []),
  specs: mapBackendSpecs(product.specs),
  description: product.description,
  warehouse: product.warehouse,
  preparationDays: 1,
  supplierWarranty: product.supplierWarranty || 'ضمانت اصالت کالا',
  featured: false,
  specialDeal: false,
  nicheIds: product.nicheIds,
  isOriginal: product.isOriginal,
});

export const mapBackendCategory = (category: import('../services/catalogApi').CategoryDto): Category => ({
  id: category.id,
  title: category.title,
  slug: category.slug,
  iconName: category.iconName,
  parentId: category.parentId ?? null,
  // FIX 4 (team 04 G8): subtree active-product count from the backend,
  // no longer a hardcoded 0.
  count: category.productCount ?? 0,
  gradient: category.gradient || 'from-slate-50 to-blue-50',
  image: category.imageUrl || '',
});

export interface ProductContextType {
  products: Product[];
  categories: Category[];
  niches: Niche[];
  catalogLoading: boolean;
  catalogError: string | null;
  // A4: true while the last catalog page fetch 400d on an inverted
  // price range. Renders as the inline slider message, never the
  // generic catalog error banner.
  catalogPriceRangeError: boolean;
  selectedCategory: string;
  setSelectedCategory: (catId: string) => void;
  searchQuery: string;
  setSearchQuery: (query: string) => void;
  selectedProduct: Product | null;
  setSelectedProduct: (prod: Product | null) => void;
  currentProductDetail: Product | null;
  openProductDetail: (product: Product) => void;
  openCatalogCategory: (categorySlug: string, query?: string) => void;
  catalogBrandRequest: { brand: string; nonce: number } | null;
  openCatalogBrand: (brand: string) => void;
  clearCatalogBrand: () => void;
  wishlistIds: string[];
  wishlists: Record<string, string[]>;
  toggleWishlist: (productId: string, store?: { id: string; slug?: string }) => void;
  isWishlisted: (productId: string, store?: { id: string; slug?: string }) => boolean;
  setWishlistScope: (storeId: string, storeSlug: string) => void;
  toggleProductOriginality: (productId: string, isOriginal: boolean) => void;
  addProduct: (product: Product) => void;
  fetchProducts: () => Promise<void>;
  fetchCategories: () => Promise<void>;
  fetchNiches: () => Promise<void>;
  searchProducts: (query: string) => Product[];
  filterByCategory: (categorySlug: string) => Product[];
  // PERF-01: the server-side catalog filter + paging. products is the
  // ACCUMULATED loaded pages for this filter (page 1 + loadMore), NOT
  // the whole catalog; catalogTotal is the full filtered count.
  // Mock mode is untouched: products is the whole mock list there,
  // loadMore is a no-op and hasMoreCatalog is false.
  catalogFilter: CatalogFilter;
  setCatalogFilter: (patch: Partial<CatalogFilter>) => void;
  resetCatalogFilter: () => void;
  loadMoreCatalog: () => Promise<void>;
  hasMoreCatalog: boolean;
  catalogTotal: number;
  catalogLoadingMore: boolean;
  catalogLoadMoreError: boolean;
}

// PERF-01: the server-side catalog filter. category/search come from
// selectedCategory/searchQuery (already in this context); these are
// the remaining server facets, set by CatalogView from its UI state.
export interface CatalogFilter {
  sort: 'featured' | 'cheapest' | 'expensive' | 'discount' | 'newest';
  inStockOnly: boolean;
  onlyOriginal: boolean;
  minPrice: number | null;
  maxPrice: number | null;
  brands: string[];
  attributes: Record<number, number[]>;
}

export const DEFAULT_CATALOG_FILTER: CatalogFilter = {
  sort: 'featured',
  inStockOnly: false,
  onlyOriginal: false,
  minPrice: null,
  maxPrice: null,
  brands: [],
  attributes: {},
};

const categoryScopedFilters: Partial<CatalogFilter> = { minPrice: null, maxPrice: null, brands: [], attributes: {} };

const EMPTY_WISHLIST_IDS: string[] = [];

const ProductContext = createContext<ProductContextType | undefined>(undefined);
export const ProductProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { customerUser, currentUser, authToken, isSessionReady } = useAuth();
  const wishlistOwner = currentUser && authToken ? JSON.stringify([currentUser.id, currentUser.storeId ?? null, authToken]) : 'guest';
  const wishlistOwnerRef = useRef(wishlistOwner);
  const [backendCatalogProducts, setBackendCatalogProducts] = useState<Product[]>([]);
  const [categories, setCategories] = useState<Category[]>(() => isMockMode() ? mockCategories : []);
  const [niches, setNiches] = useState<Niche[]>([]);
  const [catalogLoading, setCatalogLoading] = useState(!isMockMode());
  const [catalogError, setCatalogError] = useState<string | null>(null);
  const [catalogPriceRangeError, setCatalogPriceRangeError] = useState(false);
  const [selectedCategory, setSelectedCategory] = useState<string>(() => {
    if (typeof window === 'undefined') return 'all';
    return new URLSearchParams(window.location.search).get('category') || 'all';
  });
  const [searchQuery, setSearchQuery] = useState(() => {
    if (typeof window === 'undefined') return '';
    return new URLSearchParams(window.location.search).get('q') || '';
  });

  // FIX 71: brand-click navigation. A PDP brand click opens the catalog
  // filtered to that brand (all categories). The nonce makes every click
  // a fresh request even when the same brand is clicked twice in a row.
  const [catalogBrandRequest, setCatalogBrandRequest] = useState<{ brand: string; nonce: number } | null>(null);

  const [selectedProduct, setSelectedProduct] = useState<Product | null>(null);
  const [currentProductDetail, setCurrentProductDetail] = useState<Product | null>(null);
  // Per-store wishlists (owner doctrine: every dropshipper store is
  // a separate site with its own wishlist). The map is keyed by store
  // id; wishlistScope names the store the shopper is currently inside
  // (StoreContext keeps it in sync with the routed/session store -
  // the same resolution the PDP scopes by) and wishlistIds below is
  // that store's list only. Guests persist the map in localStorage;
  // logged-in customers hydrate each store's list from the API and
  // write mutations through to it.
  const [wishlistCache, setWishlistCache] = useState<{ owner: string; lists: Record<string, string[]> }>(() => {
    if (typeof window !== 'undefined' && !tokenStore.getToken()) {
      try {
        const saved = localStorage.getItem('drup_guest_wishlists');
        if (saved) {
          const parsed = JSON.parse(saved);
          if (parsed && typeof parsed === 'object') return { owner: wishlistOwner, lists: parsed as Record<string, string[]> };
        }
      } catch {}
    }
    return { owner: wishlistOwner, lists: {} };
  });
  // Signed-in responses stay in memory under immutable account + tenant. Only
  // genuinely anonymous product IDs may persist and merge into the next login.
  const wishlists = wishlistCache.owner === wishlistOwner ? wishlistCache.lists : {};
  const setWishlists = (update: React.SetStateAction<Record<string, string[]>>) => {
    const generation = tokenStore.getGeneration();
    setWishlistCache((previous) => {
      if (wishlistOwnerRef.current !== wishlistOwner || tokenStore.getGeneration() !== generation) return previous;
      const lists = previous.owner === wishlistOwner ? previous.lists : {};
      return { owner: wishlistOwner, lists: typeof update === 'function' ? update(lists) : update };
    });
  };
  const [wishlistScope, setWishlistScopeState] = useState<{ storeId: string; storeSlug: string }>({
    storeId: 'store-central',
    storeSlug: 'central',
  });
  const wishlistsRef = useRef(wishlists);
  const guestMergeListsRef = useRef<Record<string, string[]>>({});
  const mergedGuestScopesRef = useRef<Set<string>>(new Set());
  // Store id -> slug for every store this device has scoped a
  // wishlist to, persisted next to drup_wishlists: a guest list
  // written inside a store the shopper is not currently in must
  // still be mergeable under that store's own slug at login. The
  // central scope's slug is 'central' (a missing slug would also
  // mean central to the API).
  const wishlistScopeSlugsRef = useRef<Record<string, string>>((() => {
    const base: Record<string, string> = { 'store-central': 'central' };
    if (typeof window !== 'undefined') {
      try {
        const saved = localStorage.getItem('drup_wishlist_scopes');
        if (saved) {
          const parsed = JSON.parse(saved);
          if (parsed && typeof parsed === 'object') Object.assign(base, parsed as Record<string, string>);
        }
      } catch {}
    }
    return base;
  })());

  const recordWishlistScopeSlug = (storeId: string, storeSlug?: string) => {
    if (!storeSlug || wishlistScopeSlugsRef.current[storeId] === storeSlug) return;
    wishlistScopeSlugsRef.current = { ...wishlistScopeSlugsRef.current, [storeId]: storeSlug };
    if (typeof window !== 'undefined') {
      try {
        localStorage.setItem('drup_wishlist_scopes', JSON.stringify(wishlistScopeSlugsRef.current));
      } catch {}
    }
  };

  const setWishlistScope = useCallback((storeId: string, storeSlug: string) => {
    recordWishlistScopeSlug(storeId, storeSlug);
    setWishlistScopeState((prev) =>
      prev.storeId === storeId && prev.storeSlug === storeSlug ? prev : { storeId, storeSlug },
    );
  }, []);

  const wishlistIds = wishlists[wishlistScope.storeId] ?? EMPTY_WISHLIST_IDS;

  // The map is the persistence unit: mirror it to the ref (toggle
  // reads membership synchronously) and to localStorage (guest
  // reload persistence; authenticated lists never enter this guest-only key).
  useEffect(() => {
    wishlistsRef.current = wishlists;
    if (typeof window !== 'undefined' && wishlistOwner === 'guest' && !tokenStore.getToken()) {
      try {
        localStorage.setItem('drup_guest_wishlists', JSON.stringify(wishlists));
      } catch {}
    }
  }, [wishlists, wishlistOwner]);

  useLayoutEffect(() => {
    // Older builds persisted authenticated and guest lists under the same key;
    // their owner cannot be recovered safely, so discard that legacy cache.
    localStorage.removeItem('drup_wishlists');
    if (wishlistOwner !== 'guest') localStorage.removeItem('drup_guest_wishlists');
    if (wishlistOwnerRef.current === wishlistOwner) return;
    const previousOwner = wishlistOwnerRef.current;
    wishlistOwnerRef.current = wishlistOwner;
    mergedGuestScopesRef.current.clear();
    guestMergeListsRef.current = previousOwner === 'guest' && wishlistOwner !== 'guest' ? wishlistCache.lists : {};
    setWishlistCache((previous) => ({ owner: wishlistOwner, lists: previousOwner === 'guest' && wishlistOwner !== 'guest' ? previous.lists : {} }));
    // A signed-in list is never serialized into the guest key.
    localStorage.removeItem('drup_guest_wishlists');
  }, [wishlistOwner]);

  // Login / store-switch hydration. On login, the guest lists of
  // EVERY locally stored scope are merged into the account - each
  // under its own store slug, once per scope per session - and each
  // merged scope then adopts its server list. Merging only the
  // active scope would let a later hydration overwrite the other
  // stores' guest hearts with their un-merged server lists. A scope
  // with no guest items, or whose slug this device never recorded,
  // is left untouched here; the active scope is fetched on every
  // pass, so later store switches just hydrate. API failures leave
  // the local list untouched.
  useEffect(() => {
    if (isMockMode() || !isSessionReady || !currentUser?.id || !authToken || wishlistCache.owner !== wishlistOwner) return;
    const { storeId, storeSlug } = wishlistScope;
    let cancelled = false;
    const generation = tokenStore.getGeneration();
    const isCurrent = () => !cancelled && wishlistOwnerRef.current === wishlistOwner && generation === tokenStore.getGeneration();
    (async () => {
      for (const [guestStoreId, guestIds] of Object.entries(guestMergeListsRef.current)) {
        if (!isCurrent()) return;
        if (!guestIds || guestIds.length === 0) continue;
        if (mergedGuestScopesRef.current.has(guestStoreId)) continue;
        const guestSlug =
          guestStoreId === storeId ? storeSlug : wishlistScopeSlugsRef.current[guestStoreId];
        if (!guestSlug) continue;
        mergedGuestScopesRef.current.add(guestStoreId);
        for (const pid of guestIds) {
          if (!isCurrent()) return;
          try {
            await wishlistApi.add(pid, guestSlug);
          } catch {}
        }
        try {
          const res = await wishlistApi.getWishlist(guestSlug);
          if (isCurrent() && res && Array.isArray(res.wishlist)) {
            setWishlists((prev) => ({ ...prev, [guestStoreId]: res.wishlist }));
          }
        } catch {}
      }
      if (!isCurrent()) return;
      try {
        const res = await wishlistApi.getWishlist(storeSlug);
        if (isCurrent() && res && Array.isArray(res.wishlist)) {
          setWishlists((prev) => ({ ...prev, [storeId]: res.wishlist }));
        }
      } catch {}
    })();
    return () => {
      cancelled = true;
    };
  }, [wishlistOwner, wishlistCache.owner, wishlistScope, isSessionReady]);
  const [productOriginalityOverrides, setProductOriginalityOverrides] = useState<Record<string, boolean>>(
    () => isMockMode() ? ({ 'prod-3': false } as Record<string, boolean>) : {}
  );

  const [customApprovedProducts, setCustomApprovedProducts] = useState<Product[]>(() => {
    if (!isMockMode()) {
      localStorage.removeItem('drup_custom_approved_products');
      return [];
    }
    if (typeof window !== 'undefined') {
      try {
        const saved = localStorage.getItem('drup_custom_approved_products');
        if (saved) return JSON.parse(saved);
      } catch {}
    }
    return [];
  });

  const addProduct = (product: Product) => {
    // Local draft approvals are a demo feature. Live visibility is server-owned.
    if (!isMockMode()) return;
    setCustomApprovedProducts((prev) => {
      const filtered = prev.filter((p) => p.id !== product.id);
      const updated = [product, ...filtered];
      if (typeof window !== 'undefined') {
        try {
          localStorage.setItem('drup_custom_approved_products', JSON.stringify(updated));
        } catch {}
      }
      return updated;
    });
    setBackendCatalogProducts((prev) => {
      const filtered = prev.filter((p) => p.id !== product.id);
      return [product, ...filtered];
    });
  };

  const fetchCategories = async () => {
    try {
      const data = await catalogApi.getCategories();
      if (Array.isArray(data) && data.length > 0) {
        setCategories(data.map(mapBackendCategory));
      }
    } catch (err: any) {
      console.warn('Failed to fetch categories:', err.message);
    }
  };

  const fetchNiches = async () => {
    try {
      const data = await catalogApi.getNiches();
      if (Array.isArray(data) && data.length > 0) {
        setNiches(data);
      }
    } catch (err: any) {
      console.warn('Failed to fetch niches:', err.message);
    }
  };

  // PERF-01: paged catalog. products is the accumulated loaded pages
  // for the current server filter (category + search + catalogFilter);
  // the cache key is that filter as JSON, so an identical filter never
  // refetches and a changed filter always restarts from page 1. An
  // in-flight page is aborted when the filter changes (AbortController)
  // or a newer request wins (request id); stale responses never land.
  const [catalogFilter, setCatalogFilterState] = useState<CatalogFilter>(DEFAULT_CATALOG_FILTER);
  // A4: latest-filter mirror so setCatalogFilter can decide (outside its
  // updater, which must stay pure) whether the merged filter is valid.
  const catalogFilterRef = useRef(catalogFilter);
  catalogFilterRef.current = catalogFilter;
  const [catalogTotal, setCatalogTotal] = useState(0);
  // Keep the last complete page set across superseding fetches and terminal errors.
  const lastLoadedCatalog = useRef<{ products: Product[]; total: number }>({ products: [], total: 0 });
  const [hasMoreCatalog, setHasMoreCatalog] = useState(false);
  const [catalogLoadingMore, setCatalogLoadingMore] = useState(false);
  const [catalogLoadMoreError, setCatalogLoadMoreError] = useState(false);
  const catalogCursorRef = useRef<string | null>(null);
  const catalogFilterKeyRef = useRef('');
  const catalogAbortRef = useRef<AbortController | null>(null);
  const catalogRequestIdRef = useRef(0);
  const catalogLoadingMoreRef = useRef(false);

  const catalogServerFilter = React.useMemo(() => ({
    category: selectedCategory,
    search: searchQuery,
    sort: catalogFilter.sort,
    inStockOnly: catalogFilter.inStockOnly,
    onlyOriginal: catalogFilter.onlyOriginal,
    minPrice: catalogFilter.minPrice,
    maxPrice: catalogFilter.maxPrice,
    brands: catalogFilter.brands,
    attributes: catalogFilter.attributes,
  }), [selectedCategory, searchQuery, catalogFilter]);
  const catalogFilterKey = JSON.stringify(catalogServerFilter);

  const setCatalogFilter = useCallback((patch: Partial<CatalogFilter>) => {
    // A4: a valid merged range clears a stale 400 flag immediately,
    // without waiting for the network (the fetch clears it again on
    // success). An inverted merged range leaves the flag untouched.
    const next = { ...catalogFilterRef.current, ...patch };
    if (next.minPrice == null || next.maxPrice == null || next.minPrice <= next.maxPrice) {
      setCatalogPriceRangeError(false);
    }
    setCatalogFilterState((prev) => {
      const merged = { ...prev, ...patch };
      return JSON.stringify(merged) === JSON.stringify(prev) ? prev : merged;
    });
  }, []);

  const resetCatalogFilter = useCallback(() => {
    setCatalogFilterState(DEFAULT_CATALOG_FILTER);
  }, []);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const syncAfterHistory = () => {
      const path = window.location.pathname.replace(/\/+$/, '') || '/';
      if (path !== '/' && path !== '/preview' && !path.endsWith('/preview')) return;
      const params = new URLSearchParams(window.location.search);
      if (!params.has('q') && !params.has('category')) return;
      const category = params.get('category') || 'all';
      if (category !== selectedCategory) setCatalogFilter(categoryScopedFilters);
      setSelectedCategory(category);
      setSearchQuery(params.get('q') || '');
    };
    window.addEventListener('popstate', syncAfterHistory);
    return () => window.removeEventListener('popstate', syncAfterHistory);
  }, [selectedCategory, setCatalogFilter]);

  const fetchProducts = useCallback(async () => {
    if (isMockMode()) {
      setCatalogLoading(false);
      return;
    }
    catalogAbortRef.current?.abort();
    const controller = new AbortController();
    catalogAbortRef.current = controller;
    const requestId = ++catalogRequestIdRef.current;
    const key = catalogFilterKey;
    const previous = lastLoadedCatalog.current;
    setBackendCatalogProducts([]);
    setCatalogTotal(0);
    setCatalogLoading(true);
    setCatalogError(null);
    setCatalogLoadMoreError(false);
    try {
      const page = await catalogApi.getProductsPage({
        category: catalogServerFilter.category,
        search: catalogServerFilter.search || undefined,
        sort: catalogServerFilter.sort,
        inStockOnly: catalogServerFilter.inStockOnly,
        isOriginal: catalogServerFilter.onlyOriginal || undefined,
        minPrice: catalogServerFilter.minPrice ?? undefined,
        maxPrice: catalogServerFilter.maxPrice ?? undefined,
        brands: catalogServerFilter.brands,
        attributes: catalogServerFilter.attributes,
      }, { signal: controller.signal });
      if (catalogRequestIdRef.current !== requestId || controller.signal.aborted) return;
      const products = page.items.map(mapBackendProduct);
      catalogFilterKeyRef.current = key;
      catalogCursorRef.current = page.nextCursor;
      lastLoadedCatalog.current = { products, total: page.total };
      setHasMoreCatalog(page.hasMore);
      setCatalogTotal(page.total);
      setBackendCatalogProducts(products);
      setCatalogPriceRangeError(false);
    } catch (err: any) {
      if (err?.name === 'AbortError' || controller.signal.aborted) return;
      if (catalogRequestIdRef.current !== requestId) return;
      setBackendCatalogProducts(previous.products);
      setCatalogTotal(previous.total);
      // A4: the backend 400s an inverted range (same-deploy backend
      // half). It is a filter error, not a catalog outage: surface it
      // as the inline price-range message, never the generic catalog
      // error, and keep the previously loaded products on screen.
      // Detection is by status only — the body is the raw http.Error
      // string and must never be message-matched.
      if (err?.status === 400) {
        setCatalogPriceRangeError(true);
        return;
      }
      setCatalogError(err.message || 'خطا در برقراری ارتباط با کاتالوگ');
    } finally {
      if (catalogRequestIdRef.current === requestId) setCatalogLoading(false);
    }
  }, [catalogFilterKey, catalogServerFilter]);

  const loadMoreCatalog = useCallback(async () => {
    if (isMockMode() || catalogLoading || catalogLoadingMoreRef.current) return;
    if (!hasMoreCatalog || !catalogCursorRef.current) return;
    // The filter must not have changed since these pages were loaded;
    // if it did, the page-1 fetch above owns the list now.
    if (catalogFilterKeyRef.current !== catalogFilterKey) return;
    catalogLoadingMoreRef.current = true;
    setCatalogLoadingMore(true);
    setCatalogLoadMoreError(false);
    const requestId = catalogRequestIdRef.current;
    const cursor = catalogCursorRef.current;
    try {
      const page = await catalogApi.getProductsPage({
        category: catalogServerFilter.category,
        search: catalogServerFilter.search || undefined,
        sort: catalogServerFilter.sort,
        inStockOnly: catalogServerFilter.inStockOnly,
        isOriginal: catalogServerFilter.onlyOriginal || undefined,
        minPrice: catalogServerFilter.minPrice ?? undefined,
        maxPrice: catalogServerFilter.maxPrice ?? undefined,
        brands: catalogServerFilter.brands,
        attributes: catalogServerFilter.attributes,
      }, { cursor });
      if (catalogRequestIdRef.current !== requestId || catalogFilterKeyRef.current !== catalogFilterKey) return;
      catalogCursorRef.current = page.nextCursor;
      setHasMoreCatalog(page.hasMore);
      setCatalogTotal(page.total);
      const previous = lastLoadedCatalog.current.products;
      const seen = new Set(previous.map((product) => product.id));
      const fresh = page.items.map(mapBackendProduct).filter((product) => !seen.has(product.id));
      const products = fresh.length === 0 ? previous : [...previous, ...fresh];
      lastLoadedCatalog.current = { products, total: page.total };
      setBackendCatalogProducts(products);
    } catch (err: any) {
      if (catalogRequestIdRef.current !== requestId) return;
      // A failed show-more leaves the loaded pages in place; the user
      // retries with the same button. AbortError here means nothing
      // was aborted (load-more has no signal); ignore it the same way.
      // A4: a 400 here implies the stored filter was inverted (the
      // CatalogView guard prevents it) — map it like page 1.
      if (err?.status === 400) setCatalogPriceRangeError(true);
      else if (err?.name !== 'AbortError') setCatalogLoadMoreError(true);
    } finally {
      catalogLoadingMoreRef.current = false;
      setCatalogLoadingMore(false);
    }
  }, [catalogFilterKey, catalogServerFilter, hasMoreCatalog, catalogLoading]);

  useEffect(() => {
    if (!isMockMode()) {
      fetchCategories();
      fetchNiches();
    }
  }, []);

  // Page 1 reloads whenever the server filter changes (category,
  // search, sort, facets, price). The filter key is a JSON string,
  // so an identical filter never triggers a second fetch.
  useLayoutEffect(() => {
    if (isMockMode()) return;
    fetchProducts();
  }, [fetchProducts]);

  const baseCatalogList = isMockMode() ? mockProducts : backendCatalogProducts;

  // FIX 10: memoize the catalogue identity. mergedList.map() used to build
  // a brand-new array (and new product objects) on every provider render,
  // which retriggered the routed-product effect and looped the spn fetch
  // forever (15 identical 200s in 2s in the preview nginx log).
  const mergedList: Product[] = React.useMemo(() => mergeCatalogLists(baseCatalogList, customApprovedProducts, isMockMode()), [customApprovedProducts, baseCatalogList]);

  const products: Product[] = React.useMemo(() => mergedList.map((p) => ({
    ...p,
    isOriginal: productOriginalityOverrides[p.id] !== undefined
      ? productOriginalityOverrides[p.id]
      : (p.isOriginal ?? true),
  })), [mergedList, productOriginalityOverrides]);

  const openProductDetail = (product: Product) => {
    setCurrentProductDetail(product);
  };

  const openCatalogCategory = (categorySlug: string, query: string = '') => {
    if (categorySlug !== selectedCategory || catalogBrandRequest) setCatalogFilter(categoryScopedFilters);
    setSelectedCategory(categorySlug);
    setSearchQuery(query.trim());
    setCatalogBrandRequest(null);
  };

  const openCatalogBrand = (brand: string) => {
    setSelectedCategory('all');
    setSearchQuery('');
    setCatalogBrandRequest({ brand, nonce: Date.now() });
  };

  const clearCatalogBrand = () => {
    setCatalogBrandRequest(null);
  };

  const toggleWishlist = (productId: string, store?: { id: string; slug?: string }) => {
    // No explicit store -> the scope the shopper is inside; an
    // explicit store (the PDP's routed store, the wishlist view's
    // entry store) wins, so a heart always lands in the list of the
    // site it was tapped on.
    const targetId = store?.id ?? wishlistScope.storeId;
    const targetSlug =
      store?.slug ?? (targetId === wishlistScope.storeId ? wishlistScope.storeSlug : undefined);
    recordWishlistScopeSlug(targetId, targetSlug);
    const adding = !(wishlistsRef.current[targetId] ?? []).includes(productId);
    setWishlists((prev) => {
      const list = prev[targetId] ?? [];
      const next = list.includes(productId)
        ? list.filter((id) => id !== productId)
        : [...list, productId];
      return { ...prev, [targetId]: next };
    });
    // Write-through for logged-in customers: the optimistic local
    // apply stands even if the API call fails.
    if (!isMockMode() && customerUser.isLoggedIn) {
      const call = adding
        ? wishlistApi.add(productId, targetSlug)
        : wishlistApi.remove(productId, targetSlug);
      call.catch(() => {});
    }
  };

  const isWishlisted = (productId: string, store?: { id: string; slug?: string }) =>
    (wishlists[store?.id ?? wishlistScope.storeId] ?? []).includes(productId);

  const toggleProductOriginality = (productId: string, isOriginal: boolean) => {
    setProductOriginalityOverrides((prev) => ({ ...prev, [productId]: isOriginal }));
  };

  // PERF-01: these search/filter the LOADED pages only (no outside
  // consumers remain — server search lives in CatalogView and the
  // header suggestions). A product beyond the loaded pages is not
  // found here; the PDP resolves it from the server by id.
  const searchProducts = (query: string): Product[] => {
    const q = query.trim().toLowerCase();
    if (!q) return products;
    return products.filter((p) => p.title.toLowerCase().includes(q) || p.category.toLowerCase().includes(q));
  };

  const filterByCategory = (categorySlug: string): Product[] => {
    if (categorySlug === 'all') return products;
    return products.filter((p) => p.category === categorySlug);
  };

  const value: ProductContextType = {
    products,
    categories,
    niches,
    catalogLoading,
    catalogError,
    catalogPriceRangeError,
    selectedCategory,
    setSelectedCategory,
    searchQuery,
    setSearchQuery,
    selectedProduct,
    setSelectedProduct,
    currentProductDetail,
    openProductDetail,
    openCatalogCategory,
    catalogBrandRequest,
    openCatalogBrand,
    clearCatalogBrand,
    wishlistIds,
    wishlists,
    toggleWishlist,
    isWishlisted,
    setWishlistScope,
    toggleProductOriginality,
    addProduct,
    fetchProducts,
    fetchCategories,
    fetchNiches,
    searchProducts,
    filterByCategory,
    catalogFilter,
    setCatalogFilter,
    resetCatalogFilter,
    loadMoreCatalog,
    hasMoreCatalog,
    catalogTotal,
    catalogLoadingMore,
    catalogLoadMoreError,
  };

  return <ProductContext.Provider value={value}>{children}</ProductContext.Provider>;
};

export const useProducts = (): ProductContextType => {
  const context = useContext(ProductContext);
  if (!context) {
    throw new Error('useProducts must be used within a ProductProvider');
  }
  return context;
};

