import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { wmsApi, WarehouseBinDto } from '../../services/wmsApi';
import { adminApi, TapinBoxAdminDto } from '../../services/adminApi';
import { isMockMode } from '../../services/apiClient';
import { normalizePersianText } from '../../utils/routing';
import { Warehouse, Plus, Settings2, CheckCircle2, ShieldCheck, TrendingUp, Boxes } from 'lucide-react';

type CapacityDraft = { volume: string; weight: string; verify: boolean };

function initialCapacity(bin: WarehouseBinDto): CapacityDraft {
  return {
    volume: String(bin.capacityVolumeCm3 || ''),
    weight: String(bin.maxWeightGrams || ''),
    verify: bin.capacityStatus === 'verified',
  };
}

const capacityInput = 'min-h-11 w-full min-w-0 rounded-lg border border-slate-300 bg-white px-2 text-base focus-visible:outline-2 focus-visible:outline-indigo-700';

export const AdminWarehousesTab: React.FC = () => {
  const { platformSettings, updateWarehouseTariffs, formatPrice, toPersianDigits } = useStore();
  const { success } = useToast();
  const mock = isMockMode();
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
  const [capacityDrafts, setCapacityDrafts] = useState<Record<string, CapacityDraft>>({});
  const [capacityBusy, setCapacityBusy] = useState<string | null>(null);
  const [binsLoading, setBinsLoading] = useState(true);
  const [binsError, setBinsError] = useState('');
  const [binsAttempt, setBinsAttempt] = useState(0);
  const [capacityFeedback, setCapacityFeedback] = useState<{ binCode: string; kind: 'error' | 'success'; text: string }>();

  useEffect(() => {
    let mounted = true;
    void wmsApi.getBins()
      .then(result => { if (mounted) setBins(result); })
      .catch((cause: unknown) => {
        if (mounted) setBinsError(cause instanceof Error ? cause.message : 'دریافت ظرفیت جایگاه‌ها انجام نشد.');
      })
      .finally(() => { if (mounted) setBinsLoading(false); });
    return () => { mounted = false; };
  }, [binsAttempt]);

  const reloadBins = () => {
    setBinsLoading(true);
    setBinsError('');
    setBinsAttempt(value => value + 1);
  };

  const saveBinCapacity = async (bin: WarehouseBinDto, draft: CapacityDraft) => {
    if (capacityBusy || binsLoading) return;
    const { binCode } = bin;
    setCapacityFeedback(undefined);
    setCapacityBusy(binCode);
    try {
      await wmsApi.updateBinCapacity(binCode, {
        capacityVolumeCm3: Number(draft.volume),
        maxWeightGrams: Number(draft.weight) || undefined,
        verify: draft.verify,
      });
      setCapacityDrafts(current => {
        const next = { ...current };
        delete next[binCode];
        return next;
      });
      setCapacityFeedback({ binCode, kind: 'success', text: 'ظرفیت جایگاه ذخیره شد.' });
      reloadBins();
    } catch (cause: unknown) {
      setCapacityFeedback({ binCode, kind: 'error', text: cause instanceof Error ? cause.message : 'ذخیره ظرفیت انجام نشد.' });
    } finally {
      setCapacityBusy(null);
    }
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
      {mock && <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
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
      </div>}
      <section aria-labelledby="warehouse-capacity-heading" className="space-y-3 rounded-2xl border border-slate-200 bg-white p-3.5">
        <h3 id="warehouse-capacity-heading" className="text-sm font-bold text-slate-900">ظرفیت واقعی جایگاه‌ها</h3>
        <p className="text-sm leading-6 text-slate-600">برای جانمایی کالا فقط از ظرفیت جایگاه‌های تأییدشده استفاده می‌شود.</p>
        {binsLoading && <p role="status" className="text-sm text-slate-600">در حال دریافت جایگاه‌ها…</p>}
        {binsError && (
          <div className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">
            <p role="alert" className="[overflow-wrap:anywhere]">{binsError}</p>
            <button type="button" onClick={reloadBins} className="mt-1 min-h-11 rounded-lg px-2 font-bold focus-visible:outline-2 focus-visible:outline-rose-700">تلاش دوباره</button>
          </div>
        )}
        {!binsLoading && !binsError && bins.length === 0 && <p className="text-sm text-slate-600">جایگاهی برای نمایش وجود ندارد.</p>}
        {!binsError && bins.map(bin => {
          const { binCode } = bin;
          const draft = capacityDrafts[binCode] ?? initialCapacity(bin);
          const patch = (change: Partial<CapacityDraft>) =>
            setCapacityDrafts(current => ({ ...current, [binCode]: { ...(current[binCode] ?? initialCapacity(bin)), ...change } }));
          const metrics = [
            ['کل', bin.capacityVolumeCm3],
            ['اشغال', bin.occupiedVolumeCm3],
            ['رزرو', bin.reservedVolumeCm3],
            ['باقی', bin.remainingVolumeCm3],
          ] as const;
          return (
            <form key={binCode} onSubmit={event => { event.preventDefault(); void saveBinCapacity(bin, draft); }}
              className="space-y-3 rounded-xl border border-slate-200 p-3 text-sm [overflow-wrap:anywhere]">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h4 className="font-bold"><bdi>{binCode}</bdi> · {bin.hallName}</h4>
                <span className={bin.capacityStatus === 'verified' ? 'text-emerald-700' : 'text-amber-800'}>
                  {bin.capacityStatus === 'verified' ? 'تأییدشده' : 'نیازمند بازرسی'}
                </span>
              </div>
              <dl aria-label="حجم، سانتی‌متر مکعب" className="grid grid-cols-2 gap-2 rounded-lg bg-slate-50 p-2 text-slate-600 sm:grid-cols-4">
                {metrics.map(([label, value]) => (
                  <div key={label}><dt>{label}</dt><dd className="font-bold text-slate-900">{toPersianDigits(value ?? 0)}</dd></div>
                ))}
              </dl>
              <fieldset disabled={!!capacityBusy || binsLoading} className="grid min-w-0 gap-2 sm:grid-cols-2">
                <label className="space-y-1">
                  <span className="block">ظرفیت (سانتی‌متر مکعب)</span>
                  <input aria-label={`ظرفیت ${binCode}`} dir="ltr" type="number" inputMode="numeric" required min="1"
                    value={draft.volume} onChange={event => patch({ volume: event.target.value })} className={capacityInput} />
                </label>
                <label className="space-y-1">
                  <span className="block">حداکثر وزن (گرم)</span>
                  <input aria-label={`وزن ${binCode}`} dir="ltr" type="number" inputMode="numeric" min="0"
                    value={draft.weight} onChange={event => patch({ weight: event.target.value })} className={capacityInput} />
                </label>
                <label className="flex min-h-11 items-center gap-2 sm:col-span-2">
                  <input type="checkbox" checked={draft.verify} onChange={event => patch({ verify: event.target.checked })} />
                  تأیید ظرفیت پس از بازرسی فیزیکی
                </label>
                <button type="submit" className="min-h-11 rounded-lg bg-indigo-700 px-3 font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-700 disabled:opacity-50 sm:col-span-2 sm:justify-self-start">
                  {capacityBusy === binCode ? 'در حال ذخیره…' : 'ذخیره ظرفیت'}
                </button>
              </fieldset>
              {bin.unavailableReason && <p className="text-rose-800">{bin.unavailableReason}</p>}
              {capacityFeedback?.binCode === binCode && (
                <p role={capacityFeedback.kind === 'error' ? 'alert' : 'status'} className={capacityFeedback.kind === 'error' ? 'text-rose-800' : 'text-emerald-800'}>{capacityFeedback.text}</p>
              )}
            </form>
          );
        })}
      </section>

      {mock ? <>
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

      </> : <p className="rounded-xl border border-slate-200 bg-white p-3 text-sm leading-6 text-slate-700">
        تغییر تعرفه‌های پذیرش، بسته‌بندی و سهمیه دپو در این بخش در دسترس نیست.
      </p>}
      {/* Tapin box catalogue (gaps 1-3) */}
      <AdminTapinBoxesCard />
    </div>
  );
};

