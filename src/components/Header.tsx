import React, { lazy, Suspense, useEffect, useId, useMemo, useRef, useState } from 'react';
import { ArrowLeft, Bell, ChevronLeft, Heart, LayoutGrid, Package, Search, ShoppingBag, Truck, User, X } from 'lucide-react';
import { activeStoreSlugOf, expandCategorySlugsWithAncestors, useActiveStoreCategorySlugs, useStore, useStoreManager } from '../context/StoreContext';
import { mapBackendProduct } from '../context/ProductContext';
import { catalogApi, type ProductDto } from '../services/catalogApi';
import { cardPriceOf } from '../utils/cardPrice';
import { followStoreLink } from '../utils/followStoreLink';
import { buildProductUrl, pathForView } from '../utils/routing';
const NotificationDrawer = lazy(() => import('./common/NotificationDrawer').then(module => ({ default: module.NotificationDrawer })));

export const Header: React.FC<{ sticky?: boolean }> = ({ sticky = true }) => {
  const {
    currentStore, routeStoreSlug, cartItemsCount, setIsCartOpen, searchQuery,
    openCatalogCategory, setIsTrackingModalOpen, customerUser, setActiveView,
    activeView, openProductDetail, formatPrice, toPersianDigits, categories,
  } = useStore();
  const { unreadNotificationCount } = useStoreManager();
  const [draft, setDraft] = useState(searchQuery);
  const [results, setResults] = useState<{ term: string; items: ProductDto[] } | null>(null);
  const [suggestionsOpen, setSuggestionsOpen] = useState(false);
  const [isNotifOpen, setIsNotifOpen] = useState(false);
  const [isCatMenuOpen, setIsCatMenuOpen] = useState(false);
  const [selectedRoot, setSelectedRoot] = useState('');
  const searchRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLElement>(null);
  const suggestionsId = useId();
  const menuId = useId();
  const pending = useRef<AbortController | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const suppressed = useRef<string | null>(null);
  const liveDraft = useRef(draft);
  liveDraft.current = draft;
  const searchStoreSlug = activeStoreSlugOf({ routeStoreSlug, currentStore });
  const { storeSlug, categorySlugs } = useActiveStoreCategorySlugs();
  const visibleCategories = useMemo(() => {
    if (!storeSlug) return categories;
    if (!categorySlugs) return [];
    const included = expandCategorySlugsWithAncestors(categories, categorySlugs);
    return categories.filter(category => included.has(category.slug));
  }, [storeSlug, categorySlugs, categories]);
  const roots = useMemo(() => {
    const ids = new Set(visibleCategories.map(category => category.id));
    return visibleCategories.filter(category => !category.parentId || !ids.has(category.parentId));
  }, [visibleCategories]);
  const root = roots.find(item => item.slug === selectedRoot) ?? roots[0];
  const displayedResults = results?.term === draft.trim() && suggestionsOpen ? results.items : null;

  const stopSearch = (term: string) => {
    suppressed.current = term;
    window.clearTimeout(timer.current);
    pending.current?.abort();
    setSuggestionsOpen(false);
  };
  useEffect(() => {
    setDraft(searchQuery);
    suppressed.current = searchQuery.trim();
    setSuggestionsOpen(false);
    setResults(null);
    setIsCatMenuOpen(false);
  }, [searchQuery, activeView, searchStoreSlug]);

  useEffect(() => {
    const term = draft.trim();
    if (term.length < 2 || term === suppressed.current) return;
    const controller = new AbortController();
    pending.current = controller;
    timer.current = window.setTimeout(async () => {
      try {
        const page = await catalogApi.getProductsPage(
          { search: term, limit: 6, suggest: true, ...(searchStoreSlug ? { storeSlug: searchStoreSlug } : {}) },
          { signal: controller.signal },
        );
        if (controller.signal.aborted || term !== liveDraft.current.trim()) return;
        setResults({ term, items: page.items });
        if (searchRef.current?.contains(document.activeElement)) setSuggestionsOpen(true);
      } catch {
        if (!controller.signal.aborted) { setResults(null); setSuggestionsOpen(false); }
      }
    }, 320);
    return () => { window.clearTimeout(timer.current); controller.abort(); };
  }, [draft, searchStoreSlug]);

  useEffect(() => {
    if (!suggestionsOpen && !isCatMenuOpen) return;
    const closeOutside = (event: PointerEvent) => {
      const target = event.target as Node;
      if (!searchRef.current?.contains(target)) setSuggestionsOpen(false);
      if (!menuRef.current?.contains(target)) setIsCatMenuOpen(false);
    };
    document.addEventListener('pointerdown', closeOutside);
    return () => document.removeEventListener('pointerdown', closeOutside);
  }, [suggestionsOpen, isCatMenuOpen]);

  useEffect(() => {
    const header = headerRef.current;
    const shell = header?.closest<HTMLElement>('.central-site');
    if (!header || !shell || typeof ResizeObserver === 'undefined') return;
    const update = () => shell.style.setProperty('--shop-header-height', sticky ? `${Math.ceil(header.getBoundingClientRect().height)}px` : '0px');
    const observer = new ResizeObserver(update);
    update();
    observer.observe(header);
    return () => { observer.disconnect(); shell.style.removeProperty('--shop-header-height'); };
  }, [sticky]);

  const submitSearch = (term = liveDraft.current.trim()) => {
    stopSearch(term);
    openCatalogCategory('all', term);
    searchInputRef.current?.blur();
  };

  return (
    <header ref={headerRef} className={`central-header ${sticky ? 'central-header--sticky' : ''}`}>
      <div className="central-wrap central-header-main">
        <a className="central-wordmark" href="/" onClick={event => followStoreLink(event, () => setActiveView('customer'))}>
          <span className="central-mark"><ShoppingBag size={22} aria-hidden="true" /></span>
          <span>سلرشو</span>
        </a>

        <div className="central-search relative" ref={searchRef}
          onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setSuggestionsOpen(false); }}
          onKeyDown={event => {
            if (event.key === 'Escape' && (suggestionsOpen || draft.trim().length >= 2)) {
              event.preventDefault();
              stopSearch(liveDraft.current.trim());
              searchInputRef.current?.focus({ preventScroll: true });
              return;
            }
            if (!displayedResults || (event.key !== 'ArrowDown' && event.key !== 'ArrowUp')) return;
            const targets = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('[data-suggestion], .central-suggestion-all'));
            const index = targets.indexOf(event.target as HTMLElement);
            if (index < 0 && event.target !== searchInputRef.current) return;
            event.preventDefault();
            const next = event.key === 'ArrowDown' ? targets[index + 1] : targets[index < 0 ? targets.length - 1 : index - 1];
            (next ?? searchInputRef.current)?.focus();
          }}>
          <form role="search" aria-label="جست‌وجوی کالا" onSubmit={event => { event.preventDefault(); submitSearch(); }}>
            <Search className="central-search-icon" size={20} aria-hidden="true" />
            <input type="text" id="central-search" name="q" aria-label="نام کالا یا برند" autoComplete="off" enterKeyHint="search"
              value={draft} ref={searchInputRef} aria-controls={displayedResults ? suggestionsId : undefined}
              placeholder="نام کالا یا برند…"
              onKeyDown={event => { if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); submitSearch(); } }}
              onFocus={() => { if (results?.term === liveDraft.current.trim() && suppressed.current !== results.term) setSuggestionsOpen(true); }}
              onChange={event => { liveDraft.current = event.target.value; suppressed.current = null; setDraft(event.target.value); setSuggestionsOpen(false); }} />
            {draft && <button type="button" className="central-icon" aria-label="پاک کردن جست‌وجو"
              onClick={() => { stopSearch(''); setDraft(''); setResults(null); searchInputRef.current?.focus(); }}>
              <X size={18} aria-hidden="true" />
            </button>}
            <button type="submit" className="central-search-submit" aria-label="جست‌وجو">
              <span>جست‌وجو</span><ArrowLeft className="central-submit-arrow" size={18} aria-hidden="true" /><Search className="central-submit-mobile" size={18} aria-hidden="true" />
            </button>
          </form>
          {displayedResults && (
            <div id={suggestionsId} className="absolute top-full central-suggestions">
              {displayedResults.length ? (
                <ul>
                  {displayedResults.map(row => (
                    <li key={row.id}>
                      <a href={buildProductUrl(row, searchStoreSlug ?? undefined)} data-suggestion
                        onClick={event => followStoreLink(event, () => {
                          stopSearch(liveDraft.current.trim());
                          openProductDetail(mapBackendProduct(row));
                        })}>
                        {row.image && <img src={row.image} alt="" width="44" height="44" loading="lazy" onError={event => { event.currentTarget.hidden = true; }} />}
                        <span className="min-w-0 flex-1 truncate">{row.title}</span>
                        <strong>{formatPrice(cardPriceOf(row).price)} تومان</strong>
                      </a>
                    </li>
                  ))}
                </ul>
              ) : <p role="status">پیشنهادی برای این عبارت پیدا نشد.</p>}
              <button type="button" className="central-suggestion-all" onClick={() => submitSearch()}>
                دیدن نتایج جست‌وجو <ArrowLeft size={17} aria-hidden="true" />
              </button>
            </div>
          )}
        </div>

        <div className="central-header-actions">
          {customerUser.isLoggedIn && <button type="button" className="central-icon relative" onClick={() => setIsNotifOpen(true)}
            aria-label={unreadNotificationCount ? `اعلان‌ها، ${toPersianDigits(unreadNotificationCount)} خوانده‌نشده` : 'اعلان‌ها'} title="اعلان‌ها">
            <Bell size={20} aria-hidden="true" />{unreadNotificationCount > 0 && <span className="central-count central-count--dot" />}
          </button>}
          <a href="/account" className="central-icon central-desktop-action" aria-label="حساب کاربری" title="حساب کاربری"
            aria-current={activeView === 'customer_profile' ? 'page' : undefined}
            onClick={event => followStoreLink(event, () => setActiveView('customer_profile'))}>
            <User size={21} aria-hidden="true" />
          </a>
          <button type="button" className="central-icon central-desktop-action relative" onClick={() => setIsCartOpen(true)}
            aria-label={cartItemsCount ? `سبد خرید، ${toPersianDigits(cartItemsCount)} کالا` : 'سبد خرید'} title="سبد خرید">
            <ShoppingBag size={21} aria-hidden="true" />
            {cartItemsCount > 0 && <span className="central-count">{toPersianDigits(cartItemsCount)}</span>}
          </button>
          <button type="button" className="central-track-mobile" onClick={() => setIsTrackingModalOpen(true)}><Truck size={17} aria-hidden="true" /> پیگیری سفارش</button>
        </div>
      </div>

      <nav className="central-header-nav" aria-label="ناوبری فروشگاه">
        <div className="central-wrap">
          <div ref={menuRef} className="central-menu"
            onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setIsCatMenuOpen(false); }}
            onKeyDown={event => {
            if (event.key === 'Escape') {
              setIsCatMenuOpen(false);
              menuRef.current?.querySelector('button')?.focus();
            }
          }}>
            <button type="button" className="central-menu-trigger" aria-expanded={isCatMenuOpen} aria-controls={menuId}
              onClick={() => setIsCatMenuOpen(open => !open)}><LayoutGrid size={17} aria-hidden="true" /> همه دسته‌ها</button>
            <div className="central-menu-panel" id={menuId} hidden={!isCatMenuOpen}>
              <div className="central-menu-roots">
                {roots.map(category => (
                  <button key={category.id} type="button" aria-pressed={root?.id === category.id}
                    onClick={() => setSelectedRoot(category.slug)}>{category.title}<ChevronLeft size={15} aria-hidden="true" /></button>
                ))}
              </div>
              <div className="central-menu-detail">
                {root && <>
                  <a className="central-menu-all" href={pathForView('catalog', { categorySlug: root.slug })}
                    onClick={event => followStoreLink(event, () => { setIsCatMenuOpen(false); openCatalogCategory(root.slug); })}>
                    همه کالاهای {root.title}<ArrowLeft size={17} aria-hidden="true" />
                  </a>
                  {visibleCategories.filter(category => category.parentId === root.id).map(category => (
                    <a key={category.id} href={pathForView('catalog', { categorySlug: category.slug })}
                      onClick={event => followStoreLink(event, () => { setIsCatMenuOpen(false); openCatalogCategory(category.slug); })}>{category.title}</a>
                  ))}
                </>}
                <a href={pathForView('categories_tree')} onClick={event => followStoreLink(event, () => { setIsCatMenuOpen(false); setActiveView('categories_tree'); })}>همهٔ دسته‌بندی‌ها</a>
              </div>
            </div>
          </div>
          <a href={pathForView('catalog')} onClick={event => followStoreLink(event, () => openCatalogCategory('all'))} aria-current={activeView === 'catalog' ? 'page' : undefined}>همه کالاها</a>
          <a href="/account/orders" aria-current={activeView === 'customer_orders' ? 'page' : undefined} onClick={event => followStoreLink(event, () => setActiveView('customer_orders'))}><Package size={16} aria-hidden="true" /> سفارش‌های من</a>
          <a href={pathForView('customer_wishlist')} aria-current={activeView === 'customer_wishlist' ? 'page' : undefined} onClick={event => followStoreLink(event, () => setActiveView('customer_wishlist'))}><Heart size={16} aria-hidden="true" /> علاقه‌مندی‌ها</a>
          <button type="button" className="central-track" onClick={() => setIsTrackingModalOpen(true)}><Truck size={18} aria-hidden="true" /> پیگیری سفارش</button>
        </div>
      </nav>

      {isNotifOpen && (
        <Suspense fallback={<p className="central-overlay-loading" role="status">در حال بارگذاری اعلان‌ها…</p>}>
          <NotificationDrawer central isOpen={isNotifOpen} onClose={() => setIsNotifOpen(false)} />
        </Suspense>
      )}
    </header>
  );
};
