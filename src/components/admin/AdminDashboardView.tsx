import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { AdminRole } from '../../context/storeContextTypes';
import { 
  ArrowRight, 
  LayoutDashboard, 
  Store, 
  Building2, 
  Package, 
  DollarSign, 
  Warehouse, 
  Crown, 
  Activity, 
  Cpu,
  BadgePercent,
  Receipt,
  ArrowDownToLine,
  FileEdit,
  Headphones,
  ShieldAlert,
  LogOut,
  ShieldCheck,
  Lock,
  UserCheck,
  ClipboardCheck,
  MessageSquare,
  CalendarClock,
  Home,
  Image,
  FileText,
  Clock,
  RefreshCw,
  Mail,
  Star,
  RotateCcw,
  Users,
  ChevronDown,
} from 'lucide-react';

import { AdminLoginPage } from './AdminLoginPage';
import { AdminOverviewTab } from './AdminOverviewTab';
import { AdminStoresTab } from './AdminStoresTab';
import { AdminSuppliersTab } from './AdminSuppliersTab';
import { AdminCatalogTab } from './AdminCatalogTab';
import { AdminCommissionTab } from './AdminCommissionTab';
import { AdminWarehousesTab } from './AdminWarehousesTab';
import { AdminCampaignsPlansTab } from './AdminCampaignsPlansTab';
import { AdminAdNetworkTab } from './AdminAdNetworkTab';
import { AdminJobsQueueTab } from './AdminJobsQueueTab';
import { AdminReferralNetworkTab } from './AdminReferralNetworkTab';
import { GitFork } from 'lucide-react';

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
import { AdminPlaceholderTab, AdminSupportReturnsTab } from './AdminNewTabs';
import { AdminAuditLogTab } from './AdminAuditLogTab';

interface Props {
  onBackToCustomerSite: () => void;
}

interface AdminNavItem {
  id: string;
  label: string;
  icon: React.FC<{ className?: string }>;
  allowedRoles: AdminRole[];
}

interface AdminNavSection {
  heading: string | null;
  variant: 'slate' | 'teal';
  items: AdminNavItem[];
}

