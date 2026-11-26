// Team 08 live parity (D45/D46): pathname router for the public SPA.
// Live evidence (Phase-1 Team 08 report, dist index-MERGED.js): functions
// Tr (route parse), Fw (/store/<slug> match), Cy (canonical product URL),
// LS (Persian/Arabic + Persian-digit normalisation), US (spn parse),
// Wf/IS (seller dashboard ?panel=seller&tab=<tab>).
// The source had only state-based ActiveView (15 values) and a single
// /panels pathname route; this module replaces that with the live
// pathname router while keeping every existing view working.
// TODO(U43): the real DOM render of a resolved storefront could not be
// verified from this role (no live browser); the supervisor must check
// it in a browser before claiming live DOM parity.

import { RESERVED_SLUGS } from './validators';
import type { StaticPageKey } from '../context/storeContextTypes';

export { RESERVED_SLUGS };

export const SELLER_DASHBOARD_TABS = [
  'overview',
  'orders',
  'returns',
  'catalog',
  'template',
  'pricing',
  'wallet',
  'store-setup',
  'links',
  'analytics',
  'search-insight',
  'discount-campaigns',
  'discount-codes',
  'subscribers',
  'referral',
  'subscription',
  'ads',
  'builder',
  'support',
] as const;

export type SellerDashboardTab = (typeof SELLER_DASHBOARD_TABS)[number];

export function isSellerDashboardTab(value: string | null): value is SellerDashboardTab {
  return !!value && (SELLER_DASHBOARD_TABS as readonly string[]).includes(value);
}

export const STATIC_PAGE_PATHS: Record<StaticPageKey, string> = {
  about: '/about',
  terms: '/terms',
  privacy: '/privacy',
  faq: '/faq',
  license: '/license',
};

const STATIC_PAGE_KEYS: Record<string, StaticPageKey> = {
  about: 'about',
  terms: 'terms',
  privacy: 'privacy',
  faq: 'faq',
  license: 'license',
};

// The central store's slug (backend store row, also a RESERVED_SLUG so
// no dropshipper can claim it). A store slug equal to this is NOT a
// store scope: scoped product URLs are only built for real stores.
export const CENTRAL_STORE_SLUG = 'central';

export function isStoreScopeSlug(slug: string | null | undefined): slug is string {
  return !!slug && slug !== CENTRAL_STORE_SLUG;
}

// Normalise Persian/Arabic letters and Persian/Arabic digits to a single
// form (live LS behaviour): Arabic yeh/kaf -> Persian yeh/kaf, all
// Arabic-Indic and Persian digits -> Latin digits.
export function normalizePersianText(value: string): string {
  return value
    .replace(/[ي]/g, 'ی')
    .replace(/[ك]/g, 'ک')
    .replace(/[٠-٩]/g, (d) => String('٠١٢٣٤٥٦٧٨٩'.indexOf(d)))
    .replace(/[۰-۹]/g, (d) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(d)));
}

export function normalizeProductSlug(value: string): string {
  const normalized = normalizePersianText(value.trim());
  return normalized
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
}

// Canonical product URL (live Cy): /product/spn-<n>/<slug>/ when the
// product has a numeric spnNumber, otherwise /product/<id>. The ?spnc=
// variant query is preserved by the caller, not added here.
// Server lookup for spn- is Team 04's: GET /api/v1/products/spn-<n>
// (catalogApi.getProductBySpn). No second lookup is built here.
export function buildProductUrl(
  product: {
    id: string;
    slug?: string;
    spnNumber?: number | null;
  },
  storeSlug?: string,
): string {
  let base: string;
  if (product.spnNumber !== undefined && product.spnNumber !== null) {
    const slug = product.slug ? normalizeProductSlug(product.slug) : product.id;
    base = `/product/spn-${product.spnNumber}/${slug}/`;
  } else {
    base = `/product/${encodeURIComponent(product.id)}`;
  }
  // Store-carrying form: /store/<slug>/product/... — the only URL
  // shape that names a store AND a product, so a copied/shared link
  // lands on that store's PDP at that store's prices. The central
  // store (and no scope) keep the bare product path forever.
  if (isStoreScopeSlug(storeSlug)) {
    return `/store/${encodeURIComponent(storeSlug)}${base}`;
  }
  return base;
}

export function parseSpnSegment(segment: string): number | null {
  const match = /^spn-(\d+)$/.exec(normalizePersianText(segment));
  if (!match) return null;
  const n = Number(match[1]);
  return Number.isSafeInteger(n) ? n : null;
}

