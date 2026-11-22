import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { AdminFullReportModal } from './AdminFullReportModal';
import { apiRequest, isMockMode } from '../../services/apiClient';
import { ShieldCheck, TrendingUp, DollarSign, Store, Building2, Activity, FileSpreadsheet, AlertTriangle, CheckCircle2, ChevronLeft, ShoppingBag } from 'lucide-react';

interface Props {
  onNavigateTab: (tab: string) => void;
}

// Live endpoint shapes, proven from /opt/drup/dist/assets/index-MERGED.js
// (the live adminApi object there; the AdminDashboardView chunk itself only
// holds the call sites ve.getOverviewSummary() etc.):
//   GET /admin/overview/summary -> { data: OverviewSummary }
//   GET /admin/overview/trends?days=30 -> { data: TrendPoint[] }
//   GET /admin/overview/funnel -> { data: OverviewFunnel }
//   GET /admin/overview/leaders -> { data: { suppliers, stores } }
//   GET /admin/overview/alerts -> { data: OverviewAlert[] }
//   GET /admin/jobs -> { jobs: ServerJob[] }
// These fetchers are defined locally in this file on purpose: adminApi.ts
// is owned by another workstream and must not be edited here.
interface OverviewSummary {
  todayGMV: number;
  todayOrders: number;
  todayProfit: number;
  d7GMV: number;
  d7Orders: number;
  d7Profit: number;
  d30GMV: number;
  d30Profit: number;
  totalGMV: number;
  totalProfit: number;
  activeStores: number;
  activeSuppliers: number;
}

interface TrendPoint {
  gmv: number;
  orders: number;
}

interface OverviewFunnel {
  placed: number;
  packed: number;
  shipped: number;
  delivered: number;
  confirmed: number;
  disputed: number;
  cancelled: number;
}

interface LeaderRow {
  id: string;
  name: string;
  orders: number;
  gmv: number;
}

interface OverviewAlert {
  id: string;
  title: string;
  count: number;
  severity: 'high' | 'medium' | 'low';
  tab: string;
  items?: string[];
  amount?: number;
}

interface ServerJob {
  lastRunError?: unknown;
}

interface LeadersData {
  suppliers: LeaderRow[];
  stores: LeaderRow[];
}

const SEVERITY_ORDER: Record<string, number> = { high: 0, medium: 1, low: 2 };

const SEVERITY_STYLES: Record<string, { bar: string; chip: string; text: string }> = {
  high: { bar: 'bg-rose-500', chip: 'bg-rose-50 border-rose-200', text: 'text-rose-900' },
  medium: { bar: 'bg-amber-500', chip: 'bg-amber-50 border-amber-200', text: 'text-amber-900' },
  low: { bar: 'bg-sky-500', chip: 'bg-sky-50 border-sky-200', text: 'text-sky-900' },
};

// Live line/bar charts (Wn / Dn in the live bundle, viewBox "0 0 100 32").
// The chart geometry below reproduces their math; the surrounding labels
// are the live ones verbatim.
const TrendLineChart: React.FC<{ values: number[] }> = ({ values }) => {
  const { line, area } = React.useMemo(() => {
    const n = values.length;
    if (n < 2) return { line: '', area: '' };
    const max = Math.max(...values, 0);
    const height = 32;
    const path = `M${values.map((v, i) => {
      const x = 100 - (i / (n - 1)) * 100;
      const y = max > 0 ? height - 3 - (v / max) * (height - 8) : height - 3;
      return `${x.toFixed(2)},${y.toFixed(2)}`;
    }).join(' L')}`;
    return { line: path, area: `${path} L0,${height} L100,${height} Z` };
  }, [values]);
  if (values.length < 2) return null;
  return (
    <svg viewBox="0 0 100 32" preserveAspectRatio="none" style={{ height: 64 }} className="w-full" role="img" aria-hidden="true">
      <path d={area} fill="#5d44f8" fillOpacity={0.12} />
      <path d={line} fill="none" stroke="#5d44f8" strokeWidth={1.4} strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />
    </svg>
  );
};

