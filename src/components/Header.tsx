import React, { useState, useEffect, useRef, useMemo } from 'react';
import { useStore, useStoreManager, useActiveStoreCategorySlugs, expandCategorySlugsWithAncestors, activeStoreSlugOf } from '../context/StoreContext';
import { catalogApi, type ProductDto } from '../services/catalogApi';
import { storeApi } from '../services/storeApi';
import { mapBackendProduct } from '../context/ProductContext';
import { cardPriceOf } from '../utils/cardPrice';
import { AuthModal } from './auth/AuthModal';
import { NotificationDrawer } from './common/NotificationDrawer';
import { Search, ShoppingCart, Truck, User, X, Bell, Package, Heart, LayoutGrid, Menu, ChevronLeft, ArrowLeft } from 'lucide-react';
import type { Category } from '../types/index';

// FIX 12: exact live list (live /api/v1/site-content
// header.search_placeholders) — the previous list used different
// items for slots 2–3.
const searchPlaceholders = [
  'هندزفری، ساعت هوشمند، ایرپاد…',
  'هدفون، اسپیکر، پاوربانک…',
  'سرخ‌کن، قهوه‌ساز…',
  'کیف چرم، هودی اورسایز…',
  'کرم ضدچروک، جوانساز صورت…',
];

export const Header: React.FC<{ sticky?: boolean }> = ({ sticky = true }) => {
  const { 
    currentStore, 
    routeStoreSlug, 
    setCurrentStore, 
    cartItemsCount, 
    setIsCartOpen, 
    searchQuery, 
    setSearchQuery,
    openCatalogCategory,
    setIsTrackingModalOpen,
    customerUser,
    setCustomerUser,
    setActiveView,
    activeView,
    openProductDetail,
    formatPrice,
    toPersianDigits 
  } = useStore();

  const { unreadNotificationCount } = useStoreManager();

  const [placeholderIndex, setPlaceholderIndex] = useState(0);
  const [isAuthOpen, setIsAuthOpen] = useState(false);
  const [isNotifOpen, setIsNotifOpen] = useState(false);
  const [isCatMenuOpen, setIsCatMenuOpen] = useState(false);
  // FIX 16: the trigger button ref is passed to CategoriesMenu (live
  // g5 model): the menu itself owns Escape + outside-mousedown
  // closing, so clicks inside the full-width panel (rendered outside
  // this wrapper, as a direct <header> child) do not close it.
  const catMenuButtonRef = useRef<HTMLButtonElement>(null);
  // Header search dropdown (live parity, live bundle header E5
  // "sb" state): debounced server search; the dropdown opens on
  // a successful fetch and on focus when results exist; Escape /
  // outside mousedown / selection / Enter / empty query close it.
  // Live has no arrow-key navigation and no match highlighting.
  const [searchResults, setSearchResults] = useState<ProductDto[]>([]);
  const [isSearchDropOpen, setIsSearchDropOpen] = useState(false);
  const searchWrapDesktopRef = useRef<HTMLDivElement>(null);
  const searchWrapMobileRef = useRef<HTMLDivElement>(null);
  // A2: the pending debounce timer and the in-flight suggestion fetch
  // live in refs so Enter / a suggestion click can cancel them before
  // navigating — otherwise the timer/fetch resolves after navigation
  // and re-opens the dropdown over the results grid.
  const searchTimerRef = useRef<number | undefined>(undefined);
  const searchAbortRef = useRef<AbortController | null>(null);
  const cancelledTermRef = useRef<string | null>(null);
  const latestTermRef = useRef('');
  const cancelPendingSearch = () => {
    // Mark the term as submitted, so a timer for it that has not been
    // installed yet never starts (see the effect below).
    cancelledTermRef.current = latestTermRef.current.trim();
    if (searchTimerRef.current !== undefined) {
      window.clearTimeout(searchTimerRef.current);
      searchTimerRef.current = undefined;
    }
    searchAbortRef.current?.abort();
    searchAbortRef.current = null;
  };

  useEffect(() => {
    const timer = setInterval(() => {
      setPlaceholderIndex((prev) => (prev + 1) % searchPlaceholders.length);
    }, 3200);
    return () => clearInterval(timer);
  }, []);

  // Store scope for search: while the shopper is inside a store
  // (a store-scoped PDP's routeStoreSlug, or a dropshipper
  // currentStore), suggestions must come from THAT store's
  // assortment at that store's prices — the backend scopes both
  // the list and the detail fetch by storeSlug. No store context
  // → the requests below stay exactly the central ones.
  const searchStoreSlug = activeStoreSlugOf({ routeStoreSlug, currentStore });

  // Live search effect (bundle: trimmed term, 320ms debounce,
  // GET /products/?search=<term>&limit=10). Empty term closes
  // immediately with no fetch; a failed fetch also closes with
  // no rows; a successful fetch opens even with 0 rows (the
  // empty state below). PERF-01: the suggestion list is one
  // server page (limit 10) — the full catalog is never fetched
  // for ten suggestions.
  useEffect(() => {
    const term = searchQuery.trim();
    if (!term) { setSearchResults([]); setIsSearchDropOpen(false); cancelledTermRef.current = null; return; }
    // A2 race (see cancelPendingSearch): a term already submitted by
    // Enter / a suggestion click gets no timer, even if this effect
    // runs after the submit. A stale mark for a different term is
    // cleared, so editing the query suggests again.
    if (cancelledTermRef.current === term) return;
    cancelledTermRef.current = null;
    let dead = false;
    const controller = new AbortController();
    searchAbortRef.current = controller;
    searchTimerRef.current = window.setTimeout(async () => {
      try {
        // E2: a debounced suggestion fetch carries suggest=1 so the
        // server does not log it as a submitted search; the submitted
        // search (Enter -> openCatalogCategory) never carries it.
        const res = await catalogApi.getProductsPage({ search: term, limit: 10, suggest: true, ...(searchStoreSlug ? { storeSlug: searchStoreSlug } : {}) }, { signal: controller.signal });
        if (dead) return;
        setSearchResults(res.items);
        // A2 hardening: only re-open while the search input still
        // holds focus — a fetch resolving after the user moved on
        // must not pop the dropdown open over the new view.
        const stillFocused =
          searchWrapDesktopRef.current?.contains(document.activeElement) ||
          searchWrapMobileRef.current?.contains(document.activeElement);
        if (stillFocused) setIsSearchDropOpen(true);
      } catch {
        if (!dead) { setSearchResults([]); setIsSearchDropOpen(false); }
      }
    }, 320);
    return () => {
      dead = true;
      if (searchTimerRef.current !== undefined) {
        window.clearTimeout(searchTimerRef.current);
        searchTimerRef.current = undefined;
      }
      controller.abort();
      if (searchAbortRef.current === controller) searchAbortRef.current = null;
    };
  }, [searchQuery, searchStoreSlug]);

  // A2 backstop: any navigation closes the suggestion dropdown, so no
  // future navigation path can leave it open over the new view. It
  // only fires on a view *change*, never while typing on the same view.
  useEffect(() => { setIsSearchDropOpen(false); }, [activeView]);

  // Live dismissal (bundle): while open, document mousedown
  // outside the search wrapper and Escape both close. No blur
  // handler exists in live, so none is added here.
  useEffect(() => {
    if (!isSearchDropOpen) return;
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (searchWrapDesktopRef.current && searchWrapDesktopRef.current.contains(t)) return;
      if (searchWrapMobileRef.current && searchWrapMobileRef.current.contains(t)) return;
      setIsSearchDropOpen(false);
    };
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setIsSearchDropOpen(false); };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => { document.removeEventListener('mousedown', onDown); document.removeEventListener('keydown', onKey); };
  }, [isSearchDropOpen]);

  const handleProfileClick = () => {
    if (customerUser.isLoggedIn) {
      setActiveView('customer_profile');
    } else {
      setIsAuthOpen(true);
    }
  };

  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      // A2: cancel the pending debounce timer and abort the in-flight
      // suggestion fetch BEFORE navigating, or either one resolving
      // after navigation re-opens the dropdown over the results grid.
      cancelPendingSearch();
      setIsSearchDropOpen(false);
      openCatalogCategory('all', searchQuery);
    }
  };

  // Live row click (bundle): close, then open the full product
  // detail (getProductById, mapped); on failure open the row
  // itself. The candidate row is already a full DTO, so the
  // fallback maps the row DTO.
  const handleSuggestionClick = (row: ProductDto) => {
    cancelPendingSearch();
    setIsSearchDropOpen(false);
    catalogApi.getProductById(row.id, searchStoreSlug ?? undefined).then(
      (full) => { try { openProductDetail(mapBackendProduct(full)); } catch { openProductDetail(mapBackendProduct(row)); } },
      () => { openProductDetail(mapBackendProduct(row)); },
    );
  };

  // Live dropdown panel (bundle classes verbatim). The banner row
  // above the results is deliberately NOT rendered: live's banner
  // comes from GET /banners?category=<cat>, which the candidate
  // backend does not filter (it ignores `category` and would
  // return the wrong banner) -- see report gap G1. Live returns
  // [] for every probed category today, so omission also matches
  // live's current observable output.
  const searchDropdown = isSearchDropOpen && (
    <div className="absolute top-full right-0 left-0 mt-2 z-50 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl">
      {searchResults.length > 0 ? (
        <div className="max-h-80 overflow-y-auto">
          {searchResults.map((row) => (
            <button
              type="button"
              key={row.id}
              onClick={() => handleSuggestionClick(row)}
              className="flex w-full items-center gap-3 px-3 py-2.5 text-right hover:bg-slate-50"
            >
              {row.image && (
                <img
                  src={row.image}
                  alt=""
                  className="h-10 w-10 shrink-0 rounded-lg object-cover"
                  loading="lazy"
                  onError={(e) => { (e.target as HTMLImageElement).style.display = 'none'; }}
                />
              )}
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm text-slate-800">{row.title}</p>
                {typeof row.price === 'number' && (
                  <p className="text-xs font-bold text-slate-900">{`${formatPrice(cardPriceOf(row).price)} تومان`}</p>
                )}
              </div>
            </button>
          ))}
        </div>
      ) : (
        <p className="px-3 py-4 text-center text-xs text-slate-500">نتیجه‌ای یافت نشد</p>
      )}
    </div>
  );

  // Fix75: see SellerStorefrontHeader — the owned-stores list has no central.
  const goToCentral = async () => {
    try {
      const all = await storeApi.getAllStores();
      const central = (Array.isArray(all) ? all : []).find((s) => s.ownerType === 'central');
      if (central) setCurrentStore(central);
    } catch { /* keep current store; still leave the current view */ }
    setActiveView('customer');
    window.scrollTo({ top: 0, behavior: 'smooth' });
  };

  return (
    <header className={`${sticky ? 'sticky top-0' : 'relative lg:sticky lg:top-0'} z-40 bg-white/95 backdrop-blur-md border-b border-slate-100 shadow-xs`}>
      {/* Row 1: Top Navigation (Snapp Pay Style) */}
      <div className="flex items-center justify-between px-3.5 py-2.5 lg:px-8 lg:py-4 xl:px-10 2xl:px-12">
        {/* Right: Logo & Brand Name */}
        {/* FIX 13: live's header logo is the logo image, not an icon box
            (live bundle: img /static/uploads/logo/logo-icon.png, 36x36,
            rounded-xl object-contain). The preview build prefixes this src
            with /preview (see the preview build step); candidate source
            keeps the plain /static path. */}
        <div
          onClick={goToCentral}
          className="flex items-center gap-2 cursor-pointer select-none group lg:shrink-0"
        >
          <img
            src="/static/uploads/logo/logo-icon.png"
            alt="سلرشو"
            width={36}
            height={36}
            fetchPriority="high"
            decoding="async"
            className="w-9 h-9 lg:w-11 lg:h-11 rounded-xl object-contain shrink-0 group-active:scale-95 transition-transform"
          />
          <div>
            {currentStore.ownerType === 'central' ? (
              <span className="font-black text-base lg:text-xl tracking-tight leading-tight bg-gradient-to-l from-brand to-brand-deep bg-clip-text text-transparent">
                سلرشو
              </span>
            ) : (
              <>
                <span className="font-extrabold text-sm text-slate-900 tracking-tight leading-tight">
                  {currentStore.name}
                </span>
                {currentStore.tagline ? (
                  <p className="text-[10px] text-slate-400 mt-0.5 block line-clamp-1">{currentStore.tagline}</p>
                ) : null}
              </>
            )}
          </div>
        </div>

        {/* Search in the same row on desktop, like live */}
        <div ref={searchWrapDesktopRef} className="hidden lg:flex flex-1 mx-4 relative items-center bg-[#f8fafc] border border-slate-200 rounded-xl px-3 py-2.5 lg:py-3 focus-within:border-brand focus-within:bg-white focus-within:ring-2 focus-within:ring-brand-border transition-all">
          <Search className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => { latestTermRef.current = e.target.value; setSearchQuery(e.target.value); }}
            onKeyDown={handleSearchKeyDown}
            onFocus={() => { if (searchResults.length > 0) setIsSearchDropOpen(true); }}
            placeholder={searchPlaceholders[placeholderIndex]}
            className="w-full bg-transparent text-sm lg:text-base text-slate-800 placeholder-slate-400 focus:outline-hidden"
          />
          {searchDropdown}
          {searchQuery && (
            <button
              onClick={() => setSearchQuery('')}
              className="text-slate-400 hover:text-slate-600 p-2"
            >
              <X className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            </button>
          )}
        </div>

        {/* Left: Actions (Tracking, Notifications, Cart, Profile) */}
        <div className="flex items-center gap-1.5">
          <button
            onClick={() => setActiveView('customer_orders')}
            className="hidden lg:flex p-3 rounded-xl bg-brand-soft text-brand-deep hover:bg-brand-border/70 transition-colors"
            title="سفارش‌ها"
            aria-label="سفارش‌ها"
          >
            <Package className="w-5 h-5"  strokeWidth={1.5}/>
          </button>

          <button
            onClick={() => setActiveView('customer_wishlist')}
            className="hidden lg:flex p-3 rounded-xl bg-brand-soft text-brand-deep hover:bg-brand-border/70 transition-colors"
            title="علاقه‌مندی‌ها"
            aria-label="علاقه‌مندی‌ها"
          >
            <Heart className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <button
            onClick={() => setIsTrackingModalOpen(true)}
            className="p-3 rounded-xl bg-brand-soft text-brand-deep hover:bg-brand-border/70 transition-colors"
            title="پیگیری سفارش"
            aria-label="پیگیری سفارش"
          >
            <Truck className="w-5 h-5"  strokeWidth={1.5}/>
          </button>

          <button
            onClick={() => setIsNotifOpen(true)}
            className="relative p-3 rounded-xl bg-brand-soft text-brand-deep hover:bg-brand-border/70 transition-colors"
            title="اعلان‌ها"
            aria-label={unreadNotificationCount > 0 ? `اعلان‌ها (${toPersianDigits(unreadNotificationCount)} خوانده‌نشده)` : "اعلان‌ها"}
          >
            <Bell className="w-5 h-5"  strokeWidth={1.5}/>
            {unreadNotificationCount > 0 && (
              <span className="absolute top-1.5 right-1.5 w-2 h-2 rounded-full bg-rose-500 ring-2 ring-brand-soft" />
            )}
          </button>

          <button
            onClick={() => setIsCartOpen(true)}
            className="relative hidden lg:flex p-3 rounded-xl bg-brand-soft text-brand-deep hover:bg-brand-border/70 transition-colors"
            title="سبد خرید"
            aria-label={cartItemsCount > 0 ? `سبد خرید (${toPersianDigits(cartItemsCount)} کالا)` : "سبد خرید"}
          >
            <ShoppingCart className="w-5 h-5"  strokeWidth={1.5}/>
            {cartItemsCount > 0 && (
              <span className="absolute -top-1 -right-1 min-w-5 h-5 px-1 rounded-full bg-rose-500 text-white font-bold text-[10px] flex items-center justify-center ring-2 ring-white">
                {toPersianDigits(cartItemsCount)}
              </span>
            )}
          </button>

          <button
            onClick={handleProfileClick}
            className="p-3 rounded-xl bg-brand-soft text-brand-deep hover:bg-brand-border/70 transition-colors"
            title="ورود / حساب کاربری"
            aria-label={customerUser.isLoggedIn ? "حساب کاربری" : "ورود به حساب کاربری"}
          >
            <User className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>
      </div>

      {/* Row 3 (desktop): categories entry, like live's second row */}
      {/* FIX 14: live's "همه دسته‌ها" is a dropdown menu, not a plain button. */}
      <div className="hidden lg:flex items-center px-8 xl:px-10 2xl:px-12 pb-2">
        <div className="relative">
          {/* FIX 15: live trigger classes from the live bundle
              (rounded-lg, text-brand, hover bg-brand-soft, leading
              icon, no chevron) -- the previous plain-text trigger did
              not match live's button look. */}
          <button
            type="button"
            ref={catMenuButtonRef}
            aria-haspopup="true"
            aria-expanded={isCatMenuOpen}
            onClick={() => setIsCatMenuOpen((v) => !v)}
            className="shrink-0 whitespace-nowrap rounded-lg px-2.5 py-1.5 lg:px-3.5 lg:py-2 text-sm font-bold text-brand hover:bg-brand-soft transition-colors flex items-center gap-1.5 lg:font-extrabold lg:text-slate-900"
          >
            <Menu className="w-4 h-4 lg:w-5 lg:h-5"  strokeWidth={1.5}/>
            <span>همه دسته‌ها</span>
          </button>
        </div>
      </div>
      {/* FIX 16: full-width categories menu (live bundle g5) as a direct
          child of <header>, so absolute top-full inset-x-0 spans the
          header width. Trigger classes above are already byte-identical
          to live's and are untouched. */}
      <CategoriesMenu open={isCatMenuOpen} onClose={() => setIsCatMenuOpen(false)} toggleButtonRef={catMenuButtonRef} />

      {/* Customer Mobile Login Modal */}
      <AuthModal
        isOpen={isAuthOpen}
        onClose={() => setIsAuthOpen(false)}
        onSuccess={(user) => {
          setCustomerUser((prev: any) => ({ ...prev, fullName: user.fullName || prev.fullName, mobile: user.mobile, isLoggedIn: user.role === 'customer' }));
          setActiveView('customer_profile');
        }}
        roleTitle="ورود به حساب کاربری خریدار"
      />

      {/* Real-time Notifications Drawer */}
      <NotificationDrawer
        isOpen={isNotifOpen}
        onClose={() => setIsNotifOpen(false)}
      />

      {/* Row 2: Search Bar on mobile only (desktop search lives in row 1, like live) */}
      <div className="px-3.5 pb-2.5 lg:hidden">
        <div ref={searchWrapMobileRef} className="relative flex items-center bg-[#f8fafc] border border-slate-200 rounded-xl px-3 py-2.5 lg:py-3 focus-within:border-brand focus-within:bg-white focus-within:ring-2 focus-within:ring-brand-border transition-all">
          <Search className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => { latestTermRef.current = e.target.value; setSearchQuery(e.target.value); }}
            onKeyDown={handleSearchKeyDown}
            onFocus={() => { if (searchResults.length > 0) setIsSearchDropOpen(true); }}
            placeholder={searchPlaceholders[placeholderIndex]}
            className="w-full bg-transparent text-sm lg:text-base text-slate-800 placeholder-slate-400 focus:outline-hidden"
          />
          {searchDropdown}
          {searchQuery && (
            <button 
              onClick={() => setSearchQuery('')}
              className="text-slate-400 hover:text-slate-600 p-2"
            >
              <X className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            </button>
          )}
        </div>
      </div>
    </header>

  );
};