export type ParsedRoute =
  | { view: 'admin_panel' }
  | { view: 'catalog'; query?: string; category?: string }
  | { view: 'categories_tree' }
  | { view: 'supplier_landing' | 'dropshipper_landing' }
  | { view: 'customer' }
  | { view: 'customer_profile' | 'customer_orders' | 'customer_wishlist'; storeSlug?: string }
  | { view: 'supplier_dashboard' }
  | { view: 'warehouse_panel' }
  | { view: 'dashboard' }
  | { view: 'seller_dashboard'; sellerTab: SellerDashboardTab }
  | { view: 'seller_storefront'; slug: string; unsubscribe: boolean }
  | { view: 'product_detail'; id?: string; spnNumber?: number; spnc?: string; storeSlug?: string }
  | { view: 'static_page'; staticPageKey: StaticPageKey; storeSlug?: string }
  | { view: 'not_found' };

// Shared tail parser for product paths: the segment(s) after
// /product/ (or after /store/<slug>/product/). Returns null when
// there is no product segment at all.
function parseProductRemainder(
  rest: string,
  params: URLSearchParams,
): { id?: string; spnNumber?: number; spnc?: string } | null {
  const parts = rest.replace(/\/+$/, '').split('/').filter(Boolean);
  if (parts.length === 0) return null;
  const spn = parseSpnSegment(parts[0]);
  if (spn !== null) {
    return { spnNumber: spn, spnc: params.get('spnc') || undefined };
  }
  return { id: decodeURIComponent(parts[0]) };
}

