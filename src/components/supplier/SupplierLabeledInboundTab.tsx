import React, { useState, useEffect, useCallback, useId, useRef } from 'react';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { SupplierDialog } from './SupplierDialog';
import { supplierApi, type SupplierProductVariantDto } from '../../services/supplierApi';
import { ApiError } from '../../services/apiClient';
import { createCartonDraftOperation } from '../../services/cartonDraft';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { normalizePersianText } from '../../utils/routing';
import { ContextSupportButton } from '../support/ContextSupportButton';
import { SupplierCartonDetails } from './SupplierCartonDetails';
import { CheckCircle2, ChevronDown, Package, Printer, RefreshCw, X, Tag } from 'lucide-react';

const SHIPMENT_STATUS_LABELS: Record<string, string> = { draft: "پیش‌نویس", created: "ثبت‌شده", sent: "ارسال‌شده", partially_received: "بخشی رسیده", in_transit: "در راه انبار", received: "به انبار رسید", qc_passed: "کنترل کیفیت: قبول شد", qc_failed: "کنترل کیفیت: رد شد", cancelled: "لغو شده" };
const OPEN_SHIPMENT_STATUSES = new Set(['draft', 'created', 'sent', 'in_transit', 'partially_received']);
const fieldClass = 'mt-1 block min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 focus-visible:ring-2 focus-visible:ring-brand';
const shipmentButton = 'inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl border border-brand-border bg-white px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand';
type ShipmentLabel = { id: string; shipment_code: string; item_count?: number; created_at?: string; status?: string };

const parseQuantity = (value: string) => {
  const digits = normalizePersianText(value.trim());
  if (!/^(?:\d+|\d{1,3}(?:[,٬]\d{3})+)$/.test(digits)) return NaN;
  return Number(digits.replace(/[,٬]/g, ''));
};

const printCartonLabel = (shipment: ShipmentLabel) => {
  const created = shipment.created_at ? new Date(shipment.created_at).toLocaleDateString("fa-IR") : "";
  const win = window.open("", "_blank");
  if (!win) return false;
  try {
    win.opener = null;
    const doc = win.document;
    doc.documentElement.lang = 'fa';
    doc.documentElement.dir = 'rtl';
    doc.title = 'برچسب کارتن محموله — سلرشو';
    doc.body.style.cssText = 'padding: 8mm 6mm; text-align: center; font-family: Tahoma, sans-serif';
    const label = doc.createElement('main');
    label.style.cssText = 'border: 3px solid #000; padding: 14px 12px';
    const heading = doc.createElement('h1');
    heading.style.fontSize = '16px';
    heading.textContent = 'کد کارتن محموله';
    const code = doc.createElement('strong');
    code.dir = 'ltr';
    code.style.cssText = 'font: 800 40px monospace; letter-spacing: 0.1em; overflow-wrap: anywhere';
    code.textContent = shipment.shipment_code;
    const meta = doc.createElement('p');
    meta.textContent = `${(shipment.item_count ?? 0).toLocaleString('fa-IR')} تنوع${created ? ` · ${created}` : ''}`;
    const instruction = doc.createElement('p');
    instruction.textContent = 'این برچسب را روی سطح بیرونی کارتن بچسبانید.';
    label.append(heading, code, meta, instruction);
    doc.body.append(label);
    win.focus();
    win.print();
    return true;
  } catch {
    win.close();
    return false;
  }
};

interface ShipmentFlowProps {
  stage?: 'prepare' | 'track';
  selectedProductId?: string;
  onShipmentCreated?: () => void;
  openOnly?: boolean;
}

