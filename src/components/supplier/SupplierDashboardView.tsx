import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { SupplierAuthGuard } from './SupplierAuthGuard';
import { ApiError, apiRequest } from '../../services/apiClient';
import { 
  ArrowRight, 
  LayoutDashboard, 
  Package, 
  Warehouse, 
  PackagePlus, 
  AlertTriangle, 
  Megaphone, 
  Receipt,
  LogOut,
  BarChart3,
  TrendingUp,
  RotateCcw,
  Truck,
  User,
  FileText,
  Building2,
  Boxes,
  Users,
  Wallet,
  Bell,
  ChevronLeft,
  ChevronDown
} from 'lucide-react';

import { SupplierOverviewTab } from './SupplierOverviewTab';
import { SupplierProductsTab } from './SupplierProductsTab';
import { SupplierInventoryTab } from './SupplierInventoryTab';
import { SupplierInboundFlowTab, type ShipmentStage } from './SupplierInboundFlowTab';
import { SupplierDiscrepanciesTab } from './SupplierDiscrepanciesTab';
import { SupplierAdsCampaignTab } from './SupplierAdsCampaignTab';
import { SupplierSettlementTab } from './SupplierSettlementTab';
import { SupplierOrdersTab } from './SupplierOrdersTab';
import { SupplierTeamTab } from './SupplierTeamTab';
import { SupplierAnalyticsTab, SupplierCustomerReturnsTab, SupplierMyWarehousesTab, SupplierShippingMethodsTab, SupplierDemandInsightTab, SupplierContractTab } from './SupplierNewTabs';
import { SupplierProfileTab } from './SupplierProfileTab';
import { supplierApi } from '../../services/supplierApi';
import { SupplierNotificationDrawer } from './SupplierNotificationDrawer';
import { ShoppingBag } from 'lucide-react';

interface Props {
  onBackToCustomerSite: () => void;
}

type SectionDestination = 'ready' | 'propose' | 'proposals' | ShipmentStage;

function readSupplierLocation() {
  const params = new URLSearchParams(typeof window === 'undefined' ? '' : window.location.search);
  const section = params.get('section') || 'overview';
  const view = params.get('view');
  const destination = (section === 'orders' && view === 'ready')
    || (section === 'products' && (view === 'propose' || view === 'proposals'))
    || (section === 'inbound' && (view === 'products' || view === 'prepare' || view === 'track')) ? view : undefined;
  return { section, destination };
}

function supplierSectionHref(section: string, destination?: SectionDestination) {
  const params = new URLSearchParams(window.location.search);
  if (section === 'overview') params.delete('section');
  else params.set('section', section);
  if (destination) params.set('view', destination);
  else params.delete('view');
  const search = params.toString();
  return `${window.location.pathname}${search ? `?${search}` : ''}`;
}

