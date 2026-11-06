import React, { useEffect, useRef } from 'react';
import { PanelsPreview } from './components/PanelsPreview';
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
import { SpecialDealsSlider } from './components/SpecialDealsSlider';
import { HomeFeedSections } from './components/HomeFeedSections';
import { Footer } from './components/Footer';
import { ProductDetailModal } from './components/ProductDetailModal';
import { CartDrawer } from './components/CartDrawer';
import { SellerOnboardingModal } from './components/SellerOnboardingModal';
import { OrderTrackingModal } from './components/OrderTrackingModal';
import { AutoClawConnectPopup } from './components/common/AutoClawConnectPopup';
import { BottomNav } from './components/BottomNav';
import { StorefrontBottomNav } from './components/storefront/SellerStorefrontFooter';
import { storeApi } from './services/storeApi';
import { panelsForCapabilities, useAuth } from './context/AuthContext';

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
import { SellerDashboardView } from './components/seller/SellerDashboardView';
import { SellerStorefrontView } from './components/storefront/SellerStorefrontView';
import { NotFoundView } from './components/NotFoundView';
import { StoreUnsubscribeView } from './components/storefront/StoreUnsubscribeView';
import { SeoManager } from './seo/SeoManager';
import { SupplierDashboardView } from './components/supplier/SupplierDashboardView';
import { WarehouseDashboardView } from './components/warehouse/WarehouseDashboardView';
import { AdminDashboardView } from './components/admin/AdminDashboardView';

