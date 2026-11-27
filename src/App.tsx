import React, { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { AuthProvider } from './context/AuthContext';
import { ProductProvider } from './context/ProductContext';
import { CartProvider } from './context/CartContext';
import { OrderProvider } from './context/OrderContext';
import { FinanceProvider } from './context/FinanceContext';
import { WarehouseProvider } from './context/WarehouseContext';
import { StoreProvider, useStore } from './context/StoreContext';
import { ToastProvider } from './context/ToastContext';
import { RoleSwitcherBar } from './components/RoleSwitcherBar';
import { Header } from './components/Header';
import { CategoriesBar } from './components/CategoriesBar';
import { HeroSlider } from './components/HeroSlider';
import { GitDeployProbe } from './components/GitDeployProbe';
import { SpecialDealsSlider } from './components/SpecialDealsSlider';
import { HomeFeedSections } from './components/HomeFeedSections';
import { Footer } from './components/Footer';
import { SellerOnboardingProvider } from './context/SellerOnboardingContext';
import { BottomNav } from './components/BottomNav';
import { storeApi } from './services/storeApi';
import { defaultPanelForUser, panelsForCapabilities, useAuth, type AuthenticatedPanel } from './context/AuthContext';

import { NotFoundView } from './components/NotFoundView';
import { SeoManager } from './seo/SeoManager';
import { isMockMode } from './services/apiClient';
import { RouteLoading } from './components/common/RouteLoading';

const ProductDetailView = lazy(() => import('./components/views/ProductDetailView').then(module => ({ default: module.ProductDetailView })));
const CatalogView = lazy(() => import('./components/views/CatalogView').then(module => ({ default: module.CatalogView })));
const CategoriesTreeView = lazy(() => import('./components/views/CategoriesTreeView').then(module => ({ default: module.CategoriesTreeView })));
const CustomerProfileView = lazy(() => import('./components/views/CustomerProfileView').then(module => ({ default: module.CustomerProfileView })));
const CustomerOrdersView = lazy(() => import('./components/views/CustomerOrdersView').then(module => ({ default: module.CustomerOrdersView })));
const CustomerWishlistView = lazy(() => import('./components/views/CustomerWishlistView').then(module => ({ default: module.CustomerWishlistView })));
const StaticPageView = lazy(() => import('./components/views/StaticPageView').then(module => ({ default: module.StaticPageView })));
const SupplierLandingView = lazy(() => import('./components/views/SupplierLandingView').then(module => ({ default: module.SupplierLandingView })));
const DropshipperLandingView = lazy(() => import('./components/views/DropshipperLandingView').then(module => ({ default: module.DropshipperLandingView })));
const SellerStorefrontView = lazy(() => import('./components/storefront/SellerStorefrontView').then(module => ({ default: module.SellerStorefrontView })));
const SellerStorefrontHeader = lazy(() => import('./components/storefront/SellerStorefrontHeader').then(module => ({ default: module.SellerStorefrontHeader })));
const SellerStorefrontFooter = lazy(() => import('./components/storefront/SellerStorefrontFooter').then(module => ({ default: module.SellerStorefrontFooter })));
const StorefrontBottomNav = lazy(() => import('./components/storefront/SellerStorefrontFooter').then(module => ({ default: module.StorefrontBottomNav })));
const StoreUnsubscribeView = lazy(() => import('./components/storefront/StoreUnsubscribeView').then(module => ({ default: module.StoreUnsubscribeView })));
const ProductDetailModal = lazy(() => import('./components/ProductDetailModal').then(module => ({ default: module.ProductDetailModal })));
const CartDrawer = lazy(() => import('./components/CartDrawer').then(module => ({ default: module.CartDrawer })));
const SellerOnboardingModal = lazy(() => import('./components/SellerOnboardingModal').then(module => ({ default: module.SellerOnboardingModal })));
const OrderTrackingModal = lazy(() => import('./components/OrderTrackingModal').then(module => ({ default: module.OrderTrackingModal })));
const AuthModal = lazy(() => import('./components/auth/AuthModal').then(module => ({ default: module.AuthModal })));
const PanelsPreview = lazy(() => import('./components/PanelsPreview').then(module => ({ default: module.PanelsPreview })));
const SellerDashboardView = lazy(() => import('./components/seller/SellerDashboardView').then(module => ({ default: module.SellerDashboardView })));
const SupplierDashboardView = lazy(() => import('./components/supplier/SupplierDashboardView').then(module => ({ default: module.SupplierDashboardView })));
const WarehouseDashboardView = lazy(() => import('./components/warehouse/WarehouseDashboardView').then(module => ({ default: module.WarehouseDashboardView })));
const AdminDashboardView = lazy(() => import('./components/admin/AdminDashboardView').then(module => ({ default: module.AdminDashboardView })));

function PanelLoading() {
  return <RouteLoading label="در حال بارگذاری پنل…" screen />;
}

function StorePageLoading({ children = 'در حال بارگذاری…' }: { children?: string }) {
  return <main id="main-content" tabIndex={-1} className="central-wrap central-product-loading"><RouteLoading label={children} /></main>;
}

function OverlayLoading() {
  return <p className="central-overlay-loading" role="status">در حال بارگذاری…</p>;
}

const workspaceLabels: Record<AuthenticatedPanel, string> = {
  customer: 'فروشگاه',
  seller_dashboard: 'فروشنده',
  supplier_dashboard: 'تأمین‌کننده',
  warehouse_panel: 'انبار',
  admin_panel: 'مدیریت',
};

function CustomerAccountSignIn({ onBack, central }: { onBack: () => void; central: boolean }) {
  const signedIn = useRef(false);
  return (
    <main dir="rtl" className={central ? "min-h-dvh bg-[#f8f6ef]" : "min-h-dvh bg-[#f7f7fa]"}>
      <Suspense fallback={<RouteLoading label="در حال آماده‌سازی ورود…" screen />}>
        <AuthModal isOpen central={central} disableAutoNavigate roleTitle="ورود به حساب کاربری"
          onSuccess={() => { signedIn.current = true; }}
          onClose={() => { if (!signedIn.current) onBack(); }} />
      </Suspense>
    </main>
  );
}

export const AppContent: React.FC = () => {
  const { currentUser, isSessionReady, sessionCheckFailed, retrySessionCheck, logoutUser, supplierSession, sellerSession, adminSession, panelSessionErrors, retryPanelSession } = useAuth();
  const {
    activeView,
    setActiveView,
    currentProductDetail,
    currentStore,
    products,
    openProductDetail,
    storefrontSlug,
    storefrontLoading,
    storefrontResolved,
    storefrontInvalid,
    storefrontSuspended,
    storefrontUnsubscribeSlug,
    routeProductPending,
    routeProductInvalid,
    routeStoreSlug,
    routeStoreResolved,
    routeStoreInvalid,
    selectedCategory,
    searchQuery,
    openStorefront,
    isCartOpen,
    isTrackingModalOpen,
    selectedProduct,
    isSellerModalOpen,
    setIsSellerModalOpen,
  } = useStore();
  const [cartOpened, setCartOpened] = useState(false);
  useEffect(() => { if (isCartOpen) setCartOpened(true); }, [isCartOpen]);

  const customerAccountView = activeView === 'customer_profile' || activeView === 'customer_orders' || activeView === 'customer_wishlist';
  const protectedPanel = customerAccountView ? 'customer' : (Object.keys(workspaceLabels) as AuthenticatedPanel[])
    .find(panel => panel !== 'customer' && activeView === panel);
  const isWarehouseView = protectedPanel === 'warehouse_panel';
  const isOnboardingActive = isSellerModalOpen && !protectedPanel;
  const authorizedPanels = currentUser ? panelsForCapabilities(currentUser.capabilities) : [];
  const canOpenCentralProfile = activeView === 'customer_profile' && !routeStoreSlug && authorizedPanels.length > 0;
  const defaultPanel = currentUser ? defaultPanelForUser(currentUser) : 'customer';

  useEffect(() => {
    if (isSessionReady && currentUser && activeView === 'dashboard' && !isOnboardingActive) {
      setActiveView(defaultPanel);
    }
  }, [activeView, currentUser, isSessionReady, isOnboardingActive, defaultPanel, setActiveView]);

  const lastCatalogRoute = useRef<{ categorySlug: string; searchQuery: string } | null>(null);
  useEffect(() => {
    if (activeView === 'catalog') lastCatalogRoute.current = { categorySlug: selectedCategory, searchQuery };
    else if (activeView !== 'product_detail') lastCatalogRoute.current = null;
  }, [activeView, selectedCategory, searchQuery]);
  const backFromCentralProduct = () => {
    if (lastCatalogRoute.current) setActiveView('catalog', lastCatalogRoute.current);
    else setActiveView('customer');
  };

  const adClickRecorded = useRef(false);
  const adProductOpened = useRef(false);
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const adToken = params.get('ad_token');
    if (adProductOpened.current || !adToken) return;
    const campaignId = params.get('campaignId');
    const productId = params.get('productId');
    if (campaignId && !adClickRecorded.current) {
      adClickRecorded.current = true;
      void storeApi.recordSellerAdEvent(campaignId, 'click', adToken, productId || undefined).catch(() => {});
    }
    if (activeView === 'seller_storefront' && (!storefrontResolved || storefrontInvalid || storefrontSuspended)) return;
    if (routeStoreSlug && !routeStoreResolved) return;
    const product = products.find(item => item.id === productId);
    if (!product) return;
    adProductOpened.current = true;
    openProductDetail(product);
  }, [products, openProductDetail, activeView, storefrontResolved, storefrontInvalid, storefrontSuspended, routeStoreSlug, routeStoreResolved]);

  // Public pages can render while the session is verified. Workspace entry still waits.
  if (!isSessionReady && (protectedPanel || activeView === 'dashboard')) {
    if (sessionCheckFailed) {
      return (
        <main dir="rtl" className="grid min-h-dvh place-content-center justify-items-center gap-4 bg-[#f8f6ef] px-4 text-center">
          <p role="alert" className="text-sm font-semibold text-[#172d26]">بررسی حساب ممکن نشد. اتصال اینترنت را بررسی کنید.</p>
          <button type="button" onClick={retrySessionCheck} className="central-button">تلاش مجدد</button>
        </main>
      );
    }
    return <RouteLoading label="در حال بررسی حساب…" screen />;
  }
  if (routeStoreSlug && (!routeStoreResolved || routeStoreInvalid)) {
    return routeStoreInvalid
      ? <NotFoundView variant="store" onBack={() => setActiveView('seller_storefront', { storeSlug: routeStoreSlug })} />
      : <RouteLoading label="در حال بارگذاری ویترین…" screen />;
  }
  if ((activeView === 'customer_profile' || activeView === 'customer_orders' || activeView === 'customer_wishlist') &&
    !currentUser && !isMockMode()) {
    return <CustomerAccountSignIn central={!routeStoreSlug} onBack={() => {
      if (routeStoreSlug) {
        setActiveView('seller_storefront', { storeSlug: routeStoreSlug });
      } else {
        setActiveView('customer');
      }
    }} />;
  }
  if (activeView === 'dashboard') {
    if (currentUser) return <RouteLoading label="در حال باز کردن پنل…" screen />;
    return (
      <Suspense fallback={<PanelLoading />}>
        <SellerDashboardView
          onBackToCustomerSite={() => setActiveView('customer')}
          onOpenStorefront={() => setActiveView('seller_storefront')}
        />
      </Suspense>
    );
  }
  if (currentUser && protectedPanel && !isWarehouseView) {
    if (!canOpenCentralProfile && !authorizedPanels.includes(protectedPanel)) {
      return (
        <main dir="rtl" className="grid min-h-dvh w-full content-center justify-items-center bg-[#f7f7fa] p-4">
          <section aria-labelledby="workspace-access-title" className="w-full max-w-md space-y-4 rounded-2xl border border-slate-200 bg-white p-5">
            <h1 id="workspace-access-title" className="text-base font-extrabold text-slate-900">حساب فعلی مجوز پنل {workspaceLabels[protectedPanel]} را ندارد.</h1>
            <div className="flex flex-wrap gap-2">
              <button type="button" onClick={() => setActiveView(authorizedPanels.length ? 'customer_profile' : 'customer')}
                className="min-h-11 rounded-xl bg-brand-deep px-3 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand">
                {authorizedPanels.length ? 'رفتن به حساب کاربری' : 'بازگشت به سایت'}
              </button>
              <button type="button" onClick={logoutUser} className="min-h-11 rounded-xl border border-slate-200 px-3 text-sm font-bold text-slate-800 focus-visible:outline-2 focus-visible:outline-brand">خروج و ورود با حساب دیگر</button>
              {protectedPanel === 'seller_dashboard' && authorizedPanels.includes('customer') && (
                <button type="button" onClick={() => { setActiveView('dropshipper_landing'); setIsSellerModalOpen(true); }}
                  className="min-h-11 rounded-xl border border-brand-border px-3 text-sm font-bold text-brand-deep focus-visible:outline-2 focus-visible:outline-brand">ثبت فروشگاه جدید</button>
              )}
            </div>
          </section>
        </main>
      );
    }
    const ready = protectedPanel === 'customer' || (protectedPanel === 'supplier_dashboard' ? supplierSession
      : protectedPanel === 'seller_dashboard' ? sellerSession : adminSession);
    if (!ready) {
      const panelSessionError = protectedPanel === 'supplier_dashboard' || protectedPanel === 'seller_dashboard'
        ? panelSessionErrors[protectedPanel] : undefined;
      return panelSessionError
        ? <div className="flex min-h-screen flex-col items-center justify-center gap-4 p-4" dir="rtl">
            <p role="alert">{panelSessionError}</p>
            <button type="button" className="min-h-11 rounded-xl bg-brand-soft px-4 font-bold text-brand-deep" onClick={retryPanelSession}>تلاش مجدد</button>
          </div>
        : <RouteLoading label="در حال آماده‌سازی پنل…" screen />;
    }
  }

  if (activeView === 'admin_panel') {
    return (
      <>
        <Suspense fallback={<PanelLoading />}>
          <AdminDashboardView onBackToCustomerSite={() => setActiveView('customer')} />
        </Suspense>
      </>
    );
  }

  const isCentralSite = !routeStoreSlug && [
    'customer', 'catalog', 'categories_tree', 'product_detail', 'customer_profile',
    'customer_orders', 'customer_wishlist', 'static_page', 'supplier_landing', 'dropshipper_landing', 'not_found',
  ].includes(activeView);
  const showStorefrontNav = activeView === 'seller_storefront' ||
    (!!routeStoreSlug && activeView !== 'product_detail');
  const shellClass = isCentralSite
    ? 'central-site w-full min-h-screen flex flex-col'
    : 'w-full min-h-screen relative flex flex-col bg-white';

  return (
    <div className="min-h-screen bg-[#f8f6ef] flex justify-center text-slate-800 antialiased font-sans">
      <SeoManager />
      <div className={shellClass}>
        {/* Developer & Multi-Role Navigation Bar */}
        <RoleSwitcherBar />

        <Suspense fallback={<StorePageLoading />}>
        {activeView === 'seller_dashboard' ? (
          <Suspense fallback={<PanelLoading />}>
            <SellerDashboardView
              onBackToCustomerSite={() => setActiveView('customer')}
              onOpenStorefront={() => setActiveView('seller_storefront')}
            />
          </Suspense>
        ) : activeView === 'seller_storefront' ? (
          storefrontUnsubscribeSlug ? (
            <StoreUnsubscribeView slug={storefrontUnsubscribeSlug} onBack={() => setActiveView('seller_storefront', { storeSlug: storefrontUnsubscribeSlug })} />
          ) : storefrontLoading ? (
            <RouteLoading label="در حال بارگذاری ویترین فروشگاه…" />
          ) : storefrontInvalid ? (
            <NotFoundView variant="store" onBack={() => setActiveView('customer')} />
          ) : storefrontSuspended ? (
            // FIX 16: live renders its STORE-path 404 (bundle ek) for
            // suspended slugs: same 404 numerals/title, the store
            // body text, but ONLY the back link -- no primary button.
            // (Fix 15 used the generic two-button view; the browser
            // proved live's suspended page has only the back link.)
            // The API still returns 404 {"error":"Store is suspended"}.
            <NotFoundView variant="store" onBack={() => setActiveView('customer')} />
          ) : (
            <SellerStorefrontView
              onBackToCustomerSite={() => setActiveView('customer')}
            />
          )
        ) : activeView === 'not_found' ? (
          <NotFoundView onBack={() => setActiveView('customer')} />
        ) : activeView === 'supplier_dashboard' ? (
          <Suspense fallback={<PanelLoading />}>
            <SupplierDashboardView
              onBackToCustomerSite={() => setActiveView('customer')}
            />
          </Suspense>
        ) : activeView === 'warehouse_panel' ? (
          <Suspense fallback={<PanelLoading />}>
            <WarehouseDashboardView
              onBackToCustomerSite={() => setActiveView('customer')}
            />
          </Suspense>
        ) : activeView === 'product_detail' ? (
          routeProductPending ? (
            routeStoreSlug ? (
              <RouteLoading label="در حال بارگذاری کالا…" />
            ) : (
              <>
                <Header sticky={false} />
                <StorePageLoading>در حال بارگذاری کالا…</StorePageLoading>
                <Footer />
              </>
            )
          ) : routeProductInvalid || !currentProductDetail ? (
            <NotFoundView variant={routeStoreSlug ? 'store' : 'generic'} onBack={() => {
              if (routeStoreSlug) setActiveView('seller_storefront', { storeSlug: routeStoreSlug });
              else setActiveView('customer');
            }} />
          ) : routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <ProductDetailView
                product={currentProductDetail}
                onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
              />
              <SellerStorefrontFooter variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} />
            </>
          ) : (
            <>
              <Header sticky={false} />
              <Suspense fallback={<StorePageLoading />}>
                <ProductDetailView product={currentProductDetail} onBack={backFromCentralProduct} />
              </Suspense>
              <Footer />
            </>
          )
        ) : activeView === 'catalog' ? (
          <>
            <Header />
            <Suspense fallback={<StorePageLoading />}>
              <CatalogView onBack={() => setActiveView('customer')} />
            </Suspense>
            <Footer />
          </>
        ) : activeView === 'categories_tree' ? (
          <>
            <Header sticky={false} />
            <Suspense fallback={<StorePageLoading />}>
              <CategoriesTreeView onBack={() => setActiveView('customer')} />
            </Suspense>
            <Footer />
          </>
        ) : activeView === 'customer_profile' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <CustomerProfileView key={currentUser?.id} onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})} />
              <SellerStorefrontFooter variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} />
            </>
          ) : (
            <>
              <Header sticky={false} />
              <Suspense fallback={<StorePageLoading />}>
                <CustomerProfileView key={currentUser?.id} onBack={() => setActiveView('customer')} />
              </Suspense>
              <Footer />
            </>
          )
        ) : activeView === 'customer_orders' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <CustomerOrdersView onBack={() => setActiveView('customer_profile', { storeSlug: routeStoreSlug })} />
              <SellerStorefrontFooter variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} />
            </>
          ) : (
            <>
              <Header sticky={false} />
              <Suspense fallback={<StorePageLoading />}>
                <CustomerOrdersView onBack={() => setActiveView('customer_profile')} />
              </Suspense>
              <Footer />
            </>
          )
        ) : activeView === 'customer_wishlist' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <CustomerWishlistView onBack={() => setActiveView('customer_profile', { storeSlug: routeStoreSlug })} />
              <SellerStorefrontFooter variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} />
            </>
          ) : (
            <>
              <Header sticky={false} />
              <Suspense fallback={<StorePageLoading />}>
                <CustomerWishlistView onBack={() => setActiveView('customer_profile')} />
              </Suspense>
              <Footer />
            </>
          )
        ) : activeView === 'static_page' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <StaticPageView onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})} />
              <SellerStorefrontFooter variant={currentStore?.templateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} />
            </>
          ) : (
            <>
              <Header sticky={false} />
              <Suspense fallback={<StorePageLoading />}>
                <StaticPageView onBack={() => setActiveView('customer')} />
              </Suspense>
              <Footer />
            </>
          )
        ) : activeView === 'supplier_landing' ? (
          <>
            <Header sticky={false} />
            <Suspense fallback={<StorePageLoading />}>
              <SupplierLandingView onBack={() => setActiveView('customer')} />
            </Suspense>
            <Footer />
          </>
        ) : activeView === 'dropshipper_landing' ? (
          <>
            <Header sticky={false} />
            <Suspense fallback={<StorePageLoading />}>
              <DropshipperLandingView onBack={() => setActiveView('customer')} />
            </Suspense>
            <Footer />
          </>
        ) : (
          <>
            <Header />
            <GitDeployProbe />
            <main id="main-content" tabIndex={-1} className="central-wrap central-home">
              <HeroSlider />
              <CategoriesBar />
              <SpecialDealsSlider />
              <HomeFeedSections />
            </main>
            <Footer />
          </>
        )}
        </Suspense>

        <Suspense fallback={<OverlayLoading />}>
          {selectedProduct && <ProductDetailModal />}
        </Suspense>
        <Suspense fallback={<OverlayLoading />}>
          {(isCartOpen || cartOpened) && <CartDrawer />}
        </Suspense>
        <Suspense fallback={<OverlayLoading />}>
          {isSellerModalOpen && <SellerOnboardingModal />}
        </Suspense>
        <Suspense fallback={<OverlayLoading />}>
          {isTrackingModalOpen && <OrderTrackingModal central={isCentralSite} />}
        </Suspense>
        {showStorefrontNav ? <Suspense fallback={null}><StorefrontBottomNav /></Suspense> : isCentralSite ? <BottomNav /> : null}
      </div>
    </div>
  );
};

export const App: React.FC = () => {
  if (typeof window !== 'undefined' && window.location.pathname.replace(/\/+$/, '') === '/panels') {
    return <Suspense fallback={<PanelLoading />}><PanelsPreview /></Suspense>;
  }

  return (
    <AuthProvider>
      <ProductProvider>
        <CartProvider>
          <SellerOnboardingProvider>
            <OrderProvider>
              <FinanceProvider>
                <WarehouseProvider>
                  <StoreProvider>
                    <ToastProvider>
                      <AppContent />
                    </ToastProvider>
                  </StoreProvider>
                </WarehouseProvider>
              </FinanceProvider>
            </OrderProvider>
          </SellerOnboardingProvider>
        </CartProvider>
      </ProductProvider>
    </AuthProvider>
  );
};

export default App;