// AdminTapinBoxesCard edits the platform Tapin box catalogue
// (tapin_boxes). Tapin's API exposes no boxes endpoint, so this
// catalogue is the platform's own: suppliers pick from it when
// registering a Tapin shipment, and the quote flow auto-picks from
// it. The platform default box id itself lives in the Tapin platform
// settings (tapin_default_box_id) and is shown here read-only.
const AdminTapinBoxesCard: React.FC = () => {
  const { toPersianDigits } = useStore();
  const [boxes, setBoxes] = useState<TapinBoxAdminDto[]>([]);
  const [defaultBoxId, setDefaultBoxId] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [saveError, setSaveError] = useState('');
  const [saved, setSaved] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let mounted = true;
    void adminApi.getTapinBoxes()
      .then(res => {
        if (!mounted) return;
        if (res?.success !== true || !Array.isArray(res.boxes)) {
          throw new Error('پاسخ کاتالوگ جعبه‌های تاپین کامل دریافت نشد.');
        }
        setBoxes(res.boxes);
        setDefaultBoxId(res.defaultBoxId);
      })
      .catch((cause: unknown) => {
        if (mounted) setLoadError(cause instanceof Error ? cause.message : 'دریافت کاتالوگ جعبه‌های تاپین انجام نشد.');
      })
      .finally(() => { if (mounted) setLoading(false); });
    return () => { mounted = false; };
  }, [attempt]);

  const patch = (idx: number, part: Partial<TapinBoxAdminDto>) => {
    setSaved(false);
    setBoxes((prev) => prev.map((b, i) => (i === idx ? { ...b, ...part } : b)));
  };

  const addRow = () => {
    setSaved(false);
    setBoxes((prev) => [...prev, { id: '', title: '', lengthCm: 0, widthCm: 0, heightCm: 0, maxWeightGrams: null, isActive: true, sort: prev.length }]);
  };

  const save = async () => {
    if (loading || loadError || saving) return;
    setSaveError('');
    setSaved(false);
    setSaving(true);
    try {
      const res = await adminApi.updateTapinBoxes(boxes);
      if (res?.success !== true || !Array.isArray(res.boxes)) {
        throw new Error('پاسخ ذخیره کاتالوگ کامل دریافت نشد.');
      }
      setBoxes(res.boxes);
      setDefaultBoxId(res.defaultBoxId);
      setSaved(true);
    } catch (cause: unknown) {
      setSaveError(cause instanceof Error ? cause.message : 'ذخیره کاتالوگ جعبه‌ها انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  const num = (v: string) => {
    const n = parseInt(normalizePersianText(v).replace(/[^0-9]/g, ''), 10);
    return Number.isNaN(n) ? 0 : n;
  };

  return (
    <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-bold text-slate-900">جعبه‌های پستی تاپین (کاتالوگ پلتفرم)</h3>
        {defaultBoxId && <span className="rounded-full bg-blue-50 px-2 py-0.5 text-xs font-bold text-blue-800">
          جعبه پیش‌فرض پلتفرم: {toPersianDigits(defaultBoxId)}
        </span>}
      </div>
      <p className="text-[11px] leading-6 text-slate-500">
        تاپین برای جعبه‌ها API ندارد؛ این کاتالوگ را پلتفرم نگه می‌دارد. تأمین‌کننده هنگام ثبت مرسوله تاپین از همین فهرست انتخاب می‌کند و سیستم هم بر اساس ابعاد و وزن بسته، نزدیک‌ترین جعبه را از همین‌ها پیشنهاد می‌دهد. حذف جعبه با غیرفعال‌کردن آن است تا سوابق مرسوله‌ها سالم بماند.
      </p>
      {loading && <p role="status" className="text-sm text-slate-600">در حال دریافت کاتالوگ جعبه‌ها…</p>}
      {loadError && (
        <div className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">
          <p role="alert" className="[overflow-wrap:anywhere]">{loadError}</p>
          <button type="button" onClick={() => { setLoading(true); setLoadError(''); setAttempt(value => value + 1); }}
            className="mt-1 min-h-11 rounded-lg px-2 font-bold focus-visible:outline-2 focus-visible:outline-rose-700">تلاش دوباره</button>
        </div>
      )}
      {!loading && !loadError && (
        <fieldset disabled={saving} className="min-w-0 space-y-2">
          {boxes.map((b, idx) => (
            <div key={idx} className="grid grid-cols-2 gap-2 rounded-xl border border-slate-200 bg-slate-50 p-2.5 sm:grid-cols-3 xl:grid-cols-7 [&>label]:min-w-0">
              <label className="block">
                <span className="text-[10px] text-slate-500 block mb-1">شناسه</span>
                <input value={b.id} onChange={(e) => patch(idx, { id: e.target.value })} className={capacityInput} dir="ltr" />
              </label>
              <label className="block">
                <span className="text-[10px] text-slate-500 block mb-1">عنوان</span>
                <input value={b.title} onChange={(e) => patch(idx, { title: e.target.value })} className={capacityInput} />
              </label>
              <label className="block">
                <span className="text-[10px] text-slate-500 block mb-1">طول (cm)</span>
                <input value={b.lengthCm || ''} onChange={(e) => patch(idx, { lengthCm: num(e.target.value) })} className={capacityInput} dir="ltr" inputMode="numeric" />
              </label>
              <label className="block">
                <span className="text-[10px] text-slate-500 block mb-1">عرض (cm)</span>
                <input value={b.widthCm || ''} onChange={(e) => patch(idx, { widthCm: num(e.target.value) })} className={capacityInput} dir="ltr" inputMode="numeric" />
              </label>
              <label className="block">
                <span className="text-[10px] text-slate-500 block mb-1">ارتفاع (cm)</span>
                <input value={b.heightCm || ''} onChange={(e) => patch(idx, { heightCm: num(e.target.value) })} className={capacityInput} dir="ltr" inputMode="numeric" />
              </label>
              <label className="block">
                <span className="text-[10px] text-slate-500 block mb-1">حداکثر وزن (گرم، خالی = نامحدود)</span>
                <input value={b.maxWeightGrams ?? ''} onChange={(e) => patch(idx, { maxWeightGrams: e.target.value.trim() === '' ? null : num(e.target.value) })} className={capacityInput} dir="ltr" inputMode="numeric" />
              </label>
              <label className="flex min-h-11 items-center gap-2">
                <input type="checkbox" checked={b.isActive} onChange={(e) => patch(idx, { isActive: e.target.checked })} />
                <span className="text-[11px] font-bold text-slate-600">فعال</span>
              </label>
            </div>
          ))}
          <div className="flex flex-wrap gap-2 pt-1">
            <button type="button" onClick={addRow} className="min-h-11 rounded-xl border border-slate-300 px-3 text-sm font-bold text-slate-700 focus-visible:outline-2 focus-visible:outline-brand">
              + جعبه جدید
            </button>
            <button type="button" onClick={() => void save()} className="min-h-11 flex-1 rounded-xl bg-brand-deep px-3 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50">
              {saving ? 'در حال ذخیره…' : 'ذخیره کاتالوگ جعبه‌ها'}
            </button>
          </div>
        </fieldset>
      )}
      {saved && <p role="status" className="text-sm text-emerald-800">کاتالوگ جعبه‌های تاپین ذخیره شد.</p>}
      {saveError && <p role="alert" className="rounded-lg bg-rose-50 p-3 text-sm text-rose-800 [overflow-wrap:anywhere]">{saveError}</p>}
    </div>
  );
};

