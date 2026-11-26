import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { SupplierAuthGuard } from './SupplierAuthGuard';
import { ApiError, apiRequest } from '../../services/apiClient';

import { SupplierOverviewTab } from './SupplierOverviewTab';
import { SupplierProductsTab } from './SupplierProductsTab';
import { SupplierInventoryTab } from './SupplierInventoryTab';
import { SupplierInboundFlowTab, type ShipmentStage } from './SupplierInboundFlowTab';
import { SupplierDiscrepanciesTab } from './SupplierDiscrepanciesTab';
import { SupplierAdsCampaignTab } from './SupplierAdsCampaignTab';
import { SupplierSettlementTab } from './SupplierSettlementTab';
import { SupplierOrdersTab } from './SupplierOrdersTab';
import { SellerSupportTab } from '../seller/SellerSupportTab';
import { SupplierTeamTab } from './SupplierTeamTab';
import { SupplierAnalyticsTab, SupplierCustomerReturnsTab, SupplierMyWarehousesTab, SupplierShippingMethodsTab, SupplierDemandInsightTab, SupplierContractTab } from './SupplierNewTabs';
import { SupplierProfileTab } from './SupplierProfileTab';
import { supplierApi } from '../../services/supplierApi';
import { SupplierNotificationDrawer } from './SupplierNotificationDrawer';
import { SupplierDialog } from './SupplierDialog';
import { PanelWorkspace } from '../common/PanelWorkspace';
import { getSupplierNavigation } from './SupplierNavigation';

interface Props {
  onBackToCustomerSite: () => void;
}

type SectionDestination = 'ready' | 'overdue' | 'open' | 'awaiting-response' | 'propose' | 'proposals' | ShipmentStage;

function readSupplierLocation() {
  const params = new URLSearchParams(typeof window === 'undefined' ? '' : window.location.search);
  const section = params.get('section') || 'overview';
  const view = params.get('view');
  const destination = (section === 'orders' && (view === 'ready' || view === 'overdue'))
    || (section === 'products' && (view === 'propose' || view === 'proposals'))
    || (section === 'inbound' && (view === 'products' || view === 'prepare' || view === 'track' || view === 'open'))
    || (section === 'discrepancies' && view === 'awaiting-response') ? view : undefined;
  return { section, destination };
}

