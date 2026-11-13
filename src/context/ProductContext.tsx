import React, { createContext, useContext, useState, useEffect, useRef, useCallback, ReactNode } from 'react';
import { Product, Category, Niche } from '../types';
import { mockCategories, mockProducts } from '../data/mockData';
import { catalogApi, ProductDto } from '../services/catalogApi';
import { isMockMode } from '../services/apiClient';
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
}

const EMPTY_WISHLIST_IDS: string[] = [];

const ProductContext = createContext<ProductContextType | undefined>(undefined);
export const ProductProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const { customerUser } = useAuth();
  const [backendCatalogProducts, setBackendCatalogProducts] = useState<Product[]>([]);
  const [categories, setCategories] = useState<Category[]>(() => isMockMode() ? mockCategories : []);
  const [niches, setNiches] = useState<Niche[]>([]);
  const [catalogLoading, setCatalogLoading] = useState(!isMockMode());
  const [catalogError, setCatalogError] = useState<string | null>(null);
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

  // Back/forward through a search/catalog URL re-reads the query from
  // the URL (the router only sets the view; the filters live here).
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const syncFromUrl = () => {
      const params = new URLSearchParams(window.location.search);
      const path = window.location.pathname.replace(/\/+$/, '') || '/';
      const isRoot = path === '/' || path === '/preview' || path.endsWith('/preview');
      if (!isRoot) return;
      if (params.get('q') !== null) setSearchQuery(params.get('q') || '');
      if (params.get('category') !== null) setSelectedCategory(params.get('category') || 'all');
    };
    window.addEventListener('popstate', syncFromUrl);
    return () => window.removeEventListener('popstate', syncFromUrl);
  }, []);
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
  const [wishlists, setWishlists] = useState<Record<string, string[]>>(() => {
    if (typeof window !== 'undefined') {
      try {
        const saved = localStorage.getItem('drup_wishlists');
        if (saved) {
          const parsed = JSON.parse(saved);
          if (parsed && typeof parsed === 'object') return parsed as Record<string, string[]>;
        }
      } catch {}
    }
    return {};
  });
  const [wishlistScope, setWishlistScopeState] = useState<{ storeId: string; storeSlug: string }>({
    storeId: 'store-central',
    storeSlug: 'central',
  });
  const wishlistsRef = useRef(wishlists);
  const mergedGuestScopesRef = useRef<Set<string>>(new Set());
  const wasLoggedInRef = useRef(false);
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
  // reload persistence; AuthContext clears the key on logout).
  useEffect(() => {
    wishlistsRef.current = wishlists;
    if (typeof window !== 'undefined') {
      try {
        localStorage.setItem('drup_wishlists', JSON.stringify(wishlists));
      } catch {}
    }
  }, [wishlists]);

  // Logout: guest lists die with the session (the server lists are
  // re-fetched on the next login).
  useEffect(() => {
    if (wasLoggedInRef.current && !customerUser.isLoggedIn) {
      setWishlists({});
      mergedGuestScopesRef.current.clear();
    }
    wasLoggedInRef.current = customerUser.isLoggedIn;
  }, [customerUser.isLoggedIn]);

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
    if (isMockMode() || !customerUser.isLoggedIn) return;
    const { storeId, storeSlug } = wishlistScope;
    let cancelled = false;
    (async () => {
      for (const [guestStoreId, guestIds] of Object.entries(wishlistsRef.current)) {
        if (!guestIds || guestIds.length === 0) continue;
        if (mergedGuestScopesRef.current.has(guestStoreId)) continue;
        const guestSlug =
          guestStoreId === storeId ? storeSlug : wishlistScopeSlugsRef.current[guestStoreId];
        if (!guestSlug) continue;
        mergedGuestScopesRef.current.add(guestStoreId);
        for (const pid of guestIds) {
          try {
            await wishlistApi.add(pid, guestSlug);
          } catch {}
        }
        try {
          const res = await wishlistApi.getWishlist(guestSlug);
          if (!cancelled && res && Array.isArray(res.wishlist)) {
            setWishlists((prev) => ({ ...prev, [guestStoreId]: res.wishlist }));
          }
        } catch {}
      }
      try {
        const res = await wishlistApi.getWishlist(storeSlug);
        if (!cancelled && res && Array.isArray(res.wishlist)) {
          setWishlists((prev) => ({ ...prev, [storeId]: res.wishlist }));
        }
      } catch {}
    })();
    return () => {
      cancelled = true;
    };
  }, [customerUser.isLoggedIn, wishlistScope]);
  const [productOriginalityOverrides, setProductOriginalityOverrides] = useState<Record<string, boolean>>(
    () => isMockMode() ? ({ 'prod-3': false } as Record<string, boolean>) : {}
  );

  const [customApprovedProducts, setCustomApprovedProducts] = useState<Product[]>(() => {
    if (typeof window !== 'undefined') {
      try {
        const saved = localStorage.getItem('drup_custom_approved_products');
        if (saved) return JSON.parse(saved);
      } catch {}
    }
    return [];
  });

  const addProduct = (product: Product) => {
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

  const fetchProducts = async () => {
    setCatalogLoading(true);
    setCatalogError(null);
    try {
      const data = await catalogApi.getProducts();
      if (Array.isArray(data) && data.length > 0) {
        setBackendCatalogProducts(data.map(mapBackendProduct));
      }
    } catch (err: any) {
      setCatalogError(err.message || 'خطا در برقراری ارتباط با کاتالوگ');
    } finally {
      setCatalogLoading(false);
    }
  };

  useEffect(() => {
    if (!isMockMode()) {
      fetchCategories();
      fetchNiches();
      fetchProducts();
    }
  }, []);

  const baseCatalogList = isMockMode() ? mockProducts : backendCatalogProducts;

  // FIX 10: memoize the catalogue identity. mergedList.map() used to build
  // a brand-new array (and new product objects) on every provider render,
  // which retriggered the routed-product effect and looped the spn fetch
  // forever (15 identical 200s in 2s in the preview nginx log).
  const mergedList: Product[] = React.useMemo(() => [
    ...customApprovedProducts,
    ...baseCatalogList.filter((b) => !customApprovedProducts.some((c) => c.id === b.id)),
  ], [customApprovedProducts, baseCatalogList]);

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
    setSelectedCategory(categorySlug);
    setSearchQuery(query);
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

