import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi } from '../../services/storeApi';
import { TrendingUp, TrendingDown, Minus, ShoppingBag, DollarSign, Wallet, BarChart3, Package, Star } from 'lucide-react';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';

const panelClass = 'min-w-0 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5';
const summaryClass = 'min-h-11 cursor-pointer rounded-lg py-2.5 text-sm font-extrabold text-[#261b41] focus-visible:ring-2 focus-visible:ring-brand';

// Live parity (W1): port of live's seller "آمار فروش" tab (live bundle
// SellerDashboardView-MERGED.js, component En + helpers On/Wn/Rn/Ln).
// Live's /seller/analytics response shape (NOT the candidate backend's
// old SellerAnalytics shape {stores, orders, totalSales, deliveredOrders,
// views, conversionRate}): totalRevenue, orderCount, avgOrderValue,
// sellerProfit, periodComparison, views, conversionRate, statusBreakdown,
// daily, topProducts, productRows. Until the candidate backend serves
// this live shape, this tab renders live's empty state for a response
// without live fields — see the report; do NOT fake chart/table rows
// from the old shape.
// Live empty-state rule (verbatim): real stats appear once the first
// paid order exists; before that, orderCount<=0 AND no status rows AND
// no product rows => the "هنوز فروشی ثبت نشده" card.
interface PeriodValues {
  revenue: number;
  orders: number;
  avgOrderValue: number;
  profit: number;
}

interface LiveSellerAnalytics {
  totalRevenue: number;
  orderCount: number;
  avgOrderValue: number;
  sellerProfit: number;
  views?: number;
  conversionRate?: number;
  periodComparison?: { current: PeriodValues; previous: PeriodValues };
  statusBreakdown?: Array<{ status: string; count: number }>;
  daily?: Array<{ date: string; revenue: number; orders: number }>;
  topProducts?: Array<{ productId: string; name: string; quantity: number; revenue: number }>;
  productRows?: Array<{
    productId: string;
    name: string;
    quantity: number;
    revenue: number;
    profit: number;
    returnRate: number;
    returnedQty: number;
    views?: number;
    conversionRate?: number;
    reviewsCount: number;
    rating: number;
    stock: number;
    daysLeft: number | null;
  }>;
}

const STATUS_LABELS: Record<string, string> = {
  pending: 'در انتظار پرداخت',
  processing: 'در حال آماده‌سازی',
  picking: 'در حال جمع‌آوری',
  picked: 'جمع‌آوری شده',
  packed: 'بسته‌بندی شده',
  shipped: 'ارسال شده',
  delivered: 'تحویل شده',
  delivered_confirmed: 'تحویل شده',
  delivered_unconfirmed: 'رسیده به مقصد',
  delivery_disputed: 'در حال بررسی اختلاف',
  cancelled: 'لغو شده',
};

const percentChange = (current: number, previous: number): number | null =>
  previous === 0 ? (current === 0 ? 0 : null) : ((current - previous) / Math.abs(previous)) * 100;

const formatChartDate = (date: string): string => {
  // Backend day strings are 'YYYY-MM-DD'; slice defensively so a full
  // timestamp can never produce an Invalid Date (Intl.format throws on
  // one and blanked the whole tab).
  const d = new Date(`${(date || '').slice(0, 10)}T12:00:00`);
  if (isNaN(d.getTime())) return '';
  return new Intl.DateTimeFormat('fa-IR', { day: 'numeric', month: 'short' }).format(d);
};

const PeriodComparison: React.FC<{
  current: number;
  previous: number;
  formatValue: (v: number) => string;
  unit?: string;
  toPersianDigits: (v: string | number) => string;
}> = ({ current, previous, formatValue, unit, toPersianDigits }) => {
  const delta = percentChange(current, previous);
  const direction = delta === null ? 'none' : delta > 0 ? 'up' : delta < 0 ? 'down' : 'flat';
  const color = direction === 'up' ? 'text-emerald-700' : direction === 'down' ? 'text-rose-700' : 'text-slate-600';
  const Icon = direction === 'up' ? TrendingUp : direction === 'down' ? TrendingDown : Minus;
  return (
    <div className="mt-1.5 space-y-0.5">
      <p className="text-xs text-slate-600">
        ۳۰ روز اخیر: <span className="font-bold text-slate-600">{formatValue(current)}</span>
        {unit && <span> {unit}</span>}
      </p>
      <p className={`flex items-center gap-1 text-xs font-bold ${color}`}>
        <Icon aria-hidden="true" className="size-3 shrink-0" strokeWidth={1.5} />
        {delta === null ? (
          <span>دوره قبل فروشی نبود</span>
        ) : (
          <span>{toPersianDigits(Math.abs(delta).toFixed(1))}٪ نسبت به دوره قبل</span>
        )}
      </p>
    </div>
  );
};

