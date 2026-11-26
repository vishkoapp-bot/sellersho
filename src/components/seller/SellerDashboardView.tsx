import { useEffect, useRef, useState } from 'react';
import { ChevronDown, ExternalLink, Plus, Store as StoreIcon } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest, isMockMode } from '../../services/apiClient';
import { sellerNotificationsApi } from '../../services/notificationsApi';
import { storeApi } from '../../services/storeApi';
import { newUuid } from '../../services/uuid';
import type { PlatformContracts, Store } from '../../types';
import { isSellerDashboardTab } from '../../utils/routing';
import { ErrorBoundary } from '../ErrorBoundary';
import { PanelWorkspace } from '../common/PanelWorkspace';
import { RoleNotificationDrawer } from '../common/RoleNotificationDrawer';
import { SellerAdsWidgetTab } from './SellerAdsWidgetTab';
import { SellerAnalyticsTab } from './SellerAnalyticsTab';
import { SellerAuthGuard } from './SellerAuthGuard';
import { SellerBuilderTab } from './SellerBuilderTab';
import { SellerCatalogTab } from './SellerCatalogTab';
import { SellerDiscountCampaignsTab } from './SellerDiscountCampaignsTab';
import { SellerDiscountTab } from './SellerDiscountTab';
import { SellerLinksTab } from './SellerLinksTab';
import { sellerNavigation, sellerSectionHref, type SellerWorkFilter } from './SellerNavigation';
import { SellerOrdersTab } from './SellerOrdersTab';
import { SellerOverviewTab } from './SellerOverviewTab';
import { SellerPricingTab } from './SellerPricingTab';
import { SellerReferralTab } from './SellerReferralTab';
import { SellerReturnsTab } from './SellerReturnsTab';
import { SellerSearchInsightTab } from './SellerSearchInsightTab';
import { SellerCreateStoreDialog, SellerRenewContractDialog } from './SellerStoreDialogs';
import { SellerStoreSetupTab } from './SellerStoreSetupTab';
import { SellerSubscribersTab } from './SellerSubscribersTab';
import { SellerSubscriptionTab } from './SellerSubscriptionTab';
import { SellerSupportTab } from './SellerSupportTab';
import { SellerTemplateTab } from './SellerTemplateTab';
import { SellerWalletTab } from './SellerWalletTab';

interface SellerDashboardViewProps {
  onBackToCustomerSite: () => void;
  onOpenStorefront: () => void;
}

function readWorkFilter() {
  if (typeof window === 'undefined') return null;
  const params = new URLSearchParams(window.location.search);
  const work = params.get('work');
  const tab = params.get('tab');
  return work === 'attention' && tab === 'catalog' || work === 'follow-up' && tab === 'orders' ? work : null;
}

function StorePicker({ current, stores, onSelect }: {
  current: Store;
  stores: Store[];
  onSelect: (store: Store) => void;
}) {
  return (
    <label className="flex min-w-0 flex-wrap items-center gap-2 text-xs font-bold text-slate-700">
      فروشگاه فعال
      <select value={current.id} onChange={event => {
        const selected = stores.find(store => store.id === event.target.value);
        if (selected) onSelect(selected);
      }} className="min-h-11 min-w-0 max-w-full flex-1 rounded-xl border border-slate-300 bg-white px-3 text-sm text-slate-900 focus-visible:outline-2 focus-visible:outline-brand lg:max-w-52">
        {!stores.some(store => store.id === current.id) && <option value={current.id}>{current.name}</option>}
        {stores.map(store => <option value={store.id} key={store.id}>{store.name}</option>)}
      </select>
    </label>
  );
}

