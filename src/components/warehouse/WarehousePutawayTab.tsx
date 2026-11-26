import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { ArrowLeft, CheckCircle2, Layers, RefreshCw, Ruler, ScanLine } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { wmsApi, type PutawayTaskDto, type WarehouseBinDto } from '../../services/wmsApi';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { WarehouseBinLookupModal } from './WarehouseBinLookupModal';
import { BinSuggestions, WarehousePositionEditor } from './WarehouseOptimizationTools';

interface Props {
  initialInboundId?: string;
  onGoToInventory?: (binCode: string) => void;
  onGoToPicking: () => void;
}

type MeasurementField = 'lengthMm' | 'widthMm' | 'heightMm' | 'weightGrams';
type Measurement = Record<MeasurementField | 'reason', string>;
type CompletedPlacement = { title: string; binCode: string };

const measurementFields: { key: MeasurementField; label: string; max: number }[] = [
  { key: 'lengthMm', label: 'طول محصول', max: 1000000 },
  { key: 'widthMm', label: 'عرض محصول', max: 1000000 },
  { key: 'heightMm', label: 'ارتفاع محصول', max: 1000000 },
  { key: 'weightGrams', label: 'وزن محصول', max: 1000000000 },
];
const count = new Intl.NumberFormat('fa-IR');
const cardClass = 'min-w-0 space-y-4 rounded-[22px] border border-brand-border bg-white p-4 shadow-sm sm:p-5';
const inputClass = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus-visible:outline-2 focus-visible:outline-brand sm:text-sm';
const primaryClass = 'inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-brand-deep/90 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60 sm:w-auto';
const secondaryClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-brand-border bg-white px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60';

function PutawayMeasurement({ task, disabled, onSave }: {
  task: PutawayTaskDto;
  disabled: boolean;
  onSave: (value: Measurement) => Promise<void>;
}) {
  const [draft, setDraft] = useState<Measurement>(() => ({
    lengthMm: String(task.lengthMm || ''),
    widthMm: String(task.widthMm || ''),
    heightMm: String(task.heightMm || ''),
    weightGrams: String(task.weightGrams || ''),
    reason: '',
  }));

  return (
    <form className="space-y-3" onSubmit={event => { event.preventDefault(); void onSave(draft); }}>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {measurementFields.map(({ key, label, max }) => (
          <label key={key} className="min-w-0 space-y-1 text-sm font-medium text-slate-700">
            <span className="block">{label} {key === 'weightGrams' ? '(گرم)' : '(میلی‌متر)'}</span>
            <input aria-label={label} type="number" inputMode="numeric" min={1} max={max} step={1} required
              value={draft[key]} onChange={event => setDraft(current => ({ ...current, [key]: event.target.value }))}
              disabled={disabled} dir="ltr" className={inputClass} />
          </label>
        ))}
      </div>
      <label className="block space-y-1 text-sm font-medium text-slate-700">
        <span className="block">علت اندازه‌گیری</span>
        <input aria-label="علت اندازه‌گیری" value={draft.reason} disabled={disabled} required
          onChange={event => setDraft(current => ({ ...current, reason: event.target.value }))} className={inputClass} />
      </label>
      <button type="submit" disabled={disabled} className={primaryClass}>
        <Ruler aria-hidden="true" className="size-4" />تأیید اندازه‌گیری
      </button>
    </form>
  );
}

function PutawayDestination({ task, bins, disabled, onReserve }: {
  task: PutawayTaskDto;
  bins: WarehouseBinDto[];
  disabled: boolean;
  onReserve: (binCode: string) => Promise<void>;
}) {
  const reservedBin = task.status === 'assigned' ? task.binLocation : '';
  const [selection, setSelection] = useState<string | null>(null);
  const suggestedBin = task.binLocation;
  const binCode = selection ?? (bins.some(bin => bin.binCode === suggestedBin) ? suggestedBin : '');
  const selectedBin = bins.find(bin => bin.binCode === binCode);
  const reserve = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (selectedBin && selectedBin.binCode !== reservedBin && !disabled) void onReserve(selectedBin.binCode);
  };

  return (
    <div className="space-y-3">
      <form onSubmit={reserve} className="space-y-3">
        <label className="block space-y-1 text-sm font-medium text-slate-700">
          <span className="block">قفسه مقصد</span>
          <select dir="ltr" value={binCode} onChange={event => setSelection(event.target.value)} disabled={disabled || bins.length === 0}
            className={inputClass}>
            <option value="">انتخاب قفسه</option>
            {bins.map(bin => (
              <option key={bin.binCode} value={bin.binCode}>
                {bin.binCode}{bin.warehouseName ? ` · ${bin.warehouseName}` : ''}
              </option>
            ))}
          </select>
        </label>
        {selectedBin && (
          <p className="text-sm leading-6 text-slate-600">
            ظرفیت باقی‌مانده {count.format(selectedBin.remainingVolumeCm3 ?? 0)} سانتی‌متر مکعب
          </p>
        )}
        {bins.length === 0 && <p className="text-sm leading-6 text-slate-600">قفسه‌ای با ظرفیت تأییدشده و حجم آزاد در دسترس نیست.</p>}
        <button type="submit" disabled={disabled || !selectedBin || selectedBin.binCode === reservedBin} className={primaryClass}>
          {reservedBin ? 'تغییر قفسه و رزرو ظرفیت' : 'رزرو ظرفیت قفسه'}
        </button>
      </form>
      {bins.length > 0 && <BinSuggestions taskId={task.id} onSelect={setSelection} />}
    </div>
  );
}