const TrendBarChart: React.FC<{ values: number[] }> = ({ values }) => {
  const bars = React.useMemo(() => {
    const n = values.length;
    if (n === 0) return [];
    const max = Math.max(...values, 0);
    const width = 100;
    const height = 32;
    const barWidth = width / n;
    const w = Math.max(barWidth * 0.55, 0.8);
    return values.map((v, i) => {
      const x = width - (i + 1) * barWidth + (barWidth - w) / 2;
      const h = max > 0 ? Math.max((v / max) * (height - 6), v > 0 ? 1.5 : 0) : 0;
      return { x, y: height - h, w, h };
    });
  }, [values]);
  if (values.length === 0) return null;
  return (
    <svg viewBox="0 0 100 32" preserveAspectRatio="none" style={{ height: 64 }} className="w-full" role="img" aria-hidden="true">
      {bars.map((b, i) => (
        <rect key={i} x={b.x.toFixed(2)} y={b.y.toFixed(2)} width={b.w.toFixed(2)} height={b.h.toFixed(2)} rx={0.8} fill={values[i] > 0 ? '#5d44f8' : '#e2e8f0'} fillOpacity={values[i] > 0 ? 0.85 : 1} />
      ))}
    </svg>
  );
};

const EmptyState: React.FC<{ title: string; description: string }> = ({ title, description }) => (
  <div className="flex flex-col items-center text-center py-10 px-4 gap-2">
    <p className="text-sm font-bold text-slate-700">{title}</p>
    <p className="text-xs text-slate-500 max-w-xs leading-relaxed">{description}</p>
  </div>
);

const ErrorCard: React.FC<{ title: string; description: string; onRetry: () => void }> = ({ title, description, onRetry }) => (
  <div role="alert" className="space-y-2 rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs leading-7 text-rose-800">
    <p className="font-bold">{title}</p>
    <p>{description}</p>
    <button type="button" onClick={onRetry} className="min-h-11 rounded-lg bg-rose-600 px-4 py-2 text-xs font-bold text-white">تلاش مجدد</button>
  </div>
);

