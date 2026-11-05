import React from 'react';
import { useStore } from '../../context/StoreContext';
import { Store, Phone, Instagram, Home, LayoutGrid, User, MessageCircle, X } from 'lucide-react';
import { storefrontProducts } from './templates/templateTypes';
import { ProductCard } from '../ProductCard';

// FIX 13: live's storefront footer (bundle component z4) — dark #1C1917
// rounded-top footer with the about line, store navigation, trust
// links, and the fixed bottom nav (bundle component I4). The old
// Shaparak line is dropped: live does not have it. Contact row only
// when phone/instagram are configured (none of the three observed
// stores configure them, matching live). The bottom nav is visible at
// all sizes (live's I4 has no responsive hiding), centred, max 504px.
const ACCENT = '#0D7377';

export const SellerStorefrontFooter: React.FC = () => {
  const { currentStore, openStaticPage, toPersianDigits, products } = useStore();
  // FIX 22: live storefront footer «محصولات» opens an in-page
  // «همه محصولات» modal (no navigation); empty stores show 0 items
  // + «هنوز محصولی در این فروشگاه ثبت نشده است.». Non-empty list
  // content is UNPROVEN (no non-empty store modal was observed).
  const [allProductsOpen, setAllProductsOpen] = React.useState(false);
  const storeItems = storefrontProducts(currentStore, products);
  const cfg = (currentStore.templateConfig || {}) as Record<string, unknown>;
  const custom = (cfg.custom || {}) as Record<string, unknown>;
  const phone = (cfg.contactPhone as string | undefined) || (custom.contactPhone as string | undefined) || '';
  const instagram = (cfg.contactInstagram as string | undefined) || (custom.contactInstagram as string | undefined) || '';
  const instagramHandle = instagram.replace(/^https?:\/\/(www\.)?instagram\.com\//i, '').replace(/^@/, '').replace(/\/$/, '');
  const year = (() => {
    try {
      return new Intl.DateTimeFormat('fa-IR', { year: 'numeric' }).format(new Date());
    } catch {
      return '۱۴۰۵';
    }
  })();

  const scrollToTop = () => window.scrollTo({ top: 0, behavior: 'smooth' });
  const scrollToBottom = () => window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' });

  return (
    <>
    <footer className="rounded-t-[32px] mt-2" style={{ backgroundColor: '#1C1917' }}>
      <div className="px-6 pt-8 pb-[84px]">
        <div className="flex items-center gap-2.5">
          <div
            className="w-10 h-10 rounded-full flex items-center justify-center"
            style={{ backgroundColor: `${ACCENT}26` }}
          >
            <Store className="w-5 h-5" strokeWidth={1.5} style={{ color: ACCENT }} />
          </div>
          <h2 className="text-lg font-black text-white">{currentStore.name}</h2>
        </div>
        <p className="text-[13px] text-stone-400 leading-6 mt-3 max-w-[300px]">
          {currentStore.footerText && currentStore.footerText.trim() !== ''
            ? currentStore.footerText
            : `ویترین اختصاصی ${currentStore.name} — خرید مستقیم، ارسال سریع و ضمانت تست ۷ روزه.`}
        </p>
        {(phone !== '' || instagram !== '') && (
          <div className="flex flex-wrap gap-2 mt-5">
            {phone !== '' && (
              <a
                href={`tel:${phone}`}
                className="inline-flex items-center gap-2 rounded-full border border-white/15 px-4 py-2 text-[13px] text-stone-200 transition-colors hover:border-white/30"
              >
                <Phone className="w-4 h-4" style={{ color: ACCENT }}  strokeWidth={1.5}/>
                <span dir="ltr">{phone}</span>
              </a>
            )}
            {instagram !== '' && (
              <a
                href={`https://instagram.com/${instagramHandle}`}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-2 rounded-full border border-white/15 px-4 py-2 text-[13px] text-stone-200 transition-colors hover:border-white/30"
              >
                <Instagram className="w-4 h-4" style={{ color: ACCENT }}  strokeWidth={1.5}/>
                <span dir="ltr">@{instagramHandle}</span>
              </a>
            )}
          </div>
        )}
        <nav aria-label="ناوبری فروشگاه" className="flex items-center gap-5 mt-6">
          <button onClick={scrollToTop} className="text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            خانه
          </button>
          <button onClick={() => setAllProductsOpen(true)} className="text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            محصولات
          </button>
          <button onClick={scrollToBottom} className="text-[13px] font-bold text-stone-400 transition-colors hover:text-white">
            تماس
          </button>
        </nav>
        <nav aria-label="لینک‌های اعتماد" className="flex flex-wrap items-center gap-x-5 gap-y-2 mt-5">
          <button onClick={() => openStaticPage('about')} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            درباره ما
          </button>
          <button onClick={() => openStaticPage('terms')} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            شرایط استفاده
          </button>
          <button onClick={() => openStaticPage('privacy')} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            حریم خصوصی
          </button>
          <button onClick={() => openStaticPage('faq')} className="text-[11px] text-stone-500 transition-colors hover:text-stone-200">
            پرسش‌های متداول
          </button>
        </nav>
        <div className="border-t border-white/10 mt-6 pt-4">
          <p className="text-[11px] text-stone-500">
            © {toPersianDigits(year)} {currentStore.name} — تمامی حقوق محفوظ است.
          </p>
        </div>
      </div>
    </footer>
    {allProductsOpen && (
      <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none" onClick={() => setAllProductsOpen(false)}>
        <div className="bg-white rounded-2xl w-full max-w-lg max-h-[85vh] flex flex-col overflow-hidden" onClick={(e) => e.stopPropagation()}>
          <div className="flex items-center justify-between px-4 py-3 border-b border-slate-100">
            <h3 className="text-sm font-extrabold text-slate-900">
              همه محصولات <span className="text-xs font-bold text-slate-400">({toPersianDigits(storeItems.length)} کالا)</span>
            </h3>
            <button onClick={() => setAllProductsOpen(false)} aria-label="بستن" className="w-8 h-8 flex items-center justify-center rounded-full text-slate-400 hover:bg-slate-100 hover:text-slate-700 transition-colors">
              <X className="w-5 h-5"  strokeWidth={1.5}/>
            </button>
          </div>
          <div className="overflow-y-auto p-3">
            {storeItems.length === 0 ? (
              <p className="text-sm text-slate-500 text-center py-8">هنوز محصولی در این فروشگاه ثبت نشده است.</p>
            ) : (
              <div className="grid grid-cols-2 gap-2.5">
                {storeItems.map((p) => (
                  <ProductCard key={p.id} product={p} />
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    )}
    </>
  );
};

// Storefront bottom nav (bundle component I4): fixed, centred,
// max 504px. Tabs: خانه / دسته‌بندی‌ها / حساب من / تماس. Active tab:
// accent tint background + accent text. Mobile/tablet only (owner
// order, Nov 5, 2026): on desktop the pill overlapped hero titles and
// footer content, and the store header already carries these actions
// there, so it is hidden at lg and up.
export const StorefrontBottomNav: React.FC = () => {
  const { setActiveView, currentStore } = useStore();
  const [active, setActive] = React.useState<'home' | 'categories' | 'account' | 'contact'>('home');

  const scrollToTop = () => window.scrollTo({ top: 0, behavior: 'smooth' });
  const go = (tab: 'home' | 'categories' | 'account' | 'contact') => {
    setActive(tab);
    if (tab === 'home') scrollToTop();
    else if (tab === 'account') setActiveView('customer_profile');
    else if (tab === 'contact') window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' });
    else {
      const el = document.querySelector('[aria-label="دسته‌بندی‌ها"]');
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      else scrollToTop();
    }
  };

  const tabs = [
    { id: 'home' as const, label: 'خانه', Icon: Home },
    { id: 'categories' as const, label: 'دسته‌بندی‌ها', Icon: LayoutGrid },
    { id: 'account' as const, label: 'حساب من', Icon: User },
    { id: 'contact' as const, label: 'تماس', Icon: MessageCircle },
  ];

  return (
    <nav
      className="fixed bottom-0 z-30 border-t border-x lg:hidden"
      style={{
        left: 0,
        right: 0,
        marginLeft: 'auto',
        marginRight: 'auto',
        width: '100%',
        maxWidth: 504,
        backgroundColor: '#ffffff',
        borderColor: '#ECE4D6',
        boxShadow: '0 -6px 24px rgba(28, 25, 23, 0.08)',
        paddingBottom: 'env(safe-area-inset-bottom)',
      }}
    >
      <div className="flex items-stretch justify-center px-2 py-1.5">
        {tabs.map(({ id, label, Icon }) => {
          const isActive = active === id;
          return (
            <button
              key={id}
              onClick={() => go(id)}
              className="flex-1 min-w-0 flex flex-col items-center justify-center gap-1 px-1 py-2 rounded-2xl transition-all duration-200 ease-out active:scale-95"
              style={{
                backgroundColor: isActive ? `${ACCENT}16` : 'transparent',
                color: isActive ? ACCENT : '#A8A29E',
              }}
            >
              <Icon className="w-[22px] h-[22px]" strokeWidth={isActive ? 2 : 1.5} />
              <span className="text-[10px] font-bold leading-none truncate max-w-full">{label}</span>
            </button>
          );
        })}
      </div>
    </nav>
  );
};
