import React, { useCallback, useEffect, useRef, useState } from 'react';
import { CheckCircle2, ChevronDown, ClipboardList, Printer, RefreshCw, Truck } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { ApiError, apiRequest } from '../../services/apiClient';
import { wmsApi, type ShippingManifestDto } from '../../services/wmsApi';
import { toPersianDigits } from '../../utils/formatters';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

interface ServerShipItem {
  orderItemId?: string;
  productId?: string;
  productTitle?: string;
  variantName?: string;
  variantSpnc?: string;
  quantity?: number;
}

interface ServerShipOrder {
  id: string;
  orderNumber: string;
  storeName?: string;
  customerName?: string;
  customerCity?: string;
  trackingCode?: string;
  items?: ServerShipItem[];
}

interface Props {
  selectedOrderNumber?: string;
  selectedOrderId?: string;
}

interface ShippedOrder {
  orderNumber: string;
  carrier: string;
  trackingNumber?: string;
}

const primaryButton = 'flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60';
const secondaryButton = 'flex min-h-11 items-center justify-center gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';
const fieldClass = 'mt-1 min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-normal text-slate-900 focus-visible:ring-2 focus-visible:ring-brand';

export const WarehouseShippingTab: React.FC<Props> = ({ selectedOrderNumber, selectedOrderId }) => {
  const [orders, setOrders] = useState<ServerShipOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [showQueue, setShowQueue] = useState(false);
  const [chosenId, setChosenId] = useState<string | null>(null);
  const [shipped, setShipped] = useState<ShippedOrder | null>(null);
  const [busy, setBusy] = useState(false);
  const receiptHeading = useRef<HTMLHeadingElement>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try {
      const data = await apiRequest<{ orders?: ServerShipOrder[] } | ServerShipOrder[]>('/warehouse/orders/ready-to-ship', { auth: true });
      setOrders(Array.isArray(data) ? data : (data.orders || []));
    } catch (cause) {
      setLoadError(cause instanceof Error ? cause.message : 'بسته‌های آماده تحویل دریافت نشدند.');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void reload(); }, [reload]);
  useEffect(() => { if (shipped) receiptHeading.current?.focus(); }, [shipped]);

  const fromPacking = !showQueue && Boolean(selectedOrderId || selectedOrderNumber);
  const activeOrder = fromPacking
    ? orders.find(order => selectedOrderId ? order.id === selectedOrderId : order.orderNumber === selectedOrderNumber)
    : chosenId !== null ? orders.find(order => order.id === chosenId) : orders.length === 1 ? orders[0] : undefined;

  const returnToQueue = () => {
    setChosenId(null);
    setShowQueue(true);
    setShipped(null);
    void reload();
  };

  return <div className="space-y-4 pb-6">
    <SupplierSectionHeading title="رهگیری و تحویل" icon={<Truck />} description="پس از تحویل فیزیکی بسته، شرکت حمل و کد رهگیری آن را ثبت کنید."
      action={!shipped && <button type="button" aria-label="به‌روزرسانی صف تحویل" onClick={() => void reload()} disabled={loading || busy}
        className="grid size-11 place-items-center rounded-xl border border-slate-200 bg-white text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
        <RefreshCw aria-hidden="true" className="size-4" />
      </button>} />
    {shipped ? <section className="space-y-3 rounded-[22px] border border-emerald-200 bg-white p-4 shadow-sm sm:p-5">
      <div role="status" className="space-y-2">
        <h3 ref={receiptHeading} tabIndex={-1} className="flex items-center gap-2 text-base font-extrabold text-emerald-800">
          <CheckCircle2 aria-hidden="true" className="size-5 shrink-0" /> تحویل سفارش <bdi className="font-mono">{shipped.orderNumber}</bdi> ثبت شد
        </h3>
        <p className="break-words text-sm text-slate-700">شرکت حمل <strong>{shipped.carrier}</strong></p>
        {shipped.trackingNumber && <p className="break-words text-sm text-slate-700">کد رهگیری <bdi dir="ltr" className="font-mono font-bold">{shipped.trackingNumber}</bdi></p>}
      </div>
      <button type="button" onClick={returnToQueue} className={secondaryButton}>بازگشت به صف تحویل</button>
    </section> : <>
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {!loading && !loadError && <>
        {!fromPacking && orders.length > 1 && <label className="block rounded-2xl border border-[#e6e2f0] bg-white p-4 text-sm font-bold text-slate-800">
          بسته آماده تحویل
          <select aria-label="بسته آماده تحویل" className={fieldClass} value={chosenId ?? ''} disabled={busy} onChange={event => setChosenId(event.target.value || null)}>
            <option value="">سفارش را انتخاب کنید</option>
            {orders.map(order => <option key={order.id} value={order.id}>{[order.orderNumber, order.storeName, order.customerName].filter(Boolean).join(' · ')}</option>)}
          </select>
        </label>}
        {activeOrder ? <ShipOrderForm key={activeOrder.id} order={activeOrder} busy={busy} setBusy={setBusy} onShipped={setShipped} />
          : fromPacking || chosenId ? <SupplierEmptyState icon={<Truck />} title="سفارش انتخاب‌شده در صف تحویل نیست."
            action={<button type="button" onClick={returnToQueue} className={`${secondaryButton} mx-auto`}>مشاهدهٔ صف تحویل</button>} />
            : !orders.length && <SupplierEmptyState icon={<Truck />} title="بسته‌ای آماده تحویل نیست." description="سفارش پس از بسته‌بندی در این صف قرار می‌گیرد." />}
        {fromPacking && activeOrder && <button type="button" className={secondaryButton} disabled={busy} onClick={returnToQueue}>مشاهدهٔ صف تحویل</button>}
      </>}
    </>}
    <ShippingManifests />
  </div>;
};

