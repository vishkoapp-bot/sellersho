import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { supplierApi, isSupplierProductActive } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { Check, ChevronLeft, Flag } from 'lucide-react';

// FIX 35: supplier overview rebuilt to the live overview spec
// (component Cl plus getting-started checklist Sl in the live
// supplier bundle): header, four paid-only stat cards, unpaid note,
// nav buttons, five lowest-stock products, pending and settlement
// rows. The contract checklist step is omitted: live omits it when
// the signed version is unknown, and the candidate supplier session
// carries no signed contract version. The shipment step counts
// inbounds only: live also adds warehouse shipments, which are not
// visible in supplier scope here.

type PendingData = {
  inbounds: any[] | null;
  orders: any | null;
  returns: any[] | null;
  discrepancies: any[] | null;
  balance: any | null;
  history: any[] | null;
};

const GettingStarted: React.FC<{ onNavigateTab: (tab: string) => void }> = ({ onNavigateTab }) => {
  const { toPersianDigits } = useStore();
  const [proposalCount, setProposalCount] = useState<number | null>(null);
  const [productCount, setProductCount] = useState<number | null>(null);
  const [shippingReady, setShippingReady] = useState<boolean | null>(null);
  const [shipmentCount, setShipmentCount] = useState<number | null>(null);
  useEffect(() => {
    let cancelled = false;
    supplierApi.getProposals().then((d) => { if (!cancelled) setProposalCount((d || []).length); }).catch(() => {});
    supplierApi.getProducts().then((d) => { if (!cancelled) setProductCount((d || []).filter(isSupplierProductActive).length); }).catch(() => {});
    supplierApi.getShippingMethods().then((d: any) => { if (!cancelled) setShippingReady(((d.methods || d) || []).some((m: any) => m.status === 'enabled' || m.status === 'requested')); }).catch(() => {});
    supplierApi.getInbounds().then((d) => { if (!cancelled) setShipmentCount((d || []).length); }).catch(() => {});
    return () => { cancelled = true; };
  }, []);
  const steps: { id: string; title: string; hint: string; done: boolean; tab?: string; action?: string }[] = [];
  if (proposalCount !== null) steps.push({ id: 'proposal', title: 'اولین کالایت را پیشنهاد بده', hint: 'از تب «کالاها و تنوع‌ها» کالایت را بفرست تا مدیر بررسی کند.', done: proposalCount > 0, tab: 'products', action: 'پیشنهاد کالا' });
  if (productCount !== null) steps.push({ id: 'approved', title: 'کالایت تأیید شود و به فروش برسد', hint: 'بعد از تأیید مدیر، کالایت در فروشگاه‌ها نمایش داده می‌شود.', done: productCount > 0, tab: 'products', action: 'دیدن کالاها' });
  if (shippingReady !== null) steps.push({ id: 'shipping', title: 'روش فرستادن کالا را انتخاب کن', hint: 'مشخص کن سفارش‌ها را خودت می‌فرستی یا کالا را به انبار مرکزی می‌دهی.', done: shippingReady, tab: 'shipping-methods', action: 'انتخاب روش ارسال' });
  if (shipmentCount !== null) steps.push({ id: 'shipment', title: 'اولین بار را به انبار بفرست', hint: 'وقتی بار را می‌فرستی، اول همین‌جا ثبتش کن تا انبار آماده تحویل باشد.', done: shipmentCount > 0, tab: 'inbound', action: 'فرستادن بار' });
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

export const SupplierOverviewTab: React.FC<{ onNavigateTab: (tab: string) => void }> = ({ onNavigateTab }) => {
  const { formatPrice, toPersianDigits } = useStore();
  const products = useSupplierResource(supplierApi.getProducts);
  const report = useSupplierResource(supplierApi.getSalesReport);
  const loading = products.loading || report.loading;
  const error = products.error || report.error;
  const [pending, setPending] = useState<PendingData | null>(null);
  useEffect(() => {
    let cancelled = false;
    Promise.allSettled([supplierApi.getInbounds(), supplierApi.getOrders(), supplierApi.getCustomerReturns(), supplierApi.getDiscrepancies(), supplierApi.getSettlementBalance(), supplierApi.getSettlementHistory()]).then(([a, b, c, d, e, f]) => {
      if (cancelled) return;
      setPending({
        inbounds: a.status === 'fulfilled' ? (a.value as any) : null,
        orders: b.status === 'fulfilled' ? (b.value as any) : null,
        returns: c.status === 'fulfilled' ? (c.value as any) : null,
        discrepancies: d.status === 'fulfilled' ? (d.value as any) : null,
        balance: e.status === 'fulfilled' ? (e.value as any) : null,
        history: f.status === 'fulfilled' ? (f.value as any) : null,
      });
    });
    return () => { cancelled = true; };
  }, []);
  const totalRevenue = report.data?.total_revenue || 0;
  const ordersCount = report.data?.orders_count || 0;
  const lowStock = [...(products.data || [])].sort((x, y) => (x.stock ?? 0) - (y.stock ?? 0)).slice(0, 5);
  const hasPending = !!pending && (pending.inbounds !== null || pending.orders !== null || pending.returns !== null || pending.discrepancies !== null || pending.balance !== null || pending.history !== null);
  return (
    <div className="space-y-3 pb-6">
      <section className="rounded-2xl bg-slate-900 p-4 text-white">
        <h2 className="text-sm font-bold">نمای کلی کار تو</h2>
        <p className="mt-1 text-xs text-slate-300">این‌ها عددهای واقعی کالاها و فروش توست؛ از خود سامانه خوانده می‌شود.</p>
      </section>
      <GettingStarted onNavigateTab={onNavigateTab} />
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
        <div className="grid grid-cols-2 gap-2">
          <button type="button" onClick={() => onNavigateTab('products')} className="rounded-xl bg-brand-deep p-3 min-h-[44px] text-xs font-bold text-white">کالاهای من</button>
          <button type="button" onClick={() => onNavigateTab('orders')} className="rounded-xl bg-slate-100 p-3 min-h-[44px] text-xs font-bold">سفارش‌های من</button>
        </div>
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
        {hasPending && pending && (
          <section className="bg-white rounded-2xl border border-slate-100 p-4">
            <h3 className="text-xs font-extrabold text-slate-800 mb-3">کارهای در انتظار و تسویه</h3>
            <div className="space-y-1.5">
              {pending.inbounds !== null && (
                <div className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                  <span className="font-bold text-slate-800">بارهای در انتظار تحویل</span>
                  <span className="flex items-center gap-2"><span className="font-bold text-slate-500">{toPersianDigits(pending.inbounds.filter((x: any) => ['pending', 'scheduled', 'in_transit', 'receiving'].includes(x.status)).length)} مورد</span><button type="button" onClick={() => onNavigateTab('inbound')} className="font-bold text-brand-deep">دیدن</button></span>
                </div>
              )}
              {pending.orders !== null && (
                <div className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                  <span className="font-bold text-slate-800">سفارش‌های منتظر ارسال</span>
                  <span className="flex items-center gap-2"><span className="font-bold text-slate-500">{toPersianDigits((pending.orders.orders || []).filter((x: any) => x.status !== 'shipped' && x.status !== 'in_transit' && x.status !== 'delivered').length)} مورد</span><button type="button" onClick={() => onNavigateTab('orders')} className="font-bold text-brand-deep">دیدن</button></span>
                </div>
              )}
              {pending.returns !== null && (
                <div className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                  <span className="font-bold text-slate-800">مرجوعی‌های مشتریان</span>
                  <span className="flex items-center gap-2"><span className="font-bold text-slate-500">{toPersianDigits(pending.returns.length)} مورد</span><button type="button" onClick={() => onNavigateTab('customer-returns')} className="font-bold text-brand-deep">دیدن</button></span>
                </div>
              )}
              {pending.discrepancies !== null && (
                <div className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                  <span className="font-bold text-slate-800">مغایرت‌های باز</span>
                  <span className="flex items-center gap-2"><span className="font-bold text-slate-500">{toPersianDigits(pending.discrepancies.filter((x: any) => x.status === 'open').length)} مورد</span><button type="button" onClick={() => onNavigateTab('discrepancies')} className="font-bold text-brand-deep">دیدن</button></span>
                </div>
              )}
              {pending.balance !== null && (
                <div className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                  <span className="font-bold text-slate-800">موجودی قابل برداشت</span>
                  <span className="flex items-center gap-2"><span className="font-bold text-slate-500">{formatPrice(pending.balance.available ?? 0)} تومان</span><button type="button" onClick={() => onNavigateTab('settlement')} className="font-bold text-brand-deep">دیدن</button></span>
                </div>
              )}
              {pending.history !== null && (
                <div className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                  <span className="font-bold text-slate-800">برداشت‌های قبلی</span>
                  <span className="flex items-center gap-2"><span className="font-bold text-slate-500">{toPersianDigits(pending.history.length)} مورد</span><button type="button" onClick={() => onNavigateTab('settlement')} className="font-bold text-brand-deep">دیدن</button></span>
                </div>
              )}
            </div>
          </section>
        )}
      </>)}
    </div>
  );
};
