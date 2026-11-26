import { useEffect, useRef, useState } from 'react';
import { ArrowLeft, CheckCircle2, ChevronDown, ScanLine } from 'lucide-react';
import { ApiError, apiRequest } from '../../services/apiClient';
import { wmsApi, type UnitReceiptResult } from '../../services/wmsApi';
import { normalizePersianText } from '../../utils/routing';
import { completeScan, unitMutationError, type ScannedUnit } from '../../utils/unitScan';
import { UnitLookup } from '../common/UnitLookup';
import { UnitScanField } from '../common/UnitScanField';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { WarehouseUnitPrint } from './WarehouseUnitPrint';

interface ShipmentItem {
  variant_id: string;
  variant_name?: string;
  product_title?: string;
  spnc?: string;
  expected_qty: number;
  received_qty?: number;
  qc_status?: string;
}

interface Shipment {
  shipment_code: string;
  supplier_name?: string;
  status: string;
  items?: ShipmentItem[];
}

interface Props {
  onGoToQC?: (receiptLineIds: string[]) => void;
}

type QC = 'pass' | 'fail';
type ItemDraft = { amount: string; qc: QC; scans: ScannedUnit[]; confirmed: boolean };
type ReceiptPayload = { variant_id: string; received_qty: number; qc_status: QC; unit_codes?: string[] };

const openStatuses = new Set(['sent', 'in_transit', 'partially_received']);
const statusLabels: Record<string, string> = {
  draft: 'پیش‌نویس',
  created: 'ثبت‌شده',
  sent: 'ارسال‌شده',
  in_transit: 'در راه انبار',
  partially_received: 'دریافت بخشی از محموله',
  received: 'دریافت کامل',
  qc_passed: 'کنترل کیفیت تأیید شده',
  qc_failed: 'کنترل کیفیت رد شده',
  cancelled: 'لغوشده',
};
const primaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50';
const secondaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-brand-border bg-white px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';
const fieldClass = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus-visible:ring-2 focus-visible:ring-brand disabled:bg-slate-50';

function defaultDraft(item: ShipmentItem): ItemDraft {
  return {
    amount: String(item.received_qty || item.expected_qty || ''),
    qc: item.qc_status === 'failed' || item.qc_status === 'fail' ? 'fail' : 'pass',
    scans: [],
    confirmed: false,
  };
}

function counted(draft: ItemDraft) {
  const text = normalizePersianText(draft.amount.trim());
  return /^\d+$/.test(text) ? Number(text) : NaN;
}

function validDraft(item: ShipmentItem, draft: ItemDraft) {
  const quantity = counted(draft);
  return Number.isSafeInteger(quantity) && quantity >= (item.received_qty ?? 0) && quantity <= item.expected_qty &&
    completeScan(draft.scans, quantity);
}

function canCount(item: ShipmentItem) {
  return !!item.variant_id && Number.isSafeInteger(item.expected_qty) && item.expected_qty > (item.received_qty ?? 0);
}

