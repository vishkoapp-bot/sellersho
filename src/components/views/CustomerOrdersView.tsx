import { HelpExplainer } from '../common/HelpExplainer';
import React, { useState, useEffect, useRef } from 'react';
import { useOrders } from '../../context/OrderContext';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { Order } from '../../types';
import { orderApi, CustomerReturnDto, ReturnDestinationInfo, OrderRefundSummary } from '../../services/orderApi';
import { Package, Truck, CheckCircle2, Clock, Copy, Printer, AlertCircle, RotateCcw } from 'lucide-react';
import { CustomerAccountLayout } from './CustomerProfileView';

const orderFilters = [
  { id: 'all', label: 'همه' },
  { id: 'pending', label: 'در انتظار پرداخت' },
  { id: 'processing', label: 'در حال آماده‌سازی' },
  { id: 'shipped', label: 'ارسال‌شده' },
  { id: 'delivered', label: 'تحویل‌شده' },
  { id: 'cancelled', label: 'لغوشده' },
  { id: 'delivery_disputed', label: 'در حال پیگیری' },
] as const;
const processingStatuses = ['processing', 'picking', 'picked', 'packed'];
const deliveredStatuses = ['delivered', 'delivered_confirmed', 'delivered_unconfirmed'];
const returnStatuses: Record<string, string> = {
  pending: 'در انتظار بررسی', awaiting_return_test: 'در انتظار بررسی', approved: 'تأیید شده',
  rejected: 'رد شده', packed: 'بسته‌بندی شده', pickup_pending: 'آمادهٔ تحویل', postal_pending: 'آمادهٔ ارسال',
  in_transit: 'در مسیر', received: 'دریافت شده', received_pending_qc: 'در حال بررسی کالا',
  completed: 'تکمیل شده', qc_failed: 'در بررسی کالا رد شد', cancelled: 'لغو شده',
};

const accountDate = (value: string) => {
  if (!/^\d{4}-\d{2}-\d{2}/.test(value)) return value;
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleDateString('fa-IR', { day: 'numeric', month: 'long', year: 'numeric' });
};

