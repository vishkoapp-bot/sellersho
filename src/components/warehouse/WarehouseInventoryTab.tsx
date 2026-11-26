import { useCallback, useEffect, useId, useState } from 'react';
import { Layers, MapPin, RefreshCw, Search } from 'lucide-react';
import { wmsApi, type WarehouseBinDto } from '../../services/wmsApi';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

const count = new Intl.NumberFormat('fa-IR');
const pageSize = 24;
const capacityLabels = { verified: 'ظرفیت تأییدشده', unverified: 'ظرفیت تأییدنشده', disputed: 'ظرفیت مورد اختلاف' };

function BinResult({ bin, showDetails }: { bin: WarehouseBinDto; showDetails: boolean }) {
  const location = [bin.warehouseName, bin.hallName, bin.aisle, bin.shelf].filter(Boolean).join(' · ');
  const updated = bin.updatedAt ? new Date(bin.updatedAt) : null;
  return (
    <li className="min-w-0 space-y-3 rounded-2xl border border-brand-border bg-white p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h3 className="break-all text-base font-extrabold text-slate-900"><bdi className="font-mono">{bin.binCode}</bdi></h3>
          {location && <p className="mt-1 flex items-start gap-1.5 text-sm text-slate-600"><MapPin aria-hidden="true" className="mt-0.5 size-4 shrink-0" />{location}</p>}
        </div>
        {bin.capacityStatus && <span className={`rounded-full px-2.5 py-1 text-xs font-bold ${bin.capacityStatus === 'verified' ? 'bg-emerald-50 text-emerald-800' : 'bg-amber-50 text-amber-900'}`}>
          {capacityLabels[bin.capacityStatus]}
        </span>}
      </div>

      {Number.isFinite(bin.quantity) && <div className="flex flex-wrap justify-between gap-x-3 gap-y-1 rounded-xl bg-brand-bg p-3 text-sm text-slate-800">
        <p className="font-bold">موجودی کل قفسه</p>
        <p className="font-mono">{count.format(bin.quantity)} عدد</p>
      </div>}
      {bin.unavailableReason && <p className="text-sm text-amber-900">{bin.unavailableReason}</p>}

      <details open={showDetails}>
        <summary className="min-h-11 cursor-pointer border-t border-brand-border py-2.5 text-sm font-bold text-brand-deep">جزئیات ظرفیت و موجودی</summary>
        <dl className="grid grid-cols-1 gap-x-4 gap-y-2 text-sm text-slate-700 sm:grid-cols-2">
          {bin.remainingVolumeCm3 != null && <div><dt>حجم آزاد</dt><dd className="font-bold">{count.format(bin.remainingVolumeCm3)} سانتی‌متر مکعب</dd></div>}
          {bin.reservedVolumeCm3 != null && <div><dt>حجم رزروشده</dt><dd className="font-bold">{count.format(bin.reservedVolumeCm3)} سانتی‌متر مکعب</dd></div>}
          {bin.occupiedVolumeCm3 != null && <div><dt>حجم اشغال‌شده</dt><dd className="font-bold">{count.format(bin.occupiedVolumeCm3)} سانتی‌متر مکعب</dd></div>}
          {bin.capacityVolumeCm3 != null && <div><dt>ظرفیت کل</dt><dd className="font-bold">{count.format(bin.capacityVolumeCm3)} سانتی‌متر مکعب</dd></div>}
          {bin.occupiedWeightGrams != null && <div><dt>وزن ثبت‌شده</dt><dd className="font-bold">{count.format(bin.occupiedWeightGrams)} گرم</dd></div>}
          {bin.maxWeightGrams != null && <div><dt>حد وزن قفسه</dt><dd className="font-bold">{count.format(bin.maxWeightGrams)} گرم</dd></div>}
          {bin.productTitle && <div><dt>عنوان ثبت‌شدهٔ کالا</dt><dd className="break-words font-bold">{bin.productTitle}</dd></div>}
          {bin.productId && <div><dt>شناسه کالا</dt><dd><bdi className="break-all font-mono font-bold">{bin.productId}</bdi></dd></div>}
          {updated && Number.isFinite(updated.getTime()) && <div><dt>آخرین به‌روزرسانی</dt><dd><time dateTime={bin.updatedAt} className="font-bold">{updated.toLocaleString('fa-IR')}</time></dd></div>}
        </dl>
      </details>
    </li>
  );
}