// FIX 17: live admin navigation (AdminDashboardView-MERGED.js):
// 30 items in 7 groups, in this exact order. WORKSTREAM 1:
// the 14 labels below are live-bundle-verbatim; the two former
// heading:null sections are merged into the store/finance groups.
// Group ids are bundle-proven (overview/store/content/finance/
// marketing/support/system); the Persian headings are the natural
// labels for those groups. 11 items were missing; 'pricing-engine'
// (tiered pricing) is DELIBERATELY omitted per user decision Q12,
// and 'settlement-settings' is a DELIBERATE Phase-3 addition kept
// in the finance section, so the candidate shows 30 items.
// Role note (live bundle AdminDashboardView-MERGED.js nav list):
// plain 'admin' is allowed on 28 of live's 30 items; only
// 'stores-list' and 'staff' exclude it. The candidate mirrors that
// here, except 'settlement-settings' (a Phase-3 addition with no live
// counterpart), which stays super_admin-only.
const ADMIN_NAV_SECTIONS: AdminNavSection[] = [
  { heading: 'نمای کلی', variant: 'slate', items: [
    { id: 'overview', label: 'نمای کلی', icon: LayoutDashboard, allowedRoles: ['super_admin', 'admin'] },
  ] },
  { heading: 'فروشگاه', variant: 'slate', items: [
    { id: 'stores', label: 'ویترین‌های دراپ‌شیپر', icon: Store, allowedRoles: ['super_admin', 'support_specialist', 'admin'] },
    { id: 'stores-list', label: 'لیست فروشگاه‌ها', icon: Store, allowedRoles: ['super_admin'] },
    { id: 'suppliers', label: 'تأمین‌کنندگان', icon: Building2, allowedRoles: ['super_admin', 'logistics_specialist', 'admin'] },
    { id: 'catalog', label: 'تأیید کالاها', icon: Package, allowedRoles: ['super_admin', 'logistics_specialist', 'admin'] },
    { id: 'warehouses', label: 'انبارها و تعرفه‌ها', icon: Warehouse, allowedRoles: ['super_admin', 'logistics_specialist', 'admin'] },
    { id: 'historical-reconciliation', label: 'تطبیق تاریخی', icon: ClipboardCheck, allowedRoles: ['super_admin', 'logistics_specialist', 'admin'] },
  ] },
  { heading: 'محتوا و ظاهر', variant: 'slate', items: [
    { id: 'homepage', label: 'صفحه اصلی', icon: Home, allowedRoles: ['super_admin', 'admin'] },
    { id: 'banners', label: 'بنرها', icon: Image, allowedRoles: ['super_admin', 'admin'] },
    { id: 'site-content', label: 'محتوای سایت و صفحات', icon: FileText, allowedRoles: ['super_admin', 'admin'] },
  ] },
  { heading: 'مالی', variant: 'teal', items: [
    { id: 'finance-overview', label: 'تراز و دفتر کل', icon: BadgePercent, allowedRoles: ['super_admin', 'finance_specialist', 'admin'] },
    { id: 'finance-pending-payments', label: 'تأیید دستی پرداخت', icon: Clock, allowedRoles: ['super_admin', 'finance_specialist', 'admin'] },
    { id: 'finance-reconciliation', label: 'تطبیق شاپرک', icon: Receipt, allowedRoles: ['super_admin', 'finance_specialist', 'admin'] },
    { id: 'finance-callback-retries', label: 'پرداخت‌های ناموفق', icon: RefreshCw, allowedRoles: ['super_admin', 'finance_specialist', 'admin'] },
    { id: 'finance-payouts', label: 'تسویه شبا', icon: ArrowDownToLine, allowedRoles: ['super_admin', 'finance_specialist', 'admin'] },
    { id: 'finance-adjustments', label: 'تعدیل و برگشتی', icon: FileEdit, allowedRoles: ['super_admin', 'finance_specialist', 'admin'] },
    { id: 'commission', label: 'سهم پلتفرم', icon: DollarSign, allowedRoles: ['super_admin', 'admin'] },
    { id: 'settlement-settings', label: 'تنظیمات تسویه ماهانه', icon: CalendarClock, allowedRoles: ['super_admin'] },
    { id: 'storage-fee-settings', label: 'هزینه انبارداری', icon: Warehouse, allowedRoles: ['super_admin'] },
  ] },
  { heading: 'بازاریابی', variant: 'slate', items: [
    { id: 'campaigns', label: 'کمپین‌ها و بودجه', icon: Crown, allowedRoles: ['super_admin', 'admin'] },
    { id: 'ad-network', label: 'تبلیغات و ضدتقلب', icon: Activity, allowedRoles: ['super_admin', 'support_specialist', 'admin'] },
    { id: 'referral-network', label: 'دعوت همکاران', icon: GitFork, allowedRoles: ['super_admin', 'admin'] },
    { id: 'newsletter', label: 'مشترکان خبرنامه', icon: Mail, allowedRoles: ['super_admin', 'admin'] },
  ] },
  { heading: 'پشتیبانی', variant: 'slate', items: [
    { id: 'support-tickets', label: 'تیکت‌های پشتیبانی', icon: Headphones, allowedRoles: ['super_admin', 'finance_specialist', 'support_specialist', 'admin'] },
    { id: 'support-disputes', label: 'داوری و مرجوعی', icon: ShieldAlert, allowedRoles: ['super_admin', 'finance_specialist', 'logistics_specialist', 'support_specialist', 'admin'] },
    { id: 'reviews', label: 'نظرات مشتریان', icon: Star, allowedRoles: ['super_admin', 'support_specialist', 'admin'] },
    { id: 'support-returns', label: 'مرجوعی‌ها', icon: RotateCcw, allowedRoles: ['super_admin', 'finance_specialist', 'logistics_specialist', 'support_specialist', 'admin'] },
  ] },
  { heading: 'سیستم', variant: 'slate', items: [
    { id: 'staff', label: 'کارکنان انبار', icon: Users, allowedRoles: ['super_admin'] },
    { id: 'sms', label: 'پیامک و اعتبار', icon: MessageSquare, allowedRoles: ['super_admin', 'admin'] },
    { id: 'jobs-queue', label: 'صف کارها', icon: Cpu, allowedRoles: ['super_admin', 'admin'] },
    { id: 'audit-logs', label: 'گزارش حسابرسی', icon: ShieldCheck, allowedRoles: ['super_admin', 'admin'] },
  ] },
];

const ADMIN_NAV_ITEMS: AdminNavItem[] = ADMIN_NAV_SECTIONS.flatMap((s) => s.items);