function ShipOrderForm({ order, busy, setBusy, onShipped }: {
  order: ServerShipOrder;
  busy: boolean;
  setBusy: (value: boolean) => void;
  onShipped: (order: ShippedOrder) => void;
}) {
  const { success } = useToast();
  const [carrier, setCarrier] = useState('پست پیشتاز');
  const [trackingNumber, setTrackingNumber] = useState('');
  const [formError, setFormError] = useState('');
  const submitting = useRef(false);

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submitting.current) return;
    const carrierName = carrier.trim();
    if (!carrierName) { setFormError('نام شرکت حمل را وارد کنید.'); return; }
    const carrierTracking = trackingNumber.trim();
    setFormError('');
    submitting.current = true;
    setBusy(true);
    try {
      const result = await apiRequest<{ success?: boolean; status?: string; carrier?: string; trackingNumber?: string }>(`/warehouse/orders/${encodeURIComponent(order.id)}/ship`, {
        method: 'POST', auth: true,
        body: JSON.stringify({ carrier: carrierName, ...(carrierTracking ? { trackingNumber: carrierTracking } : {}) }),
      });
      if (!result?.success || result.status !== 'shipped') throw new Error('تحویل بسته تأیید نشد؛ صف را دوباره بررسی کنید.');
      onShipped({ orderNumber: order.orderNumber, carrier: result.carrier || carrierName, trackingNumber: result.trackingNumber || order.trackingCode });
      success(`تحویل سفارش ${order.orderNumber} ثبت شد.`);
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : 'ثبت تحویل به شرکت حمل ناموفق بود.');
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  };

  return <form onSubmit={submit} className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5" aria-busy={busy}>
    <div className="space-y-2 border-b border-[#e6e2f0] pb-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="break-words text-base font-extrabold text-slate-900">سفارش <bdi className="font-mono">{order.orderNumber}</bdi></h3>
        <span className="rounded-full bg-brand-soft px-2.5 py-1 text-xs font-bold text-brand-deep">آماده تحویل</span>
      </div>
      {(order.storeName || order.customerName || order.customerCity) && <p className="break-words text-sm text-slate-600">{[order.storeName, order.customerName, order.customerCity].filter(Boolean).join(' · ')}</p>}
      {order.trackingCode && <p className="break-words text-sm text-slate-700">کد رهگیری سامانه <bdi dir="ltr" className="font-mono font-bold">{order.trackingCode}</bdi></p>}
    </div>
    {!!order.items?.length && <details className="group rounded-xl border border-slate-200 px-3">
      <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-2 rounded-lg text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
        اقلام بسته <ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" />
      </summary>
      <ul className="space-y-2 pb-3 text-sm text-slate-700">
        {order.items.map((item, index) => <li key={item.orderItemId || `${item.productId || 'item'}-${index}`} className="flex flex-wrap gap-x-2 gap-y-1 break-words rounded-lg bg-slate-50 px-2.5 py-2">
          <span>{item.productTitle || 'کالا'}{item.variantName && ` · ${item.variantName}`}</span>
          <span className="font-bold">{toPersianDigits(item.quantity || 0)} عدد</span>
          {item.variantSpnc && <bdi className="font-mono text-xs text-brand-deep">{item.variantSpnc}</bdi>}
        </li>)}
      </ul>
    </details>}
    <fieldset disabled={busy} className="grid gap-3 sm:grid-cols-2">
      <label className="text-sm font-semibold text-slate-800">شرکت حمل
        <input name="carrier" value={carrier} onChange={event => setCarrier(event.target.value)} required className={fieldClass} />
      </label>
      <label className="text-sm font-semibold text-slate-800">کد رهگیری شرکت حمل <span className="font-normal text-slate-600">(اختیاری)</span>
        <input name="trackingNumber" dir="ltr" value={trackingNumber} onChange={event => setTrackingNumber(event.target.value)}
          onKeyDown={event => { if (event.key === 'Enter') event.preventDefault(); }} maxLength={50} autoComplete="off" className={fieldClass} />
      </label>
    </fieldset>
    {formError && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{formError}</p>}
    <button type="submit" disabled={busy} className={`${primaryButton} w-full sm:w-auto`}>
      <Truck aria-hidden="true" className="size-4" />{busy ? 'در حال ثبت…' : 'ثبت تحویل به شرکت حمل'}
    </button>
  </form>;
}

