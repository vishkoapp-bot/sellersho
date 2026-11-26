import { useEffect, useState, type FormEvent } from 'react';
import { ChevronLeft, Package, Search, Truck } from 'lucide-react';
import { apiRequest, ApiError } from '../../services/apiClient';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';

interface SupplierLeg {
  id: string;
  status: string;
  carrier: string;
  trackingNumber: string;
  shippedAt?: string | null;
  deliveredAt?: string | null;
}

interface TrackedOrder {
  id: string;
  orderNumber: string;
  storeName: string;
  status: string;
  province: string;
  city: string;
  fulfillmentMethod: string;
  trackingCode: string;
  trackingNumber?: string;
  carrier?: string;
  createdAt: string;
  packedAt?: string;
  shippedAt?: string;
  confirmedAt?: string;
  supplierShipments: SupplierLeg[];
}

interface OrdersPage {
  items: TrackedOrder[];
  nextCursor?: string;
}

interface OrdersQuery {
  search: string;
  status: string;
  cursor?: string;
}

const defaultQuery: OrdersQuery = { search: '', status: '' };
const orderStatusOptions = [
  { value: '', label: 'همه وضعیت‌ها' },
  { value: 'pending', label: 'در انتظار' },
  { value: 'preparing', label: 'در حال آماده‌سازی' },
  { value: 'shipped', label: 'ارسال‌شده' },
  { value: 'delivered', label: 'تحویل‌شده' },
  { value: 'delivery_disputed', label: 'اختلاف در تحویل' },
  { value: 'cancelled', label: 'لغوشده' },
];

const orderStatusLabels: Record<string, string> = {
  pending: 'در انتظار',
  processing: 'در حال آماده‌سازی',
  picking: 'در حال جمع‌آوری',
  picked: 'جمع‌آوری‌شده',
  packed: 'بسته‌بندی‌شده',
  shipped: 'ارسال‌شده',
  in_transit: 'در مسیر',
  delivered: 'تحویل‌شده',
  delivered_unconfirmed: 'تحویل‌شده، در انتظار تأیید',
  delivered_confirmed: 'تحویل تأییدشده',
  delivery_disputed: 'اختلاف در تحویل',
  cancelled: 'لغوشده',
  failed: 'ارسال ناموفق',
  returned: 'برگشت‌خورده',
};

const dateFormat = new Intl.DateTimeFormat('fa-IR', { dateStyle: 'medium', timeZone: 'Asia/Tehran' });
const eventFormat = new Intl.DateTimeFormat('fa-IR', { dateStyle: 'medium', timeStyle: 'short', timeZone: 'Asia/Tehran' });
const shipmentSources: Record<string, string> = {
  warehouse: 'انبار مرکزی',
  supplier_direct: 'ارسال مستقیم تأمین‌کننده',
  mixed: 'انبار مرکزی و تأمین‌کننده',
};
const cardClass = 'min-w-0 rounded-[22px] border border-slate-200 bg-white p-4 shadow-sm sm:p-5';
const fieldClass = 'min-h-11 w-full min-w-0 rounded-xl border border-slate-300 bg-white px-3 text-sm text-slate-900 focus-visible:outline-2 focus-visible:outline-brand';
const secondaryButtonClass = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-soft px-3 text-sm font-bold text-brand-deep hover:bg-brand-border focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand';

function OrderStatus({ status }: { status: string }) {
  const tone = ['delivered', 'delivered_confirmed', 'delivered_unconfirmed'].includes(status)
    ? 'bg-emerald-50 text-emerald-800'
    : ['cancelled', 'failed', 'delivery_disputed', 'returned'].includes(status)
      ? 'bg-rose-50 text-rose-800'
      : 'bg-brand-soft text-brand-deep';
  return <span className={`rounded-full px-2.5 py-1 text-xs font-bold ${tone}`}>{orderStatusLabels[status] ?? 'وضعیت نامشخص'}</span>;
}

function OrderTime({ value, full }: { value: string; full?: boolean }) {
  return <time dateTime={value}>{(full ? eventFormat : dateFormat).format(new Date(value))}</time>;
}

function TrackingNumber({ code }: { code: string }) {
  return <bdi dir="ltr" className="break-all select-all font-mono font-bold text-brand-deep">{code}</bdi>;
}

