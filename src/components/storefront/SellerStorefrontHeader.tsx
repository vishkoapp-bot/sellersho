import React, { useState, useEffect } from 'react';
import type { Product } from '../../types';
import { useStore } from '../../context/StoreContext';
import { ShoppingCart, Search, Store, Heart, User, Bell, Menu, X, ShoppingBag } from 'lucide-react';
import { resolveTemplateConfig, sectionEnabled, orderedEnabledSections, templateCustom, storefrontProducts } from './templates/templateTypes';
import { groupProductsByCategory } from './templates/StoreSections';
import './templates/fashion.css';
import { NotificationDrawer } from '../common/NotificationDrawer';
import { pathForView } from '../../utils/routing';
import { followStoreLink } from '../../utils/followStoreLink';

const TEMPLATE_ACCENTS: Record<string, string> = {
  'tpl-electronic-1': '#f97847',
  'tpl-fashion-1': '#793f3c',
  'tpl-digikala-1': '#ef4056',
  'tpl-modern-1': '#0D7377',
};

interface Props {
  onOpenSearch: () => void;
  variant?: 'modern' | 'themed';
  navigationProducts?: Product[];
}

export const SellerStorefrontHeader: React.FC<Props> = ({
  onOpenSearch,
  variant = 'modern',
  navigationProducts,
}) => {
  const { currentStore, cartItemsCount, setIsCartOpen, setActiveView, toPersianDigits, customerUser, activeView, routeStoreSlug, products, categories, setIsTrackingModalOpen, setSelectedCategory, clearCatalogBrand, setSearchQuery } = useStore();
  const [logoFailed, setLogoFailed] = useState(false);
  const [fashionMenuOpen, setFashionMenuOpen] = useState(false);
  const fashionMenuId = React.useId();
  const fashionMenuButton = React.useRef<HTMLButtonElement>(null);
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
      if (typeof window !== 'undefined') window.scrollTo({ top: 0, behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
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

  if (variant === 'themed') {
    const templateId = currentStore.templateId || 'tpl-modern-1';
    const cfg = resolveTemplateConfig(templateId, currentStore.templateConfig);
    const custom = templateCustom(cfg);
    const groups = groupProductsByCategory(storefrontProducts(currentStore, navigationProducts ?? products), categories, custom.categoryOverrides);
    const navItems = groups.slice(0, 3).map(g => ({ title: g.title, slug: g.slug }));
    const storeSlug = routeStoreSlug || currentStore.slug;
    const homePath = pathForView('seller_storefront', { storeSlug });
    const sections = orderedEnabledSections(cfg);
    const hasCatalogue = sections.includes('products') || sections.includes('allProducts');
    const jump = (event: React.MouseEvent<HTMLAnchorElement>, target: string, slug?: string) => followStoreLink(event, () => {
      setFashionMenuOpen(false);
      if (slug) {
        clearCatalogBrand(); setSearchQuery(''); setSelectedCategory(slug);
      }
      if (activeView !== 'seller_storefront') goToStoreHome();
      const url = new URL(window.location.href);
      url.hash = target;
      window.history.replaceState(window.history.state, '', url);
      const element = document.getElementById(target);
      element?.scrollIntoView({ block: 'start' });
      if (element instanceof HTMLInputElement) element.focus({ preventScroll: true });
    });
    const accountLink = (view: 'customer_profile' | 'customer_wishlist') => (event: React.MouseEvent<HTMLAnchorElement>) => {
      followStoreLink(event, () => {
        setFashionMenuOpen(false);
        setActiveView(view, { storeSlug });
      });
    };
    return <>
      {sectionEnabled(cfg, 'announcement') && custom.announcementText && <div className="fashion-announcement" data-storefront-theme={templateId} data-testid="store-announcement">
        {custom.announcementLink ? <a href={custom.announcementLink} {...(/^https?:\/\//i.test(custom.announcementLink) ? { target: '_blank', rel: 'noreferrer' } : {})}>{custom.announcementText}</a> : custom.announcementText}
      </div>}
      <header className="fashion-header" data-storefront-theme={templateId} onKeyDown={event => {
      if (event.key !== 'Escape' || !fashionMenuOpen) return;
      setFashionMenuOpen(false);
      fashionMenuButton.current?.focus();
    }}>
      <div className="fashion-header-row">
        <button ref={fashionMenuButton} type="button" className="fashion-icon-button fashion-mobile-toggle" onClick={() => setFashionMenuOpen(v => !v)} aria-label={fashionMenuOpen ? 'بستن منو' : 'باز کردن منو'} aria-expanded={fashionMenuOpen} aria-controls={fashionMenuId}>
          {fashionMenuOpen ? <X size={22} strokeWidth={1.5} /> : <Menu size={22} strokeWidth={1.5} />}
        </button>
        <a href={homePath} className="fashion-wordmark" onClick={event => followStoreLink(event, goToStoreHome)} aria-label={`خانه ${currentStore.name}`}>
          {currentStore.logo && !logoFailed && <img src={currentStore.logo} alt="" width={36} height={36} onError={() => setLogoFailed(true)} />}
          <span>{currentStore.name}</span>
        </a>
        <nav className="fashion-header-nav" aria-label="منوی فروشگاه">
          {hasCatalogue && <a href={homePath + '#products'} onClick={event => jump(event, 'products', 'all')}>همه کالاها</a>}
          {hasCatalogue && navItems.map(item => <a key={item.slug} href={homePath + '#products'} onClick={event => jump(event, 'products', item.slug)}>{item.title}</a>)}
          <button type="button" onClick={() => setIsTrackingModalOpen(true)}>پیگیری سفارش</button>
          <a href={homePath + '#fashion-contact'} onClick={event => jump(event, 'fashion-contact')}>تماس با ما</a>
        </nav>
        <div className="fashion-header-actions">
          {hasCatalogue && <a href={homePath + '#fashion-search'} onClick={event => jump(event, 'fashion-search')} className="fashion-icon-button" aria-label="جست‌وجو در ویترین"><Search size={21} strokeWidth={1.5} aria-hidden="true" /></a>}
          <a href={pathForView('customer_wishlist', { storeSlug })} onClick={accountLink('customer_wishlist')} className="fashion-icon-button fashion-desktop-action" aria-label="علاقه‌مندی‌ها"><Heart size={21} strokeWidth={1.5} aria-hidden="true" /></a>
          <a href={pathForView('customer_profile', { storeSlug })} onClick={accountLink('customer_profile')} className="fashion-icon-button fashion-desktop-action" aria-label="حساب کاربری"><User size={21} strokeWidth={1.5} aria-hidden="true" /></a>
          <button type="button" onClick={() => setIsNotifOpen(true)} className="fashion-icon-button fashion-extra-action" aria-label="اعلان‌ها"><Bell size={21} strokeWidth={1.5} aria-hidden="true" /></button>
          <button type="button" onClick={() => setIsCartOpen(true)} className="fashion-icon-button" aria-label="سبد خرید">
            <ShoppingBag size={21} strokeWidth={1.5} />
            {cartItemsCount > 0 && <span className="absolute top-0 right-0 min-w-4 h-4 bg-[#1c1c1a] text-white text-[9px] flex items-center justify-center rounded-full">{cartItemsCount > 99 ? '۹۹+' : toPersianDigits(cartItemsCount)}</span>}
          </button>
        </div>
      </div>
      <nav id={fashionMenuId} hidden={!fashionMenuOpen} className="fashion-header-menu" aria-label="منوی موبایل">
        {hasCatalogue && <a href={homePath + '#products'} onClick={event => jump(event, 'products', 'all')}>همه کالاها</a>}
        {hasCatalogue && navItems.map(item => <a key={item.slug} href={homePath + '#products'} onClick={event => jump(event, 'products', item.slug)}>{item.title}</a>)}
        <button type="button" onClick={() => { setFashionMenuOpen(false); setIsTrackingModalOpen(true); }}>پیگیری سفارش</button>
        <button type="button" onClick={() => { setFashionMenuOpen(false); setIsNotifOpen(true); }}>اعلان‌ها</button>
        <a href={homePath + '#fashion-contact'} onClick={event => jump(event, 'fashion-contact')}>تماس با ما</a>
        <a href={pathForView('customer_wishlist', { storeSlug })} onClick={accountLink('customer_wishlist')}>علاقه‌مندی‌ها</a>
        <a href={pathForView('customer_profile', { storeSlug })} onClick={accountLink('customer_profile')}>حساب کاربری</a>
      </nav>
    </header><NotificationDrawer isOpen={isNotifOpen} onClose={() => setIsNotifOpen(false)} /></>;
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
