import React, { useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';

// FIX 45: rebuilt from the live bundle SupplierDashboardView-MERGED.js (tn component).
// Live's tab: h2 «ثبت بار ورودی به انبار» + help text, xe loading/error
// over products + inbounds + central warehouses, the create form (product
// select, Persian-formatted qty/cartons, destination select with capacity
// line, datetime-local dispatch, carrier, optional plate, brand-deep
// «ثبت بار»), the two-line empty state, and per-inbound cards (fa-IR
// created date, counts + en status, carrier + plate, destination,
// dispatch/dispatched lines, items list, scheduled «ارسال شد»/«لغو»
// buttons). The previous candidate tab had the wrong title, no help, no
// warehouse/dispatch fields, number inputs with wrong placeholders,
// an indigo «ثبت محموله» button, wrong validation/success strings, a
// one-line empty text, raw ids + raw statuses on cards, and no
// dispatch/cancel actions at all.
// NOTES: live's tn takes an `embedded` prop (h2/help hidden when
// embedded); the tab renders it non-embedded. The help text points at a
// «با برچسب کد» mode switch above the tab — that labelled-inbound flow
// (incl. live's sn QR carton-label print component) is a separate
// parent-level feature, not part of tn, and is deferred, not fabricated.
// Capacity fields arrive from the backend as null (no capacity source
// exists) and render as live's own «نامحدود» null branches. The help box
// is rendered statically (live's ue expand behaviour unproven), same
// treatment as Fixes 34/35/37/42/44.
const INBOUND_STATUS_LABELS: Record<string, string> = { scheduled: "ثبت شده — در انتظار اعزام", pending: "ثبت شده", in_transit: "در راه انبار", receiving: "در حال تحویل‌گیری", received: "تحویل گرفته شد", cancelled: "لغوشده" };

// Persian-digit helpers reproducing live's __sepF/__sepW (same shape as
// Fix 42's inventory helpers): state holds Latin digits, display shows
// Persian digits with «٬» thousand separators.
const formatNumber = (digits: string, toPersianDigits: (n: number | string) => string): string => {
  if (digits === '') return '';
  const grouped = digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬');
  return toPersianDigits(grouped);
};
const parseDigits = (value: string): string => {
  const persian = '۰۱۲۳۴۵۶۷۸۹';
  let out = '';
  for (const ch of value) {
    const pi = persian.indexOf(ch);
    if (pi >= 0) out += String(pi);
    else if (ch >= '0' && ch <= '9') out += ch;
  }
  return out;
};

export const SupplierInboundTab: React.FC<{ embedded?: boolean }> = ({ embedded = false }) => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const products = useSupplierResource(supplierApi.getProducts);
  const inbounds = useSupplierResource(supplierApi.getInbounds);
  const warehouses = useSupplierResource(supplierApi.getCentralWarehouses);
  const sourceWarehouses = useSupplierResource(supplierApi.getMyWarehouses);
  const [sourceId, setSourceId] = useState('');
  const [productId, setProductId] = useState('');
  const [quantity, setQuantity] = useState('');
  const [cartons, setCartons] = useState('');
  const [warehouseId, setWarehouseId] = useState('');
  const [dispatchAt, setDispatchAt] = useState('');
  const [carrier, setCarrier] = useState('');
  const [plate, setPlate] = useState('');
  const [saving, setSaving] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const warehouseList = warehouses.data || [];
  const hasWarehouses = warehouseList.length > 0;
  const capacityLine = (w: any): string => {
    const units = w.remainingUnits == null ? "نامحدود" : `${toPersianDigits(w.remainingUnits)} قلم`;
    let volume = w.remainingVolumeM3 == null ? "نامحدود" : `${toPersianDigits(Number(w.remainingVolumeM3).toFixed(3))} مترمکعب${w.unknownDimensionUnits > 0 ? " (حداقل)" : ""}`;
    return `باقی‌مانده: ${units} · ${volume}`;
  };
  const formatDateTime = (t: string): string => {
    if (!t) return '';
    const d = new Date(t);
    if (Number.isNaN(d.getTime())) return t;
    return `${d.toLocaleDateString("fa-IR")} ${d.toLocaleTimeString("fa-IR", { hour: "2-digit", minute: "2-digit" })}`;
  };
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const units = Number(quantity);
    const cartonCount = Number(cartons);
    if (!productId || !Number.isSafeInteger(units) || units < 1 || !Number.isSafeInteger(cartonCount) || cartonCount < 1 || !carrier.trim()) { error('کالا را انتخاب کن و تعداد، کارتن و نام حامل را کامل وارد کن.'); return; }
    if (!sourceId) { error('انبار مبدأ را انتخاب کن.'); return; }
    if (!warehouseId) { error('انبار مرکزی مقصد را انتخاب کن.'); return; }
    if (!dispatchAt.trim()) { error('زمان اعزام را وارد کن.'); return; }
    setSaving(true);
    try {
      await supplierApi.createInbound({ sourceKind: 'transfer', supplierWarehouseId: sourceId, items: [{ product_id: productId, quantity: units, carton_count: cartonCount }], carrier: carrier.trim(), vehicle_plate: plate.trim(), warehouseId, dispatchAt: dispatchAt.trim() });
      success('بار در سامانه ثبت شد.');
      setQuantity('');
      setCartons('');
      setDispatchAt('');
      await inbounds.reload();
    } catch (cause: unknown) { error(cause instanceof Error ? cause.message : 'ثبت محموله انجام نشد.'); }
    finally { setSaving(false); }
  };
  const act = async (id: string, call: () => Promise<unknown>, doneText: string) => {
    setBusyId(id);
    try { await call(); success(doneText); await inbounds.reload(); }
    catch (cause: unknown) { error(cause instanceof Error ? cause.message : 'انجام نشد.'); }
    finally { setBusyId(null); }
  };
  return (
    <div className="space-y-3 pb-6">
{!embedded && (<>
      <h2 className="text-sm font-bold">ثبت بار ورودی به انبار</h2>
      <div className="rounded-2xl border bg-white p-3 text-[11px] leading-5 text-slate-600">وقتی بار را به انبار مرکزی می‌فرستی، قبلش اینجا ثبتش کن: کدام کالا، چند عدد، چند کارتن، به کدام انبار مرکزی و کی اعزام می‌شود. اگر روی کارتن‌ها برچسب کد چسبانده‌ای، راه «با برچسب کد» را از بالای همین صفحه انتخاب کن.</div>
</>)}
      <SupplierResourceState loading={products.loading || inbounds.loading || warehouses.loading} error={products.error || inbounds.error || warehouses.error} retry={() => { void products.reload(); void inbounds.reload(); void warehouses.reload(); }} />
      {!products.loading && !products.error && (
        <form onSubmit={submit} className="space-y-2 rounded-2xl border bg-white p-3">
          {(products.data || []).length === 0 && (<p className="text-[11px] leading-5 text-amber-700 bg-amber-50 border border-amber-100 rounded-lg px-2 py-1.5">هنوز کالای تأییدشده‌ای نداری؛ اول از تب «کالاها و تنوع‌ها» کالایت را پیشنهاد بده.</p>)}
          {!hasWarehouses && !warehouses.loading && (<p className="text-[11px] leading-5 text-amber-700 bg-amber-50 border border-amber-100 rounded-lg px-2 py-1.5">هنوز انبار مرکزی تعیین نشده است؛ تا وقتی مدیر انبار مرکزی را ثبت کند، نمی‌توانی بار بفرستی. کالاها را نگه دار و بعداً برگرد.</p>)}
          <select required value={productId} onChange={event => setProductId(event.target.value)} className="w-full rounded-xl border p-2.5 min-h-[44px] text-xs">
            <option value="">انتخاب کالا</option>
            {(products.data || []).map((item: any) => (<option key={item.id} value={item.id}>{item.title}</option>))}
          </select>
          <div className="grid grid-cols-2 gap-2">
            <input required min="1" step="1" type="text" inputMode="numeric" value={formatNumber(quantity, toPersianDigits)} onChange={event => setQuantity(parseDigits(event.target.value))} placeholder="چند عدد کالا؟" className="rounded-xl border p-2.5 min-h-[44px] text-xs" />
            <input required min="1" step="1" type="text" inputMode="numeric" value={formatNumber(cartons, toPersianDigits)} onChange={event => setCartons(parseDigits(event.target.value))} placeholder="چند کارتن؟" className="rounded-xl border p-2.5 min-h-[44px] text-xs" />
          </div>
          <label className="block text-xs">انبار مبدأ انتقال
            <select required value={sourceId} onChange={e => setSourceId(e.target.value)} className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs"><option value="">انتخاب مبدأ</option>{(sourceWarehouses.data || []).map((w: any) => <option key={w.id} value={w.id}>{w.name}</option>)}</select>
          </label>
          <label className="block text-[11px] font-bold text-slate-600">انبار مرکزی مقصد
            <select required value={warehouseId} onChange={event => setWarehouseId(event.target.value)} disabled={!hasWarehouses} className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs">
              <option value="">انتخاب انبار مرکزی</option>
              {warehouseList.map((w: any) => (<option key={w.warehouseId || w.id} value={w.warehouseId || w.id} disabled={!w.isReceivingOpen}>{w.warehouseName || w.name} — {w.cityName} · {capacityLine(w)}{w.isReceivingOpen ? "" : " · پذیرش بسته"}</option>))}
            </select>
          </label>
          <label className="block text-[11px] font-bold text-slate-600">زمان اعزام
            <input required type="datetime-local" value={dispatchAt} onChange={event => setDispatchAt(event.target.value)} disabled={!hasWarehouses} className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs" />
          </label>
          <input required value={carrier} onChange={event => setCarrier(event.target.value)} placeholder="نام حامل (راننده یا باربری)" className="w-full rounded-xl border p-2.5 min-h-[44px] text-xs" />
          <input value={plate} onChange={event => setPlate(event.target.value)} placeholder="پلاک خودرو (اختیاری)" className="w-full rounded-xl border p-2.5 min-h-[44px] text-xs" />
          <button disabled={saving || !hasWarehouses} className="w-full rounded-xl bg-brand-deep p-2.5 min-h-[44px] text-xs font-bold text-white disabled:opacity-50">{saving ? 'ثبت…' : 'ثبت بار'}</button>
        </form>
      )}
      {!inbounds.loading && !inbounds.error && !(inbounds.data || []).length && (
        <div className="rounded-2xl border bg-white p-6 text-center space-y-1.5">
          <p className="text-xs font-bold text-slate-700">هنوز باری نفرستاده‌ای.</p>
          <p className="text-[11px] leading-5 text-slate-400">بارهایی که با فرم بالا ثبت کنی، اینجا می‌آیند و وضعیتشان را می‌بینی.</p>
        </div>
      )}
      {!inbounds.error && (inbounds.data || []).map((item: any) => (
        <section key={item.id} className="rounded-2xl border bg-white p-3 text-xs">
          <strong>بار ثبت‌شده در {new Date(item.createdAt).toLocaleDateString("fa-IR")}</strong>
          <p className="mt-1">{toPersianDigits(item.itemsCount)} عدد · {toPersianDigits(item.cartonsCount)} کارتن · {INBOUND_STATUS_LABELS[item.status] || item.status}</p>
          <p className="text-slate-500">{item.carrier} {item.truckPlate}</p>
          <p className="text-slate-500">مقصد: {item.warehouseName || "—"}</p>
          {item.dispatchAt && (<p className="text-slate-500">زمان اعزام: {formatDateTime(item.dispatchAt)}</p>)}
          {item.dispatchedAt && (<p className="text-slate-500">اعزام شد: {formatDateTime(item.dispatchedAt)}</p>)}
          {(item.items || []).length > 0 && (
            <ul className="mt-2 space-y-1 border-t border-slate-100 pt-2">
              {(item.items || []).map((line: any, li: number) => (
                <li key={li} className="flex justify-between gap-2">
                  <span className="font-bold text-slate-700">{line.product_title || line.product_id || line.productId || "کالا"}</span>
                  <span className="shrink-0 text-slate-500">{toPersianDigits(line.quantity || 0)} عدد{line.carton_count ? ` · ${toPersianDigits(line.carton_count)} کارتن` : ""}</span>
                </li>
              ))}
            </ul>
          )}
          {item.status === "scheduled" && (
            <div className="flex gap-2 mt-2">
              <button type="button" disabled={busyId === item.id} onClick={() => act(item.id, () => supplierApi.dispatchInbound(item.id), "اعزام بار ثبت شد.")} className="flex-1 rounded-lg bg-brand-deep px-3 py-2 min-h-[40px] text-xs font-bold text-white disabled:opacity-50">ارسال شد</button>
              <button type="button" disabled={busyId === item.id} onClick={() => act(item.id, () => supplierApi.cancelInbound(item.id), "محموله لغو شد.")} className="flex-1 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 min-h-[40px] text-xs font-bold text-rose-700 disabled:opacity-50">لغو</button>
            </div>
          )}
        </section>
      ))}
    </div>
  );
};