function OrderTrackingDetails({ order, close }: { order: TrackedOrder; close: () => void }) {
  const title = 'جزئیات رهگیری سفارش';
  const showOrderCode = order.trackingCode && order.trackingCode !== order.trackingNumber &&
    !order.supplierShipments.some(leg => leg.trackingNumber === order.trackingCode);
  const events = [
    ['ثبت سفارش', order.createdAt], ['بسته‌بندی', order.packedAt],
    ['ارسال سفارش', order.shippedAt], ['تأیید تحویل', order.confirmedAt],
  ];
  return (
    <SupplierDialog titleId="admin-order-details-title" onDismiss={close}>
      <SupplierDialogHeading id="admin-order-details-title" title={title} onClose={close} />
      <div className="space-y-4 text-sm text-slate-700">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span>شماره سفارش <TrackingNumber code={order.orderNumber} /></span>
          <OrderStatus status={order.status} />
        </div>
        <div className="space-y-1 rounded-xl bg-slate-50 p-3">
          <p className="font-bold text-slate-900">{shipmentSources[order.fulfillmentMethod] ?? 'روش ارسال نامشخص'}</p>
          {showOrderCode && <p>کد رهگیری سفارش <TrackingNumber code={order.trackingCode} /></p>}
          {order.carrier && <p>شرکت حمل‌ونقل {order.carrier}</p>}
          {order.trackingNumber && <p>کد رهگیری مرسوله <TrackingNumber code={order.trackingNumber} /></p>}
          {!order.trackingNumber && !order.supplierShipments.some(leg => leg.trackingNumber) && <p>هنوز کد رهگیری مرسوله ثبت نشده است.</p>}
        </div>
        {order.supplierShipments.length > 0 && (
          <section aria-label="مرسوله‌های تأمین‌کننده" className="space-y-2">
            <h4 className="font-extrabold text-slate-900">مرسوله‌های تأمین‌کننده</h4>
            <ul className="space-y-2">
              {order.supplierShipments.map(leg => (
                <li key={leg.id} className="space-y-2 rounded-xl border border-slate-200 p-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-bold">{leg.carrier || 'مرسوله تأمین‌کننده'}</span>
                    <OrderStatus status={leg.status} />
                  </div>
                  <p>{leg.trackingNumber ? <>کد رهگیری <TrackingNumber code={leg.trackingNumber} /></> : 'کد رهگیری ثبت نشده است.'}</p>
                  {leg.shippedAt && <p>ارسال <OrderTime value={leg.shippedAt} full /></p>}
                  {leg.deliveredAt && <p>تحویل <OrderTime value={leg.deliveredAt} full /></p>}
                </li>
              ))}
            </ul>
          </section>
        )}
        <section aria-label="زمان‌های ثبت‌شده">
          <h4 className="font-extrabold text-slate-900">زمان‌های ثبت‌شده</h4>
          <dl className="mt-1 divide-y divide-slate-100">
            {events.map(([label, at]) => at && (
              <div key={label} className="flex flex-wrap justify-between gap-x-3 gap-y-1 py-2">
                <dt>{label}</dt><dd className="font-bold text-slate-900"><OrderTime value={at} full /></dd>
              </div>
            ))}
          </dl>
        </section>
      </div>
    </SupplierDialog>
  );
}

