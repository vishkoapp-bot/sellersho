import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, CheckCircle2, ChevronDown, RefreshCw, ScanLine } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { ApiError, apiRequest } from '../../services/apiClient';
import { listUnits, type ListUnitsResponse } from '../../services/unitsApi';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { PickingRoutePlanner } from './WarehouseOptimizationTools';

// Live contract (live bundles, read-only):
// - GET /warehouse/orders/ready-to-pick -> { orders } (ReturnWindowBadge-MERGED.js, getReadyToPick)
// - POST /warehouse/orders/{id}/start-picking (no body)
// - POST /warehouse/orders/{id}/complete-picking (no body)
// Item rows render only server fields: productTitle, variantName, variantSpnc,
// binLocation (or the honest "not recorded" line when absent), fulfillmentMethod,
// quantity. No bin or barcode is ever fabricated here.
interface ServerPickItem {
  orderItemId?: string;
  productId?: string;
  productTitle?: string;
  variantName?: string;
  variantSpnc?: string;
  binLocation?: string;
  fulfillmentMethod?: string;
  quantity?: number;
}

interface ServerPickOrder {
  id: string;
  orderNumber: string;
  storeName?: string;
  customerName?: string;
  paymentStatus?: string;
  fulfillmentMethod?: string;
  totalAmount?: number;
  status: string;
  items?: ServerPickItem[];
}

interface Props {
  onGoToPacking: (orderNumber: string, orderId: string) => void;
}

const QUANTITY_ONLY_NOTE = 'بدون کد واحد (سفارش پیش از سریال‌دار شدن)';
const nf = new Intl.NumberFormat('fa-IR');

function PickingUnitChecklist({ orderItemId }: { orderItemId: string }) {
  const [result, setResult] = useState<{ codes: string[]; truncated?: boolean; error?: string } | null>(null);
  const [checkedCodes, setCheckedCodes] = useState<string[]>([]);

  useEffect(() => {
    let active = true;
    // The list endpoint defaults to 50 rows; a pick line can allocate more
    // than that, so follow next_cursor (server max 200/page, bounded at 10
    // pages) until the allocation is fully loaded. A truncated load is shown
    // as a warning, never as a complete checklist.
    void (async () => {
      try {
        const codes: string[] = [];
        let cursor: string | undefined;
        let truncated = false;
        for (let page = 0; page < 10; page++) {
          const response: ListUnitsResponse = await listUnits({
            order_item_id: orderItemId, limit: 200, ...(cursor ? { cursor } : {}),
          });
          codes.push(...response.units.map(unit => unit.code));
          cursor = response.next_cursor ?? undefined;
          if (!cursor) break;
          if (page === 9) truncated = true;
        }
        if (active) setResult({ codes, truncated });
      } catch (cause: unknown) {
        const rawCode = cause instanceof ApiError && typeof cause.body?.error === 'string' ? cause.body.error : null;
        if (active) setResult({ codes: [], error: rawCode ?? (cause instanceof Error ? cause.message : 'دریافت کدهای واحد انجام نشد.') });
      }
    })();
    return () => { active = false; };
  }, [orderItemId]);

  if (!result) return <p role="status" className="mt-1 text-slate-500">در حال دریافت کدهای واحد…</p>;
  if (result.error) return <p role="alert" className="mt-1 text-rose-700">دریافت کدهای واحد انجام نشد. <bdi>{result.error}</bdi></p>;
  if (!result.codes.length) return <p className="mt-1 text-slate-600">{QUANTITY_ONLY_NOTE}</p>;

  return <div className="mt-1 space-y-1">
    <p role="status" className="text-slate-500">{checkedCodes.length} از {result.codes.length} تیک خورد</p>
    {result.truncated && <p role="alert" className="text-amber-700">فهرست واحدها کامل بارگذاری نشد؛ پیش از جمع‌آوری، فهرست را دوباره بررسی کنید.</p>}
    <ul className="flex flex-wrap gap-2">
      {result.codes.map(code => <li key={code}>
        <label className="inline-flex min-h-11 cursor-pointer items-center gap-2 rounded-lg border border-slate-200 bg-white px-2.5 text-slate-800">
          <input type="checkbox" className="size-4 shrink-0 accent-brand-deep focus-visible:ring-2 focus-visible:ring-brand" checked={checkedCodes.includes(code)} onChange={event => {
            const checked = event.target.checked;
            setCheckedCodes(current => checked ? [...current, code] : current.filter(value => value !== code));
          }} />
          <code className="font-mono" dir="ltr">{code}</code>
        </label>
      </li>)}
    </ul>
  </div>;
}

