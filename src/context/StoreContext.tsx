import { useSellerOnboardingDraft } from './SellerOnboardingContext';
import React, { createContext, useContext, useState, useEffect, useLayoutEffect, useRef, ReactNode } from 'react';
import { SearchX, ArrowRight } from 'lucide-react';
import { 
  Store, 
  StoreBuilderConfig, 
  StoreSocialLinks, 
  StoreNameChangeRequest, 
  StoreSlugChangeRequest, 
  CustomDomainRequest, 
  PreMadeWidget,
  Product,
  Order,
  CartItem,
  Category,
} from '../types';
import { 
  ActiveView, 
  StaticPageKey, 
  StoreContextType,
  DirectBuy,
  DirectBuyAddress,
  DirectBuyStore
} from './storeContextTypes';
import { formatPrice, toPersianDigits } from '../utils/formatters';
import { cartQuantityAllowed, resolvePurchase } from '../utils/productPurchase';
import { isMockMode } from '../services/apiClient';
import { storeApi } from '../services/storeApi';
import { catalogApi } from '../services/catalogApi';
import { mapBackendProduct } from './ProductContext';
import { parsePathname, buildProductUrl, pathForView, isSellerDashboardTab, CENTRAL_STORE_SLUG, isStoreScopeSlug } from '../utils/routing';
import { mockStores } from '../data/mockData';

// Sibling Context Hooks
import { useAuth } from './AuthContext';
import { useProducts } from './ProductContext';
import { useCart } from './CartContext';
import { useOrders } from './OrderContext';
import { useFinance } from './FinanceContext';
import { useWarehouse } from './WarehouseContext';

export { useAuth, useProducts, useCart, useOrders, useFinance, useWarehouse };
export type { DirectBuy, DirectBuyAddress, DirectBuyStore };
export { formatPrice, toPersianDigits };

const INITIAL_NAME_CHANGE_REQUESTS: StoreNameChangeRequest[] = [
  {
    id: 'REQ-N-101',
    storeId: 'store-bibi',
    currentName: 'فروشگاه بی‌بی استایل',
    requestedName: 'مزون لوکس بی‌بی استایل تهران',
    sellerMobile: '09121112233',
    status: 'pending',
    requestDate: '۱۴۰۵/۰۶/۲۹',
  },
];

const INITIAL_SLUG_CHANGE_REQUESTS: StoreSlugChangeRequest[] = [
  {
    id: 'REQ-S-201',
    storeId: 'store-bibi',
    currentSlug: 'bibi',
    requestedSlug: 'bibi-style-official',
    sellerMobile: '09121112233',
    status: 'pending',
    requestDate: '۱۴۰۵/۰۶/۲۸',
  },
];

const INITIAL_DOMAIN_REQUESTS: CustomDomainRequest[] = [
  {
    id: 'REQ-D-301',
    storeId: 'store-bibi',
    storeName: 'فروشگاه بی‌بی استایل',
    requestedDomain: 'bibistyle.ir',
    status: 'pending',
    requestDate: '۱۴۰۵/۰۶/۳۰',
  },
];

const INITIAL_PREMADE_WIDGETS: PreMadeWidget[] = [
  {
    id: 'WGT-01',
    title: 'اسلایدر تخفیف‌های داغ روز (Hot Deals)',
    previewImageUrl: 'https://web-cdn.snapp.ir/fintech-marketing/ecommerce/products/bag-1.png',
    type: 'seller_deals',
    description: 'نمایش ۶ کالای پرفروش با برچسب درصد تخفیف قرمز و شمارش معکوس',
    targetNicheIds: ['niche-leather', 'niche-gold', 'niche-beauty'],
    recommendedProductIds: ['prod-1', 'prod-2', 'prod-3'],
    bannerSize: 'landscape',
    bannerWidth: 1200,
    bannerHeight: 628,
    createdAt: '۱۴۰۵/۰۱/۱۰',
  },
];

export interface StoreState {
  currentStore: Store | null;
  setCurrentStore: (store: Store | null) => void;
  stores: Store[];
  addStore: (store: Store) => void;
  storeStatuses: { [storeId: string]: 'active' | 'suspended' };
  toggleStoreStatus: (storeId: string) => 'active' | 'suspended';
  updateStoreActiveProducts: (productIds: string[]) => Promise<{ ok: boolean; status?: number }>;
  toggleCategoryActiveProducts: (categorySlug: string, enable: boolean) => void;
  updateStoreProductPriceOverride: (productId: string, profitD: number) => void;
  updateStoreProductPriceOverrideWithMode: (productId: string, profitD: number) => Promise<{ ok: boolean; status?: number }>;
  updateStoreCategoryPriceOverride: (categorySlug: string, marginPercent: number) => void;
  updateStoreNichePriceOverride: (nicheId: string, margin: number) => Promise<{ ok: boolean; status?: number }>;
  updateStoreBuilderConfig: (config: StoreBuilderConfig, publishImmediately?: boolean) => void;
  revertStoreBuilderConfig: () => void;
  updateStoreCustomDomain: (domain: string) => void;
  updateStorePlan: (planId: 'trial' | 'standard' | 'pro' | 'vip') => void;
  storeNameChangeRequests: StoreNameChangeRequest[];
  requestStoreNameChange: (storeId: string, requestedName: string) => void;
  approveStoreNameChange: (requestId: string) => void;
  rejectStoreNameChange: (requestId: string) => void;
  storeSlugChangeRequests: StoreSlugChangeRequest[];
  requestStoreSlugChange: (storeId: string, requestedSlug: string) => void;
  approveStoreSlugChange: (requestId: string) => void;
  rejectStoreSlugChange: (requestId: string) => void;
  customDomainRequests: CustomDomainRequest[];
  requestCustomDomain: (storeId: string, domain: string) => void;
  approveCustomDomain: (requestId: string) => void;
  rejectCustomDomain: (requestId: string) => void;
  updateStoreSelectedNiches: (nicheIds: string[]) => void;
  preMadeWidgets: PreMadeWidget[];
  createPreMadeWidget: (widget: Omit<PreMadeWidget, 'id' | 'createdAt'>) => void;
  updatePreMadeWidget: (id: string, updated: Partial<PreMadeWidget>) => void;
  updateSellerDealsWidgetConfig: (config: Store['sellerDealsWidgetConfig']) => void;
  updateCentralDealsFilter: (filter: 'my_niches' | 'manual' | 'all', productIds?: string[]) => void;
  referredStoreSlug: string | null;
  setReferredStoreSlug: (slug: string | null) => void;
  verifyAndConnectDomainAutomatically: (storeId: string, domain: string) => Promise<{ success: boolean; message: string }>;
  purchaseServiceWithWallet: (storeId: string, serviceType: 'plan_renewal' | 'sms_500' | 'sms_2000' | 'catalog_upgrade', cost: number, details?: string) => boolean;
  updateStoreSocialLinks: (socialLinks: StoreSocialLinks) => void;
  updateStoreAdminMarginOverride: (storeId: string, margin: number, isLocked: boolean) => void;
  isSellerModalOpen: boolean;
  setIsSellerModalOpen: (open: boolean) => void;
  // Header bell dot (live parity): unread count fed by
  // NotificationDrawer from its own notification list; Header
  // renders the rose dot iff this is > 0. Read via
  // useStoreManager (StoreState), not the composite useStore().
  unreadNotificationCount: number;
  setUnreadNotificationCount: (count: number) => void;
  activeView: ActiveView;
  setActiveView: (view: ActiveView, navigation?: { productId?: string; staticPageKey?: StaticPageKey; storeSlug?: string; sellerTab?: string; searchQuery?: string; categorySlug?: string }) => void;
  staticPageKey: StaticPageKey;
  openStaticPage: (key: StaticPageKey) => void;
  // Team 08 (D45/D46) router/storefront states ONLY.
  storefrontSlug: string | null;
  storefrontLoading: boolean;
  storefrontResolved: boolean;
  storefrontInvalid: boolean;
  storefrontSuspended: boolean;
  storefrontUnsubscribeSlug: string | null;
  // Worker A (buy-now): live direct-buy state.
  directBuy: DirectBuy | null;
  startDirectBuy: (directBuy: DirectBuy) => void;
  clearDirectBuy: () => void;
  directBuyAddress: DirectBuyAddress | null;
  setDirectBuyAddress: (address: DirectBuyAddress | null) => void;
  sellerDashboardTab: string;
  setSellerDashboardTab: (tab: string) => void;
  routeProductPending: boolean;
  routeProductInvalid: boolean;
  // Store context of a product opened from inside a seller storefront
  // (null = the product route is unscoped, i.e. the central PDP).
  routeStoreSlug: string | null;
  // P0-2: true once the routed store is installed as currentStore (or
  // its resolve failed terminally); the PDP waits on it instead of
  // rendering under the wrong (central) store.
  routeStoreResolved: boolean;
  routeStoreInvalid: boolean;
  openStorefront: (slug?: string) => void;
}

const completePublicStore = (store: Store, slug: string): Store => ({
  ...store,
  id: store.id || `store-${slug}`,
  slug,
  name: store.name || slug,
  ownerType: store.ownerType || 'dropshipper',
  ownerName: store.ownerName || '',
  tagline: store.tagline || '',
  profitMarginPercent: store.profitMarginPercent || 0,
  description: store.description || '',
  activeCategories: store.activeCategories || [],
  totalSales: store.totalSales || 0,
  monthlyRevenueEstimate: store.monthlyRevenueEstimate || 0,
});