const SalesChart: React.FC<{
  daily: Array<{ date: string; revenue: number; orders: number }>;
  formatPrice: (v: number) => string;
  toPersianDigits: (v: string | number) => string;
}> = ({ daily, formatPrice, toPersianDigits }) => {
  const max = Math.max(1, ...daily.map((d) => d.revenue));
  const width = 306;
  const height = 108;
  const step = width / daily.length;
  const barWidth = Math.max(2, step * 0.62);
  return (
    <div dir="ltr" className="mx-auto w-full max-w-2xl">
      <svg viewBox="0 0 320 140" className="w-full h-auto" role="img" aria-label="نمودار فروش ۳۰ روز گذشته">
        {[0.25, 0.5, 0.75, 1].map((f) => (
          <line key={f} x1={6} x2={312} y1={10 + height * (1 - f)} y2={10 + height * (1 - f)} stroke="#ede9fe" strokeWidth={1} strokeDasharray="3 3" />
        ))}
        {daily.map((d, j) => {
          const barHeight = (d.revenue / max) * height;
          const x = 6 + j * step + (step - barWidth) / 2;
          const y = 10 + height - barHeight;
          return (
            <g key={d.date}>
              <rect x={x} y={y} width={barWidth} height={Math.max(barHeight, d.revenue > 0 ? 2 : 0)} rx={barWidth / 2} className={d.revenue > 0 ? 'fill-brand' : 'fill-slate-100'}>
                <title>{`${formatChartDate(d.date)} — ${formatPrice(d.revenue)} تومان • ${toPersianDigits(d.orders)} سفارش`}</title>
              </rect>
              {j % 6 === 0 && (
                <text x={x + barWidth / 2} y={134} textAnchor="middle" fontSize={8} fill="#475569">
                  {formatChartDate(d.date)}
                </text>
              )}
            </g>
          );
        })}
        <text x={312} y={8} textAnchor="end" fontSize={8} className="fill-brand-deep">
          {formatPrice(max)}
        </text>
      </svg>
    </div>
  );
};