export const SupplierLabeledInboundTab: React.FC<ShipmentFlowProps> = ({ stage, selectedProductId, onShipmentCreated, openOnly }) => {
  const { toPersianDigits, supplierSession } = useStore();
  const { success, error } = useToast();
  const products = useSupplierResource(supplierApi.getProducts);
  const [productId, setProductId] = useState('');
  const warehouses = useSupplierResource(supplierApi.getCentralWarehouses);
  const destinations = warehouses.data ?? [];
  const hasOpenDestination = destinations.some(warehouse => warehouse.isReceivingOpen !== false);
  const sourceWarehouses = useSupplierResource(supplierApi.getMyWarehouses);
  const activeSources = (sourceWarehouses.data ?? []).filter(warehouse => warehouse.isActive !== false);
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
  const [detailsId, setDetailsId] = useState('');
  const [actionError, setActionError] = useState('');
  const [pendingShipmentId, setPendingShipmentId] = useState<string | null>(null);
  const shipmentActionRef = useRef(false);
  const [cancelTarget, setCancelTarget] = useState<ShipmentLabel | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState('');
  const [created, setCreated] = useState<ShipmentLabel | null>(null);
  const receiptId = useId();
  const trackingHeading = useRef<HTMLHeadingElement>(null);
  const focusTracking = useRef(false);
  const draftOperation = useRef(createCartonDraftOperation());
  const submittingRef = useRef(false);
  useEffect(() => {
    draftOperation.current.reset();
    focusTracking.current = false;
    setLines([]);
    setCreated(null);
    setDetailsId('');
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
  useEffect(() => {
    if (stage !== 'prepare' && created && focusTracking.current) {
      focusTracking.current = false;
      trackingHeading.current?.focus();
    }
  }, [created, stage]);
  useEffect(() => {
    setShipmentStatus('');
    setShipmentQuery('');
  }, [openOnly]);
  const normalizedQuery = normalizePersianText(shipmentQuery.trim()).toUpperCase();
  const visibleShipments = (shipments.data ?? []).filter(shipment => (!openOnly || OPEN_SHIPMENT_STATUSES.has(shipment.status))
    && (!shipmentStatus || shipment.status === shipmentStatus)
    && String(shipment.shipment_code ?? '').toUpperCase().includes(normalizedQuery));
  const createdStatus = (shipments.data ?? []).find(shipment => shipment.id === created?.id)?.status ?? created?.status ?? 'draft';
  const createdInList = !shipments.loading && !shipments.error && visibleShipments.some(shipment => shipment.id === created?.id);
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
      if (cause instanceof ApiError && cause.status === 409) await shipments.reload();
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
  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submittingRef.current) return;
    setSubmitError('');
    if (!destinations.some(warehouse => warehouse.id === warehouseId && warehouse.isReceivingOpen !== false)) { setSubmitError('انبار مقصدی را انتخاب کن که پذیرش محموله دارد.'); return; }
    const items = lines.map((l) => ({ variant_id: l.variant.id, expected_qty: parseQuantity(l.expectedQty) }));
    if (items.length === 0) { setSubmitError('حداقل یک تنوع کالا به محموله اضافه کن.'); return; }
    const invalidLine = items.findIndex(item => !Number.isSafeInteger(item.expected_qty) || item.expected_qty <= 0 || item.expected_qty > 2147483647);
    if (invalidLine !== -1) {
      const line = lines[invalidLine];
      setSubmitError(`تعداد «${line.variant.variant_name}» را از ۱ تا ۲٬۱۴۷٬۴۸۳٬۶۴۷ وارد کن.`);
      return;
    }
    submittingRef.current = true;
    setSubmitting(true);
    try {
      if (sourceKind === 'transfer' && (sourceWarehouses.error || !activeSources.some(warehouse => warehouse.id === sourceId))) throw new Error('یک انبار مبدأ فعال را انتخاب کن.');
      if (sourceKind === 'external_replenishment') {
        if (!externalReference.trim()) throw new Error('شماره فاکتور خرید یا شماره مدرک ورود کالا را وارد کن.');
        if (!sourceReason.trim()) throw new Error('دلیل ورود کالا را بنویس، مثلاً خرید از تولیدکننده.');
      }
      const source = sourceKind === 'transfer' ? {source_kind: sourceKind, supplier_warehouse_id: sourceId} : {source_kind: sourceKind, external_reference: externalReference.trim(), reason: sourceReason.trim()};
      const shipment = await draftOperation.current.submit({ items, warehouse_id: warehouseId, source });
      draftOperation.current.reset();
      focusTracking.current = true;
      setCreated(shipment);
      setDetailsId(shipment.id);
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
      <section hidden={stage === 'track'} className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm space-y-4 sm:p-5">
        <h3 className="flex items-center gap-2 text-base font-extrabold text-[#261b41]"><Package aria-hidden="true" className="size-4 text-brand-deep" strokeWidth={1.5} />محمولهٔ جدید</h3>
        <form onSubmit={submit} className="space-y-5">
          <fieldset disabled={submitting} className="grid min-w-0 gap-3 rounded-2xl border border-[#e6e2f0] px-3 pb-3 pt-1 disabled:opacity-70 sm:grid-cols-2 sm:px-4 sm:pb-4">
            <legend className="px-1 text-sm font-extrabold text-brand-deep">مبدأ و مقصد</legend>
            <label className="block text-sm font-bold">کالا از کجا می‌آید؟<select value={sourceKind} onChange={e => setSourceKind(e.target.value)} className={fieldClass}><option value="transfer">از موجودی یکی از انبارهای من</option><option value="external_replenishment">ورود کالای جدید با فاکتور یا مدرک</option></select></label>
            {sourceKind === 'transfer' ? <>
              {(sourceWarehouses.loading || sourceWarehouses.error) && <div className="sm:col-span-2"><SupplierResourceState loading={sourceWarehouses.loading} error={sourceWarehouses.error} retry={sourceWarehouses.reload} /></div>}
              <label className="block text-sm font-bold">انبار مبدأ<select value={sourceId} onChange={e => setSourceId(e.target.value)} className={fieldClass}><option value="">انتخاب انبار مبدأ</option>{activeSources.map(warehouse => <option key={warehouse.id} value={warehouse.id}>{warehouse.name}</option>)}</select></label>
              {!sourceWarehouses.loading && !sourceWarehouses.error && activeSources.length === 0 && <p className="rounded-xl bg-brand-soft p-3 text-sm leading-6 text-slate-700 sm:col-span-2">برای انتقال موجودی، در بخش «انبارهای من» یک انبار فعال ثبت کن.</p>}
            </> : (
              <>
                <div>
                  <label className="block text-sm font-bold">
                    شماره فاکتور یا مدرک ورود
                    <input placeholder="مثلاً ۱۴۰۵-۲۵۸" aria-describedby="carton-document-help" value={externalReference} onChange={e => setExternalReference(e.target.value)} className={fieldClass} />
                  </label>
                  <p id="carton-document-help" className="mt-1 text-xs leading-5 text-slate-600">هر شماره برای ثبت یک محموله قابل استفاده است.</p>
                </div>
                <label className="block text-sm font-bold">
                  دلیل ورود کالا
                  <input placeholder="مثلاً خرید از تولیدکننده برای شارژ موجودی" value={sourceReason} onChange={e => setSourceReason(e.target.value)} className={fieldClass} />
                </label>
              </>
            )}
            {(warehouses.loading || warehouses.error) && <div className="sm:col-span-2"><SupplierResourceState loading={warehouses.loading} error={warehouses.error} retry={warehouses.reload} /></div>}
            <label className="block text-sm font-bold">انبار مرکزی مقصد
              <select aria-label="انبار مرکزی مقصد" value={warehouseId} onChange={e => setWarehouseId(e.target.value)} className={fieldClass}>
                <option value="">انبار مقصد را انتخاب کن</option>
                {destinations.map(warehouse => <option key={warehouse.id} value={warehouse.id} disabled={warehouse.isReceivingOpen === false}>{warehouse.name}{warehouse.isReceivingOpen === false ? ' · پذیرش بسته' : ''}</option>)}
              </select>
            </label>
            {!warehouses.loading && !warehouses.error && !hasOpenDestination && <p className="rounded-xl bg-brand-soft p-3 text-sm leading-6 text-slate-700 sm:col-span-2">فعلاً هیچ انبار مرکزی برای پذیرش محموله باز نیست.</p>}
          </fieldset>
          <fieldset disabled={submitting} className="min-w-0 space-y-3 rounded-2xl border border-[#e6e2f0] px-3 pb-3 pt-1 disabled:opacity-70 sm:px-4 sm:pb-4">
            <legend className="px-1 text-sm font-extrabold text-brand-deep">محتویات کارتن</legend>
            <SupplierResourceState loading={products.loading} error={products.error} retry={products.reload} />
            <label className="block text-sm font-bold">افزودن کالا به محموله
              <select id="carton-product" value={productId} onChange={(e) => setProductId(e.target.value)} className={fieldClass}>
                <option value="">انتخاب کالا</option>
                {(products.data ?? []).map(product => <option key={product.id} value={product.id}>{product.title}</option>)}
              </select>
            </label>
            {sourceKind === 'transfer' && sourceId && <SupplierResourceState loading={sourceInventory.loading} error={sourceInventory.error} retry={sourceInventory.reload} />}
            {productId && <SupplierResourceState loading={variants.loading} error={variants.error} retry={variants.reload} />}
            {productId && !variants.loading && !variants.error && variants.data && (
              <div className="flex flex-wrap gap-1.5">
                {variants.data.length === 0 ? (<p className="text-sm leading-6 text-slate-600">این کالا تنوعی ندارد. از بخش «کالاها و تنوع‌ها» برایش تنوع ثبت کن.</p>) : variants.data.map(v => (
                  <button key={v.id} type="button" onClick={() => addVariant(v)} disabled={lines.some(l => l.variant.id === v.id)} className="flex min-h-11 max-w-full flex-wrap items-center gap-1.5 rounded-xl bg-brand-soft px-3 py-2 text-xs font-bold text-brand-deep hover:bg-brand-border/40 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">
                    <Tag aria-hidden="true" className="size-3" strokeWidth={1.5} />
                    <span>{v.variant_name}</span>
                    <bdi dir="ltr" className="font-mono">{v.spnc}</bdi>
                    {showSourceStock && <span className={`font-medium ${(sourceStock.get(v.id) ?? 0) > 0 ? 'text-emerald-800' : 'text-rose-800'}`}>موجودی قابل رزرو {toPersianDigits(sourceStock.get(v.id) ?? 0)}</span>}
                  </button>
                ))}
              </div>
            )}
            {lines.length > 0 && (
              <div className="space-y-2">
                <p className="text-sm font-bold">{toPersianDigits(lines.length)} تنوع انتخاب شده</p>
                {lines.map((l) => (
                  <div key={l.variant.id} className="flex items-center gap-2 rounded-xl bg-slate-50 border border-slate-200 p-2">
                    <div className="flex-1 min-w-0">
                      <span className="block break-words text-sm font-bold text-slate-800">{l.variant.variant_name}</span>
                      <span className="block break-words text-xs text-slate-600">{l.productTitle} · <bdi dir="ltr" className="font-mono">{l.variant.spnc}</bdi></span>
                    </div>
                    <input type="text" inputMode="numeric" dir="ltr" value={l.expectedQty} onChange={(e) => setLines((prev) => prev.map((x) => x.variant.id === l.variant.id ? { ...x, expectedQty: e.target.value } : x))} placeholder="تعداد" aria-label={`تعداد ${l.variant.variant_name}`} className="min-h-11 w-16 rounded-lg border border-slate-300 bg-white p-2 text-center font-mono text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand" />
                    <button type="button" onClick={() => removeVariant(l.variant.id)} className="grid size-11 shrink-0 place-items-center rounded-lg bg-rose-50 text-rose-700 hover:bg-rose-100 focus-visible:ring-2 focus-visible:ring-rose-700" aria-label={`حذف ${l.variant.variant_name} از محموله`}><X aria-hidden="true" className="size-4" strokeWidth={1.5}/></button>
                  </div>
                ))}
              </div>
            )}
          </fieldset>
          {submitError && <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800">{submitError}</p>}
          <button type="submit" disabled={submitting || warehouses.loading || !!warehouses.error || !hasOpenDestination} className="flex min-h-12 w-full items-center justify-center gap-1.5 rounded-xl bg-brand-deep px-4 py-2.5 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50 sm:w-auto">
            {submitting && <RefreshCw aria-hidden="true" className="size-4 motion-safe:animate-spin" strokeWidth={1.5} />}
            <span>{submitting ? "در حال ثبت…" : "ثبت محموله و گرفتن کد کارتن"}</span>
          </button>
        </form>
      </section>
      <section hidden={stage === 'prepare'} className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm space-y-3 sm:p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 ref={trackingHeading} tabIndex={-1} className="scroll-mt-24 text-base font-extrabold text-[#261b41]">محموله‌ها و برچسب‌ها</h3>
          <button type="button" onClick={() => void shipments.reload()} disabled={shipments.loading || pendingShipmentId !== null} className={`${shipmentButton} shrink-0 disabled:opacity-50`}>
            <RefreshCw aria-hidden="true" className={`size-4 ${shipments.loading ? 'motion-safe:animate-spin' : ''}`} strokeWidth={1.5} />
            به‌روزرسانی محموله‌ها
          </button>
        </div>
        {created && createdStatus === 'draft' && (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-emerald-200 bg-emerald-50 px-3 py-2">
            <p role="status" className="flex min-w-0 items-start gap-2 text-sm leading-6 text-emerald-900">
              <CheckCircle2 aria-hidden="true" className="mt-1 size-4 shrink-0" strokeWidth={1.75} />
              <span>محمولهٔ <bdi dir="ltr" className="break-all font-mono font-bold">{created.shipment_code}</bdi> ثبت شد. کد را روی کارتن بنویس یا برچسبش را بچسبان.</span>
            </p>
            <div className="flex flex-wrap items-center gap-1">
              {!createdInList && <button type="button" onClick={() => print(created)} className={shipmentButton}>چاپ برچسب کارتن</button>}
              <button type="button" onClick={() => setCreated(null)} className="grid size-11 place-items-center rounded-xl text-emerald-900 hover:bg-emerald-100 focus-visible:ring-2 focus-visible:ring-emerald-800" aria-label="بستن پیام ثبت محموله"><X aria-hidden="true" className="size-4" /></button>
            </div>
          </div>
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
                <option value="">{openOnly ? 'محموله‌های باز' : 'همه وضعیت‌ها'}</option>
                {Object.entries(SHIPMENT_STATUS_LABELS).filter(([key]) => !openOnly || OPEN_SHIPMENT_STATUSES.has(key)).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
              </select>
            </label>
          </div>
        )}
        {!shipments.loading && !shipments.error && (visibleShipments.length === 0 ? (
          <div className="rounded-xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-700">
            <p role="status">{shipmentQuery || shipmentStatus ? 'محموله‌ای با این کد یا وضعیت پیدا نشد.' : openOnly ? 'محمولهٔ بازی وجود ندارد.' : 'هنوز محموله‌ای ثبت نشده. برای ساخت اولین محموله، به «آماده‌سازی محموله» برو.'}</p>
            {(shipmentQuery || shipmentStatus) && <button type="button" onClick={() => { setShipmentStatus(''); setShipmentQuery(''); }} className="mt-2 min-h-11 rounded-lg bg-white px-3 font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">پاک کردن فیلترها</button>}
          </div>
        ) : visibleShipments.map(shipment => {
          const expanded = detailsId === shipment.id;
          const panelId = `${receiptId}-${shipment.id}`;
          return <article key={shipment.id} className="space-y-3 rounded-2xl border border-[#e6e2f0] bg-white p-3 sm:p-4">
            <div className="flex flex-wrap items-center justify-between gap-x-5 gap-y-3">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h4 className="flex flex-wrap items-baseline gap-1.5 text-[#261b41]">
                    <span className="text-xs font-medium text-slate-600">کارتن</span>
                    <bdi dir="ltr" className="break-all font-mono text-base font-extrabold">{shipment.shipment_code}</bdi>
                  </h4>
                  <span className={`rounded-lg px-2 py-1 text-xs font-bold ${shipment.status === 'draft' ? 'bg-amber-50 text-amber-900' : 'bg-slate-100 text-slate-700'}`}>{SHIPMENT_STATUS_LABELS[shipment.status] || shipment.status}</span>
                </div>
                <p className="mt-1 text-xs text-slate-600">{toPersianDigits(shipment.item_count ?? 0)} تنوع{shipment.created_at ? ` · ${new Date(shipment.created_at).toLocaleDateString('fa-IR')}` : ''}</p>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                {shipment.status === 'draft' && <button type="button" disabled={pendingShipmentId !== null} onClick={() => void changeDispatch(shipment.id, 'dispatch')} className="min-h-11 rounded-xl bg-brand-deep px-3 text-xs font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60">ثبت ارسال</button>}
                <button type="button" aria-expanded={expanded} aria-controls={panelId}
                  onClick={() => setDetailsId(current => current === shipment.id ? '' : shipment.id)}
                  className={shipmentButton}>
                  {expanded ? 'بستن جزئیات' : 'محتویات و رسید'}
                </button>
                {shipment.status !== 'cancelled' && <button type="button" onClick={() => print(shipment)} className={shipmentButton}>
                  <Printer aria-hidden="true" className="size-4" strokeWidth={1.75} /> چاپ برچسب کارتن
                </button>}
                {pendingShipmentId === shipment.id && <span role="status" className="text-xs text-slate-600">در حال ثبت…</span>}
              </div>
            </div>
            <details className="group">
              <summary className="flex min-h-11 w-fit cursor-pointer list-none items-center gap-1.5 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
                گزینه‌های بیشتر
                <ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" />
              </summary>
              <div className="hidden flex-wrap items-center gap-2 pt-2 group-open:flex">
                <ContextSupportButton role="supplier" kind="shipment" referenceType="carton" referenceId={shipment.id} />
                {shipment.status === 'draft' && <button type="button" disabled={pendingShipmentId !== null} onClick={() => setCancelTarget(shipment)} className="min-h-11 rounded-xl px-3 text-xs font-bold text-rose-700 hover:bg-rose-50 focus-visible:ring-2 focus-visible:ring-rose-700 disabled:opacity-60">لغو محموله</button>}
              </div>
            </details>
            <div id={panelId} hidden={!expanded}>
              {expanded && stage !== 'prepare' && <SupplierCartonDetails shipmentId={shipment.id} />}
            </div>
          </article>;
        }))}
        <details className="rounded-xl border border-slate-200 px-3 text-xs leading-6 text-slate-700">
          <summary className="min-h-11 cursor-pointer rounded-lg py-2.5 font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">راهنمای کد و برچسب</summary>
          <ul className="space-y-1 pb-3">
            <li><strong>کد کارتن</strong> برای شناسایی محموله است. برچسبش را روی کارتن بچسبان و هنگام تحویل بار، «ثبت ارسال» را بزن.</li>
            <li><strong>SPNC</strong> کد مشترک واحدهای یک تنوع است.</li>
            <li><strong>SPNCX</strong> کد یکتای هر واحد است. برچسب واحدهای هر کارتن در «محتویات و رسید» قرار دارد.</li>
          </ul>
        </details>
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
