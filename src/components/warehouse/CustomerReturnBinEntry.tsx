import React, { useRef, useState } from 'react';
import { Layers } from 'lucide-react';
import { ReturnBinSourceDto, wmsApi } from '../../services/wmsApi';
import { newUuid } from '../../services/productModel';

const fa = (n: number) => n.toLocaleString('fa-IR');
const message = (e: unknown) => e instanceof Error ? e.message : 'دریافت یا ثبت اطلاعات انجام نشد.';
const inputClass = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base focus-visible:outline-2 focus-visible:outline-brand sm:text-sm';
const primaryClass = 'min-h-11 w-full rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-brand-deep/90 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60 sm:w-auto';
const secondaryClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-brand-border bg-white px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60';

const ReturnItemForm: React.FC<{ returnID: string; source: ReturnBinSourceDto; refresh: () => Promise<void> }> = ({ returnID, source, refresh }) => {
  const [bin, setBin] = useState(''); const [quantity, setQuantity] = useState('1'); const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [saved, setSaved] = useState(false);
  const command = useRef<{ payload: string; key: string } | null>(null);
  const [physical, setPhysical] = useState(() => [source.physical.lengthMm, source.physical.widthMm, source.physical.heightMm, source.physical.weightGrams].map(v => String(v || '')));
  const [measurementReason, setMeasurementReason] = useState('');
  const q = Number(quantity);
  const selected = source.bins.find(b => b.binCode === bin);
  const volume = Math.ceil(source.physical.lengthMm * source.physical.widthMm * source.physical.heightMm * q / 1000);
  const valid = /^\d+$/.test(quantity) && Number.isSafeInteger(q) && q > 0 && q <= Math.min(source.availableQuantity, 2147483647)
    && reason.trim().length > 0 && reason.length <= 2000 && selected && selected.remainingVolumeCm3 >= volume && selected.remainingWeightGrams >= q * source.physical.weightGrams;
  const payload = { binCode: bin, quantity: q, reason: reason.trim() }; const fingerprint = JSON.stringify(payload);
  const send = async (body: typeof payload, key: string) => {
    setBusy(true); setError(''); setSaved(false);
    try { await wmsApi.placeReturnInBin(returnID, source.returnItemId, body, key); command.current = null; setReason(''); setSaved(true); await refresh(); }
    catch (e) { setError(message(e)); } finally { setBusy(false); }
  };
  const submit = async (event: React.FormEvent) => {
    event.preventDefault(); if (!valid || busy) return;
    if (command.current?.payload !== fingerprint) command.current = { payload: fingerprint, key: newUuid() };
    await send(payload, command.current.key);
  };
  const measure = async () => {
    const values = physical.map(Number);
    if (busy || !measurementReason.trim() || values.some((v, i) => !Number.isSafeInteger(v) || v <= 0 || v > (i === 3 ? 1000000000 : 1000000))) return;
    setBusy(true); setError('');
    try { await wmsApi.recordMeasurement(source.productId, { variantId: source.variantId || undefined, lengthMm: values[0], widthMm: values[1], heightMm: values[2], weightGrams: values[3], reason: measurementReason.trim() }); await refresh(); }
    catch (e) { setError(message(e)); } finally { setBusy(false); }
  };
  return <form data-item={source.returnItemId} onSubmit={submit} className="min-w-0 space-y-3 rounded-2xl border border-brand-border bg-brand-bg p-3 sm:p-4">
    <div className="space-y-1 text-sm text-slate-700">
      <p className="break-words font-extrabold text-slate-900">{source.productTitle} · {source.variantName || 'موجودی کالا'} {source.variantSpnc && <bdi className="font-mono">{source.variantSpnc}</bdi>}</p>
      <p>باقی‌مانده: {fa(source.availableQuantity)} از {fa(source.quantity)} · کنترل کیفیت: {source.qcStatus === 'ok' ? 'سالم' : 'تأیید نشده'}</p>
    </div>
    {!source.sourceProven ? <p className="rounded-xl bg-amber-50 p-3 text-sm text-amber-900">جانمایی به تطبیق منبع موجودی و مقصد مرجوعی نیاز دارد.</p> : !source.physical.verified ? <div className="space-y-3 text-sm text-slate-700">
      <p className="font-bold">ابعاد و وزن همین قلم را اندازه‌گیری و تأیید کنید.</p>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {['طول (میلی‌متر)', 'عرض (میلی‌متر)', 'ارتفاع (میلی‌متر)', 'وزن (گرم)'].map((label, i) => <label key={label} className="min-w-0 space-y-1">{label}
          <input aria-label={label} type="number" min="1" step="1" inputMode="numeric" dir="ltr" value={physical[i]}
            onChange={e => setPhysical(p => p.map((v, index) => index === i ? e.target.value : v))} className={inputClass} />
        </label>)}
      </div>
      <label className="block space-y-1">دلیل اندازه‌گیری
        <input aria-label="دلیل اندازه‌گیری" value={measurementReason} onChange={e => setMeasurementReason(e.target.value)} className={inputClass} />
      </label>
      <button type="button" disabled={busy || !measurementReason.trim() || physical.some(v => !v || !/^\d+$/.test(v) || Number(v) <= 0)}
        onClick={measure} className={primaryClass}>تأیید اندازه‌گیری</button>
    </div> : <>
      <div className="grid gap-2 text-sm text-slate-700 sm:grid-cols-2">
        <label className="min-w-0 space-y-1">قفسه در انبار مقصد
          <select aria-label="قفسه مرجوعی" dir="ltr" value={bin} onChange={e => setBin(e.target.value)} className={inputClass}>
            <option value="">انتخاب قفسه</option>{source.bins.map(b => <option key={b.binCode} value={b.binCode}>{b.binCode}</option>)}
          </select>
        </label>
        <label className="min-w-0 space-y-1">تعداد
          <input aria-label="تعداد جانمایی" dir="ltr" inputMode="numeric" value={quantity} onChange={e => setQuantity(e.target.value)} className={inputClass} />
        </label>
      </div>
      {source.bins.length === 0 && <p className="text-sm text-amber-900">قفسهٔ تأییدشده و قابل استفاده در مقصد موجود نیست.</p>}
      <label className="block space-y-1 text-sm text-slate-700">دلیل
        <input aria-label="دلیل جانمایی" maxLength={2000} value={reason} onChange={e => setReason(e.target.value)} className={inputClass} />
      </label>
      <button type="submit" disabled={busy || !valid} className={primaryClass}>{busy ? 'در حال ثبت…' : 'ثبت ورود به قفسه'}</button>
      <details>
        <summary className="min-h-11 cursor-pointer py-2.5 text-sm font-bold text-brand-deep">ابعاد و وزن تأییدشده</summary>
        <p className="text-sm text-slate-700">{fa(source.physical.lengthMm)} × {fa(source.physical.widthMm)} × {fa(source.physical.heightMm)} میلی‌متر · {fa(source.physical.weightGrams)} گرم</p>
      </details>
    </>}
    {saved && <p role="status" className="text-sm font-bold text-emerald-800">ورود به قفسه ثبت شد.</p>}
    {error && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{error}</p>}
    {error && command.current?.payload === fingerprint && <button data-retry type="button" disabled={busy} onClick={() => { if (command.current) void send(payload, command.current.key); }} className={secondaryClass}>بررسی و ارسال دوباره همان درخواست</button>}
  </form>;
};

export const CustomerReturnBinEntry: React.FC<{ returnID: string }> = ({ returnID }) => {
  const [sources, setSources] = useState<ReturnBinSourceDto[] | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(false);
  const refresh = async () => { const result = await wmsApi.getReturnBinSources(returnID); setSources(result); };
  const open = async () => { setLoading(true); setError(''); try { await refresh(); } catch (e) { setError(message(e)); } finally { setLoading(false); } };
  return <div className="space-y-3">
    <button type="button" disabled={loading} onClick={open} className={secondaryClass}>
      <Layers aria-hidden="true" className="size-4" />{loading ? 'در حال دریافت…' : sources ? 'تازه‌سازی موجودی مرجوعی' : 'جانمایی مرجوعی در قفسه'}
    </button>
    {error && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{error}</p>}
    {sources?.length === 0 && <p role="status" className="text-sm text-slate-700">قلمی برای جانمایی در دسترس نیست.</p>}
    {sources?.map(s => <ReturnItemForm key={s.returnItemId} returnID={returnID} source={s} refresh={refresh} />)}
  </div>;
};