export function SellerDashboardView({ onBackToCustomerSite, onOpenStorefront }: SellerDashboardViewProps) {
  const {
    currentStore, setCurrentStore, stores, addStore, platformSettings, updateContracts, sellerSession, logoutSeller,
    toPersianDigits, sellerDashboardTab, setSellerDashboardTab, setActiveView,
  } = useStore();
  const { info, success } = useToast();
  const [unreadCount, setUnreadCount] = useState(0);
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const [showStoreForm, setShowStoreForm] = useState(false);
  const [workFilter, setWorkFilter] = useState(readWorkFilter);
  const mainRef = useRef<HTMLElement>(null);
  const storeManagement = useRef<HTMLDetailsElement>(null);
  const storeRef = useRef(currentStore);
  storeRef.current = currentStore;

  useEffect(() => {
    if (isMockMode()) return;
    let cancelled = false;
    sellerNotificationsApi.getNotifications()
      .then(result => { if (!cancelled) setUnreadCount(result.unreadCount); })
      .catch(() => undefined);
    return () => { cancelled = true; };
  }, [sellerSession?.token]);

  useEffect(() => {
    const restoreSearch = () => {
      setWorkFilter(readWorkFilter());
      window.requestAnimationFrame(() => mainRef.current?.focus({ preventScroll: true }));
    };
    window.addEventListener('popstate', restoreSearch);
    return () => window.removeEventListener('popstate', restoreSearch);
  }, []);

  const activeTab = isSellerDashboardTab(sellerDashboardTab) ? sellerDashboardTab : 'overview';
  const activeTitle = sellerNavigation.flatMap(group => group.items).find(item => item.id === activeTab)?.label ?? 'پیشخوان';
  const navigate = (tab: string, work?: SellerWorkFilter) => {
    if (!isSellerDashboardTab(tab)) return;
    const href = sellerSectionHref(tab, work);
    if (href !== window.location.pathname + window.location.search + window.location.hash) {
      window.history.pushState({ ...window.history.state, activeView: 'seller_dashboard', sellerTab: tab }, '', href);
    }
    setSellerDashboardTab(tab);
    setWorkFilter(readWorkFilter());
    window.requestAnimationFrame(() => {
      mainRef.current?.focus({ preventScroll: true });
      window.scrollTo({ top: 0, behavior: 'instant' });
    });
  };

  const ownedStores = stores.filter(store => store.ownerType === 'dropshipper' &&
    (!isMockMode() || store.id === sellerSession?.storeId || store.ownerName === currentStore.ownerName))
    .map(store => store.id === currentStore.id ? currentStore : store);
  const maxStores = ownedStores.some(store => store.activePlanId === 'vip') ? 10 : 1;
  const canCreateStore = ownedStores.length < maxStores;
  const contract = {
    text: platformSettings.contracts?.sellerContractText ?? '',
    version: platformSettings.contracts?.sellerContractVersion || 2,
  };
  const needsContract = (currentStore.signedContractVersion || 1) < contract.version;

  async function refreshContract() {
    const latest = await apiRequest<Partial<PlatformContracts>>('/platform/contracts', { method: 'GET' });
    if (!Number.isInteger(latest.sellerContractVersion) || !latest.sellerContractText?.trim()) {
      throw new Error('متن قرارداد دریافت نشد.');
    }
    updateContracts(latest);
  }

  async function createStore(name: string, slug: string) {
    if (!canCreateStore) throw new Error('ظرفیت ساخت فروشگاه در اشتراک فعلی تکمیل شده است.');
    const created = isMockMode() ? {
      ...currentStore,
      id: `store-${newUuid()}`,
      name,
      slug,
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
      storeName: name, slug, ownerName: sellerSession?.fullName || currentStore.ownerName,
      profitMarginPercent: currentStore.profitMarginPercent,
    });
    addStore(created);
    success(`فروشگاه «${created.name}» ساخته شد.`);
  }

  async function acceptContract(version: number) {
    const { id, slug } = currentStore;
    if (!isMockMode()) {
      if (!slug) throw new Error('فروشگاه پیدا نشد. دوباره وارد حساب شو.');
      await storeApi.saveStoreSettings(slug, { signedContractVersion: version });
    }
    if (storeRef.current.id === id) setCurrentStore({ ...storeRef.current, signedContractVersion: version });
    success(`نسخهٔ ${toPersianDigits(version)} قرارداد تأیید و ثبت شد.`);
  }

  const logout = () => {
    const slug = currentStore.slug;
    logoutSeller();
    info('از حساب فروشنده خارج شدی.');
    if (slug) setActiveView('seller_storefront', { storeSlug: slug });
    else setActiveView('customer');
  };

  const openManagement = () => {
    if (canCreateStore) setShowStoreForm(true);
    else navigate('subscription');
  };
  const showStores = () => {
    navigate('overview');
    window.requestAnimationFrame(() => {
      const details = storeManagement.current;
      if (!details) return;
      details.open = true;
      details.querySelector<HTMLElement>('summary')?.focus();
    });
  };
  const shopLink = (
    <button type="button" onClick={onOpenStorefront} className="flex min-h-11 w-full items-center justify-between gap-2 rounded-xl px-3 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:outline-2 focus-visible:outline-brand">
      مشاهدهٔ فروشگاه <ExternalLink aria-hidden="true" className="size-4" />
    </button>
  );

  return (
    <SellerAuthGuard onBackToCustomerSite={onBackToCustomerSite}>
      <PanelWorkspace id="seller" label="فروشنده" name={currentStore.name || 'پنل دراپ‌شیپر'}
        title={activeTitle} current={activeTab} groups={sellerNavigation} href={sellerSectionHref} onNavigate={navigate}
        onBackToSite={onBackToCustomerSite} onLogout={logout} onOpenNotifications={() => setNotificationsOpen(true)}
        unreadCount={unreadCount} formatCount={toPersianDigits} mainRef={mainRef} sidebarActions={shopLink}
        headerActions={ownedStores.length > 1 ? <StorePicker current={currentStore} stores={ownedStores} onSelect={setCurrentStore} /> : undefined}
        mobileActions={<>
          {ownedStores.length > 1 && <StorePicker current={currentStore} stores={ownedStores} onSelect={setCurrentStore} />}
          {shopLink}
        </>}
        modal={needsContract ? <SellerRenewContractDialog key={currentStore.id} contract={contract} onRetry={refreshContract} onAccept={acceptContract} onExit={logout} />
          : showStoreForm ? <SellerCreateStoreDialog maxStores={maxStores} contract={contract} onRetry={refreshContract} onCreate={createStore} onClose={() => setShowStoreForm(false)} />
          : null}>
        <ErrorBoundary key={`${currentStore.id}:${activeTab}`} title={activeTitle} onReset={() => navigate('overview')}>
          {activeTab === 'overview' && <>
            <SellerOverviewTab onNavigateTab={navigate} onOpenStorefront={onOpenStorefront} />
            <details ref={storeManagement} className="group mt-4 rounded-2xl border border-slate-200 bg-white px-4 py-1 sm:px-5">
              <summary className="flex min-h-11 scroll-mt-24 cursor-pointer list-none items-center gap-2 rounded-lg text-sm font-bold text-slate-900 focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden">
                <StoreIcon aria-hidden="true" className="size-4 shrink-0 text-brand-deep" />
                <span className="flex-1">فروشگاه‌های من</span>
                <span className="text-xs font-normal text-slate-600">{toPersianDigits(ownedStores.length)} از {toPersianDigits(maxStores)}</span>
                <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-open:rotate-180" />
              </summary>
              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 py-3">
                <StorePicker current={currentStore} stores={ownedStores} onSelect={setCurrentStore} />
                <button type="button" onClick={openManagement} className="inline-flex min-h-11 items-center gap-2 rounded-xl bg-brand-soft px-3 text-sm font-bold text-brand-deep hover:bg-brand-border focus-visible:outline-2 focus-visible:outline-brand">
                  {canCreateStore && <Plus aria-hidden="true" className="size-4" />}
                  {canCreateStore ? 'ساخت فروشگاه جدید' : 'دیدن ظرفیت اشتراک'}
                </button>
              </div>
            </details>
          </>}
          {activeTab === 'store-setup' && <SellerStoreSetupTab />}
          {activeTab === 'catalog' && <SellerCatalogTab key={workFilter ?? 'all'} initialWorkFilter={workFilter === 'attention' ? 'attention' : undefined} />}
          {activeTab === 'pricing' && <SellerPricingTab />}
          {activeTab === 'builder' && <SellerBuilderTab onOpenStorefront={onOpenStorefront} onNavigateTab={navigate} />}
          {activeTab === 'orders' && <SellerOrdersTab key={workFilter ?? 'all'} initialWorkFilter={workFilter === 'follow-up' ? 'follow-up' : undefined} onNavigateTab={navigate} onOpenStorefront={onOpenStorefront} />}
          {activeTab === 'returns' && <SellerReturnsTab />}
          {activeTab === 'links' && <SellerLinksTab />}
          {activeTab === 'wallet' && <SellerWalletTab />}
          {activeTab === 'referral' && <SellerReferralTab />}
          {activeTab === 'subscription' && <SellerSubscriptionTab onManageStores={showStores} />}
          {activeTab === 'ads' && <SellerAdsWidgetTab />}
          {activeTab === 'support' && <SellerSupportTab storeId={currentStore.id} />}
          {activeTab === 'template' && <SellerTemplateTab />}
          {activeTab === 'analytics' && <SellerAnalyticsTab />}
          {activeTab === 'search-insight' && <SellerSearchInsightTab />}
          {activeTab === 'discount-campaigns' && <SellerDiscountCampaignsTab />}
          {activeTab === 'discount-codes' && <SellerDiscountTab />}
          {activeTab === 'subscribers' && <SellerSubscribersTab onOpenStorefront={onOpenStorefront} />}
        </ErrorBoundary>
      </PanelWorkspace>
      <RoleNotificationDrawer isOpen={notificationsOpen} onClose={() => setNotificationsOpen(false)} title="اعلان‌های فروشگاه"
        emptyTitle="اعلان جدیدی نیست." emptyHint="سفارش تازه، تسویه و تغییرهای مالی فروشگاه اینجا نمایش داده می‌شوند."
        accent="brand" api={sellerNotificationsApi} onUnreadCountChange={setUnreadCount} panelDialogId="seller-notifications-title" />
    </SellerAuthGuard>
  );
}
