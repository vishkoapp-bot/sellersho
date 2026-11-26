import React, { useCallback, useEffect, useId, useState } from 'react';
import { CheckCircle2, ClipboardCheck, PackageCheck, RefreshCw, RotateCcw, Truck, XCircle } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { CarrierOptionDto, ReturnFinancialEventDto, ReturnParcelDto, SupplierReturnRequestDto, warehouseReturnsApi } from '../../services/supplierApi';
import { apiRequest } from '../../services/apiClient';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

const labels: Record<string, string> = {
  awaiting_return_test: 'در انتظار بررسی', approved: 'تأیید شده', rejected: 'رد شده', packed: 'بسته‌بندی شده',
  pickup_pending: 'آماده تحویل', postal_pending: 'آماده ارسال', in_transit: 'در حال انتقال',
  received_pending_qc: 'در انتظار QC', completed: 'تکمیل شده', qc_failed: 'مردود در QC', cancelled: 'لغو شده',
};

interface PriceReview {
  id: string;
  groupKey?: string;
  proposedPriceToman?: number;
  proposed_price_toman?: number;
  agreedPriceToman?: number | null;
  agreed_price_toman?: number | null;
  invoiceUrl?: string;
  invoice_url?: string;
  note?: string;
  status: string;
  decisionReason?: string;
  decision_reason?: string;
}

const priceOf = (review: PriceReview): number => {
  const value = review.proposedPriceToman ?? review.proposed_price_toman;
  return typeof value === 'number' ? value : Number(value || 0);
};

const agreedOf = (review: PriceReview): number | null => {
  const value = review.agreedPriceToman ?? review.agreed_price_toman;
  return value === null || value === undefined ? null : Number(value);
};

const invoiceOf = (review: PriceReview): string | undefined =>
  review.invoiceUrl || review.invoice_url || undefined;

const reasonOf = (review: PriceReview): string | undefined =>
  review.decisionReason || review.decision_reason || undefined;

const statusLabel: Record<string, string> = {
  pending: 'در انتظار بررسی',
  accepted: 'پذیرفته شد',
  rejected: 'رد شد',
};

