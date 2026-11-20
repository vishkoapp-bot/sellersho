import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import {
  Package,
  Search,
  Truck,
  CheckCircle2,
  Clock,
  Send,
  X,
  Hash,
  User,
  Calendar,
  AlertCircle,
  ChevronDown,
  MapPin,
  Warehouse,
  Printer
} from 'lucide-react';
import { supplierApi, TapinBoxDto } from '../../services/supplierApi';
import type { SupplierWarehouseOrderDto, SupplierOrderItemDto } from '../../services/supplierApi';
import { SupplierOrderItemIdentity, SupplierOrderLines } from './SupplierOrderItemIdentity';

interface SupplierOrder {
  items?: SupplierOrderItemDto[];
  id: string;
  orderId: string;
  orderNumber: string;
  customerName: string;
  customerPhone?: string;
  status: 'pending' | 'processing' | 'shipped' | 'in_transit' | 'delivered' | string;
  statusText: string;
  carrier?: string;
  trackingNumber?: string;
  shippedAt?: string;
  createdAt: string;
  itemCount: number;
}

interface SupplierOrderAddon {
  title: string;
  productTitle: string;
  feePerUnit: number;
  quantity: number;
  total: number;
}

// FIX 38: rebuilt to the live bundle SupplierDashboardView-MERGED.js (bn component).
// Divergences fixed vs the previous candidate tab: missing help box, wrong
// header/subtitle, indigo/blue badge and button colors (live uses brand
// colors), filter labels («آماده‌سازی»/«تحویل داده شده» → live's «منتظر
// ارسال»/«به دست مشتری رسیده») and button sizing, English tracking labels
// («Carrier:»/«Tracking:» → live's Persian labels), single wrong empty state
// (live has separate all-empty and filtered-empty states), missing
// access-hint error branch, missing per-order addons expand section, and
// wrong modal labels/validation/error strings (see below).
// NOTE: live's Tapin ship section inside the shipping modal is built
// below (gaps 1-3): when Tapin is connected the modal gains a
// «ثبت ارسال با تاپین» mode (service + box selects, tracking issued
// by Tapin, label printing). Post registration stays disabled with a
// note — its contract is not published in Tapin's guide.
// NOTE: live's badge/expand glyphs are minified away; Truck (badge),
// Package (expand icon) and ChevronDown (expand chevron) are lucide
// approximations. Live's createdAt formatter (Ka) is not reimplemented:
// the backend already returns a Persian-formatted date string, which the
// browser observed as 2026/10/28 on staging.
export const SupplierOrdersTab: React.FC = () => {
  const {
    toPersianDigits
  } = useStore();
  const { success, error } = useToast();

  const [backendOrders, setBackendOrders] = useState<SupplierOrder[]>([]);
  const [ordersNextCursor, setOrdersNextCursor] = useState('');
  const [ordersHasMore, setOrdersHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [filterStatus, setFilterStatus] = useState<string>('all');

  // Gap 14: view toggle — direct shipments to fulfil vs warehouse-routed
  // orders (read-only visibility), plus the accept-step in-flight marker.
  const [ordersView, setOrdersView] = useState<'direct' | 'warehouse'>('direct');
  const [warehouseOrders, setWarehouseOrders] = useState<SupplierWarehouseOrderDto[]>([]);
  const [warehouseLoading, setWarehouseLoading] = useState(false);
  const [warehouseError, setWarehouseError] = useState('');
  const [warehouseLoaded, setWarehouseLoaded] = useState(false);
  const [acceptingId, setAcceptingId] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  // Addons expand state (live's Pe toggle per order card).
  const [expandedOrderId, setExpandedOrderId] = useState<string | null>(null);
  const [addonsByOrder, setAddonsByOrder] = useState<Record<string, { addons: SupplierOrderAddon[]; total: number }>>({});
  const [loadingAddonId, setLoadingAddonId] = useState<string | null>(null);

  // Shipping Modal State
  const [selectedOrder, setSelectedOrder] = useState<SupplierOrder | null>(null);
  const [carrier, setCarrier] = useState('پست پیشتاز');
  const [trackingNumber, setTrackingNumber] = useState('');
  const [shippingDate, setShippingDate] = useState(new Date().toLocaleDateString('fa-IR'));
  const [submittingShipping, setSubmittingShipping] = useState(false);

  // Tapin ship mode (gaps 1-3).
  const [shipMode, setShipMode] = useState<'manual' | 'tapin'>('manual');
  const [tapinConnected, setTapinConnected] = useState(false);
  const [tapinBoxes, setTapinBoxes] = useState<TapinBoxDto[]>([]);
  const [tapinDefaultBoxId, setTapinDefaultBoxId] = useState('4');
  const [tapinService, setTapinService] = useState<'post' | 'tipax'>('tipax');
  const [tapinBoxId, setTapinBoxId] = useState('');
  const [tapinTracking, setTapinTracking] = useState('');
  const [printingLabel, setPrintingLabel] = useState(false);

  // Each modal open starts in manual mode with no stale Tapin result,
  // and refreshes the connection + box catalogue for the mode toggle.
  useEffect(() => {
    if (!selectedOrder) return;
    setShipMode('manual');
    setTapinService('tipax');
    setTapinTracking('');
    supplierApi.getTapinSettings()
      .then((res) => setTapinConnected(!!res.tapinConnected))
      .catch(() => setTapinConnected(false));
    supplierApi.getTapinBoxes()
      .then((res) => {
        setTapinBoxes(res.boxes || []);
        setTapinDefaultBoxId(res.defaultBoxId || '4');
        setTapinBoxId(res.defaultBoxId || '4');
      })
      .catch(() => { setTapinBoxes([]); });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedOrder?.orderId]);

  const handlePrintTapinLabel = async () => {
    if (!selectedOrder || printingLabel) return;
    setPrintingLabel(true);
    try {
      const blob = await supplierApi.getTapinLabel(selectedOrder.id);
      const url = URL.createObjectURL(blob);
      window.open(url, '_blank', 'noopener');
      setTimeout(() => URL.revokeObjectURL(url), 60000);
    } catch (err: any) {
      error(err?.message || 'دریافت برچسب از تاپین انجام نشد.');
    } finally {
      setPrintingLabel(false);
    }
  };

  // Fetch from backend API — PERF-02 Step 9: cursor pages (50 rows);
  // a cursor appends, a fresh load replaces.
  const fetchSupplierOrders = async (cursor?: string) => {
    setLoading(true);
    setLoadError('');
    try {
      const res = await supplierApi.getOrders({ limit: 50, cursor });
      setBackendOrders((prev) => (cursor ? [...prev, ...(res.orders || [])] : (res.orders || [])));
      setOrdersNextCursor(res.nextCursor || '');
      setOrdersHasMore(!!res.hasMore);
    } catch (err: any) {
      setLoadError(err?.message || 'دریافت سفارش‌ها انجام نشد.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchSupplierOrders();
  }, []);

  const fetchWarehouseOrders = async () => {
    setWarehouseLoading(true);
    setWarehouseError('');
    try {
      const res = await supplierApi.getWarehouseOrders();
      setWarehouseOrders(res.orders || []);
      setWarehouseLoaded(true);
    } catch (err: any) {
      setWarehouseError(err?.message || 'دریافت سفارش‌های انبار مرکزی انجام نشد.');
    } finally {
      setWarehouseLoading(false);
    }
  };

  useEffect(() => {
    if (ordersView === 'warehouse' && !warehouseLoaded) {
      void fetchWarehouseOrders();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ordersView]);

  const handleAcceptOrder = async (ord: SupplierOrder) => {
    if (acceptingId) return;
    setAcceptingId(ord.id);
    try {
      await supplierApi.acceptOrder(ord.orderId);
      setBackendOrders((prev) => prev.map((o) => (o.id === ord.id ? { ...o, status: 'processing' } : o)));
      success(`سفارش ${ord.orderNumber} قبول شد و در حال آماده‌سازی است.`);
    } catch (err: any) {
      error(err?.message || 'قبول سفارش انجام نشد.');
    } finally {
      setAcceptingId(null);
    }
  };

  const warehouseStatusChip = (status: string): { text: string; cls: string } => {
    if (status === 'delivered' || status === 'delivered_confirmed') return { text: 'به دست مشتری رسیده', cls: 'bg-emerald-50 text-emerald-700 border-emerald-200' };
    if (status === 'shipped') return { text: 'ارسال شده از انبار', cls: 'bg-brand-soft text-brand-deep border-brand-border' };
    if (status === 'cancelled') return { text: 'لغو شده', cls: 'bg-rose-50 text-rose-700 border-rose-200' };
    return { text: 'در حال پردازش در انبار', cls: 'bg-amber-50 text-amber-700 border-amber-200' };
  };

  const allOrders: SupplierOrder[] = backendOrders;

  const filteredOrders = allOrders.filter((ord) => {
    if (filterStatus === 'ready') {
      if (ord.status === 'shipped' || ord.status === 'in_transit' || ord.status === 'delivered') return false;
    } else if (filterStatus === 'shipped') {
      if (ord.status !== 'shipped' && ord.status !== 'in_transit') return false;
    } else if (filterStatus === 'delivered') {
      if (ord.status !== 'delivered' && ord.status !== 'delivered_confirmed') return false;
    }

    if (searchQuery.trim()) {
      const q = searchQuery.trim().toLowerCase();
      const matchNumber = ord.orderNumber?.toLowerCase().includes(q) || ord.orderId?.toLowerCase().includes(q);
      const matchCustomer = ord.customerName?.toLowerCase().includes(q);
      const matchTracking = ord.trackingNumber?.toLowerCase().includes(q);
      return matchNumber || matchCustomer || matchTracking;
    }

    return true;
  });

  const toggleAddons = async (ord: SupplierOrder) => {
    if (expandedOrderId === ord.id) {
      setExpandedOrderId(null);
      return;
    }
    setExpandedOrderId(ord.id);
    if (!addonsByOrder[ord.id]) {
      setLoadingAddonId(ord.id);
      try {
        const res = await supplierApi.getOrderAddons(ord.orderId);
        setAddonsByOrder((prev) => ({ ...prev, [ord.id]: res }));
      } catch {
        setAddonsByOrder((prev) => ({ ...prev, [ord.id]: { addons: [], total: 0 } }));
      } finally {
        setLoadingAddonId(null);
      }
    }
  };

  const handleConfirmShipping = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedOrder || submittingShipping) return;
    if (shipMode === 'tapin') {
      setSubmittingShipping(true);
      try {
        const res = await supplierApi.submitShipping(selectedOrder.orderId, {
          viaTapin: true,
          tapinService,
          tapinBoxId: tapinBoxId || undefined,
        });
        const tracking = res.tapin?.trackingNumber || res.trackingNumber || '';
        setTapinTracking(tracking);
        success(`مرسوله سفارش ${selectedOrder.orderNumber} در تاپین ثبت شد؛ کد رهگیری: «${tracking}»`);
        await fetchSupplierOrders();
      } catch (err: any) {
        error(err.message || 'ثبت مرسوله در تاپین انجام نشد.');
      } finally {
        setSubmittingShipping(false);
      }
      return;
    }
    if (!trackingNumber.trim()) {
      error('کد رهگیری را وارد کن.');
      return;
    }

    setSubmittingShipping(true);
    try {
      await supplierApi.submitShipping(selectedOrder.orderId, { carrier, trackingNumber: trackingNumber.trim(), shippingDate });

      success(`اطلاعات ارسال سفارش ${selectedOrder.orderNumber} با کد رهگیری «${trackingNumber}» ثبت شد.`);
      setSelectedOrder(null);
      setTrackingNumber('');
      await fetchSupplierOrders();
    } catch (err: any) {
      error(err.message || 'خطا در ثبت اطلاعات بارنامه ارسال.');
    } finally {
      setSubmittingShipping(false);
    }
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">سفارش‌هایی که کالایش با توست اینجاست: بسته‌بندی کن، بفرست و بعد کد رهگیری را روی همان سفارش ثبت کن.</div>

      {/* View toggle: direct fulfilment vs warehouse-routed (read-only) */}
      <div className="flex items-center gap-1.5 bg-white p-1.5 rounded-2xl border border-slate-200 shadow-2xs text-xs font-bold">
        <button
          type="button"
          onClick={() => setOrdersView('direct')}
          className={`flex-1 px-3 min-h-[40px] rounded-xl transition-all flex items-center justify-center gap-1.5 ${
            ordersView === 'direct' ? 'bg-brand-deep text-white shadow-2xs' : 'text-slate-600 hover:bg-slate-100'
          }`}
        >
          <Truck className="w-3.5 h-3.5" strokeWidth={1.5} />
          <span>ارسال مستقیم توسط تو</span>
        </button>
        <button
          type="button"
          onClick={() => setOrdersView('warehouse')}
          className={`flex-1 px-3 min-h-[40px] rounded-xl transition-all flex items-center justify-center gap-1.5 ${
            ordersView === 'warehouse' ? 'bg-brand-deep text-white shadow-2xs' : 'text-slate-600 hover:bg-slate-100'
          }`}
        >
          <Warehouse className="w-3.5 h-3.5" strokeWidth={1.5} />
          <span>سفارش‌های انبار مرکزی</span>
        </button>
      </div>

      {ordersView === 'warehouse' ? (
        <>
          <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">این سفارش‌ها از انبار مرکزی ارسال می‌شوند و نیازی به اقدام تو نیست؛ این فهرست فقط برای این است که بدانی کالاهای تو چطور فروخته و ارسال شده‌اند. مبلغ‌ها بر اساس قیمت پایه خودت (عمده) است.</div>
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xs font-bold text-slate-800">سفارش‌های انبار مرکزی</h3>
              <span className="text-[10px] text-slate-400 block mt-0.5">
                سفارش‌های پرداخت‌شده‌ای که کالای تو را از انبار مرکزی می‌فرستند
              </span>
            </div>
            <div className="flex items-center gap-1.5 bg-brand-soft px-2.5 py-1 rounded-xl text-brand-deep font-bold text-[11px]">
              <Warehouse className="w-3.5 h-3.5" strokeWidth={1.5} />
              <span>{toPersianDigits(warehouseOrders.length)} سفارش</span>
            </div>
          </div>

          {warehouseLoading && <div role="status" className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">در حال دریافت سفارش‌های انبار مرکزی…</div>}
          {warehouseError && (
            <div role="alert" className="space-y-2 rounded-2xl bg-rose-50 p-4 text-xs text-rose-800">
              <p>{warehouseError}</p>
              <button type="button" onClick={fetchWarehouseOrders} className="rounded-lg bg-white px-3 py-2 font-bold">تلاش دوباره</button>
            </div>
          )}
          {!warehouseLoading && !warehouseError && warehouseOrders.length === 0 ? (
            <div className="bg-white rounded-2xl p-8 border border-slate-200 text-center space-y-2">
              <Warehouse className="w-10 h-10 text-slate-300 mx-auto" strokeWidth={1.5} />
              <h4 className="text-xs font-bold text-slate-700">هنوز سفارشی از انبار مرکزی برای کالاهای تو ثبت نشده است.</h4>
              <p className="text-[10px] leading-5 text-slate-400">وقتی مشتری کالایی از تو را که در انبار مرکزی است بخرد و پرداخت کند، سفارشش اینجا دیده می‌شود.</p>
            </div>
          ) : (
            <div className="space-y-2.5">
              {warehouseOrders.map((ord) => {
                const chip = warehouseStatusChip(ord.status);
                return (
                  <div key={ord.orderId} className="bg-white rounded-2xl p-3.5 border border-slate-200 shadow-2xs space-y-3">
                    <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                      <div className="flex items-center gap-2">
                        <span className="font-mono font-extrabold text-xs text-brand-deep" dir="ltr">
                          {ord.orderNumber}
                        </span>
                        <span className="text-[10px] text-slate-400 flex items-center gap-1">
                          <Calendar className="w-3 h-3" strokeWidth={1.5} />
                          <span>{ord.createdAt}</span>
                        </span>
                      </div>
                      <span className={`text-[10px] font-bold px-2 py-1 rounded-full border ${chip.cls}`}>
                        <span>{chip.text}</span>
                      </span>
                    </div>

                    <div className="space-y-1.5">
                      {ord.items.map((item, index) => (
                        <div key={item.orderItemId || `${item.productId}:${item.variantId || ''}:${index}`} className="flex flex-wrap items-center justify-between gap-2 text-[11px] text-slate-600 bg-slate-50 p-2 rounded-lg">
                          <SupplierOrderItemIdentity item={item} showQuantity={false} />
                          <span className="font-bold text-slate-700 shrink-0 mr-2">{toPersianDigits(item.wholesaleCost.toLocaleString())} × {toPersianDigits(item.quantity)} = {toPersianDigits(item.lineTotal.toLocaleString())} تومان</span>
                        </div>
                      ))}
                      <div className="flex items-center justify-between text-[11px] font-bold text-slate-800 px-1">
                        <span>جمع کالاهای تو در این سفارش</span>
                        <span>{toPersianDigits(ord.itemsTotal.toLocaleString())} تومان</span>
                      </div>
                    </div>

                    <div className="flex items-center gap-1.5 text-[11px] text-slate-500">
                      <MapPin className="w-3.5 h-3.5 text-slate-400" strokeWidth={1.5} />
                      <span>مقصد: {ord.province}، {ord.city}</span>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </>
      ) : (
        <>
      {/* Header and Quick Stats */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-xs font-bold text-slate-800">سفارش‌هایی که باید بفرستی</h3>
          <span className="text-[10px] text-slate-400 block mt-0.5">
            سفارش مشتری‌ها برای کالاهای تو
          </span>
        </div>
        <div className="flex items-center gap-1.5 bg-brand-soft px-2.5 py-1 rounded-xl text-brand-deep font-bold text-[11px]">
          <Truck className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          <span>{toPersianDigits(allOrders.length)} سفارش</span>
        </div>
      </div>

      {/* Filter Tabs and Search Bar */}
      <div className="bg-white p-2.5 rounded-2xl border border-slate-200 shadow-2xs space-y-2">
        <div className="relative">
          <Search className="w-4 h-4 text-slate-400 absolute right-2.5 top-2.5"  strokeWidth={1.5}/>
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="جستجوی شماره سفارش، کد رهگیری یا نام خریدار…"
            className="w-full text-xs pr-8 pl-3 py-2 bg-slate-50 border border-slate-200 rounded-xl focus:bg-white focus:outline-hidden"
          />
        </div>

        <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar text-xs font-bold">
          <button
            type="button"
            onClick={() => setFilterStatus('all')}
            className={`px-3.5 min-h-[44px] rounded-xl transition-all shrink-0 ${
              filterStatus === 'all'
                ? 'bg-brand-deep text-white shadow-2xs'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
            }`}
          >
            همه ({toPersianDigits(allOrders.length)})
          </button>
          <button
            type="button"
            onClick={() => setFilterStatus('ready')}
            className={`px-3.5 min-h-[44px] rounded-xl transition-all shrink-0 ${
              filterStatus === 'ready'
                ? 'bg-amber-600 text-white shadow-2xs'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
            }`}
          >
            منتظر ارسال ({toPersianDigits(allOrders.filter(o => o.status !== 'shipped' && o.status !== 'in_transit' && o.status !== 'delivered').length)})
          </button>
          <button
            type="button"
            onClick={() => setFilterStatus('shipped')}
            className={`px-3.5 min-h-[44px] rounded-xl transition-all shrink-0 ${
              filterStatus === 'shipped'
                ? 'bg-brand-deep text-white shadow-2xs'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
            }`}
          >
            ارسال شده ({toPersianDigits(allOrders.filter(o => o.status === 'shipped' || o.status === 'in_transit').length)})
          </button>
          <button
            type="button"
            onClick={() => setFilterStatus('delivered')}
            className={`px-3.5 min-h-[44px] rounded-xl transition-all shrink-0 ${
              filterStatus === 'delivered'
                ? 'bg-emerald-600 text-white shadow-2xs'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
            }`}
          >
            به دست مشتری رسیده ({toPersianDigits(allOrders.filter(o => o.status === 'delivered' || o.status === 'delivered_confirmed').length)})
          </button>
        </div>
      </div>

      {/* Orders List */}
      {loading && <div role="status" className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">در حال دریافت سفارش‌ها…</div>}
      {loadError && (
        <div role="alert" className="space-y-2 rounded-2xl bg-rose-50 p-4 text-xs text-rose-800">
          <p>{loadError}</p>
          {(loadError.includes('دسترسی') || loadError.includes('Forbidden')) ? (
            <p>دسترسی شما به این بخش برقرار نیست. اگر حسابت به‌تازگی فعال شده، یک بار خارج و دوباره وارد شو؛ اگر مشکل ادامه داشت، با پشتیبانی تماس بگیر.</p>
          ) : (
            <button type="button" onClick={() => void fetchSupplierOrders()} className="rounded-lg bg-white px-3 py-2 font-bold">تلاش دوباره</button>
          )}
        </div>
      )}
      {!loading && !loadError && filteredOrders.length === 0 ? (
        <div className="bg-white rounded-2xl p-8 border border-slate-200 text-center space-y-2">
          <Package className="w-10 h-10 text-slate-300 mx-auto"  strokeWidth={1.5}/>
          {filterStatus === 'all' && allOrders.length === 0 ? (
            <>
              <h4 className="text-xs font-bold text-slate-700">هنوز سفارشی برای تو نیامده است.</h4>
              <p className="text-[10px] leading-5 text-slate-400">وقتی مشتری کالای تو را بخرد، سفارشش اینجا می‌آید. آن وقت بسته‌بندی‌اش می‌کنی، می‌فرستی و کد رهگیری را روی همان سفارش ثبت می‌کنی.</p>
            </>
          ) : (
            <>
              <h4 className="text-xs font-bold text-slate-700">سفارشی در این وضعیت نیست.</h4>
              <p className="text-[10px] text-slate-400">سفارش‌های دیگر را از دکمه‌های بالای همین صفحه ببین.</p>
            </>
          )}
        </div>
      ) : (
        <div className="space-y-2.5">
          {filteredOrders.map((ord) => {
            const isShipped = ord.status === 'shipped' || ord.status === 'in_transit';
            const isDelivered = ord.status === 'delivered' || ord.status === 'delivered_confirmed';
            const isPending = !isShipped && !isDelivered;

            return (
              <div
                key={ord.id}
                className="bg-white rounded-2xl p-3.5 border border-slate-200 shadow-2xs space-y-3"
              >
                <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                  <div className="flex items-center gap-2">
                    <span className="font-mono font-extrabold text-xs text-brand-deep" dir="ltr">
                      {ord.orderNumber}
                    </span>
                    <span className="text-[10px] text-slate-400 flex items-center gap-1">
                      <Calendar className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>{ord.createdAt}</span>
                    </span>
                  </div>

                  {isDelivered ? (
                    <span className="text-[10px] bg-emerald-50 text-emerald-700 font-bold px-2 py-1 rounded-full border border-emerald-200 flex items-center gap-1">
                      <CheckCircle2 className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>به دست مشتری رسیده</span>
                    </span>
                  ) : isShipped ? (
                    <span className="text-[10px] bg-brand-soft text-brand-deep font-bold px-2 py-1 rounded-full border border-brand-border flex items-center gap-1">
                      <Truck className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>ارسال شده</span>
                    </span>
                  ) : ord.status === 'processing' ? (
                    <span className="text-[10px] bg-brand-soft text-brand-deep font-bold px-2 py-1 rounded-full border border-brand-border flex items-center gap-1">
                      <Package className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>در حال آماده‌سازی</span>
                    </span>
                  ) : (
                    <span className="text-[10px] bg-amber-50 text-amber-700 font-bold px-2 py-1 rounded-full border border-amber-200 flex items-center gap-1">
                      <Clock className="w-3 h-3"  strokeWidth={1.5}/>
                      <span>منتظر ارسال</span>
                    </span>
                  )}
                </div>

                <div className="grid grid-cols-2 gap-2 text-[11px] text-slate-600 bg-slate-50 p-2.5 rounded-xl">
                  <div className="flex items-center gap-1.5">
                    <User className="w-3.5 h-3.5 text-slate-400"  strokeWidth={1.5}/>
                    <span>مشتری: <strong>{ord.customerName}</strong></span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <Package className="w-3.5 h-3.5 text-slate-400"  strokeWidth={1.5}/>
                    <span>تعداد محصولات: <strong>{toPersianDigits(ord.itemCount)}</strong></span>
                  </div>
                </div>

                <SupplierOrderLines items={ord.items} />

                {ord.trackingNumber && (
                  <div className="flex items-center justify-between text-[10px] bg-brand-soft/70 p-2 rounded-xl text-brand-deep font-mono" dir="ltr">
                    <span>شرکت حمل: {ord.carrier || 'پست پیشتاز'}</span>
                    <span>کد رهگیری: {ord.trackingNumber}</span>
                  </div>
                )}

                <div className="border-t border-slate-100 pt-2">
                  <button
                    type="button"
                    onClick={() => void toggleAddons(ord)}
                    className="flex items-center gap-1.5 text-[11px] font-bold"
                    style={{ color: '#5d44f8' }}
                  >
                    <Package className="w-3.5 h-3.5" strokeWidth={1.5} />
                    <span>محصولات مکمل شما در این سفارش</span>
                    <ChevronDown className={`w-3.5 h-3.5 transition-transform ${expandedOrderId === ord.id ? 'rotate-180' : ''}`} strokeWidth={1.5} />
                  </button>
                  {expandedOrderId === ord.id && (
                    <div className="mt-2 space-y-1.5">
                      {loadingAddonId === ord.id ? (
                        <p className="text-[10px] text-slate-400">در حال دریافت…</p>
                      ) : !addonsByOrder[ord.id] || addonsByOrder[ord.id].addons.length === 0 ? (
                        <p className="text-[10px] text-slate-400">در این سفارش، محصول مکملی از تو انتخاب نشده است.</p>
                      ) : (
                        <>
                          {addonsByOrder[ord.id].addons.map((addon, addonIdx) => (
                            <div key={addonIdx} className="flex items-center justify-between text-[11px] text-slate-600 bg-slate-50 p-2 rounded-lg">
                              <span className="truncate">{addon.title}<span className="text-slate-400"> — روی «{addon.productTitle}»</span></span>
                              <span className="font-bold text-slate-700 shrink-0 mr-2">{toPersianDigits(addon.feePerUnit.toLocaleString())} × {toPersianDigits(addon.quantity)} = {toPersianDigits(addon.total.toLocaleString())} تومان</span>
                            </div>
                          ))}
                          <div className="flex items-center justify-between text-[11px] font-bold text-slate-800 px-1">
                            <span>جمع محصولات مکمل تو</span>
                            <span>{toPersianDigits(addonsByOrder[ord.id].total.toLocaleString())} تومان</span>
                          </div>
                        </>
                      )}
                    </div>
                  )}
                </div>

                {isPending && (
                  <div className="space-y-2">
                    {ord.status === 'pending' && (
                      <button
                        type="button"
                        disabled={acceptingId === ord.id}
                        onClick={() => void handleAcceptOrder(ord)}
                        className="w-full py-2 bg-white border border-brand-deep text-brand-deep hover:bg-brand-soft disabled:opacity-50 font-bold text-xs rounded-xl transition-all flex items-center justify-center gap-1.5"
                      >
                        <CheckCircle2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                        <span>{acceptingId === ord.id ? 'در حال ثبت…' : 'قبول سفارش'}</span>
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => {
                        setSelectedOrder(ord);
                        setTrackingNumber(ord.trackingNumber || '');
                      }}
                      className="w-full py-2 bg-brand-deep hover:bg-brand-deep text-white font-bold text-xs rounded-xl transition-all shadow-2xs flex items-center justify-center gap-1.5"
                    >
                      <Send className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>ثبت اطلاعات ارسال</span>
                    </button>
                  </div>
                )}
              </div>
            );
          })}
          {ordersHasMore && (
            <div className="pt-2 text-center">
              <button type="button" onClick={() => void fetchSupplierOrders(ordersNextCursor)} disabled={loading} className="rounded-xl bg-violet-600 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">نمایش بیشتر</button>
              <p className="mt-1 text-[10px] text-slate-500">جستجو و فیلتر روی سفارش‌های بارگذاری‌شده اعمال می‌شود.</p>
            </div>
          )}
        </div>
      )}

        </>
      )}

      {/* Shipping Modal / Drawer */}
      {selectedOrder && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-md w-full p-4 shadow-2xl border border-slate-100 space-y-3.5 text-xs">
            <div className="flex items-center justify-between border-b pb-2">
              <div className="flex items-center gap-1.5 text-brand-deep font-bold">
                <Truck className="w-4 h-4"  strokeWidth={1.5}/>
                <span>ثبت اطلاعات ارسال سفارش {selectedOrder.orderNumber}</span>
              </div>
              <button
                type="button"
                onClick={() => setSelectedOrder(null)}
                className="p-1 rounded-lg text-slate-400 hover:bg-slate-100"
              >
                <X className="w-4 h-4"  strokeWidth={1.5}/>
              </button>
            </div>

            <SupplierOrderLines items={selectedOrder.items} />

            <form onSubmit={handleConfirmShipping} className="space-y-3">
              {tapinConnected ? (
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => setShipMode('manual')}
                    className={`flex-1 py-2 rounded-xl text-[11px] font-bold border ${shipMode === 'manual' ? 'bg-brand-deep text-white border-brand-deep' : 'bg-white text-slate-600 border-slate-200'}`}
                  >
                    ثبت دستی
                  </button>
                  <button
                    type="button"
                    onClick={() => setShipMode('tapin')}
                    className={`flex-1 py-2 rounded-xl text-[11px] font-bold border ${shipMode === 'tapin' ? 'bg-brand-deep text-white border-brand-deep' : 'bg-white text-slate-600 border-slate-200'}`}
                  >
                    ثبت ارسال با تاپین
                  </button>
                </div>
              ) : (
                <p className="p-2.5 bg-slate-50 border border-slate-200 rounded-xl text-[10px] leading-5 text-slate-500">
                  برای ثبت خودکار مرسوله و دریافت کد رهگیری از تاپین، از تب «روش‌های ارسال» اتصال تاپین را فعال کن.
                </p>
              )}

              {shipMode === 'tapin' && tapinConnected && (
                <div className="space-y-3">
                  <div>
                    <label className="text-[11px] font-bold text-slate-700 block mb-1">
                      سرویس تاپین:
                    </label>
                    <select
                      value={tapinService}
                      onChange={(e) => setTapinService(e.target.value as 'post' | 'tipax')}
                      className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden font-bold"
                    >
                      <option value="post" disabled>پست (به‌زودی)</option>
                      <option value="tipax">تیپاکس</option>
                    </select>
                    <p className="mt-1 text-[10px] leading-5 text-slate-400">
                      ثبت مرسوله پست تاپین به‌زودی فعال می‌شود؛ در حال حاضر فقط تیپاکس ثبت می‌شود و باید برای فروشگاهت در تاپین فعال شده باشد.
                    </p>
                  </div>

                  <div>
                    <label className="text-[11px] font-bold text-slate-700 block mb-1">
                      جعبه پستی:
                    </label>
                    <select
                      value={tapinBoxId}
                      onChange={(e) => setTapinBoxId(e.target.value)}
                      className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden font-bold"
                    >
                      {tapinBoxes.map((b) => (
                        <option key={b.id} value={b.id}>
                          {b.title} ({toPersianDigits(b.lengthCm)}×{toPersianDigits(b.widthCm)}×{toPersianDigits(b.heightCm)} سانتی‌متر){b.id === tapinDefaultBoxId ? ' — پیشنهادی سیستم' : ''}
                        </option>
                      ))}
                    </select>
                  </div>

                  {tapinTracking ? (
                    <div className="p-2.5 bg-emerald-50 border border-emerald-200 rounded-xl text-[11px] text-emerald-700 space-y-2">
                      <p>
                        کد رهگیری تاپین: <span className="font-mono font-bold" dir="ltr">{tapinTracking}</span>
                      </p>
                      <button
                        type="button"
                        onClick={() => void handlePrintTapinLabel()}
                        disabled={printingLabel}
                        className="flex items-center gap-1.5 rounded-lg bg-emerald-600 px-3 py-2 text-[11px] font-bold text-white disabled:opacity-50"
                      >
                        <Printer className="w-3.5 h-3.5" strokeWidth={1.5} />
                        {printingLabel ? 'در حال دریافت…' : 'چاپ برچسب'}
                      </button>
                    </div>
                  ) : (
                    <div className="p-2.5 bg-brand-soft border border-brand-border rounded-xl text-[10px] text-brand-deep flex items-start gap-1.5">
                      <AlertCircle className="w-4 h-4 text-brand-deep shrink-0 mt-0.5" strokeWidth={1.5} />
                      <span>با ثبت، مرسوله مستقیم در تاپین ثبت می‌شود و کد رهگیری به‌صورت خودکار روی سفارش می‌نشیند.</span>
                    </div>
                  )}
                </div>
              )}

              {shipMode === 'manual' && (
              <>
              <div>
                <label className="text-[11px] font-bold text-slate-700 block mb-1">
                  شرکت پستی:
                </label>
                <select
                  value={carrier}
                  onChange={(e) => setCarrier(e.target.value)}
                  className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden font-bold"
                >
                  <option value="پست پیشتاز">پست پیشتاز</option>
                  <option value="تیپاکس">تیپاکس</option>
                  <option value="پست معمولی">پست معمولی</option>
                  <option value="چاپار">چاپار</option>
                </select>
              </div>

              <div>
                <label className="text-[11px] font-bold text-slate-700 block mb-1">
                  کد رهگیری مرسوله:
                </label>
                <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-3 py-2 focus-within:border-brand focus-within:bg-white">
                  <Hash className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
                  <input
                    type="text"
                    value={trackingNumber}
                    onChange={(e) => setTrackingNumber(e.target.value)}
                    placeholder="مثال: 9876543210123"
                    className="w-full bg-transparent text-xs font-mono font-bold text-slate-900 focus:outline-hidden"
                    dir="ltr"
                    required
                  />
                </div>
              </div>

              <div>
                <label className="text-[11px] font-bold text-slate-700 block mb-1">
                  تاریخ ارسال:
                </label>
                <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-3 py-2">
                  <Calendar className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
                  <input
                    type="text"
                    value={shippingDate}
                    onChange={(e) => setShippingDate(e.target.value)}
                    className="w-full bg-transparent text-xs font-mono font-bold text-slate-900 focus:outline-hidden"
                    dir="ltr"
                  />
                </div>
              </div>

              <div className="p-2.5 bg-brand-soft border border-brand-border rounded-xl text-[10px] text-brand-deep flex items-start gap-1.5">
                <AlertCircle className="w-4 h-4 text-brand-deep shrink-0 mt-0.5"  strokeWidth={1.5}/>
                <span>
                  با ثبت اطلاعات ارسال، وضعیت سفارش به «ارسال شده» تغییر می‌کند.
                </span>
              </div>
              </>
              )}

              {shipMode === 'tapin' && tapinTracking ? (
                <div className="flex gap-2 pt-2 border-t">
                  <button
                    type="button"
                    onClick={() => setSelectedOrder(null)}
                    className="flex-1 py-2.5 bg-brand-deep text-white font-bold rounded-xl"
                  >
                    بستن
                  </button>
                </div>
              ) : (
              <div className="flex gap-2 pt-2 border-t">
                <button
                  type="submit"
                  disabled={submittingShipping}
                  className="flex-1 py-2.5 bg-brand-deep hover:bg-brand-deep disabled:opacity-50 text-white font-bold rounded-xl transition-all shadow-2xs flex items-center justify-center gap-1.5"
                >
                  <Send className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                  <span>{submittingShipping ? 'در حال ثبت…' : shipMode === 'tapin' ? 'ثبت مرسوله در تاپین' : 'ثبت و ارسال اطلاعات'}</span>
                </button>
                <button
                  type="button"
                  onClick={() => setSelectedOrder(null)}
                  className="px-3 py-2.5 bg-slate-100 hover:bg-slate-200 text-slate-600 font-bold rounded-xl"
                >
                  انصراف
                </button>
              </div>
              )}
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