export const AdminOverviewTab: React.FC<Props> = ({ onNavigateTab }) => {
  const { orders, stores, storeStatuses, products, formatPrice, toPersianDigits } = useStore();
  const [showFullReport, setShowFullReport] = useState(false);
  const [summary, setSummary] = useState<OverviewSummary | null>(null);
  const [trends, setTrends] = useState<TrendPoint[] | null>(null);
  const [funnel, setFunnel] = useState<OverviewFunnel | null>(null);
  const [leaders, setLeaders] = useState<LeadersData | null>(null);
  const [alerts, setAlerts] = useState<OverviewAlert[] | null>(null);
  const [jobs, setJobs] = useState<ServerJob[] | null>(null);
  const [errors, setErrors] = useState<Record<string, boolean>>({});

  // D5: per-endpoint retry loaders (trends / leaders only). A retry clears
  // just that endpoint's error so the section shows its loading state
  // again, then refetches that endpoint alone — the other five fetches
  // are not repeated.
  const loadTrends = () => {
    setErrors((prev) => ({ ...prev, trends: false }));
    apiRequest<{ data: TrendPoint[] }>('/admin/overview/trends?days=30', { method: 'GET', auth: true })
      .then((res) => setTrends(res.data || []))
      .catch(() => setErrors((prev) => ({ ...prev, trends: true })));
  };
  const loadLeaders = () => {
    setErrors((prev) => ({ ...prev, leaders: false }));
    apiRequest<{ data: LeadersData }>('/admin/overview/leaders', { method: 'GET', auth: true })
      .then((res) => {
        setLeaders({
          suppliers: res.data?.suppliers || [],
          stores: res.data?.stores || [],
        });
      })
      .catch(() => setErrors((prev) => ({ ...prev, leaders: true })));
  };

  useEffect(() => {
    let cancelled = false;
    const markError = (key: string) => {
      if (!cancelled) setErrors((prev) => ({ ...prev, [key]: true }));
    };
    apiRequest<{ data: OverviewSummary }>('/admin/overview/summary', { method: 'GET', auth: true })
      .then((res) => { if (!cancelled) setSummary(res.data); })
      .catch(() => markError('summary'));
    loadTrends();
    apiRequest<{ data: OverviewFunnel }>('/admin/overview/funnel', { method: 'GET', auth: true })
      .then((res) => { if (!cancelled) setFunnel(res.data); })
      .catch(() => markError('funnel'));
    loadLeaders();
    apiRequest<{ data: OverviewAlert[] }>('/admin/overview/alerts', { method: 'GET', auth: true })
      .then((res) => { if (!cancelled) setAlerts(res.data || []); })
      .catch(() => markError('alerts'));
    apiRequest<{ jobs: ServerJob[] }>('/admin/jobs', { method: 'GET', auth: true })
      .then((res) => { if (!cancelled) setJobs(res.jobs || []); })
      .catch(() => markError('jobs'));
    return () => { cancelled = true; };
  }, []);

  const ErrorBadge: React.FC<{ errorKey: string }> = ({ errorKey }) =>
    errors[errorKey] ? (
      <span className="text-[9px] font-bold text-rose-600 bg-rose-50 border border-rose-200 rounded-full px-1.5 py-0.5">خطا در دریافت داده</span>
    ) : null;

  // Live client fallbacks (used only when the summary has not arrived):
  // GMV = sum of payableAmount, profit = sum of platformProfit, active
  // stores = stores not suspended, active suppliers = distinct supplierIds
  // over products. Live also prefers a store's own status field first;
  // the candidate carries store status in storeStatuses, so that is used.
  const clientGMV = orders.reduce((sum, o) => sum + o.payableAmount, 0);
  const clientProfit = orders.reduce((sum, o) => sum + (o.platformProfit || 0), 0);
  const clientActiveStores = stores.filter((s) => storeStatuses[s.id] !== 'suspended').length;
  const clientActiveSuppliers = new Set(products.map((p) => p.supplierId).filter(Boolean)).size;

  const mock = isMockMode();
  const showFigure = summary != null || mock;
  const todayGMV = summary ? summary.todayGMV : clientGMV;
  const todayOrders = summary ? summary.todayOrders : orders.length;
  const todayProfit = summary ? summary.todayProfit : clientProfit;
  const d7GMVAvg = summary ? Math.round(summary.d7GMV / 7) : 0;
  const d7OrdersAvg = summary ? Math.round((summary.d7Orders / 7) * 10) / 10 : 0;
  const d7ProfitAvg = summary ? Math.round(summary.d7Profit / 7) : 0;
  const d30GMV = summary ? summary.d30GMV : 0;
  const d30Profit = summary ? summary.d30Profit : 0;
  const totalGMV = summary ? summary.totalGMV : clientGMV;
  const totalProfit = summary ? summary.totalProfit : clientProfit;
  const totalStores = summary ? summary.activeStores : clientActiveStores;
  const totalSuppliers = summary ? summary.activeSuppliers : clientActiveSuppliers;

  const hasAnyError = Object.values(errors).some(Boolean);
  // D5: name WHICH endpoint(s) failed in the header badge tooltip, so the
  // next triage does not need a code hunt. The badge copy itself is unchanged.
  const failedKeys = Object.keys(errors).filter((k) => errors[k]);

  const jobsStatus = (): string => {
    if (jobs === null) return '—';
    if (jobs.length === 0) return 'غیرفعال';
    const errorCount = jobs.filter((j) => j.lastRunError).length;
    return errorCount > 0 ? `فعال — ${toPersianDigits(errorCount)} خطا` : 'فعال — بدون خطا';
  };
  const jobsHaveError = jobs !== null && jobs.length > 0 && jobs.some((j) => j.lastRunError);

  const sortedAlerts = (alerts || []).slice().sort(
    (a, b) => (SEVERITY_ORDER[a.severity] ?? 9) - (SEVERITY_ORDER[b.severity] ?? 9),
  );

  const alertSubtitle = (alert: OverviewAlert): string => {
    const fa = toPersianDigits(alert.count);
    switch (alert.id) {
      case 'stockouts':
        return `${fa} کالا با موجودی صفر${alert.items && alert.items.length > 0 ? ' — ' + alert.items.slice(0, 3).join('، ') : ''}`;
      case 'pendingPayouts':
        return `${fa} درخواست به مبلغ ${formatPrice(alert.amount || 0)} تومان`;
      case 'openDisputes':
        return `${fa} اختلاف در انتظار رسیدگی`;
      case 'shipmentsAwaitingReceive':
        return `${fa} محموله ارسال‌شده و هنوز دریافت‌نشده`;
      case 'returnsAwaitingQC':
        return `${fa} مرجوعی در صف تأیید و کنترل کیفیت`;
      case 'pendingStores':
        return `${fa} فروشگاه در انتظار تأیید شما`;
      case 'pendingProposals':
        return `${fa} کالای پیشنهادی در صف بررسی`;
      case 'stuckPayments':
        return `${fa} سفارش با پرداخت ناموفق یا معلق بیش از ۲۴ ساعت`;
      case 'profitLockBacklog':
        return `${fa} سفارش به مبلغ ${formatPrice(alert.amount || 0)} تومان سود در انتظار آزادسازی`;
      default:
        return `${fa} مورد`;
    }
  };

  const funnelStages = funnel
    ? [
        { label: 'ثبت‌شده', value: funnel.placed },
        { label: 'آماده‌سازی و بسته‌بندی', value: funnel.packed },
        { label: 'ارسال‌شده', value: funnel.shipped },
        { label: 'تحویل‌شده', value: funnel.delivered },
        { label: 'تأیید نهایی', value: funnel.confirmed },
      ]
    : [];
  const funnelMax = Math.max(1, ...funnelStages.map((s) => s.value));

  const hasTrendData = trends !== null && trends.some((t) => t.gmv > 0 || t.orders > 0);

  const renderLeaderRow = (row: LeaderRow, rank: number) => (
    <div key={row.id} className="flex items-center gap-2 py-1.5 border-b border-slate-100 last:border-0">
      <span className="w-5 h-5 rounded-full bg-brand/10 text-brand-deep text-[10px] font-extrabold flex items-center justify-center shrink-0">{toPersianDigits(rank)}</span>
      <span className="text-[11px] font-bold text-slate-800 truncate flex-1">{row.name}</span>
      <span className="text-[10px] text-slate-500 shrink-0">{toPersianDigits(row.orders)} سفارش</span>
      <span className="text-[11px] font-extrabold text-slate-900 shrink-0">{formatPrice(row.gmv)}</span>
    </div>
  );

  return (
    <div className="space-y-4 pb-6">
      {/* Live intro banner text (verbatim). The live Ce banner also carries a
          help popover (helpTitle/helpText); no shared Ce component exists in
          the candidate, so only the text is reproduced here — see report. */}
      <p className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4 text-[11px] text-slate-600 leading-6">
        این صفحه اولین جایی است که هر روز نگاه می‌کنی: امروز چقدر فروخته‌ای، چقدر سود کرده‌ای و چه کارهایی منتظر تصمیم توست. از بالا به پایین بخوان؛ اول کارهای فوری، بعد عددهای امروز.
      </p>

      {/* Header card + server-jobs strip */}
      <div className="bg-gradient-to-l from-slate-950 via-slate-900 to-brand-deep rounded-2xl p-4 text-white shadow-xs">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="w-9 h-9 rounded-xl bg-brand-deep/30 border border-brand/40 flex items-center justify-center text-brand">
              <ShieldCheck className="w-5 h-5" strokeWidth={1.5} />
            </div>
            <div>
              <h2 className="text-xs font-bold flex items-center gap-1.5">
                <span>نمای کلی سلرشو</span>
                {hasAnyError ? (
                  <span title={`خطا در دریافت: ${failedKeys.join('، ')}`} className="text-[9px] bg-amber-500/20 text-amber-300 border border-amber-500/30 px-1.5 py-0.5 rounded-full font-bold">خطا در دریافت برخی داده‌ها</span>
                ) : showFigure ? (
                  <span className="text-[9px] bg-emerald-500/20 text-emerald-300 border border-emerald-500/30 px-1.5 py-0.5 rounded-full font-bold">سایت در حال کار است</span>
                ) : (
                  <span className="text-[9px] bg-white/10 text-slate-300 border border-white/20 px-1.5 py-0.5 rounded-full font-bold">در حال بررسی…</span>
                )}
              </h2>
              <span className="text-[11px] text-slate-300 block mt-1 leading-5">فروش، سود و کارهای منتظر تو — همه در یک نگاه</span>
            </div>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2 mt-3 pt-2.5 border-t border-white/10 text-[11px] text-slate-300">
          <div>
            سفارش‌های امروز: <strong className="text-white">{toPersianDigits(todayOrders)} عدد</strong>
          </div>
          <div className="flex items-center gap-1">
            <span>کارهای خودکار سرور:</span>
            <strong className={jobsHaveError ? 'text-rose-400' : 'text-emerald-400'}>
              {jobsStatus()} <ErrorBadge errorKey="jobs" />
            </strong>
            {jobs !== null && jobs.length > 0 && (
              <button onClick={() => onNavigateTab('jobs-queue')} className="min-h-[40px] px-2 flex items-center text-brand underline underline-offset-2 text-[10px] font-bold">
                دیدن جزئیات
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Waiting tasks */}
      <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4">
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-xs font-extrabold text-slate-800 flex items-center gap-1.5">
            <AlertTriangle className="w-4 h-4 text-amber-500" strokeWidth={1.5} />
            <span title="هر ردیف یک صفِ کاری است؛ رویش بزنی، درست می‌روی به همان بخشی که باید کار را انجام دهی. ردیف قرمز فوری‌تر از زرد است.">کارهایی که منتظر توست</span>
            <ErrorBadge errorKey="alerts" />
          </h3>
          {sortedAlerts.length > 0 && (
            <span className="text-[10px] font-bold text-rose-600 bg-rose-50 border border-rose-200 rounded-full px-2 py-0.5">{toPersianDigits(sortedAlerts.length)} مورد</span>
          )}
        </div>
        {alerts === null ? (
          <p className="text-[11px] text-slate-400 py-2 text-center">— در حال دریافت…</p>
        ) : sortedAlerts.length === 0 ? (
          <div className="flex items-center gap-2 py-2 text-emerald-700">
            <CheckCircle2 className="w-4 h-4 shrink-0" strokeWidth={1.5} />
            <p className="text-[11px] font-bold leading-6">همه‌چیز آرام است — هیچ کار فوری‌ای منتظر تو نیست.</p>
          </div>
        ) : (
          <div className="space-y-2">
            {sortedAlerts.map((alert) => {
              const style = SEVERITY_STYLES[alert.severity] || SEVERITY_STYLES.low;
              return (
                <button key={alert.id} onClick={() => onNavigateTab(alert.tab)} className={`w-full min-h-[44px] ${style.chip} border rounded-xl p-3 flex items-center gap-2 text-right hover:brightness-95 transition`}>
                  <span className={`w-1.5 self-stretch rounded-full ${style.bar} shrink-0`} />
                  <div className="flex-1 min-w-0">
                    <div className={`text-[11px] font-extrabold ${style.text} flex items-center gap-1.5`}>
                      {alert.title}
                      <span className="text-[10px] font-bold">{toPersianDigits(alert.count)}</span>
                    </div>
                    <div className="text-[10px] text-slate-500 truncate mt-0.5">{alertSubtitle(alert)}</div>
                  </div>
                  <ChevronLeft className="w-4 h-4 text-slate-400 shrink-0" />
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Four figure cards */}
      <div className="grid grid-cols-2 gap-2.5">
        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700 flex items-center gap-1" title="همان GMV است: جمع پول همه سفارش‌های امروز، قبل از کم شدن سهم فروشنده، تأمین‌کننده و هزینه‌ها.">
              فروش امروز (مبلغ کل سفارش‌ها) <ErrorBadge errorKey="summary" />
            </span>
            <TrendingUp className="w-4 h-4 text-brand-deep shrink-0" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {showFigure ? (
              <>{formatPrice(todayGMV)} <span className="text-[9px] font-normal text-slate-500">تومان</span></>
            ) : '—'}
          </div>
          <span className="text-[10px] text-slate-400 block mt-1.5 leading-5">
            میانگین فروش هر روز در ۷ روز گذشته: {summary ? formatPrice(d7GMVAvg) : '—'}
          </span>
        </div>

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">تعداد سفارش‌های امروز</span>
            <ShoppingBag className="w-4 h-4 text-brand-deep shrink-0" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {showFigure ? (
              <>{toPersianDigits(todayOrders)} <span className="text-[9px] font-normal text-slate-500">عدد</span></>
            ) : '—'}
          </div>
          <span className="text-[10px] text-slate-400 block mt-1.5 leading-5">
            میانگین هر روز در ۷ روز گذشته: {summary ? toPersianDigits(d7OrdersAvg) : '—'}
          </span>
        </div>

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">سود امروز پلتفرم</span>
            <DollarSign className="w-4 h-4 text-emerald-600 shrink-0" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-emerald-600">
            {showFigure ? (
              <>+{formatPrice(todayProfit)} <span className="text-[9px] font-normal text-slate-500">تومان</span></>
            ) : '—'}
          </div>
          <span className="text-[10px] text-slate-400 block mt-1.5 leading-5">
            میانگین سود هر روز در ۷ روز گذشته: {summary ? formatPrice(d7ProfitAvg) : '—'}
          </span>
        </div>

        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs">
          <div className="flex items-center justify-between mb-1">
            <span className="text-[11px] font-bold text-slate-700">فروش ۳۰ روز گذشته</span>
            <TrendingUp className="w-4 h-4 text-brand-deep shrink-0" strokeWidth={1.5} />
          </div>
          <div className="text-sm font-extrabold text-slate-900">
            {summary ? (
              <>{formatPrice(d30GMV)} <span className="text-[9px] font-normal text-slate-500">تومان</span></>
            ) : '—'}
          </div>
          <span className="text-[10px] text-slate-400 block mt-1.5 leading-5">
            سود همین ۳۰ روز: {summary ? formatPrice(d30Profit) : '—'}
          </span>
        </div>
      </div>

      {/* 30-day trend charts */}
      <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4 space-y-3">
        <h3 className="text-xs font-extrabold text-slate-800 flex items-center gap-1.5">
          فروش هر روز در ۳۰ روز گذشته <ErrorBadge errorKey="trends" />
        </h3>
        {errors['trends'] ? (
          <ErrorCard title="دریافت نمودار فروش ناموفق بود" description="نمودار ۳۰ روز گذشته بارگذاری نشد؛ فروش‌ها ثبت شده‌اند، فقط نمایش نمودار به‌روز نیست." onRetry={loadTrends} />
        ) : trends === null ? (
          <p className="text-[11px] text-slate-400 py-2 text-center">در حال دریافت…</p>
        ) : hasTrendData ? (
          <>
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] font-bold text-slate-500">مبلغ فروش هر روز (تومان) — هر نقطه یک روز است</span>
              </div>
              <TrendLineChart values={(trends || []).map((t) => t.gmv)} />
            </div>
            <div>
              <div className="flex items-center justify-between mb-1">
                <span className="text-[11px] font-bold text-slate-500">تعداد سفارش هر روز — هر ستون یک روز است</span>
              </div>
              <TrendBarChart values={(trends || []).map((t) => t.orders)} />
            </div>
          </>
        ) : (
          <EmptyState title="هنوز فروشی ثبت نشده است" description="وقتی اولین سفارش‌ها بیایند، نمودار فروش و تعداد سفارش هر روز همین‌جا کشیده می‌شود." />
        )}
      </div>

      {/* Funnel */}
      <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4">
        <h3 className="text-xs font-extrabold text-slate-800 mb-2 flex items-center gap-1.5">
          <span title="هر سفارش از «ثبت‌شده» شروع می‌شود و قدم‌به‌قدم به «تأیید نهایی» می‌رسد. هر ردیف می‌گوید همین حالا چند سفارش در آن مرحله مانده است.">سفارش‌ها الان در چه مرحله‌ای‌اند؟</span>
          <ErrorBadge errorKey="funnel" />
        </h3>
        {funnel == null ? (
          <p className="text-[11px] text-slate-400 py-2 text-center">—</p>
        ) : (
          <>
            <div className="space-y-1.5">
              {funnelStages.map((stage) => (
                <div key={stage.label} className="flex items-center gap-2">
                  <span className="text-[10px] text-slate-600 w-28 shrink-0">{stage.label}</span>
                  <div className="flex-1 h-5 bg-slate-100 rounded-md overflow-hidden">
                    <div className="h-full bg-gradient-to-l from-brand to-brand-deep rounded-md transition-all" style={{ width: `${Math.max((stage.value / funnelMax) * 100, stage.value > 0 ? 3 : 0)}%` }} />
                  </div>
                  <span className="text-[11px] font-extrabold text-slate-800 w-10 text-left shrink-0">{toPersianDigits(stage.value)}</span>
                </div>
              ))}
            </div>
            <div className="flex items-center gap-3 mt-2 pt-2 border-t border-slate-100 text-[10px] text-slate-500">
              {/* DELIBERATE deviation from live: live renders x($.disputed) /
                  x($.cancelled) directly, which prints the literal string
                  "undefined" when the server omits those fields (live's own
                  display bug, seen in the 2026-11-04 comparison). The ?? 0
                  guard below makes that rendering impossible. */}
              <span>در اختلاف: <strong className="text-rose-600">{toPersianDigits(funnel.disputed ?? 0)}</strong></span>
              <span>لغوشده: <strong className="text-slate-700">{toPersianDigits(funnel.cancelled ?? 0)}</strong></span>
            </div>
          </>
        )}
      </div>

      {/* Leaders */}
      <div className="grid grid-cols-1 gap-2.5">
        <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4">
          <h3 className="text-xs font-extrabold text-slate-800 mb-1 flex items-center gap-1.5">
            <Building2 className="w-4 h-4 text-purple-600" strokeWidth={1.5} />
            تأمین‌کنندگانی که بیشتر فروخته‌اند <span className="text-[10px] font-normal text-slate-400">(به ترتیب مبلغ فروش)</span>
          </h3>
          {errors['leaders'] ? (
            <ErrorCard title="دریافت برترین‌ها ناموفق بود" description="فهرست تأمین‌کنندگان و فروشگاه‌های پرفروش بارگذاری نشد." onRetry={loadLeaders} />
          ) : leaders === null ? (
            <p className="text-[11px] text-slate-400 py-2 text-center">— در حال دریافت…</p>
          ) : leaders.suppliers.length === 0 ? (
            <EmptyState title="هنوز فروشی ثبت نشده است" description="وقتی کالای یک تأمین‌کننده فروخته شود، نامش با تعداد سفارش و مبلغ فروش همین‌جا می‌آید." />
          ) : (
            leaders.suppliers.map((row, i) => renderLeaderRow(row, i + 1))
          )}
        </div>

        <div className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs p-4">
          <h3 className="text-xs font-extrabold text-slate-800 mb-1 flex items-center gap-1.5">
            <Store className="w-4 h-4 text-brand-deep" strokeWidth={1.5} />
            فروشگاه‌هایی که بیشتر فروخته‌اند <span className="text-[10px] font-normal text-slate-400">(به ترتیب مبلغ فروش)</span>
          </h3>
          {errors['leaders'] ? (
            <ErrorCard title="دریافت برترین‌ها ناموفق بود" description="فهرست تأمین‌کنندگان و فروشگاه‌های پرفروش بارگذاری نشد." onRetry={loadLeaders} />
          ) : leaders === null ? (
            <p className="text-[11px] text-slate-400 py-2 text-center">— در حال دریافت…</p>
          ) : leaders.stores.length === 0 ? (
            <EmptyState title="هنوز فروشی ثبت نشده است" description="وقتی فروشگاهی اولین فروشش را بکند، نامش با تعداد سفارش و مبلغ فروش همین‌جا می‌آید." />
          ) : (
            leaders.stores.map((row, i) => renderLeaderRow(row, i + 1))
          )}
        </div>
      </div>

      {/* All-time dark card */}
      <div className="bg-slate-900 rounded-2xl p-4 text-white">
        <p className="text-[11px] font-bold text-slate-300 mb-2.5">از روز اول تا امروز (جمع کل)</p>
        <div className="grid grid-cols-2 gap-2.5 text-[11px]">
          <div>
            <span className="text-slate-400 block leading-5">کل فروش از ابتدا</span>
            <strong className="text-sm">
              {showFigure ? formatPrice(totalGMV) : '—'} <span className="text-[9px] font-normal text-slate-400">تومان</span>
            </strong>
          </div>
          <div>
            <span className="text-slate-400 block leading-5">کل سود پلتفرم از ابتدا</span>
            <strong className="text-sm text-emerald-400">
              {showFigure ? `+${formatPrice(totalProfit)}` : '—'} <span className="text-[9px] font-normal text-slate-400">تومان</span>
            </strong>
          </div>
          <div>
            <span className="text-slate-400 block">فروشگاه‌های فعال</span>
            <strong className="text-sm">
              {showFigure ? toPersianDigits(totalStores) : '—'} <span className="text-[9px] font-normal text-slate-400">فروشگاه</span>
            </strong>
          </div>
          <div>
            <span className="text-slate-400 block">تأمین‌کنندگان فعال</span>
            <strong className="text-sm">
              {showFigure ? toPersianDigits(totalSuppliers) : '—'} <span className="text-[9px] font-normal text-slate-400">شرکت</span>
            </strong>
          </div>
        </div>
      </div>

      {/* Live trailing shortcuts */}
      <div className="grid grid-cols-2 gap-2.5">
        <button onClick={() => onNavigateTab('commission')} className="min-h-[56px] p-3.5 bg-white border border-slate-200/90 rounded-2xl text-right flex items-center gap-2 hover:bg-slate-50 shadow-2xs">
          <DollarSign className="w-4 h-4 text-emerald-600 shrink-0" strokeWidth={1.5} />
          <div>
            <span className="text-[11px] font-bold text-slate-800 block leading-5">سهم‌ها و هزینه‌ها</span>
            <span className="text-[10px] text-slate-400 block leading-5">سهم پلتفرم و هزینه ارسال را تنظیم کن</span>
          </div>
        </button>

        <button onClick={() => onNavigateTab('ad-network')} className="min-h-[56px] p-3.5 bg-white border border-slate-200/90 rounded-2xl text-right flex items-center gap-2 hover:bg-slate-50 shadow-2xs">
          <Activity className="w-4 h-4 text-brand-deep shrink-0" strokeWidth={1.5} />
          <div>
            <span className="text-[11px] font-bold text-slate-800 block leading-5">بررسی تقلب در تبلیغات</span>
            <span className="text-[10px] text-slate-400 block leading-5">کلیک‌ها و حراج تبلیغات را ببین</span>
          </div>
        </button>
      </div>

      {/* Full report (live trailing button + modal; label corrected to live's) */}
      <button
        onClick={() => setShowFullReport(true)}
        className="w-full min-h-[48px] px-4 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1.5"
      >
        <FileSpreadsheet className="w-4 h-4 text-brand" strokeWidth={1.5} />
        <span>دیدن گزارش کامل فروش و سود</span>
      </button>

      <AdminFullReportModal
        isOpen={showFullReport}
        onClose={() => setShowFullReport(false)}
      />
    </div>
  );
};
