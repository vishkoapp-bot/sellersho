import { SupplierSectionHeading } from './SupplierSectionHeading';
import { SupplierDialog } from './SupplierDialog';
import { ContextSupportButton } from '../support/ContextSupportButton';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import React, { useState, useEffect, useRef, useCallback } from 'react';
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
  XCircle,
  Info,
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
  dispatchDueAt?: string;
  dispatchEstimate?: boolean;
  itemCount: number;
}

interface SupplierOrderAddon {
  title: string;
  productTitle: string;
  feePerUnit: number;
  quantity: number;
  total: number;
}

export const TERMINAL_ORDER_STATUSES = ['cancelled', 'failed'] as const;
export const isTerminalOrderStatus = (status: string): boolean =>
  (TERMINAL_ORDER_STATUSES as readonly string[]).includes(status);

export const isReadyOrder = (status: string): boolean =>
  status === 'pending' || status === 'processing';

const ORDER_FILTERS = [
  { id: 'all', label: 'همه' },
  { id: 'ready', label: 'منتظر ارسال' },
  { id: 'overdue', label: 'گذشته از موعد' },
  { id: 'shipped', label: 'ارسال شده' },
  { id: 'delivered', label: 'به دست مشتری رسیده' },
] as const;

const shippingFieldClass = 'min-h-11 w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-brand';

