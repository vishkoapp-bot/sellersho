import type { ProductSchema, CatalogProductAttribute } from '../types/productSchema';
import { apiRequest } from './apiClient';

export interface CategoryDto {
  id: string;
  title: string;
  slug: string;
  iconName: string;
  gradient?: string;
  imageUrl?: string;
  parentId?: string | null;
  // FIX 4 (team 04 G8): active-product count in this subtree, served
  // by the backend category list/search responses.
  productCount?: number;
}

// FIX 4 (team 04 G1/G2): the backend variant shape, exactly as live
// serves it: variants carry id + variantName, variantPrices is keyed
// by variant id and carries originalPrice per variant.
export interface ProductVariantDto {
  selection?: Record<string,string>;
  combinationKey?: string;
  inventoryMode?: 'legacy' | 'tracked';
  stockAuthority?: 'coded-v1';
  isActive?: boolean;
  supplierBaseCost?: number;
  // Variant systems P0/P1/P2 (backend names, camelCase on the wire):
  // images from product_variant_images (images[0] = hero), physicals
  // mm/g omitempty (null = inherit product), sale + classes (P1(c)/P2(b)).
  imageUrl?: string | null;
  images?: string[];
  weightGrams?: number | null;
  lengthMm?: number | null;
  widthMm?: number | null;
  heightMm?: number | null;
  shippingClass?: string | null;
  taxClass?: string | null;
  salePrice?: number | null;
  saleFrom?: string | null;
  saleTo?: string | null;
  // FIX 14b: field order is live's (id, name, variantName, spnc,
  // attributes, isDefault, stock, minCartQty, maxCartQty).
  id: string;
  name: string;
  variantName: string;
  spnc: string;
  attributes: Record<string, unknown>;
  isDefault: boolean;
  stock: number;
  minCartQty: number;
  maxCartQty: number | null;
}

export interface VariantPriceDto {
  price: number;
  originalPrice: number;
  discountPercent: number;
  campaignName?: string;
  // P1(c): configured sale price + window; onSale true iff the sale
  // determined this price (price is already the post-sale final then).
  salePrice?: number;
  saleFrom?: string | null;
  saleTo?: string | null;
  onSale?: boolean;
}

// FIX 14b: live PDP addon card (live bundle mapper: addons +
// addonSectionTitle on the detail DTO; feePerUnit is precomputed by
// the backend, never recomputed client-side).
export interface ProductAddonDto {
  id: string;
  title: string;
  description?: string;
  feePerUnit: number;
  ownerType: string;
}

// Team 08 live parity (D40): NEW facet endpoint shapes from Team 04's
// backend (catalog_facet_handlers.go). These replace the live 404s: the
// live frontend called these paths and swallowed the error with an empty
// catch, leaving brand/attribute facets empty. The functions below do
// NOT swallow errors: callers must handle failures explicitly.
export interface FacetBrand {
  id: number;
  code: string;
  titleFa: string;
  titleEn: string;
}

export interface FacetAttributeValue {
  id: number;
  titleFa: string;
  titleEn: string;
}

export interface FacetAttribute {
  id: number;
  title: string;
  type: string;
  values: FacetAttributeValue[];
}

export interface NicheDto {
  id: string;
  title: string;
  slug: string;
  categorySlug: string;
  badge?: string;
  description?: string;
}