export const WarehousePickingTab: React.FC<Props> = ({ onGoToPacking }) => {
  const { success, error } = useToast();
  const [orders, setOrders] = useState<ServerPickOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [busyId, setBusyId] = useState<string | null>(null);
  const submitting = useRef(false);
  const mounted = useRef(false);

  const reload = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try {
      const data = await apiRequest<{ orders?: ServerPickOrder[] } | ServerPickOrder[]>('/warehouse/orders/ready-to-pick', { method: 'GET', auth: true });
      setOrders(Array.isArray(data) ? data : (data.orders || []));
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'صف سفارش‌ها دریافت نشد.';
      setLoadError(message);
      error(message);
    } finally {
      setLoading(false);
    }
  }, [error]);

  useEffect(() => {
    mounted.current = true;
    void reload();
    return () => { mounted.current = false; };
  }, [reload]);

  const startPicking = async (order: ServerPickOrder) => {
    if (submitting.current) return;
    submitting.current = true;
    setBusyId(order.id);
    try {
      const result = await apiRequest<{ success?: boolean; status?: string }>(`/warehouse/orders/${encodeURIComponent(order.id)}/start-picking`, { method: 'POST', auth: true });
      if (!result?.success || result.status !== 'picking') throw new Error('آغاز جمع‌آوری تأیید نشد؛ صف را دوباره بررسی کنید.');
      setOrders(current => current.map(entry => entry.id === order.id ? { ...entry, status: 'picking' } : entry));
      success('جمع‌آوری سفارش آغاز شد.');
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'آغاز جمع‌آوری ناموفق بود. دوباره تلاش کنید.');
    } finally {
      submitting.current = false;
      setBusyId(null);
    }
  };

  const completePicking = async (order: ServerPickOrder) => {
    if (submitting.current) return;
    submitting.current = true;
    setBusyId(order.id);
    try {
      const result = await apiRequest<{ success?: boolean; status?: string }>(`/warehouse/orders/${encodeURIComponent(order.id)}/complete-picking`, { method: 'POST', auth: true });
      if (!result?.success || result.status !== 'picked') throw new Error('پایان جمع‌آوری تأیید نشد؛ صف را دوباره بررسی کنید.');
      success(`جمع‌آوری سفارش ${order.orderNumber} کامل شد.`);
      setOrders(current => current.filter(entry => entry.id !== order.id));
      if (mounted.current) onGoToPacking(order.orderNumber, order.id);
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت پایان جمع‌آوری ناموفق بود. دوباره تلاش کنید.');
    } finally {
      submitting.current = false;
      setBusyId(null);
    }
  };

  const firstOrderId = orders.find(order => order.status === 'picking')?.id ?? orders[0]?.id;
  return <div className="space-y-4 pb-6">
    <SupplierSectionHeading title="جمع‌آوری سفارش" icon={<ScanLine />} description="سفارش را باز کنید و اقلام را از جایگاه‌های ثبت‌شده بردارید."
      action={<button type="button" aria-label="به‌روزرسانی صف جمع‌آوری" onClick={() => void reload()} disabled={loading || !!busyId}
        className="grid size-11 place-items-center rounded-xl border border-slate-200 bg-white text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
        <RefreshCw aria-hidden="true" className="size-4" />
      </button>} />
    <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
    {!loading && !loadError && (orders.length ? <>
      <p className="text-sm font-bold text-brand-deep">{nf.format(orders.length)} سفارش در صف</p>
      <div className="space-y-3">
        {orders.map(order => <PickingOrderCard key={order.id} order={order} initiallyOpen={order.id === firstOrderId}
          busy={!!busyId} submitting={busyId === order.id} onStart={() => void startPicking(order)} onComplete={() => void completePicking(order)} />)}
      </div>
      <PickingRoutePlanner orderIds={orders.map(order => order.id)} />
    </> : <SupplierEmptyState icon={<ScanLine />} title="سفارشی در صف جمع‌آوری وجود ندارد." />)}
  </div>;
};

