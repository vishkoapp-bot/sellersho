import React, { useState, useEffect, useRef } from 'react';
import { useOrders } from '../../context/OrderContext';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { Order } from '../../types';
import { orderApi, CustomerReturnDto, ReturnDestinationInfo, OrderRefundSummary } from '../../services/orderApi';
import { ArrowRight, Package, Truck, CheckCircle2, Clock, Copy, Printer, AlertCircle } from 'lucide-react';

export const CustomerOrdersView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { currentStore, setTrackingOrder, setIsTrackingModalOpen, formatPrice, toPersianDigits, confirmOrderDelivery, disputeOrderDelivery, busyOrderId } = useStore();
  const { success, error: toastError } = useToast();
  const { orders, fetchMyOrders } = useOrders();
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
  const [confirmingCancelId, setConfirmingCancelId] = useState<string | null>(null);
  const [cancelBusy, setCancelBusy] = useState(false);
  const [cancellationResults, setCancellationResults] = useState<Record<string,OrderRefundSummary>>({});

  useEffect(() => {
    let active = true;
    void fetchMyOrders();
    orderApi.getMyReturns().then((rows) => { if (active) setMyReturns(rows); }).catch(() => { if (active) setMyReturns([]); });
    return () => { active = false; ++returnQuoteSeq.current; };
  }, []);
  const [filter, setFilter] = useState<'all' | 'processing' | 'shipped' | 'delivered'>('all');

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
  const cancellableStatuses = ['pending', 'processing', 'picking', 'picked', 'packed'];
  const canCancel = (o: Order) => cancellableStatuses.includes(o.status);
  const returnDaysLeft = (o: Order): number | null => {
    if (!o.returnWindowEndsAt) return null;
    return Math.ceil((new Date(o.returnWindowEndsAt).getTime() - Date.now()) / 86400000);
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
    const ids = ord.items.filter((it) => it.orderItemId).map((it) => ({ order_item_id: it.orderItemId as string }));
    if (ids.length > 0) {
      orderApi.requestReturnQuote(ord.id, ids).then((q) => setReturnShippingCost(q.totalFareToman)).catch(() => {});
    }
  };

  // The return destination is DERIVED by the backend from the
  // shipment origins of the selected items (owner rule, Oct 28 2026)
  // — never typed by the customer. Re-quote whenever the selection
  // changes so the fare estimate and the destination both describe
  // the selected set; the created return confirms the same
  // destination in the success message.
  useEffect(() => {
    if (!returnModalOrder) return;
    const ids = Object.entries(returnSelected)
      .filter(([, v]) => v)
      .map(([id]) => ({ order_item_id: id }));
    if (ids.length === 0) {
      setReturnDestination(null);
      return;
    }
    const seq = ++returnQuoteSeq.current;
    orderApi.requestReturnQuote(returnModalOrder.id, ids).then((q) => {
      if (returnQuoteSeq.current !== seq) return;
      setReturnShippingCost(q.totalFareToman);
      setReturnDestination(q.destination ?? null);
    }).catch(() => {});
  }, [returnSelected, returnModalOrder]);

  const submitReturn = async () => {
    if (!returnModalOrder) return;
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
      const rs = await orderApi.getMyReturns().catch(() => []);
      setMyReturns(rs);
    } catch (e: any) {
      toastError(e?.message || 'ثبت مرجوعی انجام نشد.');
    } finally {
      setReturnBusy(false);
    }
  };

  const filtered = storeOrders.filter((o) => {
    if (filter === 'all') return true;
    if (filter === 'delivered') return o.status === 'delivered' || o.status === 'delivered_confirmed' || o.status === 'delivered_unconfirmed';
    return o.status === filter;
  });

  const copyTracking = (code: string) => {
    if (navigator.clipboard) {
      navigator.clipboard.writeText(code);
      success(`کد رهگیری ${code} کپی شد.`);
    }
  };

  const getBadge = (status: Order['status']) => {
    if (status === 'delivered' || status === 'delivered_confirmed') {
      return <span className="bg-emerald-50 text-emerald-700 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border border-emerald-200"><CheckCircle2 className="w-3 h-3 text-emerald-600"  strokeWidth={1.5}/>تحویل قطعی شد</span>;
    }
    if (status === 'delivered_unconfirmed') {
      return <span className="bg-indigo-50 text-indigo-700 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border border-indigo-200"><CheckCircle2 className="w-3 h-3 text-indigo-600"  strokeWidth={1.5}/>رسیده به مقصد</span>;
    }
    if (status === 'shipped') {
      return <span className="bg-blue-50 text-blue-700 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border border-blue-200"><Truck className="w-3 h-3"  strokeWidth={1.5}/>ارسال پستی</span>;
    }
    return <span className="bg-amber-50 text-amber-700 text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border border-amber-200"><Clock className="w-3 h-3"  strokeWidth={1.5}/>پردازش انبار</span>;
  };

  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc] select-none pb-20">
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button onClick={onBack} className="p-1 rounded-lg hover:bg-slate-100 text-slate-700">
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <h1 className="text-xs font-bold text-slate-900">سفارش‌های من</h1>
        </div>
        <span className="text-[10px] font-mono text-slate-400">
          {toPersianDigits(filtered.length)} سفارش
        </span>
      </header>

      {/* Tabs */}
      <div className="flex bg-white border-b border-slate-200 p-2 gap-1 overflow-x-auto no-scrollbar text-xs">
        {[
          { id: 'all', label: 'همه' },
          { id: 'processing', label: 'در حال پردازش' },
          { id: 'shipped', label: 'ارسال شده' },
          { id: 'delivered', label: 'تحویل شده' },
        ].map((t) => (
          <button
            key={t.id}
            onClick={() => setFilter(t.id as any)}
            className={`py-1 px-3 rounded-xl font-bold transition-colors ${
              filter === t.id ? 'bg-[#007dfa] text-white shadow-2xs' : 'text-slate-600 hover:bg-slate-100'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* FIX 5: live "my returns" view, compact form. */}
      {storeReturns.length > 0 && (
        <div className="mx-3.5 mt-3 bg-white rounded-2xl border border-slate-200/90 p-3 space-y-2 text-xs">
          <h2 className="font-bold text-slate-800">مرجوعی‌های من</h2>
          {storeReturns.map((r) => (
            <div key={r.id} className="flex justify-between gap-2 border-t border-slate-100 pt-1.5">
              <span className="text-slate-600 truncate">{r.reason || '—'}</span>
              <span className="text-slate-500 shrink-0">{r.status}</span>
            </div>
          ))}
        </div>
      )}

      {/* List */}
      <div className="p-3.5 space-y-2.5">
        {filtered.length === 0 ? (
          <div className="bg-white rounded-2xl p-8 text-center border border-slate-200 space-y-2">
            <Package className="w-8 h-8 text-slate-400 mx-auto"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-700">سفارشی در این وضعیت یافت نشد</h3>
          </div>
        ) : (
          filtered.map((ord) => (
            <div key={ord.id} className="bg-white rounded-2xl border border-slate-200/90 p-3 shadow-2xs space-y-2 text-xs">
              <div className="flex items-center justify-between border-b border-slate-100 pb-1.5">
                <div>
                  <span className="font-mono font-black text-slate-800">{ord.orderNumber}</span>
                  <span className="text-[10px] text-slate-400 block">{ord.createdAt}</span>
                </div>
                {getBadge(ord.status)}
              </div>

              <div className="bg-slate-50 p-2 rounded-xl text-[11px] space-y-1">
                {ord.items.map((item, idx) => (
                  <div key={idx} className="flex justify-between items-center">
                    <span className="text-slate-700 truncate">{item.product.title}</span>
                    <span className="text-slate-500 font-bold shrink-0 mr-2">{toPersianDigits(item.quantity)} عدد</span>
                  </div>
                ))}
              </div>

              {ord.status === 'delivered_confirmed' && returnDaysLeft(ord) !== null && (returnDaysLeft(ord) as number) >= 0 && (
                <p className="text-[10px] text-slate-500">مهلت تست: {toPersianDigits(returnDaysLeft(ord) as number)} روز باقی مانده است؛ تا پایان آن امکان درخواست مرجوعی فعال است.</p>
              )}

              <div className="flex items-center justify-between text-[11px] pt-1">
                <div>
                  <span className="text-slate-400 text-[10px] block">مبلغ فاکتور:</span>
                  <strong className="text-slate-900">{formatPrice(ord.payableAmount)} تومان</strong>
                </div>
                <div className="text-left font-mono text-blue-600 font-bold flex items-center gap-1">
                  <span>{ord.trackingCode}</span>
                  <button onClick={() => copyTracking(ord.trackingCode)} className="p-0.5 text-slate-400">
                    <Copy className="w-3 h-3"  strokeWidth={1.5}/>
                  </button>
                </div>
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
                          success('تحویل مرسوله تایید شد. مهلت ۷ روزه مرجوعی آغاز شد؛ سود فروشنده پس از پایان مهلت آزاد می‌شود.');
                        } catch (e: any) { toastError(e?.message || 'تأیید تحویل انجام نشد.'); }
                      }}
                      className="py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-[11px] rounded-lg transition-colors flex items-center justify-center gap-1 shadow-2xs disabled:opacity-50"
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
                      className="py-1.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[11px] rounded-lg border border-rose-200 transition-colors flex items-center justify-center gap-1 disabled:opacity-50"
                    >
                      <AlertCircle className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>هنوز دریافت نکرده‌ام</span>
                    </button>
                  </div>
                </div>
              )}

              {ord.refund && ord.refund.status !== 'none' && (
                <div role="status" data-refund-status={ord.refund.status} className="p-2 border border-amber-200 bg-amber-50 rounded-xl text-[11px] space-y-1">
                  <strong>{({pending:'بازپرداخت در انتظار انجام',succeeded:'بازپرداخت انجام شد',failed:'بازپرداخت ناموفق؛ نیازمند پیگیری',unknown:'بازپرداخت نیازمند بررسی'} as const)[ord.refund.status]}</strong>
                  <p>مبلغ ثبت‌شده: {formatPrice(ord.refund.amountToman)} تومان؛ مقصد: {({original_payment:'روش پرداخت اولیه',wallet:'کیف پول',review:'نیازمند بررسی',mixed:'چند روش پرداخت',none:'در دست بررسی'} as const)[ord.refund.destination]}</p>
                  {ord.refund.succeededToman > 0 && <p>مبلغ انجام‌شده: {formatPrice(ord.refund.succeededToman)} تومان</p>}
                  {ord.refund.status !== 'succeeded' && <p>بازگشت وجه هنوز تأیید نشده است؛ وضعیت نهایی پس از بررسی یا تأیید پرداخت نمایش داده می‌شود.</p>}
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
                    <span>دریافت کالا توسط شما تایید شد. مهلت ۷ روزه مرجوعی آغاز شد؛ تسویه پس از پایان مهلت انجام می‌شود.</span>
                  </div>
                  {ord.confirmedAt && (
                    <span className="text-[9px] text-slate-400 font-mono">{ord.confirmedAt}</span>
                  )}
                </div>
              )}

              <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-100">
                <button
                  onClick={() => { setTrackingOrder(ord); setIsTrackingModalOpen(true); }}
                  className="py-1.5 bg-blue-50 hover:bg-blue-100 text-blue-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1"
                >
                  <Truck className="w-3.5 h-3.5"  strokeWidth={1.5}/><span>رهگیری</span>
                </button>
                <button
                  onClick={() => window.print()}
                  className="py-1.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1"
                >
                  <Printer className="w-3.5 h-3.5"  strokeWidth={1.5}/><span>چاپ فاکتور</span>
                </button>
                {canCancel(ord) && (
                  <button
                    onClick={() => { if (confirmingCancelId === ord.id) void doCancel(ord); else setConfirmingCancelId(ord.id); }}
                    disabled={cancelBusy}
                    className="py-1.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1 border border-rose-200 disabled:opacity-50"
                  >
                    <span>{confirmingCancelId === ord.id ? 'آیا از لغو این سفارش اطمینان دارید؟ این عمل قابل بازگشت نیست — برای تأیید دوباره بزنید' : 'لغو سفارش'}</span>
                  </button>
                )}
                {canReturn(ord) && (
                  <button
                    onClick={() => openReturnModal(ord)}
                    className="py-1.5 bg-amber-50 hover:bg-amber-100 text-amber-800 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1 border border-amber-200"
                  >
                    <span>ثبت مرجوعی</span>
                  </button>
                )}
              </div>
            </div>
          ))
        )}
      </div>

      {/* FIX 5: return form — live shape (order_item_id + prepaid).
          The destination is not asked anymore: the backend derives
          it from the shipment origins (owner rule, Oct 28 2026) and
          it is shown read-only from the quote response. The backend
          enforces the delivered_confirmed status and the return
          window. */}
      {returnModalOrder && (
        <div className="fixed inset-0 z-50 bg-slate-900/60 flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl max-w-[480px] w-full p-4 space-y-3 text-xs max-h-[90vh] overflow-y-auto">
            <h3 className="font-bold text-slate-800">ثبت مرجوعی — {returnModalOrder.orderNumber}</h3>
            <div className="space-y-2">
              {returnModalOrder.items.filter((it) => it.orderItemId).map((it) => {
                const id = it.orderItemId as string;
                return (
                  <div key={id} className="flex items-center gap-2 bg-slate-50 rounded-xl p-2">
                    <input
                      type="checkbox"
                      checked={!!returnSelected[id]}
                      onChange={(e) => setReturnSelected((prev) => ({ ...prev, [id]: e.target.checked }))}
                    />
                    <span className="flex-1 truncate text-slate-700">{it.product.title}</span>
                    <input
                      type="number"
                      min={1}
                      max={it.quantity}
                      value={returnQty[id] || 1}
                      onChange={(e) => setReturnQty((prev) => ({ ...prev, [id]: Math.min(it.quantity, Math.max(1, parseInt(e.target.value, 10) || 1)) }))}
                      className="w-14 border rounded-lg p-1 text-center"
                    />
                    <span className="text-slate-400">از {toPersianDigits(it.quantity)} عدد</span>
                  </div>
                );
              })}
            </div>
            <input
              className="w-full border rounded-xl p-2"
              placeholder="دلیل مرجوعی"
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
            <label className="flex items-center gap-2 text-slate-700">
              <input type="checkbox" checked={returnPrepaid} onChange={(e) => setReturnPrepaid(e.target.checked)} />
              <span>هزینه ارسال مرجوعی را پیش‌پرداخت می‌کنم</span>
            </label>
            {returnShippingCost !== null && (
              <p className="text-[11px] text-slate-500">هزینه ارسال مرجوعی (برآورد): {formatPrice(returnShippingCost)} تومان</p>
            )}
            <div className="flex gap-2">
              <button
                onClick={() => void submitReturn()}
                disabled={returnBusy}
                className="flex-1 py-2.5 bg-amber-600 hover:bg-amber-700 text-white font-bold rounded-xl disabled:opacity-50"
              >
                {returnBusy ? 'در حال ثبت...' : 'ثبت درخواست مرجوعی'}
              </button>
              <button
                onClick={() => setReturnModalOrder(null)}
                className="px-4 py-2.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold rounded-xl"
              >
                انصراف
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