// FIX 16: live's categories menu (bundle component g5), replacing the
// candidate's narrow w-72 panel. Full-width panel anchored to the
// sticky header (absolute top-full inset-x-0), category column +
// detail panel + footer row. All classes are copied from the live
// bundle. Two icon identities are inferred from the minified bundle
// (aliases, not names): the chevron (Wt) is ChevronLeft and the
// "view all" arrow (Ns) is ArrowLeft -- both are the RTL-forward
// direction and must be compared visually. The empty-state icon (m1)
// is rendered as LayoutGrid for the same reason; the empty state is
// unreachable with real data (3184 categories exist).
// The footer count uses live's RAW number (Latin digits), not
// toPersianDigits: live renders {y.length} directly.
interface CatNode {
  id: string;
  title: string;
  slug: string;
  parentId?: string | null;
  count: number;
  children: CatNode[];
}

const buildCategoryTree = (cats: Category[]): CatNode[] => {
  const byId = new Map<string, CatNode>();
  cats.forEach((c) => byId.set(c.id, { id: c.id, title: c.title, slug: c.slug, parentId: c.parentId, count: c.count, children: [] }));
  const roots: CatNode[] = [];
  cats.forEach((c) => {
    const node = byId.get(c.id)!;
    if (c.parentId && byId.has(c.parentId)) byId.get(c.parentId)!.children.push(node);
    else roots.push(node);
  });
  return roots;
};