export interface ProductDto {
  productType?: 'legacy' | 'simple' | 'variable';
  schemaVersion?: number;
  productSchema?: ProductSchema;
  id: string;
  title: string;
  slug: string;
  category: string;
  supplierId?: string;
  supplierName?: string;
  originalPrice: number;
  price: number;
  // baseCostB is served ONLY on the store view (live parity, FIX 4
  // team 04 G3); it is absent on the central view.
  baseCostB?: number;
  platformFeeF: number;
  profitToman?: number;
  marginPercent?: number;
  discountPercent?: number;
  discountEligible?: boolean;
  campaignName?: string;
  isOriginal: boolean;
  stock: number;
  image: string;
  // FIX 4 (team 04 G2): gallery + specs are served by the backend and
  // must no longer be discarded by the frontend mapping.
  images?: string[];
  specs?: Record<string, string | number | boolean>;
  description: string;
  warehouse: string;
  nicheIds?: string[];
  supplierWarranty?: string;
  spnNumber?: number;
  isActive?: boolean;
  brand?: string;
  storeCount?: number;
  // P0: product-level physicals (variant fallback target).
  weightGrams?: number;
  lengthMm?: number;
  widthMm?: number;
  heightMm?: number;
  // P1(a): per-store price range (*int64 omitempty server-side).
  priceMin?: number;
  priceMax?: number;
  // P2(c): this store's default variant (store-scoped DTOs only).
  storeDefaultVariantId?: string | null;
  variants?: ProductVariantDto[];
  variantPrices?: Record<string, VariantPriceDto>;
  addons?: ProductAddonDto[];
  addonSectionTitle?: string;
  attributeValues?: { attributeId: number; valueId: number }[];
}

export interface ProductFilterParams {
  category?: string;
  niche?: string;
  search?: string;
  sort?: string;
  storeSlug?: string;
  inStockOnly?: boolean;
  isOriginal?: boolean;
  // PERF-01: server-side facets (previously client-side over the full
  // list). brands repeat (?brand=A&brand=B); attributes are
  // attr_<id>=1,2,3. minPrice/maxPrice filter on the DISPLAYED main
  // price (card price). limit is the page size (default 24, max 100).
  brands?: string[];
  attributes?: Record<number, number[]>;
  minPrice?: number;
  maxPrice?: number;
  limit?: number;
  // E2 (Batch 3): set ONLY by the header's debounced suggestion fetch;
  // the backend skips search logging when suggest=1. The submitted
  // search (Enter) and every other list read must NOT set it.
  suggest?: boolean;
  // B1(a) carve-out (Batch 3): honoured ONLY for an authenticated
  // request whose caller owns the store (OwnedStore); anonymous and
  // public callers get the filtered list. The seller management tab
  // passes it; the storefront never does.
  includeInactive?: boolean;
}

// PERF-01: the paged list envelope. items is always an array (never
// null); nextCursor is null on the last page; total is the full
// filtered count, stable across pages.
export interface ProductListPage {
  items: ProductDto[];
  nextCursor: string | null;
  hasMore: boolean;
  total: number;
  // M-2 (Batch 3 fix): echoes whether the includeInactive carve-out
  // was actually honoured for this request (owner-gated server-side).
  // False means the flag was ignored (anonymous / non-owner / expired
  // token) — the caller must NOT render the filtered list as the
  // whole assortment in that case.
  includeInactiveApplied?: boolean;
}

// M-1 (Batch 3 fix): the result of a whole-list walk. items is
// the pages loaded so far; complete is false when the walk
// stopped early (a page failed, or maxPages ran out with more
// pages left) — in that case items is PARTIAL and must never be
// presented as the whole assortment. includeInactiveApplied is
// the first page's echo of the M-2 signal (defined only when
// the walk asked for inactive and at least one page answered).
export interface ProductWalkResult {
  items: ProductDto[];
  complete: boolean;
  includeInactiveApplied?: boolean;
}

