import { useState, type ReactNode } from 'react';
import { BarChart3, Boxes, Check, ChevronDown, ChevronLeft, CircleAlert, Clock, PackagePlus, ShoppingBag, Truck, Wallet } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { supplierApi, isSupplierProductActive } from '../../services/supplierApi';
import { getSupplierWorkCounts } from '../../services/panelWorkApi';
import { PanelWorkQueue } from '../common/PanelWorkQueue';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';

type Destination = 'ready' | 'overdue' | 'open' | 'awaiting-response' | 'prepare' | 'propose' | 'proposals';
type OverviewNavigation = {
  onNavigateTab: (tab: string, destination?: Destination) => void;
  sectionHref: (tab: string, destination?: Destination) => string;
};
type Products = ReturnType<typeof useSupplierResource<Awaited<ReturnType<typeof supplierApi.getProducts>>>>;
const summaryClass = 'flex min-h-11 cursor-pointer list-none items-center gap-2 rounded-lg font-bold text-slate-800 focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden';
const linkClass = 'inline-flex min-h-11 items-center gap-2 rounded-lg px-3 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:outline-2 focus-visible:outline-brand';

function OverviewLink({ tab, destination, children, className = linkClass, onNavigateTab, sectionHref }: OverviewNavigation & {
  tab: string;
  destination?: Destination;
  children: ReactNode;
  className?: string;
}) {
  return (
    <a href={sectionHref(tab, destination)} className={className} onClick={event => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      onNavigateTab(tab, destination);
    }}>{children}</a>
  );
}