const MenuNode: React.FC<{
  node: CatNode;
  depth: number;
  onNavigate: (slug: string) => void;
  onViewTree: () => void;
}> = ({ node, depth, onNavigate, onViewTree }) => {
  const kids = node.children;
  if (kids.length === 0) {
    return (
      <button type="button" role="menuitem" onClick={() => onNavigate(node.slug)} className={`block w-full text-right rounded px-1 py-1 transition-colors hover:text-brand ${depth === 0 ? 'text-sm font-bold text-slate-800' : 'text-[13px] font-medium text-slate-600'}`}>
        {node.title}
      </button>
    );
  }
  if (depth >= 3) {
    return (
      <div className="min-w-0">
        <button type="button" role="menuitem" onClick={() => onNavigate(node.slug)} className="block w-full text-right rounded px-1 py-1 text-[13px] font-bold text-slate-700 transition-colors hover:text-brand">
          {node.title}
        </button>
        <button type="button" role="menuitem" onClick={onViewTree} className="mt-0.5 mr-2 flex items-center gap-0.5 rounded px-1 py-1 text-[12px] font-bold text-brand hover:bg-brand-soft transition-colors">
          مشاهده همهٔ زیردسته‌ها
          <ChevronLeft className="w-3.5 h-3.5 shrink-0"  strokeWidth={1.5}/>
        </button>
      </div>
    );
  }
  if (depth === 2) {
    return (
      <div className="min-w-0">
        <button type="button" role="menuitem" onClick={() => onNavigate(node.slug)} className="block w-full text-right rounded px-1 py-1 text-[13px] font-bold text-slate-700 transition-colors hover:text-brand">
          {node.title}
        </button>
        <div className="mr-2 space-y-0.5 border-r-2 border-slate-100 pr-2">
          {kids.map((ch) => <MenuNode key={ch.id} node={ch} depth={depth + 1} onNavigate={onNavigate} onViewTree={onViewTree} />)}
        </div>
      </div>
    );
  }
  return (
    <div className="min-w-0">
      <button type="button" role="menuitem" onClick={() => onNavigate(node.slug)} className={`flex items-center gap-1 rounded px-1 py-1 transition-colors hover:text-brand text-right ${depth === 0 ? 'text-sm font-extrabold text-slate-900' : 'text-[13px] font-bold text-slate-700'}`}>
        {node.title}
        <ChevronLeft className="w-3.5 h-3.5 text-slate-300 shrink-0"  strokeWidth={1.5}/>
      </button>
      <div className={depth === 0 ? 'mt-1 space-y-0.5' : 'mt-0.5 mr-2 space-y-0.5 border-r-2 border-slate-100 pr-2'}>
        {kids.map((ch) => <MenuNode key={ch.id} node={ch} depth={depth + 1} onNavigate={onNavigate} onViewTree={onViewTree} />)}
      </div>
    </div>
  );
};