export const catalogApi = {
  getProductAttributes: (slug: string) => apiRequest<{attributes: CatalogProductAttribute[]; brands: FacetBrand[]}>(`/categories/${encodeURIComponent(slug)}/product-attributes`, {method:'GET'}),
  getCategories: async (): Promise<CategoryDto[]> => {
    return apiRequest<CategoryDto[]>('/categories', { method: 'GET' });
  },

  getNiches: async (categorySlug?: string): Promise<NicheDto[]> => {
    const query = categorySlug ? `?category=${encodeURIComponent(categorySlug)}` : '';
    return apiRequest<NicheDto[]>(`/niches${query}`, { method: 'GET' });
  },

  // PERF-01: the ONLY product-list read. One bounded page: selection,
  // pricing and the total all happen server-side in SQL; the client
  // never fetches the full catalog. The old unpaged getProducts is
  // deleted — any caller still on it fails at compile time. Pass
  // cursor (from a previous page) to continue paging the SAME filter;
  // a changed filter must start again without a cursor. signal aborts
  // an obsolete in-flight page (filter change, unmount).
  getProductsPage: async (params: ProductFilterParams = {}, opts: { cursor?: string; signal?: AbortSignal } = {}): Promise<ProductListPage> => {
    const searchParams = new URLSearchParams();
    if (params.category && params.category !== 'all') searchParams.set('category', params.category);
    if (params.niche) searchParams.set('niche', params.niche);
    if (params.search) searchParams.set('search', params.search);
    if (params.sort) searchParams.set('sort', params.sort);
    if (params.storeSlug) searchParams.set('storeSlug', params.storeSlug);
    if (params.inStockOnly) searchParams.set('in_stock', 'true');
    if (params.isOriginal) searchParams.set('is_original', 'true');
    for (const brand of params.brands ?? []) searchParams.append('brand', brand);
    for (const [attrId, valueIds] of Object.entries(params.attributes ?? {})) {
      if (valueIds.length > 0) searchParams.set(`attr_${attrId}`, valueIds.join(','));
    }
    if (params.minPrice != null) searchParams.set('min_price', String(params.minPrice));
    if (params.maxPrice != null) searchParams.set('max_price', String(params.maxPrice));
    if (params.limit != null) searchParams.set('limit', String(params.limit));
    if (params.suggest) searchParams.set('suggest', '1');
    if (params.includeInactive) searchParams.set('includeInactive', '1');
    if (opts.cursor) searchParams.set('cursor', opts.cursor);

    const qs = searchParams.toString();
    // The Go mux owns the trailing-slash collection route; keep detail routes unchanged.
    const endpoint = `/products/${qs ? `?${qs}` : ''}`;
    // Store-scoped reads carry the session token: the backend only
    // serves cost fields (baseCostB/platformFeeF/supplier*) on a scoped
    // response to the store owner, the product's supplier, or an admin.
    // Unscoped (public/central) requests stay anonymous, exactly as before.
    return apiRequest<ProductListPage>(endpoint, {
      method: 'GET',
      ...(params.storeSlug ? { auth: true } : {}),
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
  },

  // PERF-01: page through a whole (small, admin/personal) list with
  // bounded requests — seller catalog management, template preview,
  // wishlist lookup. NOT for public surfaces: those page on demand
  // (getProductsPage + show-more) and never walk the whole catalog.
  // Stops early when stopWhen returns true, and never walks more than
  // maxPages pages (default 50 x limit 100) — a runaway walk returns
  // what it loaded instead of looping forever.
  // M-1 (Batch 3 fix): a mid-walk page failure returns the pages
  // already loaded with complete: false — it never throws everything
  // away, and a caller must never present a partial list as the
  // whole assortment. maxPages exhausted with pages left is likewise
  // incomplete. stopWhen is the caller's own early stop, so it
  // counts as complete for that caller's purpose.
  fetchAllProductPages: async (params: ProductFilterParams = {}, opts: { maxPages?: number; stopWhen?: (items: ProductDto[]) => boolean } = {}): Promise<ProductWalkResult> => {
    const maxPages = opts.maxPages ?? 50;
    const limit = params.limit ?? 100;
    const items: ProductDto[] = [];
    let cursor: string | undefined;
    let includeInactiveApplied: boolean | undefined;
    const done = (complete: boolean): ProductWalkResult =>
      includeInactiveApplied === undefined ? { items, complete } : { items, complete, includeInactiveApplied };
    for (let page = 0; page < maxPages; page++) {
      let res: ProductListPage;
      try {
        res = await catalogApi.getProductsPage({ ...params, limit }, { cursor });
      } catch {
        return done(false);
      }
      if (includeInactiveApplied === undefined) includeInactiveApplied = res.includeInactiveApplied;
      items.push(...res.items);
      if (opts.stopWhen?.(items)) return done(true);
      if (!res.hasMore || !res.nextCursor) return done(true);
      cursor = res.nextCursor;
    }
    return done(false);
  },

  getProductById: async (id: string, storeSlug?: string): Promise<ProductDto> => {
    const qs = storeSlug ? `?storeSlug=${encodeURIComponent(storeSlug)}` : '';
    // Same scoped-cost gate as getProductsPage: authenticate only when a
    // store scope is present; unscoped detail requests stay anonymous.
    return apiRequest<ProductDto>(`/products/${id}${qs}`, {
      method: 'GET',
      ...(storeSlug ? { auth: true } : {}),
    });
  },

  // Team 04 server spn lookup (D46): GET /api/v1/products/spn-<n>.
  // This is the ONLY spn lookup used by the frontend; no second lookup
  // is built in the router or anywhere else.
  getProductBySpn: async (spnNumber: number, storeSlug?: string): Promise<ProductDto> => {
    return catalogApi.getProductById(`spn-${spnNumber}`, storeSlug);
  },

  getBrands: async (): Promise<FacetBrand[]> => {
    return apiRequest<FacetBrand[]>('/brands', { method: 'GET' });
  },

  getCategoryBrands: async (slug: string): Promise<FacetBrand[]> => {
    return apiRequest<FacetBrand[]>(`/categories/${encodeURIComponent(slug)}/brands`, { method: 'GET' });
  },

  // FIX 14b: live's catalog component reads ye.attributes, so the
  // shape is the wrapped object (backend wraps it the same way).
  getCategoryAttributes: async (slug: string): Promise<{ attributes: FacetAttribute[]; colors: { id: number; title: string; hexCode: string }[]; brands: { id: number; titleFa: string; titleEn: string }[]; inherited: boolean }> => {
    return apiRequest<{ attributes: FacetAttribute[]; colors: { id: number; title: string; hexCode: string }[]; brands: { id: number; titleFa: string; titleEn: string }[]; inherited: boolean }>(`/categories/${encodeURIComponent(slug)}/attributes`, { method: 'GET' });
  },

  // Server caps search at 50 and returns [] for an empty query; the
  // client mirrors both so an empty query never hits the network.
  searchCategories: async (query: string): Promise<CategoryDto[]> => {
    if (!query || !query.trim()) return [];
    return apiRequest<CategoryDto[]>(`/categories/search?q=${encodeURIComponent(query.trim())}`, { method: 'GET' });
  },
  // FIX 14: live server price bounds (GET /products/price-bounds =
  // {"minPrice":198000,"maxPrice":63800000,"count":40}); the grid and
  // catalog sliders must use these, not client-computed bounds.
  getPriceBounds: async (params: { category?: string; storeSlug?: string } = {}): Promise<{ minPrice: number; maxPrice: number; count: number }> => {
    const qs = new URLSearchParams();
    if (params.category && params.category !== 'all') qs.set('category', params.category);
    if (params.storeSlug) qs.set('storeSlug', params.storeSlug);
    const q = qs.toString();
    return apiRequest<{ minPrice: number; maxPrice: number; count: number }>(`/products/price-bounds${q ? `?${q}` : ''}`, { method: 'GET' });
  },

  // PERF-01: the DISTINCT category slugs one store actually sells, in
  // one bounded query. Replaces deriving them from the full catalog.
  getStoreCategorySlugs: async (storeSlug?: string): Promise<string[]> => {
    const qs = storeSlug ? `?storeSlug=${encodeURIComponent(storeSlug)}` : '';
    const res = await apiRequest<{ slugs: string[] }>(`/products/store-categories${qs}`, { method: 'GET' });
    return res?.slugs ?? [];
  },

};