export function WarehouseReceiveTab({ onGoToQC }: Props) {
  const [code, setCode] = useState('');
  const [lookingUp, setLookingUp] = useState(false);
  const [shipment, setShipment] = useState<Shipment | null>(null);
  const [drafts, setDrafts] = useState<Record<string, ItemDraft>>({});
  const [activeId, setActiveId] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [pending, setPending] = useState(false);
  const [receipts, setReceipts] = useState<UnitReceiptResult[]>([]);
  const [lookupError, setLookupError] = useState('');
  const [receiveError, setReceiveError] = useState('');
  const [itemError, setItemError] = useState('');
  const operation = useRef(false);
  const codeInput = useRef<HTMLInputElement>(null);
  const cartonHeading = useRef<HTMLHeadingElement>(null);
  const lineHeading = useRef<HTMLHeadingElement>(null);
  const focusLine = useRef(false);
  const shipmentCode = shipment?.shipment_code;

  useEffect(() => { if (shipmentCode) cartonHeading.current?.focus(); }, [shipmentCode]);
  useEffect(() => {
    if (focusLine.current) lineHeading.current?.focus();
    focusLine.current = false;
  }, [activeId]);

  const items = shipment?.items ?? [];
  const open = !!shipment && openStatuses.has(shipment.status);
  const confirmedItems = items.filter(item => drafts[item.variant_id]?.confirmed);
  const busy = submitting || lookingUp;
  const receiptLineIds = receipts.filter(receipt => receipt.quantity > 0).map(receipt => receipt.line_id);

  async function lookup() {
    if (operation.current || pending) return;
    const query = code.trim().toUpperCase();
    if (!query) { setLookupError('کد روی کارتن را وارد کنید.'); return; }
    operation.current = true;
    setLookingUp(true);
    setLookupError('');
    try {
      const { shipment: found } = await apiRequest<{ shipment: Shipment }>(`/warehouse/shipments/${encodeURIComponent(query)}`, { method: 'GET', auth: true });
      if (!found?.shipment_code) throw new Error('اطلاعات کارتن کامل دریافت نشد.');
      const manifest = found.items ?? [];
      setShipment(found);
      setDrafts(Object.fromEntries(manifest.map(item => [item.variant_id, defaultDraft(item)])));
      setActiveId(manifest.find(canCount)?.variant_id ?? '');
      setReceipts([]);
      setReceiveError('');
      setItemError('');
      setCode(found.shipment_code);
    } catch (cause) {
      setShipment(null);
      setLookupError(cause instanceof ApiError && cause.status === 404
        ? 'کارتنی با این کد پیدا نشد. کد روی برچسب را بررسی کنید.'
        : cause instanceof Error ? cause.message : 'جست‌وجوی کارتن انجام نشد.');
    } finally {
      operation.current = false;
      setLookingUp(false);
    }
  }

  function updateDraft(item: ShipmentItem, patch: Partial<ItemDraft>) {
    setItemError('');
    setDrafts(current => ({ ...current, [item.variant_id]: { ...current[item.variant_id], ...patch, confirmed: false } }));
  }

  function openLine(id: string) {
    focusLine.current = true;
    setActiveId(id);
    setItemError('');
  }

  function confirm(item: ShipmentItem, draft: ItemDraft) {
    if (busy || pending || !open) return;
    if (!validDraft(item, draft)) {
      setItemError('تعداد کل دریافتی و تعداد کدهای اسکن‌شده را بررسی کنید.');
      return;
    }
    const next = items.find(row => row.variant_id !== item.variant_id && canCount(row) && !drafts[row.variant_id]?.confirmed);
    setDrafts(current => ({ ...current, [item.variant_id]: { ...draft, confirmed: true } }));
    setItemError('');
    if (next) openLine(next.variant_id);
  }

  async function receive() {
    if (!shipment || !open || operation.current || pending || !confirmedItems.length) return;
    const payload: ReceiptPayload[] = [];
    for (const item of confirmedItems) {
      const draft = drafts[item.variant_id];
      if (!validDraft(item, draft)) {
        setReceiveError('تعداد و کدهای اسکن‌شده اقلام تأییدشده را بررسی کنید.');
        return;
      }
      payload.push({
        variant_id: item.variant_id,
        received_qty: counted(draft),
        qc_status: draft.qc,
        ...(draft.scans.length ? { unit_codes: draft.scans.map(unit => unit.code) } : {}),
      });
    }
    operation.current = true;
    setSubmitting(true);
    setReceiveError('');
    try {
      const result = await wmsApi.receiveShipment(shipment.shipment_code, { items: payload });
      const received = result && 'line_id' in result ? [result] : result?.items;
      if (!result || ('success' in result && result.success !== true) || !Array.isArray(received) ||
        received.length !== payload.length || received.some((row, index) => !row?.line_id || row.quantity !== payload[index].received_qty)) {
        throw new Error('پاسخ ثبت دریافت کامل نبود. همان دریافت را دوباره ارسال کنید.');
      }
      const updatedItems = items.map(item => {
        const accepted = payload.find(row => row.variant_id === item.variant_id);
        return accepted ? { ...item, received_qty: accepted.received_qty, qc_status: accepted.qc_status } : item;
      });
      const nextStatus = 'status' in result && typeof result.status === 'string'
        ? result.status
        : updatedItems.every(item => (item.received_qty ?? 0) >= item.expected_qty) ? 'received' : 'partially_received';

      setShipment({ ...shipment, items: updatedItems, status: nextStatus });
      setDrafts(current => {
        const next = { ...current };
        for (const [index, row] of payload.entries()) {
          const draft = current[row.variant_id];
          next[row.variant_id] = {
            ...draft,
            amount: String(row.received_qty),
            confirmed: false,
            scans: draft.scans.length ? (received[index].unit_codes ?? []).map(code => ({ code })) : [],
          };
        }
        return next;
      });
      setActiveId(updatedItems.find(item => item.variant_id === activeId && canCount(item))?.variant_id ?? updatedItems.find(canCount)?.variant_id ?? '');
      setReceipts(current => [...new Map([...current, ...received].map(row => [row.line_id, row])).values()]);
    } catch (cause) {
      setReceiveError(unitMutationError(cause, 'ثبت دریافت انجام نشد.'));
    } finally {
      operation.current = false;
      setSubmitting(false);
    }
  }

  function nextCarton() {
    setShipment(null);
    setCode('');
    setReceipts([]);
    setLookupError('');
    setReceiveError('');
    codeInput.current?.focus();
  }

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="دریافت کارتن" description="برچسب کارتن را اسکن کنید تا اقلام همان محموله برای شمارش باز شوند." icon={<ScanLine />} />
      <form onSubmit={event => { event.preventDefault(); void lookup(); }}
        className="space-y-2 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <label htmlFor="warehouse-carton-code" className="block text-sm font-extrabold text-[#261b41]">کد روی کارتن</label>
        <div className="grid gap-2 sm:grid-cols-[1fr_auto]">
          <input ref={codeInput} id="warehouse-carton-code" value={code}
            onChange={event => { setCode(event.target.value); setLookupError(''); }} disabled={busy || pending}
            placeholder="کد محموله روی کارتن (اسکن یا تایپ دستی)" className={fieldClass + ' font-mono'}
            dir="ltr" autoComplete="off" autoCapitalize="characters" spellCheck={false} enterKeyHint="search" />
          <button type="submit" disabled={busy || pending} className={primaryButton}>
            <ScanLine aria-hidden="true" className="size-4" /> {lookingUp ? 'در حال جست‌وجو…' : 'باز کردن کارتن'}
          </button>
        </div>
        {lookupError && <p role="alert" className="text-sm text-rose-800">{lookupError}</p>}
      </form>

      {shipment && (
        <section aria-labelledby="warehouse-carton-heading" className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
          <div className="flex flex-wrap items-start justify-between gap-3 border-b border-slate-200 pb-3">
            <div className="min-w-0 space-y-1">
              <h3 ref={cartonHeading} tabIndex={-1} id="warehouse-carton-heading" className="text-base font-extrabold text-[#261b41]">اقلام کارتن</h3>
              <bdi className="block break-all font-mono text-sm font-bold text-slate-700" dir="ltr">{shipment.shipment_code}</bdi>
              {shipment.supplier_name && <p className="text-sm text-slate-600">{shipment.supplier_name}</p>}
            </div>
            <span className="rounded-full bg-brand-soft px-2.5 py-1 text-xs font-bold text-brand-deep">{statusLabels[shipment.status] ?? 'وضعیت نامشخص'}</span>
          </div>

          {open && items.length > 0 && <p className="text-sm leading-6 text-slate-600">هر قلم را بشمارید و نتیجه بازرسی آن را تأیید کنید. اگر این کارتن قبلاً بخشی دریافت شده، تعداد کل را وارد کنید.</p>}
          {items.length === 0 && <p role="status" className="text-sm text-slate-600">قلمی برای این کارتن ثبت نشده است.</p>}
          <div className="space-y-3">
            {items.map(item => {
              const draft = drafts[item.variant_id];
              const active = open && activeId === item.variant_id && canCount(item);
              const found = item.received_qty ?? 0;
              return (
                <article key={item.variant_id} data-carton-item={item.variant_id} className="space-y-3 rounded-2xl border border-[#e6e2f0] p-3 sm:p-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <h4 ref={active ? lineHeading : undefined} tabIndex={active ? -1 : undefined} className="text-sm font-extrabold text-[#261b41]">
                        {item.product_title || 'کالای محموله'}{item.variant_name && ' · ' + item.variant_name}
                      </h4>
                      <p className="break-all text-sm text-slate-700">SKU <bdi dir="ltr" className="font-mono font-bold">{item.spnc || item.variant_id}</bdi></p>
                      <p className="text-sm text-slate-600">{item.expected_qty?.toLocaleString('fa-IR')} واحد مورد انتظار · {found.toLocaleString('fa-IR')} دریافت‌شده</p>
                    </div>
                    {open && !active && canCount(item) && (
                      <button type="button" data-carton-open className={secondaryButton} disabled={busy || pending}
                        onClick={() => openLine(item.variant_id)}>
                        {draft.confirmed ? 'بازبینی قلم تأییدشده' : 'بررسی قلم'} <ChevronDown aria-hidden="true" className="size-4" />
                      </button>
                    )}
                  </div>
                  {active && (
                    <div className="space-y-3 border-t border-slate-200 pt-3">
                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="block text-sm font-bold text-slate-700">
                          تعداد کل دریافتی
                          <input aria-label="تعداد کل دریافتی این قلم" type="text" inputMode="numeric" dir="ltr" value={draft.amount}
                            disabled={busy || pending} onChange={event => updateDraft(item, { amount: event.target.value })}
                            className={fieldClass + ' mt-1 font-mono'} />
                        </label>
                        <fieldset className="min-w-0 text-sm font-bold text-slate-700" disabled={busy || pending}>
                          <legend>نتیجه بازرسی</legend>
                          <div className="mt-1 grid grid-cols-2 gap-2">
                            {(['pass', 'fail'] as const).map(state => (
                              <button type="button" key={state} aria-pressed={draft.qc === state} onClick={() => updateDraft(item, { qc: state })}
                                className={draft.qc === state ? primaryButton : secondaryButton}>
                                {state === 'pass' ? 'سالم' : 'مردود'}
                              </button>
                            ))}
                          </div>
                        </fieldset>
                      </div>
                      <div className="[&_button]:min-h-11 [&_input]:min-h-11 [&_input]:text-base">
                        <UnitScanField key={shipment.shipment_code + '/' + item.variant_id} disabled={busy}
                          scans={draft.scans} quantity={Math.max(0, counted(draft) || 0)} onPending={setPending}
                          onChange={value => updateDraft(item, { scans: value })} />
                      </div>
                      {itemError && <p role="alert" className="text-sm text-rose-800">{itemError}</p>}
                      <button type="button" data-carton-confirm disabled={busy || pending} onClick={() => draft.confirmed ? updateDraft(item, {}) : confirm(item, draft)}
                        className={secondaryButton + ' w-full sm:w-auto'}>
                        <CheckCircle2 aria-hidden="true" className="size-4" /> {draft.confirmed ? 'لغو تأیید این قلم' : 'تأیید اسکن و QC این قلم'}
                      </button>
                    </div>
                  )}
                </article>
              );
            })}
          </div>

          {open && confirmedItems.length > 0 && (
            <button type="button" data-carton-submit onClick={() => void receive()} disabled={busy || pending}
              className={primaryButton + ' w-full sm:w-auto'}>
              {submitting ? 'در حال ثبت…' : 'تأیید دریافت محموله (' + confirmedItems.length + ' قلم تأییدشده)'}
            </button>
          )}
          {receiveError && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{receiveError}</p>}
          {!open && !['received', 'qc_passed', 'qc_failed'].includes(shipment.status) && (
            <p role="status" className="rounded-xl bg-amber-50 p-3 text-sm text-amber-900">این کارتن هنوز در وضعیت قابل دریافت نیست.</p>
          )}
          {['received', 'qc_passed', 'qc_failed'].includes(shipment.status) && (
            <p role="status" className="font-bold text-slate-700">این محموله به‌طور کامل دریافت شده است.</p>
          )}

          {receipts.length > 0 && (
            <div className="space-y-3 rounded-2xl border border-emerald-200 bg-emerald-50 p-3 sm:p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p role="status" className="flex items-center gap-2 text-sm font-extrabold text-emerald-900"><CheckCircle2 aria-hidden="true" className="size-4" /> دریافت محموله ثبت شد.</p>
                {onGoToQC && receiptLineIds.length > 0 && (
                  <button type="button" data-carton-next className={secondaryButton} disabled={busy || pending} onClick={() => onGoToQC(receiptLineIds)}>
                    رفتن به کنترل کیفیت <ArrowLeft aria-hidden="true" className="size-4" />
                  </button>
                )}
              </div>
              {receipts.map(receipt => (
                <div key={receipt.line_id} className="space-y-2 rounded-xl bg-white p-3 text-sm">
                  <p>ردیف دریافت <bdi dir="ltr" className="break-all font-mono">{receipt.line_id}</bdi> · {receipt.quantity.toLocaleString('fa-IR')} واحد</p>
                  {!!receipt.unit_codes?.length && <ul className="flex flex-wrap gap-2">{receipt.unit_codes.map(code => <li key={code}><code dir="ltr" className="break-all">{code}</code></li>)}</ul>}
                  {receipt.quantity > 0 && <WarehouseUnitPrint receiptLineId={receipt.line_id} unitCodes={receipt.unit_codes} />}
                </div>
              ))}
            </div>
          )}
          <button type="button" className={secondaryButton} disabled={busy || pending} onClick={nextCarton}>اسکن کارتن دیگر</button>
        </section>
      )}

      <section aria-labelledby="warehouse-unit-lookup-heading" className="space-y-2 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <h3 id="warehouse-unit-lookup-heading" className="text-sm font-extrabold text-[#261b41]">جست‌وجوی واحد</h3>
        <UnitLookup compact />
      </section>
    </div>
  );
}