const MenuDetail: React.FC<{
  category: CatNode;
  onNavigate: (slug: string) => void;
  onViewTree: () => void;
}> = ({ category, onNavigate, onViewTree }) => {
  const kids = category.children;
  return (
    <div>
      <div className="flex items-center justify-between gap-4 pb-3 mb-4 border-b border-slate-100">
        <button type="button" role="menuitem" onClick={() => onNavigate(category.slug)} className="text-base font-extrabold text-slate-900 hover:text-brand transition-colors text-right">
          {category.title}
        </button>
        <button type="button" role="menuitem" onClick={() => onNavigate(category.slug)} className="shrink-0 inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] font-bold text-brand hover:bg-brand-soft transition-colors">
          مشاهده همه کالاهای «{category.title}»
          <ArrowLeft className="w-3.5 h-3.5"  strokeWidth={1.5}/>
        </button>
      </div>
      {kids.length === 0 ? (
        <div className="py-6 flex flex-col items-start gap-3">
          <p className="text-sm text-slate-500">برای «{category.title}» هنوز زیردسته‌ای ثبت نشده است.</p>
          <button type="button" role="menuitem" onClick={() => onNavigate(category.slug)} className="inline-flex items-center gap-1.5 rounded-xl bg-brand px-4 py-2.5 text-sm font-bold text-white hover:bg-brand-deep transition-colors">
            مشاهده همه کالاهای «{category.title}»
            <ArrowLeft className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-x-8 gap-y-6">
          {kids.map((ch) => <MenuNode key={ch.id} node={ch} depth={0} onNavigate={onNavigate} onViewTree={onViewTree} />)}
        </div>
      )}
    </div>
  );
};