function ShippingManifests() {
  const { success, error } = useToast();
  const [manifests, setManifests] = useState<ShippingManifestDto[]>([]);
  const [requested, setRequested] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [denied, setDenied] = useState(false);
  const [dispatchingId, setDispatchingId] = useState<string | null>(null);
  const submitting = useRef(false);

  const reload = async () => {
    setRequested(true);
    setLoading(true);
    setLoadError('');
    setDenied(false);
    try {
      const result = await wmsApi.getManifests();
      setManifests(Array.isArray(result) ? result : []);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 403) setDenied(true);
      else setLoadError(cause instanceof Error ? cause.message : 'مانیفست‌ها دریافت نشدند.');
    } finally {
      setLoading(false);
    }
  };

  const dispatch = async (batch: ShippingManifestDto) => {
    if (submitting.current) return;
    submitting.current = true;
    setDispatchingId(batch.id);
    try {
      const result = await wmsApi.dispatchManifest(batch.id);
      if (!result?.success || result.status !== 'handed_over') throw new Error('تحویل مانیفست تأیید نشد؛ فهرست را دوباره بررسی کنید.');
      success(`تحویل مانیفست ${batch.id} ثبت شد.`);
      await reload();
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت خروج مانیفست ناموفق بود.');
    } finally {
      submitting.current = false;
      setDispatchingId(null);
    }
  };

  return <details className="group rounded-[22px] border border-[#e6e2f0] bg-white px-4 shadow-sm" onToggle={event => {
    if (event.currentTarget.open && !requested) void reload();
  }}>
    <summary className="flex min-h-12 cursor-pointer list-none items-center justify-between gap-2 rounded-lg text-sm font-extrabold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
      <span className="flex items-center gap-2"><ClipboardList aria-hidden="true" className="size-4" />مانیفست‌های خروج</span>
      <ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" />
    </summary>
    <div className="space-y-3 border-t border-[#e6e2f0] py-3">
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {denied && <p role="status" className="text-sm text-slate-700">مانیفست‌های خروج برای این نشست در دسترس نیستند.</p>}
      {!loading && !loadError && !denied && requested && <>
        <button type="button" onClick={() => void reload()} disabled={!!dispatchingId} className={secondaryButton}>
          <RefreshCw aria-hidden="true" className="size-4" /> به‌روزرسانی مانیفست‌ها
        </button>
        {manifests.length ? <ul className="space-y-2">
          {manifests.map(batch => <li key={batch.id} className="space-y-2 rounded-xl bg-slate-50 p-3 text-sm">
            <div className="flex flex-wrap justify-between gap-2">
              <bdi className="break-all font-mono font-bold text-slate-900">{batch.id}</bdi>
              <span className="text-slate-700">{batch.status === 'handed_over' ? 'تحویل داده شد' : batch.status === 'pending_pickup' ? 'در انتظار تحویل' : 'وضعیت نامشخص'}</span>
            </div>
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-slate-700">
              {batch.carrier && <span>{batch.carrier}</span>}
              {batch.driverName && <span>نماینده {batch.driverName}</span>}
              <span>{toPersianDigits(batch.packagesCount)} مرسوله</span>
              {batch.createdAt && <time dateTime={batch.createdAt}>{new Date(batch.createdAt).toLocaleDateString('fa-IR')}</time>}
            </div>
            <div className="flex flex-wrap gap-2">
              {batch.status === 'pending_pickup' && <button type="button" onClick={() => void dispatch(batch)} disabled={!!dispatchingId} className={primaryButton}>
                <CheckCircle2 aria-hidden="true" className="size-4" />{dispatchingId === batch.id ? 'در حال ثبت…' : 'ثبت تحویل مانیفست'}
              </button>}
              <button type="button" onClick={() => window.print()} className={secondaryButton}><Printer aria-hidden="true" className="size-4" />چاپ مانیفست</button>
            </div>
          </li>)}
        </ul> : <p role="status" className="text-sm text-slate-600">مانیفستی وجود ندارد.</p>}
      </>}
    </div>
  </details>;
}
