import { useEffect, useRef, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { AdminLoginPage } from './AdminLoginPage';
import { RoleNotificationDrawer } from '../common/RoleNotificationDrawer';
import { adminNotificationsApi } from '../../services/notificationsApi';
import { apiRequest } from '../../services/apiClient';
import { AdminOverviewTab } from './AdminOverviewTab';
import { AdminOrdersTab } from './AdminOrdersTab';
import { AdminStoresTab } from './AdminStoresTab';
import { AdminSuppliersTab } from './AdminSuppliersTab';
import { AdminCatalogTab } from './AdminCatalogTab';
import { AdminCommissionTab } from './AdminCommissionTab';
import { AdminWarehousesTab } from './AdminWarehousesTab';
import { AdminCampaignsPlansTab } from './AdminCampaignsPlansTab';
import { AdminAdNetworkTab } from './AdminAdNetworkTab';
import { AdminJobsQueueTab } from './AdminJobsQueueTab';
import { AdminReferralNetworkTab } from './AdminReferralNetworkTab';

// Merged Official Finance & Support Sub-Modules (formerly standalone Phase 7)
import { FinanceOverviewTab } from '../finance/FinanceOverviewTab';
import { FinanceReconciliationTab } from '../finance/FinanceReconciliationTab';
import { FinancePayoutsTab } from '../finance/FinancePayoutsTab';
import { FinancePendingPaymentsTab } from '../finance/FinancePendingPaymentsTab';
import { FinanceAdjustmentsTab } from '../finance/FinanceAdjustmentsTab';
import { SupportTicketsTab } from '../finance/SupportTicketsTab';
import { SupportDisputesTab } from '../finance/SupportDisputesTab';
import { HistoricalReconciliationReviewTab } from './HistoricalReconciliationReviewTab';
import { AdminSmsTab } from './AdminSmsTab';
import { AdminSettlementSettingsTab } from './AdminSettlementSettingsTab';
import { AdminStorageFeeSettingsTab } from './AdminStorageFeeSettingsTab';
import { AdminShippingSettingsTab } from './AdminShippingSettingsTab';
import {
  AdminStoresListTab,
  AdminHomepageTab,
  AdminBannersTab,
  AdminSiteContentTab,
  AdminCallbackRetriesTab,
  AdminNewsletterTab,
  AdminReviewsTab,
  AdminStaffTab,
  AdminSupportReturnsTab,
} from './AdminNewTabs';
import { AdminAuditLogTab } from './AdminAuditLogTab';
import { AdminOperationsTab } from './AdminOperationsTab';
import { AdminSpncxTab } from './AdminSpncxTab';

import { PanelWorkspace } from '../common/PanelWorkspace';
import { ErrorBoundary } from '../ErrorBoundary';
import {
  adminDefaultSection, adminNavigationFor, adminSectionHref, findAdminSection, requestedAdminSection,
} from './AdminNavigation';

interface AdminOverviewAlert {
  id: string;
  title: string;
  count: number;
  tab: string;
}

export function AdminDashboardView({ onBackToCustomerSite }: { onBackToCustomerSite: () => void }) {
  const { adminSession, logoutAdmin, toPersianDigits } = useStore();
  const { warning } = useToast();
  const [requested, setRequested] = useState(requestedAdminSection);
  const mainRef = useRef<HTMLElement>(null);
  const [unreadCount, setUnreadCount] = useState(0);
  const [notificationsOpen, setNotificationsOpen] = useState(false);
  const [alerts, setAlerts] = useState<AdminOverviewAlert[]>([]);
  const role = adminSession?.role;
  const requestedItem = findAdminSection(requested);
  const permittedItem = role && requestedItem?.roles.includes(role) ? requestedItem : undefined;
  const activeTab = permittedItem?.id ?? (role ? adminDefaultSection[role] : undefined);

  useEffect(() => {
    const restore = () => setRequested(requestedAdminSection());
    window.addEventListener('popstate', restore);
    return () => window.removeEventListener('popstate', restore);
  }, []);

  useEffect(() => {
    if (!activeTab) return;
    window.scrollTo({ top: 0, behavior: 'instant' });
    mainRef.current?.focus({ preventScroll: true });
  }, [activeTab]);

  useEffect(() => {
    if (!adminSession?.token) return;
    let cancelled = false;
    setUnreadCount(0);
    adminNotificationsApi.getNotifications()
      .then(result => { if (!cancelled) setUnreadCount(result.unreadCount); })
      .catch(() => undefined);
    return () => { cancelled = true; };
  }, [adminSession?.token]);

  useEffect(() => {
    if (!notificationsOpen || !adminSession?.token || (role !== 'admin' && role !== 'super_admin')) return;
    const request = new AbortController();
    setAlerts([]);
    apiRequest<{ data: AdminOverviewAlert[] }>('/admin/overview/alerts', { auth: true, signal: request.signal })
      .then(result => { if (!request.signal.aborted) setAlerts(result.data ?? []); })
      .catch(() => undefined);
    return () => request.abort();
  }, [notificationsOpen, adminSession?.token, role]);

  if (!adminSession?.token || !role || !activeTab) {
    return <AdminLoginPage onBackToCustomerSite={onBackToCustomerSite} />;
  }

  const groups = adminNavigationFor(role);
  const title = findAdminSection(activeTab)?.label ?? 'پیشخوان مدیریت';
  function navigate(value: string) {
    const item = findAdminSection(value);
    if (!role || !item?.roles.includes(role)) {
      warning('این بخش برای حساب شما در دسترس نیست.');
      return;
    }
    const href = adminSectionHref(item.id);
    if (href !== window.location.pathname + window.location.search + window.location.hash) {
      window.history.pushState({ activeView: 'admin_panel' }, '', href);
    }
    setRequested(item.id === 'overview' ? null : item.id);
  }

  const tabs = {
    'overview': () => <AdminOverviewTab onNavigateTab={navigate} />,
    'stores': () => <AdminStoresTab />,
    'suppliers': () => <AdminSuppliersTab />,
    'catalog': () => <AdminCatalogTab />,
    'orders': () => <AdminOrdersTab />,
    'commission': () => <AdminCommissionTab />,
    'warehouses': () => <AdminWarehousesTab />,
    'historical-reconciliation': () => <HistoricalReconciliationReviewTab enableSpncx={adminSession.role === 'super_admin'} />,
    'campaigns': () => <AdminCampaignsPlansTab />,
    'ad-network': () => <AdminAdNetworkTab />,
    'referral-network': () => <AdminReferralNetworkTab />,
    'jobs-queue': () => <AdminJobsQueueTab />,
    'sms': () => <AdminSmsTab />,
    'finance-overview': () => <FinanceOverviewTab onNavigateTab={navigate} />,
    'finance-reconciliation': () => <FinanceReconciliationTab />,
    'finance-payouts': () => <FinancePayoutsTab />,
    'settlement-settings': () => <AdminSettlementSettingsTab />,
    'storage-fee-settings': () => <AdminStorageFeeSettingsTab />,
    'shipping-settings': () => <AdminShippingSettingsTab />,
    'finance-adjustments': () => <FinanceAdjustmentsTab />,
    'support-disputes': () => <SupportDisputesTab />,
    'support-tickets': () => <SupportTicketsTab />,
    'stores-list': () => <AdminStoresListTab />,
    'homepage': () => <AdminHomepageTab />,
    'banners': () => <AdminBannersTab />,
    'site-content': () => <AdminSiteContentTab />,
    'finance-pending-payments': () => <FinancePendingPaymentsTab />,
    'operations': () => <AdminOperationsTab />,
    'spncx-backfill': () => <AdminSpncxTab />,
    'finance-callback-retries': () => <AdminCallbackRetriesTab />,
    'newsletter': () => <AdminNewsletterTab />,
    'reviews': () => <AdminReviewsTab />,
    'support-returns': () => <AdminSupportReturnsTab />,
    'staff': () => <AdminStaffTab />,
    'audit-logs': () => <AdminAuditLogTab />,

  };
  const linkedAlerts = alerts.filter(alert => findAdminSection(alert.tab)?.roles.includes(role));

  return (
    <>
      <PanelWorkspace id="admin" label="مدیریت" name={adminSession.fullName || 'پنل مدیریت'}
        title={title} current={activeTab} groups={groups} href={adminSectionHref} onNavigate={navigate}
        onBackToSite={onBackToCustomerSite} onLogout={logoutAdmin}
        onOpenNotifications={() => setNotificationsOpen(true)} unreadCount={unreadCount}
        formatCount={toPersianDigits} mainRef={mainRef}>
        {requested && !permittedItem && (
          <div role="status" className="mb-4 rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
            {requestedItem ? 'این بخش برای حساب شما در دسترس نیست. بخش مجاز حساب شما باز شد.' : 'این بخش پیدا نشد. بخش اصلی پنل باز شد.'}
          </div>
        )}
        <ErrorBoundary key={activeTab} title={title} onReset={() => navigate(adminDefaultSection[role])}>
          <div className="select-text">{tabs[activeTab]()}</div>
        </ErrorBoundary>
      </PanelWorkspace>
      <RoleNotificationDrawer
        isOpen={notificationsOpen} onClose={() => setNotificationsOpen(false)} title="اعلان‌های مدیریت"
        emptyTitle="اعلان جدیدی نیست." emptyHint="درخواست‌ها و رویدادهای مربوط به حساب شما اینجا نمایش داده می‌شوند."
        accent="brand" api={adminNotificationsApi} onUnreadCountChange={setUnreadCount} panelDialogId="admin-notifications-title"
        extraContent={linkedAlerts.length > 0 ? (
          <section aria-label="هشدارهای نیازمند رسیدگی" className="space-y-2 rounded-xl border border-amber-200 bg-amber-50 p-3">
            <h4 className="text-sm font-bold text-amber-900">هشدارهای نیازمند رسیدگی</h4>
            {linkedAlerts.map(alert => (
              <button key={alert.id} type="button" onClick={() => { setNotificationsOpen(false); navigate(alert.tab); }}
                className="flex min-h-11 w-full items-center justify-between gap-2 rounded-lg border border-amber-200 bg-white px-3 py-2 text-start text-sm font-bold text-slate-800 hover:bg-amber-50 focus-visible:outline-2 focus-visible:outline-brand">
                <span>{alert.title}</span>
                <span className="shrink-0 text-amber-900">{toPersianDigits(alert.count)}</span>
              </button>
            ))}
          </section>
        ) : undefined} />
    </>
  );
}