export function directOrderChip(status: string) {
  if (status === 'delivered' || status === 'delivered_confirmed') {
    return { text: 'به دست مشتری رسیده', cls: 'bg-emerald-50 text-emerald-700 border-emerald-200', Icon: CheckCircle2 };
  }
  if (status === 'shipped' || status === 'in_transit') {
    return { text: 'ارسال شده', cls: 'bg-brand-soft text-brand-deep border-brand-border', Icon: Truck };
  }
  if (status === 'processing') {
    return { text: 'در حال آماده‌سازی', cls: 'bg-brand-soft text-brand-deep border-brand-border', Icon: Package };
  }
  if (status === 'cancelled') {
    return { text: 'لغو شده', cls: 'bg-rose-50 text-rose-700 border-rose-200', Icon: XCircle };
  }
  if (status === 'failed') {
    return { text: 'ناموفق', cls: 'bg-rose-50 text-rose-700 border-rose-200', Icon: XCircle };
  }
  if (status === 'pending') {
    return { text: 'منتظر ارسال', cls: 'bg-amber-50 text-amber-700 border-amber-200', Icon: Clock };
  }
  if (status === 'awaiting_payment') {
    return { text: 'پرداخت تکمیل نشده', cls: 'bg-slate-50 text-slate-700 border-slate-200', Icon: Clock };
  }
  return { text: 'وضعیت نامشخص', cls: 'bg-slate-50 text-slate-600 border-slate-200', Icon: Info };
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
export const SupplierOrdersTab: React.FC<{ initialFilter?: 'all' | 'ready' | 'overdue' }> = ({ initialFilter = 'all' }) => {
  const {
    toPersianDigits
  } = useStore();
  const { success, error } = useToast();

  const [backendOrders, setBackendOrders] = useState<SupplierOrder[]>([]);
  const [ordersNextCursor, setOrdersNextCursor] = useState('');
  const [ordersHasMore, setOrdersHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [filterStatus, setFilterStatus] = useState<(typeof ORDER_FILTERS)[number]['id']>(initialFilter);
  const orderRequest = useRef(0);

  // Gap 14: view toggle — direct shipments to fulfil vs warehouse-routed
  // orders (read-only visibility), plus the accept-step in-flight marker.
  const [ordersView, setOrdersView] = useState<'direct' | 'warehouse'>('direct');
  const [warehouseOrders, setWarehouseOrders] = useState<SupplierWarehouseOrderDto[]>([]);
  const [warehousePage, setWarehousePage] = useState(1);
  const [warehouseTotal, setWarehouseTotal] = useState(0);
  const [warehouseLoading, setWarehouseLoading] = useState(false);
  const [warehouseError, setWarehouseError] = useState('');
  const [warehouseLoaded, setWarehouseLoaded] = useState(false);
  const [acceptingId, setAcceptingId] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const search = searchQuery.trim();
  const warehousePageSize = 20;

  // Addons expand state (live's Pe toggle per order card).
  const [expandedOrderId, setExpandedOrderId] = useState<string | null>(null);
  const [addonsByOrder, setAddonsByOrder] = useState<Record<string, { addons: SupplierOrderAddon[]; total: number }>>({});
  const [loadingAddonId, setLoadingAddonId] = useState<string | null>(null);

  // Shipping Modal State
  const [selectedOrder, setSelectedOrder] = useState<SupplierOrder | null>(null);
  const [carrier, setCarrier] = useState('پست پیشتاز');
  const [trackingNumber, setTrackingNumber] = useState('');
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
  const fetchSupplierOrders = useCallback(async (cursor?: string) => {
    const request = ++orderRequest.current;
    setLoading(true);
    setLoadError('');
    try {
      const res = await supplierApi.getOrders({ limit: 50, cursor, work: filterStatus === 'all' ? undefined : filterStatus, search });
      if (request !== orderRequest.current) return;
      setBackendOrders((prev) => (cursor ? [...prev, ...(res.orders || [])] : (res.orders || [])));
      setOrdersNextCursor(res.nextCursor || '');
      setOrdersHasMore(!!res.hasMore);
    } catch (err: any) {
      if (request === orderRequest.current) setLoadError(err?.message || 'دریافت سفارش‌ها انجام نشد.');
    } finally {
      if (request === orderRequest.current) setLoading(false);
    }
  }, [filterStatus, search]);

  useEffect(() => {
    setOrdersHasMore(false);
    setOrdersNextCursor('');
    setBackendOrders([]);
    setLoadError('');
    setLoading(true);
    const timer = window.setTimeout(() => void fetchSupplierOrders(), search ? 250 : 0);
    return () => {
      window.clearTimeout(timer);
      orderRequest.current += 1;
    };
  }, [fetchSupplierOrders, search]);

  const fetchWarehouseOrders = async (page = warehousePage) => {
    setWarehouseLoading(true);
    setWarehouseError('');
    try {
      const res = await supplierApi.getWarehouseOrders(page, warehousePageSize);
      setWarehouseOrders(res.orders || []);
      setWarehousePage(res.page);
      setWarehouseTotal(res.total);
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
    if (!selectedOrder || submittingShipping || tapinTracking) return;
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
      await supplierApi.submitShipping(selectedOrder.orderId, { carrier, trackingNumber: trackingNumber.trim() });

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
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="سفارش‌های من" description="سفارش‌های ارسال مستقیم و انبار مرکزی را پیگیری کن." icon={<Truck />} />
      <div role="group" aria-label="مسیر ارسال سفارش" className="grid grid-cols-2 gap-2 rounded-[20px] border border-[#e6e2f0] bg-white p-2 shadow-sm">
        {([
          { id: 'direct', label: 'ارسال توسط من', Icon: Truck },
          { id: 'warehouse', label: 'ارسال از انبار مرکزی', Icon: Warehouse },
        ] as const).map(({ id, label, Icon }) => (
          <button key={id} type="button" aria-pressed={ordersView === id} onClick={() => setOrdersView(id)}
            className={`flex min-h-12 items-center justify-center gap-2 rounded-xl px-2 text-sm font-bold transition-colors focus-visible:ring-2 focus-visible:ring-brand ${ordersView === id ? 'bg-[#382660] text-white' : 'bg-[#f7f4fc] text-[#382660] hover:bg-brand-soft'}`}>
            <Icon aria-hidden="true" className="size-4 shrink-0" strokeWidth={1.75} />
            <span>{label}</span>
          </button>
        ))}
      </div>

      {ordersView === 'warehouse' ? (
        <>
          <div className="flex flex-wrap items-start justify-between gap-2 px-1">
            <div>
              <h3 className="text-base font-extrabold text-[#261b41]">سفارش‌های انبار مرکزی</h3>
              <p className="mt-1 text-sm leading-6 text-slate-600">انبار مرکزی این سفارش‌ها را می‌فرستد. مبلغ‌ها بر اساس قیمت پایهٔ کالاهای توست.</p>
            </div>
            {warehouseLoaded && !warehouseLoading && !warehouseError && <div className="rounded-lg bg-brand-soft px-2.5 py-1 text-xs font-bold text-brand-deep">
              <span>{toPersianDigits(warehouseTotal)} سفارش</span>
            </div>}
          </div>

          <SupplierResourceState loading={warehouseLoading} error={warehouseError} retry={() => fetchWarehouseOrders()} />
          {warehouseLoaded && !warehouseLoading && !warehouseError && warehouseTotal === 0 ? (
            <SupplierEmptyState icon={<Warehouse />} title="هنوز سفارشی از انبار مرکزی ثبت نشده است." description="سفارش‌های پرداخت‌شدهٔ کالاهای موجود در انبار مرکزی اینجا نمایش داده می‌شوند." />
          ) : (
            <div className="space-y-2.5">
              {warehouseOrders.map((ord) => {
                const chip = warehouseStatusChip(ord.status);
                return (
                  <article key={ord.orderId} className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5 space-y-3">
                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[#eeebf4] pb-3">
                      <div className="flex items-center gap-2">
                        <h4 className="text-sm font-extrabold text-brand-deep">سفارش <bdi dir="ltr" className="font-mono">{ord.orderNumber}</bdi></h4>
                        <span className="flex items-center gap-1 text-xs text-slate-600">
                          <Calendar aria-hidden="true" className="size-3.5" strokeWidth={1.5} />
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
                    <ContextSupportButton role="supplier" kind="order" referenceId={ord.orderId} />
                  </article>
                );
              })}
            </div>
          )}
          {warehouseLoaded && (warehousePage > 1 || warehouseTotal > warehousePageSize) && (
            <nav aria-label="صفحه‌های سفارش انبار مرکزی" className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-slate-200 bg-white p-2 text-sm">
              <button type="button" disabled={warehouseLoading || warehousePage <= 1} onClick={() => void fetchWarehouseOrders(warehousePage - 1)} className="min-h-11 rounded-lg px-3 font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">صفحه قبل</button>
              <span role="status" className="text-slate-600">صفحه {toPersianDigits(warehousePage)}</span>
              <button type="button" disabled={warehouseLoading || warehousePage * warehousePageSize >= warehouseTotal} onClick={() => void fetchWarehouseOrders(warehousePage + 1)} className="min-h-11 rounded-lg px-3 font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">صفحه بعد</button>
            </nav>
          )}
        </>
      ) : (
        <>
      <div className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-3 shadow-sm sm:p-4">
        <label htmlFor="supplier-orders-search" className="block text-sm font-bold text-slate-800">جستجوی سفارش‌ها</label>
        <div className="relative">
          <Search aria-hidden="true" className="w-4 h-4 text-slate-500 absolute start-3 top-3.5" strokeWidth={1.5} />
          <input
            id="supplier-orders-search"
            type="search"
            aria-label="جستجوی سفارش‌ها"
            maxLength={100}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="شماره سفارش، کد رهگیری یا نام خریدار"
            className="min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] ps-9 pe-3 text-sm focus:bg-white focus-visible:ring-2 focus-visible:ring-brand"
          />
        </div>

        <div role="group" aria-label="وضعیت سفارش" className="flex items-center gap-1.5 flex-wrap text-xs font-bold">
          {ORDER_FILTERS.map(({ id, label }) => (
            <button key={id} type="button" aria-pressed={filterStatus === id} onClick={() => setFilterStatus(id)}
              className={`min-h-11 rounded-xl px-3 transition-colors focus-visible:ring-2 focus-visible:ring-brand ${filterStatus === id ? 'bg-brand-deep text-white' : 'bg-slate-100 text-slate-700 hover:bg-slate-200'}`}>
              {label}
            </button>
          ))}
        </div>
        {filterStatus === 'overdue' && <p className="text-sm leading-6 text-slate-600">موعد ارسال بعضی سفارش‌های قدیمی بر اساس زمان آماده‌سازی کالا برآورد شده است.</p>}
      </div>

      {loading && <div role="status" className="rounded-2xl bg-white p-4 text-sm text-slate-600">در حال دریافت سفارش‌ها…</div>}
      {!loading && !loadError && backendOrders.length > 0 && <p role="status" className="px-1 text-sm text-slate-600">{toPersianDigits(backendOrders.length)} سفارش نمایش داده شده است.</p>}
      {loadError && (
        <div role="alert" className="space-y-2 rounded-2xl bg-rose-50 p-4 text-xs text-rose-800">
          <p>{loadError}</p>
          {(loadError.includes('دسترسی') || loadError.includes('Forbidden')) ? (
            <p>دسترسی شما به این بخش برقرار نیست. اگر حسابت به‌تازگی فعال شده، یک بار خارج و دوباره وارد شو؛ اگر مشکل ادامه داشت، با پشتیبانی تماس بگیر.</p>
          ) : (
            <button type="button" onClick={() => void fetchSupplierOrders()} className="min-h-11 rounded-lg bg-white px-3 py-2 font-bold focus-visible:ring-2 focus-visible:ring-rose-700">تلاش دوباره</button>
          )}
        </div>
      )}
      {!loading && !loadError && backendOrders.length === 0 ? (
        filterStatus === 'all' && !search ? (
          <SupplierEmptyState icon={<Package />} title="هنوز سفارش ارسال مستقیم نداری." description="سفارش‌های ارسال مستقیم و فرم ثبت کد رهگیری آن‌ها اینجا نمایش داده می‌شوند." />
        ) : (
          <SupplierEmptyState icon={<Package />} title={searchQuery.trim() ? 'سفارشی با این جستجو پیدا نشد.' : 'سفارشی در این وضعیت نیست.'}
            action={<button type="button" onClick={() => { setSearchQuery(''); setFilterStatus('all'); }} className="min-h-11 rounded-xl bg-brand-soft px-4 text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand">پاک کردن فیلترها و دیدن همه سفارش‌ها</button>} />
        )
      ) : (
        <div className="space-y-2.5">
          {backendOrders.map((ord) => {
            const { text: chipText, cls: chipClasses, Icon: ChipIcon } = directOrderChip(ord.status);

            return (
              <article
                key={ord.id}
                className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5 space-y-3"
              >
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[#eeebf4] pb-3">
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-extrabold text-brand-deep">سفارش <bdi dir="ltr" className="font-mono">{ord.orderNumber}</bdi></h3>
                    <span className="flex items-center gap-1 text-xs text-slate-600">
                      <Calendar aria-hidden="true" className="size-3.5" strokeWidth={1.5} />
                      <span>{ord.createdAt}</span>
                    </span>
                  </div>

                  <span className={`text-[10px] font-bold px-2 py-1 rounded-full border flex items-center gap-1 ${chipClasses}`}>
                    <ChipIcon className="w-3 h-3" strokeWidth={1.5}/>
                    <span>{chipText}</span>
                  </span>
                </div>

                {isReadyOrder(ord.status) && ord.dispatchDueAt && <p className={`text-xs font-bold ${Date.parse(ord.dispatchDueAt) < Date.now() ? 'text-rose-800' : 'text-slate-700'}`}>{ord.dispatchEstimate ? 'موعد برآوردی ارسال' : 'مهلت ارسال'} · <time dateTime={ord.dispatchDueAt}>{new Date(ord.dispatchDueAt).toLocaleString('fa-IR', { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Asia/Tehran' })}</time></p>}
                <div className="grid gap-2 rounded-xl bg-[#f6f3fb] p-3 text-sm text-slate-700 sm:grid-cols-2">
                  <div className="flex items-center gap-1.5">
                    <User className="w-3.5 h-3.5 text-slate-400"  strokeWidth={1.5}/>
                    <span>مشتری: <strong>{ord.customerName}</strong></span>
                  </div>
                  {Number.isFinite(ord.itemCount) && (
                    <div className="flex items-center gap-1.5">
                      <Package aria-hidden="true" className="size-3.5 shrink-0 text-slate-500" strokeWidth={1.5} />
                      <span>تعداد محصولات: <strong>{toPersianDigits(ord.itemCount)}</strong></span>
                    </div>
                  )}
                </div>

                <SupplierOrderLines items={ord.items} />

                {ord.trackingNumber && (
                  <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-brand-soft px-3 py-2 text-xs text-brand-deep">
                    <span>شرکت حمل: {ord.carrier || 'پست پیشتاز'}</span>
                    <span>کد رهگیری: <bdi dir="ltr" className="break-all font-bold">{ord.trackingNumber}</bdi></span>
                  </div>
                )}

                {isReadyOrder(ord.status) && (
                  <div className="flex flex-wrap gap-2">
                    {ord.status === 'pending' && (
                      <button type="button" disabled={!!acceptingId} onClick={() => void handleAcceptOrder(ord)}
                        className="inline-flex min-h-11 flex-1 items-center justify-center gap-1.5 rounded-xl border border-brand-border px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50 sm:flex-none">
                        <CheckCircle2 aria-hidden="true" className="size-4" strokeWidth={1.5} />
                        {acceptingId === ord.id ? 'در حال ثبت…' : 'قبول سفارش'}
                      </button>
                    )}
                    <button type="button" onClick={() => {
                      setSelectedOrder(ord);
                      setTrackingNumber(ord.trackingNumber || '');
                    }} className="inline-flex min-h-11 flex-1 items-center justify-center gap-1.5 rounded-xl bg-brand-deep px-3 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 sm:flex-none">
                      <Send aria-hidden="true" className="size-4" strokeWidth={1.5} />
                      ثبت اطلاعات ارسال
                    </button>
                  </div>
                )}

                <div className="flex flex-wrap items-center justify-between gap-x-3 border-t border-slate-100 pt-2">
                  <button
                    type="button"
                    onClick={() => void toggleAddons(ord)}
                    aria-expanded={expandedOrderId === ord.id}
                    className="flex min-h-11 items-center gap-1.5 rounded-lg px-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand"
                  >
                    <span>محصولات مکمل</span>
                    <ChevronDown aria-hidden="true" className={`w-3.5 h-3.5 ${expandedOrderId === ord.id ? 'rotate-180' : ''}`} strokeWidth={1.5} />
                  </button>
                  <details className="rounded-lg">
                    <summary className="min-h-11 cursor-pointer rounded-lg px-2 py-2.5 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">پشتیبانی</summary>
                    <div className="flex flex-wrap gap-2 py-2">
                      <ContextSupportButton role="supplier" kind="order" referenceId={ord.orderId} />
                      <ContextSupportButton role="supplier" kind="shipment" referenceType="direct_shipment" referenceId={ord.id} />
                    </div>
                  </details>
                  {expandedOrderId === ord.id && (
                    <div className="mt-2 w-full space-y-1.5">
                      {loadingAddonId === ord.id ? (
                        <p role="status" className="text-sm text-slate-600">در حال دریافت…</p>
                      ) : !addonsByOrder[ord.id] || addonsByOrder[ord.id].addons.length === 0 ? (
                        <p role="status" className="text-sm text-slate-600">این سفارش محصول مکمل ندارد.</p>
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

              </article>
            );
          })}
        </div>
      )}
          {!loading && !loadError && ordersHasMore && (
            <div className="pt-2 text-center">
              <button type="button" onClick={() => void fetchSupplierOrders(ordersNextCursor)} disabled={loading} className="min-h-11 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">نمایش سفارش‌های بیشتر</button>
            </div>
          )}

        </>
      )}

      {/* Shipping Modal / Drawer */}
      {selectedOrder && (
        <SupplierDialog titleId="supplier-shipping-dialog-title" onDismiss={submittingShipping ? undefined : () => setSelectedOrder(null)}>
          <div className="space-y-3.5 text-xs">
            <div className="flex items-center justify-between border-b pb-2">
              <div className="flex items-center gap-1.5 text-brand-deep font-bold">
                <Truck className="w-4 h-4"  strokeWidth={1.5}/>
                <h3 id="supplier-shipping-dialog-title" className="text-sm font-extrabold">ثبت ارسال سفارش {selectedOrder.orderNumber}</h3>
              </div>
              <button
                type="button"
                disabled={submittingShipping}
                aria-label="بستن فرم ثبت ارسال"
                onClick={() => setSelectedOrder(null)}
                className="grid size-11 shrink-0 place-items-center rounded-xl text-slate-600 hover:bg-slate-100 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60"
              >
                <X className="w-4 h-4"  strokeWidth={1.5}/>
              </button>
            </div>

            <SupplierOrderLines items={selectedOrder.items} />

            <form onSubmit={handleConfirmShipping} className="space-y-3">
              {tapinConnected ? (
                <div role="group" aria-label="شیوهٔ ثبت ارسال" className="flex gap-2">
                  <button
                    type="button"
                    disabled={submittingShipping || !!tapinTracking}
                    aria-pressed={shipMode === 'manual'}
                    onClick={() => setShipMode('manual')}
                    className={`min-h-11 flex-1 rounded-xl border px-2 py-2 text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand ${shipMode === 'manual' ? 'border-brand-deep bg-brand-deep text-white' : 'border-slate-200 bg-white text-slate-700'}`}
                  >
                    ثبت دستی
                  </button>
                  <button
                    type="button"
                    disabled={submittingShipping || !!tapinTracking}
                    aria-pressed={shipMode === 'tapin'}
                    onClick={() => setShipMode('tapin')}
                    className={`min-h-11 flex-1 rounded-xl border px-2 py-2 text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand ${shipMode === 'tapin' ? 'border-brand-deep bg-brand-deep text-white' : 'border-slate-200 bg-white text-slate-700'}`}
                  >
                    ثبت ارسال با تاپین
                  </button>
                </div>
              ) : (
                <p className="text-xs leading-6 text-slate-600">
                  برای ثبت خودکار مرسوله، تاپین را در «روش‌های ارسال» وصل کن.
                </p>
              )}

              {shipMode === 'tapin' && tapinConnected && (
                <div className="space-y-3">
                  <div>
                    <label htmlFor="supplier-shipping-tapin-service" className="mb-1 block text-sm font-bold text-slate-700">
                      سرویس تاپین
                    </label>
                    <select
                      id="supplier-shipping-tapin-service"
                      value={tapinService}
                      disabled={submittingShipping || !!tapinTracking}
                      onChange={(e) => setTapinService(e.target.value as 'post' | 'tipax')}
                      className={shippingFieldClass}
                    >
                      <option value="post" disabled>پست (به‌زودی)</option>
                      <option value="tipax">تیپاکس</option>
                    </select>
                    <p className="mt-1 text-xs leading-6 text-slate-600">
                      تیپاکس باید برای فروشگاهت در تاپین فعال باشد.
                    </p>
                  </div>

                  <div>
                    <label htmlFor="supplier-shipping-tapin-box" className="mb-1 block text-sm font-bold text-slate-700">
                      جعبهٔ مرسوله
                    </label>
                    <select
                      id="supplier-shipping-tapin-box"
                      value={tapinBoxId}
                      disabled={submittingShipping || !!tapinTracking}
                      onChange={(e) => setTapinBoxId(e.target.value)}
                      className={shippingFieldClass}
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
                      <p role="status">
                        کد رهگیری تاپین: <span className="font-mono font-bold" dir="ltr">{tapinTracking}</span>
                      </p>
                      <button
                        type="button"
                        onClick={() => void handlePrintTapinLabel()}
                        disabled={printingLabel}
                        className="flex min-h-11 items-center gap-1.5 rounded-lg bg-emerald-700 px-3 py-2 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-emerald-800 disabled:opacity-50"
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
                <label htmlFor="supplier-shipping-carrier" className="mb-1 block text-sm font-bold text-slate-700">
                  شرکت ارسال
                </label>
                <select
                  id="supplier-shipping-carrier"
                  value={carrier}
                  disabled={submittingShipping}
                  onChange={(e) => setCarrier(e.target.value)}
                  className={shippingFieldClass}
                >
                  <option value="پست پیشتاز">پست پیشتاز</option>
                  <option value="تیپاکس">تیپاکس</option>
                  <option value="پست معمولی">پست معمولی</option>
                  <option value="چاپار">چاپار</option>
                </select>
              </div>

              <div>
                <label htmlFor="supplier-shipping-tracking" className="mb-1 block text-sm font-bold text-slate-700">
                  کد رهگیری مرسوله
                </label>
                <div className="relative">
                  <Hash aria-hidden="true" className="absolute end-3 top-3.5 size-4 text-slate-500" strokeWidth={1.5} />
                  <input
                    id="supplier-shipping-tracking"
                    type="text"
                    value={trackingNumber}
                    disabled={submittingShipping}
                    onChange={(e) => setTrackingNumber(e.target.value)}
                    placeholder="مثال: 9876543210123"
                    className={`${shippingFieldClass} ps-9 font-mono`}
                    dir="ltr"
                    required
                  />
                </div>
              </div>

              <div className="p-2.5 bg-brand-soft border border-brand-border rounded-xl text-[10px] text-brand-deep flex items-start gap-1.5">
                <AlertCircle className="w-4 h-4 text-brand-deep shrink-0 mt-0.5"  strokeWidth={1.5}/>
                <span>
                  زمان ثبت خودکار ذخیره می‌شود و وضعیت مرسوله به «ارسال شده» تغییر می‌کند.
                </span>
              </div>
              </>
              )}

              {shipMode === 'tapin' && tapinTracking ? (
                <div className="flex gap-2 pt-2 border-t">
                  <button
                    type="button"
                    onClick={() => setSelectedOrder(null)}
                    className="min-h-11 flex-1 rounded-xl bg-brand-deep py-2.5 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand"
                  >
                    بستن
                  </button>
                </div>
              ) : (
              <div className="flex gap-2 pt-2 border-t">
                <button
                  type="submit"
                  disabled={submittingShipping}
                  className="flex min-h-11 flex-1 items-center justify-center gap-1.5 rounded-xl bg-brand-deep py-2.5 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50"
                >
                  <Send className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                  <span>{submittingShipping ? 'در حال ثبت…' : shipMode === 'tapin' ? 'ثبت مرسوله در تاپین' : 'ثبت و ارسال اطلاعات'}</span>
                </button>
                <button
                  type="button"
                  disabled={submittingShipping}
                  onClick={() => setSelectedOrder(null)}
                  className="min-h-11 rounded-xl bg-slate-100 px-3 py-2.5 text-sm font-bold text-slate-700 hover:bg-slate-200 focus-visible:ring-2 focus-visible:ring-brand"
                >
                  انصراف
                </button>
              </div>
              )}
            </form>
          </div>
        </SupplierDialog>
      )}
    </div>
  );
};
