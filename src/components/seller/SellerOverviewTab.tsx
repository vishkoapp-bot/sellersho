import { useEffect, useState, type ReactNode } from 'react';
import { Award, BarChart3, Check, ChevronDown, ChevronLeft, DollarSign, ExternalLink, Package, ShoppingBag } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { storeApi, type SellerReportResponse } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { getSellerWorkCounts } from '../../services/panelWorkApi';
import { countSellerVariantAttention } from '../../services/variantWorkspaceApi';
import type { SellerDashboardTab } from '../../utils/routing';
import { PanelWorkQueue } from '../common/PanelWorkQueue';
import { sellerSectionHref, type SellerWorkFilter } from './SellerNavigation';

interface Props {
  onNavigateTab: (tab: string, work?: SellerWorkFilter) => void;
  onOpenStorefront: () => void;
}

const summaryClass = 'flex min-h-11 cursor-pointer list-none items-center gap-2 rounded-lg font-bold text-slate-800 focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden';
const linkClass = 'inline-flex min-h-11 items-center gap-2 rounded-lg px-3 text-sm font-bold text-brand-deep hover:bg-brand-bg focus-visible:outline-2 focus-visible:outline-brand';

function SellerShortcut({ tab, onNavigateTab, children, className = linkClass }: {
  tab: SellerDashboardTab;
  onNavigateTab: Props['onNavigateTab'];
  children: ReactNode;
  className?: string;
}) {
  return (
    <a href={sellerSectionHref(tab)} className={className} onClick={event => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      onNavigateTab(tab);
    }}>{children}</a>
  );
}

