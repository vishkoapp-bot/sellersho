import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { Product, Category, Niche } from '../types';
import { mockCategories, mockProducts } from '../data/mockData';
import { catalogApi, ProductDto } from '../services/catalogApi';
import { isMockMode } from '../services/apiClient';

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

export const mapBackendProduct = (product: ProductDto): Product => ({
  id: product.id,
  title: product.title,
  slug: product.slug,
  spnNumber: product.spnNumber,
  category: product.category,
  supplierId: product.supplierId || 'sup-technolife',
  supplierName: product.supplierName || (product.supplierId === 'sup-khanoumi' ? 'خانومی' : 'تکنولایف'),
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
  storeCount: product.storeCount,
  variants: product.variants,
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
  toggleWishlist: (productId: string) => void;
  isWishlisted: (productId: string) => boolean;
  toggleProductOriginality: (productId: string, isOriginal: boolean) => void;
  addProduct: (product: Product) => void;
  fetchProducts: () => Promise<void>;
  fetchCategories: () => Promise<void>;
  fetchNiches: () => Promise<void>;
  searchProducts: (query: string) => Product[];
  filterByCategory: (categorySlug: string) => Product[];
}

const ProductContext = createContext<ProductContextType | undefined>(undefined);
export const ProductProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
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
  const [wishlistIds, setWishlistIds] = useState<string[]>([]);
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

  const toggleWishlist = (productId: string) => {
    setWishlistIds((prev) =>
      prev.includes(productId) ? prev.filter((id) => id !== productId) : [...prev, productId]
    );
  };

  const isWishlisted = (productId: string) => wishlistIds.includes(productId);

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
    toggleWishlist,
    isWishlisted,
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

