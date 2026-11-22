import React, { useRef, useState } from 'react';
import { ReturnBinSourceDto, wmsApi } from '../../services/wmsApi';
import { newUuid } from '../../services/productModel';

const fa = (n: number) => n.toLocaleString('fa-IR');
const message = (e: unknown) => e instanceof Error ? e.message : 'دریافت یا ثبت اطلاعات انجام نشد.';

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
  return <form data-item={source.returnItemId} onSubmit={submit} className="border border-slate-200 rounded-xl p-3 space-y-3 text-xs">
    <p className="font-bold">{source.productTitle} · {source.variantName || 'موجودی کالا'} {source.variantSpnc && `(${source.variantSpnc})`}</p>
    <p>باقی‌مانده: {fa(source.availableQuantity)} از {fa(source.quantity)} · کنترل کیفیت: {source.qcStatus === 'ok' ? 'سالم' : 'تأیید نشده'}</p>
    {!source.sourceProven ? <p className="text-amber-800">جانمایی به تطبیق منبع موجودی و مقصد مرجوعی نیاز دارد.</p> : !source.physical.verified ? <div className="space-y-2">
      <p>ابعاد و وزن همین قلم را اندازه‌گیری و تأیید کنید.</p>
      {['طول (میلی‌متر)', 'عرض (میلی‌متر)', 'ارتفاع (میلی‌متر)', 'وزن (گرم)'].map((label, i) => <label key={label} className="block">{label}<input aria-label={label} type="number" min="1" step="1" value={physical[i]} onChange={e => setPhysical(p => p.map((v, index) => index === i ? e.target.value : v))} className="border rounded p-2 w-full" /></label>)}
      <input aria-label="دلیل اندازه‌گیری" placeholder="دلیل اندازه‌گیری" value={measurementReason} onChange={e => setMeasurementReason(e.target.value)} className="border rounded p-2 w-full" />
      <button type="button" disabled={busy || !measurementReason.trim() || physical.some(v => !v || !/^\d+$/.test(v) || Number(v) <= 0)} onClick={measure} className="border rounded px-3 py-2 disabled:opacity-50">تأیید اندازه‌گیری</button>
    </div> : <>
      <p>اندازه‌گیری تأییدشده: {fa(source.physical.lengthMm)} × {fa(source.physical.widthMm)} × {fa(source.physical.heightMm)} میلی‌متر · {fa(source.physical.weightGrams)} گرم</p>
      <label className="block">قفسه در انبار مقصد<select aria-label="قفسه مرجوعی" value={bin} onChange={e => setBin(e.target.value)} className="border rounded p-2 w-full"><option value="">انتخاب قفسه</option>{source.bins.map(b => <option key={b.binCode} value={b.binCode}>{b.binCode}</option>)}</select></label>
      {source.bins.length === 0 && <p>قفسهٔ تأییدشده و قابل استفاده در مقصد موجود نیست.</p>}
      <label className="block">تعداد<input aria-label="تعداد جانمایی" inputMode="numeric" value={quantity} onChange={e => setQuantity(e.target.value)} className="border rounded p-2 w-full" /></label>
      <label className="block">دلیل<input aria-label="دلیل جانمایی" maxLength={2000} value={reason} onChange={e => setReason(e.target.value)} className="border rounded p-2 w-full" /></label>
      <button type="submit" disabled={busy || !valid} className="bg-indigo-600 text-white rounded px-3 py-2 disabled:opacity-50">{busy ? 'در حال ثبت…' : 'ثبت ورود به قفسه'}</button>
    </>}
    {saved && <p role="status" className="text-emerald-700">ورود به قفسه ثبت شد.</p>}
    {error && <p role="alert" className="text-red-700">{error}</p>}
    {error && command.current?.payload === fingerprint && <button data-retry type="button" disabled={busy} onClick={() => { if (command.current) void send(payload, command.current.key); }} className="border rounded px-3 py-2 disabled:opacity-50">بررسی و ارسال دوباره همان درخواست</button>}
  </form>;
};

export const CustomerReturnBinEntry: React.FC<{ returnID: string }> = ({ returnID }) => {
  const [sources, setSources] = useState<ReturnBinSourceDto[] | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(false);
  const refresh = async () => { const result = await wmsApi.getReturnBinSources(returnID); setSources(result); };
  const open = async () => { setLoading(true); setError(''); try { await refresh(); } catch (e) { setError(message(e)); } finally { setLoading(false); } };
  return <div className="space-y-3 pt-3">
    <button type="button" disabled={loading} onClick={open} className="border border-indigo-200 text-indigo-700 rounded-lg px-3 py-2 text-xs">{loading ? 'در حال دریافت…' : sources ? 'تازه‌سازی موجودی مرجوعی' : 'جانمایی مرجوعی در قفسه'}</button>
    {error && <p role="alert" className="text-xs text-red-700">{error}</p>}
    {sources?.map(s => <ReturnItemForm key={s.returnItemId} returnID={returnID} source={s} refresh={refresh} />)}
  </div>;
};
