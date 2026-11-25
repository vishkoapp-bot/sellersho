import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { SupplierDialog } from './SupplierDialog';
import { supplierApi, type SupplierProductVariantDto } from '../../services/supplierApi';
import { createCartonDraftOperation } from '../../services/cartonDraft';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { normalizePersianText } from '../../utils/routing';
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
const fieldClass = 'mt-1 block min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 focus-visible:ring-2 focus-visible:ring-brand';
type ShipmentLabel = { id: string; shipment_code: string; item_count?: number; created_at?: string };

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

const printCartonLabel = (shipment: ShipmentLabel) => {
  const created = shipment.created_at ? new Date(shipment.created_at).toLocaleDateString("fa-IR") : "";
  const win = window.open("", "_blank");
  if (!win) return false;
  win.document.write(`<!doctype html><html lang="fa" dir="rtl"><head><meta charset="utf-8"><title>لیبل کارتن محموله — سلرشو</title></head><body style="padding:8mm 6mm;text-align:center;font-family:inherit"><div style="border:3px solid #000;padding:14px 12px"><div style="font-size:14px;font-weight:700;margin-bottom:6px">لیبل کارتن محموله — سلرشو</div><div dir="ltr" style="font-size:40px;font-weight:800;letter-spacing:0.12em">${shipment.shipment_code}</div><div style="font-size:12px;color:#333;margin-top:10px">${shipment.item_count ?? 0} قلم${created ? ` • ${created}` : ""}</div><div style="font-size:11px;color:#555;margin-top:4px">این لیبل را روی کارتن بچسبانید</div></div><script>window.onload = () => window.print();<\/script></body></html>`);
  win.document.close();
  return true;
};

interface ShipmentFlowProps {
  stage?: 'prepare' | 'track';
  selectedProductId?: string;
  onShipmentCreated?: () => void;
  onUnitLabels?: () => void;
  unitLabels?: React.ReactNode;
}

