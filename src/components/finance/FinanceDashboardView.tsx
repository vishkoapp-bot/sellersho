import React, { useState } from 'react';
import { 
  ArrowRight, 
  LayoutDashboard, 
  Receipt, 
  ArrowDownToLine, 
  FileEdit, 
  Headphones, 
  ShieldAlert 
} from 'lucide-react';

import { FinanceOverviewTab } from './FinanceOverviewTab';
import { FinanceReconciliationTab } from './FinanceReconciliationTab';
import { FinancePayoutsTab } from './FinancePayoutsTab';
import { FinanceAdjustmentsTab } from './FinanceAdjustmentsTab';
import { SupportTicketsTab } from './SupportTicketsTab';
import { SupportDisputesTab } from './SupportDisputesTab';

interface Props {
  onBackToCustomerSite: () => void;
}

export const FinanceDashboardView: React.FC<Props> = ({ onBackToCustomerSite }) => {
  const [activeTab, setActiveTab] = useState('overview');

  const menuItems = [
    { id: 'overview', label: 'نمای کلی مالی', icon: LayoutDashboard },
    { id: 'reconciliation', label: 'تطبیق درگاه شاپرک', icon: Receipt },
    { id: 'payouts', label: 'تسویه‌حساب شبا', icon: ArrowDownToLine },
    { id: 'adjustments', label: 'اسناد تعدیل و اصلاح', icon: FileEdit },
    { id: 'tickets', label: 'میز تیکت پشتیبانی', icon: Headphones },
    { id: 'disputes', label: 'داوری و مرجوعی', icon: ShieldAlert },
  ];

  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc]">
      {/* Top Header */}
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200/80 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={onBackToCustomerSite}
            className="p-1.5 rounded-xl hover:bg-slate-100 text-slate-600 transition-colors"
            title="بازگشت به سایت اصلی"
          >
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <div>
            <h1 className="text-xs font-bold text-slate-800">پنل امور مالی، تطبیق و پشتیبانی</h1>
            <span className="text-[10px] text-slate-400 block">دفتر اسناد، تراز درگاه، حواله‌های پایا و حل اختلاف</span>
          </div>
        </div>
      </header>

      {/* Horizontal Tabs */}
      <div className="bg-white border-b border-slate-200/80 px-2 py-2 flex items-center gap-1.5 overflow-x-auto no-scrollbar">
        {menuItems.map((item) => {
          const Icon = item.icon;
          const isActive = activeTab === item.id;
          return (
            <button
              key={item.id}
              onClick={() => setActiveTab(item.id)}
              className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-bold whitespace-nowrap transition-all shrink-0 ${
                isActive
                  ? 'bg-teal-700 text-white shadow-xs'
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
        {activeTab === 'overview' && <FinanceOverviewTab onNavigateTab={setActiveTab} />}
        {activeTab === 'reconciliation' && <FinanceReconciliationTab />}
        {activeTab === 'payouts' && <FinancePayoutsTab />}
        {activeTab === 'adjustments' && <FinanceAdjustmentsTab />}
        {activeTab === 'tickets' && <SupportTicketsTab />}
        {activeTab === 'disputes' && <SupportDisputesTab />}
      </div>
    </div>
  );
};
