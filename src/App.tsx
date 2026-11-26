import React, { lazy, Suspense, useEffect, useRef } from 'react';
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
import { ProductDetailModal } from './components/ProductDetailModal';
import { CartDrawer } from './components/CartDrawer';
import { SellerOnboardingProvider } from './context/SellerOnboardingContext';
import { SellerOnboardingModal } from './components/SellerOnboardingModal';
import { OrderTrackingModal } from './components/OrderTrackingModal';
import { BottomNav } from './components/BottomNav';
import { StorefrontBottomNav } from './components/storefront/SellerStorefrontFooter';
import { storeApi } from './services/storeApi';
import { defaultPanelForUser, panelsForCapabilities, useAuth, type AuthenticatedPanel } from './context/AuthContext';

// Customer-Facing Priority 1 Views
import { ProductDetailView } from './components/views/ProductDetailView';
import { SellerStorefrontHeader } from './components/storefront/SellerStorefrontHeader';
import { SellerStorefrontFooter } from './components/storefront/SellerStorefrontFooter';
import { CatalogView } from './components/views/CatalogView';
import { CategoriesTreeView } from './components/views/CategoriesTreeView';
import { CustomerProfileView } from './components/views/CustomerProfileView';
import { CustomerOrdersView } from './components/views/CustomerOrdersView';
import { CustomerWishlistView } from './components/views/CustomerWishlistView';
import { StaticPageView } from './components/views/StaticPageView';
import { SupplierLandingView } from './components/views/SupplierLandingView';
import { DropshipperLandingView } from './components/views/DropshipperLandingView';

// Phase 2, 3, 4, 5, 6 & 7 Components
import { SellerStorefrontView } from './components/storefront/SellerStorefrontView';
import { NotFoundView } from './components/NotFoundView';
import { StoreUnsubscribeView } from './components/storefront/StoreUnsubscribeView';
import { SeoManager } from './seo/SeoManager';
import { AuthModal } from './components/auth/AuthModal';
import { isMockMode } from './services/apiClient';

const PanelsPreview = lazy(() => import('./components/PanelsPreview').then(module => ({ default: module.PanelsPreview })));
const SellerDashboardView = lazy(() => import('./components/seller/SellerDashboardView').then(module => ({ default: module.SellerDashboardView })));
const SupplierDashboardView = lazy(() => import('./components/supplier/SupplierDashboardView').then(module => ({ default: module.SupplierDashboardView })));
const WarehouseDashboardView = lazy(() => import('./components/warehouse/WarehouseDashboardView').then(module => ({ default: module.WarehouseDashboardView })));
const AdminDashboardView = lazy(() => import('./components/admin/AdminDashboardView').then(module => ({ default: module.AdminDashboardView })));

function PanelLoading() {
  return <div className="central-route-loading" role="status">در حال بارگذاری پنل…</div>;
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
      <AuthModal isOpen central={central} disableAutoNavigate roleTitle="ورود به حساب کاربری"
        onSuccess={() => { signedIn.current = true; }}
        onClose={() => { if (!signedIn.current) onBack(); }} />
    </main>
  );
}