export const SupplierLabeledInboundTab: React.FC<ShipmentFlowProps> = ({ stage, selectedProductId, onShipmentCreated, onUnitLabels, unitLabels }) => {
  const { toPersianDigits, supplierSession } = useStore();
  const { success, error } = useToast();
  const products = useSupplierResource(supplierApi.getProducts);
  const [productId, setProductId] = useState('');
  const warehouses = useSupplierResource(supplierApi.getCentralWarehouses);
  const sourceWarehouses = useSupplierResource(supplierApi.getMyWarehouses);
  const sourceInventory = useSupplierResource(supplierApi.getInventoryLocations);
  const [sourceId, setSourceId] = useState('');
  const [sourceKind, setSourceKind] = useState('transfer');
  const [externalReference, setExternalReference] = useState('');
  const [sourceReason, setSourceReason] = useState('');
  const [warehouseId, setWarehouseId] = useState('');
  const loadVariants = useCallback(() => productId ? supplierApi.getVariants(productId) : Promise.resolve([]), [productId]);
  const variants = useSupplierResource(loadVariants);
  const [lines, setLines] = useState<{ variant: SupplierProductVariantDto; productTitle: string; expectedQty: string }[]>([]);
  const shipments = useSupplierResource(supplierApi.getShipments);
  const [shipmentQuery, setShipmentQuery] = useState('');
  const [shipmentStatus, setShipmentStatus] = useState('');
  const [actionError, setActionError] = useState('');
  const [pendingShipmentId, setPendingShipmentId] = useState<string | null>(null);
  const shipmentActionRef = useRef(false);
  const [cancelTarget, setCancelTarget] = useState<ShipmentLabel | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState('');
  const [created, setCreated] = useState<ShipmentLabel | null>(null);
  const draftOperation = useRef(createCartonDraftOperation());
  const submittingRef = useRef(false);
  useEffect(() => {
    draftOperation.current.reset();
    setLines([]);
    setCreated(null);
    setProductId('');
    setWarehouseId('');
    setSourceId('');
    setExternalReference('');
    setSourceReason('');
    setSubmitError('');
    setActionError('');
  }, [supplierSession?.supplierId, supplierSession?.token]);
  useEffect(() => {
    if (selectedProductId) setProductId(selectedProductId);
  }, [selectedProductId, supplierSession?.supplierId, supplierSession?.token]);
  useEffect(() => {
    if (stage === 'prepare') setCancelTarget(null);
  }, [stage]);
  const normalizedQuery = normalizePersianText(shipmentQuery.trim()).toUpperCase();
  const visibleShipments = (shipments.data ?? []).filter(shipment => (!shipmentStatus || shipment.status === shipmentStatus)
    && String(shipment.shipment_code ?? '').toUpperCase().includes(normalizedQuery));
  const print = (shipment: ShipmentLabel) => {
    if (!printCartonLabel(shipment)) error('پنجره چاپ باز نشد. اجازه باز شدن پنجره‌های چاپ را در مرورگر بده و دوباره تلاش کن.');
  };
  const changeDispatch = async (id: string, action: 'dispatch' | 'cancel') => {
    if (shipmentActionRef.current) return;
    shipmentActionRef.current = true;
    setPendingShipmentId(id);
    setActionError('');
    try {
      await supplierApi.changeShipmentDispatch(id, action);
      setCreated(current => current?.id === id ? null : current);
      success(action === 'dispatch' ? 'ارسال محموله ثبت شد.' : 'محموله لغو شد.');
      await shipments.reload();
      void sourceInventory.reload();
    } catch (cause: unknown) {
      setActionError(cause instanceof Error ? cause.message : action === 'dispatch' ? 'ارسال ثبت نشد.' : 'لغو ثبت نشد.');
    } finally {
      shipmentActionRef.current = false;
      setPendingShipmentId(null);
    }
  };
  const sourceStock = new Map<string, number>();
  for (const row of sourceInventory.data?.inventory ?? []) {
    if (row.variantId && row.supplierWarehouseId === sourceId && row.ownerType === 'supplier'
        && row.locationType === 'supplier' && row.status === 'sellable') {
      sourceStock.set(row.variantId, (sourceStock.get(row.variantId) ?? 0) + row.quantity);
    }
  }
  const showSourceStock = sourceKind === 'transfer' && !!sourceId && !!sourceInventory.data
    && !sourceInventory.loading && !sourceInventory.error;
  const addVariant = (variant: SupplierProductVariantDto) => {
    const productTitle = products.data?.find(product => product.id === productId)?.title ?? '';
    setLines(previous => previous.some(line => line.variant.id === variant.id) ? previous : [...previous, { variant, productTitle, expectedQty: '' }]);
  };
  const removeVariant = (id: string) => { setLines((prev) => prev.filter((l) => l.variant.id !== id)); };
  const submit = async () => {
    if (submittingRef.current) return;
    setSubmitError('');
    if (!warehouseId || !(warehouses.data || []).some((w: any) => w.id === warehouseId)) { setSubmitError('انبار مقصد را انتخاب کن.'); return; }
    const items = lines.map((l) => ({ variant_id: l.variant.id, expected_qty: Number(l.expectedQty) }));
    if (items.length === 0) { setSubmitError('حداقل یک مدل کالا به محموله اضافه کن.'); return; }
    const invalidLine = items.findIndex(item => !Number.isSafeInteger(item.expected_qty) || item.expected_qty <= 0 || item.expected_qty > 2147483647);
    if (invalidLine !== -1) {
      const line = lines[invalidLine];
      setSubmitError(`تعداد «${line.variant.variant_name}» را از ۱ تا ۲٬۱۴۷٬۴۸۳٬۶۴۷ وارد کن.`);
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    try {
      if (sourceKind === 'transfer' && !sourceId) throw new Error('انبار مبدأ را انتخاب کن.');
      if (sourceKind === 'external_replenishment') {
        if (!externalReference.trim()) throw new Error('شماره فاکتور خرید یا شماره مدرک ورود کالا را وارد کن.');
        if (!sourceReason.trim()) throw new Error('دلیل ورود کالا را بنویس، مثلاً خرید از تولیدکننده.');
      }
      const source = sourceKind === 'transfer' ? {source_kind: sourceKind, supplier_warehouse_id: sourceId} : {source_kind: sourceKind, external_reference: externalReference.trim(), reason: sourceReason.trim()};
      const shipment = await draftOperation.current.submit({ items, warehouse_id: warehouseId, source });
      draftOperation.current.reset();
      setCreated(shipment);
      setShipmentQuery('');
      setShipmentStatus('');
      onShipmentCreated?.();
      setLines([]);
      setExternalReference('');
      setSourceReason('');
      success("محموله ثبت شد. کدش را روی کارتن بنویس.");
      await shipments.reload();
    } catch (cause: unknown) { setSubmitError(cause instanceof Error ? cause.message : 'ثبت محموله انجام نشد.'); }
    finally {
      if (sourceKind === 'transfer') void sourceInventory.reload();
      submittingRef.current = false;
      setSubmitting(false);
    }
  };
  return (
    <div className="space-y-3.5 pb-6">
      {stage !== 'prepare' && created && (
        <div className="rounded-2xl border-2 border-emerald-200 bg-emerald-50 p-4 text-center space-y-2">
          <div className="flex items-center justify-center gap-1.5 text-emerald-700 text-xs font-extrabold"><CheckCircle2 className="w-4 h-4"  strokeWidth={1.5}/><span>ثبت شد! این کد را روی کارتن بنویس</span></div>
          <div className="font-mono text-3xl font-black text-emerald-800 tracking-widest" dir="ltr">{created.shipment_code}</div>
          <div className="flex items-center justify-center gap-3">
            <button type="button" onClick={() => print(created)} className="inline-flex items-center gap-1.5 px-3 py-2 min-h-11 rounded-xl bg-emerald-700 hover:bg-emerald-800 text-white text-xs font-bold transition-colors"><Printer className="w-3.5 h-3.5" strokeWidth={1.5}/>چاپ برچسب کارتن</button>
            <button type="button" onClick={() => setCreated(null)} className="min-h-11 px-2 text-xs font-bold text-emerald-700 hover:underline">بستن</button>
          </div>
        </div>
      )}
      <section hidden={stage === 'track'} className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm space-y-4 sm:p-5">
        <h3 className="text-base font-extrabold text-[#261b41] flex items-center gap-2"><Package className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/><span>محموله تازه با کد کارتن</span></h3>
        <p className="text-sm leading-7 text-slate-700">مبدأ و مقصد را مشخص کن، بعد همه کالاها و مدل‌هایی را که در این محموله می‌فرستی اضافه کن.</p>
        <fieldset disabled={submitting} className="min-w-0 space-y-4 disabled:opacity-70">
        <label className="block text-sm font-bold">کالا از کجا می‌آید؟<select value={sourceKind} onChange={e => setSourceKind(e.target.value)} className={fieldClass}><option value="transfer">از موجودی یکی از انبارهای من</option><option value="external_replenishment">ورود کالای جدید با فاکتور یا مدرک</option></select></label>
        {sourceKind === 'transfer' ? <>
          <SupplierResourceState loading={sourceWarehouses.loading} error={sourceWarehouses.error} retry={sourceWarehouses.reload} />
          <label className="block text-sm font-bold">انبار مبدأ<select value={sourceId} onChange={e => setSourceId(e.target.value)} className={fieldClass}><option value="">انتخاب انبار مبدأ</option>{(sourceWarehouses.data ?? []).map(w => <option key={w.id} value={w.id}>{w.name}</option>)}</select></label>
          {!sourceWarehouses.loading && !sourceWarehouses.error && !sourceWarehouses.data?.length && <p className="text-sm text-slate-700">برای انتقال موجودی، ابتدا در بخش «انبارهای من» یک انبار ثبت کن.</p>}
        </> : (
          <div className="space-y-3">
            <label className="block text-sm font-bold">
              شماره فاکتور خرید یا شماره مدرک ورود
              <input placeholder="مثلاً شماره فاکتور ۱۴۰۵-۲۵۸" aria-describedby="carton-document-help" value={externalReference} onChange={e => setExternalReference(e.target.value)} className={fieldClass} />
            </label>
            <p id="carton-document-help" className="text-xs leading-5 text-slate-600">شماره‌ای را وارد کن که روی فاکتور خرید، رسید تولید یا مدرک ورود درج شده است. هر شماره برای ثبت یک محموله قابل استفاده است.</p>
            <label className="block text-sm font-bold">
              دلیل ورود کالا
              <input placeholder="مثلاً خرید از تولیدکننده برای شارژ موجودی" value={sourceReason} onChange={e => setSourceReason(e.target.value)} className={fieldClass} />
            </label>
          </div>
        )}
        {sourceKind === 'transfer' && sourceId && (
          <SupplierResourceState loading={sourceInventory.loading} error={sourceInventory.error} retry={sourceInventory.reload} />
        )}
        <SupplierResourceState loading={warehouses.loading} error={warehouses.error} retry={warehouses.reload} />
        <label className="block text-sm font-bold">انبار مرکزی مقصد
          <select aria-label="انبار مرکزی مقصد" value={warehouseId} onChange={e => setWarehouseId(e.target.value)} className={fieldClass}>
            <option value="">انبار مقصد را انتخاب کن</option>
            {(warehouses.data || []).map((w: any) => <option key={w.id} value={w.id}>{w.name}</option>)}
          </select>
        </label>
        <SupplierResourceState loading={products.loading} error={products.error} retry={products.reload} />
        <label className="block text-sm font-bold">افزودن کالا به محموله
          <select id="carton-product" value={productId} onChange={(e) => setProductId(e.target.value)} className={fieldClass}>
            <option value="">انتخاب کالا</option>
            {(products.data ?? []).map(product => <option key={product.id} value={product.id}>{product.title}</option>)}
          </select>
        </label>
        {productId && <SupplierResourceState loading={variants.loading} error={variants.error} retry={variants.reload} />}
        {productId && !variants.loading && !variants.error && variants.data && (
          <div className="flex flex-wrap gap-1.5">
            {variants.data.length === 0 ? (<p className="text-sm leading-6 text-slate-600">این کالا مدلی ندارد. از بخش «کالاها و تنوع‌ها» برایش مدل بساز.</p>) : variants.data.map(v => (
              <button key={v.id} type="button" onClick={() => addVariant(v)} disabled={lines.some(l => l.variant.id === v.id)} className="flex min-h-11 max-w-full flex-wrap items-center gap-1.5 rounded-xl bg-brand-soft px-3 py-2 text-xs font-bold text-brand-deep hover:bg-brand-border/40 disabled:opacity-60">
                <Tag className="w-3 h-3"  strokeWidth={1.5}/>
                <span>{v.variant_name}</span>
                <span className="font-mono text-[10px]" dir="ltr">{v.spnc}</span>
                {showSourceStock && (<span className={`text-[10px] font-black ${(sourceStock.get(v.id) ?? 0) > 0 ? "text-emerald-700" : "text-rose-600"}`}>موجودی قابل رزرو: {toPersianDigits(sourceStock.get(v.id) ?? 0)}</span>)}
              </button>
            ))}
          </div>
        )}
        {lines.length > 0 && (
          <div className="space-y-2">
            <p className="text-sm font-bold">مدل‌های داخل محموله ({toPersianDigits(lines.length)})</p>
            {lines.map((l) => (
              <div key={l.variant.id} className="flex items-center gap-2 rounded-xl bg-slate-50 border border-slate-200 p-2">
                <div className="flex-1 min-w-0">
                  <span className="block break-words text-sm font-bold text-slate-800">{l.variant.variant_name}</span>
                  <span className="block break-words text-xs text-slate-600">{l.productTitle} · <bdi dir="ltr" className="font-mono">{l.variant.spnc}</bdi></span>
                </div>
                <input type="text" inputMode="numeric" value={formatNumber(l.expectedQty, toPersianDigits)} onChange={(e) => setLines((prev) => prev.map((x) => x.variant.id === l.variant.id ? { ...x, expectedQty: parseDigits(e.target.value) } : x))} placeholder="تعداد" aria-label={`تعداد ${l.variant.variant_name}`} className="min-h-11 w-16 rounded-lg border border-slate-300 bg-white p-2 text-center font-mono text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand" />
                <button type="button" onClick={() => removeVariant(l.variant.id)} className="grid size-11 shrink-0 place-items-center rounded-lg bg-rose-50 text-rose-700 hover:bg-rose-100 focus-visible:ring-2 focus-visible:ring-rose-700" aria-label={`حذف ${l.variant.variant_name} از محموله`}><X aria-hidden="true" className="size-4" strokeWidth={1.5}/></button>
              </div>
            ))}
          </div>
        )}
        </fieldset>
        {submitError && <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">{submitError}</p>}
        <button type="button" onClick={submit} disabled={submitting || warehouses.loading || !!warehouses.error} className="w-full py-2.5 min-h-12 rounded-xl bg-brand-deep text-white text-sm font-bold hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 transition-colors disabled:opacity-50 flex items-center justify-center gap-1.5">
          {submitting && (<RefreshCw className="w-4 h-4 animate-spin"  strokeWidth={1.5}/>)}
          <Truck className="w-4 h-4"  strokeWidth={1.5}/>
          <span>{submitting ? "در حال ثبت…" : "ثبت محموله و گرفتن کد کارتن"}</span>
        </button>
      </section>
      <section hidden={stage === 'prepare'} className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm space-y-3 sm:p-5">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-base font-extrabold text-[#261b41]">محموله‌ها و برچسب‌ها</h3>
          <button type="button" onClick={() => void shipments.reload()} disabled={shipments.loading || pendingShipmentId !== null} className="grid size-11 shrink-0 place-items-center rounded-xl border border-slate-200 text-slate-600 hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50" aria-label="به‌روزرسانی محموله‌ها" title="به‌روزرسانی"><RefreshCw className={`size-4 ${shipments.loading ? 'animate-spin' : ''}`} strokeWidth={1.5}/></button>
        </div>
        {stage === 'track' && (
          <>
            <div className="flex flex-wrap items-start gap-2">
              {onUnitLabels && <button type="button" aria-expanded={!!unitLabels} aria-controls={unitLabels ? 'supplier-unit-labels' : undefined} onClick={onUnitLabels} className="min-h-11 rounded-xl border border-[#dfd8ef] bg-brand-bg px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">{unitLabels ? 'بستن برچسب‌های واحد' : 'دیدن کدهای واحد کالا'}</button>}
              <details className="min-w-0 flex-1 rounded-xl bg-slate-50 px-3 text-xs leading-6 text-slate-700">
                <summary className="min-h-11 cursor-pointer py-2.5 font-bold text-[#261b41]">راهنمای چاپ</summary>
                <div className="space-y-1 pb-3">
                  <p><strong>برچسب کارتن</strong> را روی کارتن بچسبان و هنگام تحویل بار، «ثبت ارسال» را بزن.</p>
                  <p><strong>برچسب واحد (SPNCX)</strong> روی بسته‌بندی هر واحد کالا می‌چسبد. برای کالای جدید، انبار با ثبت دریافت این کدها را صادر می‌کند.</p>
                </div>
              </details>
            </div>
            {unitLabels}
          </>
        )}
        {actionError && <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">{actionError}</p>}
        <SupplierResourceState loading={shipments.loading} error={shipments.error} retry={shipments.reload} />
        {shipments.data && (shipments.data.length > 0 || shipmentQuery || shipmentStatus) && (
          <div className="grid grid-cols-2 gap-2.5 sm:gap-3">
            <label className="block min-w-0 text-xs font-bold sm:text-sm">کد محموله
              <input type="search" dir="auto" aria-label="جستجوی کد محموله" value={shipmentQuery} onChange={event => setShipmentQuery(event.target.value)} placeholder="جستجوی کد" className={fieldClass} />
            </label>
            <label className="block min-w-0 text-xs font-bold sm:text-sm">وضعیت محموله
              <select value={shipmentStatus} onChange={event => setShipmentStatus(event.target.value)} className={fieldClass}>
                <option value="">همه وضعیت‌ها</option>
                {Object.entries(SHIPMENT_STATUS_LABELS).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
              </select>
            </label>
          </div>
        )}
        {!shipments.loading && !shipments.error && (visibleShipments.length === 0 ? (
          <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-700">
            <p role="status">{shipments.data?.length ? 'محموله‌ای با این کد یا وضعیت پیدا نشد.' : 'هنوز محموله‌ای ثبت نشده. برای ساخت اولین محموله، به «آماده‌سازی محموله» برو.'}</p>
            {(shipmentQuery || shipmentStatus) && <button type="button" onClick={() => { setShipmentStatus(''); setShipmentQuery(''); }} className="mt-2 min-h-11 rounded-lg bg-white px-3 font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">پاک کردن فیلترها</button>}
          </div>
        ) : visibleShipments.map(s => (
          <article key={s.id} className="space-y-2 rounded-2xl border border-[#e6e2f0] bg-[#f9f7fc] p-3 sm:flex sm:items-center sm:justify-between sm:gap-3 sm:space-y-0">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <bdi dir="ltr" className="break-all text-sm font-black tracking-wider text-[#261b41]">{s.shipment_code}</bdi>
                <span className="rounded-lg bg-brand-soft px-2 py-1 text-[11px] font-bold text-brand-deep">{SHIPMENT_STATUS_LABELS[s.status] || s.status}</span>
              </div>
              <p className="mt-1 text-xs text-slate-600">{toPersianDigits(s.item_count ?? 0)} مدل{s.created_at ? ` · ${new Date(s.created_at).toLocaleDateString('fa-IR')}` : ''}</p>
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              <button type="button" onClick={() => print(s)} className="inline-flex min-h-11 items-center gap-1.5 rounded-xl border border-[#dfd8ef] bg-white px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">
                <Printer aria-hidden="true" className="size-4" strokeWidth={1.75} /> چاپ برچسب کارتن
              </button>
              {s.status === 'draft' && (
                <>
                  <button type="button" disabled={pendingShipmentId !== null} onClick={() => void changeDispatch(s.id, 'dispatch')} className="min-h-11 rounded-xl bg-brand-deep px-3 text-xs font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60">ثبت ارسال</button>
                  <button type="button" disabled={pendingShipmentId !== null} onClick={() => setCancelTarget(s)} className="min-h-11 rounded-xl px-2 text-xs font-bold text-rose-700 hover:bg-rose-50 focus-visible:ring-2 focus-visible:ring-rose-700 disabled:opacity-60">لغو محموله</button>
                  {pendingShipmentId === s.id && <span role="status" className="text-xs text-slate-600">در حال ثبت…</span>}
                </>
              )}
            </div>
          </article>
        )))}
        {cancelTarget && stage !== 'prepare' && <SupplierDialog titleId="supplier-cancel-heading" descriptionId="supplier-cancel-message" onDismiss={() => setCancelTarget(null)}>
          <h3 id="supplier-cancel-heading" className="text-base font-extrabold">محموله لغو شود؟</h3>
          <p id="supplier-cancel-message" className="mt-2 text-sm leading-7 text-slate-700">محمولهٔ <bdi dir="ltr" className="font-bold">{cancelTarget.shipment_code}</bdi> لغو می‌شود. اگر برایش موجودی رزرو شده باشد، رزرو آزاد می‌شود.</p>
          <div className="mt-4 flex flex-wrap gap-2">
            <button type="button" onClick={() => setCancelTarget(null)} className="min-h-11 rounded-xl border border-slate-300 px-4 text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand">بازگشت</button>
            <button type="button" onClick={() => { setCancelTarget(null); void changeDispatch(cancelTarget.id, 'cancel'); }} className="min-h-11 rounded-xl bg-rose-700 px-4 text-sm font-bold text-white hover:bg-rose-800 focus-visible:ring-2 focus-visible:ring-rose-700">تأیید لغو محموله</button>
          </div>
        </SupplierDialog>}
      </section>
    </div>
  );
};