function SellerSetupChecklist({ onNavigateTab }: Pick<Props, 'onNavigateTab'>) {
  const { currentStore, platformSettings, toPersianDigits } = useStore();
  const [serverSignedVersion, setServerSignedVersion] = useState<number | null>(null);
  const [serverActiveCount, setServerActiveCount] = useState<number | null>(null);
  useEffect(() => {
    if (!currentStore?.id) return;
    let cancelled = false;
    storeApi.getOwnedStores().then(owned => {
      const mine = owned.find(store => store.id === currentStore.id);
      if (!cancelled && typeof mine?.signedContractVersion === 'number') setServerSignedVersion(mine.signedContractVersion);
    }).catch(() => undefined);
    if (currentStore.slug) {
      storeApi.getStoreBySlug(currentStore.slug).then(store => {
        if (!cancelled && Array.isArray(store.activeProductIds)) setServerActiveCount(store.activeProductIds.length);
      }).catch(() => undefined);
    }
    return () => { cancelled = true; };
  }, [currentStore?.id, currentStore?.slug]);
  if (!currentStore) return null;

  const latest = platformSettings.contracts?.sellerContractVersion ?? 0;
  const signed = Math.max(serverSignedVersion ?? 0, currentStore.signedContractVersion ?? 0);
  const steps = [
    { done: latest === 0 || signed >= latest, title: 'قرارداد همکاری را تأیید کن', detail: 'متن قرارداد را در مشخصات فروشگاه ببین.', tab: 'store-setup', action: 'دیدن قرارداد' },
    { done: !!currentStore.templateId, title: 'ظاهر فروشگاه را انتخاب کن', detail: 'با یک قالب آماده شروع کن و رنگ و بنر را متناسب با فروشگاهت تنظیم کن.', tab: 'template', action: 'انتخاب قالب' },
    { done: (serverActiveCount ?? currentStore.activeProductIds?.length ?? 0) > 0, title: 'اولین کالا را برای فروش انتخاب کن', detail: 'از کالاهای آمادهٔ سلرشو مواردی را که می‌خواهی در ویترین بگذار.', tab: 'catalog', action: 'انتخاب کالا' },
    { done: !!(currentStore.tagline?.trim() && currentStore.logo), title: 'شعار و لوگوی فروشگاه را اضافه کن', detail: 'این اطلاعات در ویترین به مشتری‌ها نشان داده می‌شود.', tab: 'template', action: 'ویرایش ظاهر فروشگاه' },
  ] as const;
  const [next, ...remaining] = steps.filter(step => !step.done);
  if (!next) return <p className="flex items-center gap-2 px-1 text-sm font-bold text-emerald-800"><Check aria-hidden="true" className="size-4 shrink-0" /> راه‌اندازی فروشگاه کامل است</p>;

  return (
    <section aria-labelledby="seller-next-title" className="rounded-2xl border border-brand-border bg-brand-bg p-4 sm:p-5">
      <p className="text-xs font-bold text-brand-deep">قدم بعدی راه‌اندازی</p>
      <div className="mt-1 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <h2 id="seller-next-title" className="text-base font-extrabold text-slate-900">{next.title}</h2>
          <p className="mt-1 text-sm leading-6 text-slate-700">{next.detail}</p>
        </div>
        <SellerShortcut tab={next.tab} onNavigateTab={onNavigateTab}
          className="inline-flex min-h-11 items-center gap-2 rounded-lg bg-brand-deep px-4 text-sm font-bold text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand">
          {next.action}<ChevronLeft aria-hidden="true" className="size-4 shrink-0" />
        </SellerShortcut>
      </div>
      {remaining.length > 0 && (
        <details className="group mt-3 border-t border-brand-border">
          <summary className={summaryClass}><ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" /> {toPersianDigits(remaining.length)} کار دیگر برای راه‌اندازی</summary>
          <ul className="divide-y divide-brand-border">
            {remaining.map(step => (
              <li key={step.title} className="flex flex-wrap items-center justify-between gap-2 py-1">
                <span className="text-sm text-slate-700">{step.title}</span>
                <SellerShortcut tab={step.tab} onNavigateTab={onNavigateTab}>{step.action}</SellerShortcut>
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}

type OverviewRead<T> = { slug: string; value?: T | null };

function overviewValue<T>(reading: OverviewRead<T> | undefined, slug: string) {
  return reading?.slug === slug ? reading.value : undefined;
}

function SellerOverviewAmount({ value, formatPrice }: { value?: number | null; formatPrice: (value: number) => string }) {
  if (value === undefined) return <div className="h-5 w-20 rounded bg-slate-100 motion-safe:animate-pulse" aria-label="در حال بارگذاری مبلغ" />;
  if (value === null) return <p className="text-[11px] text-rose-600">بارگذاری مبلغ انجام نشد.</p>;
  return <div className="break-words text-base font-extrabold tabular-nums text-slate-900">{formatPrice(value)} <span className="text-[10px] font-normal text-slate-500">تومان</span></div>;
}

export function SellerOverviewTab({ onNavigateTab, onOpenStorefront }: Props) {
  const { currentStore, formatPrice, toPersianDigits } = useStore();
  const slug = currentStore.slug;
  const [report, setReport] = useState<OverviewRead<SellerReportResponse>>();
  const [balance, setBalance] = useState<OverviewRead<number>>();
  const [pending, setPending] = useState<OverviewRead<number>>();
  const [rewardOpen, setRewardOpen] = useState(false);
  const [workAttempt, setWorkAttempt] = useState(0);
  const [work, setWork] = useState<{ store: string; orders?: number; products?: number; failed?: boolean }>();
  const mockMode = isMockMode();
  useEffect(() => {
    if (mockMode || !slug || !currentStore.id) return;
    const controller = new AbortController();
    const store = currentStore.id;
    setWork(undefined);
    void Promise.allSettled([
      getSellerWorkCounts(slug, controller.signal),
      countSellerVariantAttention(store, controller.signal),
    ]).then(([orders, variants]) => {
      if (controller.signal.aborted) return;
      setWork({ store,
        orders: orders.status === 'fulfilled' ? orders.value.orderFollowUp : undefined,
        products: variants.status === 'fulfilled' ? variants.value : undefined,
        failed: orders.status === 'rejected' || variants.status === 'rejected',
      });
    });
    return () => controller.abort();
  }, [slug, currentStore.id, mockMode, workAttempt]);
  const activeWork = work?.store === currentStore.id ? work : undefined;
  useEffect(() => {
    if (!slug) return;
    const controller = new AbortController();
    const { signal } = controller;
    const read = <T,>(request: Promise<T>, publish: (reading: OverviewRead<T>) => void) => {
      publish({ slug });
      request.then(
        (value) => { if (!signal.aborted) publish({ slug, value }); },
        () => { if (!signal.aborted) publish({ slug, value: null }); },
      );
    };
    read(storeApi.getSellerReport(slug, undefined, undefined, { signal }), setReport);
    read(storeApi.getOwnedBalance(slug, { signal }).then(result => result.availableAmount), setBalance);
    read(storeApi.getWalletBreakdown(slug, { signal }).then(result => result.pendingProfit), setPending);
    return () => controller.abort();
  }, [slug]);

  const currentReport = overviewValue(report, slug);
  const totalSales = currentReport === null ? null : currentReport?.revenue;
  const orderCount = currentReport?.orders;
  const units = currentReport?.units;
  const avgOrder = currentReport == null ? currentReport
    : currentReport.orders > 0 ? Math.round(currentReport.revenue / currentReport.orders) : 0;

  return (
    <div className="space-y-4">
      {!mockMode && <PanelWorkQueue
        loading={Boolean(slug && !activeWork)}
        error={activeWork?.failed ? 'دریافت بخشی از شمارنده‌ها انجام نشد.' : undefined}
        retry={() => setWorkAttempt(value => value + 1)}
        items={[
          { icon: ShoppingBag, label: 'سفارش‌های نیازمند پیگیری', count: activeWork?.orders, urgent: true, href: sellerSectionHref('orders', 'follow-up'), open: () => onNavigateTab('orders', 'follow-up') },
          { icon: Package, label: 'کالاهای غیرفعال یا ناموجود', count: activeWork?.products, href: sellerSectionHref('catalog', 'attention'), open: () => onNavigateTab('catalog', 'attention') },
        ]}
      />}
      <SellerSetupChecklist key={currentStore.id} onNavigateTab={onNavigateTab} />

      <section aria-labelledby="seller-quick-actions" className="rounded-2xl border border-slate-200 bg-white p-4 sm:p-5">
        <h2 id="seller-quick-actions" className="text-base font-extrabold text-slate-900">کارهای پرکاربرد</h2>
        <ul className="mt-2 grid gap-1 sm:grid-cols-3">
          <li>
            <SellerShortcut tab="catalog" onNavigateTab={onNavigateTab} className={`${linkClass} w-full border border-transparent hover:border-brand-border`}>
              <Package aria-hidden="true" className="size-4 shrink-0" />
              <span className="min-w-0 flex-1">انتخاب کالا</span>
              <ChevronLeft aria-hidden="true" className="size-4 shrink-0" />
            </SellerShortcut>
          </li>
          <li>
            <SellerShortcut tab="pricing" onNavigateTab={onNavigateTab} className={`${linkClass} w-full border border-transparent hover:border-brand-border`}>
              <DollarSign aria-hidden="true" className="size-4 shrink-0" />
              <span className="min-w-0 flex-1">تنظیم سود فروشگاه</span>
              <ChevronLeft aria-hidden="true" className="size-4 shrink-0" />
            </SellerShortcut>
          </li>
          <li>
            <button type="button" onClick={onOpenStorefront} className={`${linkClass} w-full border border-transparent text-start hover:border-brand-border`}>
              <span className="min-w-0 flex-1">مشاهدهٔ فروشگاه</span>
              <ExternalLink aria-hidden="true" className="size-4 shrink-0" />
            </button>
          </li>
        </ul>
      </section>

      <details className="group rounded-2xl border border-slate-200 bg-white px-4 py-1 sm:px-5">
        <summary className={summaryClass}>
          <BarChart3 aria-hidden="true" className="size-5 shrink-0 text-brand-deep" />
          <span className="flex-1">خلاصهٔ فروش و کیف پول</span>
          <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-open:rotate-180" />
        </summary>
        <dl className="grid grid-cols-2 gap-2 border-t border-slate-100 pb-2 pt-3 sm:gap-3 xl:grid-cols-4">
          <div className="min-w-0 rounded-xl bg-slate-50 p-3">
            <dt className="text-xs font-bold text-slate-700">سود آماده برداشت</dt>
            <dd><SellerOverviewAmount value={overviewValue(balance, slug)} formatPrice={formatPrice} /></dd>
          </div>
          <div className="min-w-0 rounded-xl bg-slate-50 p-3">
            <dt className="text-xs font-bold text-slate-700">سود در راه</dt>
            <dd>
              <SellerOverviewAmount value={overviewValue(pending, slug)} formatPrice={formatPrice} />
              <p className="text-xs leading-5 text-slate-600">پس از پایان مهلت مرجوعی آزاد می‌شود.</p>
            </dd>
          </div>
          <div className="min-w-0 rounded-xl bg-slate-50 p-3">
            <dt className="text-xs font-bold text-slate-700">مبلغ فروش</dt>
            <dd>
              <SellerOverviewAmount value={totalSales} formatPrice={formatPrice} />
              <p className="text-xs leading-5 text-slate-600">۳۱ روز اخیر</p>
              {orderCount != null && units != null && <p className="text-xs text-slate-600">{toPersianDigits(orderCount)} سفارش · {toPersianDigits(units)} کالا</p>}
            </dd>
          </div>
          <div className="min-w-0 rounded-xl bg-slate-50 p-3">
            <dt className="text-xs font-bold text-slate-700">میانگین مبلغ هر سفارش</dt>
            <dd>
              <SellerOverviewAmount value={avgOrder} formatPrice={formatPrice} />
              <p className="text-xs leading-5 text-slate-600">۳۱ روز اخیر</p>
            </dd>
          </div>
        </dl>
        <div className="flex flex-wrap gap-1">
          <SellerShortcut tab="wallet" onNavigateTab={onNavigateTab}>کیف پول و تسویه <ChevronLeft aria-hidden="true" className="size-4" /></SellerShortcut>
          <SellerShortcut tab="analytics" onNavigateTab={onNavigateTab}>تحلیل فروش <ChevronLeft aria-hidden="true" className="size-4" /></SellerShortcut>
        </div>
      </details>
      {/* 4. Network reward (live: collapsible, real values) */}
      <div className="bg-white rounded-2xl border border-slate-200 overflow-hidden">
        <button
          type="button"
          onClick={() => setRewardOpen((v) => !v)}
          aria-expanded={rewardOpen}
          className="w-full p-3.5 flex items-center justify-between gap-2 text-right min-h-[52px]"
        >
          <span className="flex items-center gap-1.5">
            <Award className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>
            <span className="text-xs font-bold text-slate-800">پاداش همکاری فروشگاه‌ها</span>
            <span className="text-[10px] text-slate-400">وقتی همه فروشگاه‌ها با هم زیاد بفروشند، به همه پاداش می‌رسد</span>
          </span>
          <ChevronDown className={`w-4 h-4 text-slate-400 shrink-0 motion-safe:transition-transform ${rewardOpen ? "rotate-180" : ""}`}  strokeWidth={1.5}/>
        </button>
        {rewardOpen && (
          <div className="px-3.5 pb-3.5 space-y-2.5">
            <p className="text-[11px] text-slate-500 leading-relaxed">وقتی مجموع فروش همه فروشگاه‌ها به ۵۰۰ عدد برسد، نصف اختلاف قیمت تأمین (پاداش شبکه) به کیف پول شما اضافه می‌شود.</p>
            <div className="space-y-1">
              <div className="flex justify-between text-[11px]">
                <span className="text-slate-600">فروش همه فروشگاه‌ها در این دوره:</span>
                <span className="font-bold text-slate-800">{toPersianDigits(0)} از {toPersianDigits(500)} عدد</span>
              </div>
              <div className="w-full h-2 bg-slate-100 rounded-full overflow-hidden">
                <div className="h-full bg-gradient-to-l from-brand-deep to-brand-deep rounded-full" style={{ width: '0%' }} />
              </div>
              <p className="text-[10px] text-slate-400">آمار فروش همه فروشگاه‌ها هنوز در دسترس نیست.</p>
            </div>
            <div className="p-2.5 bg-slate-50 rounded-xl flex items-center justify-between text-[11px]">
              <span className="text-slate-600">پاداش احتمالی شما:</span>
              <strong className="text-slate-500 font-bold text-xs">{formatPrice(0)} تومان</strong>
            </div>
            <p className="text-[10px] text-slate-400">وقتی فروش گروهی به حد لازم برسد، پاداش شما حساب و این‌جا نشان داده می‌شود.</p>
          </div>
        )}
      </div>
    </div>
  );
}
