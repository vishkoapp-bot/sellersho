import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { supplierApi, isSupplierProductActive } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { Check, ChevronLeft, Flag, PackagePlus, ShoppingBag } from 'lucide-react';
import { isReadyOrder } from './SupplierOrdersTab';
import { canRespondToDiscrepancy } from './SupplierDiscrepanciesTab';

type Navigate = (tab: string, destination?: 'ready' | 'track') => void;

const GettingStarted: React.FC<{ onNavigateTab: Navigate; shipmentDone: boolean | null }> = ({ onNavigateTab, shipmentDone }) => {
  const { toPersianDigits } = useStore();
  const [proposalCount, setProposalCount] = useState<number | null>(null);
  const [productCount, setProductCount] = useState<number | null>(null);
  const [shippingReady, setShippingReady] = useState<boolean | null>(null);
  useEffect(() => {
    let cancelled = false;
    supplierApi.getProposals().then((d) => { if (!cancelled) setProposalCount((d || []).length); }).catch(() => {});
    supplierApi.getProducts().then((d) => { if (!cancelled) setProductCount((d || []).filter(isSupplierProductActive).length); }).catch(() => {});
    supplierApi.getShippingMethods().then((d: any) => { if (!cancelled) setShippingReady(((d.methods || d) || []).some((m: any) => m.status === 'enabled' || m.status === 'requested')); }).catch(() => {});
    return () => { cancelled = true; };
  }, []);
  const steps: { id: string; title: string; hint: string; done: boolean; tab?: string; action?: string }[] = [];
  if (proposalCount !== null) steps.push({ id: 'proposal', title: 'اولین کالایت را پیشنهاد بده', hint: 'اطلاعات کالا را در مسیر ثبت کالا وارد کن تا بررسی شود.', done: proposalCount > 0 || (productCount ?? 0) > 0, tab: 'inbound', action: 'پیشنهاد کالا' });
  if (productCount !== null) steps.push({ id: 'approved', title: 'کالایت تأیید شود و به فروش برسد', hint: 'بعد از تأیید مدیر، کالایت در فروشگاه‌ها نمایش داده می‌شود.', done: productCount > 0, tab: 'products', action: 'دیدن کالاها' });
  if (shippingReady !== null) steps.push({ id: 'shipping', title: 'روش فرستادن کالا را انتخاب کن', hint: 'مشخص کن سفارش‌ها را خودت می‌فرستی یا کالا را به انبار مرکزی می‌دهی.', done: shippingReady, tab: 'shipping-methods', action: 'انتخاب روش ارسال' });
  if (shipmentDone !== null) steps.push({ id: 'shipment', title: 'اولین بار را به انبار بفرست', hint: 'وقتی بار را می‌فرستی، اول همین‌جا ثبتش کن تا انبار آماده تحویل باشد.', done: shipmentDone, tab: 'inbound', action: 'فرستادن بار' });
  if (steps.length === 0) return null;
  const doneCount = steps.filter((s) => s.done).length;
  if (doneCount === steps.length) {
    return (
      <div className="rounded-2xl border border-emerald-200 bg-emerald-50 px-3.5 py-3 flex items-center gap-2.5">
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
    <div className="rounded-2xl border border-brand-border bg-white p-3.5 shadow-2xs space-y-3">
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
          <div key={s.id} className={`flex items-center gap-2.5 rounded-xl border p-2.5 ${s.done ? 'border-emerald-100 bg-emerald-50/50' : 'border-slate-200 bg-white'}`}>
            <span className={`w-7 h-7 rounded-full flex items-center justify-center shrink-0 text-[11px] font-extrabold ${s.done ? 'bg-emerald-500 text-white' : 'bg-slate-100 text-slate-500'}`}>{s.done ? <Check className="w-4 h-4" strokeWidth={2} /> : toPersianDigits(idx + 1)}</span>
            <div className="flex-1 min-w-0">
              <p className={`text-xs font-bold ${s.done ? 'text-slate-500 line-through decoration-emerald-300' : 'text-slate-800'}`}>{s.title}</p>
              {!s.done && <p className="text-[11px] leading-5 text-slate-500 mt-0.5">{s.hint}</p>}
            </div>
            {!s.done && s.tab && (
              <button type="button" onClick={() => onNavigateTab(s.tab as string)} className="shrink-0 flex items-center gap-0.5 min-h-[44px] px-3 rounded-xl bg-brand-deep text-white text-xs font-bold"><span>{s.action}</span><ChevronLeft className="w-3.5 h-3.5" strokeWidth={1.5} /></button>
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
    { label: orders.data?.hasMore ? 'منتظر ارسال در سفارش‌های اخیر' : 'سفارش‌های منتظر ارسال', count: readyOrders, action: 'رسیدگی به سفارش‌ها', open: () => onNavigateTab('orders', 'ready') },
    { label: 'محموله‌های باز', count: openShipments, action: 'پیگیری محموله‌ها', open: () => onNavigateTab('inbound', 'track') },
    { label: 'مغایرت‌های نیازمند پاسخ', count: needsResponse, action: 'پاسخ به مغایرت‌ها', open: () => onNavigateTab('discrepancies') },
  ];
  const workError = orders.error || shipments.error || discrepancies.error;
  const workLoading = orders.loading || shipments.loading || discrepancies.loading;
  const totalRevenue = report.data?.total_revenue || 0;
  const ordersCount = report.data?.orders_count || 0;
  const lowStock = [...(products.data || [])].sort((x, y) => (x.stock ?? 0) - (y.stock ?? 0)).slice(0, 5);
  return (
    <div className="space-y-3 pb-6">
      <section className="rounded-2xl bg-slate-900 p-4 text-white sm:p-5">
        <h2 className="text-base font-bold">از کجا شروع کنم؟</h2>
        <div className="mt-3 flex flex-wrap gap-2">
          <button type="button" onClick={() => onNavigateTab('inbound')} className="inline-flex min-h-11 items-center gap-2 rounded-xl bg-white px-3 text-sm font-bold text-slate-900 hover:bg-slate-100">
            <PackagePlus aria-hidden="true" className="size-4" /> ثبت کالا و ارسال به انبار
          </button>
          <button type="button" onClick={() => onNavigateTab('orders')} className="inline-flex min-h-11 items-center gap-2 rounded-xl border border-white/30 px-3 text-sm font-bold hover:bg-white/10">
            <ShoppingBag aria-hidden="true" className="size-4" /> سفارش‌های من
          </button>
        </div>
      </section>
      <section className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4">
        <h3 className="text-sm font-bold text-slate-900">کارهای پیگیری</h3>
        <SupplierResourceState loading={workLoading} error={workError} retry={() => { void orders.reload(); void shipments.reload(); void discrepancies.reload(); }} />
        <ul className="divide-y divide-slate-100">
          {workItems.map(({ label, count, action, open }) => (
            <li key={action} className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-1.5">
              <span className="text-sm text-slate-700">{label}{count !== undefined && <strong className="ms-2 tabular-nums text-slate-900">{toPersianDigits(count)}</strong>}</span>
              <button type="button" onClick={open} className="min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft">{action}</button>
            </li>
          ))}
          <li className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-1.5">
            <span className="text-sm text-slate-700">موجودی قابل برداشت{balance.data && <strong className="ms-2 tabular-nums text-slate-900">{formatPrice(balance.data.available)} تومان</strong>}</span>
            <button type="button" onClick={() => onNavigateTab('settlement')} className="min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft">برداشت و سوابق</button>
          </li>
          <li className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 py-1.5">
            <span className="text-sm text-slate-700">مرجوعی‌های مشتریان</span>
            <button type="button" onClick={() => onNavigateTab('customer-returns')} className="min-h-11 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft">پیگیری مرجوعی‌ها</button>
          </li>
        </ul>
      </section>
      <GettingStarted onNavigateTab={onNavigateTab} shipmentDone={shipments.data ? shipments.data.some(shipment => !['draft', 'cancelled'].includes(shipment.status)) : null} />
      <SupplierResourceState loading={loading} error={error} retry={() => { void products.reload(); void report.reload(); }} />
      {!loading && !error && (<>
        <div className="grid grid-cols-2 gap-2">
          {[
            ['کالاهای فعال', toPersianDigits((products.data || []).filter(isSupplierProductActive).length)],
            ['فروش پرداخت‌شده (مبلغ پایه)', `${formatPrice(totalRevenue)} تومان`],
            ['واحد فروخته‌شده (پرداخت‌شده)', toPersianDigits(report.data?.items_sold || 0)],
            ['سفارش پرداخت‌شده', toPersianDigits(ordersCount)],
          ].map(([label, value]) => (
            <div key={label} className="rounded-xl border bg-white p-3"><span className="block text-[10px] text-slate-500">{label}</span><strong className="mt-1 block text-sm">{value}</strong></div>
          ))}
        </div>
        {totalRevenue === 0 && ordersCount === 0 && (<p className="text-[11px] leading-5 text-slate-500 bg-white rounded-2xl border border-slate-100 p-3">هنوز فروش پرداخت‌شده‌ای ثبت نشده است؛ سفارش‌های در انتظار پرداخت در این عددها نیستند.</p>)}
        <section className="bg-white rounded-2xl border border-slate-100 p-4">
          <h3 className="text-xs font-extrabold text-slate-800 mb-3">کم‌موجودی‌ترین کالاها</h3>
          {(products.data || []).length === 0 ? (<p className="text-[11px] text-slate-400">کالایی ثبت نشده است.</p>) : (
            <div className="space-y-1.5">
              {lowStock.map((x) => (
                <div key={x.id} className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                  <span className="font-bold text-slate-800">{x.title}</span>
                  <span className={(x.stock ?? 0) === 0 ? 'font-bold text-rose-600' : 'font-bold text-slate-500'}>موجودی: {toPersianDigits(x.stock ?? 0)} عدد</span>
                </div>
              ))}
              <button type="button" onClick={() => onNavigateTab('products')} className="text-[11px] font-bold text-brand-deep">دیدن همهٔ کالاها</button>
            </div>
          )}
        </section>
      </>)}
    </div>
  );
};
