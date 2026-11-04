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
}

export const catalogApi = {
  getCategories: async (): Promise<CategoryDto[]> => {
    return apiRequest<CategoryDto[]>('/categories', { method: 'GET' });
  },

  getNiches: async (categorySlug?: string): Promise<NicheDto[]> => {
    const query = categorySlug ? `?category=${encodeURIComponent(categorySlug)}` : '';
    return apiRequest<NicheDto[]>(`/niches${query}`, { method: 'GET' });
  },

  getProducts: async (params: ProductFilterParams = {}): Promise<ProductDto[]> => {
    const searchParams = new URLSearchParams();
    if (params.category && params.category !== 'all') searchParams.set('category', params.category);
    if (params.niche) searchParams.set('niche', params.niche);
    if (params.search) searchParams.set('search', params.search);
    if (params.sort) searchParams.set('sort', params.sort);
    if (params.storeSlug) searchParams.set('storeSlug', params.storeSlug);
    if (params.inStockOnly) searchParams.set('in_stock', 'true');

    const qs = searchParams.toString();
    // The Go mux owns the trailing-slash collection route; keep detail routes unchanged.
    const endpoint = `/products/${qs ? `?${qs}` : ''}`;
    // FIX 4 (team 04 G5): the backend encodes an empty list as JSON
    // null exactly like live; normalise it so callers always get [].
    const data = await apiRequest<ProductDto[] | null>(endpoint, { method: 'GET' });
    return data ?? [];
  },

  getProductById: async (id: string, storeSlug?: string): Promise<ProductDto> => {
    const qs = storeSlug ? `?storeSlug=${encodeURIComponent(storeSlug)}` : '';
    return apiRequest<ProductDto>(`/products/${id}${qs}`, { method: 'GET' });
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
  getPriceBounds: async (): Promise<{ minPrice: number; maxPrice: number; count: number }> => {
    return apiRequest<{ minPrice: number; maxPrice: number; count: number }>('/products/price-bounds', { method: 'GET' });
  },

};