function GettingStarted({ products, ...navigation }: OverviewNavigation & { products: Products }) {
  const { toPersianDigits } = useStore();
  const proposals = useSupplierResource(supplierApi.getProposals);
  const methods = useSupplierResource(supplierApi.getShippingMethods);
  const warehouseEnabled = methods.data?.some(method => method.method === 'warehouse' && method.status === 'enabled') ?? false;
  const shipments = useSupplierResource(warehouseEnabled ? supplierApi.getShipments : noShipments);
  const resources = warehouseEnabled ? [products, proposals, methods, shipments] : [products, proposals, methods];
  const loading = resources.some(resource => resource.loading);
  const error = resources.find(resource => resource.error)?.error ?? '';
  if (loading || error) {
    return <SupplierResourceState loading={loading} error={error} retry={() => {
      resources.filter(resource => resource.error).forEach(resource => { void resource.reload(); });
    }} />;
  }

  const hasActiveProduct = products.data?.some(isSupplierProductActive) ?? false;
  const hasProposal = (proposals.data?.length ?? 0) > 0;
  const awaitingProposal = proposals.data?.some(proposal => proposal.status === 'pending');
  const shippingReady = methods.data?.some(method => method.status === 'enabled') ?? false;
  const awaitingShipping = !shippingReady && methods.data?.some(method => method.status === 'requested');
  const hasSentShipment = shipments.data?.some(shipment => !['draft', 'created', 'cancelled'].includes(shipment.status)) ?? false;
  const steps: { title: string; detail: string; tab: string; destination?: Destination; action: string }[] = [];
  if (!hasActiveProduct) {
    if (hasProposal) {
      steps.push({ title: awaitingProposal ? 'در انتظار بررسی کالا' : 'نتیجهٔ بررسی کالا را ببین',
        detail: 'در فهرست پیشنهادها می‌توانی نتیجه و کار بعدی را ببینی.', tab: 'products', destination: 'proposals', action: 'پیگیری پیشنهاد کالا' });
    } else {
      steps.push({ title: 'اولین کالایت را پیشنهاد بده', detail: 'نام، تصویر، قیمت و مشخصات کالا را وارد کن.',
        tab: 'products', destination: 'propose', action: 'پیشنهاد کالا' });
    }
  }
  if (!shippingReady) {
    steps.push({ title: awaitingShipping ? 'در انتظار تأیید روش ارسال' : 'روش ارسال را فعال کن',
      detail: awaitingShipping ? 'وضعیت درخواستت را می‌توانی در روش‌های ارسال ببینی.' : 'ارسال مستقیم یا تحویل به انبار مرکزی را انتخاب کن.',
      tab: 'shipping-methods', action: awaitingShipping ? 'دیدن وضعیت درخواست' : 'انتخاب روش ارسال' });
  }
  if (hasActiveProduct && warehouseEnabled && !hasSentShipment) {
    steps.push({ title: 'اولین بار را به انبار بفرست', detail: 'اقلام محموله را انتخاب کن و برای کارتن برچسب بگیر.',
      tab: 'inbound', destination: 'prepare', action: 'آماده‌سازی محموله' });
  }
  const [next, ...remaining] = steps;
  if (!next) {
    return <p className="flex items-center gap-2 px-1 text-sm font-bold text-emerald-800"><Check aria-hidden="true" className="size-4 shrink-0" /> راه‌اندازی حساب کامل است</p>;
  }
  return (
    <section aria-labelledby="supplier-next-title" className="rounded-2xl border border-brand-border bg-brand-bg p-4 sm:p-5">
      <p className="text-xs font-bold text-brand-deep">قدم بعدی راه‌اندازی</p>
      <div className="mt-1 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h2 id="supplier-next-title" className="text-base font-extrabold text-slate-900">{next.title}</h2>
          <p className="mt-1 text-sm leading-6 text-slate-700">{next.detail}</p>
        </div>
        <OverviewLink {...navigation} {...next} className="inline-flex min-h-11 items-center gap-2 rounded-lg bg-brand-deep px-4 text-sm font-bold text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand">
          {next.action}<ChevronLeft aria-hidden="true" className="size-4 shrink-0" />
        </OverviewLink>
      </div>
      {remaining.length > 0 && (
        <details className="group mt-3 border-t border-brand-border">
          <summary className={summaryClass}><ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" /> {toPersianDigits(remaining.length)} کار دیگر برای راه‌اندازی</summary>
          <ul className="divide-y divide-brand-border">
            {remaining.map(step => (
              <li key={step.title} className="flex flex-wrap items-center justify-between gap-2 py-1">
                <span className="text-sm text-slate-700">{step.title}</span>
                <OverviewLink {...navigation} {...step}>{step.action}</OverviewLink>
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}

const noShipments = async (): Promise<Awaited<ReturnType<typeof supplierApi.getShipments>>> => [];

function OverviewNumbers({ products, ...navigation }: OverviewNavigation & { products: Products }) {
  const { formatPrice, toPersianDigits } = useStore();
  const report = useSupplierResource(supplierApi.getSalesReport);
  const balance = useSupplierResource(supplierApi.getSettlementBalance);
  const activeProducts = (products.data ?? []).filter(isSupplierProductActive);
  const stock = [...activeProducts].sort((a, b) => (a.stock ?? 0) - (b.stock ?? 0)).slice(0, 5);
  return (
    <div className="space-y-4 border-t border-slate-100 pt-3">
      <SupplierResourceState loading={products.loading || report.loading || balance.loading} error={products.error || report.error || balance.error} retry={() => {
        if (products.error) void products.reload();
        if (report.error) void report.reload();
        if (balance.error) void balance.reload();
      }} />
      <dl className="grid grid-cols-2 gap-x-3 gap-y-4 text-sm sm:grid-cols-3">
        {[
          ['کالاهای فعال', products.data ? toPersianDigits(activeProducts.length) : undefined],
          ['سفارش‌های پرداخت‌شده', report.data ? toPersianDigits(report.data.orders_count) : undefined],
          ['واحدهای فروخته‌شده', report.data ? toPersianDigits(report.data.items_sold) : undefined],
          ['فروش پرداخت‌شده (مبلغ پایه)', report.data ? `${formatPrice(report.data.total_revenue)} تومان` : undefined],
          ['موجودی قابل برداشت', balance.data ? `${formatPrice(balance.data.available)} تومان` : undefined],
        ].filter(([, value]) => value !== undefined).map(([label, value]) => (
          <div key={label} className="min-w-0 rounded-lg bg-slate-50 px-3 py-2">
            <dt className="text-xs leading-5 text-slate-600">{label}</dt>
            <dd className="break-words font-extrabold tabular-nums text-slate-900">{value}</dd>
          </div>
        ))}
      </dl>
      {stock.length > 0 && (
        <section aria-label="کالاها با کمترین موجودی ثبت‌شده">
          <h3 className="font-bold text-slate-800">کالاها با کمترین موجودی ثبت‌شده</h3>
          <ul className="mt-2 divide-y divide-slate-200">
            {stock.map(product => (
              <li key={product.id} className="flex items-start justify-between gap-3 py-2 text-sm">
                <span className="min-w-0 break-words text-slate-800">{product.title}</span>
                <span className={`shrink-0 tabular-nums ${product.stock ? 'text-slate-700' : 'font-bold text-rose-800'}`}>{toPersianDigits(product.stock ?? 0)} عدد</span>
              </li>
            ))}
          </ul>
          <OverviewLink {...navigation} tab="inventory">دیدن موجودی همهٔ کالاها <ChevronLeft aria-hidden="true" className="size-4" /></OverviewLink>
        </section>
      )}
    </div>
  );
}

export function SupplierOverviewTab(navigation: OverviewNavigation) {
  const products = useSupplierResource(supplierApi.getProducts);
  const counts = useSupplierResource(getSupplierWorkCounts);
  const [showNumbers, setShowNumbers] = useState(false);
  const workItems = [
    { icon: Clock, label: 'سفارش‌های دیرکرده', count: counts.data?.overdue, urgent: true, tab: 'orders', destination: 'overdue' },
    { icon: ShoppingBag, label: 'سفارش‌های منتظر ارسال', count: counts.data?.ready, tab: 'orders', destination: 'ready' },
    { icon: Truck, label: 'محموله‌های باز', count: counts.data?.openCartons, tab: 'inbound', destination: 'open' },
    { icon: CircleAlert, label: 'مغایرت‌های نیازمند پاسخ', count: counts.data?.discrepancies, tab: 'discrepancies', destination: 'awaiting-response' },
  ] as const;
  return (
    <div className="space-y-4">
      <PanelWorkQueue items={workItems.map(({ tab, destination, ...item }) => ({
        ...item, href: navigation.sectionHref(tab, destination), open: () => navigation.onNavigateTab(tab, destination),
      }))} loading={counts.loading} error={counts.error} retry={counts.reload} />
      <GettingStarted {...navigation} products={products} />

      <section aria-labelledby="supplier-quick-actions" className="rounded-2xl border border-slate-200 bg-white p-4 sm:p-5">
        <h2 id="supplier-quick-actions" className="text-base font-extrabold text-slate-900">کارهای پرکاربرد</h2>
        <ul className="mt-2 grid gap-1 sm:grid-cols-3">
          {[
            { tab: 'inbound', title: 'ثبت کالا و ارسال به انبار', icon: PackagePlus },
            { tab: 'inventory', title: 'بررسی موجودی کالاها', icon: Boxes },
            { tab: 'settlement', title: 'تسویه‌حساب', icon: Wallet },
          ].map(({ tab, title, icon: Icon }) => (
            <li key={tab} className="min-w-0">
              <OverviewLink {...navigation} tab={tab} className={`${linkClass} w-full justify-between border border-transparent hover:border-brand-border`}>
                <Icon aria-hidden="true" className="size-4 shrink-0" />
                <span className="min-w-0 flex-1">{title}</span>
                <ChevronLeft aria-hidden="true" className="size-4 shrink-0" />
              </OverviewLink>
            </li>
          ))}
        </ul>
      </section>

      <details className="group rounded-2xl border border-slate-200 bg-white px-4 py-1 sm:px-5" onToggle={event => {
        if (event.currentTarget.open) setShowNumbers(true);
      }}>
        <summary className={summaryClass}>
          <BarChart3 aria-hidden="true" className="size-5 shrink-0 text-brand-deep" />
          <span className="flex-1">خلاصهٔ فروش و موجودی</span>
          <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-open:rotate-180" />
        </summary>
        {showNumbers && <OverviewNumbers {...navigation} products={products} />}
      </details>
    </div>
  );
}
