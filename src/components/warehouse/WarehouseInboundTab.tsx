import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { ArrowLeft, CheckCircle2, Package, RefreshCw, ScanBarcode } from 'lucide-react';
import { type InboundShipmentDto, type UnitReceiptResult, wmsApi } from '../../services/wmsApi';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { WarehouseInboundLineReceipt } from './WarehouseInboundLineReceipt';

interface Props {
  onGoToQC: (inboundId: string) => void;
}

type ManifestLine = NonNullable<InboundShipmentDto['items']>[number] & {
  variant_id?: string;
  variantId?: string;
  product_title?: string;
  productTitle?: string;
};

const openStatuses = new Set(['pending', 'in_transit', 'receiving']);
const statusLabels: Record<string, string> = {
  scheduled: 'برنامه‌ریزی‌شده',
  pending: 'در انتظار دریافت',
  in_transit: 'در راه انبار',
  receiving: 'در حال دریافت',
  received: 'دریافت‌شده',
  cancelled: 'لغوشده',
  qc_passed: 'کنترل کیفیت تأیید شده',
  qc_failed: 'کنترل کیفیت رد شده',
};

const primaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50';
const secondaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-brand-border px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';
const inputClass = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus-visible:ring-2 focus-visible:ring-brand disabled:bg-slate-50';

function legacyCounts(shipment: InboundShipmentDto) {
  const lines = new Map<string, { declared: number; scanned: number }>();
  for (const item of shipment.items ?? []) {
    const id = item.productId || item.product_id;
    const quantity = item.quantity || (shipment.items?.length === 1 ? shipment.itemsCount : 0);
    if (!id || !Number.isSafeInteger(quantity) || quantity < 1) return new Map<string, { declared: number; scanned: number }>();
    lines.set(id, { declared: (lines.get(id)?.declared ?? 0) + quantity, scanned: 0 });
  }
  for (const scan of shipment.scannedItems ?? []) {
    const id = scan.productId || scan.product_id;
    const line = id && lines.get(id);
    if (line) line.scanned += scan.quantity ?? 0;
  }
  return lines;
}

const lineIdFor = (item: ManifestLine) => item.line_id || item.lineId;