export const SupplierDashboardView: React.FC<Props> = ({ onBackToCustomerSite }) => {
  const { supplierSession, setSupplierSession, logoutSupplier, platformSettings, updateContracts, toPersianDigits } = useStore();
  const { info, success, error } = useToast();
  const [location, setLocation] = useState(readSupplierLocation);
  const mainRef = useRef<HTMLElement>(null);
  const navigateTab = useCallback((tab: string, destination?: SectionDestination, focusContent = true) => {
    const href = supplierSectionHref(tab, destination);
    if (href !== window.location.pathname + window.location.search) {
      window.history.pushState(window.history.state, '', href);
    }
    setLocation(readSupplierLocation());
    if (focusContent) {
      mainRef.current?.focus({ preventScroll: true });
      window.scrollTo({ top: 0, behavior: 'instant' });
    }
  }, []);
  useEffect(() => {
    const syncLocation = () => setLocation(readSupplierLocation());
    window.addEventListener('popstate', syncLocation);
    return () => window.removeEventListener('popstate', syncLocation);
  }, []);
  // Reject-notification bell (DESIGN.md §4.3): unread count lives here,
  // NOT in StoreContext.setUnreadNotificationCount — that counter feeds
  // the customer/storefront header bell and would leak supplier counts
  // into the storefront. focusProposalId is the deep-link target the
  // drawer hands to SupplierProductsTab (one tap -> proposed_offers,
  // highlight + SupplierFeedbackModal).
  const [unreadNotifCount, setUnreadNotifCount] = useState(0);
  const [isNotifOpen, setIsNotifOpen] = useState(false);
  const [workflowProductId, setWorkflowProductId] = useState('');
  const [focusProposalId, setFocusProposalId] = useState<string | null>(null);
  const refreshUnreadNotifCount = useCallback(() => {
    supplierApi.getNotifications().then((res) => setUnreadNotifCount(res.unreadCount)).catch(() => {});
  }, []);
  useEffect(() => { refreshUnreadNotifCount(); }, [refreshUnreadNotifCount]);
  const [reviewedContractVersion, setReviewedContractVersion] = useState<number | null>(null);
  const [contractSaving, setContractSaving] = useState(false);
  useEffect(() => { setReviewedContractVersion(null); }, [supplierSession?.token]);
  const latestContractVersion = platformSettings.contracts?.supplierContractVersion || 0;
  const contractChecked = reviewedContractVersion === latestContractVersion;
  const signedContractVersion = supplierSession?.signedContractVersion ?? 0;
  const canAcceptContract = supplierSession?.permissions?.includes('supplier.manage') ?? false;
  // Live parity (SupplierDashboardView-MERGED.js header, both occurrences): the header shows
  // نامشخص only when warehouseCityConfirmed is explicitly false, otherwise the city.
  // Cast: the flag rides on the session object at runtime (see AuthContext) but is not yet
  // declared on the SupplierSession type; drop the cast once the type field lands.
  const warehouseCityConfirmed = (supplierSession as typeof supplierSession & { warehouseCityConfirmed?: boolean })?.warehouseCityConfirmed;
  const showContractModal = !!supplierSession && canAcceptContract && latestContractVersion > 0 && signedContractVersion < latestContractVersion;
  const acceptContract = async () => {
    if (!contractChecked || !supplierSession || contractSaving) return;
    setContractSaving(true);
    try {
      const { signedContractVersion: acceptedVersion } = await supplierApi.acceptContract(latestContractVersion);
      setSupplierSession(current => current?.token === supplierSession.token ? { ...current, signedContractVersion: acceptedVersion } : current);
      success(`نسخهٔ ${toPersianDigits(acceptedVersion)} قرارداد تأیید و ثبت شد.`);
    } catch (cause: unknown) {
      if (cause instanceof ApiError && cause.status === 409 && cause.body?.code === 'contract_version_changed') {
        setReviewedContractVersion(null);
        try {
          const contracts = await apiRequest<{ supplierContractVersion: number; supplierContractText: string }>('/platform/contracts', { method: 'GET' });
          if (!Number.isInteger(contracts.supplierContractVersion) || !contracts.supplierContractText?.trim()) throw new Error('Contract version or text missing');
          updateContracts(contracts);
          info('متن تازهٔ قرارداد بارگذاری شد. آن را بخوان و دوباره تأیید کن.');
        } catch {
          error('دریافت متن تازهٔ قرارداد انجام نشد. برای دیدن نسخهٔ جدید صفحه را تازه کن.');
        }
      } else {
        error(cause instanceof Error ? cause.message : 'ثبت تأیید قرارداد انجام نشد.');
      }
    } finally {
      setContractSaving(false);
    }
  };

  // FIX 17: live supplier menu (SupplierDashboardView-MERGED.js):
  // 17 live tabs in this exact order with live labels. The
  // candidate had 9; 8 are added here. 'pricing' (tiered pricing)
  // is DELIBERATELY omitted per user decision Q12, so the
  // candidate shows 16 supplier tabs, not 17.
  const menuItems = [
    { id: 'overview', label: 'نمای کلی', icon: LayoutDashboard },
    { id: 'analytics', label: 'تحلیل فروش', icon: BarChart3 },
    { id: 'demand-insight', label: 'تقاضای بازار', icon: TrendingUp },
    { id: 'orders', label: 'سفارش‌های من', icon: ShoppingBag },
    { id: 'customer-returns', label: 'مرجوعی‌های مشتریان', icon: RotateCcw },
    { id: 'products', label: 'کالاها و تنوع‌ها', icon: Package },
    { id: 'inventory', label: 'موجودی کالاها', icon: Boxes },
    { id: 'my-warehouses', label: 'انبارهای من', icon: Warehouse },
    { id: 'inbound', label: 'ثبت کالا و ارسال به انبار', icon: PackagePlus },
    { id: 'shipping-methods', label: 'روش‌های ارسال', icon: Truck },
    { id: 'discrepancies', label: 'مغایرت‌های تحویل انبار', icon: AlertTriangle },
    { id: 'settlement', label: 'تسویه‌حساب', icon: Receipt },
    { id: 'ads', label: 'تبلیغات', icon: Megaphone },
    { id: 'team', label: 'همکاران من', icon: Users },
    { id: 'profile', label: 'پروفایل', icon: User },
    { id: 'contract', label: 'قرارداد من', icon: FileText },
  ];

  const tabPermissions: Record<string, string> = { team: 'supplier.manage', products: 'products.manage', inventory: 'inventory.manage', inbound: 'inventory.manage', discrepancies: 'inventory.manage', orders: 'orders.manage', ads: 'marketing.manage', settlement: 'finance.manage', overview: 'supplier.manage', analytics: 'supplier.manage', 'demand-insight': 'supplier.manage', 'customer-returns': 'orders.manage', 'my-warehouses': 'inventory.manage', 'shipping-methods': 'orders.manage', profile: 'supplier.manage', contract: 'supplier.manage' };
  const permissions = supplierSession?.permissions || [];
  const allowedTabs = menuItems.filter((item) => permissions.includes('supplier.manage') || permissions.includes(tabPermissions[item.id]) || (item.id === 'inbound' && permissions.includes('products.manage')));
  const visibleTab = allowedTabs.some((item) => item.id === location.section) ? location.section : allowedTabs[0]?.id;
  const tabDestination = visibleTab === location.section ? location.destination : undefined;
  const canManageProducts = permissions.includes('supplier.manage') || permissions.includes('products.manage');
  const canManageShipments = permissions.includes('supplier.manage') || permissions.includes('inventory.manage');
  const shipmentStage = tabDestination === 'products' || tabDestination === 'prepare' || tabDestination === 'track'
    ? tabDestination : canManageProducts ? 'products' : 'prepare';
  const consumeWorkflowProduct = useCallback(() => setWorkflowProductId(''), []);
  const handleShipmentStage = useCallback((stage: ShipmentStage) => navigateTab('inbound', stage, false), [navigateTab]);

  // Header back button: from inside any tab it first returns to the
  // panel's own main view (the overview tab; for team accounts without
  // the overview permission, the first permitted tab -- which is what
  // the panel opens on). Only on that main view does the button leave
  // the panel for the customer site.
  const panelHomeTab = allowedTabs.some((item) => item.id === 'overview') ? 'overview' : allowedTabs[0]?.id;
  const panelHomeLabel = allowedTabs.find((item) => item.id === panelHomeTab)?.label;
  const isPanelHome = visibleTab === panelHomeTab;
  const handleHeaderBack = () => {
    if (!isPanelHome && panelHomeTab) {
      navigateTab(panelHomeTab);
    } else {
      onBackToCustomerSite();
    }
  };

  // Desktop sidebar grouping (layout only): the same permission-filtered
  // tabs as the mobile tab bar, partitioned into sidebar groups covering
  // all 16 tab ids exactly once. A group with no permitted tab disappears
  // entirely, so gating is inherited unchanged.
  const menuGroups: { heading: string; ids: string[] }[] = [
    { heading: 'پیشخوان', ids: ['overview', 'analytics', 'demand-insight'] },
    { heading: 'سفارش و ارسال', ids: ['orders', 'customer-returns', 'shipping-methods'] },
    { heading: 'کالا و انبار', ids: ['inbound', 'products', 'inventory', 'my-warehouses', 'discrepancies'] },
    { heading: 'مالی', ids: ['settlement'] },
    { heading: 'بازاریابی', ids: ['ads'] },
    { heading: 'حساب کاربری', ids: ['team', 'profile', 'contract'] },
  ];
  const sidebarGroups = menuGroups
    .map((group) => ({
      heading: group.heading,
      items: group.ids
        .map((id) => allowedTabs.find((item) => item.id === id))
        .filter((item): item is (typeof menuItems)[number] => !!item),
    }))
    .filter((group) => group.items.length > 0);

  // Active tab label for the desktop header title slot (item: at lg the
  // header stops repeating the identity and names the current section).
  const activeTabLabel = allowedTabs.find((item) => item.id === visibleTab)?.label;
  // A rejection that lands while the panel is open shows only after
  // the next drawer open / Products activation (no polling in v1).
  useEffect(() => { if (visibleTab === 'products') refreshUnreadNotifCount(); }, [visibleTab, refreshUnreadNotifCount]);
  const handleSelectNotifProposal = useCallback((proposalId: string) => {
    setIsNotifOpen(false);
    navigateTab('products');
    setFocusProposalId(proposalId);
  }, [navigateTab]);
  const warehouseStatus = warehouseCityConfirmed === false ? 'انبار تأییدنشده' : supplierSession?.warehouseCity ? 'انبار تأییدشده' : 'بدون انبار ثبت‌شده';

  return (
    <SupplierAuthGuard onBackToCustomerSite={onBackToCustomerSite}>
    <div dir="rtl" className="flex min-h-screen flex-1 flex-col bg-[#f5f5fa] text-slate-900">
      {showContractModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-3xl max-w-md w-full p-4 shadow-2xl border border-slate-100 space-y-3 text-xs">
            <div className="flex items-center gap-2 text-brand-deep font-bold border-b border-slate-100 pb-2">
              <FileText className="w-5 h-5 text-brand-deep"  strokeWidth={1.5}/>
              <div>
                <h3 className="text-xs font-bold text-slate-900">قرارداد همکاری عوض شده است</h3>
                <span className="text-[10px] text-slate-400 block font-normal">نسخه جدید {toPersianDigits(latestContractVersion)} (به‌جای نسخه {toPersianDigits(signedContractVersion)})</span>
              </div>
            </div>
            <p className="text-[11px] text-slate-600 leading-relaxed">برای ادامه کار، متن تازه قرارداد را بخوان و قبول کن:</p>
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-2xl max-h-40 overflow-y-auto text-[11px] text-slate-700 leading-relaxed font-mono whitespace-pre-line no-scrollbar">
              {platformSettings.contracts?.supplierContractText}
            </div>
            <label className="flex items-start gap-2 p-2.5 rounded-xl border border-slate-200 bg-slate-50 cursor-pointer">
              <input type="checkbox" checked={contractChecked} disabled={contractSaving} onChange={e => setReviewedContractVersion(e.target.checked ? latestContractVersion : null)} className="w-4 h-4 accent-brand-deep rounded mt-0.5" />
              <span className="text-[11px] text-slate-800 leading-relaxed font-bold">متن تازه قرارداد را خواندم و قبول دارم.</span>
            </label>
            <button disabled={!contractChecked || contractSaving} onClick={() => void acceptContract()} className="min-h-11 w-full rounded-xl bg-brand-deep py-2.5 text-xs font-bold text-white shadow-xs transition-all disabled:opacity-50">
              {contractSaving ? 'در حال ثبت…' : 'قبول دارم، ادامه بده'}
            </button>
          </div>
        </div>
      )}
      <a href="#supplier-main" className="sr-only focus:not-sr-only focus:absolute focus:z-40 focus:rounded-lg focus:bg-white focus:p-3">رفتن به محتوای پنل</a>
      <header className="sticky top-0 z-30 border-b border-[#e5e3f0] bg-white/95 backdrop-blur-md">
        <div className="mx-auto flex min-h-16 w-full max-w-[1520px] items-center justify-between gap-3 px-4 sm:px-6">
          <div className="flex min-w-0 items-center gap-3">
            <button type="button" onClick={handleHeaderBack}
              aria-label={isPanelHome ? 'بازگشت به سایت اصلی' : `بازگشت به ${panelHomeLabel}`}
              title={isPanelHome ? 'بازگشت به سایت اصلی' : `بازگشت به ${panelHomeLabel}`}
              className="grid size-11 shrink-0 place-items-center rounded-xl border border-slate-200 text-slate-700 hover:border-brand-border hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand">
              <ArrowRight aria-hidden="true" className="size-5" strokeWidth={1.75} />
            </button>
            <div className="min-w-0">
              <p className="truncate text-xs font-medium text-slate-600">
                {supplierSession?.companyName || 'پنل تأمین‌کننده'}
              </p>
              <h1 className="truncate text-base font-extrabold sm:text-lg">{activeTabLabel || 'پنل تأمین‌کننده'}</h1>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <button type="button" onClick={() => setIsNotifOpen(true)} aria-label="اعلان‌ها" title="اعلان‌ها"
              className="relative grid size-11 place-items-center rounded-xl border border-slate-200 text-slate-700 hover:border-brand-border hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand">
              <Bell aria-hidden="true" className="size-5" strokeWidth={1.75} />
              {unreadNotifCount > 0 && (
                <span className="absolute -start-1 -top-1 grid min-h-5 min-w-5 place-items-center rounded-full bg-rose-600 px-1 text-[10px] font-bold text-white ring-2 ring-white">
                  {toPersianDigits(unreadNotifCount)}
                </span>
              )}
            </button>
            <button type="button" onClick={() => { logoutSupplier(); info('از حساب تأمین‌کننده خارج شدی.'); }}
              aria-label="خروج از حساب تأمین‌کننده" title="خروج از حساب تأمین‌کننده"
              className="flex min-h-11 items-center gap-2 rounded-xl px-2.5 text-xs font-bold text-slate-700 hover:bg-rose-50 hover:text-rose-700 focus-visible:ring-2 focus-visible:ring-rose-600 sm:px-3">
              <LogOut aria-hidden="true" className="size-4" strokeWidth={1.75} />
              <span className="hidden sm:inline">خروج</span>
            </button>
          </div>
        </div>
      </header>

      {!!sidebarGroups.length && (
        <nav aria-label="منوی موبایل تأمین‌کننده" className="px-4 pt-4 sm:px-6 lg:hidden">
          <label htmlFor="supplier-section" className="mb-1.5 block text-xs font-bold text-slate-700">بخش پنل</label>
          <div className="relative">
            <select id="supplier-section" value={visibleTab} onChange={event => navigateTab(event.target.value)}
              className="min-h-12 w-full appearance-none rounded-2xl border border-[#e5e3f0] bg-white ps-4 pe-10 text-sm font-bold text-slate-900 shadow-sm focus-visible:ring-2 focus-visible:ring-brand">
              {sidebarGroups.map(group => (
                <optgroup key={group.heading} label={group.heading}>
                  {group.items.map(item => <option key={item.id} value={item.id}>{item.label}</option>)}
                </optgroup>
              ))}
            </select>
            <ChevronDown aria-hidden="true" className="pointer-events-none absolute end-4 top-4 size-4 text-brand-deep" />
          </div>
        </nav>
      )}

      <div className="mx-auto flex w-full max-w-[1520px] flex-1 flex-col gap-4 px-4 pb-8 pt-4 sm:px-6 lg:flex-row lg:gap-6 lg:pt-6">
        <aside className="hidden w-[250px] shrink-0 self-start overflow-y-auto rounded-[26px] bg-[#211a3d] p-3 text-white shadow-lg shadow-[#262044]/15 lg:sticky lg:top-[88px] lg:block lg:max-h-[calc(100dvh-110px)] xl:w-[270px]">
          <div className="mb-2 flex min-w-0 items-start gap-3 rounded-2xl border border-white/10 bg-white/8 p-3">
            <span className="grid size-10 shrink-0 place-items-center rounded-xl bg-[#dacfff] text-[#352466]">
              <Building2 aria-hidden="true" className="size-5" strokeWidth={1.75} />
            </span>
            <div className="min-w-0">
              <p className="truncate text-sm font-extrabold" title={supplierSession?.companyName || 'تأمین‌کننده'}>{supplierSession?.companyName || 'تأمین‌کننده'}</p>
              <p className="mt-1 text-[11px] text-[#d3c9ed]">
                {warehouseStatus}{warehouseCityConfirmed !== false && supplierSession?.warehouseCity ? ` · ${supplierSession.warehouseCity}` : ''}
              </p>
            </div>
          </div>

          {!!sidebarGroups.length && (
            <nav aria-label="منوی پنل تأمین‌کننده" className="space-y-3 py-1">
              {sidebarGroups.map(group => (
                <section key={group.heading}>
                  <h2 className="px-3 pb-1 text-[11px] font-semibold text-[#c7b7e5]">{group.heading}</h2>
                  <div className="space-y-0.5">
                    {group.items.map(({ id, label, icon: Icon }) => {
                      const isActive = visibleTab === id;
                      return (
                        <a key={id} href={typeof window === 'undefined' ? `/supplier?section=${id}` : supplierSectionHref(id)} data-section={id}
                          onClick={event => {
                            if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
                            event.preventDefault();
                            navigateTab(id);
                          }} aria-current={isActive ? 'page' : undefined}
                          className={`flex min-h-10 w-full items-center gap-2.5 rounded-xl px-3 py-1.5 text-start text-xs font-bold transition-colors focus-visible:ring-2 focus-visible:ring-[#e4d9ff] ${isActive ? 'bg-[#e8dfff] text-[#302056]' : 'text-[#f0eafa] hover:bg-white/12'}`}>
                          <Icon aria-hidden="true" className="size-4 shrink-0" strokeWidth={1.75} />
                          <span className="flex-1 leading-5">{label}</span>
                          {isActive && <ChevronLeft aria-hidden="true" className="size-4 shrink-0" />}
                        </a>
                      );
                    })}
                  </div>
                </section>
              ))}
            </nav>
          )}
        </aside>

        {/* Main Tab Content */}
        <main ref={mainRef} id="supplier-main" aria-label={activeTabLabel} tabIndex={-1} className="flex-1 min-w-0 scroll-mt-20">
          <div className="min-w-0">
            {allowedTabs.length === 0 && <div role="alert" className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">مجوزهای حساب تأمین‌کننده دریافت نشد. لطفاً یک‌بار خارج شوید و دوباره وارد شوید.</div>}
            {visibleTab === 'overview' && <SupplierOverviewTab onNavigateTab={navigateTab} />}
            {visibleTab === 'orders' && <SupplierOrdersTab key={tabDestination ?? 'all'} initialFilter={tabDestination === 'ready' ? 'ready' : 'all'} />}
            {visibleTab === 'products' && <SupplierProductsTab key={tabDestination ?? 'catalog'} openProposal={tabDestination === 'propose'} showProposals={tabDestination === 'proposals'} onPrepareShipment={canManageShipments ? (id) => { setWorkflowProductId(id); navigateTab('inbound', 'prepare'); } : undefined} focusProposalId={focusProposalId} onFocusConsumed={() => setFocusProposalId(null)} />}
            {visibleTab === 'inventory' && <SupplierInventoryTab />}
            {visibleTab === 'inbound' && <SupplierInboundFlowTab
              initialProductId={workflowProductId}
              activeStage={shipmentStage}
              onStageChange={handleShipmentStage}
              onProductConsumed={consumeWorkflowProduct}
              canManageProducts={canManageProducts}
              canManageShipments={canManageShipments}
            />}
            {visibleTab === 'discrepancies' && <SupplierDiscrepanciesTab />}
            {visibleTab === 'ads' && <SupplierAdsCampaignTab />}
            {visibleTab === 'settlement' && <SupplierSettlementTab />}
            {visibleTab === 'team' && <SupplierTeamTab />}
            {visibleTab === 'analytics' && <SupplierAnalyticsTab />}
            {visibleTab === 'demand-insight' && <SupplierDemandInsightTab onPropose={() => navigateTab('products', 'propose')} />}
            {visibleTab === 'customer-returns' && <SupplierCustomerReturnsTab />}
            {visibleTab === 'my-warehouses' && <SupplierMyWarehousesTab />}
            {visibleTab === 'shipping-methods' && <SupplierShippingMethodsTab />}
            {visibleTab === 'profile' && <SupplierProfileTab />}
            {visibleTab === 'contract' && <SupplierContractTab />}
          </div>
        </main>
      </div>
    </div>
      <SupplierNotificationDrawer
        isOpen={isNotifOpen}
        onClose={() => setIsNotifOpen(false)}
        onSelectProposal={handleSelectNotifProposal}
        onUnreadCountChange={setUnreadNotifCount}
      />
    </SupplierAuthGuard>
  );
};
