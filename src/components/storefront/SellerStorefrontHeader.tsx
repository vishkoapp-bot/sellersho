import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { ShoppingCart, Search, Store, Heart, User, Bell, Menu, X, ShoppingBag } from 'lucide-react';
import { resolveTemplateConfig, sectionEnabled, templateCustom, storefrontProducts } from './templates/templateTypes';
import { groupProductsByCategory } from './templates/StoreSections';
import './templates/fashion.css';
import { NotificationDrawer } from '../common/NotificationDrawer';

// FIX 13: live's storefront header (bundle component Db). Beige #FAF8F4
// background, #ECE4D6 border, 40px CIRCLE icon filled with the resolved
// accent, stone-900 15px name, stone buttons. Accent resolution is
// templateConfig.accentColor || Ue[templateId] || '#0D7377' — live's
// default teal. Live's public store API strips templateConfig entirely,
// so live's three observed storefronts all render the teal default.
// (The staging mone accent #2563eb vs live builderConfig #007dfa
// remains a DATA gap for Richard's decision, not a code gap.)
// Headers are TEMPLATE-dependent on live: the Digikala template has
// only 2 buttons + an always-visible search input (live Digikala chunk);
// the Modern view has the 4 buttons. The empty-storefront path always
// uses the Modern header (that is what live renders for all three
// observed empty stores); only the template path passes
// variant="digikala".
const TEMPLATE_ACCENTS: Record<string, string> = {
  'tpl-electronic-1': '#f97847',
  'tpl-fashion-1': '#793f3c',
  'tpl-digikala-1': '#ef4056',
  'tpl-modern-1': '#0D7377',
};

interface Props {
  onOpenSearch: () => void;
  variant?: 'modern' | 'digikala' | 'fashion';
  searchQuery?: string;
  onSearchChange?: (q: string) => void;
}

