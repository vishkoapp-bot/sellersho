import React, { useEffect, useState } from 'react';
import { AuthProvider, useAuth } from '../context/AuthContext';
import { ProductProvider } from '../context/ProductContext';
import { CartProvider } from '../context/CartContext';
import { OrderProvider } from '../context/OrderContext';
import { FinanceProvider } from '../context/FinanceContext';
import { WarehouseProvider } from '../context/WarehouseContext';
import { StoreProvider, useStoreManager } from '../context/StoreContext';
import { ToastProvider } from '../context/ToastContext';
import { SellerDashboardView } from './seller/SellerDashboardView';
import { SupplierDashboardView } from './supplier/SupplierDashboardView';
import { WarehouseDashboardView } from './warehouse/WarehouseDashboardView';
import { AdminDashboardView } from './admin/AdminDashboardView';
import { mockStores } from '../data/mockData';
import { isMockMode } from '../services/apiClient';
import { ArrowLeft, Building2, LayoutDashboard, ShieldCheck, Store, Warehouse } from 'lucide-react';

type PanelKey = 'overview' | 'dropshipper' | 'supplier' | 'warehouse' | 'admin';
type IconType = React.ComponentType<{ className?: string }>;
type Panel = { id: PanelKey; title: string; subtitle: string; icon: IconType; tone: string };
const panels: Panel[] = [
  { id: 'overview', title: 'مرکز عملیات', subtitle: 'نمای کلی همه پنل‌ها', icon: LayoutDashboard, tone: 'bg-blue-50 text-blue-700' },
  { id: 'dropshipper', title: 'دراپ‌شیپرها', subtitle: 'ویترین، سفارش و درآمد', icon: Store, tone: 'bg-emerald-50 text-emerald-700' },
  { id: 'supplier', title: 'تأمین‌کنندگان', subtitle: 'کالا، موجودی و تسویه', icon: Building2, tone: 'bg-amber-50 text-amber-700' },
  { id: 'warehouse', title: 'انبار و لجستیک', subtitle: 'ورودی، QC و ارسال', icon: Warehouse, tone: 'bg-violet-50 text-violet-700' },
  { id: 'admin', title: 'سوپر ادمین', subtitle: 'کنترل و سلامت پلتفرم', icon: ShieldCheck, tone: 'bg-rose-50 text-rose-700' },
];
const PreviewSessions: React.FC = () => {
  const { setAdminSession, setSellerSession, setWarehouseSession } = useAuth();
  const { setCurrentStore } = useStoreManager();
  useEffect(() => {
    if (!isMockMode()) return;
    setAdminSession({ token: 'preview-admin', username: 'preview-admin', fullName: 'مدیر پیش‌نمایش', role: 'super_admin', roleLabel: 'مدیر ارشد پلتفرم', loginAt: 'preview' });
    setSellerSession({ token: 'preview-seller', mobile: '09120000000', fullName: 'فروشنده نمایشی', status: 'active', storeId: 'store-bibi', loginAt: 'preview' });
    setWarehouseSession({ token: 'preview-warehouse', staffId: 'preview-staff', staffName: 'سرپرست شیفت نمایشی', role: 'supervisor', loginAt: 'preview' });
    const previewStore = mockStores.find((store) => store.id === 'store-bibi') || mockStores[0];
    setCurrentStore({ ...previewStore, activePlanId: 'vip', signedContractVersion: 2 });
  }, [setAdminSession, setSellerSession, setWarehouseSession, setCurrentStore]);
  return null;
};

const RealPanel: React.FC<{ panel: Exclude<PanelKey, 'overview'> }> = ({ panel }) => {
  if (panel === 'dropshipper') return <SellerDashboardView onBackToCustomerSite={() => {}} onOpenStorefront={() => {}} />;
  if (panel === 'supplier') return <SupplierDashboardView onBackToCustomerSite={() => {}} />;
  if (panel === 'warehouse') return <WarehouseDashboardView onBackToCustomerSite={() => {}} />;
  return <AdminDashboardView onBackToCustomerSite={() => {}} />;
};

const PreviewApp: React.FC = () => {
  const { currentStore } = useStoreManager();
  const [activePanel, setActivePanel] = useState<PanelKey>('dropshipper');
  return (
    <div dir="rtl" className="flex h-dvh flex-col bg-[#f8fafc] text-slate-900">
      <nav aria-label="انتخاب پنل" className="shrink-0 border-b border-slate-700 bg-[#0d1b2a] px-3 py-2 text-white">
        <div className="flex items-center gap-2 overflow-x-auto no-scrollbar">
          {panels.filter((panel) => panel.id !== 'overview').map((panel) => {
            const Icon = panel.icon;
            return (
              <button
                key={panel.id}
                type="button"
                aria-pressed={activePanel === panel.id}
                onClick={() => setActivePanel(panel.id)}
                className={`flex shrink-0 items-center gap-2 rounded-lg px-3 py-2 text-xs font-bold transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white ${activePanel === panel.id ? 'bg-[#4ecdc4] text-[#0d1b2a]' : 'text-slate-300 hover:bg-white/10 hover:text-white'}`}
              >
                <Icon className="h-4 w-4" />
                <span>{panel.title}</span>
              </button>
            );
          })}
          <a href="/" className="mr-auto flex shrink-0 items-center gap-2 px-3 py-2 text-xs text-slate-300 hover:text-white">
            <ArrowLeft className="h-4 w-4"  strokeWidth={1.5}/> سایت اصلی
          </a>
        </div>
      </nav>
      <main className="min-h-0 w-full flex-1 overflow-y-auto">
        {activePanel !== 'overview' && <RealPanel key={activePanel} panel={activePanel} />}
      </main>
    </div>
  );
};

export const PanelsPreview: React.FC = () => (
  <AuthProvider><ProductProvider><CartProvider><OrderProvider><FinanceProvider><WarehouseProvider><StoreProvider allowUnselectedChildren><ToastProvider><PreviewSessions /><PreviewApp /></ToastProvider></StoreProvider></WarehouseProvider></FinanceProvider></OrderProvider></CartProvider></ProductProvider></AuthProvider>
);

