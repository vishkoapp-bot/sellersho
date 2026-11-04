import React, { useCallback, useEffect, useState } from 'react';
import { CheckCircle2, ClipboardCheck, PackageCheck, RefreshCw, Truck, XCircle } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { CarrierOptionDto, ReturnFinancialEventDto, ReturnParcelDto, SupplierReturnRequestDto, warehouseReturnsApi } from '../../services/supplierApi';
import { apiRequest } from '../../services/apiClient';

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
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [backendInactive, setBackendInactive] = useState(false);
  const [agreed, setAgreed] = useState<Record<string, string>>({});
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [busyId, setBusyId] = useState<string | null>(null);

  const reload = useCallback(async () => {
    if (!returnId) { setReviews([]); setLoaded(false); setBackendInactive(false); return; }
    setLoading(true);
    setBackendInactive(false);
    try {
      const data = await apiRequest<PriceReview[] | { reviews?: PriceReview[] }>(
        `/warehouse/returns/${encodeURIComponent(returnId)}/price-review`,
        { method: 'GET', auth: true },
      );
      setReviews(Array.isArray(data) ? data : (data.reviews || []));
      setLoaded(true);
    } catch (cause) {
      setReviews([]);
      setLoaded(true);
      setBackendInactive(true);
      error(cause instanceof Error ? cause.message : 'دریافت بازبینی‌های قیمت انجام نشد.');
    } finally {
      setLoading(false);
    }
  }, [returnId, error]);

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

  if (!returnId) {
    return (
      <section className="rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs space-y-2">
        <h4 className="text-xs font-extrabold text-slate-800">بازبینی قیمت مرجوعی</h4>
        <p className="text-[11px] leading-relaxed text-slate-500">
          بازبینی قیمت را تو — انباردار — بررسی می‌کنی: مرسوله دست توست و مبلغ واقعی پرداختی در پست با فاکتور قابل راستی‌آزمایی است. اول یک مرجوعی از فهرست بالا انتخاب کن تا درخواست‌های بازبینی آن اینجا بیاید.
        </p>
      </section>
    );
  }

  return (
    <section className="rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs space-y-2.5">
      <div className="flex items-center justify-between">
        <h4 className="text-xs font-extrabold text-slate-800">بازبینی قیمت مرجوعی — تصمیم انباردار</h4>
        <button
          type="button"
          onClick={() => void reload()}
          disabled={loading}
          className="p-1.5 rounded-xl border border-slate-200 text-slate-600 disabled:opacity-50"
          title="به‌روزرسانی"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} strokeWidth={1.5} />
        </button>
      </div>
      <p className="text-[11px] leading-relaxed text-slate-500">
        مشتری قیمت تعرفه (سقف تعرفهٔ نرخنامهٔ پستی) را با مبلغی که واقعاً در پست پرداخته مقایسه می‌کند؛ اگر متفاوت بود، درخواست بازبینی می‌فرستد. با پذیرش، مبلغ توافقی جایگزین قیمت تعرفهٔ همان گروه در تسویه می‌شود. توافق بالاتر از سقف تعرفه فقط با فاکتور پیوست‌شده ممکن است.
      </p>
      {loading && !loaded ? (
        <p className="text-[11px] text-slate-400 text-center py-2">در حال دریافت بازبینی‌های قیمت…</p>
      ) : backendInactive ? (
        <p className="text-[11px] leading-relaxed text-amber-700 bg-amber-50 border border-amber-200 rounded-xl p-2.5">
          بخش بازبینی قیمت هنوز در بک‌اند فعال نشده است؛ پس از فعال‌شدن، درخواست‌های این مرجوعی اینجا نمایش داده می‌شوند.
        </p>
      ) : reviews.length === 0 ? (
        <p className="text-[11px] text-slate-400 text-center py-2">برای این مرجوعی درخواست بازبینی قیمتی ثبت نشده است.</p>
      ) : (
        <div className="space-y-2.5">
          {reviews.map((review) => {
            const agreedPrice = agreedOf(review);
            const invoice = invoiceOf(review);
            const decisionReason = reasonOf(review);
            return (
              <div key={review.id} className="rounded-xl border border-slate-200 p-3 space-y-2 text-xs bg-slate-50/50">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    {review.groupKey && <span className="text-xs font-black text-slate-800">گروه {review.groupKey}</span>}
                    {review.note && <span className="text-[11px] text-slate-500 block mt-0.5">یادداشت مشتری: {review.note}</span>}
                  </div>
                  <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full border shrink-0 ${review.status === 'pending' ? 'bg-amber-50 text-amber-700 border-amber-200' : review.status === 'accepted' ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-rose-50 text-rose-700 border-rose-200'}`}>
                    {statusLabel[review.status] || review.status}
                  </span>
                </div>
                <div className="rounded-xl bg-white border border-slate-200 p-2.5 text-[11px] space-y-1">
                  <div className="flex justify-between">
                    <span className="text-slate-500">مبلغ پیشنهادی مشتری (پرداختی در پست)</span>
                    <span className="font-bold text-slate-800">{new Intl.NumberFormat('fa-IR').format(priceOf(review))} تومان</span>
                  </div>
                  {agreedPrice !== null && (
                    <div className="flex justify-between">
                      <span className="text-slate-500">مبلغ توافقی</span>
                      <span className="font-bold text-slate-800">{new Intl.NumberFormat('fa-IR').format(agreedPrice)} تومان</span>
                    </div>
                  )}
                  {invoice ? (
                    <div className="flex justify-between items-center">
                      <span className="text-slate-500">فاکتور مبلغ پرداختی</span>
                      <a href={invoice} target="_blank" rel="noreferrer" className="font-bold text-blue-700 underline">مشاهدهٔ فاکتور</a>
                    </div>
                  ) : (
                    <p className="text-slate-400">فاکتور پیوست نشده است — توافق بالاتر از سقف تعرفه بدون فاکتور ممکن نیست.</p>
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
                        className="w-full rounded-xl border border-slate-200 bg-white p-2.5 text-sm font-mono font-bold text-center"
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
                        className="w-full rounded-xl border border-slate-200 bg-white p-2.5 text-xs"
                      />
                    </label>
                    <div className="grid grid-cols-2 gap-2">
                      <button
                        type="button"
                        disabled={busyId === review.id}
                        onClick={() => void decide(review, 'accept')}
                        className="flex items-center justify-center gap-1.5 min-h-[44px] px-4 rounded-xl bg-blue-700 text-white text-xs font-bold hover:bg-blue-800 disabled:opacity-50"
                      >
                        <span>تأیید با مبلغ توافقی</span>
                      </button>
                      <button
                        type="button"
                        disabled={busyId === review.id}
                        onClick={() => void decide(review, 'reject')}
                        className="flex items-center justify-center gap-1.5 min-h-[44px] px-4 rounded-xl bg-rose-600 text-white text-xs font-bold hover:bg-rose-700 disabled:opacity-50"
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

export const WarehouseReturnsTab: React.FC = () => {
  const { success, error } = useToast();
  const [items, setItems] = useState<SupplierReturnRequestDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, string>>({});
  const [qc, setQc] = useState<Record<string, { accepted: string; failed: string }>>({});
  const [parcels, setParcels] = useState<Record<string, ReturnParcelDto[]>>({});
  const [parcelForm, setParcelForm] = useState<Record<string, { quantity:string; carrierCode:string; paymentMethod:'sender_pays'|'receiver_pays'; origin:string; destination:string; lengthMm:string; widthMm:string; heightMm:string; weightGrams:string; finalCharge:string; finalChargeSource:'manual'|'tariff'; finalChargeReference:string }>>({});
  const [quotes, setQuotes] = useState<Record<string, number>>({});
  const [financialEvents, setFinancialEvents] = useState<Record<string, ReturnFinancialEventDto[]>>({});
  const [carriers, setCarriers] = useState<CarrierOptionDto[]>([]);
  const [priceReviewReturnId, setPriceReviewReturnId] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    try { setItems((await warehouseReturnsApi.list()).returns || []); }
    catch (cause) { error(cause instanceof Error ? cause.message : 'صف مرجوعی‌ها دریافت نشد.'); }
    finally { setLoading(false); }
  }, [error]);

  useEffect(() => { void reload(); void warehouseReturnsApi.getCarrierOptions().then((response) => setCarriers(response.carriers || [])).catch(() => undefined); }, [reload]);
  useEffect(() => { items.filter((item) => ['approved','packed','in_transit','received_pending_qc'].includes(item.status)).forEach((item) => { void loadParcels(item.id); }); }, [items]);

  const run = async (id: string, operation: () => Promise<unknown>, message: string) => {
    setBusy(id);
    try { await operation(); success(message); await reload(); }
    catch (cause) { error(cause instanceof Error ? cause.message : 'عملیات انجام نشد.'); }
    finally { setBusy(null); }
  };

  const loadParcels = async (id: string) => { try { const [response, finance] = await Promise.all([warehouseReturnsApi.getParcels(id), warehouseReturnsApi.getFinancialEvents(id)]); setParcels((current) => ({ ...current, [id]: response.parcels || [] })); setFinancialEvents((current) => ({ ...current, [id]: finance.events || [] })); } catch (cause) { error(cause instanceof Error ? cause.message : 'مرسوله‌ها دریافت نشدند.'); } };
  const parcelDefaults = (id: string) => parcelForm[id] || { quantity:'1', carrierCode:'tipax', paymentMethod:'sender_pays' as const, origin:'انبار مرکزی تهران', destination:'', lengthMm:'200', widthMm:'150', heightMm:'100', weightGrams:'1000', finalCharge:'', finalChargeSource:'manual' as const, finalChargeReference:'' };
  const createParcel = async (item: SupplierReturnRequestDto) => { const value = parcelDefaults(item.id); if (!value.destination.trim() || Number(value.quantity) <= 0 || Number(value.lengthMm) <= 0 || Number(value.widthMm) <= 0 || Number(value.heightMm) <= 0 || Number(value.weightGrams) <= 0) { error('تعداد، مقصد، ابعاد و وزن باید معتبر باشند.'); return; } const finalCharge = value.finalCharge ? Number(value.finalCharge) : undefined; if (value.paymentMethod === 'sender_pays' && (!finalCharge || finalCharge <= 0)) { error('برای پرداخت فرستنده مبلغ نهایی الزامی است.'); return; } await run(item.id, async () => { await warehouseReturnsApi.createParcel(item.id, { quantity:Number(value.quantity), carrierCode:value.carrierCode, paymentMethod:value.paymentMethod, origin:value.origin, destination:value.destination, lengthMm:Number(value.lengthMm), widthMm:Number(value.widthMm), heightMm:Number(value.heightMm), weightGrams:Number(value.weightGrams), finalCharge, finalChargeSource: value.finalChargeSource, finalChargeReference:value.finalChargeReference }); await loadParcels(item.id); }, 'مرسوله ثبت شد.'); };
  const quoteParcel = async (item: SupplierReturnRequestDto) => { const value = parcelDefaults(item.id); try { const quote = await warehouseReturnsApi.quote({ carrierCode:value.carrierCode, originRegion:value.origin, destinationRegion:value.destination, sizeClass:'size-4', weightGrams:Number(value.weightGrams) }); setQuotes((current) => ({ ...current, [item.id]: quote.charge })); setParcelForm((current) => ({ ...current, [item.id]: { ...value, finalCharge:String(quote.charge), finalChargeSource:'tariff' } })); success(`برآورد تعرفه نسخه ${quote.tariffVersion} ثبت شد.`); } catch (cause) { error(cause instanceof Error ? cause.message : 'تعرفه فعال برای این مسیر پیدا نشد.'); } };

  const action = (item: SupplierReturnRequestDto) => {
    const note = notes[item.id] || '';
    if (item.status === 'awaiting_return_test') return <div className="grid grid-cols-2 gap-2"><button disabled={busy === item.id} onClick={() => run(item.id, () => warehouseReturnsApi.review(item.id, 'approved', note || 'تأیید بررسی اولیه'), 'درخواست تأیید شد.')} className="flex items-center justify-center gap-1 rounded-xl bg-emerald-600 py-2 text-[10px] font-bold text-white disabled:opacity-50"><CheckCircle2 className="h-3.5 w-3.5"  strokeWidth={1.5}/>تأیید</button><button disabled={busy === item.id} onClick={() => run(item.id, () => warehouseReturnsApi.review(item.id, 'rejected', note || 'رد بررسی اولیه'), 'درخواست رد و رزرو آزاد شد.')} className="flex items-center justify-center gap-1 rounded-xl bg-rose-600 py-2 text-[10px] font-bold text-white disabled:opacity-50"><XCircle className="h-3.5 w-3.5"  strokeWidth={1.5}/>رد</button></div>;
    if (item.status === 'approved') { const value = parcelDefaults(item.id); const activeCarriers = carriers.filter((carrier) => value.paymentMethod === 'sender_pays' ? carrier.supportsSenderPays : carrier.supportsReceiverPays); return <div className="space-y-2"><div className="grid grid-cols-2 gap-2"><input aria-label="تعداد parcel" type="number" min="1" value={value.quantity} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,quantity:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="تعداد"/><select aria-label="شرکت حمل" value={value.carrierCode} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,carrierCode:event.target.value}}))} className="rounded-lg border p-2 text-xs">{activeCarriers.map((carrier)=><option key={carrier.code} value={carrier.code}>{carrier.displayName}</option>)}</select><select aria-label="روش پرداخت" value={value.paymentMethod} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,paymentMethod:event.target.value as 'sender_pays'|'receiver_pays'}}))} className="rounded-lg border p-2 text-xs"><option value="sender_pays">پرداخت فرستنده</option><option value="receiver_pays">پس‌کرایه گیرنده</option></select><input aria-label="مقصد" value={value.destination} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,destination:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="مقصد واقعی"/></div><div className="grid grid-cols-4 gap-1"><input aria-label="طول" value={value.lengthMm} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,lengthMm:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="طول mm"/><input aria-label="عرض" value={value.widthMm} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,widthMm:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="عرض"/><input aria-label="ارتفاع" value={value.heightMm} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,heightMm:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="ارتفاع"/><input aria-label="وزن" value={value.weightGrams} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,weightGrams:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="وزن g"/></div><div className="grid grid-cols-2 gap-2"><input aria-label="مبلغ نهایی" value={value.finalCharge} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,finalCharge:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="مبلغ نهایی"/><input aria-label="مرجع مبلغ" value={value.finalChargeReference} onChange={(event)=>setParcelForm((current)=>({...current,[item.id]:{...value,finalChargeReference:event.target.value}}))} className="rounded-lg border p-2 text-xs" placeholder="مرجع استعلام دستی"/></div><div className="grid grid-cols-2 gap-2"><button disabled={busy === item.id} onClick={() => void quoteParcel(item)} className="rounded-xl bg-indigo-50 py-2 text-[10px] font-bold text-indigo-700 disabled:opacity-50">استعلام تعرفه</button><button disabled={busy === item.id} onClick={() => void createParcel(item)} className="rounded-xl bg-amber-600 py-2 text-[10px] font-bold text-white disabled:opacity-50">ثبت مرسوله</button></div></div>; }
    if (['packed', 'pickup_pending', 'postal_pending'].includes(item.status)) return <button disabled={busy === item.id} onClick={() => run(item.id, () => warehouseReturnsApi.handover(item.id), 'تحویل فیزیکی ثبت شد.')} className="flex w-full items-center justify-center gap-1 rounded-xl bg-blue-600 py-2 text-[10px] font-bold text-white disabled:opacity-50"><Truck className="h-3.5 w-3.5"  strokeWidth={1.5}/>ثبت تحویل</button>;
    if (item.status === 'in_transit') return <button disabled={busy === item.id} onClick={() => run(item.id, () => warehouseReturnsApi.receive(item.id), 'دریافت ثبت و کالا قرنطینه شد.')} className="flex w-full items-center justify-center gap-1 rounded-xl bg-indigo-600 py-2 text-[10px] font-bold text-white disabled:opacity-50"><PackageCheck className="h-3.5 w-3.5"  strokeWidth={1.5}/>ثبت دریافت</button>;
    if (item.status === 'received_pending_qc') {
      const value = qc[item.id] || { accepted: String(item.requestedQuantity), failed: '0' };
      return <div className="space-y-2"><div className="grid grid-cols-2 gap-2"><input aria-label="تعداد سالم" type="number" min="0" value={value.accepted} onChange={(event) => setQc((current) => ({ ...current, [item.id]: { ...value, accepted: event.target.value } }))} className="rounded-lg border p-2 text-xs" placeholder="سالم" /><input aria-label="تعداد مردود" type="number" min="0" value={value.failed} onChange={(event) => setQc((current) => ({ ...current, [item.id]: { ...value, failed: event.target.value } }))} className="rounded-lg border p-2 text-xs" placeholder="مردود" /></div><button disabled={busy === item.id} onClick={() => run(item.id, () => warehouseReturnsApi.inspect(item.id, Number(value.accepted), Number(value.failed), note || 'نتیجه QC ثبت شد'), 'نتیجه QC ثبت شد.')} className="flex w-full items-center justify-center gap-1 rounded-xl bg-amber-600 py-2 text-[10px] font-bold text-white disabled:opacity-50"><ClipboardCheck className="h-3.5 w-3.5"  strokeWidth={1.5}/>ثبت نتیجه QC</button></div>;
    }
    return null;
  };

  return <div className="space-y-3.5 pb-6"><div className="flex items-center justify-between rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs"><div><h3 className="text-xs font-bold text-slate-800">مرجوعی‌های تأمین‌کنندگان</h3><p className="mt-1 text-[10px] leading-relaxed text-slate-500">صف عملیاتی متصل به درخواست‌های واقعی، رزرو موجودی، handover و QC.</p></div><button onClick={() => void reload()} disabled={loading} className="rounded-xl border border-slate-200 p-2 text-slate-600 disabled:opacity-50" title="به‌روزرسانی"><RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`}  strokeWidth={1.5}/></button></div>{loading ? <div className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">در حال دریافت صف...</div> : items.length === 0 ? <div className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">درخواست مرجوعی فعالی وجود ندارد.</div> : <div className="space-y-3">{items.map((item) => <section key={item.id} className="space-y-2.5 rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs"><div className="flex items-start justify-between gap-3 border-b border-slate-100 pb-2"><div><span className="font-mono text-xs font-black text-slate-800">{item.id}</span><span className="mt-0.5 block text-[10px] text-slate-400">کالا: {item.productTitle} · تعداد: {item.requestedQuantity}</span></div><span className="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-bold text-amber-700">{labels[item.status] || item.status}</span></div><div className="rounded-xl bg-slate-50 p-2 text-[10px] text-slate-600"><p>روش: {item.collectionMethod === 'postal' ? 'ارسال با شرکت حمل' : 'تحویل توسط تأمین‌کننده'}</p><p className="mt-1 font-bold text-slate-800">علت: {item.reason}</p></div><input aria-label={`یادداشت ${item.id}`} value={notes[item.id] || ''} onChange={(event) => setNotes((current) => ({ ...current, [item.id]: event.target.value }))} placeholder="یادداشت عملیات / علت تصمیم" className="w-full rounded-lg border p-2 text-xs" />{action(item)}<button onClick={() => setPriceReviewReturnId(item.id)} className="w-full rounded-xl bg-indigo-50 py-2 text-[10px] font-bold text-indigo-700">بازبینی قیمت مرجوعی</button>{(parcels[item.id] || []).map((parcel) => <div key={parcel.id} className="space-y-1 rounded-xl border border-slate-200 p-2 text-[10px] text-slate-600"><p>مرسوله {parcel.parcelNumber} · {parcel.status} · {parcel.paymentMethod} · مبلغ نهایی: {parcel.finalCharge || 'ثبت نشده'}</p><p>{parcel.origin} ← {parcel.destination} · {parcel.weightGrams}g · {parcel.lengthMm}×{parcel.widthMm}×{parcel.heightMm}mm</p>{parcel.status === 'draft' && <button disabled={busy === item.id} onClick={() => run(item.id, () => warehouseReturnsApi.confirmCharge(item.id, parcel.id), 'اثر مالی مرسوله تأیید شد.')} className="rounded-lg bg-emerald-600 px-3 py-1.5 font-bold text-white disabled:opacity-50">تأیید مبلغ و اثر مالی</button>}{(financialEvents[item.id] || []).filter((event) => event.parcelId === parcel.id).map((event) => <p key={event.id} className="text-[9px] text-emerald-700">{event.operation} · {event.amount} · ledger: {event.ledgerEntryId || 'بدون debit'}</p>)}</div>)}</section>)}</div>}<WarehousePriceReviewSection returnId={priceReviewReturnId} /></div>;
};