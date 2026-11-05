import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { SupplierAuthGuard } from './SupplierAuthGuard';
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
  Wallet
} from 'lucide-react';

import { SupplierOverviewTab } from './SupplierOverviewTab';
import { SupplierProductsTab } from './SupplierProductsTab';
import { SupplierInventoryTab } from './SupplierInventoryTab';
import { SupplierInboundFlowTab } from './SupplierInboundFlowTab';
import { SupplierDiscrepanciesTab } from './SupplierDiscrepanciesTab';
import { SupplierAdsCampaignTab } from './SupplierAdsCampaignTab';
import { SupplierSettlementTab } from './SupplierSettlementTab';
import { SupplierOrdersTab } from './SupplierOrdersTab';
import { SupplierTeamTab } from './SupplierTeamTab';
import { SupplierAnalyticsTab, SupplierCustomerReturnsTab, SupplierMyWarehousesTab, SupplierShippingMethodsTab, SupplierDemandInsightTab, SupplierContractTab, SupplierPlaceholderTab } from './SupplierNewTabs';
import { SupplierProfileTab } from './SupplierProfileTab';
import { supplierApi } from '../../services/supplierApi';
import { ShoppingBag } from 'lucide-react';

interface Props {
  onBackToCustomerSite: () => void;
}