export const AppContent: React.FC = () => {
  const { currentUser, isSessionReady, panelForRole, supplierSession, sellerSession, warehouseSession, adminSession, panelSessionError, retryPanelSession } = useAuth();
  const {
    activeView,
    setActiveView,
    currentProductDetail,
    currentStore,
    products,
    openProductDetail,
    storefrontSlug,
    storefrontLoading,
    storefrontInvalid,
    storefrontSuspended,
    storefrontUnsubscribeSlug,
    routeProductPending,
    routeProductInvalid,
    routeStoreSlug,
    openStorefront,
  } = useStore();

  const routedIdentity = useRef<string | null>(null);
  useEffect(() => {
    if (!isSessionReady || !currentUser) return;
    const requiredView = panelForRole(currentUser.role);
    const authorizedViews = panelsForCapabilities(currentUser.capabilities?.length ? currentUser.capabilities : [currentUser.role]);
    const identity = `${currentUser.id}:${currentUser.role}:${(currentUser.capabilities || []).join(',')}`;
    const protectedViews = ['seller_dashboard', 'supplier_dashboard', 'warehouse_panel', 'admin_panel'];
    const identityChanged = routedIdentity.current !== identity;
    const activeViewAuthorized = authorizedViews.includes(activeView as any);
    if (identityChanged || (protectedViews.includes(activeView) && !activeViewAuthorized)) {
      routedIdentity.current = identity;
      if (identityChanged && activeViewAuthorized) return;
      setActiveView(identityChanged ? requiredView : (authorizedViews[0] || requiredView));
    }
  }, [activeView, currentUser, isSessionReady, panelForRole, setActiveView]);

  // ISO-A (model a, picked-only showcase): central-home/under-own-store
  // frame guard. The central `customer` view must never render under a
  // dropshipper's own session store: banner/header would brand it with
  // the store while hero + grid show the central catalogue, and any
  // order placed from that frame would be attributed to the dropshipper
  // store and fail published-only validation. This fires on every entry
  // into that state -- post-login, session restore, workspace-switcher
  // `customer`, and dashboard back-to-site -- not just on login, so the
  // frame cannot recur through navigation. It is referral-safe: it
  // requires an active seller session whose storeId matches the current
  // store, so anonymous referral frames (central store) and admin-only
  // sessions are untouched, and the header logo (which sets the central
  // store first) never trips it. Back-button caveat: returning to `/`
  // via history while the own store is current re-enters the frame and
  // bounces forward again, because central-home-with-own-store is not
  // a legitimate state under model (a).
  const ownStorefrontRedirectRef = useRef<string | null>(null);
  useEffect(() => {
    const ownStoreId = sellerSession?.storeId;
    if (
      activeView !== 'customer' ||
      sellerSession?.status !== 'active' ||
      !ownStoreId ||
      currentStore?.ownerType !== 'dropshipper' ||
      currentStore.id !== ownStoreId ||
      !currentStore.slug
    ) {
      ownStorefrontRedirectRef.current = null;
      return;
    }
    if (ownStorefrontRedirectRef.current === ownStoreId) return;
    ownStorefrontRedirectRef.current = ownStoreId;
    openStorefront(currentStore.slug);
  }, [activeView, sellerSession, currentStore, openStorefront]);

  const adProductOpened = useRef(false);
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    if (adProductOpened.current || !params.has('ad_token')) return;
    const campaignId = params.get('campaignId');
    if (campaignId) storeApi.recordSellerAdEvent(campaignId, 'click', params.get('ad_token') || '', params.get('productId') || undefined).catch(() => undefined);
    const product = products.find((item) => item.id === params.get('productId'));
    if (!product) return;
    adProductOpened.current = true;
    openProductDetail(product);
    // The ad click landed from a store's campaign: the PDP opens in
    // THAT store's scope (its prices, its frame, its store-carrying
    // URL), never the central store's. The landing store is the
    // storefront the link opened on, else the store being browsed.
    // The central slug is not a scope (scopes are dropshipper slugs
    // or none), so a central landing keeps the unscoped open.
    const landingSlug = storefrontSlug ?? currentStore?.slug ?? null;
    setActiveView('product_detail', {
      productId: product.id,
      storeSlug: landingSlug && landingSlug !== 'central' ? landingSlug : undefined,
    });
  }, [products, openProductDetail, setActiveView, storefrontSlug, currentStore]);

  if (!isSessionReady) return <div role="status">در حال بررسی نشست…</div>;
  if (currentUser) {
    const panel = panelForRole(currentUser.role);
    const ready = panel === 'customer' ||
      (panel === 'supplier_dashboard' && supplierSession) ||
      (panel === 'seller_dashboard' && sellerSession) ||
      (panel === 'warehouse_panel' && warehouseSession) ||
      (panel === 'admin_panel' && adminSession);
    if (!ready) {
      return <div className="min-h-screen flex flex-col items-center justify-center gap-4" dir="rtl">
        {panelSessionError ? <><p role="alert">{panelSessionError}</p><button onClick={retryPanelSession}>تلاش مجدد</button></> : <p role="status">در حال آماده‌سازی پنل…</p>}
      </div>;
    }
  }
  const authorizedPanels = currentUser ? panelsForCapabilities(currentUser.capabilities) : [];
  if (currentUser && ['seller_dashboard', 'supplier_dashboard', 'warehouse_panel', 'admin_panel'].includes(activeView) && !authorizedPanels.includes(activeView as any)) {
    return <div role="status">در حال انتقال به پنل مجاز…</div>;
  }

  // Part 1: Standalone Isolated Admin & Finance Portal (/admin) outside the 504px customer mobile shell
  if (activeView === 'admin_panel') {
    return (
      <AdminDashboardView
        onBackToCustomerSite={() => setActiveView('customer')}
      />
    );
  }

  // Team 08 (D74): desktop storefront variants widen the shell on lg
  // screens; every other view keeps the 504px mobile frame.
  // Homepage-layout fix: the central homepage (the default branch below,
  // i.e. every view that is NOT one of the listed views) must render
  // full-width on desktop like the live Nov 2 desktop-merged homepage,
  // not inside the 504px mobile frame. All listed views keep their frame.
  const isStorefront = activeView === 'seller_storefront';
  // FIX 16: live's shell for static pages is full-width on desktop
  // (w-full max-w-[504px] lg:max-w-none, like every non-storefront
  // view in live's App shell) with the site header + footer around
  // the page; the static body itself already caps at lg:max-w-4xl
  // xl:max-w-5xl centred (Fix 15). The old lg:max-w-4xl shell capped
  // the whole page and dropped the site chrome -- the re-check's
  // narrower-column, no-header/footer difference.
  const isStaticPage = activeView === 'static_page';
  // FIX 18: live's App shell renders site header + footer for every view
  // except dashboards, storefront, suspended and admin (live bundle App
  // switch: E5/Z5 guarded by that exact exclusion list). These views also
  // get live's full-width desktop shell (lg:max-w-none) like static_page.
  // FIX 19: seller_dashboard added: live seller gate renders full-width
  // (browser-observed), but the candidate shell capped it at 504px, making
  // the gate a narrow centered column. The gate component itself matches
  // live; the cap came from this shell expression, not the gate.
  const isWideChrome = ['seller_dashboard', 'supplier_dashboard', 'categories_tree', 'customer_profile', 'customer_orders', 'customer_wishlist', 'supplier_landing', 'dropshipper_landing'].includes(activeView as string);
  const nonHomepageViews = ['seller_dashboard', 'seller_storefront', 'supplier_dashboard', 'warehouse_panel', 'product_detail', 'categories_tree', 'customer_profile', 'customer_orders', 'customer_wishlist', 'static_page', 'supplier_landing', 'dropshipper_landing'];
  const isHomepage = !nonHomepageViews.includes(activeView as string);
  // FIX 8: the live standalone product page renders full-width on desktop
  // (live browser evidence 2026-11-03: product page full-width at ~1920px,
  // while live storefronts stay framed). This corrects FIX 7, which kept
  // product_detail in the 504px frame on bundle-count inference alone.
  const isProductDetail = activeView === 'product_detail';
  return (
    <div className="min-h-screen bg-[#edf2f7] flex justify-center text-slate-800 antialiased font-sans select-none">
      <SeoManager />
      {/*
        Exact Snapp Pay (pl.snapp.ir) Container:
        max-w-[504px] centered mobile/PWA frame on desktop, 100% full width on mobile
      */}
      {/* FIX 12 (superseded Nov 5, 2026 by owner order): the storefront shell
          was lg:max-w-7xl, leaving pale side voids on wide desktops — the
          owner rejected the framed column; storefronts now render
          full-width on desktop like the homepage/static views. */}
      <div className={(isHomepage ? 'homepage-shell w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : isProductDetail ? 'w-full bg-white min-h-screen relative flex flex-col pb-20 lg:pb-0' : `w-full bg-white min-h-screen shadow-2xl relative flex flex-col border-x border-slate-200/80 pb-20 ${isStorefront ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isStaticPage ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : isWideChrome ? 'max-w-[504px] lg:max-w-none lg:shadow-none lg:border-x-0 lg:pb-0' : 'max-w-[504px]'}`)}>
        {currentUser && authorizedPanels.length > 1 && (
          <nav aria-label="Workspace switcher" className="flex items-center gap-1 overflow-x-auto border-b border-slate-200 bg-white px-3 py-2 no-scrollbar">
            {authorizedPanels.map((panel) => {
              const labels: Record<string, string> = {
                customer: 'فروشگاه',
                seller_dashboard: 'فروشنده',
                supplier_dashboard: 'تأمین‌کننده',
                warehouse_panel: 'انبار',
                admin_panel: 'مدیریت',
              };
              return (
                <button
                  key={panel}
                  type="button"
                  aria-pressed={activeView === panel}
                  onClick={() => setActiveView(panel)}
                  className={`shrink-0 rounded-lg px-3 py-1.5 text-[11px] font-bold ${activeView === panel ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-600'}`}
                >
                  {labels[panel]}
                </button>
              );
            })}
          </nav>
        )}
        {/* Developer & Multi-Role Navigation Bar */}
        <RoleSwitcherBar />

        {/* View Switching based on active role & subpages */}
        {activeView === 'seller_dashboard' ? (
          <SellerDashboardView
            onBackToCustomerSite={() => setActiveView('customer')}
            onOpenStorefront={() => setActiveView('seller_storefront')}
          />
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
          <SupplierDashboardView
            onBackToCustomerSite={() => setActiveView('customer')}
          />
        ) : activeView === 'warehouse_panel' ? (
          <WarehouseDashboardView
            onBackToCustomerSite={() => setActiveView('customer')}
          />
        ) : activeView === 'product_detail' ? (
          routeProductPending ? (
            <div role="status" className="flex-1 flex items-center justify-center p-6 text-xs text-slate-500">در حال بارگذاری کالا…</div>
          ) : routeProductInvalid || !currentProductDetail ? (
            <NotFoundView onBack={() => setActiveView('customer')} />
          ) : routeStoreSlug || currentStore?.ownerType === 'dropshipper' ? (
            <>
              <SellerStorefrontHeader
                onOpenSearch={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
                variant={currentStore?.templateId === 'tpl-digikala-1' ? 'digikala' : 'modern'}
              />
              <ProductDetailView
                product={currentProductDetail}
                onBack={() => setActiveView('seller_storefront', routeStoreSlug ? { storeSlug: routeStoreSlug } : {})}
              />
              <SellerStorefrontFooter />
            </>
          ) : (
            <>
              <Header sticky={false} />
              <ProductDetailView
                product={currentProductDetail}
                onBack={() => setActiveView('customer')}
              />
              <Footer />
            </>
          )
        ) : activeView === 'catalog' ? (
          <>
            <Header sticky={false} />
            <CatalogView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : activeView === 'categories_tree' ? (
          <>
            <Header sticky={false} />
            <CategoriesTreeView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : activeView === 'customer_profile' ? (
          routeStoreSlug || currentStore?.ownerType === 'dropshipper' ? (
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
              <Header sticky={false} />
              <CustomerProfileView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'customer_orders' ? (
          routeStoreSlug || currentStore?.ownerType === 'dropshipper' ? (
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
              <Header sticky={false} />
              <CustomerOrdersView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'customer_wishlist' ? (
          routeStoreSlug || currentStore?.ownerType === 'dropshipper' ? (
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
              <Header sticky={false} />
              <CustomerWishlistView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'static_page' ? (
          routeStoreSlug || currentStore?.ownerType === 'dropshipper' ? (
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
              <Header sticky={false} />
              <StaticPageView onBack={() => setActiveView('customer')} />
              <Footer />
            </>
          )
        ) : activeView === 'supplier_landing' ? (
          <>
            <Header sticky={false} />
            <SupplierLandingView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : activeView === 'dropshipper_landing' ? (
          <>
            <Header sticky={false} />
            <DropshipperLandingView onBack={() => setActiveView('customer')} />
            <Footer />
          </>
        ) : (
          <>
            <Header />
            {/* FIX 16: hero lives OUTSIDE the gutter-padded main (live
                banner-reorder model: full-width child, no negative
                margins). The wrapper preserves the exact vertical gaps
                the hero had inside main: top = main pt (0.5/4) and
                bottom = hero pb + wrapper pb + main pt = 12px mobile
                (4+6+2) and 24px lg (8+0+16), as before. */}
            <div className="pt-0.5 pb-1.5 lg:pt-4 lg:pb-0">
              <HeroSlider />
            </div>
            <main className="homepage-main flex-1 space-y-2 pt-0.5 lg:space-y-4 lg:py-4">
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
        <OrderTrackingModal />
        {/* AutoClaw connectivity test popup: central homepage only. */}
        {isHomepage && <AutoClawConnectPopup />}
        {activeView === 'seller_storefront' || (activeView === 'product_detail' && (routeStoreSlug || currentStore?.ownerType === 'dropshipper')) ? <StorefrontBottomNav /> : isHomepage ? <div className="lg:hidden"><BottomNav /></div> : <BottomNav />}
      </div>
    </div>
  );
};

export const App: React.FC = () => {
  if (typeof window !== 'undefined' && window.location.pathname.replace(/\/+$/, '') === '/panels') {
    return <PanelsPreview />;
  }

  return (
    <AuthProvider>
      <ProductProvider>
        <CartProvider>
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
        </CartProvider>
      </ProductProvider>
    </AuthProvider>
  );
};

export default App;


