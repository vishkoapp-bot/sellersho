import { useCallback, useEffect, useRef, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { warehouseNotificationsApi } from '../../services/notificationsApi';
import { ErrorBoundary } from '../ErrorBoundary';
import { PanelWorkspace } from '../common/PanelWorkspace';
import { RoleNotificationDrawer } from '../common/RoleNotificationDrawer';
import { WarehouseAuthGuard } from './WarehouseAuthGuard';
import { WarehouseInboundTab } from './WarehouseInboundTab';
import { WarehouseInventoryTab } from './WarehouseInventoryTab';
import {
  warehouseNavigation, warehouseSections, warehouseSectionFromSearch, warehouseSectionHref,
  WarehouseWorkflow, type WarehouseSection,
} from './WarehouseNavigation';
import { WarehouseCustomerReturnsTab, WarehouseReceiveTab, WarehouseReturnBacksTab } from './WarehouseNewTabs';
import { WarehouseOverviewTab } from './WarehouseOverviewTab';
import { WarehousePackingTab } from './WarehousePackingTab';
import { WarehousePickingTab } from './WarehousePickingTab';
import { WarehousePutawayTab } from './WarehousePutawayTab';
import { WarehouseQCTab } from './WarehouseQCTab';
import { WarehouseReturnsTab } from './WarehouseReturnsTab';
import { WarehouseShippingTab } from './WarehouseShippingTab';

interface Handoff {
  inboundId?: string;
  receiptLineIds?: string[];
  binCode?: string;
  orderNumber?: string;
  orderId?: string;
}

interface WarehouseLocation {
  section: WarehouseSection;
  scope?: string;
  handoff?: Handoff;
}

function readWarehouseLocation(): WarehouseLocation {
  if (typeof window === 'undefined') return { section: 'overview' };
  const section = warehouseSectionFromSearch(window.location.search);
  const saved: WarehouseLocation | undefined = window.history.state?.warehouseWorkflow;
  return saved?.section === section ? { section, scope: saved.scope, handoff: saved.handoff } : { section };
}

export function WarehouseDashboardView({ onBackToCustomerSite }: { onBackToCustomerSite: () => void }) {
  const { warehouseSession, logoutWarehouse, toPersianDigits } = useStore();
  const [location, setLocation] = useState(readWarehouseLocation);
  const mainRef = useRef<HTMLElement>(null);
  const scope = warehouseSession ? `${warehouseSession.staffId}:${warehouseSession.warehouseId}` : undefined;
  const { section } = location;
  const activeTitle = warehouseSections.find(item => item.id === section)?.label ?? 'پیشخوان انبار';
  const handoff = scope && location.scope === scope ? location.handoff : undefined;
  const [unreadCount, setUnreadCount] = useState(0);
  const [notificationsOpen, setNotificationsOpen] = useState(false);

  useEffect(() => {
    if (!warehouseSession?.token) return;
    let cancelled = false;
    warehouseNotificationsApi.getNotifications()
      .then(result => { if (!cancelled) setUnreadCount(result.unreadCount); })
      .catch(() => undefined);
    return () => { cancelled = true; };
  }, [warehouseSession?.token]);

  useEffect(() => {
    const restoreLocation = () => {
      setLocation(readWarehouseLocation());
      window.requestAnimationFrame(() => mainRef.current?.focus({ preventScroll: true }));
    };
    window.addEventListener('popstate', restoreLocation);
    return () => window.removeEventListener('popstate', restoreLocation);
  }, []);

  const navigate = useCallback((next: WarehouseSection, task?: Handoff) => {
    const href = warehouseSectionHref(next);
    const current = readWarehouseLocation();
    const entry: WarehouseLocation = { section: next, scope, handoff: task };
    if (href !== window.location.pathname + window.location.search + window.location.hash ||
      current.scope !== scope || JSON.stringify(current.handoff) !== JSON.stringify(task)) {
      // Task identities follow this history entry. The shareable URL only
      // identifies the queue, and another signed-in operator gets its own view.
      window.history.pushState({ activeView: 'warehouse_panel', warehouseWorkflow: entry }, '', href);
    }
    setLocation(entry);
    window.requestAnimationFrame(() => {
      mainRef.current?.focus({ preventScroll: true });
      window.scrollTo({ top: 0, behavior: 'instant' });
    });
  }, [scope]);

  const goToQC = (inboundId?: string) => navigate('qc', inboundId ? { inboundId } : undefined);
  const goToPutaway = (inboundId?: string) => navigate('putaway', inboundId ? { inboundId } : undefined);
  const outboundSelection = { selectedOrderNumber: handoff?.orderNumber, selectedOrderId: handoff?.orderId };

  return (
    <WarehouseAuthGuard onBackToCustomerSite={onBackToCustomerSite}>
      <PanelWorkspace id="warehouse" label="انبار" name={warehouseSession?.staffName || 'پنل انبار'}
        title={activeTitle} current={section} groups={warehouseNavigation} href={warehouseSectionHref} onNavigate={navigate}
        onBackToSite={onBackToCustomerSite} onLogout={logoutWarehouse} logoutLabel="خروج از شیفت انبار"
        onOpenNotifications={() => setNotificationsOpen(true)} unreadCount={unreadCount}
        formatCount={toPersianDigits} mainRef={mainRef}>
        <ErrorBoundary key={`${scope}:${section}:${JSON.stringify(handoff ?? {})}`}
          title={activeTitle} onReset={() => navigate('overview')}>
          <div className="space-y-4 select-text">
            <WarehouseWorkflow section={section} navigate={navigate} />
            {section === 'overview' && <WarehouseOverviewTab onNavigateTab={navigate} />}
            {section === 'receive' && <WarehouseReceiveTab onGoToQC={receiptLineIds => navigate('qc', { receiptLineIds })} />}
            {section === 'inbound' && <WarehouseInboundTab onGoToQC={goToQC} />}
            {section === 'qc' && <WarehouseQCTab initialInboundId={handoff?.inboundId}
              initialReceiptLineIds={handoff?.receiptLineIds} onGoToPutaway={goToPutaway} />}
            {section === 'putaway' && <WarehousePutawayTab initialInboundId={handoff?.inboundId}
              onGoToPicking={() => navigate('picking')} onGoToInventory={binCode => navigate('inventory', { binCode })} />}
            {section === 'inventory' && <WarehouseInventoryTab initialBinCode={handoff?.binCode} />}
            {section === 'picking' && <WarehousePickingTab onGoToPacking={(orderNumber, orderId) => navigate('packing', { orderId, orderNumber })} />}
            {section === 'packing' && <WarehousePackingTab {...outboundSelection}
              onGoToShipping={(orderNumber, orderId) => navigate('shipping', { orderId, orderNumber })} />}
            {section === 'shipping' && <WarehouseShippingTab {...outboundSelection} />}
            {section === 'returns' && <WarehouseReturnsTab />}
            {section === 'customer-returns' && <WarehouseCustomerReturnsTab />}
            {section === 'return-backs' && <WarehouseReturnBacksTab />}
          </div>
        </ErrorBoundary>
      </PanelWorkspace>
      <RoleNotificationDrawer isOpen={notificationsOpen} onClose={() => setNotificationsOpen(false)} title="اعلان‌های انبار"
        emptyTitle="اعلان جدیدی نیست." emptyHint="ورود محموله و مرجوعیِ آمادهٔ کنترل کیفیت اینجا نمایش داده می‌شوند."
        accent="brand" api={warehouseNotificationsApi} onUnreadCountChange={setUnreadCount} panelDialogId="warehouse-notifications-title" />
    </WarehouseAuthGuard>
  );
}
