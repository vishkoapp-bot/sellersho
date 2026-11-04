import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { WarehouseOverviewDto, wmsApi } from '../../services/wmsApi';
import { apiRequest } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
import { Warehouse, Package, CheckCircle2, Clock, ScanLine, Boxes } from 'lucide-react';

interface Props {
  onNavigateTab: (tab: string) => void;
}

export const WarehouseOverviewTab: React.FC<Props> = ({ onNavigateTab }) => {
  const { warehouseSession, toPersianDigits } = useStore();
  const { error } = useToast();
  const [overview, setOverview] = useState<WarehouseOverviewDto | null>(null);
  const [overviewLoading, setOverviewLoading] = useState(true);

  useEffect(() => {
    let active = true;
    wmsApi.getOverview()
      .then((value) => { if (active) setOverview(value); })
      .catch((cause) => { if (active) error(cause instanceof Error ? cause.message : 'نمای کلی انبار دریافت نشد.'); })
      .finally(() => { if (active) setOverviewLoading(false); });
    return () => { active = false; };
  }, [error]);

  const warehouseName = overview?.warehouseName || warehouseSession?.warehouseName || 'انبار مرکزی';
  const [pickCount, setPickCount] = useState(0);
  const [packCount, setPackCount] = useState(0);

  useEffect(() => {
    let active = true;
    const loadCount = async (queuePath: string, setCount: (n: number) => void) => {
      try {
        const data = await apiRequest<{ orders?: unknown[] } | unknown[]>(queuePath, { method: 'GET', auth: true });
        if (active) setCount(Array.isArray(data) ? data.length : (data.orders?.length || 0));
      } catch { /* keep the last count; the overview fetch surfaces failures */ }
    };
    void loadCount('/warehouse/orders/ready-to-pick', setPickCount);
    void loadCount('/warehouse/orders/ready-to-pack', setPackCount);
    return () => { active = false; };
  }, []);

  const readyToPick = pickCount;
  const packingQueue = packCount;
  const capacityUsedPercent = overview?.capacityPercent || 0;

  return (
    <div className="space-y-3 pb-6">
      {/* 1. Header Card */}
      <div className="bg-gradient-to-l from-slate-900 via-amber-950 to-slate-900 rounded-2xl p-3.5 text-white shadow-xs">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="w-9 h-9 rounded-xl bg-amber-500/20 border border-amber-400/30 flex items-center justify-center text-amber-400">
              <Warehouse className="w-5 h-5"  strokeWidth={1.5}/>
            </div>
            <div>
              <h2 className="text-xs font-bold flex items-center gap-1.5">
                <span>{warehouseName}</span>
                <span className="text-[9px] bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 px-1.5 py-0.2 rounded-full font-bold">
                  فعال
                </span>
              </h2>
              <span className="text-[10px] text-slate-400 block mt-0.5">
                حجم تأییدشده: {overviewLoading ? 'در حال دریافت...' : `${toPersianDigits(overview?.capacityVolumeCm3 || 0)} cm³`}
              </span>
            </div>
          </div>
        </div>

        <div className="mt-2.5 pt-2 border-t border-white/10 space-y-1">
          <div className="flex justify-between text-[10px]">
            <span className="text-slate-300">ظرفیت اشغال‌شده:</span>
            <span className="font-bold text-amber-300">{overviewLoading ? '...' : `٪${toPersianDigits(capacityUsedPercent)}`}</span>
          </div>
          <div className="w-full h-2 bg-black/40 rounded-full overflow-hidden">
            <div className="h-full bg-amber-500 rounded-full" style={{ width: `${capacityUsedPercent}%` }} />
          </div>
        </div>
      </div>

      {/* 2. Operations Metrics */}
      <div className="grid grid-cols-2 gap-2">
        <div className="bg-white rounded-xl p-3 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">آماده جمع‌آوری (Pick)</span>
            <ScanLine className="w-3.5 h-3.5 text-amber-600"  strokeWidth={1.5}/>
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {toPersianDigits(readyToPick)} <span className="text-[9px] font-normal text-slate-500">سفارش</span>
          </div>
          <span className="text-[9px] text-amber-600 font-bold block mt-1">تأییدیه درگاه صادر شد</span>
        </div>

        <div className="bg-white rounded-xl p-3 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">صف بسته‌بندی</span>
            <Boxes className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {toPersianDigits(packingQueue)} <span className="text-[9px] font-normal text-slate-500">بسته</span>
          </div>
          <span className="text-[9px] text-blue-600 font-medium block mt-1">منتظر چاپ بارنامه</span>
        </div>

        <div className="bg-white rounded-xl p-3 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">محموله در اسکله</span>
            <Clock className="w-3.5 h-3.5 text-indigo-600"  strokeWidth={1.5}/>
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {overviewLoading ? '...' : toPersianDigits(overview?.inboundPending || 0)} <span className="text-[9px] font-normal text-slate-500">محموله</span>
          </div>
          <span className="text-[9px] text-slate-400 block mt-1">آماده بازرسی QC</span>
        </div>

        <div className="bg-white rounded-xl p-3 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">دقت پردازش</span>
            <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600"  strokeWidth={1.5}/>
          </div>
          <div className="text-sm font-extrabold text-emerald-600">
            {overviewLoading ? '...' : toPersianDigits(overview?.verifiedMovements || 0)}
          </div>
          <span className="text-[9px] text-emerald-600 font-bold block mt-1">movement canonical تأییدشده</span>
        </div>
      </div>

      {/* 3. Action Buttons */}
      <div className="grid grid-cols-2 gap-2">
        <button
          onClick={() => onNavigateTab('picking')}
          className="p-2.5 bg-white border border-slate-200/90 rounded-xl text-right flex items-center gap-2 hover:bg-slate-50 shadow-2xs"
        >
          <ScanLine className="w-4 h-4 text-amber-600 shrink-0"  strokeWidth={1.5}/>
          <div>
            <span className="text-[11px] font-bold text-slate-800 block">شروع جمع‌آوری</span>
            <span className="text-[9px] text-slate-400 block">اسکن اقلام سفارش</span>
          </div>
        </button>

        <button
          onClick={() => onNavigateTab('packing')}
          className="p-2.5 bg-white border border-slate-200/90 rounded-xl text-right flex items-center gap-2 hover:bg-slate-50 shadow-2xs"
        >
          <Package className="w-4 h-4 text-blue-600 shrink-0"  strokeWidth={1.5}/>
          <div>
            <span className="text-[11px] font-bold text-slate-800 block">میز بسته‌بندی</span>
            <span className="text-[9px] text-slate-400 block">چاپ فاکتور و برچسب</span>
          </div>
        </button>
      </div>
    </div>
  );
};