export function WarehousePutawayTab({ initialInboundId, onGoToInventory, onGoToPicking }: Props) {
  const { success } = useToast();
  const [tasks, setTasks] = useState<PutawayTaskDto[]>([]);
  const [bins, setBins] = useState<WarehouseBinDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [loadError, setLoadError] = useState('');
  const [actionError, setActionError] = useState<{ id: string; message: string } | null>(null);
  const [completed, setCompleted] = useState<CompletedPlacement | null>(null);
  const [lookup, setLookup] = useState<{ binCode?: string } | null>(null);

  const reload = useCallback(async (showLoading = true) => {
    if (showLoading) setLoading(true);
    setLoadError('');
    try {
      const [queue, binList] = await Promise.all([wmsApi.getPutawayQueue(), wmsApi.getBins()]);
      setTasks(queue.queue || []);
      setBins(binList || []);
    } catch (cause) {
      setLoadError(cause instanceof Error ? cause.message : 'صف جانمایی دریافت نشد.');
    } finally {
      if (showLoading) setLoading(false);
    }
  }, []);
  useEffect(() => { void reload(); }, [reload]);

  async function run(task: PutawayTaskDto, operation: () => Promise<unknown>, message: string, after?: () => void) {
    if (busy) return;
    setBusy(task.id);
    setActionError(null);
    try {
      await operation();
      after?.();
      success(message);
      await reload(false);
    } catch (cause) {
      setActionError({ id: task.id, message: cause instanceof Error ? cause.message : 'عملیات جانمایی انجام نشد.' });
    } finally {
      setBusy(null);
    }
  }

  async function saveMeasurement(task: PutawayTaskDto, draft: Measurement) {
    if (!draft.reason.trim() || measurementFields.some(({ key, max }) => {
      const value = Number(draft[key]);
      return !Number.isSafeInteger(value) || value <= 0 || value > max;
    })) {
      setActionError({ id: task.id, message: 'ابعاد، وزن و علت اندازه‌گیری را بررسی کنید.' });
      return;
    }
    await run(task, () => wmsApi.recordMeasurement(task.productId, {
      variantId: task.variantId || undefined,
      lengthMm: Number(draft.lengthMm),
      widthMm: Number(draft.widthMm),
      heightMm: Number(draft.heightMm),
      weightGrams: Number(draft.weightGrams),
      reason: draft.reason.trim(),
    }), 'اندازه‌گیری کالا تأیید شد.');
  }

  const eligibleBins = bins.filter(bin => bin.capacityStatus === 'verified' && (bin.remainingVolumeCm3 ?? 0) > 0);
  const contextTasks = initialInboundId ? tasks.filter(task => task.inboundId === initialInboundId) : [];
  const queue = contextTasks.length ? [...contextTasks, ...tasks.filter(task => task.inboundId !== initialInboundId)] : tasks;

  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="جانمایی کالا" description="کالاهای تأییدشده را به قفسه مقصد منتقل کنید." icon={<Layers />}
        action={<button type="button" onClick={() => void reload()} disabled={loading || busy !== null} className={secondaryClass}>
          <RefreshCw aria-hidden="true" className={`size-4 ${loading ? 'motion-safe:animate-spin' : ''}`} />به‌روزرسانی
        </button>} />

      {completed && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
          <p role="status" className="text-sm font-bold leading-6 text-emerald-900">
            {completed.title} در قفسه <bdi className="font-mono">{completed.binCode}</bdi> ثبت شد.
          </p>
          <button type="button" className={secondaryClass} onClick={() => {
            if (onGoToInventory) onGoToInventory(completed.binCode);
            else setLookup({ binCode: completed.binCode });
          }}>بررسی موجودی قفسه</button>
        </div>
      )}
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {!loading && !loadError && queue.length === 0 && (
        <SupplierEmptyState icon={<Layers />} title="کالایی در انتظار جانمایی نیست." />
      )}
      {!loading && !loadError && queue.map(task => {
        const isVerified = task.measurementStatus === 'verified';
        const reservedBin = task.status === 'assigned' ? task.binLocation : '';
        const reservedWarehouse = bins.find(bin => bin.binCode === reservedBin)?.warehouseName;
        const reserve = (binCode: string) => run(task, () => wmsApi.assignPutawayTask(task.id, binCode), 'رزرو ظرفیت قفسه ثبت شد.');
        const measurement = <PutawayMeasurement key={task.measurementRevision || task.measurementStatus} task={task}
          disabled={busy !== null} onSave={draft => saveMeasurement(task, draft)} />;
        const destination = <PutawayDestination key={reservedBin} task={task} bins={eligibleBins} disabled={busy !== null} onReserve={reserve} />;

        return (
          <section key={task.id} className={cardClass}>
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="min-w-0 space-y-1">
                <h3 className="break-words text-base font-extrabold leading-7 text-slate-900">{task.title}</h3>
                <p className="break-words text-sm text-slate-600">
                  {task.variantName || (task.variantId ? 'تنوع کالا' : 'کالای بدون تنوع')}
                  {task.variantSpnc && <> · <bdi className="font-mono">{task.variantSpnc}</bdi></>}
                </p>
              </div>
              <span className="rounded-full bg-brand-soft px-3 py-1 text-xs font-bold text-brand-deep">{count.format(task.quantity)} عدد</span>
            </div>
            {task.reconciliationRequired ? (
              <p className="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-900">برای جانمایی، موجودی و رسید این کالا باید تطبیق داده شوند.</p>
            ) : !isVerified ? (
              <>
                <h4 className="text-sm font-extrabold text-slate-900">اندازه‌گیری همین کالا را تأیید کنید</h4>
                {task.measurementStatus === 'disputed' && <p className="text-sm text-amber-900">ابعاد یا وزن ثبت‌شده مورد اختلاف است.</p>}
                {measurement}
              </>
            ) : reservedBin ? (
              <>
                <div className="space-y-1 rounded-xl border border-brand-border bg-brand-bg p-3">
                  <p className="text-sm font-bold text-brand-deep">قفسه رزروشده <bdi className="break-all font-mono">{reservedBin}</bdi></p>
                  {reservedWarehouse && <p className="text-sm text-slate-700">{reservedWarehouse}</p>}
                  <p className="text-sm leading-6 text-slate-700">بعد از قرار دادن کالا در این قفسه، جانمایی را تکمیل کنید.</p>
                </div>
                <button type="button" className={primaryClass} disabled={busy !== null} onClick={() => void run(task,
                  () => wmsApi.confirmPutaway(task.id), 'جانمایی و انتقال موجودی ثبت شد.',
                  () => setCompleted({ title: task.title, binCode: reservedBin }))}>
                  <CheckCircle2 aria-hidden="true" className="size-4" />تکمیل جانمایی
                </button>
                <details className="border-t border-brand-border pt-2">
                  <summary className="min-h-11 cursor-pointer py-2 text-sm font-bold text-brand-deep">تغییر قفسه مقصد</summary>
                  <div className="space-y-3">
                    <button type="button" disabled={busy !== null} className={secondaryClass} onClick={() => void reserve(reservedBin)}>
                      تازه‌سازی رزرو همین قفسه
                    </button>
                    {destination}
                  </div>
                </details>
              </>
            ) : (
              <>
                <h4 className="text-sm font-extrabold text-slate-900">قفسه مقصد را رزرو کنید</h4>
                {destination}
              </>
            )}
            {actionError?.id === task.id && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{actionError.message}</p>}
            <details className="border-t border-brand-border pt-2">
              <summary className="min-h-11 cursor-pointer py-2 text-sm font-bold text-brand-deep">جزئیات جانمایی{isVerified && !task.reconciliationRequired && ' و اندازه‌گیری'}</summary>
              <dl className="mb-3 space-y-1 text-sm text-slate-700">
                <div className="flex flex-wrap items-baseline gap-2">
                  <dt>شناسه جانمایی</dt><dd><bdi className="break-all font-mono">{task.id}</bdi></dd>
                </div>
                {task.inboundId && <div className="flex flex-wrap items-baseline gap-2">
                  <dt>رسید ورودی</dt><dd><bdi className="break-all font-mono">{task.inboundId}</bdi></dd>
                </div>}
                {task.variantId && <div className="flex flex-wrap items-baseline gap-2">
                  <dt>شناسه تنوع</dt><dd><bdi className="break-all font-mono">{task.variantId}</bdi></dd>
                </div>}
                {isVerified && <div className="flex flex-wrap items-baseline gap-2">
                  <dt>اندازه‌گیری تأییدشده</dt><dd>{count.format(task.lengthMm)} × {count.format(task.widthMm)} × {count.format(task.heightMm)} میلی‌متر · {count.format(task.weightGrams)} گرم</dd>
                </div>}
              </dl>
              {isVerified && !task.reconciliationRequired && measurement}
            </details>
          </section>
        );
      })}
      <div className="flex flex-wrap gap-2">
        <button type="button" className={secondaryClass} onClick={() => setLookup({})}>
          <ScanLine aria-hidden="true" className="size-4" />استعلام قفسه
        </button>
        <button type="button" className={secondaryClass} onClick={onGoToPicking}>
          صف جمع‌آوری سفارش <ArrowLeft aria-hidden="true" className="size-4" />
        </button>
      </div>
      <WarehousePositionEditor />
      {lookup && <WarehouseBinLookupModal isOpen onClose={() => setLookup(null)} initialBinCode={lookup.binCode} />}
    </div>
  );
}
