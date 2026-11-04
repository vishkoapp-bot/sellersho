import React, { useCallback, useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest } from '../../services/apiClient';
import { wmsApi, ShippingManifestDto } from '../../services/wmsApi';
import { Truck, CheckCircle2, Printer, RefreshCw } from 'lucide-react';

// Live contract (live bundles, read-only):
// - GET /warehouse/orders/ready-to-ship -> { orders } (ReturnWindowBadge-MERGED.js, getReadyToShip)
// - POST /warehouse/orders/{id}/ship { carrier } (ReturnWindowBadge-MERGED.js, shipWarehouseOrder)
// - GET /warehouse/manifests -> manifests (ReturnWindowBadge-MERGED.js, getManifests)
// - POST /warehouse/manifests/{id}/dispatch (ReturnWindowBadge-MERGED.js, dispatchManifest)
// Manifests come only from the server: when the server returns none, the tab
// shows live's honest empty state, never a fabricated manifest row.
interface ServerShipItem {
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
  paymentStatus?: string;
  items?: ServerShipItem[];
}

export const WarehouseShippingTab: React.FC = () => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();

  const [carrier, setCarrier] = useState('پست پیشتاز');
  const [shipOrders, setShipOrders] = useState<ServerShipOrder[]>([]);
  const [shipLoading, setShipLoading] = useState(true);
  const [shipError, setShipError] = useState('');
  const [shippingId, setShippingId] = useState<string | null>(null);

  const [manifests, setManifests] = useState<ShippingManifestDto[]>([]);
  const [manifestsLoading, setManifestsLoading] = useState(true);
  const [manifestsError, setManifestsError] = useState('');
  const [dispatchingId, setDispatchingId] = useState<string | null>(null);

  const reloadShip = useCallback(async () => {
    setShipLoading(true);
    setShipError('');
    try {
      const data = await apiRequest<{ orders?: ServerShipOrder[] } | ServerShipOrder[]>('/warehouse/orders/ready-to-ship', { method: 'GET', auth: true });
      setShipOrders(Array.isArray(data) ? data : (data.orders || []));
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'بسته‌های آماده تحویل دریافت نشدند.';
      setShipError(message);
    } finally {
      setShipLoading(false);
    }
  }, []);

  const reloadManifests = useCallback(async () => {
    setManifestsLoading(true);
    setManifestsError('');
    try {
      const data = await wmsApi.getManifests();
      setManifests(Array.isArray(data) ? data : []);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'مانیفست‌ها دریافت نشدند.';
      setManifestsError(message);
    } finally {
      setManifestsLoading(false);
    }
  }, []);

  useEffect(() => { void reloadShip(); void reloadManifests(); }, [reloadShip, reloadManifests]);

  const handleShipOrder = async (order: ServerShipOrder) => {
    setShippingId(order.id);
    try {
      await apiRequest(`/warehouse/orders/${encodeURIComponent(order.id)}/ship`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({ carrier }),
      });
      success(`سفارش ${order.orderNumber} به ${carrier} تحویل شد.`);
      await reloadShip();
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت تحویل به شرکت حمل ناموفق بود.');
    } finally {
      setShippingId(null);
    }
  };

  const handleDispatchBatch = async (batchId: string, batchCarrier: string) => {
    setDispatchingId(batchId);
    try {
      await wmsApi.dispatchManifest(batchId);
      await reloadManifests();
      success(`مانیفست ${batchId} با موفقیت به ناوگان حمل (${batchCarrier}) تحویل شد.`);
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'خطا در ثبت خروج مانیفست در سرور');
    } finally {
      setDispatchingId(null);
    }
  };

  const handlePrintManifest = (batch: ShippingManifestDto) => {
    success(`رسید مانیفست ${batch.id} برای چاپ آماده شد.`);
    window.print();
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <h3 className="text-xs font-bold text-slate-800">تحویل بسته‌ها به شرکت حمل</h3>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          سفارش‌های بسته‌بندی‌شده از سرور خوانده می‌شوند؛ با ثبت تحویل، وضعیت سفارش به «تحویل به شرکت حمل» تغییر می‌کند.
        </p>
        <div className="flex gap-2 items-center">
          <label className="text-[10px] text-slate-600 shrink-0">شرکت حمل:</label>
          <input
            value={carrier}
            onChange={(e) => setCarrier(e.target.value)}
            className="flex-1 text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden"
            placeholder="نام شرکت حمل"
          />
        </div>
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-bold text-slate-800">بسته‌های آماده تحویل</h4>
          <span className="text-[10px] bg-blue-50 text-blue-700 font-bold px-2 py-0.5 rounded-full border border-blue-200">
            {shipLoading ? 'در حال دریافت…' : `${toPersianDigits(shipOrders.length)} بسته`}
          </span>
        </div>
        {shipLoading ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs">در حال دریافت بسته‌های آماده تحویل…</div>
        ) : shipError ? (
          <div className="bg-rose-50 rounded-2xl p-6 text-center text-rose-700 text-xs space-y-2">
            <p>{shipError}</p>
            <button onClick={() => void reloadShip()} className="font-bold underline">تلاش مجدد</button>
          </div>
        ) : shipOrders.length === 0 ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs">بسته‌ای آماده تحویل به شرکت حمل وجود ندارد.</div>
        ) : (
          shipOrders.map((order) => (
            <div key={order.id} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div>
                  <span className="text-xs font-black text-slate-800 font-mono">{order.orderNumber}</span>
                  <span className="text-[10px] text-slate-400 block mt-0.5">
                    {order.storeName}{order.storeName && order.customerName ? ' • ' : ''}{order.customerName}
                  </span>
                </div>
                <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-amber-50 text-amber-700 border border-amber-200">
                  بسته‌بندی شده
                </span>
              </div>
              <div className="text-[11px] text-slate-600">
                {(order.items || []).map((item) => `${item.productTitle || 'کالا'}${item.variantName ? ` — ${item.variantName}` : ''} (${toPersianDigits(item.quantity || 0)} عدد)`).join(' + ') || '—'}
              </div>
              {(order.items || []).some((item) => item.variantSpnc) && (
                <div className="flex flex-wrap gap-1">
                  {(order.items || []).filter((item) => item.variantSpnc).map((item, idx) => (
                    <span
                      key={`${item.productId || 'item'}-${idx}`}
                      className="inline-block text-[9px] font-mono font-bold px-1.5 py-px rounded-full border bg-violet-50 text-violet-700 border-violet-200"
                      dir="ltr"
                      title={item.variantName || undefined}
                    >
                      {item.variantSpnc}
                    </span>
                  ))}
                </div>
              )}
              <button
                type="button"
                onClick={() => void handleShipOrder(order)}
                disabled={shippingId === order.id}
                className="w-full py-2 bg-emerald-600 hover:bg-emerald-700 disabled:opacity-60 disabled:cursor-wait text-white font-bold text-[11px] rounded-xl flex items-center justify-center gap-1.5 transition-colors"
              >
                <Truck className="w-3.5 h-3.5" strokeWidth={1.5} />
                <span>{shippingId === order.id ? 'در حال ثبت تحویل…' : 'تحویل به شرکت حمل'}</span>
              </button>
            </div>
          ))
        )}
      </div>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h4 className="text-xs font-bold text-slate-800 flex items-center gap-1.5">
            <Truck className="w-4 h-4 text-blue-600" strokeWidth={1.5} />
            مانیفست‌های خروج
          </h4>
          <button
            type="button"
            onClick={() => void reloadManifests()}
            disabled={manifestsLoading}
            className="rounded-xl border border-slate-200 p-1.5 text-slate-600 disabled:opacity-50"
            title="به‌روزرسانی"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${manifestsLoading ? 'animate-spin' : ''}`} strokeWidth={1.5} />
          </button>
        </div>
        {manifestsLoading ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs">در حال دریافت مانیفست‌ها…</div>
        ) : manifestsError ? (
          <div className="bg-rose-50 rounded-2xl p-6 text-center text-rose-700 text-xs space-y-2">
            <p>{manifestsError}</p>
            <button onClick={() => void reloadManifests()} className="font-bold underline">تلاش مجدد</button>
          </div>
        ) : manifests.length === 0 ? (
          <div className="bg-white rounded-2xl p-6 text-center text-slate-400 text-xs">مانیفستی وجود ندارد.</div>
        ) : (
          manifests.map((batch) => (
            <div key={batch.id} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5">
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div>
                  <span className="text-xs font-black text-slate-800 font-mono">{batch.id}</span>
                  <span className="text-[10px] text-slate-400 block mt-0.5">
                    {batch.createdAt ? new Date(batch.createdAt).toLocaleDateString('fa-IR') : '—'}
                  </span>
                </div>
                <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full border ${batch.status === 'handed_over' ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-amber-50 text-amber-700 border-amber-200'}`}>
                  {batch.status === 'handed_over' ? 'تحویل داده شد' : 'در انتظار تحویل'}
                </span>
              </div>

              <div className="text-[11px] text-slate-600 space-y-1">
                <div className="flex justify-between">
                  <span className="text-slate-400">شرکت حمل:</span>
                  <span className="font-bold text-slate-800">{batch.carrier}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-400">نماینده دریافت:</span>
                  <span>{batch.driverName}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-400">تعداد بسته‌های پلمب:</span>
                  <span className="font-bold text-blue-600">{toPersianDigits(batch.packagesCount)} مرسوله</span>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2 pt-1 border-t border-slate-100">
                {batch.status === 'pending_pickup' ? (
                  <button
                    type="button"
                    onClick={() => void handleDispatchBatch(batch.id, batch.carrier)}
                    disabled={dispatchingId === batch.id}
                    className="py-2 bg-emerald-600 hover:bg-emerald-700 disabled:opacity-60 disabled:cursor-wait text-white font-bold text-[11px] rounded-xl flex items-center justify-center gap-1.5 transition-colors"
                  >
                    <CheckCircle2 className="w-3.5 h-3.5" strokeWidth={1.5} />
                    <span>{dispatchingId === batch.id ? 'در حال ثبت…' : 'تحویل مرسولات به پیک'}</span>
                  </button>
                ) : (
                  <div className="py-2 bg-emerald-50 text-emerald-700 font-bold text-[10px] rounded-xl text-center">
                    بسته‌ها تحویل گردید
                  </div>
                )}
                <button
                  type="button"
                  onClick={() => handlePrintManifest(batch)}
                  className="py-2 bg-slate-50 hover:bg-slate-100 text-slate-700 border border-slate-200 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1.5 transition-colors"
                >
                  <Printer className="w-3.5 h-3.5 text-blue-600" strokeWidth={1.5} />
                  <span>چاپ مانیفست بارنامه</span>
                </button>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
};
