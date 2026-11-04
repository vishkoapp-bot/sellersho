import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { wmsApi, WarehouseBinDto } from '../../services/wmsApi';
import { Warehouse, Plus, Settings2, CheckCircle2, ShieldCheck, TrendingUp, Boxes } from 'lucide-react';

export const AdminWarehousesTab: React.FC = () => {
  const { platformSettings, updateWarehouseTariffs, formatPrice, toPersianDigits } = useStore();
  const { success } = useToast();
  const [inboundFeePerCarton, setInboundFeePerCarton] = useState(
    (platformSettings.warehouseTariffs?.inboundFeePerCarton || 15000).toString()
  );
  const [storageFeePerCubicDay, setStorageFeePerCubicDay] = useState(
    (platformSettings.warehouseTariffs?.storageFeePerCubicDay || 8000).toString()
  );
  const [pickingPackingFee, setPickingPackingFee] = useState(
    (platformSettings.warehouseTariffs?.pickingPackingFee !== undefined ? platformSettings.warehouseTariffs.pickingPackingFee : 0).toString()
  );
  const [depotMultiplier, setDepotMultiplier] = useState(
    (platformSettings.warehouseTariffs?.depotBufferMultiplier || 1.5).toString()
  );
  const [smallLightQuota, setSmallLightQuota] = useState(
    (platformSettings.warehouseTariffs?.depotBaseQuotaByCategory?.small_light || 100).toString()
  );
  const [mediumQuota, setMediumQuota] = useState(
    (platformSettings.warehouseTariffs?.depotBaseQuotaByCategory?.medium || 50).toString()
  );
  const [bulkyQuota, setBulkyQuota] = useState(
    (platformSettings.warehouseTariffs?.depotBaseQuotaByCategory?.bulky_large || 20).toString()
  );
  const [saved, setSaved] = useState(false);
  const [bins, setBins] = useState<WarehouseBinDto[]>([]);
  const [capacityDrafts, setCapacityDrafts] = useState<Record<string, { volume: string; weight: string; verify: boolean }>>({});
  const [capacityBusy, setCapacityBusy] = useState<string | null>(null);
  React.useEffect(() => { wmsApi.getBins().then(setBins).catch(() => undefined); }, []);
  const saveBinCapacity = async (bin: WarehouseBinDto) => {
    const draft = capacityDrafts[bin.binCode] || { volume: String(bin.capacityVolumeCm3 || ''), weight: String(bin.maxWeightGrams || ''), verify: bin.capacityStatus === 'verified' };
    setCapacityBusy(bin.binCode);
    try { await wmsApi.updateBinCapacity(bin.binCode, { capacityVolumeCm3: Number(draft.volume), maxWeightGrams: Number(draft.weight) || undefined, verify: draft.verify }); setBins(await wmsApi.getBins()); success(`ظرفیت ${bin.binCode} ذخیره شد.`); }
    catch (cause) { success(cause instanceof Error ? cause.message : 'ذخیره ظرفیت انجام نشد.'); }
    finally { setCapacityBusy(null); }
  };

  const warehouses = [
    {
      id: 'WH-TEH-01',
      name: 'انبار مرکزی تهران (مجتمع لجستیک غرب)',
      city: 'تهران',
      capacityM3: 8500,
      occupiedM3: 6290,
      activeSuppliers: 18,
      status: 'active',
    },
    {
      id: 'WH-MHD-02',
      name: 'مرکز پخش و هاب منطقه‌ای شرق (مشهد)',
      city: 'مشهد',
      capacityM3: 3200,
      occupiedM3: 1450,
      activeSuppliers: 5,
      status: 'active',
    },
  ];

  const handleSaveTariff = (e: React.FormEvent) => {
    e.preventDefault();
    updateWarehouseTariffs({
      inboundFeePerCarton: Number(inboundFeePerCarton),
      storageFeePerCubicDay: Number(storageFeePerCubicDay),
      pickingPackingFee: Number(pickingPackingFee),
      depotBufferMultiplier: Number(depotMultiplier),
      depotBaseQuotaByCategory: {
        small_light: Number(smallLightQuota) || 100,
        medium: Number(mediumQuota) || 50,
        bulky_large: Number(bulkyQuota) || 20,
        digital: Number(smallLightQuota) || 100,
        beauty: Number(smallLightQuota) || 100,
        gold: Number(smallLightQuota) || 100,
        fashion: Number(mediumQuota) || 50,
        culture: Number(mediumQuota) || 50,
        travel: Number(mediumQuota) || 50,
        supermarket: Number(mediumQuota) || 50,
        appliances: Number(bulkyQuota) || 20,
        car: Number(bulkyQuota) || 20,
      },
    });
    setSaved(true);
    success('تعرفه‌ها و سهمیه‌های پایه دپوی انبار بر اساس نوع کالا ذخیره شد.');
    setTimeout(() => setSaved(false), 2000);
  };

  return (
    <div className="space-y-3.5 pb-6">
      {/* 1. Warehouses List */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-slate-800">مراکز پردازش و انبارهای متمرکز</h3>
          <span className="text-[10px] text-blue-600 bg-blue-50 px-2 py-0.5 rounded-full font-bold">
            {toPersianDigits(warehouses.length)} مرکز فعال
          </span>
        </div>

        <div className="space-y-2">
          {warehouses.map((wh) => (
            <div key={wh.id} className="p-3 bg-slate-50 rounded-xl border border-slate-200/80 space-y-1.5">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold text-slate-900">{wh.name}</span>
                <span className="text-[9px] bg-emerald-100 text-emerald-800 font-bold px-1.5 py-0.2 rounded">
                  عملیاتی
                </span>
              </div>
              <div className="flex items-center justify-between text-[10px] text-slate-500">
                <span>شهر: {wh.city}</span>
                <span>تأمین‌کنندگان دپوشده: {toPersianDigits(wh.activeSuppliers)} شرکت</span>
              </div>
              <div className="w-full h-1.5 bg-slate-200 rounded-full overflow-hidden mt-1">
                <div
                  className="h-full bg-amber-500 rounded-full"
                  style={{ width: `${Math.round((wh.occupiedM3 / wh.capacityM3) * 100)}%` }}
                />
              </div>
            </div>
          ))}
        </div>
      </div>
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between"><h3 className="text-xs font-bold text-slate-800">ظرفیت واقعی جایگاه‌ها</h3><span className="text-[10px] text-slate-500">فقط ظرفیت تأییدشده در putaway قابل استفاده است</span></div>
        {bins.map((bin) => { const draft = capacityDrafts[bin.binCode] || { volume: String(bin.capacityVolumeCm3 || ''), weight: String(bin.maxWeightGrams || ''), verify: bin.capacityStatus === 'verified' }; return <div key={bin.binCode} className="space-y-2 rounded-xl border border-slate-200 p-2.5 text-[10px]"><div className="flex justify-between font-bold"><span>{bin.binCode} · {bin.hallName}</span><span className={bin.capacityStatus === 'verified' ? 'text-emerald-700' : 'text-amber-700'}>{bin.capacityStatus === 'verified' ? 'تأییدشده' : 'نیازمند بازرسی'}</span></div><div className="grid grid-cols-4 gap-1 text-slate-500"><span>کل: {bin.capacityVolumeCm3 || 0} cm³</span><span>اشغال: {bin.occupiedVolumeCm3 || 0}</span><span>رزرو: {bin.reservedVolumeCm3 || 0}</span><span>باقی: {bin.remainingVolumeCm3 || 0}</span></div><div className="grid grid-cols-3 gap-2"><input aria-label={`ظرفیت ${bin.binCode}`} value={draft.volume} onChange={(event)=>setCapacityDrafts((current)=>({...current,[bin.binCode]:{...draft,volume:event.target.value}}))} className="rounded-lg border p-2" placeholder="ظرفیت cm³"/><input aria-label={`وزن ${bin.binCode}`} value={draft.weight} onChange={(event)=>setCapacityDrafts((current)=>({...current,[bin.binCode]:{...draft,weight:event.target.value}}))} className="rounded-lg border p-2" placeholder="حد وزن g"/><button disabled={capacityBusy === bin.binCode} onClick={()=>void saveBinCapacity(bin)} className="rounded-lg bg-indigo-600 p-2 font-bold text-white disabled:opacity-50">{draft.verify ? 'ذخیره verified' : 'تأیید فیزیکی'}</button></div>{bin.unavailableReason && <p className="text-rose-700">{bin.unavailableReason}</p>}</div>; })}</div>

      {/* 2. Service Tariffs (Section 11) */}
      <form onSubmit={handleSaveTariff} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-1.5 text-blue-600">
          <Settings2 className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">تعرفه‌های مصوب خدمات انبارداری و پردازش</h3>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          تعرفه‌های خدمات پذیرش، انبارداری و بسته‌بندی به‌صورت ماهانه از مطالبات تأمین‌کننده در کاردکس مالی کسر می‌گردد.
        </p>

        {/* Free First Month Banner (Requirement 19) */}
        <div className="p-2.5 bg-emerald-50 border border-emerald-200 rounded-xl text-[10px] text-emerald-800 leading-relaxed font-bold">
          ✓ نگهداری ماه اول رایگان: هزینه روزانه نگهداری در قفسه برای ۳۰ روز اول ورود هر محموله برابر با ۰ تومان محاسبه می‌شود.
        </div>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">تعرفه تخلیه و پذیرش هر کارتن مادر (تومان)</label>
          <input
            type="number"
            value={inboundFeePerCarton}
            onChange={(e) => setInboundFeePerCarton(e.target.value)}
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
            required
          />
        </div>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">هزینه روزانه نگهداری در قفسه (هر مترمکعب بعد از ۳۰ روز - تومان)</label>
          <input
            type="number"
            value={storageFeePerCubicDay}
            onChange={(e) => setStorageFeePerCubicDay(e.target.value)}
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
            required
          />
        </div>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">هزینه جمع‌آوری و کارتن‌بندی مکانیزه مرسوله (تومان - فعلاً ۰)</label>
          <input
            type="number"
            value={pickingPackingFee}
            onChange={(e) => setPickingPackingFee(e.target.value)}
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
            required
          />
        </div>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">ضریب اطمینان سهمیه دپوی تأمین‌کنندگان (برابر میانگین فروش ۳ ماهه)</label>
          <input
            type="number"
            step="0.1"
            value={depotMultiplier}
            onChange={(e) => setDepotMultiplier(e.target.value)}
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden font-mono"
            required
          />
        </div>

        {/* Item 3: Category-based Baseline Quota Floor Mapping */}
        <div className="pt-2 border-t border-slate-200/80 space-y-2">
          <label className="text-[11px] font-bold text-slate-800 block">
            کف سقف دپوی اولیه تأمین‌کنندگان تازه‌وارد (بدون سابقه فروش) بر اساس رسته کالا:
          </label>
          <div className="grid grid-cols-3 gap-2 text-xs">
            <div>
              <span className="text-[10px] text-slate-500 block mb-1">کالای سبک / دیجیتال:</span>
              <input
                type="number"
                value={smallLightQuota}
                onChange={(e) => setSmallLightQuota(e.target.value)}
                className="w-full p-2 bg-slate-50 border border-slate-200 rounded-xl font-bold font-mono text-center"
              />
            </div>
            <div>
              <span className="text-[10px] text-slate-500 block mb-1">کالای متوسط / پوشاک:</span>
              <input
                type="number"
                value={mediumQuota}
                onChange={(e) => setMediumQuota(e.target.value)}
                className="w-full p-2 bg-slate-50 border border-slate-200 rounded-xl font-bold font-mono text-center"
              />
            </div>
            <div>
              <span className="text-[10px] text-slate-500 block mb-1">کالای سنگین / لوازم خانگی:</span>
              <input
                type="number"
                value={bulkyQuota}
                onChange={(e) => setBulkyQuota(e.target.value)}
                className="w-full p-2 bg-slate-50 border border-slate-200 rounded-xl font-bold font-mono text-center"
              />
            </div>
          </div>
          <span className="text-[9px] text-slate-400 block">
            * تأمین‌کنندگان بدون سابقه فروش به جای ۱۰۰ واحد تخت، سهمیه متناسب با حجم کالا دریافت می‌کنند.
          </span>
        </div>

        <button
          type="submit"
          className="w-full py-2 bg-blue-600 hover:bg-blue-700 text-white font-bold text-xs rounded-xl shadow-xs transition-colors"
        >
          {saved ? 'تعرفه‌های جدید در سیستم ثبت شد!' : 'به‌روزرسانی تعرفه خدمات انبار'}
        </button>
      </form>

      {/* Requirement 17 & Item 3: Dynamic Depot Cap per Supplier */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-indigo-600">
            <Boxes className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">سقف دپوی مجاز تأمین‌کنندگان (پویا از فروش ۳ ماهه و نوع کالا)</h3>
          </div>
          <span className="text-[10px] text-indigo-600 bg-indigo-50 font-bold px-2 py-0.5 rounded-full">
            فرمول: میانگین فروش × {toPersianDigits(depotMultiplier)} (کف بر اساس رسته)
          </span>
        </div>

        <div className="space-y-2">
          {[
            { id: 'sup-technolife', name: 'تکنولایف', category: 'digital', catLabel: 'کالای سبک / دیجیتال', avgMonthlySales: 480, currentStock: 620 },
            { id: 'sup-mashhad-leather', name: 'صنایع چرم مشهد', category: 'fashion', catLabel: 'کالای متوسط / پوشاک', avgMonthlySales: 210, currentStock: 190 },
            { id: 'sup-khanoumi', name: 'خانومی', category: 'beauty', catLabel: 'کالای سبک / زیبایی', avgMonthlySales: 350, currentStock: 410 },
            { id: 'sup-snowa', name: 'لوازم خانگی اسنوا (تازه‌وارد)', category: 'appliances', catLabel: 'کالای حجیم / لوازم خانگی بزرگ', avgMonthlySales: 0, currentStock: 12 },
            { id: 'sup-gaj', name: 'انتشارات بین‌المللی گاج (تازه‌وارد)', category: 'culture', catLabel: 'کالای متوسط / کتاب و فرهنگ', avgMonthlySales: 0, currentStock: 35 },
          ].map((sup) => {
            const baseFloor = sup.category === 'appliances' ? Number(bulkyQuota) : sup.category === 'culture' || sup.category === 'fashion' ? Number(mediumQuota) : Number(smallLightQuota);
            const allowedCap = sup.avgMonthlySales > 0 ? Math.round(sup.avgMonthlySales * Number(depotMultiplier)) : baseFloor;
            const usagePercent = Math.min(100, Math.round((sup.currentStock / allowedCap) * 100));
            return (
              <div key={sup.id} className="p-2.5 bg-slate-50 border border-slate-200/80 rounded-xl space-y-1 text-xs">
                <div className="flex items-center justify-between font-bold">
                  <div>
                    <span className="text-slate-900">{sup.name}</span>
                    <span className="text-[10px] text-slate-400 block font-normal">{sup.catLabel}</span>
                  </div>
                  <span className="text-slate-600 font-mono text-[11px]">
                    سقف مجاز: {toPersianDigits(allowedCap)} واحد {sup.avgMonthlySales === 0 ? '(کف رسته)' : ''} (موجودی: {toPersianDigits(sup.currentStock)})
                  </span>
                </div>
                <div className="w-full h-1.5 bg-slate-200 rounded-full overflow-hidden mt-1">
                  <div
                    className={`h-full rounded-full ${usagePercent > 85 ? 'bg-amber-500' : 'bg-blue-600'}`}
                    style={{ width: `${usagePercent}%` }}
                  />
                </div>
              </div>
            );
          })}
        </div>
      </div>
      {/* Requirement 7: Packaging & Shipping Tariffs by Weight & Distance */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-indigo-600">
            <Plus className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">تعرفه‌های ماتریسی بسته‌بندی و ارسال (وزن / مسافت)</h3>
          </div>
          <span className="text-[9px] bg-indigo-50 text-indigo-700 font-bold px-2 py-0.5 rounded-full border border-indigo-200">
            محور وزن و مقصد
          </span>
        </div>

        <p className="text-[10px] text-slate-500 leading-relaxed">
          تعرفه‌های زیر هزینه نهایی کارتن، پلمب و بارنامه پستی را متناسب با وزن مرسوله و مقصد (درون‌استانی / برون‌استانی) محاسبه کرده و در تسویه‌حساب سفارش‌ها منظور می‌نمایند.
        </p>

        <div className="space-y-2">
          {(platformSettings.packagingTariffs || []).map((tariff) => (
            <div
              key={tariff.id}
              className="p-3 bg-slate-50 border border-slate-200/80 rounded-xl space-y-1.5 text-xs"
            >
              <div className="flex items-center justify-between">
                <span className="font-bold text-slate-800">{tariff.title}</span>
                <span className="font-extrabold text-blue-700 font-mono text-xs">
                  {formatPrice(tariff.cost)} تومان
                </span>
              </div>
              <div className="flex items-center justify-between text-[10px] text-slate-500">
                <span>
                  بازه وزن: {toPersianDigits(tariff.minWeightGrams)} تا {toPersianDigits(tariff.maxWeightGrams)} گرم
                </span>
                <span className="bg-white px-2 py-0.5 rounded border border-slate-200 font-bold">
                  {tariff.destinationType === 'intra_province' ? 'درون‌استانی (تهران)' : 'برون‌استانی (سایر استان‌ها)'}
                </span>
              </div>
              <div className="text-[9px] text-slate-400">
                ابعاد پیشنهادی: {tariff.boxSize}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
