import React from 'react';
import { useStore } from '../../context/StoreContext';
import { supplierApi, isSupplierProductActive } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { ArrowLeft, Boxes, Check, ChevronLeft, CircleAlert, Flag, Package, PackagePlus, RotateCcw, ShoppingBag, Truck, Wallet } from 'lucide-react';
import { isReadyOrder } from './SupplierOrdersTab';
import { canRespondToDiscrepancy } from './SupplierDiscrepanciesTab';

type Destination = 'ready' | 'track' | 'prepare' | 'propose' | 'proposals';
type Navigate = (tab: string, destination?: Destination) => void;
type Resource<T> = ReturnType<typeof useSupplierResource<T>>;

const GettingStarted: React.FC<{
  onNavigateTab: Navigate;
  products: Resource<Awaited<ReturnType<typeof supplierApi.getProducts>>>;
  shipments: Resource<{ status: string }[]>;
}> = ({ onNavigateTab, products, shipments }) => {
  const { toPersianDigits } = useStore();
  const proposals = useSupplierResource(supplierApi.getProposals);
  const methods = useSupplierResource(supplierApi.getShippingMethods);
  const warehouseEnabled = methods.data?.some(method => method.method === 'warehouse' && method.status === 'enabled');
  const resources = warehouseEnabled ? [products, proposals, methods, shipments] : [products, proposals, methods];
  const loading = resources.some(resource => resource.loading);
  const loadError = resources.find(resource => resource.error)?.error ?? '';
  if (loading || loadError) {
    return <SupplierResourceState loading={loading} error={loadError} retry={() => {
      resources.filter(resource => resource.error).forEach(resource => { void resource.reload(); });
    }} />;
  }

  const hasActiveProduct = products.data?.some(isSupplierProductActive) ?? false;
  const hasProposal = hasActiveProduct || (proposals.data?.length ?? 0) > 0;
  const shippingReady = methods.data?.some(method => method.status === 'enabled') ?? false;
  const awaitingShipping = !shippingReady && methods.data?.some(method => method.status === 'requested');
  const steps: { id: string; title: string; hint: string; done: boolean; tab: string; action: string; destination?: Destination }[] = [
    { id: 'proposal', title: 'اولین کالایت را پیشنهاد بده', hint: 'اطلاعات کالا را در فرم پیشنهاد وارد کن.', done: hasProposal, tab: 'products', destination: 'propose', action: 'پیشنهاد کالا' },
    { id: 'approved', title: 'تأیید اولین کالا', hint: 'نتیجهٔ بررسی مدیر در فهرست پیشنهادها نمایش داده می‌شود.', done: hasActiveProduct, tab: 'products', destination: 'proposals', action: 'پیگیری پیشنهادها' },
    { id: 'shipping', title: awaitingShipping ? 'در انتظار تأیید روش ارسال' : 'فعال‌سازی روش ارسال', hint: awaitingShipping ? 'درخواستت ثبت شده و در حال بررسی است.' : 'برای ارسال مستقیم یا تحویل به انبار مرکزی، درخواست فعال‌سازی بده.', done: shippingReady, tab: 'shipping-methods', action: awaitingShipping ? 'دیدن وضعیت درخواست' : 'انتخاب روش ارسال' },
  ];
  if (warehouseEnabled) {
    steps.push({ id: 'shipment', title: 'اولین بار را به انبار بفرست', hint: 'کالاهای محموله را انتخاب کن و برای کارتن کد بگیر.', done: shipments.data?.some(shipment => !['draft', 'created', 'cancelled'].includes(shipment.status)) ?? false, tab: 'inbound', destination: 'prepare', action: 'آماده‌سازی محموله' });
  }
  const doneCount = steps.filter((s) => s.done).length;
  if (doneCount === steps.length) {
    return (
      <div className="rounded-[22px] border border-emerald-200 bg-emerald-50 p-4 flex flex-wrap items-center gap-2.5">
        <span className="w-9 h-9 rounded-xl bg-emerald-500 text-white flex items-center justify-center shrink-0"><Check className="w-4.5 h-4.5" strokeWidth={1.5} /></span>
        <div className="flex-1 min-w-0">
          <p className="text-xs font-extrabold text-emerald-800">همه‌چیز آماده است</p>
          <p className="text-[11px] text-emerald-700 mt-0.5">همه قدم‌های شروع کار کامل شده. سفارش‌ها را از تب «سفارش‌های من» دنبال کن.</p>
        </div>
        <button type="button" onClick={() => onNavigateTab('orders')} className="shrink-0 min-h-[44px] px-3 rounded-xl bg-emerald-600 text-white text-xs font-bold">سفارش‌های من</button>
      </div>
    );
  }
  return (
    <div className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm space-y-3">
      <div className="flex items-center gap-2">
        <span className="w-9 h-9 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center shrink-0"><Flag className="w-4.5 h-4.5" strokeWidth={1.5} /></span>
        <div>
          <h3 className="text-xs font-extrabold text-slate-800">شروع کار: این قدم‌ها را کامل کن</h3>
          <p className="text-[11px] text-slate-500 mt-0.5">{toPersianDigits(doneCount)} از {toPersianDigits(steps.length)} قدم انجام شده — هر قدم را با دکمه‌اش انجام بده.</p>
        </div>
      </div>
      <div className="h-2 rounded-full bg-slate-100 overflow-hidden" aria-hidden="true">
        <div className="h-full rounded-full bg-brand-deep transition-all" style={{ width: `${Math.round((doneCount / steps.length) * 100)}%` }} />
      </div>
      <div className="space-y-2">
        {steps.map((s, idx) => (
          <div key={s.id} className={`flex flex-wrap items-center gap-2.5 rounded-xl border p-2.5 ${s.done ? 'border-emerald-100 bg-emerald-50/50' : 'border-slate-200 bg-white'}`}>
            <span className={`w-7 h-7 rounded-full flex items-center justify-center shrink-0 text-[11px] font-extrabold ${s.done ? 'bg-emerald-500 text-white' : 'bg-slate-100 text-slate-500'}`}>{s.done ? <Check className="w-4 h-4" strokeWidth={2} /> : toPersianDigits(idx + 1)}</span>
            <div className="flex-1 min-w-0">
              <p className={`text-xs font-bold ${s.done ? 'text-slate-500 line-through decoration-emerald-300' : 'text-slate-800'}`}>{s.title}</p>
              {!s.done && <p className="text-[11px] leading-5 text-slate-500 mt-0.5">{s.hint}</p>}
            </div>
            {!s.done && (s.id !== 'approved' || hasProposal) && (
              <button type="button" onClick={() => onNavigateTab(s.tab, s.destination)} className="shrink-0 flex items-center gap-1 min-h-11 px-3 rounded-xl bg-brand-soft text-brand-deep text-xs font-bold hover:bg-brand-border focus-visible:ring-2 focus-visible:ring-brand"><span>{s.action}</span><ChevronLeft aria-hidden="true" className="size-3.5" strokeWidth={1.5} /></button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
};

export const SupplierOverviewTab: React.FC<{ onNavigateTab: Navigate }> = ({ onNavigateTab }) => {
  const { formatPrice, toPersianDigits } = useStore();
  const products = useSupplierResource(supplierApi.getProducts);
  const report = useSupplierResource(supplierApi.getSalesReport);
  const loading = products.loading || report.loading;
  const error = products.error || report.error;
  const shipments = useSupplierResource(supplierApi.getShipments);
  const orders = useSupplierResource(supplierApi.getOrders);
  const discrepancies = useSupplierResource(supplierApi.getDiscrepancies);
  const balance = useSupplierResource(supplierApi.getSettlementBalance);
  const readyOrders = orders.data?.orders.filter(order => isReadyOrder(order.status)).length;
  const openShipments = shipments.data?.filter(shipment => ['draft', 'created', 'sent', 'in_transit', 'partially_received'].includes(shipment.status)).length;
  const needsResponse = discrepancies.data?.filter(item => !item.responded_at && canRespondToDiscrepancy(item)).length;
  const workItems = [
    { icon: ShoppingBag, label: orders.data?.hasMore ? 'منتظر ارسال در سفارش‌های اخیر' : 'سفارش‌های منتظر ارسال', count: readyOrders, action: 'رسیدگی به سفارش‌ها', open: () => onNavigateTab('orders', 'ready') },
    { icon: Truck, label: 'محموله‌های باز', count: openShipments, action: 'پیگیری محموله‌ها', open: () => onNavigateTab('inbound', 'track') },
    { icon: CircleAlert, label: 'مغایرت‌های نیازمند پاسخ', count: needsResponse, action: 'پاسخ به مغایرت‌ها', open: () => onNavigateTab('discrepancies') },
  ];
  const workError = orders.error || shipments.error || discrepancies.error;
  const workLoading = orders.loading || shipments.loading || discrepancies.loading;
  const totalRevenue = report.data?.total_revenue || 0;
  const ordersCount = report.data?.orders_count || 0;
  const lowStock = (products.data ?? []).filter(isSupplierProductActive).sort((x, y) => (x.stock ?? 0) - (y.stock ?? 0)).slice(0, 5);
  return (
    <div className="space-y-4 sm:space-y-5">
      <section className="relative isolate overflow-hidden rounded-[26px] bg-[#2c2050] px-5 py-6 text-white shadow-lg shadow-[#2c2050]/15 sm:px-8 sm:py-8">
        <div aria-hidden="true" className="pointer-events-none absolute inset-0 -z-10 bg-[radial-gradient(ellipse_at_0%_0%,#57408d_0%,transparent_66%)]" />
        <div className="flex max-w-3xl flex-col items-start">
          <h2 className="text-2xl font-black leading-tight tracking-tight sm:text-3xl">کالا، محموله و سفارش‌هایت</h2>
          <p className="mt-2 max-w-md text-sm leading-7 text-[#e8e0fb]">کالای تازه ثبت کن، بار را به انبار بفرست یا سفارش‌های آمادهٔ ارسال را پیگیری کن.</p>
          <div className="mt-5 flex w-full flex-wrap gap-2.5">
            <button type="button" onClick={() => onNavigateTab('inbound')} className="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-[#ede5ff] px-4 text-sm font-extrabold text-[#2c2050] hover:bg-white focus-visible:ring-2 focus-visible:ring-white">
              <PackagePlus aria-hidden="true" className="size-4" /> ثبت کالا و ارسال به انبار
            </button>
            <button type="button" onClick={() => onNavigateTab('orders')} className="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-white/40 px-4 text-sm font-bold hover:bg-white/10 focus-visible:ring-2 focus-visible:ring-white">
              <ShoppingBag aria-hidden="true" className="size-4" /> سفارش‌های من
            </button>
          </div>
        </div>
      </section>

      <SupplierResourceState loading={loading} error={error} retry={() => { void products.reload(); void report.reload(); }} />
      {!loading && !error && (
        <dl className="grid grid-cols-2 gap-2.5 xl:grid-cols-4">
          {[
            { label: 'کالاهای فعال', value: toPersianDigits((products.data || []).filter(isSupplierProductActive).length), Icon: Package },
            { label: 'فروش پرداخت‌شده (مبلغ پایه)', value: formatPrice(totalRevenue), unit: 'تومان', Icon: Wallet },
            { label: 'واحد فروخته‌شده (پرداخت‌شده)', value: toPersianDigits(report.data?.items_sold || 0), Icon: Boxes },
            { label: 'سفارش پرداخت‌شده', value: toPersianDigits(ordersCount), Icon: ShoppingBag },
          ].map(({ label, value, unit, Icon }) => (
            <div key={label} className="min-w-0 rounded-2xl border border-[#e6e2f0] bg-white p-3 shadow-sm sm:p-4">
              <Icon aria-hidden="true" className="mb-2 size-5 text-[#5a428b]" strokeWidth={1.75} />
              <dt className="min-h-9 text-xs font-medium leading-5 text-slate-600 sm:min-h-0">{label}</dt>
              <dd className="mt-1 break-words text-lg font-black tabular-nums text-[#261b41] sm:text-xl">
                {value}{unit && <span className="ms-1 text-xs font-semibold text-slate-600">{unit}</span>}
              </dd>
            </div>
          ))}
        </dl>
      )}

      <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(0,1fr)]">
        <section className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
          <h3 className="mb-2 text-base font-extrabold text-[#261b41]">کارهای پیگیری</h3>
          <SupplierResourceState loading={workLoading} error={workError} retry={() => { void orders.reload(); void shipments.reload(); void discrepancies.reload(); }} />
          <ul className="divide-y divide-[#eeebf4]">
            {workItems.map(({ icon: Icon, label, count, action, open }) => (
              <li key={action} className="flex flex-wrap items-center justify-between gap-x-3 py-2">
                <span className="flex min-w-0 items-center gap-2 text-xs font-semibold leading-6 text-slate-700 sm:text-sm">
                  <Icon aria-hidden="true" className="size-4 shrink-0 text-[#5a428b]" />
                  <span>{label}</span>
                  {count !== undefined && <strong className={`grid min-h-6 min-w-6 shrink-0 place-items-center rounded-md px-1 tabular-nums ${count ? 'bg-[#eee7ff] text-[#3b2767]' : 'bg-slate-100 text-slate-600'}`}>{toPersianDigits(count)}</strong>}
                </span>
                <button type="button" onClick={open} className="ms-auto inline-flex min-h-11 items-center gap-1 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">{action}<ArrowLeft aria-hidden="true" className="size-3.5" /></button>
              </li>
            ))}
            <li className="flex flex-wrap items-center justify-between gap-x-3 py-2">
              <span className="flex min-w-0 flex-wrap items-center gap-2 text-xs font-semibold leading-6 text-slate-700 sm:text-sm"><Wallet aria-hidden="true" className="size-4 text-[#5a428b]" /> موجودی قابل برداشت{balance.data && <strong className="tabular-nums text-[#261b41]">{formatPrice(balance.data.available)} تومان</strong>}</span>
              <button type="button" onClick={() => onNavigateTab('settlement')} className="ms-auto min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">برداشت و سوابق</button>
            </li>
            <li className="flex flex-wrap items-center justify-between gap-x-3 pt-2">
              <span className="flex items-center gap-2 text-xs font-semibold leading-6 text-slate-700 sm:text-sm"><RotateCcw aria-hidden="true" className="size-4 text-[#5a428b]" /> مرجوعی‌های مشتریان</span>
              <button type="button" onClick={() => onNavigateTab('customer-returns')} className="ms-auto min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">پیگیری مرجوعی‌ها</button>
            </li>
          </ul>
        </section>

        <div className="min-w-0 space-y-4">
          <GettingStarted onNavigateTab={onNavigateTab} products={products} shipments={shipments} />
          {!loading && !error && (
            <section className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm">
              <h3 className="mb-2 text-sm font-extrabold text-[#261b41]">کم‌موجودی‌ترین کالاها</h3>
              {lowStock.length === 0 ? <p className="text-xs text-slate-600">کالایی ثبت نشده است.</p> : (
                <div className="space-y-1.5">
                  {lowStock.map(product => (
                    <div key={product.id} className="flex items-center justify-between gap-3 rounded-xl bg-[#f6f5fa] px-3 py-2 text-xs">
                      <span className="min-w-0 break-words font-bold text-slate-800">{product.title}</span>
                      <span className={`shrink-0 tabular-nums ${(product.stock ?? 0) === 0 ? 'font-bold text-rose-700' : 'text-slate-700'}`}>{toPersianDigits(product.stock ?? 0)} عدد</span>
                    </div>
                  ))}
                  <button type="button" onClick={() => onNavigateTab('products')} className="min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">دیدن همهٔ کالاها</button>
                </div>
              )}
            </section>
          )}
        </div>
      </div>
    </div>
  );
};
