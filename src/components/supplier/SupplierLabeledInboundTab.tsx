import React, { useState, useEffect, useCallback } from 'react';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { supplierApi } from '../../services/supplierApi';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { Package, Truck, CheckCircle2, Printer, RefreshCw, X, Tag } from 'lucide-react';

// FIX 46: built from the live bundle SupplierDashboardView-MERGED.js (ln component).
// Labelled inbound flow: pick a product, pick its variants one by one
// with quantities, submit for a carton code, print the carton label (sn),
// and list previous code shipments with refresh. Live's ln shows no
// xe loading/error block: load failures surface only as toasts, and the
// shipments section shows «در حال دریافت…» while loading.
// NOTES: lucide icons are approximations (live's wa/il/ve/Nt/ca/Se/Re
// identities are bundle-internal). The print label mirrors live's sn
// layout (title, mono code, item count + date, stick-on instruction)
// but WITHOUT the QR code: no QR library exists in the pinned
// node_modules and a fresh install is forbidden (it pulls incompatible
// lucide-react), so the QR is a known print-output gap, not fabricated.
// Live's shipment-code format is unproven; the backend generates an
// 8-character code from an unambiguous alphabet (see repo comment).
const SHIPMENT_STATUS_LABELS: Record<string, string> = { draft: "پیش‌نویس", created: "ثبت‌شده", sent: "ارسال‌شده", partially_received: "بخشی رسیده", in_transit: "در راه انبار", received: "به انبار رسید", qc_passed: "کنترل کیفیت: قبول شد", qc_failed: "کنترل کیفیت: رد شد", cancelled: "لغو شده" };

// Persian-digit helpers reproducing live's __sepF/__sepW (same shape as
// Fix 42's inventory helpers).
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

const printCartonLabel = (shipment: any) => {
  const created = shipment.created_at ? new Date(shipment.created_at).toLocaleDateString("fa-IR") : "";
  const win = window.open("", "_blank");
  if (!win) return;
  win.document.write(`<!doctype html><html lang="fa" dir="rtl"><head><meta charset="utf-8"><title>لیبل کارتن محموله — سلرشو</title></head><body style="padding:8mm 6mm;text-align:center;font-family:inherit"><div style="border:3px solid #000;padding:14px 12px"><div style="font-size:14px;font-weight:700;margin-bottom:6px">لیبل کارتن محموله — سلرشو</div><div dir="ltr" style="font-size:40px;font-weight:800;letter-spacing:0.12em">${shipment.shipment_code}</div><div style="font-size:12px;color:#333;margin-top:10px">${shipment.item_count ?? 0} قلم${created ? ` • ${created}` : ""}</div><div style="font-size:11px;color:#555;margin-top:4px">این لیبل را روی کارتن بچسبانید — انبار با اسکن QR تحویل می‌گیرد</div></div><script>window.onload = () => window.print();<\/script></body></html>`);
  win.document.close();
};