export const SupplierDashboardView: React.FC<Props> = ({ onBackToCustomerSite }) => {
  const { supplierSession, setSupplierSession, logoutSupplier, platformSettings, toPersianDigits } = useStore();
  const { info, success, error } = useToast();
  const [activeTab, setActiveTab] = useState('overview');
  const [contractChecked, setContractChecked] = useState(false);
  const latestContractVersion = platformSettings.contracts?.supplierContractVersion || 0;
  const signedContractVersion = supplierSession?.signedContractVersion || 1;
  // Live parity (SupplierDashboardView-MERGED.js header, both occurrences): the header shows
  // نامشخص only when warehouseCityConfirmed is explicitly false, otherwise the city.
  // Cast: the flag rides on the session object at runtime (see AuthContext) but is not yet
  // declared on the SupplierSession type; drop the cast once the type field lands.
  const warehouseCityConfirmed = (supplierSession as typeof supplierSession & { warehouseCityConfirmed?: boolean })?.warehouseCityConfirmed;
  const showContractModal = !!supplierSession && latestContractVersion > 0 && signedContractVersion < latestContractVersion;
  const acceptContract = async () => {
    if (!contractChecked || !supplierSession) return;
    try {
      await supplierApi.acceptContract(latestContractVersion);
      setSupplierSession({ ...supplierSession, signedContractVersion: latestContractVersion });
      success(`قرارداد جدید (نسخه ${toPersianDigits(latestContractVersion)}) قبول و ثبت شد.`);
    } catch (err: any) {
      error(err.message || 'ثبت قبول قرارداد انجام نشد.');
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
    { id: 'demand-insight', label: 'اینسایت تقاضا', icon: TrendingUp },
    { id: 'orders', label: 'سفارش‌های من', icon: ShoppingBag },
    { id: 'customer-returns', label: 'مرجوعی‌های مشتریان', icon: RotateCcw },
    { id: 'products', label: 'کالاها و تنوع‌ها', icon: Package },
    { id: 'inventory', label: 'موجودی کالاها', icon: Boxes },
    { id: 'my-warehouses', label: 'انبارهای من', icon: Warehouse },
    { id: 'inbound', label: 'فرستادن بار به انبار', icon: PackagePlus },
    { id: 'shipping-methods', label: 'روش‌های ارسال', icon: Truck },
    { id: 'discrepancies', label: 'اختلاف‌های تحویل', icon: AlertTriangle },
    { id: 'settlement', label: 'تسویه‌حساب', icon: Receipt },
    { id: 'ads', label: 'تبلیغات', icon: Megaphone },
    { id: 'team', label: 'همکاران من', icon: Users },
    { id: 'profile', label: 'پروفایل', icon: User },
    { id: 'contract', label: 'قرارداد من', icon: FileText },
  ];

  const tabPermissions: Record<string, string> = { team: 'supplier.manage', products: 'products.manage', inventory: 'inventory.manage', inbound: 'inventory.manage', discrepancies: 'inventory.manage', orders: 'orders.manage', ads: 'marketing.manage', settlement: 'finance.manage', overview: 'supplier.manage', analytics: 'supplier.manage', 'demand-insight': 'supplier.manage', 'customer-returns': 'orders.manage', 'my-warehouses': 'inventory.manage', 'shipping-methods': 'orders.manage', profile: 'supplier.manage', contract: 'supplier.manage' };
  const permissions = supplierSession?.permissions || [];
  const allowedTabs = menuItems.filter((item) => permissions.includes('supplier.manage') || permissions.includes(tabPermissions[item.id]));
  const visibleTab = allowedTabs.some((item) => item.id === activeTab) ? activeTab : allowedTabs[0]?.id;

  // Desktop sidebar grouping (layout only): the same permission-filtered
  // tabs as the mobile tab bar, partitioned into sidebar groups covering
  // all 16 tab ids exactly once. A group with no permitted tab disappears
  // entirely, so gating is inherited unchanged.
  const menuGroups: { heading: string; icon: typeof LayoutDashboard; ids: string[] }[] = [
    { heading: 'پیشخوان', icon: LayoutDashboard, ids: ['overview', 'analytics', 'demand-insight'] },
    { heading: 'سفارش و ارسال', icon: Truck, ids: ['orders', 'shipping-methods', 'discrepancies', 'customer-returns'] },
    { heading: 'کالا و انبار', icon: Warehouse, ids: ['products', 'inventory', 'my-warehouses', 'inbound'] },
    { heading: 'مالی', icon: Wallet, ids: ['settlement'] },
    { heading: 'بازاریابی', icon: Megaphone, ids: ['ads'] },
    { heading: 'حساب کاربری', icon: User, ids: ['team', 'profile', 'contract'] },
  ];
  const sidebarGroups = menuGroups
    .map((group) => ({
      heading: group.heading,
      icon: group.icon,
      items: group.ids
        .map((id) => allowedTabs.find((item) => item.id === id))
        .filter((item): item is (typeof menuItems)[number] => !!item),
    }))
    .filter((group) => group.items.length > 0);

  // Active tab label for the desktop header title slot (item: at lg the
  // header stops repeating the identity and names the current section).
  const activeTabLabel = allowedTabs.find((item) => item.id === visibleTab)?.label;
  // Warehouse status pill for the sidebar identity card, driven by the same
  // warehouseCityConfirmed flag as the header line: explicitly unconfirmed
  // -> amber; a recorded city -> confirmed (emerald); no city -> neutral.
  const warehousePill = warehouseCityConfirmed === false
    ? { text: 'انبار تأییدنشده', cls: 'bg-amber-50 text-amber-700 border-amber-200' }
    : supplierSession?.warehouseCity
      ? { text: 'انبار تأییدشده', cls: 'bg-emerald-50 text-emerald-700 border-emerald-200' }
      : { text: 'بدون انبار ثبت‌شده', cls: 'bg-slate-50 text-slate-500 border-slate-200' };

  return (
    <SupplierAuthGuard onBackToCustomerSite={onBackToCustomerSite}>
    <div className="flex-1 flex flex-col bg-[#f8fafc]">
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
              <input type="checkbox" checked={contractChecked} onChange={(e) => setContractChecked(e.target.checked)} className="w-4 h-4 accent-brand-deep rounded mt-0.5" />
              <span className="text-[11px] text-slate-800 leading-relaxed font-bold">متن تازه قرارداد را خواندم و قبول دارم.</span>
            </label>
            <button disabled={!contractChecked} onClick={() => void acceptContract()} className={`w-full py-2.5 min-h-[44px] font-bold text-xs rounded-xl shadow-xs transition-all ${contractChecked ? 'bg-brand-deep hover:bg-brand-deep text-white' : 'bg-slate-200 text-slate-400 cursor-not-allowed'}`}>
              قبول دارم، ادامه بده
            </button>
          </div>
        </div>
      )}
      {/* Top Header */}
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200/80 py-2.5">
        <div className="max-w-7xl mx-auto w-full px-3.5 lg:px-6 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={onBackToCustomerSite}
            className="p-2.5 rounded-xl hover:bg-slate-100 text-slate-600 transition-colors"
            title="بازگشت به سایت اصلی"
            aria-label="بازگشت به سایت اصلی"
          >
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          {/* Below lg the header carries the identity; at lg the sidebar
              identity card is the single identity surface, so the header
              slot shows the active tab's label instead. */}
          <div className="lg:hidden">
            <h1 className="text-xs font-bold text-slate-800">
              {supplierSession?.companyName || 'پنل تأمین‌کننده'}
            </h1>
            <span className="text-[10px] text-slate-400 block">
              مدیر: {supplierSession?.managerName} • انبار: {warehouseCityConfirmed === false ? 'نامشخص' : supplierSession?.warehouseCity}
            </span>
          </div>
          <div className="hidden lg:block">
            <h1 className="text-[13px] font-extrabold text-slate-900">{activeTabLabel || 'پنل تأمین‌کننده'}</h1>
          </div>
        </div>

        <button
          onClick={() => {
            logoutSupplier();
            info('از حساب تأمین‌کننده خارج شدی.');
          }}
          className="flex items-center gap-1.5 min-h-[36px] px-2.5 rounded-xl bg-rose-50 hover:bg-rose-100 text-rose-600 text-[11px] font-bold transition-colors"
          title="خروج از حساب تأمین‌کننده"
        >
          <LogOut className="w-4 h-4"  strokeWidth={1.5}/>
          <span>خروج</span>
        </button>
        </div>
      </header>

      {/* Horizontal Tabs */}
      <div className="bg-white border-b border-slate-200/80 px-2 py-2 flex items-center gap-1.5 overflow-x-auto no-scrollbar lg:hidden">
        {allowedTabs.map((item) => {
          const Icon = item.icon;
          const isActive = visibleTab === item.id;
          return (
            <button
              key={item.id}
              onClick={() => setActiveTab(item.id)}
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-bold whitespace-nowrap transition-all shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/50 ${
                isActive
                  ? 'bg-brand text-white shadow-xs'
                  : 'bg-slate-50 text-slate-600 hover:bg-slate-100'
              }`}
            >
              <Icon className="w-3.5 h-3.5" />
              <span>{item.label}</span>
            </button>
          );
        })}
      </div>

      {/* Desktop workspace (sidebar + content), following the admin
          workspace geometry (max-w-7xl row, w-72 aside, white rounded-2xl
          cards) and the seller panel's brand accent. Below lg the wrapper
          is a plain flex column, the sidebar is hidden and the content
          keeps its exact mobile padding, so mobile is pixel-unchanged.
          Content width: tab content inside <main> is intentionally
          uncapped (unlike admin's max-w-3xl tab cap) because the
          supplier tabs are data-table-heavy and need the full row width. */}
      <div className="flex-1 w-full min-w-0 max-w-7xl mx-auto flex flex-col lg:flex-row lg:gap-6 lg:p-6">
        {/* Sidebar Navigation (desktop only; same permission-filtered tabs as the mobile bar) */}
        <aside className="hidden lg:block w-72 shrink-0 space-y-4 lg:sticky lg:top-[73px] lg:self-start lg:max-h-[calc(100vh-97px)] lg:overflow-y-auto">
          {/* Identity Card */}
          <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs flex items-center gap-2.5">
            <div className="w-9 h-9 rounded-xl bg-brand flex items-center justify-center text-white shrink-0">
              <Building2 className="w-4 h-4" strokeWidth={1.5} />
            </div>
            <div className="min-w-0">
              <span className="text-[10px] text-slate-400 block">پنل تأمین‌کننده</span>
              <span className="text-xs font-extrabold text-slate-900 block truncate" title={supplierSession?.companyName || 'تأمین‌کننده'}>
                {supplierSession?.companyName || 'تأمین‌کننده'}
              </span>
              <span className="text-[10px] text-slate-400 block truncate" title={`مدیر: ${supplierSession?.managerName ?? ''} • انبار: ${warehouseCityConfirmed === false ? 'نامشخص' : supplierSession?.warehouseCity ?? ''}`}>
                مدیر: {supplierSession?.managerName} • انبار: {warehouseCityConfirmed === false ? 'نامشخص' : supplierSession?.warehouseCity}
              </span>
              <span className={`inline-block mt-1.5 text-[10px] font-bold px-2 py-0.5 rounded-full border ${warehousePill.cls}`}>
                {warehousePill.text}
              </span>
            </div>
          </div>

          {/* Grouped Menu */}
          {sidebarGroups.length > 0 && (
            <nav className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs" aria-label="منوی پنل تأمین‌کننده">
              {sidebarGroups.map((group, groupIndex) => {
                const GroupIcon = group.icon;
                return (
                  <div key={group.heading} className={groupIndex > 0 ? 'mt-3 border-t border-slate-100 pt-2' : ''}>
                    <div className="flex items-center gap-2.5 px-2 py-1.5">
                      <GroupIcon className="w-4 h-4 shrink-0 text-slate-400" strokeWidth={1.5} />
                      <span className="flex-1 text-xs font-extrabold text-slate-900">{group.heading}</span>
                      <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 border border-slate-200">
                        {toPersianDigits(group.items.length)}
                      </span>
                    </div>
                    <div className="border-r-2 border-slate-100 pr-2 space-y-1">
                      {group.items.map((item) => {
                        const Icon = item.icon;
                        const isActive = visibleTab === item.id;
                        return (
                          <button
                            key={item.id}
                            onClick={() => setActiveTab(item.id)}
                            aria-current={isActive ? 'page' : undefined}
                            title={item.label}
                            className={`flex items-center gap-2.5 px-3 py-2 rounded-xl text-xs font-bold transition-all w-full text-right focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand/50 ${
                              isActive
                                ? 'bg-brand text-white shadow-xs'
                                : 'text-slate-600 hover:bg-slate-100'
                            }`}
                          >
                            <Icon className={`w-4 h-4 shrink-0 ${isActive ? 'text-white' : 'text-slate-400'}`} strokeWidth={1.5} />
                            <span className="flex-1 truncate">{item.label}</span>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                );
              })}
            </nav>
          )}
        </aside>

        {/* Main Tab Content */}
        <main className="flex-1 min-w-0">
          <div className="p-3.5 lg:p-0">
            {allowedTabs.length === 0 && <div role="alert" className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">مجوزهای حساب تأمین‌کننده دریافت نشد. لطفاً یک‌بار خارج شوید و دوباره وارد شوید.</div>}
            {visibleTab === 'overview' && <SupplierOverviewTab onNavigateTab={setActiveTab} />}
            {visibleTab === 'orders' && <SupplierOrdersTab />}
            {visibleTab === 'products' && <SupplierProductsTab />}
            {visibleTab === 'inventory' && <SupplierInventoryTab />}
            {visibleTab === 'inbound' && <SupplierInboundFlowTab />}
            {visibleTab === 'discrepancies' && <SupplierDiscrepanciesTab />}
            {visibleTab === 'ads' && <SupplierAdsCampaignTab />}
            {visibleTab === 'settlement' && <SupplierSettlementTab />}
            {visibleTab === 'team' && <SupplierTeamTab />}
            {visibleTab === 'analytics' && <SupplierAnalyticsTab />}
            {visibleTab === 'demand-insight' && <SupplierDemandInsightTab />}
            {visibleTab === 'customer-returns' && <SupplierCustomerReturnsTab />}
            {visibleTab === 'my-warehouses' && <SupplierMyWarehousesTab />}
            {visibleTab === 'shipping-methods' && <SupplierShippingMethodsTab />}
            {visibleTab === 'profile' && <SupplierProfileTab />}
            {visibleTab === 'contract' && <SupplierContractTab />}
          </div>
        </main>
      </div>
    </div>
    </SupplierAuthGuard>
  );
};