function supplierSectionHref(section: string, destination?: SectionDestination) {
  if (typeof window === 'undefined') {
    const params = new URLSearchParams();
    if (section !== 'overview') params.set('section', section);
    if (destination) params.set('view', destination);
    return `/supplier${params.size ? `?${params}` : ''}`;
  }
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
      window.requestAnimationFrame(() => {
        mainRef.current?.focus({ preventScroll: true });
        window.scrollTo({ top: 0, behavior: 'instant' });
      });
    }
  }, []);
  useEffect(() => {
    const syncLocation = () => {
      setLocation(readSupplierLocation());
      window.requestAnimationFrame(() => mainRef.current?.focus({ preventScroll: true }));
    };
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

  const permissions = supplierSession?.permissions || [];
  const manager = permissions.includes('supplier.manage');
  const navigation = getSupplierNavigation(permissions);
  const allowedTabs = navigation.flatMap(group => group.items);
  const panelHome = ['overview', 'orders', 'products', 'inventory', 'settlement', 'ads', 'support']
    .find(section => allowedTabs.some(tab => tab.id === section));
  const currentTab = allowedTabs.find(item => item.id === location.section)
    ?? allowedTabs.find(item => item.id === panelHome);
  const visibleTab = currentTab?.id;
  const activeTabLabel = currentTab?.label ?? 'پنل تأمین‌کننده';
  const tabDestination = visibleTab === location.section ? location.destination : undefined;
  const sectionUnavailable = location.section !== 'overview' && location.section !== visibleTab && !!visibleTab;
  const canManageProducts = manager || permissions.includes('products.manage');
  const canManageShipments = manager || permissions.includes('inventory.manage');
  const shipmentStage = tabDestination === 'products' || tabDestination === 'prepare' || tabDestination === 'track'
    ? tabDestination : canManageProducts ? 'products' : 'prepare';
  const consumeWorkflowProduct = useCallback(() => setWorkflowProductId(''), []);
  const handleShipmentStage = useCallback((stage: ShipmentStage) => navigateTab('inbound', stage, false), [navigateTab]);
  const logout = () => {
    logoutSupplier();
    info('از حساب تأمین‌کننده خارج شدی.');
  };

  // A rejection that lands while the panel is open shows only after
  // the next drawer open / Products activation (no polling in v1).
  useEffect(() => { if (visibleTab === 'products') refreshUnreadNotifCount(); }, [visibleTab, refreshUnreadNotifCount]);
  const handleSelectNotifProposal = useCallback((proposalId: string) => {
    setIsNotifOpen(false);
    navigateTab('products');
    setFocusProposalId(proposalId);
  }, [navigateTab]);

  return (
    <SupplierAuthGuard onBackToCustomerSite={onBackToCustomerSite}>
      <PanelWorkspace id="supplier" label="تأمین‌کننده" name={supplierSession?.companyName || 'پنل تأمین‌کننده'}
        title={activeTabLabel} current={visibleTab} groups={navigation} href={supplierSectionHref} onNavigate={navigateTab}
        onBackToSite={onBackToCustomerSite} onLogout={logout} unreadCount={unreadNotifCount} formatCount={toPersianDigits}
        onOpenNotifications={() => setIsNotifOpen(true)} mainRef={mainRef}
        modal={showContractModal ? (
          <SupplierDialog titleId="supplier-contract-title" descriptionId="supplier-contract-description">
          <h2 id="supplier-contract-title" className="text-lg font-extrabold text-slate-900">قرارداد همکاری به‌روز شده است</h2>
          <p id="supplier-contract-description" className="mt-2 text-sm leading-7 text-slate-700">
            برای ادامهٔ کار، نسخهٔ {toPersianDigits(latestContractVersion)} قرارداد را بخوان و تأیید کن.
          </p>
          <div role="region" aria-label="متن قرارداد" tabIndex={0} className="my-4 max-h-56 overflow-y-auto overscroll-contain whitespace-pre-line rounded-xl border border-slate-200 bg-slate-50 p-3 text-sm leading-7 text-slate-800 focus-visible:outline-2 focus-visible:outline-brand">
            {platformSettings.contracts?.supplierContractText}
          </div>
          <label className="flex min-h-11 cursor-pointer items-center gap-2 rounded-xl border border-slate-200 px-3 py-2 text-sm font-bold text-slate-800 focus-within:outline-2 focus-within:outline-brand">
            <input type="checkbox" checked={contractChecked} disabled={contractSaving} onChange={event => setReviewedContractVersion(event.target.checked ? latestContractVersion : null)} className="size-4 shrink-0 accent-brand-deep" />
            متن تازهٔ قرارداد را خواندم و قبول دارم.
          </label>
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <button type="button" disabled={!contractChecked || contractSaving} onClick={() => void acceptContract()} className="min-h-11 flex-1 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50">
              {contractSaving ? 'در حال ثبت…' : 'قبول دارم، ادامه بده'}
            </button>
            <button type="button" disabled={contractSaving} onClick={logout} className="min-h-11 rounded-xl px-3 text-sm font-bold text-slate-700 hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-brand disabled:opacity-50">خروج از حساب</button>
          </div>
        </SupplierDialog>
        ) : null}>
          <div className="min-w-0">
            {!currentTab && (
              <div role="alert" className="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm leading-7 text-amber-950">
                دسترسی‌های حساب دریافت نشد. برای تازه‌کردن دسترسی، دوباره وارد حساب شو.
                <button type="button" onClick={logout} className="ms-2 min-h-11 rounded-lg px-3 text-sm font-bold underline focus-visible:outline-2 focus-visible:outline-brand">خروج از حساب</button>
              </div>
            )}
            {sectionUnavailable && (
              <p role="status" className="mb-3 rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-950">این بخش در دسترس حساب تو نیست. بخش‌های مجاز در منوی پنل نمایش داده می‌شوند.</p>
            )}
            {visibleTab === 'overview' && <SupplierOverviewTab onNavigateTab={navigateTab} sectionHref={supplierSectionHref} />}
            {visibleTab === 'orders' && <SupplierOrdersTab key={tabDestination ?? 'all'} initialFilter={tabDestination === 'ready' || tabDestination === 'overdue' ? tabDestination : 'all'} />}
            {visibleTab === 'products' && <SupplierProductsTab key={tabDestination ?? 'catalog'} openProposal={tabDestination === 'propose'} showProposals={tabDestination === 'proposals'} onPrepareShipment={canManageShipments ? (id) => { setWorkflowProductId(id); navigateTab('inbound', 'prepare'); } : undefined} focusProposalId={focusProposalId} onFocusConsumed={() => setFocusProposalId(null)} />}
            {visibleTab === 'inventory' && <SupplierInventoryTab />}
            {visibleTab === 'inbound' && <SupplierInboundFlowTab
              initialProductId={workflowProductId}
              activeStage={tabDestination === 'open' ? 'track' : shipmentStage}
              openOnly={tabDestination === 'open'}
              onStageChange={handleShipmentStage}
              onProductConsumed={consumeWorkflowProduct}
              canManageProducts={canManageProducts}
              canManageShipments={canManageShipments}
            />}
            {visibleTab === 'discrepancies' && <SupplierDiscrepanciesTab key={tabDestination ?? 'all'} initialAwaitingOnly={tabDestination === 'awaiting-response'} />}
            {visibleTab === 'ads' && <SupplierAdsCampaignTab />}
            {visibleTab === 'settlement' && <SupplierSettlementTab key={supplierSession?.supplierId} />}
            {visibleTab === 'team' && <SupplierTeamTab />}
            {visibleTab === 'analytics' && <SupplierAnalyticsTab />}
            {visibleTab === 'demand-insight' && <SupplierDemandInsightTab onPropose={() => navigateTab('products', 'propose')} />}
            {visibleTab === 'customer-returns' && <SupplierCustomerReturnsTab />}
            {visibleTab === 'my-warehouses' && <SupplierMyWarehousesTab />}
            {visibleTab === 'shipping-methods' && <SupplierShippingMethodsTab />}
            {visibleTab === 'profile' && <SupplierProfileTab />}
            {visibleTab === 'contract' && <SupplierContractTab />}
            {visibleTab === 'support' && <SellerSupportTab role="supplier" />}
          </div>
      </PanelWorkspace>
      <SupplierNotificationDrawer
        isOpen={isNotifOpen}
        onClose={() => setIsNotifOpen(false)}
        onSelectProposal={handleSelectNotifProposal}
        onUnreadCountChange={setUnreadNotifCount}
      />
    </SupplierAuthGuard>
  );
};