function PickingOrderCard({ order, initiallyOpen, busy, submitting, onStart, onComplete }: {
  order: ServerPickOrder;
  initiallyOpen: boolean;
  busy: boolean;
  submitting: boolean;
  onStart: () => void;
  onComplete: () => void;
}) {
  const [open, setOpen] = useState(initiallyOpen);
  const [openedBefore, setOpenedBefore] = useState(initiallyOpen);
  const picking = order.status === 'picking';
  return <details name="warehouse-picking-orders" open={open} onToggle={event => {
    const nextOpen = event.currentTarget.open;
    setOpen(nextOpen);
    if (nextOpen) setOpenedBefore(true);
  }} className="group rounded-[22px] border border-[#e6e2f0] bg-white shadow-sm">
    <summary className="flex min-h-14 cursor-pointer list-none flex-wrap items-center justify-between gap-2 rounded-t-[22px] px-4 py-3 text-sm focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
      <span className="min-w-0">
        <strong className="break-words text-slate-900">سفارش <bdi className="font-mono">{order.orderNumber}</bdi></strong>
        {(order.storeName || order.customerName) && <span className="mt-0.5 block break-words text-slate-600">{[order.storeName, order.customerName].filter(Boolean).join(' · ')}</span>}
      </span>
      <span className="flex items-center gap-2">
        <span className={`rounded-full px-2.5 py-1 text-xs font-bold ${picking ? 'bg-brand-soft text-brand-deep' : 'bg-slate-100 text-slate-700'}`}>
          {picking ? 'در حال جمع‌آوری' : 'در انتظار جمع‌آوری'}
        </span>
        <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-brand-deep group-open:rotate-180" />
      </span>
    </summary>
    {openedBefore && <div className="space-y-3 border-t border-[#e6e2f0] p-4" aria-busy={submitting}>
      {order.fulfillmentMethod === 'mixed' && <p className="rounded-xl bg-brand-bg px-3 py-2 text-sm text-brand-deep">سفارش ترکیبی است. اقلام انباری در این فهرست آمده‌اند.</p>}
      {order.items?.length ? <ul className="space-y-2">
        {order.items.map((item, index) => <li key={item.orderItemId || `${item.productId || 'item'}-${index}`} className="space-y-2 rounded-xl bg-slate-50 p-3 text-sm">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <strong className="min-w-0 break-words text-slate-900">{item.productTitle || 'کالا'}{item.variantName && ` · ${item.variantName}`}</strong>
            <span className="shrink-0 font-bold text-brand-deep">{nf.format(item.quantity || 0)} عدد</span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-slate-600">
            {item.binLocation ? <span>قفسه <bdi className="font-mono font-bold text-slate-800" dir="ltr">{item.binLocation}</bdi></span> : <span>جایگاه قفسه ثبت نشده است</span>}
            {item.variantSpnc && <bdi className="font-mono text-xs" dir="ltr">{item.variantSpnc}</bdi>}
            {item.fulfillmentMethod === 'supplier_direct' && <span>ارسال مستقیم تأمین‌کننده</span>}
          </div>
          {item.orderItemId ? <PickingUnitChecklist orderItemId={item.orderItemId} /> : <p className="text-slate-600">{QUANTITY_ONLY_NOTE}</p>}
        </li>)}
      </ul> : <p className="text-sm text-slate-600">قلمی برای این سفارش نمایش داده نشده است.</p>}
      {typeof order.totalAmount === 'number' && <p className="text-sm text-slate-600">مبلغ سفارش <strong>{nf.format(Math.round(order.totalAmount))} تومان</strong></p>}
      <button type="button" onClick={picking ? onComplete : onStart} disabled={busy}
        className="flex min-h-11 w-full items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60 sm:w-auto">
        {picking ? <CheckCircle2 aria-hidden="true" className="size-4" /> : <ScanLine aria-hidden="true" className="size-4" />}
        {submitting ? 'در حال ثبت…' : picking ? 'پایان جمع‌آوری' : 'شروع جمع‌آوری'}
        {picking && !submitting && <ArrowLeft aria-hidden="true" className="size-4" />}
      </button>
    </div>}
  </details>;
}
