import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { 
  ArrowRight, 
  LayoutDashboard, 
  Store, 
  Package, 
  DollarSign, 
  Palette, 
  ShoppingBag, 
  Link2, 
  Wallet, 
  Crown, 
  Megaphone, 
  Headphones,
  LogOut,
  Plus,
  X,
  LayoutTemplate,
  BarChart3,
  Search,
  Percent,
  Mail,
  Tag,
  RotateCcw
} from 'lucide-react';

import { SellerAuthGuard } from './SellerAuthGuard';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { validateStoreSlug } from '../../utils/validators';
import { SellerOverviewTab } from './SellerOverviewTab';
import { SellerStoreSetupTab } from './SellerStoreSetupTab';
import { SellerCatalogTab } from './SellerCatalogTab';
import { SellerPricingTab } from './SellerPricingTab';
import { SellerBuilderTab } from './SellerBuilderTab';
import { SellerOrdersTab } from './SellerOrdersTab';
import { SellerReturnsTab } from './SellerReturnsTab';
import { SellerLinksTab } from './SellerLinksTab';
import { SellerWalletTab } from './SellerWalletTab';
import { SellerSubscriptionTab } from './SellerSubscriptionTab';
import { SellerAdsWidgetTab } from './SellerAdsWidgetTab';
import { SellerSupportTab } from './SellerSupportTab';
import { SellerReferralTab } from './SellerReferralTab';
import { SellerTemplateTab } from './SellerTemplateTab';
import { SellerAnalyticsTab } from './SellerAnalyticsTab';
import { SellerSearchInsightTab } from './SellerSearchInsightTab';
import { SellerDiscountCampaignsTab } from './SellerDiscountCampaignsTab';
import { SellerDiscountTab } from './SellerDiscountTab';
import { SellerSubscribersTab } from './SellerSubscribersTab';
import { isSellerDashboardTab } from '../../utils/routing';
import { Users, ShieldCheck } from 'lucide-react';

interface SellerDashboardViewProps {
  onBackToCustomerSite: () => void;
  onOpenStorefront: () => void;
}

// crypto.randomUUID exists only in secure contexts (HTTPS/localhost); the live and preview origins are plain HTTP, so fall back instead of throwing. Uniqueness is all the call sites need.
const newUuid = (): string => {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch { /* fall through to the fallback */ }
  return 'id-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2);
};