export function WarehouseInboundTab({ onGoToQC }: Props) {
  const [shipments, setShipments] = useState<InboundShipmentDto[]>([]);
  const [selectedId, setSelectedId] = useState('');
  const [code, setCode] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [lookupError, setLookupError] = useState('');
  const [receiptError, setReceiptError] = useState('');
  const [busy, setBusy] = useState(false);
  const [counts, setCounts] = useState<Record<string, string>>({});
  const [receipts, setReceipts] = useState<Record<string, UnitReceiptResult>>({});
  const [completed, setCompleted] = useState<Set<string>>(new Set());
  const scanInput = useRef<HTMLInputElement>(null);
  const detailHeading = useRef<HTMLHeadingElement>(null);
  const submitting = useRef(false);

  const reload = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try { setShipments(await wmsApi.getInbounds()); }
    catch (cause) { setLoadError(cause instanceof Error ? cause.message : 'صف محموله‌های ورودی دریافت نشد.'); }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { void reload(); }, [reload]);
  useEffect(() => { if (selectedId) detailHeading.current?.focus(); }, [selectedId]);

  const shipment = shipments.find(item => item.id === selectedId);
  const queue = shipments.filter(item => openStatuses.has(item.status));
  const lines = (shipment?.items ?? []) as ManifestLine[];
  const hasExactLines = lines.some(lineIdFor);
  const isOpen = !!shipment && openStatuses.has(shipment.status);
  const recorded = shipment ? legacyCounts(shipment) : new Map<string, { declared: number; scanned: number }>();
  const hasReceipt = !!shipment && (
    completed.has(shipment.id) || ['received', 'qc_passed', 'qc_failed'].includes(shipment.status) ||
    lines.some(line => { const id = lineIdFor(line); return !!id && !!receipts[id]?.quantity; })
  );

  function inspect(item: InboundShipmentDto) {
    if (submitting.current) return;
    if (selectedId === item.id) {
      detailHeading.current?.focus();
    } else {
      const recordedCounts = legacyCounts(item);
      const hasScans = [...recordedCounts.values()].some(line => line.scanned > 0);
      setCounts(Object.fromEntries([...recordedCounts].map(([id, line]) => [id, String(hasScans ? line.scanned : line.declared)])));
      setSelectedId(item.id);
      setReceiptError('');
    }
    setLookupError('');
    setCode('');
  }

  function lookup(event: FormEvent) {
    event.preventDefault();
    if (loading || loadError || submitting.current) return;
    const query = code.trim().toUpperCase();
    const item = query && shipments.find(candidate => candidate.id.toUpperCase() === query);
    if (item) inspect(item);
    else setLookupError(query ? 'محموله‌ای با این کد پیدا نشد. کد روی مانیفست را بررسی کنید.' : 'کد محموله را وارد کنید.');
  }

  async function receiveManifest() {
    if (!shipment || !isOpen || submitting.current || hasExactLines || recorded.size === 0) return;
    const totals = new Map([...recorded].map(([id]) => [id, Number(counts[id])]));
    if ([...totals].every(([, quantity]) => quantity === 0) || [...recorded].some(([id, line]) => {
      const total = totals.get(id)!;
      return !counts[id]?.trim() || !Number.isSafeInteger(total) || total < line.scanned || total > line.declared || total > 2147483647;
    })) {
      setReceiptError('تعداد کل دریافتی را با مانیفست و تعداد ثبت‌شده تطبیق دهید.');
      return;
    }

    submitting.current = true;
    setBusy(true);
    setReceiptError('');
    try {
      // A scan appends to the ledger. Read the stored totals first so an
      // interrupted request can resume with only the missing quantities.
      const latest = await wmsApi.getInbounds();
      setShipments(latest);
      const current = latest.find(item => item.id === shipment.id);
      if (!current || current.items?.some(lineIdFor) || ![...openStatuses, 'received'].includes(current.status)) {
        throw new Error('وضعیت محموله تغییر کرده است. دوباره آن را بررسی کنید.');
      }
      const latestCounts = legacyCounts(current);
      if (latestCounts.size !== recorded.size || [...latestCounts].some(([id, line]) =>
        line.declared !== recorded.get(id)?.declared || line.scanned > (totals.get(id) ?? 0) ||
        (current.status === 'received' && line.scanned !== totals.get(id)))) {
        throw new Error('تعدادهای ثبت‌شده تغییر کرده‌اند. جزئیات محموله را دوباره بررسی کنید.');
      }
      if (current.status !== 'receiving' && current.status !== 'received') {
        const started = await wmsApi.startReceiving(shipment.id);
        if (!started?.success) throw new Error('پاسخ شروع دریافت کامل نبود. دوباره تلاش کنید.');
      }
      for (const [productId, line] of latestCounts) {
        const quantity = totals.get(productId)! - line.scanned;
        if (!quantity) continue;
        const scan = await wmsApi.scanInboundItem(shipment.id, { productId, barcode: shipment.id, quantity });
        if (!scan?.success) throw new Error('پاسخ ثبت تعداد کامل نبود. دوباره تلاش کنید.');
      }
      const receipt = await wmsApi.completeReceiving(shipment.id);
      if (!receipt?.success) throw new Error('پاسخ ثبت دریافت کامل نبود. دوباره تلاش کنید.');
      setCompleted(previous => new Set(previous).add(shipment.id));
      await reload();
    } catch (cause) {
      setReceiptError(cause instanceof Error ? cause.message : 'ثبت دریافت انجام نشد.');
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }

  function returnToQueue() {
    setSelectedId('');
    setReceiptError('');
    scanInput.current?.focus();
  }

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="محموله‌های ورودی" description="کد مانیفست را اسکن کنید یا محموله را از صف باز کنید." icon={<Package />} />
      <form onSubmit={lookup} className="space-y-2 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <label htmlFor="warehouse-manifest-code" className="block text-sm font-extrabold text-[#261b41]">کد مانیفست</label>
        <div className="grid gap-2 sm:grid-cols-[1fr_auto]">
          <input ref={scanInput} id="warehouse-manifest-code" value={code}
            onChange={event => { setCode(event.target.value); setLookupError(''); }} disabled={busy}
            placeholder="کد محموله را اسکن یا وارد کنید" className={inputClass + ' font-mono'} autoComplete="off" spellCheck={false}
            autoCapitalize="characters" enterKeyHint="search" dir="ltr" />
          <button disabled={loading || !!loadError || busy} type="submit" className={primaryButton}>
            <ScanBarcode aria-hidden="true" className="size-4" /> جست‌وجوی محموله
          </button>
        </div>
        {lookupError && <p role="alert" className="text-sm text-rose-800">{lookupError}</p>}
      </form>

      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />

      {shipment ? (
        <section aria-labelledby="warehouse-manifest-details" className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
          <div className="flex flex-wrap items-start justify-between gap-3 border-b border-slate-200 pb-3">
            <div className="min-w-0">
              <h3 ref={detailHeading} tabIndex={-1} id="warehouse-manifest-details" className="text-base font-extrabold text-[#261b41]">محتویات محموله</h3>
              <bdi dir="ltr" className="break-all font-mono text-sm font-bold text-slate-700">{shipment.id}</bdi>
            </div>
            <button type="button" className={secondaryButton} disabled={busy} onClick={returnToQueue}>بازگشت به صف</button>
          </div>
          <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
            <div><dt className="text-slate-600">تأمین‌کننده</dt><dd className="font-bold">{shipment.supplierName || 'ثبت نشده'}</dd></div>
            <div><dt className="text-slate-600">حامل</dt><dd className="font-bold">{shipment.carrier || 'ثبت نشده'}</dd></div>
            <div><dt className="text-slate-600">کارتن مورد انتظار</dt><dd className="font-bold">{shipment.cartonsCount?.toLocaleString('fa-IR') ?? '—'}</dd></div>
            <div><dt className="text-slate-600">واحد مورد انتظار</dt><dd className="font-bold">{shipment.itemsCount?.toLocaleString('fa-IR') ?? '—'}</dd></div>
          </dl>
          {isOpen && hasExactLines && (
            <div className="space-y-3">
              <p className="text-sm leading-6 text-slate-600">محتویات را بشمارید و دریافت را برای هر ردیف دارای شناسه ثبت کنید.</p>
              {lines.map(line => {
                const lineId = lineIdFor(line);
                return lineId && <WarehouseInboundLineReceipt key={lineId} lineId={lineId} expected={line.quantity ?? 0}
                  title={line.productTitle || line.product_title} productId={line.productId || line.product_id}
                  variantId={line.variantId || line.variant_id} receipt={receipts[lineId]}
                  onReceived={result => setReceipts(current => ({ ...current, [lineId]: result }))} />;
              })}
              {lines.some(line => !lineIdFor(line)) && <p className="rounded-xl bg-amber-50 p-3 text-sm text-amber-900">بعضی اقلام این مانیفست شناسه ردیف دریافت ندارند و به بررسی نیاز دارند.</p>}
            </div>
          )}
          {isOpen && !hasExactLines && recorded.size > 0 && (
            <form onSubmit={event => { event.preventDefault(); void receiveManifest(); }} className="space-y-3">
              <p className="text-sm leading-6 text-slate-600">کالاها را از روی مانیفست بشمارید و تعداد کل دریافتی هر کالا را ثبت کنید.</p>
              {[...recorded].map(([id, line]) => (
                <label key={id} className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-brand-bg p-3 text-sm">
                  <span className="min-w-0">
                    <bdi dir="ltr" className="block break-all font-mono font-bold">{id}</bdi>
                    <span className="text-slate-600">مورد انتظار {line.declared.toLocaleString('fa-IR')} واحد{line.scanned > 0 && ' · شمارش ثبت‌شده ' + line.scanned.toLocaleString('fa-IR')}</span>
                  </span>
                  <span className="w-32">
                    تعداد کل دریافتی
                    <input type="number" inputMode="numeric" min={line.scanned} max={line.declared} required value={counts[id] ?? ''} disabled={busy}
                      onChange={event => setCounts(current => ({ ...current, [id]: event.target.value }))} className={inputClass + ' mt-1'} />
                  </span>
                </label>
              ))}
              <button data-manifest-submit type="submit" className={primaryButton + ' w-full sm:w-auto'} disabled={busy || loading || !!loadError}>
                {busy ? 'در حال ثبت دریافت…' : 'تأیید دریافت محموله'}
              </button>
            </form>
          )}
          {isOpen && !hasExactLines && recorded.size === 0 && <p role="status" className="rounded-xl bg-amber-50 p-3 text-sm text-amber-900">اطلاعات اقلام این مانیفست برای ثبت دریافت کامل نیست.</p>}
          {!isOpen && <p className="text-sm font-bold text-slate-700">{statusLabels[shipment.status] ?? 'وضعیت نامشخص'}</p>}
          {receiptError && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{receiptError}</p>}
          {hasReceipt && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-emerald-200 bg-emerald-50 p-3">
              <p role="status" className="flex items-center gap-2 text-sm font-bold text-emerald-800"><CheckCircle2 aria-hidden="true" className="size-4" />{completed.has(shipment.id) ? 'دریافت محموله ثبت شد.' : 'محموله دارای دریافت ثبت‌شده است.'}</p>
              <button type="button" onClick={() => onGoToQC(shipment.id)} disabled={busy} className={secondaryButton + ' bg-white'}>
                رفتن به کنترل کیفیت <ArrowLeft aria-hidden="true" className="size-4" />
              </button>
            </div>
          )}
        </section>
      ) : !loading && !loadError && (
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-base font-extrabold text-[#261b41]">صف دریافت</h3>
            <button type="button" onClick={() => void reload()} className={secondaryButton}>
              <RefreshCw aria-hidden="true" className="size-4" /> به‌روزرسانی
            </button>
          </div>
          {queue.length === 0 ? <SupplierEmptyState icon={<Package />} title="محموله‌ای در صف دریافت نیست." /> :
            <ul className="space-y-2">
              {queue.map(item => (
                <li key={item.id} className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-[#e6e2f0] bg-white p-3 shadow-sm sm:p-4">
                  <div className="min-w-0">
                    <bdi dir="ltr" className="block break-all font-mono text-sm font-extrabold text-[#261b41]">{item.id}</bdi>
                    <p className="text-sm leading-6 text-slate-600">{item.supplierName || 'تأمین‌کننده ثبت نشده'} · {statusLabels[item.status]}</p>
                  </div>
                  <button type="button" onClick={() => inspect(item)} className={secondaryButton}>بررسی محموله</button>
                </li>
              ))}
            </ul>}
        </div>
      )}
    </div>
  );
}
