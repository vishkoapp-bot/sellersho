import { useCallback, useState, useEffect, type ReactNode } from 'react';
import { CustomerReturnBinEntry } from './CustomerReturnBinEntry';
import { WarehouseReturnReceipt } from './WarehouseReturnReceipt';
import { wmsApi } from '../../services/wmsApi';
import { RefreshCw, RotateCcw, Undo2 } from 'lucide-react';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

export { WarehouseReceiveTab } from './WarehouseReceiveTab';

type ReturnItem = Parameters<typeof WarehouseReturnReceipt>[0]['items'][number];
interface ReturnRow {
  id?: string;
  orderId?: string;
  order_id?: string;
  orderNumber?: string;
  status?: string;
  reason?: string;
  trackingNumber?: string;
  returnCode?: string;
  destinationName?: string;
  destinationCity?: string;
  supplierName?: string;
  supplier_name?: string;
  title?: string;
  items?: ReturnItem[];
  orderItems?: ReturnItem[];
  order_items?: ReturnItem[];
}

const statuses: Record<string, string> = {
  requested: 'درخواست‌شده', approved: 'آماده دریافت', in_transit: 'در مسیر انبار',
  received: 'دریافت‌شده', refunded: 'وجه مسترد شد', rejected: 'ردشده', cancelled: 'لغوشده',
};
const card = 'min-w-0 space-y-3 rounded-[22px] border border-brand-border bg-white p-4 shadow-sm sm:p-5';

function useList(fetch: () => Promise<ReturnRow[]>) {
  const [items, setItems] = useState<ReturnRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const reload = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try { setItems(await fetch()); }
    catch (cause) { setLoadError(cause instanceof Error ? cause.message : 'فهرست مرجوعی‌ها دریافت نشد.'); }
    finally { setLoading(false); }
  }, [fetch]);
  useEffect(() => { void reload(); }, [reload]);
  return { items, loading, loadError, reload };
}

function ListShell({ title, description, icon, loading, loadError, reload, empty, children }: {
  title: string; description: string; icon: ReactNode; loading: boolean; loadError: string; reload: () => Promise<void>; empty: boolean; children: ReactNode;
}) {
  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title={title} description={description} icon={icon}
        action={<button type="button" disabled={loading} onClick={() => void reload()}
          className="inline-flex min-h-11 items-center gap-2 rounded-xl border border-brand-border bg-white px-3 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">
          <RefreshCw aria-hidden="true" className={`size-4 ${loading ? 'motion-safe:animate-spin' : ''}`} />به‌روزرسانی
        </button>} />
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {!loading && !loadError && empty && <SupplierEmptyState icon={icon} title="درخواست مرجوعی در این فهرست نیست." />}
      {!loading && !loadError && !empty && children}
    </div>
  );
}

export function WarehouseCustomerReturnsTab() {
  const { items, ...state } = useList(wmsApi.getCustomerReturns);
  return (
    <ListShell title="مرجوعی مشتریان" description="دریافت و جانمایی اقلام برگشتی را ثبت کنید." icon={<RotateCcw />} empty={items.length === 0} {...state}>
      {items.map((item, i) => (
        <section key={item.id || i} className={card}>
          <div className="flex flex-wrap items-start justify-between gap-2">
            <h3 className="min-w-0 break-words text-base font-extrabold text-slate-900">سفارش <bdi className="font-mono">{item.orderNumber || item.orderId || item.order_id || '—'}</bdi></h3>
            {item.status && <span className="rounded-full bg-brand-soft px-3 py-1 text-xs font-bold text-brand-deep">{statuses[item.status] || item.status}</span>}
          </div>
          {item.destinationName && <p className="text-sm text-slate-700">مقصد {item.destinationName}{item.destinationCity && ` · ${item.destinationCity}`}</p>}
          {item.id ? (
            item.status === 'received' || item.status === 'refunded' ? <CustomerReturnBinEntry returnID={item.id} /> :
              <WarehouseReturnReceipt returnId={item.id} orderId={item.orderId ?? item.order_id} items={item.items ?? item.orderItems ?? item.order_items ?? []} />
          ) : <p role="alert" className="text-sm text-rose-800">شناسه مرجوعی در دسترس نیست.</p>}
          {(item.reason || item.trackingNumber || item.returnCode) && <details className="border-t border-brand-border">
            <summary className="min-h-11 cursor-pointer py-2.5 text-sm font-bold text-brand-deep">جزئیات مرجوعی</summary>
            <dl className="space-y-1 text-sm text-slate-700">
              {item.returnCode && <div><dt className="inline font-bold">کد مرجوعی </dt><dd className="inline"><bdi className="break-all font-mono">{item.returnCode}</bdi></dd></div>}
              {item.trackingNumber && <div><dt className="inline font-bold">کد رهگیری </dt><dd className="inline"><bdi className="break-all font-mono">{item.trackingNumber}</bdi></dd></div>}
              {item.reason && <div><dt className="inline font-bold">دلیل </dt><dd className="inline break-words">{item.reason}</dd></div>}
            </dl>
          </details>}
        </section>
      ))}
    </ListShell>
  );
}

export function WarehouseReturnBacksTab() {
  const { items, ...state } = useList(wmsApi.getReturnBacks);
  return (
    <ListShell title="پیگیری برگشت‌ها" description="وضعیت مرجوعی‌های در راه انبار یا دریافت‌شده را ببینید." icon={<Undo2 />} empty={items.length === 0} {...state}>
      {items.map((item, i) => (
        <section key={item.id || i} className={card}>
          <div className="flex flex-wrap items-start justify-between gap-2">
            <h3 className="min-w-0 break-words text-base font-extrabold text-slate-900">
              {item.supplierName || item.supplier_name || item.title || <>سفارش <bdi className="break-all font-mono">{item.orderNumber || item.orderId || item.order_id || item.id || '—'}</bdi></>}
            </h3>
            {item.status && <span className="rounded-full bg-brand-soft px-3 py-1 text-xs font-bold text-brand-deep">{statuses[item.status] || item.status}</span>}
          </div>
          {item.reason && <p className="break-words text-sm text-slate-700">{item.reason}</p>}
        </section>
      ))}
    </ListShell>
  );
}