const CategoriesMenu: React.FC<{
  open: boolean;
  onClose: () => void;
  toggleButtonRef: React.RefObject<HTMLButtonElement | null>;
}> = ({ open, onClose, toggleButtonRef }) => {
  const { categories, openCatalogCategory, setActiveView } = useStore();
  const panelRef = useRef<HTMLDivElement>(null);
  // While a store context is active this menu is the STORE's menu:
  // it lists only the categories the store actually sells (its
  // scoped assortment, ancestors included). While that set loads,
  // the full platform tree must NOT flash — the panel shows a
  // loading row instead. No store context: the platform tree,
  // exactly as before.
  const { storeSlug, categorySlugs } = useActiveStoreCategorySlugs();
  const visibleCategories = useMemo(() => {
    if (!storeSlug) return categories;
    if (!categorySlugs) return null;
    const included = expandCategorySlugsWithAncestors(categories, categorySlugs);
    return categories.filter((c) => included.has(c.slug));
  }, [categories, storeSlug, categorySlugs]);
  const roots = useMemo(() => buildCategoryTree(visibleCategories ?? []), [visibleCategories]);
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    if (roots.length === 0) { setSelectedSlug(null); return; }
    if (!selectedSlug || !roots.some((r) => r.slug === selectedSlug)) setSelectedSlug(roots[0].slug);
  }, [open, roots, selectedSlug]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (panelRef.current && panelRef.current.contains(t)) return;
      if (toggleButtonRef.current && toggleButtonRef.current.contains(t)) return;
      onClose();
    };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open, onClose, toggleButtonRef]);

  if (!open) return null;

  const navigate = (slug: string) => { onClose(); openCatalogCategory(slug); };
  const viewTree = () => { onClose(); setActiveView('categories_tree'); };
  const selected = roots.find((r) => r.slug === selectedSlug) ?? roots[0] ?? null;

  return (
    <div ref={panelRef} role="menu" aria-label="همه دسته‌ها" className="hidden lg:block absolute top-full inset-x-0 z-50 bg-white border-b border-slate-200 shadow-xl shadow-slate-900/10">
      {storeSlug && !visibleCategories ? (
        <div className="px-8 xl:px-10 2xl:px-12 py-10 flex flex-col items-center gap-3 text-center">
          <LayoutGrid className="w-10 h-10 text-slate-300"  strokeWidth={1.5}/>
          <p className="text-sm font-semibold text-slate-600">در حال بارگذاری دسته‌بندی‌های فروشگاه…</p>
        </div>
      ) : roots.length === 0 ? (
        <div className="px-8 xl:px-10 2xl:px-12 py-10 flex flex-col items-center gap-3 text-center">
          <LayoutGrid className="w-10 h-10 text-slate-300"  strokeWidth={1.5}/>
          <p className="text-sm font-semibold text-slate-600">دسته‌بندی‌ای ثبت نشده است</p>
          <p className="text-xs text-slate-400">به‌محض ثبت دسته‌بندی، اینجا نمایش داده می‌شود.</p>
          <button type="button" role="menuitem" onClick={viewTree} className="mt-1 inline-flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm font-bold text-brand hover:bg-brand-soft transition-colors">
            <LayoutGrid className="w-4 h-4"  strokeWidth={1.5}/>
            مشاهده همه دسته‌ها
          </button>
        </div>
      ) : (
        <>
          <div className="px-8 xl:px-10 2xl:px-12 py-5 flex gap-8">
            <ul className="w-60 xl:w-64 shrink-0 max-h-[60vh] overflow-y-auto no-scrollbar">
              {roots.map((cat) => {
                const active = selected?.slug === cat.slug;
                return (
                  <li key={cat.id}>
                    <button type="button" role="menuitem" onMouseEnter={() => setSelectedSlug(cat.slug)} onFocus={() => setSelectedSlug(cat.slug)} onClick={() => navigate(cat.slug)} className={`w-full flex items-center justify-between gap-2 rounded-lg px-3 py-2.5 text-sm transition-colors text-right ${active ? 'bg-brand-soft text-brand-deep font-extrabold' : 'text-slate-700 font-semibold hover:bg-slate-50 hover:text-slate-900'}`}>
                      <span className="truncate">{cat.title}</span>
                      {cat.children.length > 0 && <ChevronLeft className={`w-4 h-4 shrink-0 transition-colors ${active ? 'text-brand' : 'text-slate-300'}`}  strokeWidth={1.5}/>}
                    </button>
                  </li>
                );
              })}
            </ul>
            <span aria-hidden="true" className="w-px self-stretch bg-slate-100 shrink-0" />
            <div className="flex-1 min-w-0 max-h-[60vh] overflow-y-auto no-scrollbar">
              {selected && <MenuDetail category={selected} onNavigate={navigate} onViewTree={viewTree} />}
            </div>
          </div>
          <div className="border-t border-slate-100 px-8 xl:px-10 2xl:px-12 py-3 flex items-center justify-between">
            <button type="button" role="menuitem" onClick={viewTree} className="inline-flex items-center gap-1.5 rounded-lg px-2 py-1.5 text-sm font-bold text-brand hover:bg-brand-soft transition-colors">
              <LayoutGrid className="w-4 h-4"  strokeWidth={1.5}/>
              مشاهده همه دسته‌ها
            </button>
            <span className="text-xs text-slate-400">{roots.length} دسته‌بندی اصلی</span>
          </div>
        </>
      )}
    </div>
  );
};
