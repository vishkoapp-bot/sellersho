import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { TrendingUp, DollarSign, ShoppingBag, Clock, ArrowUpRight, Package, ExternalLink, Flame, Layers, Award, ChevronDown, Check, ChevronLeft, Rocket } from 'lucide-react';
import { storeApi, type SellerReportResponse } from '../../services/storeApi';

interface Props {
  onNavigateTab: (tab: string) => void;
  onOpenStorefront: () => void;
}


// Live parity (W1): seller setup checklist (live bundle
// SellerDashboardView-MERGED.js, component Ol, rendered inside the
// overview between the header card and the KPI cards). Live shows the
// "ready" row once every step is done, otherwise a progress card with
// one button per unfinished step.
interface SetupStep {
  id: string;
  title: string;
  hint: string;
  done: boolean;
  tab: string;
  action: string;
}

const SellerSetupChecklist: React.FC<{ onNavigateTab: (tab: string) => void }> = ({ onNavigateTab }) => {
  const { currentStore, platformSettings, toPersianDigits } = useStore();
  const [serverSignedVersion, setServerSignedVersion] = useState<number | null>(null);
  const [serverActiveCount, setServerActiveCount] = useState<number | null>(null);
  useEffect(() => {
    if (!currentStore?.id) return;
    let cancelled = false;
    storeApi.getOwnedStores()
      .then((owned) => {
        if (cancelled) return;
        const mine = owned.find((s) => s.id === currentStore.id);
        if (mine && typeof mine.signedContractVersion === 'number') setServerSignedVersion(mine.signedContractVersion);
      })
      .catch(() => {});
    if (currentStore.slug) {
      storeApi.getStoreBySlug(currentStore.slug)
        .then((store) => {
          if (!cancelled && Array.isArray(store.activeProductIds)) setServerActiveCount(store.activeProductIds.length);
        })
        .catch(() => {});
    }
    return () => { cancelled = true; };
  }, [currentStore?.id, currentStore?.slug]);
  if (!currentStore) return null;
  const latestContract = platformSettings.contracts?.sellerContractVersion || 0;
  const signed = serverSignedVersion !== null
    ? serverSignedVersion
    : (typeof currentStore.signedContractVersion === 'number' ? currentStore.signedContractVersion : null);
  const steps: SetupStep[] = [];
  if (signed !== null) {
    steps.push({
      id: 'contract',
      title: 'قرارداد همکاری را امضا کن',
      hint: 'بدون امضای قرارداد، فروشگاهت شروع به کار نمی‌کند.',
      done: latestContract > 0 && signed >= latestContract,
      tab: 'store-setup',
      action: 'دیدن قرارداد',
    });
  }
  steps.push(
    {
      id: 'template',
      title: 'ظاهر فروشگاهت را انتخاب کن',
      hint: 'یک قالب آماده انتخاب کن؛ رنگ و بنر را همان‌جا درست می‌کنی.',
      done: !!currentStore.templateId,
      tab: 'template',
      action: 'انتخاب ظاهر',
    },
    {
      id: 'products',
      title: 'کالا برای فروش انتخاب کن',
      hint: 'از کالاهای آماده پلتفرم انتخاب کن؛ لازم نیست خودت جنس بخری یا انبار داشته باشی.',
      done: serverActiveCount !== null ? serverActiveCount > 0 : ((currentStore.activeProductIds?.length ?? 0) > 0),
      tab: 'catalog',
      action: 'انتخاب کالا',
    },
    {
      id: 'identity',
      title: 'شعار و لوگوی فروشگاهت را بگذار',
      hint: 'مشتری با اسم، شعار و لوگوی فروشگاهت تو را می‌شناسد.',
      done: !!(currentStore.tagline?.trim() && currentStore.logo),
      tab: 'template',
      action: 'گذاشتن شعار و لوگو',
    },
  );
  const doneCount = steps.filter((s) => s.done).length;
  if (doneCount === steps.length) {
    return (
      <div className="flex items-center gap-2 px-1.5 py-0.5">
        <span className="w-6 h-6 rounded-full bg-emerald-600 text-white flex items-center justify-center shrink-0">
          <Check className="w-3.5 h-3.5" strokeWidth={2} />
        </span>
        <p className="text-[11px] text-slate-600">
          <span className="font-bold text-emerald-600">فروشگاهت آماده فروش است</span>
          {' — '}
          لینک فروشگاهت را از بخش «لینک و کد QR» برای مشتری‌ها بفرست.
        </p>
      </div>
    );
  }
  return (
    <div className="rounded-2xl border border-brand-border bg-white p-3.5 shadow-2xs space-y-3">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="w-9 h-9 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center shrink-0">
            <Rocket className="w-4 h-4" strokeWidth={1.5} />
          </span>
          <div>
            <h3 className="text-xs font-extrabold text-slate-800">راه‌اندازی فروشگاهت</h3>
            <p className="text-[11px] text-slate-500 mt-0.5">
              {toPersianDigits(doneCount)} از {toPersianDigits(steps.length)} قدم انجام شده — هر قدم را با دکمه‌اش انجام بده.
            </p>
          </div>
        </div>
      </div>
      <div className="h-2 rounded-full bg-slate-100 overflow-hidden" aria-hidden="true">
        <div className="h-full rounded-full bg-brand-deep transition-all" style={{ width: `${Math.round((doneCount / steps.length) * 100)}%` }} />
      </div>
      <div className="space-y-2">
        {steps.map((step, i) => (
          <div key={step.id} className={`flex items-center gap-2.5 rounded-xl border p-2.5 ${step.done ? 'border-emerald-100 bg-emerald-50/50' : 'border-slate-200 bg-white'}`}>
            <span className={`w-7 h-7 rounded-full flex items-center justify-center shrink-0 text-[11px] font-extrabold ${step.done ? 'bg-emerald-600 text-white' : 'bg-slate-100 text-slate-500'}`}>
              {step.done ? <Check className="w-4 h-4" strokeWidth={2} /> : toPersianDigits(i + 1)}
            </span>
            <div className="flex-1 min-w-0">
              <p className={`text-xs font-bold ${step.done ? 'text-slate-500 line-through decoration-emerald-300' : 'text-slate-800'}`}>{step.title}</p>
              {!step.done && <p className="text-[11px] leading-5 text-slate-500 mt-0.5">{step.hint}</p>}
            </div>
            {!step.done && step.tab && (
              <button type="button" onClick={() => onNavigateTab(step.tab)} className="shrink-0 flex items-center gap-0.5 min-h-[44px] px-3 rounded-xl bg-brand-deep text-white text-xs font-bold">
                <span>{step.action}</span>
                <ChevronLeft className="w-3.5 h-3.5" strokeWidth={1.5} />
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
};

type OverviewRead<T> = { slug: string; value?: T | null };

function overviewValue<T>(reading: OverviewRead<T> | undefined, slug: string) {
  return reading?.slug === slug ? reading.value : undefined;
}

function SellerOverviewAmount({ value, formatPrice }: { value?: number | null; formatPrice: (value: number) => string }) {
  if (value === undefined) return <div className="h-5 w-20 rounded bg-slate-100 animate-pulse" aria-label="در حال بارگذاری مبلغ" />;
  if (value === null) return <p className="text-[11px] text-rose-600">بارگذاری مبلغ انجام نشد.</p>;
  return <div className="text-sm font-extrabold text-slate-900">{formatPrice(value)} <span className="text-[10px] font-normal text-slate-500">تومان</span></div>;
}

export const SellerOverviewTab: React.FC<Props> = ({ onNavigateTab, onOpenStorefront }) => {
  const { currentStore, formatPrice, toPersianDigits } = useStore();
  const slug = currentStore.slug;
  const [report, setReport] = useState<OverviewRead<SellerReportResponse>>();
  const [balance, setBalance] = useState<OverviewRead<number>>();
  const [pending, setPending] = useState<OverviewRead<number>>();
  const [rewardOpen, setRewardOpen] = useState(false);
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
    <div className="space-y-3 pb-6">
      {/* 1. Header Card */}
      <div className="bg-white border border-slate-200 rounded-2xl p-4">
        <div className="flex items-center justify-between gap-2">
          <div className="min-w-0">
            <h2 className="text-[13px] font-extrabold text-slate-900 flex items-center gap-1.5">
              <span className="truncate">{currentStore.name}</span>
              <span className="shrink-0 inline-flex items-center gap-1 text-[10px] font-bold bg-emerald-50 text-emerald-600 px-2 py-0.5 rounded-full">
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-600" />
                فعال
              </span>
            </h2>
            <p className="text-[10px] text-slate-400 font-mono mt-1" dir="ltr">sellershow.ir/store/{currentStore.slug}</p>
          </div>
          <button
            onClick={onOpenStorefront}
            className="shrink-0 flex items-center gap-1 bg-brand text-white text-[11px] font-bold py-2 px-3 min-h-[44px] rounded-xl active:scale-95"
          >
            <span>مشاهده فروشگاه</span>
            <ExternalLink className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          </button>
        </div>
      </div>

      <SellerSetupChecklist onNavigateTab={onNavigateTab} />

      {/* 2. KPIs */}
      <div className="grid grid-cols-2 gap-2">
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">سود آماده برداشت</span>
            <DollarSign className="w-3.5 h-3.5 text-emerald-600"  strokeWidth={1.5}/>
          </div>
          <SellerOverviewAmount value={overviewValue(balance, slug)} formatPrice={formatPrice} />
          <span className="text-[10px] text-emerald-600 font-medium block mt-1">موجودی قابل برداشت کیف پول</span>
        </div>

        <div className="bg-white rounded-2xl p-3.5 border border-slate-200">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">سود در راه</span>
            <Clock className="w-3.5 h-3.5 text-amber-600"  strokeWidth={1.5}/>
          </div>
          <SellerOverviewAmount value={overviewValue(pending, slug)} formatPrice={formatPrice} />
          <span className="text-[10px] text-amber-600 font-medium block mt-1">بعد از تمام‌شدن مهلت مرجوعی آزاد می‌شود</span>
          <span className="text-[10px] text-slate-500 block">بر اساس تفکیک موجودی کیف پول</span>
        </div>

        <div className="bg-white rounded-2xl p-3.5 border border-slate-200">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">مبلغ کل فروش‌ها</span>
            <ShoppingBag className="w-3.5 h-3.5 text-brand-deep"  strokeWidth={1.5}/>
          </div>
          <SellerOverviewAmount value={totalSales} formatPrice={formatPrice} />
          <span className="text-[10px] text-slate-400 block mt-1">۳۱ روز اخیر</span>
          {orderCount != null && units != null && <span className="text-[10px] text-slate-400 block">{toPersianDigits(orderCount)} سفارش · {toPersianDigits(units)} کالا</span>}
        </div>

        <div className="bg-white rounded-2xl p-3.5 border border-slate-200">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">میانگین مبلغ هر سفارش</span>
            <TrendingUp className="w-3.5 h-3.5 text-brand-deep"  strokeWidth={1.5}/>
          </div>
          <SellerOverviewAmount value={avgOrder} formatPrice={formatPrice} />
          <span className="text-[10px] text-slate-400 block mt-1">۳۱ روز اخیر</span>
        </div>
      </div>

      {/* 3. Quick Actions */}
      <div className="grid grid-cols-2 gap-2">
        <button
          onClick={() => onNavigateTab('catalog')}
          className="p-3 min-h-[56px] bg-white border border-slate-200 rounded-2xl text-right flex items-center gap-2 hover:bg-slate-50"
        >
          <Package className="w-5 h-5 text-brand-deep shrink-0"  strokeWidth={1.5}/>
          <div>
            <span className="text-xs font-bold text-slate-800 block">انتخاب کالا</span>
            <span className="text-[10px] text-slate-400 block">کالاهایی که می‌خواهی بفروشی</span>
          </div>
        </button>
        <button
          onClick={() => onNavigateTab('pricing')}
          className="p-3 min-h-[56px] bg-white border border-slate-200 rounded-2xl text-right flex items-center gap-2 hover:bg-slate-50"
        >
          <DollarSign className="w-5 h-5 text-emerald-600 shrink-0"  strokeWidth={1.5}/>
          <div>
            <span className="text-xs font-bold text-slate-800 block">تنظیم سود فروشگاه</span>
            <span className="text-[10px] text-slate-400 block">از هر فروش چقدر سود می‌کنی</span>
          </div>
        </button>
      </div>

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
          <ChevronDown className={`w-4 h-4 text-slate-400 shrink-0 transition-transform ${rewardOpen ? "rotate-180" : ""}`}  strokeWidth={1.5}/>
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
};
