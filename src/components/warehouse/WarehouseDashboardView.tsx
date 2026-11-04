import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { 
  ArrowRight, 
  LayoutDashboard, 
  Truck, 
  ShieldCheck, 
  Layers, 
  ScanLine, 
  Package, 
  Send, 
  RotateCcw,
  PackageCheck,
  Undo2,
  LogOut
} from 'lucide-react';

import { WarehouseAuthGuard } from './WarehouseAuthGuard';
import { WarehouseOverviewTab } from './WarehouseOverviewTab';
import { WarehouseInboundTab } from './WarehouseInboundTab';
import { WarehouseQCTab } from './WarehouseQCTab';
import { WarehousePutawayTab } from './WarehousePutawayTab';
import { WarehousePickingTab } from './WarehousePickingTab';
import { WarehousePackingTab } from './WarehousePackingTab';
import { WarehouseShippingTab } from './WarehouseShippingTab';
import { WarehouseReturnsTab } from './WarehouseReturnsTab';
import { WarehouseReceiveTab, WarehouseCustomerReturnsTab, WarehouseReturnBacksTab } from './WarehouseNewTabs';

interface Props {
  onBackToCustomerSite: () => void;
}

export const WarehouseDashboardView: React.FC<Props> = ({ onBackToCustomerSite }) => {
  const { warehouseSession, logoutWarehouse } = useStore();
  const [activeTab, setActiveTab] = useState('overview');
  const [selectedPackingOrderId, setSelectedPackingOrderId] = useState<string | undefined>();

  // FIX 17: live warehouse menu (WarehouseDashboardView-MERGED.js):
  // 11 live tabs in this exact order. 'receive', 'customer-returns'
  // and 'return-backs' were missing; 'returns' is relabelled to
  // live's meaning (supplier returns), not customer returns.
  const menuItems = [
    { id: 'overview', label: 'نمای کلی انبار', icon: LayoutDashboard },
    { id: 'receive', label: 'دریافت محموله (SKU)', icon: PackageCheck },
    { id: 'inbound', label: 'بارانداز ورودی', icon: Truck },
    { id: 'qc', label: 'کنترل کیفیت (QC)', icon: ShieldCheck },
    { id: 'putaway', label: 'جانمایی در قفسه', icon: Layers },
    { id: 'picking', label: 'جمع‌آوری (Pick)', icon: ScanLine },
    { id: 'packing', label: 'میز بسته‌بندی', icon: Package },
    { id: 'shipping', label: 'مانیفست خروج', icon: Send },
    { id: 'returns', label: 'مرجوعی‌های تأمین‌کنندگان', icon: RotateCcw },
    { id: 'customer-returns', label: 'دریافت مرجوعی (SKU)', icon: RotateCcw },
    { id: 'return-backs', label: 'درخواست‌های برگشت تأمین‌کننده', icon: Undo2 },
  ];

  return (
    <WarehouseAuthGuard onBackToCustomerSite={onBackToCustomerSite}>
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
            <h1 className="text-xs font-bold text-slate-800">سامانه مدیریت انبار مکانیزه مرکزی (WMS)</h1>
            <span className="text-[10px] text-slate-400 block">
              {warehouseSession ? `${warehouseSession.warehouseName} • ${warehouseSession.staffName}` : 'پذیرش، کنترل کیفیت، جمع‌آوری و ارسال'}
            </span>
          </div>
        </div>
        {warehouseSession && (
          <button
            onClick={logoutWarehouse}
            className="flex items-center gap-1 text-[11px] font-bold text-rose-600 hover:text-rose-700 bg-rose-50 hover:bg-rose-100 px-2.5 py-1 rounded-xl transition-colors border border-rose-200"
            title="خروج از شیفت انبار"
          >
            <LogOut className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>خروج شیفت</span>
          </button>
        )}
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
                  ? 'bg-amber-600 text-white shadow-xs'
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
        {activeTab === 'overview' && <WarehouseOverviewTab onNavigateTab={setActiveTab} />}
        {activeTab === 'inbound' && <WarehouseInboundTab onGoToQC={() => setActiveTab('qc')} />}
        {activeTab === 'qc' && <WarehouseQCTab onGoToPutaway={() => setActiveTab('putaway')} />}
        {activeTab === 'putaway' && <WarehousePutawayTab onGoToPicking={() => setActiveTab('picking')} />}
        {activeTab === 'picking' && (
          <WarehousePickingTab
            onGoToPacking={(orderNumber) => {
              setSelectedPackingOrderId(orderNumber);
              setActiveTab('packing');
            }}
          />
        )}
        {activeTab === 'packing' && (
          <WarehousePackingTab
            selectedOrderNumber={selectedPackingOrderId}
            onGoToShipping={() => setActiveTab('shipping')}
          />
        )}
        {activeTab === 'shipping' && <WarehouseShippingTab />}
        {activeTab === 'returns' && <WarehouseReturnsTab />}
        {activeTab === 'receive' && <WarehouseReceiveTab />}
        {activeTab === 'customer-returns' && <WarehouseCustomerReturnsTab />}
        {activeTab === 'return-backs' && <WarehouseReturnBacksTab />}
      </div>
    </div>
    </WarehouseAuthGuard>
  );
};