export const WarehousePriceReviewSection: React.FC<{ returnId: string | null }> = ({ returnId }) => {
  const { success, error } = useToast();
  const [reviews, setReviews] = useState<PriceReview[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [agreed, setAgreed] = useState<Record<string, string>>({});
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [busyId, setBusyId] = useState<string | null>(null);

  const reload = useCallback(async () => {
    if (!returnId) return;
    setLoading(true);
    setLoadError('');
    try {
      const data = await apiRequest<PriceReview[] | { reviews?: PriceReview[] }>(
        `/warehouse/returns/${encodeURIComponent(returnId)}/price-review`,
        { method: 'GET', auth: true },
      );
      setReviews(Array.isArray(data) ? data : (data.reviews || []));
    } catch (cause) {
      setReviews([]);
      setLoadError(cause instanceof Error ? cause.message : 'دریافت بازبینی‌های قیمت انجام نشد.');
    } finally {
      setLoading(false);
    }
  }, [returnId]);

  useEffect(() => { void reload(); }, [reload]);

  const decide = async (review: PriceReview, kind: 'accept' | 'reject') => {
    if (!returnId) return;
    const reason = (reasons[review.id] || '').trim();
    let agreedPrice = 0;
    if (kind === 'accept') {
      const raw = (agreed[review.id] ?? String(priceOf(review))).trim();
      const parsed = Number(raw);
      if (!Number.isSafeInteger(parsed) || parsed <= 0) {
        error('مبلغ توافقی (تومان) را درست وارد کنید.');
        return;
      }
      agreedPrice = parsed;
    }
    setBusyId(review.id);
    try {
      if (kind === 'accept') {
        const body: { agreed_price_toman: number; reason?: string } = { agreed_price_toman: agreedPrice };
        if (reason) body.reason = reason;
        await apiRequest(
          `/warehouse/returns/${encodeURIComponent(returnId)}/price-review/${encodeURIComponent(review.id)}/accept`,
          { method: 'POST', auth: true, body: JSON.stringify(body) },
        );
        success(`بازبینی قیمت پذیرفته شد — مبلغ توافقی ${new Intl.NumberFormat('fa-IR').format(agreedPrice)} تومان در تسویه جایگزین قیمت تعرفه می‌شود.`);
      } else {
        const body: { reason?: string } = {};
        if (reason) body.reason = reason;
        await apiRequest(
          `/warehouse/returns/${encodeURIComponent(returnId)}/price-review/${encodeURIComponent(review.id)}/reject`,
          { method: 'POST', auth: true, body: JSON.stringify(body) },
        );
        success('درخواست بازبینی قیمت رد شد.');
      }
      setAgreed((current) => { const next = { ...current }; delete next[review.id]; return next; });
      setReasons((current) => { const next = { ...current }; delete next[review.id]; return next; });
      await reload();
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت تصمیم بازبینی قیمت انجام نشد.');
    } finally {
      setBusyId(null);
    }
  };

  if (!returnId) return null;

  return (
    <section className="space-y-3">
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {!loading && !loadError && reviews.length === 0 && (
        <p role="status" className="rounded-xl bg-brand-bg p-3 text-sm text-slate-700">برای این مرجوعی درخواست بازبینی قیمت ثبت نشده است.</p>
      )}
      {!loading && !loadError && reviews.length > 0 && (
        <div className="space-y-2.5">
          {reviews.map((review) => {
            const agreedPrice = agreedOf(review);
            const invoice = invoiceOf(review);
            const decisionReason = reasonOf(review);
            return (
              <div key={review.id} className="space-y-3 rounded-2xl border border-brand-border bg-brand-bg p-3 text-sm">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div>
                    {review.groupKey && <span className="text-xs font-black text-slate-800">گروه {review.groupKey}</span>}
                    {review.note && <span className="text-[11px] text-slate-500 block mt-0.5">یادداشت مشتری: {review.note}</span>}
                  </div>
                  <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full border shrink-0 ${review.status === 'pending' ? 'bg-amber-50 text-amber-700 border-amber-200' : review.status === 'accepted' ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-rose-50 text-rose-700 border-rose-200'}`}>
                    {statusLabel[review.status] || review.status}
                  </span>
                </div>
                <div className="space-y-2 rounded-xl border border-brand-border bg-white p-3 text-sm">
                  <div className="flex flex-wrap justify-between gap-2">
                    <span className="text-slate-500">مبلغ پیشنهادی مشتری (پرداختی در پست)</span>
                    <span className="font-bold text-slate-800">{new Intl.NumberFormat('fa-IR').format(priceOf(review))} تومان</span>
                  </div>
                  {agreedPrice !== null && (
                    <div className="flex flex-wrap justify-between gap-2">
                      <span className="text-slate-500">مبلغ توافقی</span>
                      <span className="font-bold text-slate-800">{new Intl.NumberFormat('fa-IR').format(agreedPrice)} تومان</span>
                    </div>
                  )}
                  {invoice ? (
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <span className="text-slate-500">فاکتور مبلغ پرداختی</span>
                      <a href={invoice} target="_blank" rel="noreferrer" className="font-bold text-blue-700 underline">مشاهدهٔ فاکتور</a>
                    </div>
                  ) : (
                    <p className="text-slate-700">برای توافق بالاتر از سقف تعرفه، فاکتور پرداخت لازم است.</p>
                  )}
                  {decisionReason && (
                    <p className="text-slate-500">دلیل تصمیم: <span className="text-slate-700">{decisionReason}</span></p>
                  )}
                </div>
                {review.status === 'pending' && (
                  <>
                    <label className="block">
                      <span className="text-[11px] font-bold text-slate-700 block mb-1">مبلغ توافقی (تومان)</span>
                      <input
                        type="text"
                        inputMode="numeric"
                        value={agreed[review.id] ?? String(priceOf(review))}
                        onChange={(e) => setAgreed((current) => ({ ...current, [review.id]: e.target.value }))}
                        className="min-h-11 w-full rounded-xl border border-slate-300 bg-white p-2.5 text-center font-mono text-sm font-bold"
                        dir="ltr"
                      />
                    </label>
                    <label className="block">
                      <span className="text-[11px] font-bold text-slate-700 block mb-1">دلیل تصمیم (اختیاری)</span>
                      <input
                        type="text"
                        value={reasons[review.id] ?? ''}
                        onChange={(e) => setReasons((current) => ({ ...current, [review.id]: e.target.value }))}
                        placeholder="مثلاً: مطابق فاکتور پیوست"
                        className="min-h-11 w-full rounded-xl border border-slate-300 bg-white p-2.5 text-sm"
                      />
                    </label>
                    <div className="grid grid-cols-2 gap-2">
                      <button
                        type="button"
                        disabled={busyId === review.id}
                        onClick={() => void decide(review, 'accept')}
                        className="min-h-11 rounded-xl bg-brand-deep px-3 py-2 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60"
                      >
                        <span>تأیید با مبلغ توافقی</span>
                      </button>
                      <button
                        type="button"
                        disabled={busyId === review.id}
                        onClick={() => void decide(review, 'reject')}
                        className="min-h-11 rounded-xl border border-rose-200 bg-white px-3 py-2 text-sm font-bold text-rose-800 focus-visible:ring-2 focus-visible:ring-rose-700 disabled:opacity-60"
                      >
                        <span>رد درخواست بازبینی</span>
                      </button>
                    </div>
                  </>
                )}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
};

type ParcelDraft = {
  quantity: string;
  carrierCode: string;
  paymentMethod: 'sender_pays' | 'receiver_pays';
  origin: string;
  destination: string;
  lengthMm: string;
  widthMm: string;
  heightMm: string;
  weightGrams: string;
  finalCharge: string;
  finalChargeSource: 'manual' | 'tariff';
  finalChargeReference: string;
};

const fa = new Intl.NumberFormat('fa-IR');
const inputClass = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base focus-visible:outline-2 focus-visible:outline-brand sm:text-sm';
const primaryClass = 'inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-brand-deep/90 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60 sm:w-auto';
const secondaryClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-brand-border bg-white px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60';

function ParcelFields({ value, carriers, update, disabled, quote, create }: {
  value: ParcelDraft;
  carriers: CarrierOptionDto[];
  update: (patch: Partial<ParcelDraft>) => void;
  disabled: boolean;
  quote: () => void;
  create: () => void;
}) {
  const activeCarriers = carriers.filter(carrier =>
    value.paymentMethod === 'sender_pays' ? carrier.supportsSenderPays : carrier.supportsReceiverPays);
  const fields = [
    ['quantity', 'تعداد اقلام'],
    ['lengthMm', 'طول (میلی‌متر)'],
    ['widthMm', 'عرض (میلی‌متر)'],
    ['heightMm', 'ارتفاع (میلی‌متر)'],
    ['weightGrams', 'وزن (گرم)'],
    ['finalCharge', 'مبلغ نهایی (تومان)'],
  ] as const;

  return (
    <form onSubmit={event => { event.preventDefault(); if (!disabled) create(); }} className="space-y-3 pt-2">
      <fieldset disabled={disabled} className="grid min-w-0 gap-3 text-sm text-slate-700 sm:grid-cols-2">
        <label className="min-w-0 space-y-1">مبدأ
          <input value={value.origin} required onChange={event => update({ origin: event.target.value })} className={inputClass} />
        </label>
        <label className="min-w-0 space-y-1">مقصد
          <input value={value.destination} required onChange={event => update({ destination: event.target.value })} className={inputClass} />
        </label>
        <label className="min-w-0 space-y-1">روش پرداخت
          <select value={value.paymentMethod} onChange={event => update({ paymentMethod: event.target.value as ParcelDraft['paymentMethod'] })} className={inputClass}>
            <option value="sender_pays">پرداخت فرستنده</option>
            <option value="receiver_pays">پس‌کرایه گیرنده</option>
          </select>
        </label>
        <label className="min-w-0 space-y-1">شرکت حمل
          <select value={value.carrierCode} onChange={event => update({ carrierCode: event.target.value })} className={inputClass}>
            {activeCarriers.map(carrier => <option key={carrier.code} value={carrier.code}>{carrier.displayName}</option>)}
          </select>
        </label>
        {fields.map(([key, label]) => (
          <label key={key} className="min-w-0 space-y-1">{label}
            <input type="number" min={key === 'finalCharge' ? 0 : 1} step="1" required={key !== 'finalCharge'}
              inputMode="numeric" dir="ltr" value={value[key]} onChange={event => update({ [key]: event.target.value })} className={inputClass} />
          </label>
        ))}
        <label className="min-w-0 space-y-1 sm:col-span-2">مرجع مبلغ
          <input value={value.finalChargeReference} onChange={event => update({ finalChargeReference: event.target.value })} className={inputClass} />
        </label>
      </fieldset>
      <div className="flex flex-wrap gap-2">
        <button type="submit" disabled={disabled} className={primaryClass}>ثبت مرسوله</button>
        <button type="button" disabled={disabled || !value.origin.trim() || !value.destination.trim() || Number(value.weightGrams) <= 0}
          onClick={quote} className={secondaryClass}>استعلام تعرفه</button>
      </div>
    </form>
  );
}

export function WarehouseReturnsTab() {
  const { success, error } = useToast();
  const priceReviewTitleId = useId();
  const [items, setItems] = useState<SupplierReturnRequestDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, string>>({});
  const [qc, setQc] = useState<Record<string, { accepted: string; failed: string }>>({});
  const [parcels, setParcels] = useState<Record<string, ReturnParcelDto[]>>({});
  const [parcelForm, setParcelForm] = useState<Record<string, ParcelDraft>>({});
  const [financialEvents, setFinancialEvents] = useState<Record<string, ReturnFinancialEventDto[]>>({});
  const [carriers, setCarriers] = useState<CarrierOptionDto[]>([]);
  const [priceReviewReturnId, setPriceReviewReturnId] = useState<string | null>(null);

  const reload = useCallback(async (showLoading = true) => {
    if (showLoading) setLoading(true);
    setLoadError('');
    try { setItems((await warehouseReturnsApi.list()).returns || []); }
    catch (cause) { setLoadError(cause instanceof Error ? cause.message : 'صف مرجوعی‌ها دریافت نشد.'); }
    finally { if (showLoading) setLoading(false); }
  }, []);
  const loadParcels = useCallback(async (id: string) => {
    try {
      const [response, finance] = await Promise.all([warehouseReturnsApi.getParcels(id), warehouseReturnsApi.getFinancialEvents(id)]);
      setParcels(current => ({ ...current, [id]: response.parcels || [] }));
      setFinancialEvents(current => ({ ...current, [id]: finance.events || [] }));
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'مرسوله‌ها دریافت نشدند.');
    }
  }, [error]);

  useEffect(() => { void reload(); }, [reload]);
  useEffect(() => { void warehouseReturnsApi.getCarrierOptions().then(response => setCarriers(response.carriers || [])).catch(() => undefined); }, []);
  useEffect(() => {
    items.filter(item => ['approved', 'packed', 'pickup_pending', 'postal_pending', 'in_transit', 'received_pending_qc'].includes(item.status))
      .forEach(item => { void loadParcels(item.id); });
  }, [items, loadParcels]);

  const run = async (id: string, operation: () => Promise<unknown>, message: string) => {
    if (busy) return;
    setBusy(id);
    try { await operation(); success(message); await reload(false); }
    catch (cause) { error(cause instanceof Error ? cause.message : 'عملیات انجام نشد.'); }
    finally { setBusy(null); }
  };

  const parcelDraft = (item: SupplierReturnRequestDto): ParcelDraft => parcelForm[item.id] || {
    quantity: '1', carrierCode: 'tipax', paymentMethod: 'sender_pays',
    origin: item.warehouseName || 'انبار مرکزی تهران', destination: '',
    lengthMm: '200', widthMm: '150', heightMm: '100', weightGrams: '1000',
    finalCharge: '', finalChargeSource: 'manual', finalChargeReference: '',
  };

  const createParcel = async (item: SupplierReturnRequestDto) => {
    const value = parcelDraft(item);
    const [quantity, lengthMm, widthMm, heightMm, weightGrams] = [value.quantity, value.lengthMm, value.widthMm, value.heightMm, value.weightGrams].map(Number);
    if (!value.origin.trim() || !value.destination.trim() || [quantity, lengthMm, widthMm, heightMm, weightGrams].some(number => !Number.isSafeInteger(number) || number <= 0)) {
      error('تعداد، مبدأ، مقصد، ابعاد و وزن را بررسی کنید.');
      return;
    }
    const finalCharge = value.finalCharge ? Number(value.finalCharge) : undefined;
    if (value.paymentMethod === 'sender_pays' && (!finalCharge || finalCharge <= 0)) {
      error('برای پرداخت فرستنده مبلغ نهایی الزامی است.');
      return;
    }
    await run(item.id, async () => {
      await warehouseReturnsApi.createParcel(item.id, {
        quantity, carrierCode: value.carrierCode, paymentMethod: value.paymentMethod, origin: value.origin,
        destination: value.destination, lengthMm, widthMm, heightMm, weightGrams, finalCharge,
        finalChargeSource: value.finalChargeSource, finalChargeReference: value.finalChargeReference,
      });
      await loadParcels(item.id);
    }, 'مرسوله ثبت شد.');
  };

  const quoteParcel = async (item: SupplierReturnRequestDto) => {
    if (busy) return;
    setBusy(item.id);
    const value = parcelDraft(item);
    try {
      const quote = await warehouseReturnsApi.quote({
        carrierCode: value.carrierCode, originRegion: value.origin, destinationRegion: value.destination,
        sizeClass: 'size-4', weightGrams: Number(value.weightGrams),
      });
      setParcelForm(current => ({ ...current, [item.id]: { ...value, finalCharge: String(quote.charge), finalChargeSource: 'tariff' } }));
      success(`برآورد تعرفه نسخه ${quote.tariffVersion} ثبت شد.`);
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'تعرفه فعال برای این مسیر پیدا نشد.');
    } finally {
      setBusy(null);
    }
  };

  const action = (item: SupplierReturnRequestDto) => {
    const note = notes[item.id] || '';
    const noteField = <label className="block space-y-1 text-sm font-medium text-slate-700">یادداشت عملیات
      <input aria-label={`یادداشت ${item.id}`} value={note} onChange={event => setNotes(current => ({ ...current, [item.id]: event.target.value }))} className={inputClass} />
    </label>;
    if (item.status === 'awaiting_return_test') return (
      <div className="space-y-3">
        {noteField}
        <div className="flex flex-wrap gap-2">
          <button type="button" disabled={busy !== null} className={primaryClass}
            onClick={() => void run(item.id, () => warehouseReturnsApi.review(item.id, 'approved', note || 'تأیید بررسی اولیه'), 'درخواست تأیید شد.')}>
            <CheckCircle2 aria-hidden="true" className="size-4" />تأیید درخواست
          </button>
          <button type="button" disabled={busy !== null} className={secondaryClass}
            onClick={() => void run(item.id, () => warehouseReturnsApi.review(item.id, 'rejected', note || 'رد بررسی اولیه'), 'درخواست رد و رزرو آزاد شد.')}>
            <XCircle aria-hidden="true" className="size-4 text-rose-700" />رد درخواست
          </button>
        </div>
      </div>
    );
    if (item.status === 'approved') return (
      <details className="rounded-xl border border-brand-border bg-brand-bg px-3">
        <summary className="min-h-11 cursor-pointer py-2.5 text-sm font-extrabold text-brand-deep">ثبت مرسوله برگشت</summary>
        <div className="pb-3">
          <ParcelFields value={parcelDraft(item)} carriers={carriers} disabled={busy !== null}
            update={patch => setParcelForm(current => ({ ...current, [item.id]: { ...parcelDraft(item), ...patch } }))}
            quote={() => void quoteParcel(item)} create={() => void createParcel(item)} />
        </div>
      </details>
    );
    if (['packed', 'pickup_pending', 'postal_pending'].includes(item.status)) return (
      <button type="button" disabled={busy !== null} className={primaryClass}
        onClick={() => void run(item.id, () => warehouseReturnsApi.handover(item.id), 'تحویل فیزیکی ثبت شد.')}>
        <Truck aria-hidden="true" className="size-4" />ثبت تحویل
      </button>
    );
    if (item.status === 'in_transit') return (
      <button type="button" disabled={busy !== null} className={primaryClass}
        onClick={() => void run(item.id, () => warehouseReturnsApi.receive(item.id), 'دریافت ثبت و کالا قرنطینه شد.')}>
        <PackageCheck aria-hidden="true" className="size-4" />ثبت دریافت
      </button>
    );
    if (item.status === 'received_pending_qc') {
      const value = qc[item.id] || { accepted: String(item.requestedQuantity), failed: '0' };
      return (
        <form className="space-y-3" onSubmit={event => {
          event.preventDefault();
          void run(item.id, () => warehouseReturnsApi.inspect(item.id, Number(value.accepted), Number(value.failed), note || 'نتیجه QC ثبت شد'), 'نتیجه QC ثبت شد.');
        }}>
          <div className="grid grid-cols-2 gap-2 text-sm text-slate-700">
            <label className="min-w-0 space-y-1">تعداد سالم
              <input type="number" min="0" step="1" required value={value.accepted} className={inputClass}
                onChange={event => setQc(current => ({ ...current, [item.id]: { ...value, accepted: event.target.value } }))} />
            </label>
            <label className="min-w-0 space-y-1">تعداد مردود
              <input type="number" min="0" step="1" required value={value.failed} className={inputClass}
                onChange={event => setQc(current => ({ ...current, [item.id]: { ...value, failed: event.target.value } }))} />
            </label>
          </div>
          {noteField}
          <button type="submit" disabled={busy !== null} className={primaryClass}>
            <ClipboardCheck aria-hidden="true" className="size-4" />ثبت نتیجه کنترل کیفیت
          </button>
        </form>
      );
    }
    return null;
  };

  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="مرجوعی تأمین‌کنندگان" description="درخواست‌ها، تحویل مرسوله و نتیجه کنترل کیفیت را ثبت کنید." icon={<RotateCcw />}
        action={<button type="button" onClick={() => void reload()} disabled={loading || busy !== null} className={secondaryClass}>
          <RefreshCw aria-hidden="true" className={`size-4 ${loading ? 'motion-safe:animate-spin' : ''}`} />به‌روزرسانی
        </button>} />
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {!loading && !loadError && items.length === 0 && <SupplierEmptyState icon={<RotateCcw />} title="درخواست مرجوعی فعالی وجود ندارد." />}
      {!loading && !loadError && items.map(item => (
        <section key={item.id} className="min-w-0 space-y-3 rounded-[22px] border border-brand-border bg-white p-4 shadow-sm sm:p-5">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <h3 className="break-words text-base font-extrabold text-slate-900">{item.productTitle}</h3>
              <p className="text-sm text-slate-600">{fa.format(item.requestedQuantity)} عدد{item.warehouseName && ` · ${item.warehouseName}`}</p>
            </div>
            <span className="rounded-full bg-brand-soft px-3 py-1 text-xs font-bold text-brand-deep">{labels[item.status] || item.status}</span>
          </div>
          {action(item)}
          {(parcels[item.id] || []).filter(parcel => parcel.status === 'draft').map(parcel => (
            <div key={parcel.id} className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-brand-bg p-3 text-sm text-slate-700">
              <p>مرسوله {fa.format(parcel.parcelNumber)}{parcel.finalCharge != null && ` · ${fa.format(parcel.finalCharge)} تومان`}</p>
              <button type="button" disabled={busy !== null} className={secondaryClass}
                onClick={() => void run(item.id, async () => {
                  await warehouseReturnsApi.confirmCharge(item.id, parcel.id);
                  await loadParcels(item.id);
                }, 'اثر مالی مرسوله تأیید شد.')}>تأیید مبلغ و اثر مالی</button>
            </div>
          ))}
          <details className="border-t border-brand-border">
            <summary className="min-h-11 cursor-pointer py-2.5 text-sm font-bold text-brand-deep">جزئیات مرجوعی و مرسوله‌ها</summary>
            <div className="space-y-2 text-sm text-slate-700">
              <p>شناسه <bdi className="break-all font-mono">{item.id}</bdi></p>
              <p>{item.collectionMethod === 'postal' ? 'ارسال با شرکت حمل' : 'تحویل توسط تأمین‌کننده'}</p>
              {item.reason && <p className="break-words">علت {item.reason}</p>}
              {(parcels[item.id] || []).map(parcel => (
                <div key={parcel.id} className="space-y-1 rounded-xl border border-brand-border p-3">
                  <p>مرسوله {fa.format(parcel.parcelNumber)} · {labels[parcel.status] || parcel.status} · {parcel.paymentMethod === 'sender_pays' ? 'پرداخت فرستنده' : 'پس‌کرایه گیرنده'}</p>
                  <p>{parcel.origin} ← {parcel.destination} · {fa.format(parcel.weightGrams)} گرم · {fa.format(parcel.lengthMm)} × {fa.format(parcel.widthMm)} × {fa.format(parcel.heightMm)} میلی‌متر</p>
                  {parcel.finalCharge != null && <p>مبلغ نهایی {fa.format(parcel.finalCharge)} تومان</p>}
                  {(financialEvents[item.id] || []).filter(event => event.parcelId === parcel.id).map(event => <p key={event.id} className="text-emerald-800">
                    <bdi>{event.operation}</bdi> · {fa.format(event.amount)} تومان{event.ledgerEntryId && <> · سند <bdi className="break-all font-mono">{event.ledgerEntryId}</bdi></>}
                  </p>)}
                </div>
              ))}
              <button type="button" onClick={() => setPriceReviewReturnId(item.id)} className={secondaryClass}>بازبینی قیمت مرجوعی</button>
            </div>
          </details>
        </section>
      ))}
      {priceReviewReturnId && <SupplierDialog titleId={priceReviewTitleId} onDismiss={() => setPriceReviewReturnId(null)} wide>
        <SupplierDialogHeading id={priceReviewTitleId} title="بازبینی قیمت مرجوعی" onClose={() => setPriceReviewReturnId(null)} />
        <WarehousePriceReviewSection key={priceReviewReturnId} returnId={priceReviewReturnId} />
      </SupplierDialog>}
    </div>
  );
}