export const SellerStorefrontHeader: React.FC<Props> = ({
  onOpenSearch,
  variant = 'modern',
  searchQuery = '',
  onSearchChange,
}) => {
  const { currentStore, cartItemsCount, setIsCartOpen, setActiveView, toPersianDigits, customerUser, activeView, routeStoreSlug, products, categories, setSelectedCategory, clearCatalogBrand, setSearchQuery } = useStore();
  const [logoFailed, setLogoFailed] = useState(false);
  const [fashionMenuOpen, setFashionMenuOpen] = useState(false);
  const fashionMenuId = React.useId();
  useEffect(() => { setFashionMenuOpen(false); }, [currentStore.id]);
  // ISO-E: a failed logo for store A must not suppress store B's
  // valid logo (same component instance across SPA navigation).
  useEffect(() => { setLogoFailed(false); }, [currentStore.id, currentStore.logo]);
  const [isNotifOpen, setIsNotifOpen] = useState(false);
  // FIX 14: live's public store API strips templateConfig, so live's
  // observed storefronts all render the teal default icon (#0D7377),
  // not the staging per-store accent colours.
  const iconBg = '#0D7377';
  const accent =
    (currentStore.templateConfig as { accentColor?: string } | undefined)?.accentColor ||
    TEMPLATE_ACCENTS[currentStore.templateId || ''] ||
    '#0D7377';
  const tagline = currentStore.shortDescription || currentStore.tagline || '';
  const initial = (currentStore.name || '?').charAt(0);

  // Brand click goes to THIS store's home (owner decision, Nov 5,
  // 2026 — reverses the Fix70 goToCentral escape to the central
  // store): a storefront must never eject its visitor to the central
  // store. On the storefront itself the click mirrors the bottom
  // nav's «خانه» action exactly — a smooth scroll to the top and
  // nothing else (that action resets no tab/filter state either).
  // From a store-scoped PDP it navigates back to this store's
  // storefront carrying the store's slug, exactly like the PDP's
  // own back/search actions in App.tsx; the store used is the one
  // this header renders for (routeStoreSlug, else currentStore —
  // the context keeps currentStore aligned to routeStoreSlug while
  // a scoped PDP is shown).
  const goToStoreHome = () => {
    if (activeView === 'seller_storefront') {
      if (typeof window !== 'undefined') window.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }
    const slug = routeStoreSlug || currentStore?.slug;
    setActiveView('seller_storefront', slug ? { storeSlug: slug } : {});
  };

  const iconContent = currentStore.logo && !logoFailed ? (
    <img
      src={currentStore.logo}
      alt={currentStore.name}
      className="w-full h-full object-cover"
      onError={() => setLogoFailed(true)}
    />
  ) : currentStore.logo ? (
    <span className="text-white font-black text-lg leading-none select-none">{initial}</span>
  ) : (
    <Store className="w-5 h-5" strokeWidth={1.5} />
  );

  const cartButton = (
    <button
      onClick={() => setIsCartOpen(true)}
      className="relative p-2 rounded-full text-stone-600 hover:bg-stone-200/70 transition-colors"
      title="سبد خرید"
    >
      <ShoppingCart className="w-5 h-5" strokeWidth={1.5} />
      {cartItemsCount > 0 && (
        <span className="absolute top-0.5 right-0.5 text-white font-bold text-[9px] min-w-4 h-4 px-0.5 rounded-full flex items-center justify-center" style={{ backgroundColor: accent, boxShadow: '0 0 0 2px #FAF8F4' }}>
          {cartItemsCount > 99 ? '۹۹+' : toPersianDigits(cartItemsCount)}
        </span>
      )}
    </button>
  );

  const accountButton = (
    <button
      onClick={() => setActiveView('customer_profile')}
      className="p-2 rounded-full text-stone-600 hover:bg-stone-200/70 transition-colors"
      title={(customerUser?.isLoggedIn || customerUser?.mobile) ? 'حساب کاربری' : 'ورود به حساب کاربری'}
    >
      <User className="w-5 h-5" strokeWidth={1.5} />
    </button>
  );

  if (variant === 'digikala') {
    // Live Digikala template header: brand row with 2 buttons, plus an
    // always-visible search input row below (live Digikala chunk).
    return (
      <>
      <header className="sticky top-0 z-30 bg-white border-b border-slate-200">
        <div className="flex items-center justify-between px-4 py-2.5">
          <div className="flex items-center gap-2.5 min-w-0 cursor-pointer" onClick={goToStoreHome} title={`خانه ${currentStore.name}`}>
            <div
              className="w-8 h-8 rounded-full flex items-center justify-center text-white font-black text-base shrink-0 shadow-sm overflow-hidden"
              style={{ backgroundColor: iconBg }}
            >
              {iconContent}
            </div>
            <div className="min-w-0">
              <h1 className="font-black text-[22px] tracking-tight leading-tight truncate" style={{ color: accent }}>
                {currentStore.name}
              </h1>
            </div>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            <button
              onClick={() => setIsNotifOpen(true)}
              className="relative p-2 rounded-full text-stone-600 hover:bg-stone-200/70 transition-colors"
              title="اعلان‌ها"
            >
              <Bell className="w-5 h-5" strokeWidth={1.5} />
            </button>
            {cartButton}
          </div>
        </div>
        <div className="px-4 pt-3 pb-2">
          <div
            className="flex items-center gap-2 h-[44px] px-3"
            style={{ backgroundColor: `${accent}0D`, borderRadius: 16, border: `1px solid ${accent}33` }}
          >
            <Search className="w-5 h-5 shrink-0" style={{ color: accent }} strokeWidth={1.5} />
            <input
              type="search"
              value={searchQuery}
              onChange={(e) => onSearchChange?.(e.target.value)}
              placeholder={`جستجو در ${currentStore.name}`}
              className="flex-1 bg-transparent outline-none text-base placeholder:text-[#a1a3a8]"
              style={{ color: '#23254e' }}
              aria-label="جستجو"
            />
          </div>
        </div>
      </header>
      <NotificationDrawer isOpen={isNotifOpen} onClose={() => setIsNotifOpen(false)} />
      </>
    );
  }

  if (variant === 'fashion') {
    const cfg = resolveTemplateConfig('tpl-fashion-1', currentStore.templateConfig);
    const custom = templateCustom(cfg);
    const groups = groupProductsByCategory(storefrontProducts(currentStore, products), categories, custom.categoryOverrides);
    const navItems = groups.slice(0, 3).map(g => ({ title: g.title, slug: g.slug }));
    const select = (slug: string) => {
      clearCatalogBrand(); setSearchQuery(''); setSelectedCategory(slug); setFashionMenuOpen(false);
      if (activeView !== 'seller_storefront') goToStoreHome();
    };
    return <header className="fashion-header">
      {sectionEnabled(cfg, 'announcement') && custom.announcementText && <div className="fashion-announcement" data-testid="store-announcement">
        {custom.announcementLink ? <a href={custom.announcementLink} {...(/^https?:\/\//i.test(custom.announcementLink) ? { target: '_blank', rel: 'noreferrer' } : {})}>{custom.announcementText}</a> : custom.announcementText}
      </div>}
      <div className="fashion-header-row">
        <button type="button" className="fashion-icon-button fashion-mobile-toggle" onClick={() => setFashionMenuOpen(v => !v)} aria-label={fashionMenuOpen ? 'بستن منو' : 'باز کردن منو'} aria-expanded={fashionMenuOpen} aria-controls={fashionMenuId}>
          {fashionMenuOpen ? <X size={22} strokeWidth={1.5} /> : <Menu size={22} strokeWidth={1.5} />}
        </button>
        <button type="button" className="fashion-wordmark" onClick={goToStoreHome} aria-label={`خانه ${currentStore.name}`}>
          {currentStore.logo && !logoFailed && <img src={currentStore.logo} alt="" onError={() => setLogoFailed(true)} />}
          <span>{currentStore.name}</span>
        </button>
        <nav className="fashion-header-nav" aria-label="منوی فروشگاه">
          {navItems.map(item => <a key={item.slug} href="#products" onClick={() => select(item.slug)}>{item.title}</a>)}
          <a href="#products" onClick={() => select('all')}>تازه‌ها</a>
          <a href="#fashion-contact">تماس با ما</a>
        </nav>
        <div className="fashion-header-actions">
          <button type="button" onClick={onOpenSearch} className="fashion-icon-button" aria-label="جستجو در محصولات"><Search size={21} strokeWidth={1.5} /></button>
          <button type="button" onClick={() => setActiveView('customer_wishlist')} className="fashion-icon-button fashion-desktop-action" aria-label="علاقه‌مندی‌ها"><Heart size={21} strokeWidth={1.5} /></button>
          <button type="button" onClick={() => setActiveView('customer_profile')} className="fashion-icon-button fashion-desktop-action" aria-label="حساب کاربری"><User size={21} strokeWidth={1.5} /></button>
          <button type="button" onClick={() => setIsCartOpen(true)} className="fashion-icon-button" aria-label="سبد خرید">
            <ShoppingBag size={21} strokeWidth={1.5} />
            {cartItemsCount > 0 && <span className="absolute top-0 right-0 min-w-4 h-4 bg-[#1c1c1a] text-white text-[9px] flex items-center justify-center rounded-full">{cartItemsCount > 99 ? '۹۹+' : toPersianDigits(cartItemsCount)}</span>}
          </button>
        </div>
      </div>
      {fashionMenuOpen && <nav id={fashionMenuId} className="fashion-header-menu" aria-label="منوی موبایل">
        {navItems.map(item => <a key={item.slug} href="#products" onClick={() => select(item.slug)}>{item.title}</a>)}
        <a href="#products" onClick={() => select('all')}>همه کالاها</a>
        <a href="#fashion-contact" onClick={() => setFashionMenuOpen(false)}>تماس با ما</a>
        <a href="#" onClick={e => { e.preventDefault(); setFashionMenuOpen(false); setActiveView('customer_profile'); }}>حساب کاربری</a>
      </nav>}
    </header>;
  }

  // Modern (live default) header: 4 icon buttons, search via overlay.
  return (
    <header
      className="sticky top-0 z-30 border-b"
      style={{ backgroundColor: '#FAF8F4', borderColor: '#ECE4D6' }}
    >
      <div className="mx-auto w-full max-w-[1440px] px-4 md:px-6 lg:px-10 flex items-center justify-between py-2.5">
        <div className="flex items-center gap-2.5 min-w-0 cursor-pointer" onClick={goToStoreHome} title={`خانه ${currentStore.name}`}>
          <div
            className="w-10 h-10 rounded-full flex items-center justify-center text-white font-black text-base shrink-0 shadow-sm overflow-hidden"
            style={{ backgroundColor: iconBg }}
          >
            {iconContent}
          </div>
          <div className="min-w-0">
            <h1 className="font-extrabold text-[15px] tracking-tight leading-tight truncate" style={{ color: '#1C1917' }}>
              {currentStore.name}
            </h1>
            {tagline !== '' && (
              <span className="text-[10px] text-stone-400 mt-0.5 block line-clamp-1">{tagline}</span>
            )}
          </div>
        </div>
        <div className="flex items-center gap-1 shrink-0">
          <button
            onClick={onOpenSearch}
            className="p-2 rounded-full text-stone-600 hover:bg-stone-200/70 transition-colors"
            title="جستجو در محصولات"
          >
            <Search className="w-5 h-5" strokeWidth={1.5} />
          </button>
          <button
            onClick={() => setActiveView('customer_wishlist')}
            className="p-2 rounded-full text-stone-600 hover:bg-stone-200/70 transition-colors"
            title="علاقه‌مندی‌ها"
          >
            <Heart className="w-5 h-5" strokeWidth={1.5} />
          </button>
          {cartButton}
          {accountButton}
        </div>
      </div>
    </header>
  );
};