export const SupplierLabeledInboundTab: React.FC = () => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [products, setProducts] = useState<any[]>([]);
  const [productsLoading, setProductsLoading] = useState(true);
  const [productId, setProductId] = useState('');
  const warehouses = useSupplierResource(supplierApi.getCentralWarehouses);
  const [warehouseId, setWarehouseId] = useState('');
  const [variants, setVariants] = useState<any[]>([]);
  const [variantsLoading, setVariantsLoading] = useState(false);
  const [lines, setLines] = useState<{ variant: any; expectedQty: string }[]>([]);
  const [shipments, setShipments] = useState<any[]>([]);
  const [shipmentsLoading, setShipmentsLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [created, setCreated] = useState<any | null>(null);
  const loadProducts = useCallback(async () => {
    setProductsLoading(true);
    try { setProducts(await supplierApi.getProducts()); }
    catch { error("کالاها نیامدند؛ دوباره تلاش کن."); }
    finally { setProductsLoading(false); }
  }, [error]);
  const loadShipments = useCallback(async () => {
    setShipmentsLoading(true);
    try { setShipments(await supplierApi.getShipments()); }
    catch { error("محموله‌ها نیامدند؛ دوباره تلاش کن."); }
    finally { setShipmentsLoading(false); }
  }, [error]);
  useEffect(() => { loadProducts(); loadShipments(); }, [loadProducts, loadShipments]);
  useEffect(() => {
    if (!productId) { setVariants([]); return; }
    setVariantsLoading(true);
    supplierApi.getVariants(productId)
      .then(setVariants)
      .catch(() => { error("مدل‌های کالا نیامدند."); })
      .finally(() => { setVariantsLoading(false); });
  }, [productId, error]);
  const addVariant = (v: any) => {
    if (lines.some((l) => l.variant.id === v.id)) { error("این مدل را قبلاً اضافه کرده‌ای."); return; }
    setLines((prev) => [...prev, { variant: v, expectedQty: "" }]);
  };
  const removeVariant = (id: string) => { setLines((prev) => prev.filter((l) => l.variant.id !== id)); };
  const submit = async () => {
    if (!warehouseId || !(warehouses.data || []).some((w: any) => w.id === warehouseId)) { error("انبار مقصد را انتخاب کن."); return; }
    const items = lines.map((l) => ({ variant_id: l.variant.id, expected_qty: Number(l.expectedQty) })).filter((l) => Number.isSafeInteger(l.expected_qty) && l.expected_qty > 0);
    if (items.length === 0) { error("حداقل یک مدل با تعداد درست اضافه کن."); return; }
    setSubmitting(true);
    try {
      const shipment = await supplierApi.createShipment({ items, warehouse_id: warehouseId });
      setCreated(shipment);
      setLines([]);
      success("محموله ثبت شد. کدش را روی کارتن بنویس.");
      await loadShipments();
    } catch (cause: unknown) { error(cause instanceof Error ? cause.message : "ثبت محموله انجام نشد."); }
    finally { setSubmitting(false); }
  };
  return (
    <div className="space-y-3.5 pb-6">
      {created && (
        <div className="rounded-2xl border-2 border-emerald-200 bg-emerald-50 p-4 text-center space-y-2">
          <div className="flex items-center justify-center gap-1.5 text-emerald-700 text-xs font-extrabold"><CheckCircle2 className="w-4 h-4"  strokeWidth={1.5}/><span>ثبت شد! این کد را روی کارتن بنویس</span></div>
          <div className="font-mono text-3xl font-black text-emerald-800 tracking-widest" dir="ltr">{created.shipment_code}</div>
          <div className="flex items-center justify-center gap-3">
            <button type="button" onClick={() => printCartonLabel(created)} className="inline-flex items-center gap-1.5 px-3 py-2 min-h-[40px] rounded-xl bg-emerald-600 hover:bg-emerald-700 text-white text-[11px] font-bold transition-colors"><Printer className="w-3.5 h-3.5"  strokeWidth={1.5}/>چاپ برچسب کارتن</button>
            <button type="button" onClick={() => setCreated(null)} className="text-[11px] font-bold text-emerald-700 hover:underline">بستن</button>
          </div>
        </div>
      )}
      <section className="rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs space-y-3">
        <h3 className="text-xs font-extrabold text-slate-800 flex items-center gap-1.5"><Package className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/><span>محموله تازه با کد کارتن</span></h3>
        <p className="text-[10px] leading-5 text-slate-500">این راه برای کالاهایی است که هر مدلشان کد جدا دارد: کالا و مدلش را انتخاب کن، تعداد را بنویس؛ سامانه یک کد می‌دهد. آن کد را روی کارتن بچسبان و بار را بفرست. </p>
        <SupplierResourceState loading={warehouses.loading} error={warehouses.error} retry={warehouses.reload} />
        <label className="block text-xs font-bold text-slate-700">انبار مرکزی مقصد
          <select aria-label="انبار مرکزی مقصد" value={warehouseId} onChange={e => setWarehouseId(e.target.value)} className="mt-1 w-full rounded-xl border border-slate-200 p-2.5 min-h-[44px] bg-white">
            <option value="">انبار مقصد را انتخاب کن</option>
            {(warehouses.data || []).map((w: any) => <option key={w.id} value={w.id}>{w.name}</option>)}
          </select>
        </label>
        <select value={productId} onChange={(e) => setProductId(e.target.value)} className="w-full rounded-xl border border-slate-200 p-2.5 min-h-[44px] text-xs bg-white focus:border-brand-deep outline-none">
          <option value="">{productsLoading ? "صبر کن… کالاها می‌آیند" : "انتخاب کالا"}</option>
          {products.map((p: any) => (<option key={p.id} value={p.id}>{p.title}</option>))}
        </select>
        {variantsLoading ? (<p className="text-[11px] text-slate-500">مدل‌های کالا می‌آیند…</p>) : productId ? (
          <div className="flex flex-wrap gap-1.5">
            {variants.length === 0 ? (<p className="text-[11px] leading-5 text-slate-500">این کالا مدلی ندارد. اول از تب «کالاها و تنوع‌ها» برایش مدل بساز، بعد اینجا می‌آید.</p>) : variants.map((v: any) => (
              <button key={v.id} type="button" onClick={() => addVariant(v)} disabled={lines.some((l) => l.variant.id === v.id)} className="flex items-center gap-1 px-3 py-2 min-h-[40px] rounded-xl bg-brand-soft text-brand-deep text-[11px] font-bold hover:bg-brand-border/40 disabled:opacity-40">
                <Tag className="w-3 h-3"  strokeWidth={1.5}/>
                <span>{v.variant_name}</span>
                <span className="font-mono text-[10px]" dir="ltr">{v.spnc}</span>
                {typeof v.stock === "number" && (<span className={`text-[10px] font-black ${v.stock > 0 ? "text-emerald-700" : "text-rose-600"}`}>موجودی: {toPersianDigits(v.stock)}</span>)}
              </button>
            ))}
          </div>
        ) : null}
        {lines.length > 0 && (
          <div className="space-y-2">
            {lines.map((l) => (
              <div key={l.variant.id} className="flex items-center gap-2 rounded-xl bg-slate-50 border border-slate-200 p-2">
                <div className="flex-1 min-w-0">
                  <span className="text-[11px] font-bold text-slate-800 block truncate">{l.variant.variant_name}</span>
                  <span className="font-mono text-[10px] text-slate-400" dir="ltr">{l.variant.spnc}</span>
                </div>
                <input type="text" inputMode="numeric" min="1" value={formatNumber(l.expectedQty, toPersianDigits)} onChange={(e) => setLines((prev) => prev.map((x) => x.variant.id === l.variant.id ? { ...x, expectedQty: parseDigits(e.target.value) } : x))} placeholder="تعداد" className="w-20 rounded-lg border border-slate-200 p-2 text-xs text-center font-bold font-mono" />
                <button type="button" onClick={() => removeVariant(l.variant.id)} className="p-2 rounded-lg bg-rose-50 text-rose-600" aria-label="حذف"><X className="w-3.5 h-3.5"  strokeWidth={1.5}/></button>
              </div>
            ))}
          </div>
        )}
        <button type="button" onClick={submit} disabled={submitting || lines.length === 0 || !warehouseId || warehouses.loading || !!warehouses.error} className="w-full py-2.5 min-h-[44px] rounded-xl bg-brand-deep text-white text-xs font-bold hover:bg-brand transition-colors disabled:opacity-50 flex items-center justify-center gap-1.5">
          {submitting && (<RefreshCw className="w-4 h-4 animate-spin"  strokeWidth={1.5}/>)}
          <Truck className="w-4 h-4"  strokeWidth={1.5}/>
          <span>{submitting ? "در حال ثبت…" : "ثبت محموله و گرفتن کد کارتن"}</span>
        </button>
      </section>
      <section className="rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs space-y-2.5">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-extrabold text-slate-800">محموله‌های قبلی</h3>
          <button type="button" onClick={() => void loadShipments()} disabled={shipmentsLoading} className="p-1.5 rounded-xl border border-slate-200 text-slate-600 disabled:opacity-50" title="به‌روزرسانی"><RefreshCw className={`w-3.5 h-3.5 ${shipmentsLoading ? "animate-spin" : ""}`}  strokeWidth={1.5}/></button>
        </div>
        {shipmentsLoading ? (<p className="text-[11px] text-slate-500 text-center py-4">در حال دریافت…</p>) : shipments.length === 0 ? (<p className="text-[11px] leading-5 text-slate-500 text-center py-4">هنوز محموله‌ای با کد نفرستاده‌ای. اولین محموله‌ات را با فرم بالا بساز.</p>) : shipments.map((s: any) => (
          <div key={s.id} className="flex items-center justify-between rounded-xl bg-slate-50 border border-slate-200 p-2.5">
            <div>
              <span className="font-mono text-sm font-black text-slate-800 tracking-wider" dir="ltr">{s.shipment_code}</span>
              <span className="text-[10px] text-slate-400 block mt-0.5">{toPersianDigits(s.item_count ?? 0)} مدل • {s.created_at ? new Date(s.created_at).toLocaleDateString("fa-IR") : ""}</span>
            </div>
            <div className="flex items-center gap-1.5">
              <button type="button" onClick={() => printCartonLabel(s)} className="p-1.5 rounded-lg bg-white border border-slate-200 text-slate-600 hover:text-brand-deep hover:border-brand-border transition-colors" title="چاپ برچسب کارتن"><Printer className="w-3.5 h-3.5"  strokeWidth={1.5}/></button>
              <span className="text-[10px] font-bold bg-brand-soft text-brand-deep px-2.5 py-1 rounded-full">{SHIPMENT_STATUS_LABELS[s.status] || s.status}</span>
            </div>
          </div>
        ))}
      </section>
    </div>
  );
};