export const CustomerOrdersView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { currentStore, routeStoreSlug, openCatalogCategory, setTrackingOrder, setIsTrackingModalOpen, formatPrice, toPersianDigits, confirmOrderDelivery, disputeOrderDelivery, busyOrderId } = useStore();
  const { success, error: toastError } = useToast();
  const { orders, fetchMyOrders } = useOrders();
  const central = !routeStoreSlug && currentStore?.ownerType !== 'dropshipper';
  const [showReturns, setShowReturns] = useState(() => typeof window !== 'undefined' && window.location.hash === '#returns');
  const [loading, setLoading] = useState(true);
  const [returnsError, setReturnsError] = useState(false);
  const [returnsLoading, setReturnsLoading] = useState(true);
  const [returnsReload, setReturnsReload] = useState(0);
  const returnModalRef = useRef<HTMLDialogElement>(null);
  const showSection = (returns: boolean) => {
    setShowReturns(returns);
    const url = new URL(window.location.href);
    url.hash = returns ? 'returns' : '';
    window.history.replaceState(window.history.state, '', url);
  };
  const [myReturns, setMyReturns] = useState<CustomerReturnDto[]>([]);
  const [returnModalOrder, setReturnModalOrder] = useState<Order | null>(null);
  const [returnSelected, setReturnSelected] = useState<Record<string, boolean>>({});
  const [returnQty, setReturnQty] = useState<Record<string, number>>({});
  const [returnReason, setReturnReason] = useState('');
  const [returnPrepaid, setReturnPrepaid] = useState(false);
  const [returnDestination, setReturnDestination] = useState<ReturnDestinationInfo | null>(null);
  const returnQuoteSeq = useRef(0);
  const [returnShippingCost, setReturnShippingCost] = useState<number | null>(null);
  const [returnBusy, setReturnBusy] = useState(false);
  const [quoteState, setQuoteState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
  const [quoteRetry, setQuoteRetry] = useState(0);
  const [confirmingCancelId, setConfirmingCancelId] = useState<string | null>(null);
  const [cancelBusy, setCancelBusy] = useState(false);
  const [cancellationResults, setCancellationResults] = useState<Record<string,OrderRefundSummary>>({});

  useEffect(() => {
    let active = true;
    fetchMyOrders().finally(() => { if (active) setLoading(false); });
    const syncSection = () => setShowReturns(window.location.hash === '#returns');
    window.addEventListener('popstate', syncSection);
    window.addEventListener('hashchange', syncSection);
    return () => {
      active = false;
      ++returnQuoteSeq.current;
      window.removeEventListener('popstate', syncSection);
      window.removeEventListener('hashchange', syncSection);
    };
  }, []);
  useEffect(() => {
    let active = true;
    setReturnsError(false);
    setReturnsLoading(true);
    orderApi.getMyReturns()
      .then((rows) => { if (active) setMyReturns(rows); })
      .catch(() => { if (active) setReturnsError(true); })
      .finally(() => { if (active) setReturnsLoading(false); });
    return () => { active = false; };
  }, [returnsReload]);
  const [filter, setFilter] = useState<typeof orderFilters[number]['id']>('all');

  // The authenticated backend selects ownership by immutable user ID. Recipient
  // names and phones are editable shipping details, never ownership selectors.
  const myOrders = orders.map(o => cancellationResults[o.id] ? {...o,status:'cancelled' as const,refund:o.status==='cancelled' ? o.refund ?? cancellationResults[o.id] : cancellationResults[o.id]} : o);

  // Fix82: per-store buyer panels. One account may hold panels at
  // central, mone and other dropshippers, but each panel shows ONLY
  // that store's orders (orders already carry storeId, ISO-B).
  // Fail closed: with no current store resolved, show nothing rather
  // than leaking another store's orders into this panel.
  const storeOrders = currentStore
    ? myOrders.filter((o) => o.storeId === currentStore.id)
    : [];
  const storeOrderIds = new Set(storeOrders.map((o) => o.id));
  const storeReturns = myReturns.filter((r) => storeOrderIds.has(r.orderId));

  // FIX 5 (Team 03 G4): cancel is offered for the backend's
  // cancellable statuses; return is offered for delivered_confirmed
  // orders inside their return window (the backend enforces the
  // window too, so an unknown window still shows the button).
  const cancellableStatuses = ['pending', ...processingStatuses];
  const canCancel = (o: Order) => cancellableStatuses.includes(o.status);
  const returnDaysLeft = (o: Order): number | null => {
    if (!o.returnWindowEndsAt) return null;
    const remaining = Date.parse(o.returnWindowEndsAt) - Date.now();
    if (!Number.isFinite(remaining)) return null;
    return remaining <= 0 ? -1 : Math.ceil(remaining / 86400000);
  };
  const canReturn = (o: Order) => {
    if (o.status !== 'delivered_confirmed') return false;
    const left = returnDaysLeft(o);
    if (left !== null && left < 0) return false;
    return o.items.some((it) => !!it.orderItemId);
  };

  const doCancel = async (ord: Order) => {
    setCancelBusy(true);
    try {
      const result = await orderApi.cancelOrder(ord.id);
      if (result?.success !== true || result.status !== 'cancelled' || result.orderId !== ord.id || !result.refund) throw new Error('نتیجه لغو سفارش تأیید نشد؛ وضعیت سفارش را دوباره بررسی کنید.');
      setCancellationResults(previous=>({...previous,[ord.id]:result.refund}));
      success(result.refund.status==='none' ? `سفارش ${ord.orderNumber} لغو شد.` : `سفارش ${ord.orderNumber} لغو شد؛ نتیجه بازپرداخت را در همین سفارش پیگیری کنید.`);
      setConfirmingCancelId(null);
      await fetchMyOrders();
    } catch (e: any) {
      toastError(e?.message || 'لغو سفارش انجام نشد.');
    } finally {
      setCancelBusy(false);
    }
  };

  const openReturnModal = (ord: Order) => {
    const sel: Record<string, boolean> = {};
    const qty: Record<string, number> = {};
    ord.items.forEach((it) => {
      if (it.orderItemId) { sel[it.orderItemId] = false; qty[it.orderItemId] = 1; }
    });
    setReturnModalOrder(ord);
    setReturnSelected(sel);
    setReturnQty(qty);
    setReturnReason('');
    setReturnPrepaid(false);
    setReturnDestination(null);
    setReturnShippingCost(null);
  };

  useEffect(() => {
    if (returnModalOrder && returnModalRef.current && !returnModalRef.current.open) returnModalRef.current.showModal();
  }, [returnModalOrder]);

  // Only a quote for the selected lines may display a destination or fare.
  useEffect(() => {
    if (!returnModalOrder) return;
    const seq = ++returnQuoteSeq.current;
    const items = Object.entries(returnSelected)
      .filter(([, selected]) => selected)
      .map(([id]) => ({ order_item_id: id }));
    setReturnDestination(null);
    setReturnShippingCost(null);
    setQuoteState(items.length > 0 ? 'loading' : 'idle');
    if (items.length > 0) {
      orderApi.requestReturnQuote(returnModalOrder.id, items).then((quote) => {
        if (returnQuoteSeq.current !== seq) return;
        setReturnShippingCost(quote.totalFareToman);
        setReturnDestination(quote.destination ?? null);
        setQuoteState('ready');
      }).catch(() => {
        if (returnQuoteSeq.current === seq) setQuoteState('error');
      });
    }
    return () => { ++returnQuoteSeq.current; };
  }, [returnSelected, returnModalOrder, quoteRetry]);

  const submitReturn = async () => {
    if (!returnModalOrder || (returnPrepaid && quoteState !== 'ready')) return;
    const items = Object.entries(returnSelected)
      .filter(([, v]) => v)
      .map(([id]) => ({ order_item_id: id, qty: Math.max(1, returnQty[id] || 1) }));
    if (items.length === 0) {
      toastError('لطفاً دست‌کم یک قلم را برای مرجوعی انتخاب کنید.');
      return;
    }
    if (!returnReason.trim()) {
      toastError('لطفاً دلیل مرجوعی را بنویسید.');
      return;
    }
    setReturnBusy(true);
    try {
      const created = await orderApi.requestReturn(returnModalOrder.id, {
        items,
        reason: returnReason.trim(),
        prepaid: returnPrepaid,
      });
      const destLabel = created.destinationType
        ? ` مقصد مرجوعی: ${created.destinationType === 'supplier' ? 'انبار تأمین‌کننده' : 'انبار مرکزی'}${created.destinationName ? ` — ${created.destinationName}` : ''}${created.destinationCity ? ` (${created.destinationCity})` : ''}`
        : '';
      success(`درخواست مرجوعی با موفقیت ثبت شد.${destLabel}`);
      setReturnModalOrder(null);
      await fetchMyOrders();
      setReturnsReload(value => value + 1);
      showSection(true);
    } catch (e: any) {
      toastError(e?.message || 'ثبت مرجوعی انجام نشد.');
    } finally {
      setReturnBusy(false);
    }
  };

  const matchesFilter = (status: Order['status'], activeFilter: typeof filter) => {
    if (activeFilter === 'all') return true;
    if (activeFilter === 'delivered') return deliveredStatuses.includes(status);
    if (activeFilter === 'processing') return processingStatuses.includes(status);
    return status === activeFilter;
  };
  const filtered = storeOrders.filter((order) => matchesFilter(order.status, filter));
  const visibleFilters = orderFilters.filter((entry) =>
    entry.id === filter || storeOrders.some((order) => matchesFilter(order.status, entry.id)),
  );
  const printOrder = (event: React.MouseEvent<HTMLButtonElement>) => {
    const article = event.currentTarget.closest('article');
    if (!article) return;
    article.setAttribute('data-customer-order-print', '');
    const reset = () => article.removeAttribute('data-customer-order-print');
    window.addEventListener('afterprint', reset, { once: true });
    try { window.print(); } catch {
      window.removeEventListener('afterprint', reset);
      reset();
      toastError('پنجرهٔ چاپ باز نشد.');
    }
  };

  const copyTracking = async (code: string) => {
    try {
      await navigator.clipboard.writeText(code);
      success('کد رهگیری کپی شد.');
    } catch {
      toastError('کپی خودکار در دسترس نیست. می‌توانید کد را دستی کپی کنید.');
    }
  };

  const getBadge = (status: Order['status']) => {
    if (status === 'pending' || status === 'cancelled' || status === 'delivery_disputed') {
      return <span className="rounded-full bg-slate-100 px-2.5 py-1 text-xs font-bold text-slate-800">
        {status === 'pending' ? 'در انتظار پرداخت' : status === 'cancelled' ? 'لغوشده' : 'در حال پیگیری تحویل'}
      </span>;
    }
    if (status === 'delivered' || status === 'delivered_confirmed') {
      return <span className="bg-emerald-50 text-emerald-700 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border border-emerald-200"><CheckCircle2 className="w-3 h-3 text-emerald-600"  strokeWidth={1.5}/>تحویل قطعی شد</span>;
    }
    if (status === 'delivered_unconfirmed') {
      return <span className="inline-flex items-center gap-1 rounded-full border border-[var(--account-line)] bg-[var(--account-soft)] px-2 py-1 text-xs font-bold text-[var(--account-accent)]"><CheckCircle2 className="size-3" strokeWidth={1.5} aria-hidden="true" />رسیده به مقصد</span>;
    }
    if (status === 'shipped') {
      return <span className="bg-blue-50 text-blue-700 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border border-blue-200"><Truck className="w-3 h-3"  strokeWidth={1.5}/>ارسال پستی</span>;
    }
    return <span className="bg-amber-50 text-amber-700 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border border-amber-200"><Clock className="w-3 h-3"  strokeWidth={1.5}/>پردازش انبار</span>;
  };

  return (
    <CustomerAccountLayout title="سفارش‌های من" onBack={onBack} central={central}>
      <style>{`@media print {
        body:has([data-customer-order-print]) *:not(:has([data-customer-order-print])):not([data-customer-order-print], [data-customer-order-print] *) { display: none !important; }
        body:has([data-customer-order-print]), body *:has([data-customer-order-print]) { display: block !important; min-height: 0 !important; }
        [data-customer-order-print] { border: 0 !important; box-shadow: none !important; }
      }`}</style>
      <nav aria-label="سفارش و مرجوعی" className="mb-4 flex gap-2 rounded-xl border border-[var(--account-line)] bg-[var(--account-soft)] p-1.5 sm:mb-6 sm:w-fit">
        {[{ returns: false, label: 'سفارش‌ها', icon: Package }, { returns: true, label: 'مرجوعی‌های من', icon: RotateCcw }].map(({ returns, label, icon: Icon }) => (
          <button key={label} type="button" aria-pressed={returns === showReturns} onClick={() => showSection(returns)}
            className={`flex min-h-11 flex-1 items-center justify-center gap-2 rounded-lg px-3 text-sm font-bold sm:flex-none sm:px-5 ${returns === showReturns ? 'bg-white text-[var(--account-accent)] shadow-xs' : 'text-slate-700 hover:bg-white/70'}`}>
            <Icon className="size-4 shrink-0" aria-hidden="true" /> {label}
          </button>
        ))}
      </nav>

      {showReturns ? (
        <section id="returns" aria-labelledby="customer-returns-heading" className="space-y-3">
          <h2 id="customer-returns-heading" className="text-base font-extrabold sm:text-xl">مرجوعی‌های من</h2>
          {returnsError && (
            <div role="status" className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
              دریافت وضعیت مرجوعی انجام نشد.
              <button type="button" className="min-h-11 font-bold underline" onClick={() => setReturnsReload(value => value + 1)}>تلاش دوباره</button>
            </div>
          )}
          {storeReturns.length === 0 && !loading && !returnsLoading && !returnsError && (
            <div className="rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white p-5 sm:p-7">
              <p className="font-bold">درخواستی برای مرجوعی ثبت نکرده‌اید.</p>
              <p className="mt-1 text-sm leading-7 text-slate-600">برای کالای قابل مرجوعی، سفارش تحویل‌شده را باز کنید.</p>
              <button type="button" onClick={() => { setFilter('delivered'); showSection(false); }} className="mt-2 min-h-11 text-sm font-bold text-[var(--account-accent)] hover:underline">مشاهدهٔ سفارش‌های تحویل‌شده</button>
            </div>
          )}
          {storeReturns.length > 0 && <ul className="divide-y divide-slate-100 overflow-hidden rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white">
            {storeReturns.map((record) => (
              <li key={record.id} className="flex flex-wrap items-center justify-between gap-3 p-4 sm:p-5">
                <div className="min-w-0">
                  <p className="break-words text-sm font-bold">{record.reason || 'درخواست مرجوعی'}</p>
                  <p className="mt-1 text-xs text-slate-600">سفارش <bdi className="font-semibold">{storeOrders.find(order => order.id === record.orderId)?.orderNumber}</bdi>{record.createdAt && <> · {accountDate(record.createdAt)}</>}</p>
                </div>
                <span className="rounded-full bg-[var(--account-soft)] px-3 py-1 text-xs font-bold text-[var(--account-accent)]">{returnStatuses[record.status] ?? 'در حال پیگیری'}</span>
              </li>
            ))}
          </ul>}
          {(loading || returnsLoading) && storeReturns.length === 0 && !returnsError && <p role="status" className="text-sm text-slate-600">در حال دریافت مرجوعی‌ها…</p>}
        </section>
      ) : (
      <>
        {storeOrders.length > 0 && (
          <div aria-label="فیلتر وضعیت سفارش" className="mb-4 flex flex-wrap items-center gap-2">
            {visibleFilters.map((entry) => (
              <button key={entry.id} type="button" aria-pressed={filter === entry.id} onClick={() => setFilter(entry.id)}
                className={`min-h-11 rounded-full border px-3.5 text-xs font-bold sm:px-4 sm:text-sm ${filter === entry.id ? 'border-[var(--account-accent)] bg-[var(--account-accent)] text-white' : 'border-slate-200 bg-white text-slate-700 hover:bg-[var(--account-soft)]'}`}>
                {entry.label}
              </button>
            ))}
          </div>
        )}
        <div className={`grid gap-3 ${central ? 'lg:grid-cols-2 lg:gap-4' : ''}`}>
        {loading && filtered.length === 0 ? (
          <p role="status" className="col-span-full rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white p-5 text-sm text-slate-700">در حال دریافت سفارش‌ها…</p>
        ) : filtered.length === 0 ? (
          <div className="col-span-full rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white p-6 text-center sm:p-9">
            <Package className="mx-auto mb-3 size-8 text-[var(--account-accent)]" aria-hidden="true" />
            <h2 className="font-extrabold">{filter === 'all' ? 'هنوز سفارشی در این فروشگاه ندارید' : 'سفارشی در این وضعیت یافت نشد'}</h2>
            {filter === 'all' && openCatalogCategory ? (
              <button type="button" onClick={() => openCatalogCategory('all')} className="mt-3 inline-flex min-h-11 items-center rounded-xl bg-[var(--account-accent)] px-4 text-sm font-bold text-white">مشاهده محصولات</button>
            ) : filter !== 'all' && (
              <button type="button" onClick={() => setFilter('all')} className="mt-2 min-h-11 text-sm font-bold text-[var(--account-accent)] hover:underline">دیدن همه سفارش‌ها</button>
            )}
          </div>
        ) : (
          filtered.map((ord) => (
            <article key={ord.id} className="min-w-0 space-y-3 rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white p-4 text-sm sm:p-5">
              <div className="flex flex-wrap items-start justify-between gap-2 border-b border-[var(--account-line)] pb-3">
                <div>
                  <h2 className="font-extrabold text-slate-900">سفارش <bdi className="select-text">{ord.orderNumber}</bdi></h2>
                  <time className="mt-1 block text-xs text-[var(--account-muted)]" dateTime={ord.createdAt}>{accountDate(ord.createdAt)}</time>
                </div>
                {getBadge(ord.status)}
              </div>

              <div className="space-y-2 rounded-xl bg-[var(--account-bg)] p-3 text-sm">
                {ord.items.map((item, idx) => (
                  <div key={idx} className="flex justify-between items-center">
                    <span className="text-slate-700 min-w-0 break-words">{item.product.title}</span>
                    <span className="text-slate-500 font-bold shrink-0 mr-2">{toPersianDigits(item.quantity)} عدد</span>
                  </div>
                ))}
              </div>

              {ord.status === 'delivered_confirmed' && returnDaysLeft(ord) !== null && (returnDaysLeft(ord) as number) >= 0 && (
                <p className="text-[10px] text-slate-500">مهلت تست: {toPersianDigits(returnDaysLeft(ord) as number)} روز باقی مانده است؛ تا پایان آن امکان درخواست مرجوعی فعال است.</p>
              )}

              <div className="flex flex-wrap items-center justify-between gap-2 pt-1 text-sm tabular-nums">
                <div>
                  <span className="block text-xs text-[var(--account-muted)]">مبلغ فاکتور:</span>
                  <strong className="text-base font-extrabold text-[var(--account-ink)]">{formatPrice(ord.payableAmount)} تومان</strong>
                </div>
                {ord.trackingCode && (
                  <div className="flex items-center gap-1 text-[var(--account-accent)]">
                    <bdi className="max-w-full break-all select-text font-bold">{ord.trackingCode}</bdi>
                    <button type="button" aria-label="کپی کد رهگیری" onClick={() => void copyTracking(ord.trackingCode)}
                      className="grid size-11 place-items-center rounded-lg hover:bg-[var(--account-soft)]">
                      <Copy className="size-4" aria-hidden="true" />
                    </button>
                  </div>
                )}
              </div>

              {/* Requirement 4: Delivery Confirmation & Dispute Triggers */}
              {(ord.status === 'shipped' || ord.status === 'delivered_unconfirmed' || ord.status === 'delivered') && (
                <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-xl space-y-2">
                  <div className="flex items-center justify-between text-[10px]">
                    <span className="font-bold text-slate-800">آیا این بسته به دست شما رسیده است؟</span>
                    <span className="text-slate-400 font-mono">کد: {ord.trackingCode}</span>
                  </div>
                  <div className="grid grid-cols-2 gap-1.5">
                    <button
                      type="button"
                      disabled={busyOrderId === ord.id}
                      onClick={async () => {
                        try {
                          await confirmOrderDelivery(ord.id);
                          success('تحویل مرسوله تأیید شد. مهلت مرجوعی را در جزئیات سفارش ببینید.');
                        } catch (e: any) { toastError(e?.message || 'تأیید تحویل انجام نشد.'); }
                      }}
                      className="min-h-11 px-3 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[11px] rounded-lg transition-colors flex items-center justify-center gap-1 shadow-2xs disabled:opacity-50"
                    >
                      <CheckCircle2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>کالا را دریافت کردم</span>
                    </button>
                    <button
                      type="button"
                      disabled={busyOrderId === ord.id}
                      onClick={async () => {
                        try {
                          const res = await disputeOrderDelivery(ord.id, 'خریدار اعلام کرد بسته پستی هنوز به دست ایشان نرسیده است.');
                          success(res?.id ? `گزارش عدم دریافت کالا ثبت شد (شماره پیگیری: ${res.id}). سفارش تا بررسی پشتیبانی، تأیید خودکار نمی‌شود.` : 'گزارش عدم دریافت کالا ثبت شد و سفارش در صف پیگیری قرار گرفت.');
                        } catch (e: any) { toastError(e?.message || 'ثبت گزارش عدم دریافت انجام نشد.'); }
                      }}
                      className="min-h-11 px-3 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[11px] rounded-lg border border-rose-200 transition-colors flex items-center justify-center gap-1 disabled:opacity-50"
                    >
                      <AlertCircle className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>هنوز دریافت نکرده‌ام</span>
                    </button>
                  </div>
                </div>
              )}

              {ord.refund && ord.refund.status !== 'none' && (
                <div role="status" data-refund-status={ord.refund.status} className="p-2 border border-amber-200 bg-amber-50 rounded-xl text-[11px] space-y-1">
                  <div className="flex items-center gap-1">
                  <strong>{({pending:'بازپرداخت در انتظار انجام',succeeded:'بازپرداخت انجام شد',failed:'بازپرداخت ناموفق؛ نیازمند پیگیری',unknown:'بازپرداخت نیازمند بررسی'} as const)[ord.refund.status]}</strong>
                    {ord.refund.status !== 'succeeded' && <HelpExplainer text="بازگشت وجه هنوز تأیید نشده است؛ وضعیت نهایی پس از بررسی یا تأیید پرداخت نمایش داده می‌شود." />}
                  </div>
                  <p>مبلغ ثبت‌شده: {formatPrice(ord.refund.amountToman)} تومان؛ مقصد: {({original_payment:'روش پرداخت اولیه',wallet:'کیف پول',review:'نیازمند بررسی',mixed:'چند روش پرداخت',none:'در دست بررسی'} as const)[ord.refund.destination]}</p>
                  {ord.refund.succeededToman > 0 && <p>مبلغ انجام‌شده: {formatPrice(ord.refund.succeededToman)} تومان</p>}
                </div>
              )}

              {ord.status === 'delivery_disputed' && (
                <div className="p-2 bg-amber-50 border border-amber-200/80 rounded-xl text-[10px] text-amber-900 flex items-center justify-between">
                  <div className="flex items-center gap-1 font-bold">
                    <AlertCircle className="w-3.5 h-3.5 text-amber-600"  strokeWidth={1.5}/>
                    <span>گزارش عدم دریافت ثبت شد (در حال پیگیری تیم پشتیبانی)</span>
                  </div>
                </div>
              )}

              {ord.status === 'delivered_confirmed' && (
                <div className="p-2 bg-emerald-50 border border-emerald-200/80 rounded-xl text-[10px] text-emerald-800 flex items-center justify-between">
                  <div className="flex items-center gap-1 font-bold">
                    <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600"  strokeWidth={1.5}/>
                    <span>دریافت کالا را تأیید کردید.</span>
                  </div>
                  {ord.confirmedAt && (
                    <span className="text-[9px] text-slate-400 font-mono">{ord.confirmedAt}</span>
                  )}
                </div>
              )}

              <div className="grid grid-cols-2 gap-2 border-t border-[var(--account-line)] pt-3 sm:flex sm:flex-wrap">
                <button
                  onClick={() => { setTrackingOrder(ord); setIsTrackingModalOpen(true); }}
                  className="min-h-11 px-3 bg-[var(--account-soft)] hover:brightness-95 text-[var(--account-accent)] font-bold text-[11px] rounded-xl flex items-center justify-center gap-1"
                >
                  <Truck className="w-3.5 h-3.5"  strokeWidth={1.5}/><span>رهگیری</span>
                </button>
                <button
                  type="button"
                  onClick={printOrder}
                  className="min-h-11 px-3 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1"
                >
                  <Printer className="w-3.5 h-3.5"  strokeWidth={1.5}/><span>چاپ فاکتور</span>
                </button>
                {canCancel(ord) && (
                  <button
                    type="button"
                    aria-expanded={confirmingCancelId === ord.id}
                    onClick={() => setConfirmingCancelId(ord.id)}
                    disabled={cancelBusy}
                    className="min-h-11 px-3 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1 border border-rose-200 disabled:opacity-50"
                  >
                    <span>لغو سفارش</span>
                  </button>
                )}
                {canReturn(ord) && (
                  <button
                    onClick={() => openReturnModal(ord)}
                    className="min-h-11 px-3 bg-amber-50 hover:bg-amber-100 text-amber-800 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1 border border-amber-200"
                  >
                    <span>ثبت مرجوعی</span>
                  </button>
                )}
              </div>
              {confirmingCancelId === ord.id && (
                <div className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-rose-900" role="group" aria-label="تأیید لغو سفارش">
                  <p className="text-sm font-semibold">سفارش لغو شود؟ این تغییر قابل بازگشت نیست.</p>
                  <div className="mt-2 flex flex-wrap gap-2">
                    <button type="button" disabled={cancelBusy} onClick={() => void doCancel(ord)}
                      className="min-h-11 rounded-lg bg-rose-800 px-3 text-xs font-bold text-white disabled:opacity-50">
                      {cancelBusy ? 'در حال لغو…' : 'تأیید لغو سفارش'}
                    </button>
                    <button type="button" disabled={cancelBusy} onClick={() => setConfirmingCancelId(null)}
                      className="min-h-11 rounded-lg border border-rose-200 bg-white px-3 text-xs font-bold">انصراف</button>
                  </div>
                </div>
              )}
            </article>
          ))
        )}
      </div>
      </>
      )}

      {returnModalOrder && (
        <dialog ref={returnModalRef} aria-labelledby="customer-return-title" onCancel={(event) => { event.preventDefault(); if (!returnBusy) setReturnModalOrder(null); }}
          className="fixed inset-0 m-auto max-h-[calc(100dvh-1.5rem)] w-[calc(100%-1.5rem)] max-w-xl overflow-y-auto rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white p-4 text-sm shadow-2xl backdrop:bg-slate-950/60 sm:p-6">
          <div className="space-y-4">
            <h3 id="customer-return-title" className="font-extrabold text-slate-900">ثبت مرجوعی برای سفارش <bdi>{returnModalOrder.orderNumber}</bdi></h3>
            <p className="text-slate-600">کالاهای مرجوعی و تعداد هر کدام را انتخاب کنید.</p>
            <div className="space-y-2">
              {returnModalOrder.items.filter((it) => it.orderItemId).map((it) => {
                const id = it.orderItemId as string;
                return (
                  <div key={id} className="flex flex-wrap items-center gap-2 rounded-xl bg-slate-50 p-2">
                    <input
                      type="checkbox"
                      id={`return-line-${id}`}
                      checked={!!returnSelected[id]}
                      className="size-5 accent-[var(--account-accent)]"
                      disabled={returnBusy}
                      onChange={(e) => setReturnSelected((prev) => ({ ...prev, [id]: e.target.checked }))}
                    />
                    <label htmlFor={`return-line-${id}`} className="flex min-h-11 min-w-32 flex-1 cursor-pointer items-center text-slate-700">{it.product.title}</label>
                    <input
                      type="number"
                      min={1}
                      max={it.quantity}
                      value={returnQty[id] || 1}
                      onChange={(e) => setReturnQty((prev) => ({ ...prev, [id]: Math.min(it.quantity, Math.max(1, parseInt(e.target.value, 10) || 1)) }))}
                      aria-label={`تعداد ${it.product.title}`}
                      disabled={returnBusy || !returnSelected[id]}
                      className="min-h-11 w-16 rounded-lg border border-slate-300 p-1 text-center disabled:opacity-50"
                    />
                    <span className="text-slate-400">از {toPersianDigits(it.quantity)} عدد</span>
                  </div>
                );
              })}
            </div>
            <label className="block font-bold" htmlFor="customer-return-reason">دلیل مرجوعی</label>
            <textarea id="customer-return-reason" rows={3} disabled={returnBusy}
              className="w-full rounded-xl border border-slate-300 p-3 text-base"
              placeholder="در چند جمله دلیل مرجوعی را توضیح دهید"
              value={returnReason}
              onChange={(e) => setReturnReason(e.target.value)}
            />
            {returnDestination && (
              <div className="bg-slate-50 rounded-xl p-2 space-y-0.5">
                <p className="text-slate-700 font-bold">
                  مقصد مرجوعی: {returnDestination.type === 'supplier' ? 'انبار تأمین‌کننده' : 'انبار مرکزی'}
                  {returnDestination.name ? ` — ${returnDestination.name}` : ''}
                  {returnDestination.city ? ` (${returnDestination.city})` : ''}
                </p>
                {returnDestination.address && (
                  <p className="text-[11px] text-slate-500">{returnDestination.address}</p>
                )}
              </div>
            )}
            <label className="flex min-h-11 items-center gap-2 text-slate-700">
              <input type="checkbox" className="size-5 accent-[var(--account-accent)]" disabled={returnBusy} checked={returnPrepaid} onChange={(e) => setReturnPrepaid(e.target.checked)} />
              <span>هزینه ارسال مرجوعی را پیش‌پرداخت می‌کنم</span>
            </label>
            <div aria-live="polite" className="text-xs text-slate-700">
              {quoteState === 'loading' && <p>در حال محاسبهٔ هزینهٔ ارسال مرجوعی…</p>}
              {quoteState === 'error' && (
                <div className="flex flex-wrap items-center gap-x-3 text-amber-900">
                  <p>برآورد هزینهٔ ارسال دریافت نشد.</p>
                  <button type="button" disabled={returnBusy} onClick={() => setQuoteRetry(value => value + 1)} className="min-h-11 font-bold underline">تلاش دوباره</button>
                </div>
              )}
              {quoteState === 'ready' && returnShippingCost !== null && <p>برآورد هزینهٔ ارسال مرجوعی {formatPrice(returnShippingCost)} تومان</p>}
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => void submitReturn()}
                disabled={returnBusy || (returnPrepaid && quoteState !== 'ready')}
                className="min-h-11 flex-1 rounded-xl bg-[var(--account-accent)] px-4 font-bold text-white disabled:opacity-50"
              >
                {returnBusy ? 'در حال ثبت…' : 'ثبت درخواست مرجوعی'}
              </button>
              <button
                onClick={() => setReturnModalOrder(null)}
                disabled={returnBusy}
                className="min-h-11 px-4 py-2.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold rounded-xl"
              >
                انصراف
              </button>
            </div>
          </div>
        </dialog>
      )}
    </CustomerAccountLayout>
  );
};