export function parsePathname(pathname: string, search = ''): ParsedRoute {
  const path = pathname.replace(/\/+$/, '') || '/';
  const params = new URLSearchParams(search);

  if (path === '/admin' || path.startsWith('/admin/')) {
    return { view: 'admin_panel' };
  }

  if (path === '/seller') return { view: 'seller_dashboard', sellerTab: 'overview' };
  if (path === '/supplier') return { view: 'supplier_dashboard' };
  if (path === '/warehouse') return { view: 'warehouse_panel' };
  const account = /^(?:\/store\/([^/]+))?\/account(\/orders|\/wishlist)?$/.exec(path);
  if (account) {
    const slug = account[1] && decodeURIComponent(account[1]);
    const view = account[2] === '/orders' ? 'customer_orders' : account[2] === '/wishlist' ? 'customer_wishlist' : 'customer_profile';
    return { view, storeSlug: isStoreScopeSlug(slug) ? slug : undefined };
  }

  // Seller dashboard tab URLs (live Wf): ?panel=seller&tab=<tab>.
  if (params.get('panel') === 'seller') {
    const tab = params.get('tab');
    return {
      view: 'seller_dashboard',
      sellerTab: isSellerDashboardTab(tab) ? tab : 'overview',
    };
  }

  // Store-scoped product URL: /store/<slug>/product/<id|spn-n/...>.
  // Matched BEFORE the plain store route: the link names both the
  // store and the product, and must land on that store's PDP. The
  // central slug is not a scope (bare product route, as always).
  const storeProductMatch = /^\/store\/([^/?#]+)\/product\/(.+)$/.exec(path);
  if (storeProductMatch) {
    const product = parseProductRemainder(storeProductMatch[2], params);
    if (product) {
      const slug = decodeURIComponent(storeProductMatch[1]);
      return {
        view: 'product_detail',
        ...product,
        storeSlug: isStoreScopeSlug(slug) ? slug : undefined,
      };
    }
  }

  if (path === '/store/central') return parsePathname('/', search);

  const storeMatch = /^\/store\/([^/?#]+)(\/unsubscribe)?$/.exec(path);
  if (storeMatch) {
    return {
      view: 'seller_storefront',
      slug: decodeURIComponent(storeMatch[1]),
      unsubscribe: !!storeMatch[2],
    };
  }

  const productMatch = /^\/product\/(.+)$/.exec(path);
  if (productMatch) {
    const product = parseProductRemainder(productMatch[1], params);
    if (product) {
      // Legacy seed: /product/<id>?storeSlug=<slug> scopes the PDP
      // to that store, exactly like the /store/<slug>/product/ path
      // form. The bare path itself stays the central PDP forever.
      const seed = params.get('storeSlug');
      return {
        view: 'product_detail',
        ...product,
        storeSlug: isStoreScopeSlug(seed) ? (seed as string) : undefined,
      };
    }
  }

  const staticMatch = /^(?:\/store\/([^/]+))?\/([^/]+)$/.exec(path);
  const staticKey = staticMatch ? STATIC_PAGE_KEYS[staticMatch[2]] : undefined;
  if (staticKey) {
    const slug = staticMatch?.[1] && decodeURIComponent(staticMatch[1]);
    return { view: 'static_page', staticPageKey: staticKey, ...(isStoreScopeSlug(slug) ? { storeSlug: slug } : {}) };
  }

  // Live search/catalog URLs live on the root path: /?q=<query> shows
  // the search page and /?category=<slug> the category catalogue. The
  // previous router ignored the query entirely and always returned the
  // homepage, so a direct /?q=... link showed the homepage.
  if (path === '/' || path === '') {
    const q = params.get('q');
    const category = params.get('category');
    if (q || category) return { view: 'catalog', ...(q ? { query: q } : {}), ...(category ? { category } : {}) };
    return { view: 'customer' };
  }

  if (path === '/categories') return { view: 'categories_tree' };
  if (path === '/become-supplier') return { view: 'supplier_landing' };
  if (path === '/become-seller') return { view: 'dropshipper_landing' };
  if (path === '/dashboard') return { view: 'dashboard' };

  // Live behaviour: unknown URLs render the NEW not_found view (it did
  // not exist in the source, where unknown URLs fell back to home).
  return { view: 'not_found' };
}

export function pathForStoreDomain(pathname: string, slug: string): string {
  if (!isStoreScopeSlug(slug)) return pathname;
  const path = pathname.replace(/\/+$/, '') || '/';
  const base = `/store/${encodeURIComponent(slug)}`;
  if (path === '/' || path === '/categories') return base;
  if (path === base || path.startsWith(`${base}/`)) return pathname;
  if (path.startsWith('/store/')) return base;
  if (path.startsWith('/product/') || /^\/account(?:\/orders|\/wishlist)?$/.test(path) || STATIC_PAGE_KEYS[path.slice(1)]) {
    return `${base}${pathname}`;
  }
  return pathname;
}

export function pathForView(
  view: string,
  options: {
    storeSlug?: string;
    sellerTab?: string;
    productUrl?: string;
    staticPageKey?: StaticPageKey;
    searchQuery?: string;
    categorySlug?: string;
  } = {},
): string {
  switch (view) {
    case 'catalog': {
      const query = options.searchQuery?.trim();
      const category = options.categorySlug || 'all';
      const parts = [];
      if (query) parts.push(`q=${encodeURIComponent(query)}`);
      if (!query || category !== 'all') parts.push(`category=${encodeURIComponent(category)}`);
      return `/?${parts.join('&')}`;
    }
    case 'categories_tree':
      return '/categories';
    case 'supplier_landing':
      return '/become-supplier';
    case 'dropshipper_landing':
      return '/become-seller';
    case 'admin_panel':
      return '/admin';
    case 'seller_storefront':
      return isStoreScopeSlug(options.storeSlug) ? `/store/${encodeURIComponent(options.storeSlug)}` : '/';
    case 'customer_profile':
    case 'customer_orders':
    case 'customer_wishlist': {
      const prefix = isStoreScopeSlug(options.storeSlug) ? `/store/${encodeURIComponent(options.storeSlug)}` : '';
      const suffix = view === 'customer_orders' ? '/orders' : view === 'customer_wishlist' ? '/wishlist' : '';
      return `${prefix}/account${suffix}`;
    }
    case 'product_detail': {
      const productPath = options.productUrl || '/';
      // A store-scoped product view keeps its store prefix, so a
      // caller building the path from a bare product URL stays
      // inside the store.
      if (isStoreScopeSlug(options.storeSlug) && productPath.startsWith('/product/')) {
        return `/store/${encodeURIComponent(options.storeSlug)}${productPath}`;
      }
      return productPath;
    }
    case 'static_page': {
      const path = options.staticPageKey ? STATIC_PAGE_PATHS[options.staticPageKey] : '/';
      return isStoreScopeSlug(options.storeSlug) ? `/store/${encodeURIComponent(options.storeSlug)}${path}` : path;
    }
    case 'dashboard':
      return '/dashboard';
    case 'seller_dashboard':
      return options.sellerTab && options.sellerTab !== 'overview'
        ? `/?panel=seller&tab=${options.sellerTab}`
        : '/seller';
    case 'supplier_dashboard':
      return '/supplier';
    case 'warehouse_panel':
      return '/warehouse';
    case 'not_found':
      return '/';
    default:
      return '/';
  }
}
