import React from 'react';
import { useStore } from '../context/StoreContext';
import { isMockMode } from '../services/apiClient';
import { 
  ShoppingBag, 
  Store, 
  Layers, 
  Building2, 
  Warehouse, 
  ShieldCheck, 
  BadgePercent 
} from 'lucide-react';

export const RoleSwitcherBar: React.FC = () => {
  const { activeView, setActiveView } = useStore();

  if (!isMockMode()) {
    return null;
  }

  const roles = [
    { id: 'customer', label: 'سایت مرکزی', icon: ShoppingBag },
    { id: 'seller_dashboard', label: 'پنل دراپ‌شیپر', icon: Store },
    { id: 'seller_storefront', label: 'ویترین سلر', icon: Layers },
    { id: 'supplier_dashboard', label: 'پنل تأمین‌کننده', icon: Building2 },
    { id: 'warehouse_panel', label: 'پنل انبار', icon: Warehouse },
    { id: 'admin_panel', label: 'پرتال مدیریت و مالی (/admin)', icon: ShieldCheck },
  ];

  return (
    <div className="bg-slate-900 text-white text-[11px] py-1.5 px-3 border-b border-slate-800 flex items-center justify-between select-none">
      <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar w-full">
        <span className="text-[10px] text-slate-400 font-bold shrink-0 ml-1">نقش پلتفرم:</span>
        {roles.map((r) => {
          const Icon = r.icon;
          const isActive = activeView === r.id;
          return (
            <button
              key={r.id}
              onClick={() => setActiveView(r.id as any)}
              className={`flex items-center gap-1 px-2.5 py-1 rounded-lg font-bold whitespace-nowrap transition-all shrink-0 ${
                isActive
                  ? 'bg-[#007dfa] text-white shadow-xs'
                  : 'bg-slate-800/80 hover:bg-slate-700 text-slate-300'
              }`}
            >
              <Icon className="w-3 h-3" />
              <span>{r.label}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
};