export const AppContent: React.FC = () => {
  const { currentUser, isSessionReady, logoutUser, supplierSession, sellerSession, adminSession, panelSessionErrors, retryPanelSession } = useAuth();
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
    isSellerModalOpen,
    setIsSellerModalOpen,
  } = useStore();

  const customerAccountView = activeView === 'customer_profile' || activeView === 'customer_orders' || activeView === 'customer_wishlist';
  const protectedPanel = customerAccountView ? 'customer' : (Object.keys(workspaceLabels) as AuthenticatedPanel[])
    .find(panel => panel !== 'customer' && activeView === panel);
  const isWarehouseView = protectedPanel === 'warehouse_panel';
  const isOnboardingActive = isSellerModalOpen && !protectedPanel;
  const authorizedPanels = currentUser ? panelsForCapabilities(currentUser.capabilities) : [];
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

  // OTP login revalidates the session while an open wizard still owns its draft.
  if (!isSessionReady && (activeView === 'dashboard' || !isOnboardingActive)) return <div role="status">در حال بررسی نشست…</div>;
  if (routeStoreSlug && (!routeStoreResolved || routeStoreInvalid)) {
    return routeStoreInvalid
      ? <NotFoundView variant="store" onBack={() => setActiveView('seller_storefront', { storeSlug: routeStoreSlug })} />
      : <div className="central-route-loading" role="status">در حال بارگذاری ویترین…</div>;
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
    if (currentUser) return <div role="status">در حال انتقال به پنل مجاز…</div>;
    return (
      <Suspense fallback={<PanelLoading />}>
        <SellerDashboardView
          onBackToCustomerSite={() => setActiveView('customer')}
          onOpenStorefront={() => setActiveView('seller_storefront')}
        />
      </Suspense>
    );
  }
  const workspaceSwitcher = currentUser && authorizedPanels.length > 1 && (
    <nav dir="rtl" aria-label="پنل‌های مجاز" className="workspace-switcher flex flex-wrap items-center gap-1 border-b border-slate-200 bg-white px-3 py-1.5">
      {authorizedPanels.map(panel => (
        <button key={panel} type="button" aria-pressed={activeView === panel}
          onClick={() => setActiveView(panel)}
          className={`min-h-11 rounded-xl px-3 text-xs font-bold focus-visible:outline-2 focus-visible:outline-brand ${activeView === panel ? 'bg-brand-deep text-white' : 'bg-brand-soft text-brand-deep hover:bg-brand-border'}`}>
          {workspaceLabels[panel]}
        </button>
      ))}
    </nav>
  );

  if (currentUser && protectedPanel && !isWarehouseView) {
    if (!authorizedPanels.includes(protectedPanel)) {
      return (
        <main dir="rtl" className="grid min-h-dvh w-full content-center justify-items-center bg-[#f7f7fa] p-4">
          <section aria-labelledby="workspace-access-title" className="w-full max-w-md space-y-4 rounded-2xl border border-slate-200 bg-white p-5">
            <h1 id="workspace-access-title" className="text-base font-extrabold text-slate-900">حساب فعلی مجوز پنل {workspaceLabels[protectedPanel]} را ندارد.</h1>
            <div className="flex flex-wrap gap-2">
              <button type="button" onClick={() => setActiveView(defaultPanel)}
                className="min-h-11 rounded-xl bg-brand-deep px-3 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand">
                {defaultPanel === 'customer' ? 'بازگشت به سایت' : `باز کردن پنل ${workspaceLabels[defaultPanel]}`}
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
      return <div className="flex min-h-screen flex-col items-center justify-center gap-4 p-4" dir="rtl">
        {panelSessionError ? <><p role="alert">{panelSessionError}</p><button type="button" className="min-h-11 rounded-xl bg-brand-soft px-4 font-bold text-brand-deep" onClick={retryPanelSession}>تلاش مجدد</button></>
          : <p role="status">در حال آماده‌سازی پنل…</p>}
      </div>;
    }
  }

  if (activeView === 'admin_panel') {
    return (
      <>
        {workspaceSwitcher}
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
        {workspaceSwitcher}
        {/* Developer & Multi-Role Navigation Bar */}
        <RoleSwitcherBar />

        {/* View Switching based on active role & subpages */}
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
            <div role="status" className="flex-1 flex items-center justify-center p-6 text-xs text-slate-500">در حال بارگذاری ویترین فروشگاه…</div>
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
              <div role="status" className="flex-1 flex items-center justify-center p-6 text-sm text-slate-600">در حال بارگذاری کالا…</div>
            ) : (
              <>
                <Header sticky={false} mainId="main-content" />
                <main id="main-content" tabIndex={-1} className="central-wrap central-product-loading">
                  <p role="status">در حال بارگذاری کالا…</p>
                </main>
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
              <Header sticky={false} mainId="main-content" />
              <ProductDetailView product={currentProductDetail} onBack={backFromCentralProduct} />
              <Footer />
            </>
          )
        ) : activeView === 'catalog' ? (
          <>
            <Header mainId="main-content" />
            <CatalogView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : activeView === 'categories_tree' ? (
          <>
            <Header sticky={false} mainId="main-content" />
            <CategoriesTreeView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : activeView === 'customer_profile' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <CustomerProfileView onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})} />
              <SellerStorefrontFooter />
            </>
          ) : (
            <>
              <Header sticky={false} mainId="main-content" />
              <CustomerProfileView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'customer_orders' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <CustomerOrdersView onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})} />
              <SellerStorefrontFooter />
            </>
          ) : (
            <>
              <Header sticky={false} mainId="main-content" />
              <CustomerOrdersView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'customer_wishlist' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <CustomerWishlistView onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})} />
              <SellerStorefrontFooter />
            </>
          ) : (
            <>
              <Header sticky={false} mainId="main-content" />
              <CustomerWishlistView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'static_page' ? (
          routeStoreSlug ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <StaticPageView onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})} />
              <SellerStorefrontFooter />
            </>
          ) : (
            <>
              <Header sticky={false} mainId="main-content" />
              <StaticPageView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'supplier_landing' ? (
          <>
            <Header sticky={false} mainId="main-content" />
            <SupplierLandingView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : activeView === 'dropshipper_landing' ? (
          <>
            <Header sticky={false} mainId="main-content" />
            <DropshipperLandingView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : (
          <>
            <Header mainId="main-content" />
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

        {/* Global Modals & Drawers */}
        <ProductDetailModal />
        <CartDrawer />
        <SellerOnboardingModal />
        <OrderTrackingModal central={isCentralSite} />
        {showStorefrontNav ? <StorefrontBottomNav /> : isCentralSite ? <BottomNav /> : null}
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