export function AdminOrdersTab() {
  const [searchInput, setSearchInput] = useState('');
  const [query, setQuery] = useState<OrdersQuery>(defaultQuery);
  const [orders, setOrders] = useState<TrackedOrder[]>([]);
  const [nextCursor, setNextCursor] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selectedOrder, setSelectedOrder] = useState<TrackedOrder>();

  useEffect(() => {
    const controller = new AbortController();
    const params = new URLSearchParams({ limit: '25' });
    if (query.search) params.set('search', query.search);
    if (query.status) params.set('status', query.status);
    if (query.cursor) params.set('cursor', query.cursor);
    apiRequest<OrdersPage>(`/admin/orders?${params}`, { auth: true, signal: controller.signal })
      .then(page => {
        if (controller.signal.aborted) return;
        if (!Array.isArray(page?.items)) throw new Error('Incomplete order page');
        setOrders(current => query.cursor ? [...current, ...page.items] : page.items);
        setNextCursor(page.nextCursor);
      })
      .catch(cause => {
        if (controller.signal.aborted) return;
        setError(cause instanceof ApiError && cause.status === 403 ? 'دسترسی شما به رهگیری سفارش‌ها برقرار نیست.' : 'دریافت سفارش‌ها انجام نشد.');
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [query]);

  const load = (nextQuery: OrdersQuery) => {
    setLoading(true);
    setError('');
    if (!nextQuery.cursor) {
      setOrders([]);
      setNextCursor(undefined);
    }
    setQuery({ ...nextQuery });
  };
  const submitSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    load({ search: searchInput.trim(), status: query.status });
  };
  const clearFilters = () => {
    setSearchInput('');
    load(defaultQuery);
  };
  const filtered = !!(query.status || query.search);

  return (
    <div className="space-y-3 sm:space-y-4">
      <form role="search" aria-label="جست‌وجوی سفارش‌ها" onSubmit={submitSearch} className={`${cardClass} grid items-end gap-3 md:grid-cols-[minmax(0,1fr)_minmax(10rem,0.45fr)]`}>
        <div className="min-w-0">
          <label htmlFor="admin-orders-search" className="mb-1.5 block text-sm font-bold text-slate-800">شماره سفارش یا کد رهگیری</label>
          <div className="flex gap-2">
            <input id="admin-orders-search" type="search" dir="auto" maxLength={255} value={searchInput}
              onChange={event => setSearchInput(event.target.value)} className={fieldClass} />
            <button type="submit" className="inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-xl bg-brand-deep px-3 text-sm font-bold text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand">
              <Search aria-hidden="true" className="size-4" /> جست‌وجو
            </button>
          </div>
        </div>
        <div className="min-w-0">
          <label htmlFor="admin-orders-status" className="mb-1.5 block text-sm font-bold text-slate-800">وضعیت سفارش</label>
          <select id="admin-orders-status" value={query.status} className={fieldClass}
            onChange={event => load({ search: query.search, status: event.target.value })}>
            {orderStatusOptions.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        </div>
      </form>

      {!loading && !error && orders.length === 0 && (
        <SupplierEmptyState icon={<Package />} title={filtered ? 'سفارشی با این فیلترها پیدا نشد.' : 'هنوز سفارشی ثبت نشده است.'}
          action={filtered && <button type="button" onClick={clearFilters} className={secondaryButtonClass}>پاک کردن فیلترها</button>} />
      )}

      {orders.length > 0 && (
        <ul aria-label="سفارش‌ها" className="grid gap-3 xl:grid-cols-2">
          {orders.map(order => {
            const destination = [...new Set([order.province, order.city].filter(Boolean))].join('، ');
            return (
              <li key={order.id} className={`${cardClass} space-y-3`}>
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <h2 className="min-w-0 text-sm font-extrabold text-slate-900">سفارش <TrackingNumber code={order.orderNumber} /></h2>
                  <OrderStatus status={order.status} />
                </div>
                <div className="space-y-1 text-sm text-slate-700">
                  <p className="break-words font-bold text-slate-800">{order.storeName}</p>
                  <p className="flex flex-wrap gap-x-3 gap-y-1">
                    {destination && <span>{destination}</span>}
                    <OrderTime value={order.createdAt} />
                  </p>
                </div>
                <button type="button" aria-label={`جزئیات رهگیری سفارش ${order.orderNumber}`} aria-haspopup="dialog"
                  onClick={() => setSelectedOrder(order)} className={`${secondaryButtonClass} w-full justify-between sm:w-auto`}>
                  <Truck aria-hidden="true" className="size-4" /> جزئیات رهگیری <ChevronLeft aria-hidden="true" className="size-4" />
                </button>
              </li>
            );
          })}
        </ul>
      )}

      <SupplierResourceState loading={loading} error={error} retry={() => load(query)} />
      {nextCursor && !loading && !error && (
        <button type="button" className={`${secondaryButtonClass} w-full sm:w-auto`}
          onClick={() => load({ ...query, cursor: nextCursor })}>سفارش‌های بیشتر</button>
      )}
      {selectedOrder && <OrderTrackingDetails order={selectedOrder} close={() => setSelectedOrder(undefined)} />}
    </div>
  );
}