export function WarehouseBinInventory({ initialBinCode = '' }: { initialBinCode?: string }) {
  const queryId = useId();
  const [bins, setBins] = useState<WarehouseBinDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState(initialBinCode);
  const [query, setQuery] = useState(initialBinCode);
  const [limit, setLimit] = useState(pageSize);

  const reload = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setBins(await wmsApi.getBins());
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'موجودی قفسه‌ها دریافت نشد.');
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => { void reload(); }, [reload]);

  const exactBin = query ? bins.find(bin => bin.binCode === query) : undefined;
  const search = query.toLowerCase();
  const results = exactBin ? [exactBin] : search ? bins.filter(bin =>
    [bin.binCode, bin.warehouseName, bin.productTitle].some(value => value?.toLowerCase().includes(search))) : bins;

  return (
    <div className="space-y-4">
      <form onSubmit={event => { event.preventDefault(); setQuery(draft.trim()); setLimit(pageSize); }}
        className="space-y-2 rounded-2xl border border-brand-border bg-white p-4">
        <label className="block text-sm font-bold text-slate-900" htmlFor={queryId}>کد قفسه، نام انبار یا کالا</label>
        <div className="flex flex-wrap items-center gap-2">
          <input id={queryId} type="search" value={draft} onChange={event => setDraft(event.target.value)}
            autoComplete="off" autoCapitalize="off" spellCheck={false} enterKeyHint="search" dir="auto"
            className="min-h-11 min-w-0 grow basis-36 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base focus-visible:outline-2 focus-visible:outline-brand sm:text-sm" />
          <button type="submit" className="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-3 py-2 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand">
            <Search aria-hidden="true" className="size-4" />جست‌وجو
          </button>
          <button type="button" aria-label="تازه‌سازی موجودی" title="تازه‌سازی موجودی" onClick={() => void reload()} disabled={loading}
            className="grid size-11 shrink-0 place-items-center rounded-xl border border-brand-border text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">
            <RefreshCw aria-hidden="true" className={`size-4 ${loading ? 'motion-safe:animate-spin' : ''}`} />
          </button>
        </div>
      </form>
      <SupplierResourceState loading={loading} error={error} retry={() => void reload()} />
      {!loading && !error && results.length === 0 && (
        <SupplierEmptyState icon={<Layers />} title={query ? 'قفسه‌ای با این جست‌وجو یافت نشد.' : 'قفسه‌ای در دسترس نیست.'} />
      )}
      {!loading && !error && results.length > 0 && (
        <>
          <p role="status" className="px-1 text-sm text-slate-600">{count.format(results.length)} قفسه</p>
          <ul className="grid items-start gap-3">
            {results.slice(0, limit).map(bin => <BinResult key={bin.binCode} bin={bin} showDetails={results.length === 1} />)}
          </ul>
          {results.length > limit && <button type="button" onClick={() => setLimit(current => current + pageSize)}
            className="min-h-11 rounded-xl border border-brand-border bg-white px-4 py-2 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand">
            نمایش قفسه‌های بیشتر
          </button>}
        </>
      )}
    </div>
  );
}

export function WarehouseInventoryTab({ initialBinCode }: { initialBinCode?: string }) {
  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="موجودی قفسه‌ها" description="کالا و ظرفیت ثبت‌شده را در قفسه مقصد بررسی کنید." icon={<Layers />} />
      <WarehouseBinInventory key={initialBinCode} initialBinCode={initialBinCode} />
    </div>
  );
}
