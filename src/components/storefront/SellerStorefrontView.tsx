import React, { useEffect, useRef, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { mapBackendProduct } from '../../context/ProductContext';
import { SellerStorefrontHeader } from './SellerStorefrontHeader';
import { SellerStorefrontHero } from './SellerStorefrontHero';
import { SellerStorefrontCategories } from './SellerStorefrontCategories';
import { SellerStorefrontProductGrid } from './SellerStorefrontProductGrid';
import { SellerStorefrontAbout } from './SellerStorefrontAbout';
import { SellerStorefrontContact } from './SellerStorefrontContact';
import { SellerStorefrontFooter } from './SellerStorefrontFooter';
import { ProductCard } from '../ProductCard';
import { SpecialDealsSlider } from '../SpecialDealsSlider';
import { Search, Home, Info, PhoneCall, PackageSearch, Package, X } from 'lucide-react';
import { storeApi } from '../../services/storeApi';
import { catalogApi } from '../../services/catalogApi';
import { StorefrontTemplate, TemplateFallback, isKnownTemplateId, resolveTemplateAccent } from './templates/TemplateRegistry';
import { storefrontProducts } from './templates/templateTypes';
import type { TemplateConfig } from './templates/templateTypes';

interface Props {
  onBackToCustomerSite?: () => void;
}

// crypto.randomUUID exists only in secure contexts (HTTPS/localhost); the live and preview origins are plain HTTP, so fall back instead of throwing. Uniqueness is all the call sites need.
const newUuid = (): string => {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch { /* fall through to the fallback */ }
  return 'id-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2);
};

export const SellerStorefrontView: React.FC<Props> = ({ onBackToCustomerSite }) => {
  const { currentStore, products: centralProducts, categories, searchQuery, setSearchQuery, setIsTrackingModalOpen, sellerSession } = useStore();

  // Fix74: storefront grid must show STORE prices, not central prices.
  // ProductContext's catalogue is unscoped (central margin), while the PDP
  // and the order path resolve store-scoped prices — grid 2,178,000 vs PDP
  // 2,277,000 for the same speaker. For a dropshipper storefront, fetch the
  // store-scoped list (same products, priced at the store's margin) and use
  // it for every grid/template/widget in this view. Central storefronts keep
  // the context list. Priced surfaces never fall back to the central list:
  // while the scoped fetch is in flight or after it fails they receive an
  // empty list instead (see templateProducts / fallbackProducts below).
  const [scopedProducts, setScopedProducts] = useState<ReturnType<typeof mapBackendProduct>[] | null>(null);
  const [scopedCursor, setScopedCursor] = useState<string | null>(null);
  const [hasMoreScoped, setHasMoreScoped] = useState(false);
  const [scopedLoadingMore, setScopedLoadingMore] = useState(false);
  const [loadedStoreSlug, setLoadedStoreSlug] = useState<string | null>(null);
  const [scopedError, setScopedError] = useState('');
  const [scopedReload, setScopedReload] = useState(0);
  const feedGeneration = useRef(0);
  const liveStoreSlug = useRef(currentStore?.slug);
  liveStoreSlug.current = currentStore?.slug;
  // PERF-01: the storefront loads page 1 of its own assortment (store
  // prices) and pages on demand from the floor grid below; template
  // sections render from the loaded pages (same documented limitation
  // as the central catalog's facets).
  useEffect(() => {
    const generation = ++feedGeneration.current;
    setScopedError('');
    setLoadedStoreSlug(null);
    setScopedLoadingMore(false);
    if (currentStore?.ownerType !== 'dropshipper' || !currentStore.slug) {
      setScopedProducts(null);
      setScopedCursor(null);
      setHasMoreScoped(false);
      return;
    }
    const slug = currentStore.slug;
    const currentFeed = () => feedGeneration.current === generation && liveStoreSlug.current === slug;
    catalogApi.getProductsPage({ storeSlug: slug })
      .then(page => {
        if (!currentFeed()) return;
        setScopedProducts(page.items.map(mapBackendProduct));
        setScopedCursor(page.nextCursor);
        setHasMoreScoped(page.hasMore);
        setLoadedStoreSlug(slug);
      })
      .catch(() => {
        if (!currentFeed()) return;
        setScopedProducts(null);
        setHasMoreScoped(false);
        setScopedError('دریافت کالاهای ویترین انجام نشد.');
        setLoadedStoreSlug(slug);
      });
    return () => { feedGeneration.current += 1; };
  }, [currentStore?.ownerType, currentStore?.slug, scopedReload]);
  const loadMoreScoped = async () => {
    if (!currentStore?.slug || loadedStoreSlug !== currentStore.slug || !hasMoreScoped || !scopedCursor || scopedLoadingMore) return;
    const slug = currentStore.slug;
    const generation = feedGeneration.current;
    const currentFeed = () => generation === feedGeneration.current && liveStoreSlug.current === slug;
    setScopedError('');
    setScopedLoadingMore(true);
    try {
      const page = await catalogApi.getProductsPage({ storeSlug: slug }, { cursor: scopedCursor });
      if (!currentFeed()) return;
      setScopedProducts((prev) => {
        const base = prev ?? [];
        const seen = new Set(base.map((p) => p.id));
        const fresh = page.items.map(mapBackendProduct).filter((p) => !seen.has(p.id));
        return [...base, ...fresh];
      });
      setScopedCursor(page.nextCursor);
      setHasMoreScoped(page.hasMore);
    } catch {
      if (currentFeed()) setScopedError('دریافت کالاهای بیشتر انجام نشد.');
    } finally {
      if (currentFeed()) setScopedLoadingMore(false);
    }
  };
  const products = scopedProducts ?? centralProducts;
  const [templateId, setTemplateId] = useState<string | null>(null);
  // ISO-E: the store this templateId was resolved FOR. A plain
  // templateId state survives SPA navigation, so store B's first
  // paint would otherwise render store A's template/accent until
  // the effect re-ran. Rendering only uses templateId when its
  // owner is the current store (see resolvedTemplateId below).
  const [templateStoreId, setTemplateStoreId] = useState<string | null>(null);
  const [templateConfig, setTemplateConfig] = useState<TemplateConfig>({});
  // FIX3 (step-zero decision): template resolution. The public store
  // response now carries the store's templateId, templateConfig
  // (presentation keys only) and logo (see team05PublicStoreView):
  // live's own storefront fetches the store by slug with NO auth and
  // renders H4[templateId] ?? Modern, and the template catalogue is
  // public, so the configured template is public presentation, not
  // owner-private data. A public visitor therefore sees the store's
  // configured template (no more silent Modern fallback for public).
  // The authenticated seller template API remains only as a fallback
  // for the owner's own preview when the public response has no
  // templateId yet. Absent templateId still means tpl-modern-1, the
  // live default (jv). An unknown templateId renders the status page
  // (TemplateFallback), never a silent fallback to another template.
  useEffect(() => {
    // ISO-E: owner-track every resolution, and cancel the in-flight
    // seller-template fetch when the store changes, so a slow
    // response for store A can never install A's template on B.
    const storeId = currentStore.id;
    if (currentStore.templateId) {
      setTemplateId(currentStore.templateId);
      setTemplateStoreId(storeId);
      setTemplateConfig((currentStore.templateConfig || {}) as TemplateConfig);
      return;
    }
    if (sellerSession?.storeId === currentStore.id && currentStore.slug) {
      let cancelled = false;
      storeApi.getSellerStoreTemplate(currentStore.slug)
        .then((res) => {
          if (cancelled) return;
          setTemplateId(res.templateId || 'tpl-modern-1');
          setTemplateStoreId(storeId);
          setTemplateConfig((res.templateConfig || {}) as TemplateConfig);
        })
        .catch(() => { if (!cancelled) { setTemplateId('tpl-modern-1'); setTemplateStoreId(storeId); } });
      return () => { cancelled = true; };
    }
    setTemplateId('tpl-modern-1');
    setTemplateStoreId(storeId);
  }, [currentStore.id, currentStore.slug, currentStore.templateId, sellerSession?.storeId]);
  const [activeTab, setActiveTab] = useState<'home' | 'about' | 'contact'>('home');
  // The storefront footer's «درباره ما» points at the store's OWN
  // about: template layouts render it as a page section and the
  // footer scrolls to it directly; this fallback layout keeps the
  // about behind the tab above, so the footer dispatches this event
  // and the view selects the tab itself.
  useEffect(() => {
    const showAbout = () => {
      setActiveTab('about');
      window.scrollTo({ top: 0, behavior: 'smooth' });
    };
    window.addEventListener('selersho:storefront-about', showAbout);
    return () => window.removeEventListener('selersho:storefront-about', showAbout);
  }, []);
  const [isSearchOpen, setIsSearchOpen] = useState(false);
  const [publicWidget, setPublicWidget] = useState<{ id: string; name: string; productIds: string[] } | null>(null);
  // Owner preview resolves builderConfig.widgets from the panel
  // session; real visitors carry no builderConfig, so fall back to
  // the public DTO's widgets-only subset (builderWidgets) before
  // defaulting every toggle to on.
  const builderWidgets = currentStore.builderConfig?.widgets || currentStore.builderWidgets || {
    heroBanner: true,
    dealsSlider: true,
    categoriesGrid: true,
    storeDealsWidget: true,
    trustBar: true,
    contactInfo: true,
  };
  useEffect(() => {
    // ISO-E: clear the previous store's widget FIRST (it used to
    // linger on store B until B's fetch resolved, and forever when
    // B's fetch failed), and ignore a stale response for store A.
    setPublicWidget(null);
    if (!currentStore.slug || !builderWidgets.storeDealsWidget) return;
    let cancelled = false;
    storeApi.getPublicSellerCampaigns(currentStore.slug).then((campaigns) => {
      if (cancelled) return;
      const widget = campaigns.find((campaign) => campaign.type === 'widget');
      if (widget) { setPublicWidget({ id: widget.id, name: widget.name, productIds: widget.productIds }); storeApi.recordSellerAdEvent(widget.id, 'impression', newUuid()).catch(() => undefined); }
    }).catch(() => undefined);
    return () => { cancelled = true; };
  }, [currentStore.slug, builderWidgets.storeDealsWidget]);

  // Storefront Guard: If store doesn't exist or is invalid
  if (!currentStore || !currentStore.id) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center p-6 text-center bg-slate-50 min-h-[480px] space-y-3.5">
        <div className="w-16 h-16 rounded-2xl bg-rose-100 text-rose-600 flex items-center justify-center shadow-xs">
          <PackageSearch className="w-8 h-8"  strokeWidth={1.5}/>
        </div>
        <div>
          <h2 className="text-sm font-bold text-slate-900">ویترین فروشگاه در دسترس نیست</h2>
          <p className="text-xs text-slate-500 mt-1 max-w-[280px]">
            نشانی فروشگاه نامعتبر است یا در حال حاضر توسط مدیریت فروشگاه موقتاً غیرفعال شده است. لطفاً بعداً مراجعه فرمایید.
          </p>
        </div>
      </div>
    );
  }

  // Fix74 (completed): the template renderer and the fallback
  // public-widget section are as strict as the fallback grid/slider
  // below: on a dropshipper storefront they receive the store-scoped
  // list only - empty while it loads or after it failed - never the
  // central-priced context catalogue, whose prices checkout would
  // contradict. Central storefronts keep `products` (their context
  // catalogue) unchanged. With an empty list the template branch
  // takes its designed empty state (Fix81: template chrome plus the
  // coming-soon block), not an error page.
  const templateProducts =
    currentStore.ownerType === 'dropshipper'
      ? loadedStoreSlug === currentStore.slug ? scopedProducts ?? [] : []
      : products;

  // FIX3: when a template is resolved, the template component
  // renders the storefront (with the existing about/contact/footer
  // sections below it, placement unchanged). An unknown templateId
  // renders the status page, not another template and not the builder
  // layout. The source builder layout below is the fallback only while
  // no template id has been resolved at all (loading state).
  // ISO-E: only a template resolved for THIS store may render; any
  // other value means "not resolved yet" (builder loading layout).
  const resolvedTemplateId = templateStoreId === currentStore.id ? templateId : null;
  if (resolvedTemplateId) {
    const items = storefrontProducts(currentStore, templateProducts);
    const searchOverlay = isSearchOpen ? (
      <div className="bg-slate-50 border-b border-slate-200 p-3 flex items-center gap-2 animate-in fade-in duration-200">
        <div className="relative flex-1 flex items-center bg-white border border-slate-200 rounded-xl px-2.5 py-1.5 shadow-2xs">
          <Search className="w-4 h-4 text-slate-400 shrink-0 ml-1.5"  strokeWidth={1.5}/>
          <input
            type="text"
            autoFocus
            placeholder="جستجوی محصول در این فروشگاه..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
          />
        </div>
        <button
          onClick={() => {
            setIsSearchOpen(false);
            setSearchQuery('');
          }}
          className="p-1.5 rounded-xl hover:bg-slate-200 text-slate-500"
        >
          <X className="w-5 h-5"  strokeWidth={1.5}/>
        </button>
      </div>
    ) : null;
    // Fix81: an empty storefront must still render its resolved
    // template. The old generic empty page (default Modern header +
    // generic hero) made every template pick a visible no-op for
    // stores with no catalogue products yet (live mone: 11 picks, 0
    // active). Section components return null when empty, so the
    // template renders its own hero/chrome; the coming-soon message
    // below is kept so visitors still get the explanation.
    if (items.length === 0 && resolvedTemplateId !== 'tpl-fashion-1') {
      return (
        <div className="flex-1 flex flex-col bg-white">
          <SellerStorefrontHeader
            onOpenSearch={() => setIsSearchOpen(true)}
            variant={resolvedTemplateId === 'tpl-digikala-1' ? 'digikala' : resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : 'modern'}
            navigationProducts={templateProducts}
            searchQuery={searchQuery}
            onSearchChange={setSearchQuery}
          />
          {searchOverlay}
          {isKnownTemplateId(resolvedTemplateId) ? (
            <StorefrontTemplate
              templateId={resolvedTemplateId}
              store={currentStore}
              products={[]}
              categories={categories}
              accentColor={resolveTemplateAccent(resolvedTemplateId, templateConfig.accentColor)}
              config={templateConfig}
            />
          ) : (
            <TemplateFallback templateId={resolvedTemplateId} />
          )}
          {/* FIX 12: live empty state — heading "به‌زودی محصولات جدید
              می‌رسند" plus the coming-soon paragraph. The earlier
              "صاحب فروشگاه هنوز محصولی را فعال نکرده است…" wording
              appears nowhere in the live bundle and was a misquote;
              the live bundle default is the paragraph below. The
              generic welcome heading lives in the hero above. */}
          {/* FIX 16: live's template empty block (bundle: icon circle +
              h3 text-lg + text-sm stone body). The candidate's h2
              text-sm block had the same texts but the wrong heading
              level/size and no icon -- the re-check's heading-level
              difference. Accent is live's teal default: live strips
              templateConfig, so all observed stores render it. */}
          <div id={resolvedTemplateId === 'tpl-fashion-1' ? 'products' : undefined} className="flex flex-col items-center justify-center px-8 py-20 text-center">
            <div className="w-20 h-20 rounded-full flex items-center justify-center mb-4" style={{ backgroundColor: '#0D737714' }}>
              <Package className="w-9 h-9" strokeWidth={1.5} style={{ color: '#0D7377' }} />
            </div>
            <h3 className="text-lg font-black text-[#1C1917]">به‌زودی محصولات جدید می‌رسند</h3>
            <p className="text-sm text-stone-500 mt-2 leading-6 max-w-[280px]">صاحب فروشگاه به‌زودی کالاهای تازه را به این ویترین اضافه می‌کند. کمی بعد دوباره سر بزنید.</p>
          </div>
          <SellerStorefrontFooter variant={resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} storeProducts={templateProducts} />
        </div>
      );
    }
    return (
      <div className="flex-1 flex flex-col bg-white">
        {/* FIX 13: per-template header — Digikala's own header (2 buttons
            + always-visible search) only in the template path; the
            empty path above always uses the Modern header. */}
        <SellerStorefrontHeader
          onOpenSearch={() => setIsSearchOpen(true)}
          variant={resolvedTemplateId === 'tpl-digikala-1' ? 'digikala' : resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : 'modern'}
            navigationProducts={templateProducts}
          searchQuery={searchQuery}
          onSearchChange={setSearchQuery}
        />
        {searchOverlay}
        {isKnownTemplateId(resolvedTemplateId) ? (
          <StorefrontTemplate
            templateId={resolvedTemplateId}
            store={currentStore}
            products={templateProducts}
            categories={categories}
            accentColor={resolveTemplateAccent(resolvedTemplateId, templateConfig.accentColor)}
            config={templateConfig}
            productFeed={resolvedTemplateId === 'tpl-fashion-1' && currentStore.ownerType === 'dropshipper' ? {
              loading: loadedStoreSlug !== currentStore.slug,
              error: loadedStoreSlug === currentStore.slug ? scopedError : '',
              hasMore: loadedStoreSlug === currentStore.slug && hasMoreScoped,
              loadingMore: scopedLoadingMore,
              loadMore: () => void loadMoreScoped(),
              retry: () => { if (hasMoreScoped) void loadMoreScoped(); else setScopedReload(n => n + 1); },
            } : undefined}
          />
        ) : (
          <TemplateFallback templateId={resolvedTemplateId} />
        )}
        <SellerStorefrontFooter variant={resolvedTemplateId === 'tpl-fashion-1' ? 'fashion' : 'modern'} storeProducts={templateProducts} />
      </div>
    );
  }

  // Fallback (builder) layout surfaces: the deals slider and the
  // legacy grid used to read the unscoped context catalogue directly,
  // showing central prices while the order path charges store
  // prices. On a dropshipper storefront they now receive the
  // store-scoped list; while that fetch is in flight (or has failed)
  // they receive an empty list - their existing empty states - and
  // never central-priced cards. Central storefronts keep the view's
  // list, which for them is the context catalogue, unchanged.
  const fallbackProducts =
    currentStore.ownerType === 'dropshipper' ? scopedProducts ?? [] : products;

  return (
    <div className="flex-1 flex flex-col bg-white">
      {/* 1. Branded Header */}
      <SellerStorefrontHeader onOpenSearch={() => setIsSearchOpen(true)} />

      {/* Search Overlay Drawer */}
      {isSearchOpen && (
        <div className="bg-slate-50 border-b border-slate-200 p-3 flex items-center gap-2 animate-in fade-in duration-200">
          <div className="relative flex-1 flex items-center bg-white border border-slate-200 rounded-xl px-2.5 py-1.5 shadow-2xs">
            <Search className="w-4 h-4 text-slate-400 shrink-0 ml-1.5"  strokeWidth={1.5}/>
            <input
              type="text"
              autoFocus
              placeholder="جستجوی محصول در این فروشگاه..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="w-full text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
            />
          </div>
          <button
            onClick={() => {
              setIsSearchOpen(false);
              setSearchQuery('');
            }}
            className="p-1.5 rounded-xl hover:bg-slate-200 text-slate-500"
          >
            <X className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>
      )}

      {/* 2. Secondary Navigation Tabs */}
      <div className="bg-slate-50 border-b border-slate-200/80 px-3 py-1.5 flex items-center justify-around text-xs font-bold text-slate-600 select-none">
        <button
          onClick={() => setActiveTab('home')}
          className={`flex items-center gap-1 py-1 px-3 rounded-lg transition-all ${
            activeTab === 'home' ? 'bg-[#007dfa] text-white shadow-2xs' : 'hover:text-blue-600'
          }`}
        >
          <Home className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          <span>ویترین خانه</span>
        </button>

        <button
          onClick={() => setActiveTab('about')}
          className={`flex items-center gap-1 py-1 px-3 rounded-lg transition-all ${
            activeTab === 'about' ? 'bg-[#007dfa] text-white shadow-2xs' : 'hover:text-blue-600'
          }`}
        >
          <Info className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          <span>درباره فروشگاه</span>
        </button>

        <button
          onClick={() => setActiveTab('contact')}
          className={`flex items-center gap-1 py-1 px-3 rounded-lg transition-all ${
            activeTab === 'contact' ? 'bg-[#007dfa] text-white shadow-2xs' : 'hover:text-blue-600'
          }`}
        >
          <PhoneCall className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          <span>تماس با ما</span>
        </button>

        <button
          onClick={() => setIsTrackingModalOpen(true)}
          className="flex items-center gap-1 py-1 px-3 rounded-lg text-slate-600 hover:text-blue-600"
        >
          <PackageSearch className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>
          <span>پیگیری خرید</span>
        </button>
      </div>

      {/* 3. Main Storefront Body */}
      <main className="flex-1 space-y-2">
        {activeTab === 'home' && (
          <>
            {builderWidgets.heroBanner && <SellerStorefrontHero />}
            {builderWidgets.categoriesGrid && <SellerStorefrontCategories />}
            {builderWidgets.dealsSlider && <SpecialDealsSlider products={fallbackProducts} storeBrand={{ name: currentStore.name, accentColor: currentStore.builderConfig?.accentColor ?? resolveTemplateAccent(currentStore.templateId || 'tpl-modern-1', templateConfig.accentColor) }} />}
            {builderWidgets.storeDealsWidget && publicWidget && (
              <div className="mx-3.5 my-2 p-3 bg-gradient-to-r from-blue-600 to-indigo-600 rounded-2xl text-white flex items-center justify-between shadow-xs">
                <div>
                  <span className="text-[10px] bg-white/20 px-2 py-0.5 rounded-full font-bold">پیشنهاد شگفت‌انگیز ویترین</span>
                  <h4 className="text-xs font-black mt-1">{publicWidget.name}</h4>
                </div>
                <span className="text-[11px] font-bold bg-white text-blue-700 px-2.5 py-1 rounded-xl">مشاهده تخفیف‌ها</span>
              </div>
            )}
            {builderWidgets.storeDealsWidget && publicWidget && <section className="mx-3.5 grid grid-cols-2 gap-2" aria-label="کالاهای منتخب ویجت">
              {storefrontProducts(currentStore, templateProducts).filter((product) => publicWidget.productIds.includes(product.id)).map((product) => <ProductCard key={product.id} product={product} />)}
            </section>}
            <SellerStorefrontProductGrid products={fallbackProducts} hasMore={hasMoreScoped} onLoadMore={loadMoreScoped} loadingMore={scopedLoadingMore} />
          </>
        )}

        {activeTab === 'about' && <SellerStorefrontAbout />}
        {activeTab === 'contact' && builderWidgets.contactInfo && <SellerStorefrontContact />}
      </main>

      {/* 4. Branded Footer */}
      <SellerStorefrontFooter storeProducts={fallbackProducts} />
    </div>
  );
};