export const AdminDashboardView: React.FC<Props> = ({ onBackToCustomerSite }) => {
  const { adminSession, logoutAdmin } = useStore();
  const { info, warning } = useToast();
  const [activeTab, setActiveTab] = useState<string>('overview');

  // Automatically set default permitted tab when role changes
  useEffect(() => {
    if (!adminSession) return;
    if (adminSession.role === 'finance_specialist') {
      setActiveTab('finance-overview');
    } else if (adminSession.role === 'logistics_specialist') {
      setActiveTab('warehouses');
    } else if (adminSession.role === 'support_specialist') {
      setActiveTab('support-tickets');
    } else {
      setActiveTab('overview');
    }
  }, [adminSession?.role]);

  // WORKSTREAM 1: one collapsible card per admin group (live Os groups).
  // All groups start expanded; the group holding the active tab is
  // re-opened automatically so the active item is never hidden.
  // Group header icon is the group's first item icon (live groups
  // carry the same icon as their first item, e.g. store/nt).
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(
    () => new Set(ADMIN_NAV_SECTIONS.map((s) => s.heading ?? s.items[0].id)),
  );
  const toggleGroup = (groupKey: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(groupKey)) {
        next.delete(groupKey);
      } else {
        next.add(groupKey);
      }
      return next;
    });
  };
  useEffect(() => {
    const group = ADMIN_NAV_SECTIONS.find((s) => s.items.some((i) => i.id === activeTab));
    if (group) {
      const groupKey = group.heading ?? group.items[0].id;
      setExpandedGroups((prev) => (prev.has(groupKey) ? prev : new Set(prev).add(groupKey)));
    }
  }, [activeTab]);

  // 1. SESSION GUARD: Block all /admin routes if no valid session token exists
  if (!adminSession || !adminSession.token) {
    return <AdminLoginPage onBackToCustomerSite={onBackToCustomerSite} />;
  }

  const currentNavItem = ADMIN_NAV_ITEMS.find((item) => item.id === activeTab);
  const isAuthorizedForCurrentTab =
    currentNavItem && currentNavItem.allowedRoles.includes(adminSession.role);

  const visibleSections = ADMIN_NAV_SECTIONS.map((section) => ({
    ...section,
    items: section.items.filter((i) => i.allowedRoles.includes(adminSession.role)),
  })).filter((s) => s.items.length > 0);

  const handleNavigate = (targetTab: string) => {
    const normalized =
      targetTab === 'payouts'
        ? 'finance-payouts'
        : targetTab === 'disputes'
        ? 'support-disputes'
        : targetTab === 'reconciliation'
        ? 'finance-reconciliation'
        : targetTab === 'adjustments'
        ? 'finance-adjustments'
        : targetTab === 'tickets'
        ? 'support-tickets'
        : targetTab;

    const targetItem = ADMIN_NAV_ITEMS.find((i) => i.id === normalized);
    if (targetItem && !targetItem.allowedRoles.includes(adminSession.role)) {
      warning('دسترسی به این بخش فقط برای نقش «مدیر ارشد پلتفرم» مجاز است.');
      return;
    }
    setActiveTab(normalized);
  };

  const handleLogout = () => {
    logoutAdmin();
    info('نشست سازمانی شما با موفقیت خاتمه یافت و توکن امنیتی پاک‌سازی شد.');
  };

  return (
    <div className="min-h-screen w-full bg-[#f1f5f9] text-slate-800 flex flex-col select-none">
      {/* Standalone Enterprise Top Header (Outside 504px Customer Shell) */}
      <header className="sticky top-0 z-40 bg-slate-950 text-white border-b border-slate-800 px-4 lg:px-8 py-3 flex flex-wrap items-center justify-between gap-3 shadow-md">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-xl bg-gradient-to-tr from-blue-600 to-indigo-600 flex items-center justify-center text-white shadow-sm">
            <ShieldCheck className="w-5 h-5"  strokeWidth={1.5}/>
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-sm font-extrabold tracking-tight">
                سامانه مدیریت یکپارچه و دفتر مالی سلرشو
              </h1>
              <span className="text-[10px] font-mono bg-blue-500/20 text-blue-300 border border-blue-500/30 px-2 py-0.5 rounded-full" dir="ltr">
                /admin/{activeTab}
              </span>
            </div>
            <span className="text-[11px] text-slate-400 block">
              کاربر فعال: <strong className="text-slate-200">{adminSession.fullName}</strong> • ساعت ورود: {adminSession.loginAt}
            </span>
          </div>
        </div>

        {/* Right Controls: Customer Site return and Logout */}
        <div className="flex items-center gap-2 flex-wrap">

          <button
            onClick={onBackToCustomerSite}
            className="px-3 py-1.5 rounded-xl bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 text-xs font-bold flex items-center gap-1.5 transition-colors"
          >
            <ArrowRight className="w-3.5 h-3.5 text-blue-400"  strokeWidth={1.5}/>
            <span>مشاهده سایت مشتری</span>
          </button>

          <button
            onClick={handleLogout}
            className="px-3 py-1.5 rounded-xl bg-rose-600/20 hover:bg-rose-600 text-rose-300 hover:text-white border border-rose-500/30 text-xs font-bold flex items-center gap-1.5 transition-colors"
            title="پاک‌سازی نشست و خروج"
          >
            <LogOut className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>خروج امن</span>
          </button>
        </div>
      </header>

      {/* Main Standalone Workspace (Sidebar + Content) */}
      <div className="flex-1 max-w-7xl w-full mx-auto flex flex-col lg:flex-row gap-6 p-4 lg:p-6">
        {/* Sidebar Navigation (Filtered by Role) */}
        <aside className="w-full lg:w-72 shrink-0 space-y-4">
          {/* Active Role Badge Card */}
          <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              <div
                className={`w-9 h-9 rounded-xl flex items-center justify-center text-white ${
                  adminSession.role === 'super_admin'
                    ? 'bg-blue-600'
                    : adminSession.role === 'finance_specialist'
                    ? 'bg-teal-600'
                    : adminSession.role === 'logistics_specialist'
                    ? 'bg-amber-600'
                    : 'bg-purple-600'
                }`}
              >
                <UserCheck className="w-4 h-4"  strokeWidth={1.5}/>
              </div>
              <div>
                <span className="text-[10px] text-slate-400 block">نقش سازمانی فعال</span>
                <span className="text-xs font-extrabold text-slate-900">
                  {adminSession.roleLabel}
                </span>
              </div>
            </div>
            <span className="text-[9px] font-bold px-2 py-0.5 rounded-full bg-emerald-50 text-emerald-700 border border-emerald-200">
              توکن معتبر
            </span>
          </div>

          {visibleSections.map((section) => {
            const groupKey = section.heading ?? section.items[0].id;
            const isExpanded = expandedGroups.has(groupKey);
            const GroupIcon = section.items[0].icon;
            const teal = section.variant === 'teal';
            return (
              <div key={groupKey} className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-1">
                <button
                  onClick={() => toggleGroup(groupKey)}
                  aria-expanded={isExpanded}
                  className="flex w-full items-center gap-2.5 px-2 py-1.5 rounded-xl text-xs font-extrabold text-slate-900 hover:bg-slate-100 transition-colors text-right"
                >
                  <GroupIcon className="w-4 h-4 shrink-0 text-slate-400" />
                  <span className="flex-1 text-right">{section.heading}</span>
                  <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200">
                    {section.items.length}
                  </span>
                  <ChevronDown className={`w-4 h-4 shrink-0 text-slate-400 transition-transform ${isExpanded ? 'rotate-180' : ''}`} />
                </button>
                {isExpanded && (
                  <div className="border-r-2 border-slate-100 pr-2 space-y-1">
                    {section.items.map((item) => {
                      const Icon = item.icon;
                      const isActive = activeTab === item.id;
                      return (
                        <button
                          key={item.id}
                          onClick={() => handleNavigate(item.id)}
                          className={`flex items-center gap-2.5 px-3 py-2 rounded-xl text-xs font-bold whitespace-nowrap transition-all w-full text-right ${
                            isActive
                              ? teal ? 'bg-teal-700 text-white shadow-xs' : 'bg-slate-900 text-white shadow-xs'
                              : teal ? 'text-slate-600 hover:bg-teal-50/60 hover:text-teal-900' : 'text-slate-600 hover:bg-slate-100'
                          }`}
                        >
                          <Icon className={`w-4 h-4 shrink-0 ${isActive ? (teal ? 'text-teal-200' : 'text-blue-400') : (teal ? 'text-teal-600' : 'text-slate-400')}`} />
                          <span>{item.label}</span>
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          })}
        </aside>

        {/* Main Content Area with Real Route Guard */}
        <main className="flex-1 min-w-0">
          {!isAuthorizedForCurrentTab ? (
            <div className="bg-white rounded-3xl p-8 border border-rose-200 shadow-xs text-center max-w-lg mx-auto my-8 space-y-4">
              <div className="w-14 h-14 rounded-2xl bg-rose-50 text-rose-600 flex items-center justify-center mx-auto">
                <Lock className="w-7 h-7"  strokeWidth={1.5}/>
              </div>
              <h3 className="text-sm font-extrabold text-slate-900">
                محدودیت سطح دسترسی
              </h3>
              <p className="text-xs text-slate-500 leading-relaxed">
                نقش «{adminSession.roleLabel}» به این بخش دسترسی ندارد.
              </p>
            </div>
          ) : (
            <div className="max-w-3xl mx-auto">
              {/* Core Admin Tabs */}
              {activeTab === 'overview' && <AdminOverviewTab onNavigateTab={handleNavigate} />}
              {activeTab === 'stores' && <AdminStoresTab />}
              {activeTab === 'suppliers' && <AdminSuppliersTab />}
              {activeTab === 'catalog' && <AdminCatalogTab />}
              {activeTab === 'commission' && <AdminCommissionTab />}
              {activeTab === 'warehouses' && <AdminWarehousesTab />}
              {activeTab === 'historical-reconciliation' && <HistoricalReconciliationReviewTab />}
              {activeTab === 'campaigns' && <AdminCampaignsPlansTab />}
              {activeTab === 'ad-network' && <AdminAdNetworkTab />}
              {activeTab === 'referral-network' && <AdminReferralNetworkTab />}
              {activeTab === 'jobs-queue' && <AdminJobsQueueTab />}
              {activeTab === 'sms' && <AdminSmsTab />}

              {/* Merged Finance & Support Submenu Tabs */}
              {activeTab === 'finance-overview' && <FinanceOverviewTab onNavigateTab={handleNavigate} />}
              {activeTab === 'finance-reconciliation' && <FinanceReconciliationTab />}
              {activeTab === 'finance-payouts' && <FinancePayoutsTab />}
              {activeTab === 'settlement-settings' && <AdminSettlementSettingsTab />}
              {activeTab === 'storage-fee-settings' && <AdminStorageFeeSettingsTab />}
              {activeTab === 'finance-adjustments' && <FinanceAdjustmentsTab />}
              {activeTab === 'support-disputes' && <SupportDisputesTab />}
              {activeTab === 'support-tickets' && <SupportTicketsTab />}
              {activeTab === 'stores-list' && <AdminPlaceholderTab title="لیست فروشگاه‌ها" description="فهرست فروشگاه‌های پلتفرم؛ محتوای آن پس از ورود به حساب سوپرادمین نمایش داده می‌شود." />}
              {activeTab === 'homepage' && <AdminPlaceholderTab title="صفحه اصلی" description="مدیریت بخش‌های صفحه اصلی؛ محتوای آن پس از ورود به حساب سوپرادمین نمایش داده می‌شود." />}
              {activeTab === 'banners' && <AdminPlaceholderTab title="بنرها" description="مدیریت بنرهای صفحه اصلی؛ محتوای آن پس از ورود به حساب سوپرادمین نمایش داده می‌شود." />}
              {activeTab === 'site-content' && <AdminPlaceholderTab title="محتوای سایت و صفحات" description="مدیریت محتوای سایت و صفحات ثابت؛ محتوای آن پس از ورود به حساب سوپرادمین نمایش داده می‌شود." />}
              {activeTab === 'finance-pending-payments' && <FinancePendingPaymentsTab />}
              {activeTab === 'finance-callback-retries' && <AdminPlaceholderTab title="پرداخت‌های ناموفق" description="تلاش‌های مجدد بازگشت از درگاه؛ محتوای آن پس از ورود به حساب نمایش داده می‌شود." />}
              {activeTab === 'newsletter' && <AdminPlaceholderTab title="مشترکان خبرنامه" description="فهرست مشترکان خبرنامه؛ محتوای آن پس از ورود به حساب سوپرادمین نمایش داده می‌شود." />}
              {activeTab === 'reviews' && <AdminPlaceholderTab title="نظرات مشتریان" description="نظرات ثبت‌شده مشتریان؛ محتوای آن پس از ورود به حساب نمایش داده می‌شود." />}
              {activeTab === 'support-returns' && <AdminSupportReturnsTab />}
              {activeTab === 'staff' && <AdminPlaceholderTab title="کارکنان انبار" description="فهرست کارکنان انبار؛ محتوای آن پس از ورود به حساب سوپرادمین نمایش داده می‌شود." />}
              {activeTab === 'audit-logs' && <AdminAuditLogTab />}
            </div>
          )}
        </main>
      </div>
    </div>
  );
};