export const SellerDashboardView: React.FC<SellerDashboardViewProps> = ({
  onBackToCustomerSite,
  onOpenStorefront,
}) => {
  const { currentStore, setCurrentStore, stores, addStore, platformSettings, sellerSession, logoutSeller, toPersianDigits, sellerDashboardTab, setSellerDashboardTab, setActiveView } = useStore();
  const { info, success, error } = useToast();
  // Team 08 (D47): tab state lives in the router context so tab URLs
  // (?panel=seller&tab=<tab>) survive back/forward and deep links.
  const activeTab = sellerDashboardTab;
  const setActiveTab = (tab: string) => {
    if (!isSellerDashboardTab(tab)) return;
    setSellerDashboardTab(tab);
    if (typeof window !== 'undefined') {
      window.history.replaceState(null, '', `/?panel=seller&tab=${tab}`);
    }
  };
  const [showStoreForm, setShowStoreForm] = useState(false);
  const [newStoreName, setNewStoreName] = useState('');
  const [newStoreSlug, setNewStoreSlug] = useState('');
  const [creatingStore, setCreatingStore] = useState(false);
  // W5 (live parity): the live create-store form renders the contract
  // block (Hs) and gates submit on its acceptance.
  const [createContractAccepted, setCreateContractAccepted] = useState(false);
  const createContractText = platformSettings.contracts?.sellerContractText || '';
  const createContractVersion = platformSettings.contracts?.sellerContractVersion || 0;
  const createContractAcceptedEffective = createContractAccepted && !!createContractText.trim();

  const ownedStores = stores.filter((store) => store.ownerType === 'dropshipper' && (!isMockMode() || store.id === sellerSession?.storeId || store.ownerName === currentStore.ownerName)).map((store) => store.id === currentStore.id ? currentStore : store);
  const maxStores = ownedStores.some((store) => store.activePlanId === 'vip') ? 10 : 1;
  const canCreateStore = ownedStores.length < maxStores;

  const handleCreateStore = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!createContractAcceptedEffective) return;
    if (!canCreateStore) {
      error('ظرفیت فروشگاه‌های پلن شما تکمیل شده است. پلن VIP امکان ساخت تا ۱۰ فروشگاه را دارد.');
      return;
    }
    if (!newStoreName.trim()) {
      error('نام فروشگاه را وارد کنید.');
      return;
    }
    const slugValidation = validateStoreSlug(newStoreSlug);
    if (!slugValidation.valid) {
      error(slugValidation.error || 'نامک (Slug) انگلیسی معتبر نیست.');
      return;
    }
    setCreatingStore(true);
    try {
      const createdStore = isMockMode() ? {
        ...currentStore,
        id: `store-${newUuid()}`,
        name: newStoreName.trim(),
        slug: newStoreSlug.trim().toLowerCase(),
        activePlanId: 'vip' as const,
        tagline: '',
        description: '',
        totalSales: 0,
        monthlyRevenueEstimate: 0,
        activeCategories: [],
        activeProductIds: [],
        selectedNicheIds: [],
        builderConfig: undefined,
        customDomain: undefined,
        referralCode: undefined,
        productPriceOverrides: {},
        nichePriceOverrides: {},
      } : await storeApi.registerStore({
        storeName: newStoreName.trim(),
        slug: newStoreSlug.trim().toLowerCase(),
        ownerName: sellerSession?.fullName || currentStore.ownerName,
        profitMarginPercent: currentStore.profitMarginPercent,
      });
      addStore(createdStore);
      setNewStoreName('');
      setNewStoreSlug('');
      setShowStoreForm(false);
      setCreateContractAccepted(false);
      success(`فروشگاه «${createdStore.name}» با موفقیت ساخته شد.`);
    } catch (err: any) {
      error(err?.message || 'ساخت فروشگاه انجام نشد.');
    } finally {
      setCreatingStore(false);
    }
  };
  const [reconfirmContractAccepted, setReconfirmContractAccepted] = useState(false);

  // Item 7: Digital contract version check
  const activeSellerContractVer = platformSettings.contracts?.sellerContractVersion || 2;
  const currentStoreContractVer = currentStore.signedContractVersion || 1;
  const needsContractReconfirmation = currentStoreContractVer < activeSellerContractVer;

  // FIX 69 (G2): the reconfirm modal must persist acceptance via the
  // store-settings save path (same signature as SellerStoreSetupTab),
  // not only local state, or acceptance is lost on reload. The backend
  // column write for signedContractVersion is a separate worker's job;
  // local state is applied only after a successful save so a failed
  // save leaves the modal open instead of falsely closing it.
  const handleReconfirmContract = async () => {
    if (!reconfirmContractAccepted) return;
    if (!isMockMode() && currentStore.slug) {
      try {
        await storeApi.saveStoreSettings(currentStore.slug, { signedContractVersion: activeSellerContractVer });
      } catch (err: any) {
        error(err?.message || 'ثبت تایید قرارداد در سرور انجام نشد.');
        return;
      }
    }
    setCurrentStore({
      ...currentStore,
      signedContractVersion: activeSellerContractVer,
    });
    success(`نسخه به‌روزشده قرارداد (${toPersianDigits(activeSellerContractVer)}.۰) با موفقیت تأیید گردید.`);
  };
  // FIX 17: live seller menu (SellerDashboardView-MERGED.js, single
  // menu definition): 18 live tabs in this exact order, with live
  // labels. 'returns' (3rd) was missing; 12 labels differed.
  // 'discount-codes' after 'discount-campaigns' is a DELIBERATE
  // Phase-3 addition (user's margin-only discount-code rule), NOT
  // a live tab -- kept on purpose, so the candidate shows 19 tabs.
  const menuItems = [
    { id: 'overview', label: 'نمای کلی', icon: LayoutDashboard },
    { id: 'orders', label: 'سفارش‌ها', icon: ShoppingBag },
    { id: 'returns', label: 'مرجوعی‌ها', icon: RotateCcw },
    { id: 'catalog', label: 'انتخاب کالا', icon: Package },
    { id: 'template', label: 'ظاهر فروشگاه', icon: LayoutTemplate },
    { id: 'pricing', label: 'سود من', icon: DollarSign },
    { id: 'wallet', label: 'کیف پول', icon: Wallet },
    { id: 'store-setup', label: 'مشخصات فروشگاه', icon: Store },
    { id: 'links', label: 'لینک و کد QR', icon: Link2 },
    { id: 'analytics', label: 'آمار فروش', icon: BarChart3 },
    { id: 'search-insight', label: 'اینسایت جستجو', icon: Search },
    { id: 'discount-campaigns', label: 'کمپین تخفیف', icon: Percent },
    { id: 'discount-codes', label: 'کدهای تخفیف', icon: Tag },
    { id: 'subscribers', label: 'مشترک‌های فروشگاه', icon: Mail },
    { id: 'referral', label: 'دعوت و پاداش', icon: Users },
    { id: 'subscription', label: 'اشتراک فروشگاه', icon: Crown },
    { id: 'ads', label: 'تبلیغات', icon: Megaphone },
    { id: 'builder', label: 'صفحه‌ساز', icon: Palette },
    { id: 'support', label: 'پشتیبانی', icon: Headphones },
  ];

  return (
    <SellerAuthGuard onBackToCustomerSite={onBackToCustomerSite}>
      <div className="flex-1 flex flex-col bg-[#f8fafc]">
        {/* Top Sticky Header */}
        <header className="sticky top-0 z-30 bg-white border-b border-slate-200/80">
          <div className="max-w-7xl mx-auto w-full flex flex-wrap gap-2 items-center justify-between px-3.5 py-2.5">
          <div className="flex items-center gap-2">
            <button
              onClick={onBackToCustomerSite}
              className="p-2.5 rounded-xl hover:bg-slate-100 text-slate-600 transition-colors"
              title="بازگشت به فروشگاه من"
              aria-label="بازگشت به فروشگاه من"
            >
              <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
            </button>
            <div>
              <h1 className="text-[13px] font-bold text-slate-800">پنل فروشگاه من</h1>
              <span className="text-[11px] text-slate-400 block">
                {sellerSession?.fullName} • {toPersianDigits(sellerSession?.mobile || '')}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-1.5">
            <select
              value={currentStore.id}
              onChange={(event) => {
                const selected = ownedStores.find((store) => store.id === event.target.value);
                if (selected) setCurrentStore(selected);
              }}
              className="max-w-[150px] rounded-xl border border-slate-200 bg-slate-50 px-2 py-1.5 text-[11px] font-bold text-slate-700"
              aria-label="فروشگاه فعال"
            >
              {ownedStores.map((store) => (
                <option key={store.id} value={store.id}>{store.name}</option>
              ))}
            </select>
            {canCreateStore && (
              <button
                onClick={() => setShowStoreForm(true)}
                className="flex items-center gap-1 rounded-xl bg-brand-soft px-2.5 py-1.5 text-[11px] font-bold text-brand-deep hover:bg-brand-border"
                title="ساخت فروشگاه جدید"
              >
                <Plus className="h-3.5 w-3.5"  strokeWidth={1.5}/>
                <span>فروشگاه جدید</span>
              </button>
            )}
            <button
              onClick={() => {
                const slug = currentStore?.slug;
                logoutSeller();
                info('از حساب کاربری خود خارج شدید.');
                if (slug) setActiveView('seller_storefront', { storeSlug: slug });
                else setActiveView('customer');
              }}
              className="flex items-center gap-1 px-2.5 py-2 rounded-xl bg-rose-50 hover:bg-rose-100 text-rose-600 transition-colors text-[11px] font-bold"
              title="خروج از حساب"
            >
              <LogOut className="w-4 h-4"  strokeWidth={1.5}/>
              <span>خروج</span>
            </button>
          </div>
          </div>
        </header>

        {showStoreForm && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-4">
            <form onSubmit={handleCreateStore} className="w-full max-w-sm max-h-[90vh] overflow-y-auto space-y-3 rounded-3xl bg-white p-5 shadow-2xl">
              <div className="flex items-center justify-between">
                <h2 className="text-sm font-black text-slate-900">ساخت فروشگاه جدید</h2>
                <button type="button" onClick={() => { setShowStoreForm(false); setCreateContractAccepted(false); }} className="rounded-lg p-1 text-slate-400 hover:bg-slate-100" aria-label="بستن"><X className="h-4 w-4"  strokeWidth={1.5}/></button>
              </div>
              <p className="text-[11px] leading-6 text-slate-500">در پلن فعلی تا {toPersianDigits(maxStores)} فروشگاه می‌توانید با همین حساب بسازید.</p>
              <div>
                <label className="block text-xs font-bold text-slate-700 mb-1">نام فروشگاه</label>
                <input value={newStoreName} onChange={(event) => setNewStoreName(event.target.value)} placeholder="مثلاً: فروشگاه مد رها" className="w-full rounded-xl border border-slate-200 bg-slate-50 p-3 text-[13px]" />
              </div>
              <div>
                <label className="block text-xs font-bold text-slate-700 mb-1">آدرس انگلیسی فروشگاه</label>
                <input value={newStoreSlug} onChange={(event) => setNewStoreSlug(event.target.value)} placeholder="مثلاً: raha-shop" dir="ltr" className="w-full rounded-xl border border-slate-200 bg-slate-50 p-3 text-[13px]" />
                <span className="text-[11px] text-slate-400 mt-1 block">مشتری‌ها با این آدرس وارد فروشگاهت می‌شوند؛ فقط حرف و عدد انگلیسی و خط تیره.</span>
              </div>
              <div className="space-y-2.5 rounded-2xl border border-slate-200 bg-slate-50/60 p-3">
                <div className="flex items-center gap-2 text-brand-deep">
                  <h4 className="text-xs font-bold text-slate-800">قرارداد همکاری را بخوان و قبول کن</h4>
                  {createContractText.trim() && (
                    <span className="text-[9px] bg-brand-border text-brand-deep px-1.5 py-0.5 rounded font-mono font-bold">نسخه {toPersianDigits(createContractVersion)}</span>
                  )}
                </div>
                <p className="text-[11px] text-slate-500 leading-relaxed">ساخت فروشگاه یعنی قبول قرارداد همکاری زیر؛ اول متنش را کامل بخوان:</p>
                <div className="p-3 bg-white border border-slate-200 rounded-2xl max-h-44 overflow-y-auto text-[11px] text-slate-700 leading-relaxed whitespace-pre-line">
                  {createContractText.trim() ? createContractText : 'متن قرارداد الان از سرور نرسید؛ چند لحظه دیگر دوباره تلاش کن.'}
                </div>
                <label className={`flex items-start gap-2 p-2.5 rounded-xl border border-slate-200 bg-white ${createContractText.trim() ? 'cursor-pointer' : 'cursor-not-allowed opacity-60'}`}>
                  <input
                    type="checkbox"
                    checked={createContractAcceptedEffective}
                    disabled={!createContractText.trim()}
                    onChange={(e) => setCreateContractAccepted(e.target.checked)}
                    className="w-4 h-4 accent-[#5d44f8] rounded mt-0.5 cursor-pointer shrink-0 disabled:cursor-not-allowed"
                  />
                  <span className="text-[11px] text-slate-800 leading-relaxed font-bold">
                    قرارداد همکاری را کامل خواندم و همه‌اش را قبول دارم.
                  </span>
                </label>
              </div>
              <button type="submit" disabled={creatingStore || !createContractAcceptedEffective} className="w-full rounded-xl bg-brand-deep py-3 text-[13px] font-bold text-white disabled:opacity-50">{creatingStore ? 'در حال ساخت…' : 'ساخت فروشگاه'}</button>
            </form>
          </div>
        )}
      {/* Horizontal Scrollable Tabs */}
      <div className="bg-white border-b border-slate-200/80 px-2 py-2 flex items-center gap-1.5 overflow-x-auto no-scrollbar">
        {menuItems.map((item) => {
          const Icon = item.icon;
          const isActive = activeTab === item.id;
          return (
            <button
              key={item.id}
              onClick={() => setActiveTab(item.id)}
              className={`flex items-center gap-1.5 px-3.5 py-2.5 min-h-[44px] rounded-xl text-xs font-bold whitespace-nowrap transition-all shrink-0 ${
                isActive
                  ? 'bg-brand text-white shadow-xs'
                  : 'bg-slate-50 text-slate-600 hover:bg-slate-100'
              }`}
            >
              <Icon className="w-4 h-4" strokeWidth={1.5} />
              <span>{item.label}</span>
            </button>
          );
        })}
      </div>

      {/* Main Tab Content */}
        {/* Item 7: Mandatory Re-confirmation Modal for Updated Contracts */}
        {needsContractReconfirmation && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
            <div className="bg-white rounded-3xl max-w-md w-full p-4 shadow-2xl border border-slate-100 space-y-3 text-xs">
              <div className="flex items-center gap-2 text-indigo-700 font-bold border-b border-slate-100 pb-2">
                <ShieldCheck className="w-5 h-5 text-indigo-600"  strokeWidth={1.5}/>
                <div>
                  <h3 className="text-xs font-bold text-slate-900">قرارداد همکاری عوض شده است</h3>
                  <span className="text-[10px] text-slate-400 block font-normal">
                    نسخه جدید {toPersianDigits(activeSellerContractVer)}.۰ (جایگزین نسخه {toPersianDigits(currentStoreContractVer)}.۰)
                  </span>
                </div>
              </div>

              <p className="text-[11px] text-slate-600 leading-relaxed">
                قوانین همکاری فروشگاه‌ها به‌روز شده است. برای ادامه کار فروشگاهت، متن جدید را بخوان و قبول کن:
              </p>

              <div className="p-3 bg-slate-50 border border-slate-200 rounded-2xl max-h-40 overflow-y-auto text-[11px] text-slate-700 leading-relaxed font-mono whitespace-pre-line no-scrollbar">
                {platformSettings.contracts?.sellerContractText}
              </div>

              <label className="flex items-start gap-2 p-2.5 rounded-xl border border-slate-200 bg-slate-50 cursor-pointer">
                <input
                  type="checkbox"
                  checked={reconfirmContractAccepted}
                  onChange={(e) => setReconfirmContractAccepted(e.target.checked)}
                  className="w-4 h-4 accent-indigo-600 rounded mt-0.5"
                />
                <span className="text-[11px] text-slate-800 leading-relaxed font-bold">
                  قوانین جدید همکاری را خواندم و قبول دارم.
                </span>
              </label>

              <button
                disabled={!reconfirmContractAccepted}
                onClick={handleReconfirmContract}
                className={`w-full py-2.5 font-bold text-xs rounded-xl shadow-xs transition-all ${
                  reconfirmContractAccepted
                    ? 'bg-indigo-600 hover:bg-indigo-700 text-white'
                    : 'bg-slate-200 text-slate-400 cursor-not-allowed'
                }`}
              >
                تأیید نسخه جدید و ادامه فعالیت
              </button>
            </div>
          </div>
        )}

      <div className="p-3.5 flex-1">
        {activeTab === 'overview' && (
        <section className="mb-4 space-y-3 rounded-2xl border border-slate-200 bg-white p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-sm font-bold">فروشگاه‌های من · {toPersianDigits(ownedStores.length)} از {toPersianDigits(maxStores)}</h2>
            <button type="button" onClick={() => canCreateStore ? setShowStoreForm(true) : setActiveTab('subscription')} className="flex items-center gap-1 rounded-xl bg-brand-soft px-3 py-2.5 min-h-[44px] text-xs font-bold text-brand-deep hover:bg-brand-border"><Plus className="h-4 w-4"  strokeWidth={1.5}/>{canCreateStore ? 'ساخت فروشگاه جدید' : 'ارتقا برای ساخت فروشگاه بیشتر'}</button>
          </div>
          <p className="text-xs leading-6 text-slate-500">هر فروشگاه اسم و آدرس خودش را دارد. برای کار با هر فروشگاه، اول آن را این‌جا انتخاب کن.</p>
          <div className="flex flex-wrap gap-2">
            {ownedStores.map((store) => <button key={store.id} type="button" onClick={() => setCurrentStore(store)} aria-pressed={store.id === currentStore.id} className={`rounded-xl border px-3 py-2.5 min-h-[44px] text-right text-xs ${store.id === currentStore.id ? 'border-brand-deep bg-brand-soft text-brand-deep' : 'border-slate-200 text-slate-600'}`}><span className="block font-bold">{store.name}{store.id === currentStore.id ? ' · فروشگاه فعلی' : ''}</span><span className="mt-1 block font-mono" dir="ltr">/{store.slug}</span></button>)}
          </div>
        </section>
        )}
        {activeTab === 'overview' && (
          <SellerOverviewTab 
            onNavigateTab={setActiveTab} 
            onOpenStorefront={onOpenStorefront} 
          />
        )}
        {activeTab === 'store-setup' && <SellerStoreSetupTab key={currentStore.id} />}
        {activeTab === 'catalog' && <SellerCatalogTab />}
        {activeTab === 'pricing' && <SellerPricingTab />}
        {activeTab === 'builder' && <SellerBuilderTab onOpenStorefront={onOpenStorefront} onNavigateTab={setActiveTab} />}
        {activeTab === 'orders' && <SellerOrdersTab onNavigateTab={setActiveTab} onOpenStorefront={onOpenStorefront} />}
        {activeTab === 'returns' && <SellerReturnsTab />}
        {activeTab === 'links' && <SellerLinksTab />}
        {activeTab === 'wallet' && <SellerWalletTab />}
        {activeTab === 'referral' && <SellerReferralTab />}
        {activeTab === 'subscription' && <SellerSubscriptionTab onManageStores={() => { setActiveTab('overview'); if (canCreateStore) setShowStoreForm(true); }} />}
        {activeTab === 'ads' && <SellerAdsWidgetTab key={currentStore.id} />}
        {activeTab === 'support' && <SellerSupportTab />}
        {activeTab === 'template' && <SellerTemplateTab />}
        {activeTab === 'analytics' && <SellerAnalyticsTab />}
        {activeTab === 'search-insight' && <SellerSearchInsightTab />}
        {activeTab === 'discount-campaigns' && <SellerDiscountCampaignsTab />}
        {activeTab === 'discount-codes' && <SellerDiscountTab />}
        {activeTab === 'subscribers' && <SellerSubscribersTab onOpenStorefront={onOpenStorefront} />}
      </div>
      </div>
    </SellerAuthGuard>
  );
};