export const SellerAnalyticsTab: React.FC = () => {
  const { currentStore, formatPrice, toPersianDigits } = useStore();
  const [result, setResult] = useState<{ storeId: string; data: LiveSellerAnalytics | null }>();
  const sequence = useRef(0);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const storeId = currentStore?.id;
  const data = result?.storeId === storeId ? result?.data : undefined;

  useEffect(() => {
    if (!storeId) return;
    const controller = new AbortController();
    const request = ++sequence.current;
    let cancelled = false;
    const isCurrent = () => !cancelled && request === sequence.current;
    setLoading(true);
    setFailed(false);
    (storeApi.getSellerAnalytics(storeId, { signal: controller.signal }) as unknown as Promise<LiveSellerAnalytics>)
      .then((data) => {
        if (!isCurrent()) return;
        setResult({ storeId, data: data || null });
        setFailed(!data);
      })
      .catch(() => {
        if (!isCurrent()) return;
        setResult({ storeId, data: null });
        setFailed(true);
      })
      .finally(() => { if (isCurrent()) setLoading(false); });
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [storeId, attempt]);

  const cards = useMemo(() => {
    if (!data) return [];
    const period = data.periodComparison;
    return [
      { icon: TrendingUp, label: 'مجموع فروش (همهٔ زمان)', value: formatPrice(data.totalRevenue), unit: 'تومان', period: period ? { current: period.current.revenue, previous: period.previous.revenue, format: formatPrice, unit: 'تومان' } : undefined },
      { icon: ShoppingBag, label: 'تعداد سفارش‌ها (همهٔ زمان)', value: toPersianDigits(data.orderCount), unit: '', period: period ? { current: period.current.orders, previous: period.previous.orders, format: (v: number) => toPersianDigits(v), unit: '' } : undefined },
      { icon: DollarSign, label: 'میانگین ارزش سفارش (همهٔ زمان)', value: formatPrice(data.avgOrderValue), unit: 'تومان', period: period ? { current: period.current.avgOrderValue, previous: period.previous.avgOrderValue, format: formatPrice, unit: 'تومان' } : undefined },
      { icon: Wallet, label: 'سود شما (همهٔ زمان)', value: formatPrice(data.sellerProfit), unit: 'تومان', period: period ? { current: period.current.profit, previous: period.previous.profit, format: formatPrice, unit: 'تومان' } : undefined },
    ];
  }, [data, formatPrice, toPersianDigits]);

  const heading = <SupplierSectionHeading title="تحلیل فروش" description="روند فروش، بازدید و عملکرد کالاهای فروشگاهت را بررسی کن." icon={<BarChart3 />} />;
  const pending = loading || result?.storeId !== storeId;
  if (pending || failed) {
    return (
      <div className="space-y-4 pb-6 sm:space-y-5">
        {heading}
        <SupplierResourceState loading={pending} error={failed ? 'بارگذاری تحلیل فروش انجام نشد.' : ''} retry={() => { setLoading(true); setAttempt(value => value + 1); }} />
      </div>
    );
  }

  const statusRows = data?.statusBreakdown ?? [];
  const productRows = data?.productRows ?? [];
  const topProducts = data?.topProducts ?? [];
  const daily = data?.daily ?? [];
  const hasData = ((data?.orderCount ?? 0) > 0) || statusRows.length > 0 || productRows.length > 0;

  if (data && !hasData) {
    return (
      <div className="space-y-4 pb-6 sm:space-y-5">
        {heading}
        <SupplierEmptyState icon={<BarChart3 />} title="هنوز فروشی ثبت نشده است." description="پس از اولین سفارش پرداخت‌شده، نمودار فروش و پرفروش‌ترین محصولات اینجا نمایش داده می‌شوند." />
      </div>
    );
  }

  if (!data) return null;

  const statusTotal = statusRows.reduce((sum, row) => sum + row.count, 0);

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      {heading}
      <dl className="grid grid-cols-2 gap-2.5 xl:grid-cols-4">
        {cards.map((card) => (
          <div key={card.label} className="min-w-0 rounded-2xl border border-[#e6e2f0] bg-white p-3.5 shadow-sm">
            <dt className="mb-2 flex items-center gap-1.5">
              <span className="w-7 h-7 rounded-xl bg-brand-soft border border-brand-border flex items-center justify-center shrink-0">
                <card.icon aria-hidden="true" className="w-3.5 h-3.5 text-brand-deep" strokeWidth={1.5} />
              </span>
              <span className="text-xs font-bold text-slate-700">{card.label}</span>
            </dt>
            <dd className="break-words text-base font-extrabold tabular-nums text-slate-900">
              {card.value}
              {card.unit && <span className="ms-1 text-xs font-normal text-slate-600">{card.unit}</span>}
            </dd>
            {card.period && (
              <dd><PeriodComparison current={card.period.current} previous={card.period.previous} formatValue={card.period.format} unit={card.period.unit || undefined} toPersianDigits={toPersianDigits} /></dd>
            )}
          </div>
        ))}
      </dl>

      <section className={panelClass}>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h3 className="flex items-center gap-1.5 text-sm font-extrabold text-slate-800"><TrendingUp aria-hidden="true" className="size-4 text-brand-deep" strokeWidth={1.5} />فروش ۳۰ روز اخیر</h3>
          <span className="text-xs text-slate-600">تومان</span>
        </div>
        {daily.length > 0 ? (
          <>
            <SalesChart daily={daily} formatPrice={formatPrice} toPersianDigits={toPersianDigits} />
            <details className="mt-2 border-t border-slate-100">
              <summary className={summaryClass}>فروش روزانه</summary>
              <ul className="divide-y divide-slate-100 text-sm text-slate-700">
                {daily.map(day => (
                  <li key={day.date} className="flex flex-wrap items-center justify-between gap-x-3 py-2">
                    <time dateTime={day.date}>{formatChartDate(day.date)}</time>
                    <span>{formatPrice(day.revenue)} تومان · {toPersianDigits(day.orders)} سفارش</span>
                  </li>
                ))}
              </ul>
            </details>
          </>
        ) : <p className="py-3 text-sm text-slate-600">آمار روزانه‌ای برای این دوره ثبت نشده است.</p>}
      </section>

      <section className={panelClass}>
        <div className="flex items-center gap-1.5 mb-2">
          <span className="w-7 h-7 rounded-xl bg-brand-soft border border-brand-border flex items-center justify-center shrink-0">
            <BarChart3 aria-hidden="true" className="w-3.5 h-3.5 text-brand-deep" strokeWidth={1.5} />
          </span>
          <h3 className="text-sm font-extrabold text-slate-800">بازدید و تبدیل — ۳۰ روز اخیر</h3>
        </div>
        <dl className="grid grid-cols-2 gap-3 rounded-xl bg-[#f6f3fb] p-3">
          <div>
            <dt className="text-xs text-slate-600">بازدید فروشگاه</dt>
            <dd className="text-base font-extrabold text-slate-900">{toPersianDigits(data.views ?? 0)}</dd>
          </div>
          <div>
            <dt className="text-xs text-slate-600">نرخ تبدیل بازدید به سفارش</dt>
            <dd className="text-base font-extrabold text-slate-900">{toPersianDigits(((data.conversionRate ?? 0) * 100).toFixed(1))}٪</dd>
          </div>
        </dl>
        <p className="mt-2 text-xs leading-6 text-slate-600">هر بازدیدکننده در هر روز یک بار شمرده می‌شود. نرخ تبدیل از تقسیم سفارش‌های پرداخت‌شده بر بازدید به دست می‌آید.</p>
      </section>

      {statusRows.length > 0 && (
        <details className={panelClass}>
          <summary className={summaryClass}>وضعیت سفارش‌های پرداخت‌شده (همهٔ زمان)</summary>
          <ul className="divide-y divide-slate-50">
            {statusRows.map((row) => (
              <li key={row.status} className="py-2 flex items-center gap-3">
                <span className="flex-1 min-w-0">
                  <span className="block text-[11px] font-bold text-slate-800">{STATUS_LABELS[row.status] || row.status}</span>
                  <span className="mt-1 block h-1.5 rounded-full bg-slate-100 overflow-hidden">
                    <span className="block h-full rounded-full bg-brand/70" style={{ width: `${statusTotal > 0 ? Math.max(2, (row.count / statusTotal) * 100) : 0}%` }} />
                  </span>
                </span>
                <span className="text-[11px] font-extrabold text-slate-900 shrink-0">{toPersianDigits(row.count)} سفارش</span>
              </li>
            ))}
          </ul>
        </details>
      )}

      <section className={panelClass}>
        <h3 className="mb-3 text-sm font-extrabold text-slate-800">پرفروش‌ترین محصولات (همهٔ زمان)</h3>
        {topProducts.length === 0 ? (
          <p className="text-[11px] text-slate-400 text-center py-4">اطلاعات جزئی محصولات هنوز ثبت نشده است.</p>
        ) : (
          <ul className="divide-y divide-slate-50">
            {topProducts.map((p, i) => (
              <li key={p.productId} className="py-2.5 flex items-center gap-3">
                <span className="w-6 h-6 rounded-lg bg-brand-soft border border-brand-border flex items-center justify-center text-[10px] font-extrabold text-brand-deep shrink-0">{toPersianDigits(i + 1)}</span>
                <Package className="w-4 h-4 text-slate-300 shrink-0" strokeWidth={1.5} />
                <div className="flex-1 min-w-0">
                  <p className="text-[11px] font-bold text-slate-800 truncate">{p.name}</p>
                  <p className="text-[10px] text-slate-400">{toPersianDigits(p.quantity)} عدد فروخته‌شده</p>
                </div>
                <div className="text-left shrink-0">
                  <p className="text-[11px] font-extrabold text-slate-900">{formatPrice(p.revenue)}</p>
                  <p className="text-[9px] text-slate-400">تومان</p>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <details className={panelClass}>
        <summary id="seller-analytics-products" className={summaryClass}>همه محصولات فعال من</summary>
        <p className="mb-3 text-xs leading-6 text-slate-600">فروش و سود، مجموع همهٔ سفارش‌های پرداخت‌شده است. روزهای باقی‌ماندهٔ موجودی بر اساس سرعت فروش در ۳۰ روز اخیر برآورد می‌شود.</p>
        {productRows.length === 0 ? (
          <p className="text-[11px] text-slate-400 text-center py-4">محصول فعالی در فروشگاه ثبت نشده است.</p>
        ) : (
          <div role="region" aria-labelledby="seller-analytics-products" tabIndex={0} className="overflow-x-auto rounded-md focus-visible:ring-2 focus-visible:ring-brand">
            <table className="w-full min-w-[680px] text-[11px]">
              <thead>
                <tr className="text-slate-400 text-right border-b border-slate-100">
                  <th className="py-2 pl-2 font-bold">محصول</th>
                  <th className="py-2 pl-2 font-bold">فروش</th>
                  <th className="py-2 pl-2 font-bold">مبلغ فروش</th>
                  <th className="py-2 pl-2 font-bold">سود</th>
                  <th className="py-2 pl-2 font-bold">مرجوعی</th>
                  <th className="py-2 pl-2 font-bold">بازدید ۳۰ روز</th>
                  <th className="py-2 pl-2 font-bold">تبدیل</th>
                  <th className="py-2 pl-2 font-bold">امتیاز</th>
                  <th className="py-2 pl-2 font-bold">موجودی</th>
                  <th className="py-2 font-bold">روزهای باقی‌مانده</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-50">
                {productRows.map((row) => {
                  const returnPct = row.returnRate * 100;
                  const highReturn = row.returnRate > 0.1;
                  const lowDays = row.daysLeft !== null && row.daysLeft < 7;
                  return (
                    <tr key={row.productId} className="align-top">
                      <td className="py-2.5 pl-2 font-bold text-slate-800 max-w-[140px]">
                        <span className="block truncate">{row.name}</span>
                      </td>
                      <td className="py-2.5 pl-2 text-slate-600">{toPersianDigits(row.quantity)} عدد</td>
                      <td className="py-2.5 pl-2 font-bold text-slate-900 whitespace-nowrap">{formatPrice(row.revenue)} تومان</td>
                      <td className="py-2.5 pl-2 text-emerald-600 font-bold whitespace-nowrap">{formatPrice(row.profit)} تومان</td>
                      <td className={`py-2.5 pl-2 whitespace-nowrap ${highReturn ? 'text-rose-600 font-extrabold' : 'text-slate-600'}`}>
                        {toPersianDigits(returnPct.toFixed(1))}٪
                        {row.returnedQty > 0 && <span className="block text-[9px] font-normal text-slate-400">{toPersianDigits(row.returnedQty)} عدد برگشتی</span>}
                        {highReturn && <span className="block text-[9px]">مرجوعی بالاست</span>}
                      </td>
                      <td className="py-2.5 pl-2 text-slate-600 whitespace-nowrap">{toPersianDigits(row.views ?? 0)} بازدید</td>
                      <td className="py-2.5 pl-2 text-slate-600 whitespace-nowrap">
                        {(row.views ?? 0) > 0 ? (
                          <>{toPersianDigits(((row.conversionRate ?? 0) * 100).toFixed(1))}٪</>
                        ) : (
                          <span className="text-slate-400">بازدیدی ندارد</span>
                        )}
                      </td>
                      <td className="py-2.5 pl-2 text-slate-600 whitespace-nowrap">
                        {row.reviewsCount > 0 ? (
                          <span className="inline-flex items-center gap-1">
                            <Star className="w-3 h-3 text-amber-400" strokeWidth={1.5} />
                            {toPersianDigits(row.rating.toFixed(1))}
                            <span className="text-slate-400">({toPersianDigits(row.reviewsCount)} نظر)</span>
                          </span>
                        ) : (
                          <span className="text-slate-400">نظری ندارد</span>
                        )}
                      </td>
                      <td className="py-2.5 pl-2 text-slate-600">{toPersianDigits(row.stock)} عدد</td>
                      <td className={`py-2.5 whitespace-nowrap ${lowDays ? 'text-rose-600 font-extrabold' : 'text-slate-600'}`}>
                        {row.daysLeft === null ? (
                          <span className="text-slate-400">بدون فروش اخیر</span>
                        ) : (
                          <>
                            {toPersianDigits(Math.floor(row.daysLeft))} روز
                            {lowDays && <span className="block text-[9px]">موجودی رو به اتمام است</span>}
                          </>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </details>
    </div>
  );
};