const StoreContext = createContext<StoreState | undefined>(undefined);

export const StoreProvider: React.FC<{ children: ReactNode; allowUnselectedChildren?: boolean }> = ({ children, allowUnselectedChildren = false }) => {
  const { products, openProductDetail, currentProductDetail, setSearchQuery, setSelectedCategory, clearCatalogBrand, resetCatalogFilter, setWishlistScope } = useProducts();
  const { sellerSession } = useAuth();
  const onboardingDraft = useSellerOnboardingDraft();
  const [stores, setStores] = useState<Store[]>([]);
  const [selectedStore, setCurrentStore] = useState<Store | null>(null);
  const [centralStore, setCentralStore] = useState<Store | null>(null);
  const [ownerStoreLoadedFor, setOwnerStoreLoadedFor] = useState<string | null>(null);
  const [reloadStores, setReloadStores] = useState(0);
  const [resolvedShop, setResolvedShop] = useState<Store | null>(null);
  const cart = useCart();
  const [storesLoading, setStoresLoading] = useState(true);
  const [storeStatuses, setStoreStatuses] = useState<{ [storeId: string]: 'active' | 'suspended' }>({
    'central-store': 'active',
    'store-bibi': 'active',
    'store-techhub': 'active',
  });

  // Team 08 (D45/D46): initial view comes from the live pathname router,
  // not from state alone. /store/<slug>, /product/<id|spn-n>, static
  // pages, ?panel=seller&tab=<tab> and unknown URLs (not_found) are all
  // resolved here; the source only recognised /admin.
  const initialRoute = () => {
    if (typeof window === 'undefined') return { view: 'customer' as ActiveView, slug: null as string | null, unsubscribe: false, staticKey: 'about' as StaticPageKey, sellerTab: 'overview', productId: undefined as string | undefined, spnNumber: undefined as number | undefined, spnc: undefined as string | undefined, storeSlug: null as string | null };
    const parsed = parsePathname(window.location.pathname, window.location.search);
    if (onboardingDraft?.isOpen && parsed.view === 'customer' && window.history.state?.activeView === 'dropshipper_landing') {
      return { view: 'dropshipper_landing' as ActiveView, slug: null, unsubscribe: false, staticKey: 'about' as StaticPageKey, sellerTab: 'overview', productId: undefined, spnNumber: undefined, spnc: undefined, storeSlug: null };
    }
    if (parsed.view === 'seller_storefront') return { view: parsed.view as ActiveView, slug: parsed.slug, unsubscribe: parsed.unsubscribe, staticKey: 'about' as StaticPageKey, sellerTab: 'overview', productId: undefined, spnNumber: undefined, spnc: undefined, storeSlug: null };
    if (parsed.view === 'product_detail') return { view: parsed.view as ActiveView, slug: null, unsubscribe: false, staticKey: 'about' as StaticPageKey, sellerTab: 'overview', productId: parsed.id, spnNumber: parsed.spnNumber, spnc: parsed.spnc, storeSlug: parsed.storeSlug ?? null };
    if (parsed.view === 'customer_profile' || parsed.view === 'customer_orders' || parsed.view === 'customer_wishlist') {
      return { view: parsed.view, slug: null, unsubscribe: false, staticKey: 'about' as StaticPageKey, sellerTab: 'overview', productId: undefined, spnNumber: undefined, spnc: undefined, storeSlug: parsed.storeSlug ?? null };
    }
    if (parsed.view === 'static_page') return { view: parsed.view as ActiveView, slug: null, unsubscribe: false, staticKey: parsed.staticPageKey, sellerTab: 'overview', productId: undefined, spnNumber: undefined, spnc: undefined, storeSlug: parsed.storeSlug ?? null };
    if (parsed.view === 'seller_dashboard') return { view: parsed.view as ActiveView, slug: null, unsubscribe: false, staticKey: 'about' as StaticPageKey, sellerTab: parsed.sellerTab, productId: undefined, spnNumber: undefined, spnc: undefined, storeSlug: null };
    if (parsed.view === 'dashboard' || parsed.view === 'catalog') return { view: parsed.view as ActiveView, slug: null, unsubscribe: false, staticKey: 'about' as StaticPageKey, sellerTab: 'overview', productId: undefined, spnNumber: undefined, spnc: undefined, storeSlug: null };
    return { view: parsed.view as ActiveView, slug: null, unsubscribe: false, staticKey: 'about' as StaticPageKey, sellerTab: 'overview', productId: undefined, spnNumber: undefined, spnc: undefined, storeSlug: null };
  };
  const [initialRouteState] = useState(initialRoute);

  const [activeView, setActiveViewState] = useState<ActiveView>(initialRouteState.view);
  const [storefrontSlug, setStorefrontSlug] = useState<string | null>(initialRouteState.slug);
  const [storefrontLoading, setStorefrontLoading] = useState<boolean>(initialRouteState.view === 'seller_storefront' && !!initialRouteState.slug);
  const [storefrontResolved, setStorefrontResolved] = useState<boolean>(false);
  const [storefrontInvalid, setStorefrontInvalid] = useState<boolean>(false);
  const [storefrontSuspended, setStorefrontSuspended] = useState<boolean>(false);
  const [storefrontUnsubscribeSlug, setStorefrontUnsubscribeSlug] = useState<string | null>(
    initialRouteState.view === 'seller_storefront' && initialRouteState.unsubscribe ? initialRouteState.slug : null,
  );
  const [sellerDashboardTab, setSellerDashboardTab] = useState<string>(initialRouteState.sellerTab);
  const [routeProduct, setRouteProduct] = useState<{ id?: string; spnNumber?: number; spnc?: string } | null>(
    initialRouteState.view === 'product_detail' ? { id: initialRouteState.productId, spnNumber: initialRouteState.spnNumber, spnc: initialRouteState.spnc } : null,
  );
  const [routeProductPending, setRouteProductPending] = useState<boolean>(initialRouteState.view === 'product_detail');
  const [routeProductInvalid, setRouteProductInvalid] = useState<boolean>(false);
  const [routeStoreSlug, setRouteStoreSlug] = useState<string | null>(initialRouteState.storeSlug);
  const resolvedStorefrontSlugRef = useRef<string | null>(null);
  const [routeStoreInvalid, setRouteStoreInvalid] = useState(false);
  const publicShopSlug = routeStoreSlug ?? (activeView === 'seller_storefront' ? storefrontSlug : null);
  const scopedShop = publicShopSlug
    ? (resolvedShop?.slug === publicShopSlug ? resolvedShop : stores.find(store => store.slug === publicShopSlug))
    : undefined;
  // Public buyer routes use their URL's store. Keep the seller's private
  // selection separate so loading a public store never switches the dashboard.
  const managementView = activeView === 'seller_dashboard' || allowUnselectedChildren;
  const requiredOwnerStore = !allowUnselectedChildren && managementView && sellerSession?.status === 'active' ? sellerSession.storeId : null;
  const waitingForOwnerStore = Boolean(requiredOwnerStore && ownerStoreLoadedFor !== requiredOwnerStore);
  const currentStore = requiredOwnerStore
    ? waitingForOwnerStore ? null : selectedStore
    : (managementView ? selectedStore : scopedShop) ?? centralStore;
  useLayoutEffect(() => {
    if (!currentStore) return;
    cart.setActiveCartStore(currentStore.id);
    setWishlistScope(currentStore.id, currentStore.slug);
  }, [currentStore?.id, currentStore?.slug]);
  const discoveryScope = publicShopSlug ?? CENTRAL_STORE_SLUG;
  const previousDiscoveryScope = useRef(discoveryScope);
  useEffect(() => {
    if (previousDiscoveryScope.current === discoveryScope) return;
    previousDiscoveryScope.current = discoveryScope;
    const params = new URLSearchParams(window.location.search);
    setSearchQuery(params.get('q') || '');
    setSelectedCategory(params.get('category') || 'all');
    clearCatalogBrand();
    resetCatalogFilter();
  }, [discoveryScope]);
  // P0-2: the store scope the currently installed routed product was
  // resolved with (null = unscoped central). The already-resolved
  // guard must compare scope too, or bibi -> sepehr on the same
  // product id returns early as "already resolved" with the wrong price.
  const routeProductScopeRef = useRef<string | null | undefined>(undefined);
  // P0-2: resolved = no route scope, or the routed store is the
  // current store, or its resolve failed terminally (invalid view).
  const routeStoreResolved = !routeStoreSlug || Boolean(scopedShop) || routeStoreInvalid;

  type NavigationState = {
    activeView: ActiveView;
    productId?: string;
    staticPageKey?: StaticPageKey;
    storeSlug?: string;
    sellerTab?: string;
    searchQuery?: string;
    categorySlug?: string;
  };

  const setActiveView = (view: ActiveView, navigation: Partial<NavigationState> = {}) => {
    const accountPage = view === 'customer_profile' || view === 'customer_orders' || view === 'customer_wishlist';
    const scopedPage = accountPage || view === 'static_page';
    const pageStore = scopedPage ? navigation.storeSlug ?? publicShopSlug ?? undefined : undefined;
    const ownShopSlug = activeView === 'seller_dashboard' ? selectedStore?.slug : undefined;
    const nextShopSlug = navigation.storeSlug ?? ownShopSlug ?? publicShopSlug;
    const storefrontTarget = isStoreScopeSlug(nextShopSlug) && nextShopSlug !== centralStore?.slug ? nextShopSlug : null;
    const targetView = view === 'seller_storefront' && !storefrontTarget ? 'customer' : view;
    setActiveViewState(targetView);
    if (targetView === 'customer' && activeView !== 'customer') {
      setSearchQuery('');
      setSelectedCategory('all');
      clearCatalogBrand();
      resetCatalogFilter();
    }
    if (view === 'seller_storefront') {
      const slug = storefrontTarget;
      setStorefrontSlug(slug);
      setStorefrontLoading(Boolean(slug));
      setStorefrontUnsubscribeSlug(null);
      setStorefrontInvalid(false);
      setStorefrontResolved(false);
      setStorefrontSuspended(false);
      resolvedStorefrontSlugRef.current = null;
    }
    if (view === 'static_page' && navigation.staticPageKey) {
      setStaticPageKey(navigation.staticPageKey);
    }
    if (view === 'seller_dashboard' && navigation.sellerTab && isSellerDashboardTab(navigation.sellerTab)) {
      setSellerDashboardTab(navigation.sellerTab);
    }
    if (view === 'product_detail') {
      setRouteProductInvalid(false);
      if (navigation.productId) setRouteProduct({ id: navigation.productId });
      // The store scope of a product open is explicit: a navigation
      // naming a store scopes the PDP to it; a fresh product open
      // naming none is unscoped (central) — it must never inherit a
      // stale scope from an earlier in-store product. Navigations with
      // neither (e.g. the ad-token effect re-asserting the view) leave
      // the scope the opener set untouched.
      if (navigation.storeSlug !== undefined) setRouteStoreSlug(navigation.storeSlug);
      else if (navigation.productId) setRouteStoreSlug(null);
    } else if (scopedPage) {
      setRouteStoreSlug(pageStore ?? null);
    } else {
      setRouteStoreSlug(null);
    }
    if (typeof window !== 'undefined') {
      // FIX 11: views without an explicit branch used to push a bare '/'
      // here, which in the /preview/ build escaped the preview prefix and
      // landed on the live root URL. Route every view through pathForView
      // (which the preview build prefixes) instead.
      let path = pathForView(view);
      if (view === 'seller_storefront') {
        path = pathForView(view, { storeSlug: storefrontTarget ?? undefined });
      } else if (view === 'product_detail') {
        const product = navigation.productId
          ? products.find((item) => item.id === navigation.productId)
          : undefined;
        // The written URL carries the store scope with the product:
        // the slug this navigation resolves to - the explicit
        // navigation value, else cleared for a fresh unscoped open,
        // else the scope already in force - prefixes the product
        // path, so a copied link lands back inside the same store.
        const scopeSlug =
          navigation.storeSlug !== undefined
            ? navigation.storeSlug
            : navigation.productId
              ? null
              : routeStoreSlug;
        path = navigation.productId
          ? buildProductUrl(product ?? { id: navigation.productId }, scopeSlug ?? undefined)
          : '/';
      } else if (accountPage) {
        path = pathForView(view, { storeSlug: pageStore });
      } else if (view === 'static_page') {
        path = pathForView(view, { staticPageKey: navigation.staticPageKey || staticPageKey, storeSlug: pageStore });
      } else if (view === 'seller_dashboard') {
        path = pathForView(view, { sellerTab: navigation.sellerTab || sellerDashboardTab });
      } else if (view === 'dashboard') {
        path = '/dashboard';
      } else if (view === 'supplier_dashboard' || view === 'warehouse_panel' || view === 'admin_panel') {
        path = pathForView(view);
      } else if (view === 'catalog') {
        path = pathForView('catalog', { searchQuery: navigation.searchQuery, categorySlug: navigation.categorySlug });
      } else if (view === 'not_found') {
        path = window.location.pathname + window.location.search;
      }
      const nextState: NavigationState = { ...navigation, activeView: targetView };
      window.history.pushState(nextState, '', path);
    }
  };

  const openStorefront = (slug?: string) => {
    if ((slug || currentStore?.slug) === CENTRAL_STORE_SLUG) {
      setActiveView('customer');
      return;
    }
    setActiveView('seller_storefront', slug ? { storeSlug: slug } : {});
  };

  const navigationInitialized = useRef(false);

  useEffect(() => {
    if (typeof window === 'undefined') return;

    if (!navigationInitialized.current) {
      const initialState = window.history.state as NavigationState | null;
      window.history.replaceState(
        { ...initialState, activeView: activeView },
        '',
        window.location.href
      );
      navigationInitialized.current = true;
    }

    const handlePopState = (event: PopStateEvent) => {
      // Live router: back/forward re-parses the pathname (plus tab and
      // static-page params); stored history state is only a fallback.
      const parsed = parsePathname(window.location.pathname, window.location.search);
      setRouteStoreSlug('storeSlug' in parsed ? parsed.storeSlug ?? null : null);
      if (parsed.view === 'seller_storefront') {
        setStorefrontSlug(parsed.slug);
        setStorefrontLoading(!parsed.unsubscribe);
        setStorefrontUnsubscribeSlug(parsed.unsubscribe ? parsed.slug : null);
        setStorefrontInvalid(false);
        setStorefrontResolved(false);
        setStorefrontSuspended(false);
        resolvedStorefrontSlugRef.current = null;
        setActiveViewState('seller_storefront');
        return;
      }
      if (parsed.view === 'product_detail') {
        setRouteProduct({ id: parsed.id, spnNumber: parsed.spnNumber, spnc: parsed.spnc });
        setRouteProductPending(true);
        setRouteProductInvalid(false);
        setActiveViewState('product_detail');
        return;
      }
      if (parsed.view === 'static_page') {
        setStaticPageKey(parsed.staticPageKey);
        setActiveViewState('static_page');
        return;
      }
      if (parsed.view === 'seller_dashboard') {
        setSellerDashboardTab(parsed.sellerTab);
        setActiveViewState('seller_dashboard');
        return;
      }
      if (parsed.view === 'dashboard' || parsed.view === 'supplier_dashboard' || parsed.view === 'warehouse_panel' || parsed.view === 'admin_panel' || parsed.view === 'catalog' ||
        parsed.view === 'customer_profile' || parsed.view === 'customer_orders' || parsed.view === 'customer_wishlist' || parsed.view === 'categories_tree') {
        setActiveViewState(parsed.view);
        return;
      }
      const navigation = event.state as NavigationState | null;
      setActiveViewState(parsed.view === 'not_found' ? 'not_found' : (navigation?.activeView || parsed.view));
      if (navigation?.productId) {
        const product = products.find((item) => item.id === navigation.productId);
        if (product) openProductDetail(product);
      }
    };

    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, [products, openProductDetail]);

const [staticPageKey, setStaticPageKey] = useState<StaticPageKey>(initialRouteState.staticKey);
  const [localSellerModalOpen, setLocalSellerModalOpen] = useState(false);
  const isSellerModalOpen = onboardingDraft?.isOpen ?? localSellerModalOpen;
  const setIsSellerModalOpen = onboardingDraft?.setIsOpen ?? setLocalSellerModalOpen;
  // Worker A (buy-now): live direct-buy state; cleared as a pair.
  const [directBuy, setDirectBuy] = useState<DirectBuy | null>(null);
  const [directBuyAddress, setDirectBuyAddress] = useState<DirectBuyAddress | null>(null);
  const startDirectBuy = (next: DirectBuy) => {
    const purchase = resolvePurchase(next.product, next.variantId);
    if (!purchase || !cartQuantityAllowed([], purchase, next.qty)) return;
    setDirectBuy({ ...next, product: purchase.product, variantId: purchase.variant?.id, variantLabel: purchase.variant?.name ?? next.variantLabel });
  };
  const clearDirectBuy = () => {
    setDirectBuy(null);
    setDirectBuyAddress(null);
  };
  // Header bell dot state (see interface note above). Owned here
  // so Header can read it without the drawer being open; the
  // drawer syncs it from its notification list on every change.
  const [unreadNotificationCount, setUnreadNotificationCount] = useState(0);
  const [storeNameChangeRequests, setStoreNameChangeRequests] = useState<StoreNameChangeRequest[]>(
    () => isMockMode() ? INITIAL_NAME_CHANGE_REQUESTS : []
  );
  const [storeSlugChangeRequests, setStoreSlugChangeRequests] = useState<StoreSlugChangeRequest[]>(
    () => isMockMode() ? INITIAL_SLUG_CHANGE_REQUESTS : []
  );
  const [customDomainRequests, setCustomDomainRequests] = useState<CustomDomainRequest[]>(
    () => isMockMode() ? INITIAL_DOMAIN_REQUESTS : []
  );
  const [preMadeWidgets, setPreMadeWidgets] = useState<PreMadeWidget[]>(
    () => isMockMode() ? INITIAL_PREMADE_WIDGETS : []
  );

  const [referredStoreSlug, setReferredStoreSlugState] = useState<string | null>(() => {
    if (typeof window !== 'undefined') {
      const urlParams = new URLSearchParams(window.location.search);
      const ref = urlParams.get('ref');
      if (ref) {
        localStorage.setItem('sellershow_referral_store', ref);
        return ref;
      }
      return localStorage.getItem('sellershow_referral_store');
    }
    return null;
  });

  useEffect(() => {
    let cancelled = false;
    const loadStores = async () => {
      try {
        const publicStores = isMockMode() ? Promise.resolve(mockStores) : storeApi.getAllStores();
        const ownedStores = !isMockMode() && sellerSession?.status === 'active' ? storeApi.getOwnedStores() : publicStores;
        const [publicResult, ownedResult] = await Promise.allSettled([publicStores, ownedStores]);
        if (cancelled) return;

        const publicList = publicResult.status === 'fulfilled' && Array.isArray(publicResult.value) ? publicResult.value : [];
        const available = ownedResult.status === 'fulfilled' && Array.isArray(ownedResult.value) ? ownedResult.value : [];
        const platformStore = publicList.find(store => store.ownerType === 'central' || store.slug === CENTRAL_STORE_SLUG)
          ?? await storeApi.getStoreBySlug(CENTRAL_STORE_SLUG).catch(() => null);
        if (cancelled) return;
        if (platformStore) setCentralStore(platformStore);
        setStores(available);
        setOwnerStoreLoadedFor(sellerSession?.storeId ?? null);
        setCurrentStore(previous => {
          if (!sellerSession?.storeId) return allowUnselectedChildren ? previous : null;
          return available.find(store => store.id === previous?.id) ??
            available.find(store => store.id === sellerSession.storeId) ?? null;
        });
      } finally {
        if (!cancelled) setStoresLoading(false);
      }
    };
    void loadStores();
    return () => { cancelled = true; };
  }, [sellerSession?.storeId, sellerSession?.status, allowUnselectedChildren, reloadStores]);

  const setReferredStoreSlug = (slug: string | null) => {
    setReferredStoreSlugState(slug);
    if (typeof window !== 'undefined') {
      if (slug) localStorage.setItem('sellershow_referral_store', slug);
      else localStorage.removeItem('sellershow_referral_store');
    }
  };

  // Team 08 (D45): storefront resolve. Live states:
  // storefrontSlug / storefrontLoading / storefrontResolved /
  // storefrontInvalid, plus the suspended page. The public store API
  // returns only 12 keys (D52, no templateId/ownerPhone); the resolved
  // store is merged over safe defaults so the Store type stays complete.
  // TODO(U42): ownerPhone origin is unverified; it is used only when
  // the API provides it (see StoreSuspendedView).
  useEffect(() => {
    if (activeView !== 'seller_storefront') return;
    if (!storefrontSlug) {
      // Opened from inside the dashboard (no slug in the URL yet):
      // the current store is the storefront, already resolved.
      setStorefrontResolved(true);
      setStorefrontLoading(false);
      setStorefrontSuspended(storeStatuses[currentStore?.id || ''] === 'suspended');
      return;
    }
    if (storefrontUnsubscribeSlug) {
      setStorefrontLoading(false);
      setStorefrontResolved(true);
      return;
    }
    if (resolvedStorefrontSlugRef.current === storefrontSlug) return;
    let cancelled = false;
    setStorefrontLoading(true);
    setStorefrontInvalid(false);
    setStorefrontResolved(false);
    storeApi.getStoreBySlug(storefrontSlug)
      .then((resolved: any) => {
        if (cancelled) return;
        resolvedStorefrontSlugRef.current = storefrontSlug;
        setResolvedShop(completePublicStore(resolved, storefrontSlug));
        setStorefrontSuspended(resolved.status === 'suspended');
        setStorefrontResolved(true);
        setStorefrontLoading(false);
      })
      .catch((error: any) => {
        if (cancelled) return;
        // FIX 14: the single-store endpoint now 404s suspended stores
        // like live ("Store is suspended"). Show the suspended view
        // with a minimal store instead of the generic invalid page.
        if (error?.status === 404 && typeof error?.message === 'string' && error.message.toLowerCase().includes('suspended')) {
          resolvedStorefrontSlugRef.current = storefrontSlug;
          setResolvedShop({
            id: `store-${storefrontSlug}`,
            name: storefrontSlug,
            slug: storefrontSlug,
            ownerType: 'dropshipper',
            ownerName: '',
            tagline: '',
            profitMarginPercent: 0,
            description: '',
            activeCategories: [],
            totalSales: 0,
            monthlyRevenueEstimate: 0,
            status: 'suspended',
          } as Store);
          setStorefrontSuspended(true);
          setStorefrontResolved(true);
          setStorefrontLoading(false);
          return;
        }
        setStorefrontInvalid(true);
        setStorefrontLoading(false);
      });
    return () => { cancelled = true; };
  }, [activeView, storefrontSlug, storefrontUnsubscribeSlug]);

  useEffect(() => {
    setRouteStoreInvalid(false);
    if (!routeStoreSlug || scopedShop) return;
    let cancelled = false;
    storeApi.getStoreBySlug(routeStoreSlug)
      .then(resolved => {
        if (cancelled) return;
        if (!resolved) {
          setRouteStoreInvalid(true);
          return;
        }
        setResolvedShop(completePublicStore(resolved, routeStoreSlug));
      })
      .catch(() => {
        if (!cancelled) setRouteStoreInvalid(true);
      });
    return () => { cancelled = true; };
  }, [routeStoreSlug, scopedShop]);

  // Team 08 (D46): routed product resolve. Uses Team 04's server spn
  // lookup (catalogApi.getProductBySpn) for /product/spn-<n>/ URLs and
  // the normal detail endpoint for /product/<id>. No second lookup.
  useEffect(() => {
    if (activeView !== 'product_detail' || !routeProduct) return;
    // FIX 10: guard against re-resolving a route that is already showing.
    // Without this, every provider re-render re-ran this effect for spn
    // routes (the list DTO carries no spnNumber, so `found` never matches)
    // and the spn fetch looped forever, leaving the page stuck on
    // "loading product" in the preview browser check.
    // Slug routes: routeProduct.id carries the SLUG (parseProductRemainder
    // returns the path segment), while currentProductDetail.id is the real
    // product id — the id-only comparison above never matched, so every
    // slug PDP refetched + reopened in a loop (~10 req/s), unmounting the
    // PDP (loading flash) and resetting any variant selection. Match the
    // slug too, mirroring the FIX 10 spn guard.
    // P0-2: the same product id under a different store scope is NOT
    // already resolved — the installed product carries the old store's
    // price. The scope the installed product was resolved with must
    // match the route's scope.
    const alreadyResolved =
      routeProductScopeRef.current === (routeStoreSlug ?? null) &&
      ((routeProduct.spnNumber !== undefined && currentProductDetail?.spnNumber === routeProduct.spnNumber) ||
      (routeProduct.id !== undefined && currentProductDetail?.id === routeProduct.id) ||
      (routeProduct.id !== undefined && currentProductDetail?.slug === routeProduct.id));
    if (alreadyResolved) {
      setRouteProductPending(false);
      return;
    }
    // P0-2: the in-memory catalogue is unscoped (central prices) — it
    // must never install the PDP product for a store-scoped route.
    // Scoped routes always take the detail fetch below, which sends
    // ?storeSlug; only unscoped (central) routes use the list shortcut.
    const found = routeStoreSlug
      ? undefined
      : (routeProduct.spnNumber !== undefined
        ? products.find((item) => item.spnNumber === routeProduct.spnNumber)
        : routeProduct.id
          ? products.find((item) => item.id === routeProduct.id || item.slug === routeProduct.id)
          : undefined);
    if (found) {
      routeProductScopeRef.current = null;
      openProductDetail(found);
      setRouteProductPending(false);
      return;
    }
    let cancelled = false;
    setRouteProductPending(true);
    const fetch = routeProduct.spnNumber !== undefined
      ? catalogApi.getProductBySpn(routeProduct.spnNumber, routeStoreSlug ?? undefined)
      : routeProduct.id
        ? catalogApi.getProductById(routeProduct.id, routeStoreSlug ?? undefined)
        : Promise.reject(new Error('no product route'));
    fetch
      .then((dto) => {
        if (cancelled) return;
        routeProductScopeRef.current = routeStoreSlug ?? null;
        openProductDetail(mapBackendProduct(dto));
        setRouteProductPending(false);
      })
      .catch(() => {
        if (cancelled) return;
        routeProductScopeRef.current = undefined;
        setRouteProductPending(false);
        setRouteProductInvalid(true);
      });
    return () => { cancelled = true; };
  }, [activeView, routeProduct, routeStoreSlug, products, currentProductDetail]);

  const openStaticPage = (key: StaticPageKey) => {
    setStaticPageKey(key);
    setActiveView('static_page', { staticPageKey: key });
    if (typeof window !== 'undefined') {
      window.scrollTo({ top: 0, behavior: 'smooth' });
    }
  };

  const addStore = (store: Store) => {
    setStores((prev) => [...prev, store]);
    setCurrentStore(store);
  };

  const toggleStoreStatus = (storeId: string): 'active' | 'suspended' => {
    const current = storeStatuses[storeId] || 'active';
    const next = current === 'active' ? 'suspended' : 'active';
    setStoreStatuses((prev) => ({ ...prev, [storeId]: next }));
    return next;
  };

  const updateCurrentStoreObject = (updated: Store) => {
    setCurrentStore(updated);
    setStores((prev) => prev.map((s) => (s.id === updated.id ? updated : s)));
  };

  const updateStoreActiveProducts = (productIds: string[]): Promise<{ ok: boolean; status?: number }> => {
    if (!currentStore) return Promise.resolve({ ok: false });
    updateCurrentStoreObject({ ...currentStore, activeProductIds: productIds });
    if (isMockMode()) return Promise.resolve({ ok: true });
    if (!currentStore.slug) return Promise.resolve({ ok: false });

    return storeApi.saveStoreProducts(currentStore.slug, productIds)
      .then(() => ({ ok: true }))
      .catch((err) => {
        console.warn('Failed to persist active products:', err);
        return { ok: false, status: typeof err?.status === 'number' ? err.status : undefined };
      });
  };

  const toggleCategoryActiveProducts = (categorySlug: string, enable: boolean) => {
    if (!currentStore) return;
    const categoryProductIds = products.filter((p) => p.category === categorySlug).map((p) => p.id);
    const currentActive = currentStore.activeProductIds || products.map((p) => p.id);
    let nextActive: string[];
    if (enable) {
      nextActive = Array.from(new Set([...currentActive, ...categoryProductIds]));
    } else {
      nextActive = currentActive.filter((id) => !categoryProductIds.includes(id));
    }
    updateStoreActiveProducts(nextActive);
  };

  const updateStoreProductPriceOverride = (productId: string, profitD: number) => {
    if (!currentStore) return;
    const overrides = { ...(currentStore.productPriceOverrides || {}) };
    if (profitD <= 0) delete overrides[productId];
    else overrides[productId] = profitD;
    updateCurrentStoreObject({ ...currentStore, productPriceOverrides: overrides });
  };


  const updateStoreProductPriceOverrideWithMode = (productId: string, profitD: number): Promise<{ ok: boolean; status?: number }> => {
    if (!currentStore) return Promise.resolve({ ok: false });
    const overrides = { ...(currentStore.productPriceOverrides || {}) };
    const modes = { ...(currentStore.productPriceOverrideModes || {}) };
    if (profitD <= 0) {
      delete overrides[productId];
      delete modes[productId];
    } else {
      overrides[productId] = profitD;
      modes[productId] = 'percent';
    }
    const prevProductOverrides = currentStore.productPriceOverrides;
    const prevProductOverrideModes = currentStore.productPriceOverrideModes;
    updateCurrentStoreObject({
      ...currentStore,
      productPriceOverrides: overrides,
      productPriceOverrideModes: modes,
    });
    if (!isMockMode() && currentStore.slug) {
      return storeApi.updateStorePricing(currentStore.slug, {
        type: 'product',
        targetId: productId,
        value: profitD,
        mode: 'percent',
      }).then(() => ({ ok: true })).catch((err: any) => { console.warn('Failed to persist product price override:', err); updateCurrentStoreObject({ ...currentStore, productPriceOverrides: prevProductOverrides, productPriceOverrideModes: prevProductOverrideModes }); return { ok: false, status: typeof err?.status === 'number' ? err.status : undefined }; });
    }
    return Promise.resolve({ ok: true });
  };

  const updateStoreCategoryPriceOverride = (categorySlug: string, marginPercent: number) => {
    if (!currentStore) return;
    const overrides = { ...(currentStore.categoryPriceOverrides || {}) };
    if (marginPercent <= 0) delete overrides[categorySlug];
    else overrides[categorySlug] = marginPercent;
    updateCurrentStoreObject({ ...currentStore, categoryPriceOverrides: overrides });
  };


  const updateStoreNichePriceOverride = (nicheId: string, margin: number): Promise<{ ok: boolean; status?: number }> => {
    if (!currentStore) return Promise.resolve({ ok: false });
    const nicheOverrides = { ...(currentStore.nichePriceOverrides || {}) };
    const nicheModes = { ...(currentStore.nichePriceOverrideModes || {}) };
    if (margin <= 0) {
      delete nicheOverrides[nicheId];
      delete nicheModes[nicheId];
    } else {
      nicheOverrides[nicheId] = margin;
      nicheModes[nicheId] = 'percent';
    }
    const prevNicheOverrides = currentStore.nichePriceOverrides;
    const prevNicheOverrideModes = currentStore.nichePriceOverrideModes;
    updateCurrentStoreObject({
      ...currentStore,
      nichePriceOverrides: nicheOverrides,
      nichePriceOverrideModes: nicheModes,
    });
    if (!isMockMode() && currentStore.slug) {
      return storeApi.updateStorePricing(currentStore.slug, {
        type: 'niche',
        targetId: nicheId,
        value: margin,
        mode: 'percent',
      }).then(() => ({ ok: true })).catch((err: any) => { console.warn('Failed to persist niche price override:', err); updateCurrentStoreObject({ ...currentStore, nichePriceOverrides: prevNicheOverrides, nichePriceOverrideModes: prevNicheOverrideModes }); return { ok: false, status: typeof err?.status === 'number' ? err.status : undefined }; });
    }
    return Promise.resolve({ ok: true });
  };

  const updateStoreBuilderConfig = (config: StoreBuilderConfig, publishImmediately = false) => {
    if (!currentStore) return;
    const base = currentStore.builderConfig || {
      selectedTheme: 'general',
      accentColor: '#007dfa',
      widgets: {
        heroBanner: true,
        dealsSlider: true,
        categoriesGrid: true,
        storeDealsWidget: true,
        trustBar: true,
        contactInfo: true,
      },
    };
    const nextConfig: StoreBuilderConfig = publishImmediately
      ? { ...config, publishedAt: new Date().toLocaleDateString('fa-IR'), draftConfig: undefined }
      : { ...base, draftConfig: config };
    updateCurrentStoreObject({ ...currentStore, builderConfig: nextConfig });
    if (!isMockMode() && currentStore.slug) {
      storeApi.saveStoreSettings(currentStore.slug, { builderConfig: nextConfig }).catch((err) => console.warn('Failed to persist builder config:', err));
    }
  };

  const revertStoreBuilderConfig = () => {
    if (!currentStore) return;
    if (!currentStore.builderConfig) return;
    updateCurrentStoreObject({
      ...currentStore,
      builderConfig: { ...currentStore.builderConfig, draftConfig: undefined },
    });
  };

  const updateStoreCustomDomain = (domain: string) => {
    if (!currentStore) return;
    updateCurrentStoreObject({ ...currentStore, customDomain: domain.trim().toLowerCase() });
  };

  const updateStorePlan = (planId: 'trial' | 'standard' | 'pro' | 'vip') => {
    if (!currentStore) return;
    updateCurrentStoreObject({ ...currentStore, activePlanId: planId });
  };

  const requestStoreNameChange = (storeId: string, requestedName: string) => {
    if (!currentStore) return;
    const req: StoreNameChangeRequest = {
      id: `REQ-N-${Date.now().toString().slice(-4)}`,
      storeId,
      currentName: currentStore.name,
      requestedName,
      sellerMobile: '09121112233',
      status: 'pending',
      requestDate: new Date().toLocaleDateString('fa-IR'),
    };
    setStoreNameChangeRequests((prev) => [req, ...prev]);
  };

  const approveStoreNameChange = (requestId: string) => {
    if (!currentStore) return;
    const req = storeNameChangeRequests.find((r) => r.id === requestId);
    if (!req) return;
    setStoreNameChangeRequests((prev) =>
      prev.map((r) => (r.id === requestId ? { ...r, status: 'approved' } : r))
    );
    updateCurrentStoreObject({ ...currentStore, name: req.requestedName });
  };

  const rejectStoreNameChange = (requestId: string) => {
    setStoreNameChangeRequests((prev) =>
      prev.map((r) => (r.id === requestId ? { ...r, status: 'rejected' } : r))
    );
  };

  const requestStoreSlugChange = (storeId: string, requestedSlug: string) => {
    if (!currentStore) return;
    const req: StoreSlugChangeRequest = {
      id: `REQ-S-${Date.now().toString().slice(-4)}`,
      storeId,
      currentSlug: currentStore.slug,
      requestedSlug,
      sellerMobile: '09121112233',
      status: 'pending',
      requestDate: new Date().toLocaleDateString('fa-IR'),
    };
    setStoreSlugChangeRequests((prev) => [req, ...prev]);
  };

  const approveStoreSlugChange = (requestId: string) => {
    if (!currentStore) return;
    const req = storeSlugChangeRequests.find((r) => r.id === requestId);
    if (!req) return;
    setStoreSlugChangeRequests((prev) =>
      prev.map((r) => (r.id === requestId ? { ...r, status: 'approved' } : r))
    );
    updateCurrentStoreObject({ ...currentStore, slug: req.requestedSlug });
  };

  const rejectStoreSlugChange = (requestId: string) => {
    setStoreSlugChangeRequests((prev) =>
      prev.map((r) => (r.id === requestId ? { ...r, status: 'rejected' } : r))
    );
  };

  const requestCustomDomain = (storeId: string, domain: string) => {
    if (!currentStore) return;
    const req: CustomDomainRequest = {
      id: `REQ-D-${Date.now().toString().slice(-4)}`,
      storeId,
      storeName: currentStore.name,
      requestedDomain: domain,
      status: 'pending',
      requestDate: new Date().toLocaleDateString('fa-IR'),
    };
    setCustomDomainRequests((prev) => [req, ...prev]);
  };

  const approveCustomDomain = (requestId: string) => {
    const req = customDomainRequests.find((r) => r.id === requestId);
    if (!req) return;
    setCustomDomainRequests((prev) =>
      prev.map((r) => (r.id === requestId ? { ...r, status: 'approved' } : r))
    );
    updateStoreCustomDomain(req.requestedDomain);
  };

  const rejectCustomDomain = (requestId: string) => {
    setCustomDomainRequests((prev) =>
      prev.map((r) => (r.id === requestId ? { ...r, status: 'rejected' } : r))
    );
  };
  const updateStoreSelectedNiches = (nicheIds: string[]) => {
    if (!currentStore) return;
    updateCurrentStoreObject({ ...currentStore, selectedNicheIds: nicheIds });
  };

  const createPreMadeWidget = (widget: Omit<PreMadeWidget, 'id' | 'createdAt'>) => {
    const created: PreMadeWidget = {
      id: `WGT-${Date.now().toString().slice(-4)}`,
      createdAt: new Date().toLocaleDateString('fa-IR'),
      ...widget,
    };
    setPreMadeWidgets((prev) => [created, ...prev]);
  };

  const updatePreMadeWidget = (id: string, updated: Partial<PreMadeWidget>) => {
    setPreMadeWidgets((prev) => prev.map((w) => (w.id === id ? { ...w, ...updated } : w)));
  };

  const updateSellerDealsWidgetConfig = (config: Store['sellerDealsWidgetConfig']) => {
    if (!currentStore) return;
    updateCurrentStoreObject({ ...currentStore, sellerDealsWidgetConfig: config });
  };

  const updateCentralDealsFilter = (filter: 'my_niches' | 'manual' | 'all', productIds?: string[]) => {
    if (!currentStore) return;
    const nextProductIds = productIds ?? currentStore.centralDealsProductIds;
    updateCurrentStoreObject({ ...currentStore, centralDealsFilter: filter, centralDealsProductIds: nextProductIds });
    if (!isMockMode() && currentStore.slug) {
      storeApi.saveStoreSettings(currentStore.slug, { centralDealsFilter: filter, centralDealsProductIds: nextProductIds }).catch((err) => console.warn('Failed to persist central deals filter:', err));
    }
  };

  const verifyAndConnectDomainAutomatically = async (storeId: string, domain: string) => {
    updateStoreCustomDomain(domain);
    return { success: true, message: `دامنه ${domain} با موفقیت تایید و به ویترین متصل شد.` };
  };

  const purchaseServiceWithWallet = (storeId: string, serviceType: any, cost: number, details?: string): boolean => {
    if (isMockMode()) return true;
    if (!currentStore?.slug || currentStore.id !== storeId) return false;
    return false;
  };

  const updateStoreSocialLinks = (socialLinks: StoreSocialLinks) => {
    if (!currentStore) return;
    if (currentStore.builderConfig) {
      updateCurrentStoreObject({
        ...currentStore,
        builderConfig: { ...currentStore.builderConfig, socialLinks },
      });
    }
  };

  const updateStoreAdminMarginOverride = (storeId: string, margin: number, isLocked: boolean) => {
    if (!currentStore) return;
    updateCurrentStoreObject({
      ...currentStore,
      profitMarginPercent: margin,
      isMarginLockedByAdmin: isLocked,
    });
  };


  const value: StoreState = {
    currentStore,
    setCurrentStore,
    stores,
    addStore,
    storeStatuses,
    toggleStoreStatus,
    updateStoreActiveProducts,
    toggleCategoryActiveProducts,
    updateStoreProductPriceOverride,
    updateStoreProductPriceOverrideWithMode,
    updateStoreCategoryPriceOverride,
    updateStoreNichePriceOverride,
    updateStoreBuilderConfig,
    revertStoreBuilderConfig,
    updateStoreCustomDomain,
    updateStorePlan,
    storeNameChangeRequests,
    requestStoreNameChange,
    approveStoreNameChange,
    rejectStoreNameChange,
    storeSlugChangeRequests,
    requestStoreSlugChange,
    approveStoreSlugChange,
    rejectStoreSlugChange,
    customDomainRequests,
    requestCustomDomain,
    approveCustomDomain,
    rejectCustomDomain,
    updateStoreSelectedNiches,
    preMadeWidgets,
    createPreMadeWidget,
    updatePreMadeWidget,
    updateSellerDealsWidgetConfig,
    updateCentralDealsFilter,
    referredStoreSlug,
    setReferredStoreSlug,
    verifyAndConnectDomainAutomatically,
    purchaseServiceWithWallet,
    updateStoreSocialLinks,
    updateStoreAdminMarginOverride,
    isSellerModalOpen,
    setIsSellerModalOpen,
    directBuy,
    startDirectBuy,
    clearDirectBuy,
    directBuyAddress,
    setDirectBuyAddress,
    unreadNotificationCount,
    setUnreadNotificationCount,
    activeView,
    setActiveView,
    staticPageKey,
    openStaticPage,
    storefrontSlug,
    storefrontLoading,
    storefrontResolved,
    storefrontInvalid,
    storefrontSuspended,
    storefrontUnsubscribeSlug,
    sellerDashboardTab,
    setSellerDashboardTab,
    routeProductPending,
    routeProductInvalid,
    routeStoreSlug,
    routeStoreResolved,
    routeStoreInvalid,
    openStorefront,
  };

  if (storesLoading || waitingForOwnerStore) {
    return (
      <StoreContext.Provider value={value}>
        <div className="min-h-screen bg-slate-50 flex items-center justify-center p-6 text-center">
          <div className="text-sm text-slate-500">در حال بارگذاری فروشگاه‌ها...</div>
        </div>
      </StoreContext.Provider>
    );
  }

  if (!currentStore) {
    // M1 (round 2): when the routed store is terminally invalid
    // (unknown/suspended slug on cold load), render the store 404
    // HERE instead of the bare dead-end line. The children cannot
    // simply be let through: useStore() throws when currentStore
    // is null (AppContent + ProductDetailView both consume it), so
    // mounting them crashes the app to a blank page. This markup
    // is the NotFoundView variant="store" content verbatim (same
    // copy, same back link: history back, else home) — that
    // component cannot be imported here because it pulls
    // Header/Footer, which import this context (cycle).
    // routeStoreInvalid is only ever set by the PDP resolve
    // effect, so no other view is affected.
    if (routeStoreInvalid) {
      return (
        <StoreContext.Provider value={value}>
          <div className="flex-1 flex flex-col bg-white min-h-screen">
            <main className="flex-1 flex flex-col items-center justify-center px-6 py-16 text-center select-none bg-[#FAF9F7]">
              <div className="w-20 h-20 rounded-full flex items-center justify-center mb-5 bg-stone-200/70">
                <SearchX className="w-10 h-10 text-stone-400" strokeWidth={1.5} />
              </div>
              <h1 className="text-5xl font-black tracking-tight text-stone-900">۴۰۴</h1>
              <h2 className="mt-2 text-sm font-bold text-stone-800">صفحه مورد نظر یافت نشد</h2>
              <p className="mt-2 text-xs leading-relaxed max-w-[260px] text-stone-500">
                به نظر می‌رسد نشانی فروشگاه اشتباه است یا این فروشگاه دیگر فعال نیست.
              </p>
              <button
                type="button"
                onClick={() => { if (typeof window !== 'undefined' && window.history.length > 1) { window.history.back(); } else if (typeof window !== 'undefined') { window.location.href = '/'; } }}
                className="mt-6 text-xs font-bold text-stone-500 hover:text-stone-800 flex items-center gap-1 transition-colors"
              >
                <ArrowRight className="w-3.5 h-3.5" strokeWidth={1.5} />
                <span>بازگشت به صفحه قبل</span>
              </button>
            </main>
          </div>
        </StoreContext.Provider>
      );
    }
    return (
      <StoreContext.Provider value={value}>
        {allowUnselectedChildren ? children : (
          <div className="min-h-screen bg-slate-50 flex items-center justify-center p-6 text-center">
            <div className="space-y-3">
              <p role="alert" className="text-sm text-slate-700">بارگذاری فروشگاه انجام نشد.</p>
              <button type="button" onClick={() => { setStoresLoading(true); setReloadStores(value => value + 1); }}
                className="min-h-11 rounded-xl bg-[#164638] px-4 text-sm font-bold text-white">تلاش دوباره</button>
            </div>
          </div>
        )}
      </StoreContext.Provider>
    );
  }

  return <StoreContext.Provider value={value}>{children}</StoreContext.Provider>;
};

export const useStoreManager = (): StoreState => {
  const context = useContext(StoreContext);
  if (!context) {
    throw new Error('useStoreManager must be used within a StoreProvider');
  }
  return context;
};

export const useStore = (): StoreContextType => {
  const store = useStoreManager();
  const auth = useAuth();
  const prod = useProducts();
  const cart = useCart();
  const ord = useOrders();
  const fin = useFinance();
  const wh = useWarehouse();

  if (!store.currentStore) {
    throw new Error('A store must be selected before consuming the store context');
  }

  // Store-scoped category/brand navigation: while a store context is
  // active (a store-scoped PDP's routeStoreSlug, or a dropshipper
  // currentStore), category navigation lands on THAT store's own
  // storefront — never the central catalog. The prod-level calls in
  // openCatalogCategory / openCatalogBrand have already written the
  // shared selectedCategory / searchQuery / catalogBrandRequest
  // state, which is exactly the channel the storefront consumes (the
  // fashion floor, the shared ProductsGridSection, the legacy grid
  // and the storefront categories row all read it), so only the VIEW
  // differs. Already inside that store's storefront, no view change
  // is needed: reveal the products floor instead of re-navigating,
  // which would reset the storefront's resolution state and flash
  // its loader.
  const goToScopedStorefront = (slug: string) => {
    if (store.activeView === 'seller_storefront' && store.currentStore?.slug === slug) {
      if (typeof window !== 'undefined') {
        const floor = document.getElementById('products');
        if (floor) floor.scrollIntoView({ behavior: 'smooth' });
        else window.scrollTo({ top: 0, behavior: 'smooth' });
      }
      return;
    }
    store.setActiveView('seller_storefront', { storeSlug: slug });
    if (typeof window !== 'undefined') {
      window.scrollTo({ top: 0, behavior: 'smooth' });
    }
  };

  return {
    currentStore: store.currentStore,
    setCurrentStore: store.setCurrentStore as (store: Store) => void,
    stores: store.stores,
    addStore: store.addStore,
    storeStatuses: store.storeStatuses,
    toggleStoreStatus: store.toggleStoreStatus,
    updateStoreActiveProducts: store.updateStoreActiveProducts,
    toggleCategoryActiveProducts: store.toggleCategoryActiveProducts,
    updateStoreProductPriceOverride: store.updateStoreProductPriceOverride,
    updateStoreProductPriceOverrideWithMode: store.updateStoreProductPriceOverrideWithMode,
    updateStoreCategoryPriceOverride: store.updateStoreCategoryPriceOverride,
    updateStoreNichePriceOverride: store.updateStoreNichePriceOverride,
    updateStoreBuilderConfig: store.updateStoreBuilderConfig,
    revertStoreBuilderConfig: store.revertStoreBuilderConfig,
    updateStoreCustomDomain: store.updateStoreCustomDomain,
    updateStorePlan: store.updateStorePlan,
    storeNameChangeRequests: store.storeNameChangeRequests,
    requestStoreNameChange: store.requestStoreNameChange,
    approveStoreNameChange: store.approveStoreNameChange,
    rejectStoreNameChange: store.rejectStoreNameChange,
    storeSlugChangeRequests: store.storeSlugChangeRequests,
    requestStoreSlugChange: store.requestStoreSlugChange,
    approveStoreSlugChange: store.approveStoreSlugChange,
    rejectStoreSlugChange: store.rejectStoreSlugChange,
    customDomainRequests: store.customDomainRequests,
    requestCustomDomain: store.requestCustomDomain,
    approveCustomDomain: store.approveCustomDomain,
    rejectCustomDomain: store.rejectCustomDomain,
    updateStoreSelectedNiches: store.updateStoreSelectedNiches,
    preMadeWidgets: store.preMadeWidgets,
    createPreMadeWidget: store.createPreMadeWidget,
    updatePreMadeWidget: store.updatePreMadeWidget,
    updateSellerDealsWidgetConfig: store.updateSellerDealsWidgetConfig,
    updateCentralDealsFilter: store.updateCentralDealsFilter,
    referredStoreSlug: store.referredStoreSlug,
    setReferredStoreSlug: store.setReferredStoreSlug,
    verifyAndConnectDomainAutomatically: store.verifyAndConnectDomainAutomatically,
    purchaseServiceWithWallet: store.purchaseServiceWithWallet,
    updateStoreSocialLinks: store.updateStoreSocialLinks,
    updateStoreAdminMarginOverride: store.updateStoreAdminMarginOverride,
    isSellerModalOpen: store.isSellerModalOpen,
    setIsSellerModalOpen: store.setIsSellerModalOpen,
    activeView: store.activeView,
    setActiveView: store.setActiveView,
    staticPageKey: store.staticPageKey,
    openStaticPage: store.openStaticPage,
    storefrontSlug: store.storefrontSlug,
    storefrontLoading: store.storefrontLoading,
    storefrontResolved: store.storefrontResolved,
    storefrontInvalid: store.storefrontInvalid,
    storefrontSuspended: store.storefrontSuspended,
    storefrontUnsubscribeSlug: store.storefrontUnsubscribeSlug,
    storefrontProductDetail: store.activeView === 'seller_storefront' ? prod.currentProductDetail : null,
    openStorefront: store.openStorefront,
    sellerDashboardTab: store.sellerDashboardTab,
    setSellerDashboardTab: store.setSellerDashboardTab,
    routeProductPending: store.routeProductPending,
    routeProductInvalid: store.routeProductInvalid,
    routeStoreSlug: store.routeStoreSlug,
    routeStoreResolved: store.routeStoreResolved,
    routeStoreInvalid: store.routeStoreInvalid,
    formatPrice,
    toPersianDigits,
    // Auth Context
    customerUser: auth.customerUser,
    setCustomerUser: auth.setCustomerUser,
    logoutCustomer: auth.logoutCustomer,
    adminSession: auth.adminSession,
    setAdminSession: auth.setAdminSession,
    logoutAdmin: auth.logoutAdmin,
    sellerSession: auth.sellerSession,
    setSellerSession: auth.setSellerSession,
    logoutSeller: auth.logoutSeller,
    supplierSession: auth.supplierSession,
    setSupplierSession: auth.setSupplierSession,
    logoutSupplier: auth.logoutSupplier,
    warehouseSession: auth.warehouseSession,
    setWarehouseSession: auth.setWarehouseSession,
    logoutWarehouse: auth.logoutWarehouse,

    // Product Context
    products: prod.products,
    categories: prod.categories,
    niches: prod.niches,
    catalogLoading: prod.catalogLoading,
    catalogError: prod.catalogError,
    fetchCatalogProducts: prod.fetchProducts,
    catalogPriceRangeError: prod.catalogPriceRangeError,
    // PERF-01: paged catalog passthrough (page 1 + show-more).
    catalogFilter: prod.catalogFilter,
    setCatalogFilter: prod.setCatalogFilter,
    resetCatalogFilter: prod.resetCatalogFilter,
    loadMoreCatalog: prod.loadMoreCatalog,
    hasMoreCatalog: prod.hasMoreCatalog,
    catalogTotal: prod.catalogTotal,
    catalogLoadingMore: prod.catalogLoadingMore,
    catalogLoadMoreError: prod.catalogLoadMoreError,
    searchQuery: prod.searchQuery,
    setSearchQuery: prod.setSearchQuery,
    selectedCategory: prod.selectedCategory,
    setSelectedCategory: prod.setSelectedCategory,
    selectedProduct: prod.selectedProduct,
    setSelectedProduct: prod.setSelectedProduct,
    currentProductDetail: prod.currentProductDetail,
    openProductDetail: (product: Product) => {
      prod.openProductDetail(product);
      // Store-scoped opens: a click inside a seller storefront — or on
      // a product reached from one (e.g. PDP related items) — must keep
      // that store as the PDP's context. The product route carries no
      // store, so the slug is threaded through the navigation state;
      // without it the PDP defaults to the central store's frame,
      // pricing and back target.
      const scopedSlug = activeStoreSlugOf(store);
      store.setActiveView(
        'product_detail',
        scopedSlug ? { productId: product.id, storeSlug: scopedSlug } : { productId: product.id },
      );
      if (typeof window !== 'undefined') {
        window.scrollTo({ top: 0, behavior: 'smooth' });
      }
    },
    openCatalogCategory: (categorySlug: string, query: string = '') => {
      prod.openCatalogCategory(categorySlug, query);
      const slug = activeStoreSlugOf(store);
      if (slug) {
        goToScopedStorefront(slug);
        return;
      }
      store.setActiveView('catalog', { searchQuery: query, categorySlug });
      if (typeof window !== 'undefined') {
        window.scrollTo({ top: 0, behavior: 'smooth' });
      }
    },
    // FIX 71: PDP brand click -> catalog filtered to that brand (all categories).
    // Store-scoped PDPs stay in-store, like category navigation above:
    // the shared catalogBrandRequest state is consumed by the
    // storefront floors, so the brand click lands on the store's own
    // storefront instead of ejecting to the central catalog.
    openCatalogBrand: (brand: string) => {
      prod.openCatalogBrand(brand);
      const slug = activeStoreSlugOf(store);
      if (slug) {
        goToScopedStorefront(slug);
        return;
      }
      store.setActiveView('catalog', { searchQuery: '', categorySlug: 'all' });
      if (typeof window !== 'undefined') {
        window.scrollTo({ top: 0, behavior: 'smooth' });
      }
    },
    catalogBrandRequest: prod.catalogBrandRequest,
    clearCatalogBrand: prod.clearCatalogBrand,
    wishlistIds: prod.wishlistIds,
    wishlists: prod.wishlists,
    toggleWishlist: prod.toggleWishlist,
    isWishlisted: prod.isWishlisted,
    toggleProductOriginality: prod.toggleProductOriginality,
    addProduct: prod.addProduct,

    // Cart Context
    cart: cart.cart,
    addToCart: (p: Product, q?: number, c?: string, variantId?: string, addonIds?: string[]) => {
      if (!store.currentStore) return;
      // P0-2: while a store-scoped PDP is still resolving its store,
      // no line may be stamped with the wrong (central) cart bucket.
      if (store.routeStoreSlug && store.currentStore.slug !== store.routeStoreSlug) return;
      if (!variantId && (p.productType === 'variable' || (p.variants?.length && !resolvePurchase(p)))) {
        prod.openProductDetail(p); return;
      }
      cart.addToCart(p, q, c, store.currentStore, variantId, addonIds);
    },
    removeFromCart: cart.removeFromCart,
    updateQuantity: cart.updateQuantity,
    clearCart: cart.clearCart,
    cartTotal: cart.cartTotal,
    cartDiscount: cart.cartDiscount,
    cartPayable: cart.cartPayable,
    cartAddonsTotal: cart.cartAddonsTotal,
    cartItemsCount: cart.cartItemsCount,
    isCartOpen: cart.isCartOpen,
    setIsCartOpen: cart.setIsCartOpen,
    directBuy: store.directBuy,
    startDirectBuy: store.startDirectBuy,
    clearDirectBuy: store.clearDirectBuy,
    directBuyAddress: store.directBuyAddress,
    setDirectBuyAddress: store.setDirectBuyAddress,

    // Order Context
    orders: ord.orders,
    trackingOrder: ord.trackingOrder,
    setTrackingOrder: ord.setTrackingOrder,
    isTrackingModalOpen: ord.isTrackingModalOpen,
    setIsTrackingModalOpen: ord.setIsTrackingModalOpen,
    // Worker A (buy-now): live's placeOrder wrapper accepts a lines/store
    // override for direct buy (index-MERGED.js ze(): placeOrder(p, y, j, N)
    // with N.lines/N.store). A direct order must NOT clear the cart.
    placeOrder: (customerInfo: Order['customerInfo'], paymentMethod: 'cash', discountCode?: string, shippingQuoteId?: string, direct?: { lines: CartItem[]; store?: DirectBuyStore }, address?: { addressId?: string; saveAddress?: boolean }) => {
      const directStore = direct?.store
        ? (store.stores.find((s) => s.id === direct.store!.id) || direct.store || store.currentStore || undefined)
        : (store.currentStore || undefined);
      return ord.placeOrder({
        customerInfo,
        paymentMethod,
        discountCode,
        shippingQuoteId,
        addressId: address?.addressId,
        saveAddress: address?.saveAddress,
        cartItems: direct?.lines ?? cart.cart,
        currentStore: direct ? directStore : (store.currentStore || undefined),
        customerPhone: auth.customerUser.mobile,
        referredStoreSlug: store.referredStoreSlug,
        stores: store.stores,
      }).then((res: Order) => {
        if (!direct) cart.clearCart();
        return res;
      });
    },
    confirmOrderDelivery: ord.confirmOrderDelivery,
    disputeOrderDelivery: ord.disputeOrderDelivery,
    busyOrderId: ord.busyOrderId,
    adminResolveDeliveryDispute: ord.adminResolveDeliveryDispute,

    // Finance Context
    payoutRequests: fin.payoutRequests,
    requestPayout: fin.requestPayout,
    approvePayout: fin.approvePayout,
    rejectPayout: fin.rejectPayout,
    adjustmentRecords: fin.adjustmentRecords,
    createAdjustmentRecord: fin.createAdjustmentRecord,
    disputes: fin.disputes,
    resolveDisputeAndAdjust: fin.resolveDisputeAndAdjust,
    verifyShebaOwnerName: fin.verifyShebaOwnerName,
    executeSupplierMonthlyPayouts: fin.executeSupplierMonthlyPayouts,
    platformSettings: fin.platformSettings,
    updatePlatformSettings: fin.updatePlatformSettings,
    updateContracts: fin.updateContracts,
    updateSubscriptionPlans: fin.updateSubscriptionPlans,
    updateTrialCatalogQuota: fin.updateTrialCatalogQuota,
    updateDeliveryConfirmationSettings: fin.updateDeliveryConfirmationSettings,
    updateWarehouseTariffs: fin.updateWarehouseTariffs,
    updatePackagingTariffs: fin.updatePackagingTariffs,
    unifiedPendingApprovals: fin.unifiedPendingApprovals,
    approveUnifiedItem: fin.approveUnifiedItem,
    rejectUnifiedItem: fin.rejectUnifiedItem,
    auditLogs: fin.auditLogs,
    logAuditEvent: fin.logAuditEvent,

    // Warehouse Context
    inboundShipments: wh.inboundShipments,
    createInboundShipment: wh.createInboundShipment,
    receiveInboundShipment: wh.receiveInboundShipment,
    completePutaway: wh.completePutaway,
    completeOrderPicking: wh.completeOrderPicking,
    completeOrderPacking: wh.completeOrderPacking,
    dispatchOrdersToShipping: wh.dispatchOrdersToShipping,
    processWarehouseReturn: wh.processWarehouseReturn,
    qcDiscrepancies: wh.qcDiscrepancies,
    submitQCDiscrepancy: wh.submitQCDiscrepancy,
    acceptQCDiscrepancy: wh.acceptQCDiscrepancy,
    disputeQCDiscrepancy: wh.disputeQCDiscrepancy,
    proposedProducts: wh.proposedProducts,
    submitProposedProduct: wh.submitProposedProduct,
    approveAndMergeProduct: wh.approveAndMergeProduct,
    rejectProposedProduct: wh.rejectProposedProduct,
    updateWholesaleCost: wh.updateWholesaleCost,
    adCampaigns: wh.adCampaigns,
    createAdCampaign: wh.createAdCampaign,
    toggleAdCampaign: wh.toggleAdCampaign,
    createSupplierPromotion: wh.createSupplierPromotion,
    deleteSupplierPromotion: wh.deleteSupplierPromotion,
  };
};

// --- Store-scoped navigation helpers --------------------------------
// The store context a shopper is currently inside, if any: the store
// a store-scoped PDP was opened from (routeStoreSlug), else a
// dropshipper currentStore. Central browsing yields null. Category
// navigation (useStore's openCatalogCategory above) and every
// category menu/tree use this single derivation, so they can never
// disagree about whether a store context is active.
export const activeStoreSlugOf = (scope: {
  routeStoreSlug: string | null;
  currentStore?: { ownerType?: string; slug?: string } | null;
}): string | null =>
  scope.routeStoreSlug ??
  (scope.currentStore?.ownerType === 'dropshipper' ? scope.currentStore.slug ?? null : null);

// One scoped fetch per store, shared by every consumer (header mega
// menu, categories tree view, home categories bar): the set of
// category slugs the store actually sells, taken from its
// store-scoped product list — the same fetch SellerStorefrontView
// prices its grids from.
const storeCategorySlugsCache = new Map<string, Promise<Set<string>>>();
const fetchStoreCategorySlugs = (storeSlug: string): Promise<Set<string>> => {
  let pending = storeCategorySlugsCache.get(storeSlug);
  if (!pending) {
    // PERF-01: one DISTINCT query (GET /products/store-categories),
    // never the full store catalog.
    pending = catalogApi
      .getStoreCategorySlugs(storeSlug)
      .then((slugs) => new Set(slugs.filter((slug): slug is string => !!slug)))
      .catch(() => new Set<string>());
    storeCategorySlugsCache.set(storeSlug, pending);
  }
  return pending;
};

// The active store's sold-category slugs. categorySlugs is null
// while the scoped fetch is in flight (and whenever no store
// context is active): a consumer that has a storeSlug but null
// categorySlugs must treat it as "not loaded yet" and never fall
// back to the full platform category list — that fallback is
// exactly the leak this restriction exists to close.
export const useActiveStoreCategorySlugs = (): { storeSlug: string | null; categorySlugs: Set<string> | null } => {
  const { currentStore, routeStoreSlug } = useStore();
  const storeSlug = activeStoreSlugOf({ routeStoreSlug, currentStore });
  const [categorySlugs, setCategorySlugs] = useState<Set<string> | null>(null);
  useEffect(() => {
    if (!storeSlug) {
      setCategorySlugs(null);
      return;
    }
    let cancelled = false;
    setCategorySlugs(null);
    fetchStoreCategorySlugs(storeSlug).then((slugs) => {
      if (!cancelled) setCategorySlugs(slugs);
    });
    return () => {
      cancelled = true;
    };
  }, [storeSlug]);
  return { storeSlug, categorySlugs };
};

// Products sit in leaf categories, so a sold leaf's ANCESTORS are
// part of the store's category set too — without them a filtered
// tree would orphan the leaves and lose the navigation path to them.
export const expandCategorySlugsWithAncestors = (categories: Category[], slugs: Set<string>): Set<string> => {
  const bySlug = new Map(categories.map((c) => [c.slug, c]));
  const byId = new Map(categories.map((c) => [c.id, c]));
  const included = new Set<string>();
  slugs.forEach((slug) => {
    let cur = bySlug.get(slug);
    const guard = new Set<string>();
    while (cur && !guard.has(cur.id)) {
      guard.add(cur.id);
      included.add(cur.slug);
      cur = cur.parentId ? byId.get(cur.parentId) : undefined;
    }
  });
  return included;
};
