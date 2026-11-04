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
  FileText
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
    { id: 'inventory', label: 'موجودی کالاها', icon: Warehouse },
    { id: 'my-warehouses', label: 'انبارهای من', icon: Warehouse },
    { id: 'inbound', label: 'فرستادن بار به انبار', icon: PackagePlus },
    { id: 'shipping-methods', label: 'روش‌های ارسال', icon: Truck },
    { id: 'discrepancies', label: 'اختلاف‌های تحویل', icon: AlertTriangle },
    { id: 'settlement', label: 'تسویه‌حساب', icon: Receipt },
    { id: 'ads', label: 'تبلیغات', icon: Megaphone },
    { id: 'team', label: 'همکاران من', icon: PackagePlus },
    { id: 'profile', label: 'پروفایل', icon: User },
    { id: 'contract', label: 'قرارداد من', icon: FileText },
  ];

  const tabPermissions: Record<string, string> = { team: 'supplier.manage', products: 'products.manage', inventory: 'inventory.manage', inbound: 'inventory.manage', discrepancies: 'inventory.manage', orders: 'orders.manage', ads: 'marketing.manage', settlement: 'finance.manage', overview: 'supplier.manage', analytics: 'supplier.manage', 'demand-insight': 'supplier.manage', 'customer-returns': 'orders.manage', 'my-warehouses': 'inventory.manage', 'shipping-methods': 'orders.manage', profile: 'supplier.manage', contract: 'supplier.manage' };
  const permissions = supplierSession?.permissions || [];
  const allowedTabs = menuItems.filter((item) => permissions.includes('supplier.manage') || permissions.includes(tabPermissions[item.id]));
  const visibleTab = allowedTabs.some((item) => item.id === activeTab) ? activeTab : allowedTabs[0]?.id;

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
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200/80 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={onBackToCustomerSite}
            className="p-1.5 rounded-xl hover:bg-slate-100 text-slate-600 transition-colors"
            title="بازگشت به سایت اصلی"
            aria-label="بازگشت به سایت اصلی"
          >
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <div>
            <h1 className="text-xs font-bold text-slate-800">
              {supplierSession?.companyName || 'پنل تأمین‌کننده'}
            </h1>
            <span className="text-[10px] text-slate-400 block">
              مدیر: {supplierSession?.managerName} • انبار: {warehouseCityConfirmed === false ? 'نامشخص' : supplierSession?.warehouseCity}
            </span>
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
      </header>

      {/* Horizontal Tabs */}
      <div className="bg-white border-b border-slate-200/80 px-2 py-2 flex items-center gap-1.5 overflow-x-auto no-scrollbar">
        {allowedTabs.map((item) => {
          const Icon = item.icon;
          const isActive = visibleTab === item.id;
          return (
            <button
              key={item.id}
              onClick={() => setActiveTab(item.id)}
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-bold whitespace-nowrap transition-all shrink-0 ${
                isActive
                  ? 'bg-indigo-600 text-white shadow-xs'
                  : 'bg-slate-50 text-slate-600 hover:bg-slate-100'
              }`}
            >
              <Icon className="w-3.5 h-3.5" />
              <span>{item.label}</span>
            </button>
          );
        })}
      </div>

      {/* Main Tab Content */}
      <div className="p-3.5 flex-1">
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
    </div>
    </SupplierAuthGuard>
  );
};
