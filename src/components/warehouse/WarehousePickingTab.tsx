import React, { useCallback, useEffect, useState } from 'react';
import { ArrowRight, CheckCircle2, RefreshCw, ScanLine } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { apiRequest } from '../../services/apiClient';
import { PickingRoutePlanner } from './WarehouseOptimizationTools';

// Live contract (live bundles, read-only):
// - GET /warehouse/orders/ready-to-pick -> { orders } (ReturnWindowBadge-MERGED.js, getReadyToPick)
// - POST /warehouse/orders/{id}/start-picking (no body)
// - POST /warehouse/orders/{id}/complete-picking (no body)
// Item rows render only server fields: productTitle, variantName, variantSpnc,
// binLocation (or the honest "not recorded" line when absent), fulfillmentMethod,
// quantity. No bin or barcode is ever fabricated here.
interface ServerPickItem {
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
  onGoToPacking: (orderNumber: string) => void;
}

export const WarehousePickingTab: React.FC<Props> = ({ onGoToPacking }) => {
  const { success, error } = useToast();
  const [orders, setOrders] = useState<ServerPickOrder[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [busyId, setBusyId] = useState<string | null>(null);

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

  useEffect(() => { void reload(); }, [reload]);

  const startPicking = async (order: ServerPickOrder) => {
    setBusyId(order.id);
    try {
      await apiRequest(`/warehouse/orders/${encodeURIComponent(order.id)}/start-picking`, { method: 'POST', auth: true });
      success('جمع‌آوری سفارش آغاز شد.');
      await reload();
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'آغاز جمع‌آوری ناموفق بود. دوباره تلاش کنید.');
    } finally {
      setBusyId(null);
    }
  };

  const completePicking = async (order: ServerPickOrder) => {
    setBusyId(order.id);
    try {
      await apiRequest(`/warehouse/orders/${encodeURIComponent(order.id)}/complete-picking`, { method: 'POST', auth: true });
      success(`جمع‌آوری سفارش ${order.orderNumber} کامل شد.`);
      onGoToPacking(order.orderNumber);
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت پایان جمع‌آوری ناموفق بود. دوباره تلاش کنید.');
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="space-y-3.5 pb-6">
      <PickingRoutePlanner orderIds={orders.map((order) => order.id)} />
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-slate-800">برگه جمع‌آوری سفارش‌ها</h3>
          <span className="text-[10px] bg-amber-50 text-amber-700 font-bold px-2 py-0.5 rounded-full border border-amber-200">
            {loading ? 'در حال دریافت…' : `${new Intl.NumberFormat('fa-IR').format(orders.length)} سفارش در صف`}
          </span>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          سفارش‌ها مستقیماً از سرور خوانده می‌شوند. انباردار با مراجعه به جایگاه قفسه، اقلام هر سفارش را جمع‌آوری و سپس پایان جمع‌آوری را ثبت می‌کند.
        </p>
      </div>

      <div className="space-y-3">
        {loading ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs">در حال دریافت صف جمع‌آوری…</div>
        ) : loadError ? (
          <div className="bg-rose-50 rounded-2xl p-6 text-center text-rose-700 text-xs space-y-2">
            <p>{loadError}</p>
            <button onClick={() => void reload()} className="font-bold underline">تلاش مجدد</button>
          </div>
        ) : orders.length === 0 ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs space-y-1">
            <p>سفارشی در صف جمع‌آوری وجود ندارد.</p>
            <p className="text-[10px]">فقط سفارش‌های پرداخت‌شده در این صف نمایش داده می‌شوند.</p>
          </div>
        ) : (
          orders.map((order) => {
            const picking = order.status === 'picking';
            const busy = busyId === order.id;
            return (
              <div key={order.id} className="p-3.5 rounded-2xl border bg-white border-slate-200/90 shadow-2xs space-y-2.5">
                <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                  <div>
                    <span className="text-xs font-black text-slate-800 font-mono">{order.orderNumber}</span>
                    <span className="text-[10px] text-slate-400 block mt-0.5">
                      {order.storeName}{order.storeName && order.customerName ? ' • ' : ''}{order.customerName}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    {order.fulfillmentMethod === 'mixed' && (
                      <span
                        className="text-[10px] font-bold px-2 py-0.5 rounded-full border bg-violet-50 text-violet-700 border-violet-200"
                        title="این سفارش هم اقلام انباری و هم اقلام ارسال مستقیم دارد. فقط اقلام انباری برای جمع‌آوری نمایش داده شده‌اند."
                      >
                        سفارش ترکیبی
                      </span>
                    )}
                    <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full border ${picking ? 'bg-amber-50 text-amber-700 border-amber-200' : 'bg-slate-100 text-slate-600 border-slate-200'}`}>
                      {picking ? 'در حال جمع‌آوری' : 'در انتظار جمع‌آوری'}
                    </span>
                  </div>
                </div>

                <div className="space-y-1.5">
                  {(order.items || []).map((item, itemIdx) => (
                    <div key={`${item.productId || 'item'}-${itemIdx}`} className="flex items-center justify-between gap-2 text-[11px] bg-slate-50 rounded-xl px-2.5 py-2">
                      <div className="min-w-0">
                        <span className="font-bold text-slate-800 block truncate">{item.productTitle || 'کالا'}</span>
                        {item.variantName && (
                          <span className="text-[10px] text-slate-600 font-bold block">تنوع: {item.variantName}</span>
                        )}
                        {item.variantSpnc && (
                          <span className="inline-block text-[9px] font-mono font-bold px-1.5 py-px rounded-full border bg-violet-50 text-violet-700 border-violet-200 mt-0.5" dir="ltr">
                            {item.variantSpnc}
                          </span>
                        )}
                        {item.binLocation ? (
                          <span className="text-[10px] text-slate-500 font-mono block" dir="ltr">{item.binLocation}</span>
                        ) : (
                          <span className="text-[10px] text-slate-400 block">جایگاه قفسه ثبت نشده است</span>
                        )}
                        <span className={`inline-block text-[9px] font-bold px-1.5 py-px rounded-full border mt-1 ${item.fulfillmentMethod === 'supplier_direct' ? 'bg-sky-50 text-sky-700 border-sky-200' : 'bg-emerald-50 text-emerald-700 border-emerald-200'}`}>
                          {item.fulfillmentMethod === 'supplier_direct' ? 'ارسال مستقیم تأمین‌کننده' : 'انبار'}
                        </span>
                      </div>
                      <span className="text-slate-700 font-bold shrink-0">
                        {new Intl.NumberFormat('fa-IR').format(item.quantity || 0)} عدد
                      </span>
                    </div>
                  ))}
                </div>

                <div className="flex items-center justify-between text-[11px] text-slate-500 border-t border-slate-100 pt-2">
                  <span className="flex items-center gap-1">
                    مبلغ سفارش: <strong className="text-slate-700">{new Intl.NumberFormat('fa-IR').format(Math.round(order.totalAmount || 0))} تومان</strong>
                  </span>
                </div>

                <div className="pt-1">
                  {picking ? (
                    <button
                      onClick={() => void completePicking(order)}
                      disabled={busy}
                      className="w-full py-2 bg-emerald-600 hover:bg-emerald-700 disabled:opacity-60 disabled:cursor-wait text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1"
                    >
                      <CheckCircle2 className="w-4 h-4" strokeWidth={1.5} />
                      <span>{busy ? 'در حال ثبت…' : 'پایان جمع‌آوری و انتقال به میز بسته‌بندی'}</span>
                      <ArrowRight className="w-3.5 h-3.5" strokeWidth={1.5} />
                    </button>
                  ) : (
                    <button
                      onClick={() => void startPicking(order)}
                      disabled={busy}
                      className="w-full py-2 bg-amber-500 hover:bg-amber-600 disabled:opacity-60 disabled:cursor-wait text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1"
                    >
                      <ScanLine className="w-4 h-4" strokeWidth={1.5} />
                      <span>{busy ? 'در حال ثبت…' : 'شروع جمع‌آوری اقلام این سفارش'}</span>
                    </button>
                  )}
                </div>
              </div>
            );
          })
        )}
      </div>

      <div className="flex justify-end">
        <button
          onClick={() => void reload()}
          disabled={loading}
          className="rounded-xl border border-slate-200 p-2 text-slate-600 disabled:opacity-50"
          title="به‌روزرسانی"
        >
          <RefreshCw className={loading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} strokeWidth={1.5} />
        </button>
      </div>
    </div>
  );
};
