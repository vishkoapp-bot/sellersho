import { SupplierSectionHeading } from './SupplierSectionHeading';
import React, { useCallback, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { supplierApi } from '../../services/supplierApi';
import { Search, Plus } from 'lucide-react';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import { useSupplierResource } from './useSupplierResource';

// اینسایت تقاضا — GET /supplier/search-insight?days=7|30|90.
// The server computes PLATFORM-WIDE search aggregates and footprint-scopes
// them to this supplier (their own product titles, brands and category
// titles); this tab renders exactly what the API returns and does no
// scoping or guessing of its own.

interface SupplierDemandInsightTabProps {
  onPropose?: () => void;
}

const WINDOW_OPTIONS: Array<{ days: number; label: string }> = [
  { days: 7, label: '۷ روز اخیر' },
  { days: 30, label: '۳۰ روز اخیر' },
  { days: 90, label: '۹۰ روز اخیر' },
];

const formatDelta = (value: number): string => {
  const raw = Math.abs(value).toFixed(1).replace(/\.0$/, '').replace('.', '٫');
  return raw;
};

export const SupplierDemandInsightTab: React.FC<SupplierDemandInsightTabProps> = ({ onPropose }) => {
  const { toPersianDigits } = useStore();
  const [days, setDays] = useState(30);
  const load = useCallback(() => supplierApi.getSearchInsight(days), [days]);
  const { data, loading, error, reload } = useSupplierResource(load);
  const topDemand = data?.topDemand ?? [];
  const unmetInScope = data?.unmetInScope ?? [];
  const ownProductTrend = data?.ownProductTrend ?? [];
  const hasAny = topDemand.length > 0 || unmetInScope.length > 0 || ownProductTrend.length > 0;

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="تقاضای بازار" description="جستجوهای مرتبط با کالاها و دسته‌های فروشگاهت را ببین." icon={<Search />}
        action={<div role="group" aria-label="بازهٔ زمانی تقاضا" className="flex flex-wrap gap-1 rounded-2xl border border-[#e6e2f0] bg-[#f8f6fc] p-1">
            {WINDOW_OPTIONS.map((opt) => (
              <button
                key={opt.days}
                type="button"
                aria-pressed={days === opt.days}
                onClick={() => setDays(opt.days)}
                className={`min-h-11 rounded-xl px-3 text-xs font-bold transition-colors focus-visible:ring-2 focus-visible:ring-brand ${days === opt.days ? 'bg-[#382660] text-white' : 'text-slate-700 hover:bg-brand-soft'}`}
              >
                {opt.label}
              </button>
            ))}
        </div>} />

      <SupplierResourceState loading={loading} error={error} retry={reload} />
      {!loading && !error && data && (!hasAny ? (
        <SupplierEmptyState icon={<Search />} title="در این بازه جستجویی ثبت نشده است." description="بازهٔ زمانی دیگری انتخاب کن تا جستجوهای مرتبط با کالاها و دسته‌هایت را ببینی." />
      ) : (
        <>
          <section className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <h3 className="mb-3 text-base font-extrabold text-[#261b41]">تقاضا برای کالاهای تو</h3>
            {topDemand.length === 0 ? (
              <p className="rounded-xl bg-slate-50 p-3 text-sm leading-6 text-slate-600">در این بازه جستجویی برای کالاها و دسته‌هایت ثبت نشده است.</p>
            ) : (
              <div className="overflow-x-auto -mx-4 px-4">
                <table className="w-full min-w-[420px] text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 text-right text-slate-700">
                      <th className="py-2 pl-2 font-bold">عبارت</th>
                      <th className="py-2 pl-2 font-bold">تعداد جستجو</th>
                      <th className="py-2 font-bold">بی‌نتیجه</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-50">
                    {topDemand.map((term) => (
                      <tr key={term.term}>
                        <td className="py-2.5 pl-2">
                          <span className="font-bold text-slate-800">{term.term}</span>
                          {(term.matchedProducts ?? []).length > 0 && (
                            <span className="flex flex-wrap gap-1.5 mt-1.5">
                              {term.matchedProducts.map((p) => (
                                <span key={p.productId} className="inline-flex items-center gap-1.5 rounded-lg bg-slate-50 border border-slate-100 py-0.5 pl-2 pr-0.5">
                                  {p.imageUrl ? (
                                    <img src={p.imageUrl} alt={p.title} className="w-6 h-6 rounded-md object-contain bg-white border border-slate-100 p-0.5 shrink-0" />
                                  ) : (
                                    <span className="w-6 h-6 rounded-md bg-white border border-slate-100 flex items-center justify-center shrink-0">
                                      <Search className="w-3 h-3 text-slate-300" strokeWidth={1.5} />
                                    </span>
                                  )}
                                  <span className="text-[10px] font-bold text-slate-600 max-w-[140px] truncate">{p.title}</span>
                                </span>
                              ))}
                            </span>
                          )}
                        </td>
                        <td className="py-2.5 pl-2 text-slate-600">{toPersianDigits(term.searches)}</td>
                        <td className="py-2.5 text-slate-600">{toPersianDigits(term.zeroResultSearches)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <h3 className="mb-1 text-base font-extrabold text-[#261b41]">فرصت‌های پیشنهاد کالا</h3>
            <p className="mb-3 text-sm leading-6 text-slate-600">مشتری‌ها این عبارت‌های مرتبط با دسته‌ها یا برندهایت را جستجو کرده‌اند و کالایی پیدا نکرده‌اند.</p>
            {unmetInScope.length === 0 ? (
              <p className="rounded-xl bg-slate-50 p-3 text-sm leading-6 text-slate-600">در این بازه فرصت تازه‌ای پیدا نشده است.</p>
            ) : (
              <>
                <ul className="divide-y divide-slate-50">
                  {unmetInScope.map((term) => (
                    <li key={term.term} className="py-2.5">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="min-w-0 break-words text-sm font-bold text-slate-800">{term.term}</span>
                        <span className="rounded-lg bg-brand-bg px-2 py-1 text-xs text-slate-700">{toPersianDigits(term.searches)} جستجو · {toPersianDigits(term.zeroResultSearches)} بی‌نتیجه</span>
                      </div>
                      {(term.scopeMatches ?? []).length > 0 && (
                        <div className="flex flex-wrap gap-1.5 mt-1.5">
                          {term.scopeMatches.map((m, i) => (
                            <span key={`${m.type}-${m.name}-${i}`} className="text-[10px] font-bold bg-brand-soft text-brand-deep px-2 py-0.5 rounded-full">
                              {m.type === 'category' ? `دسته: ${m.name}` : `برند: ${m.name}`}
                            </span>
                          ))}
                        </div>
                      )}
                    </li>
                  ))}
                </ul>
                {onPropose && (
                  <button type="button" onClick={onPropose} className="mt-3 inline-flex min-h-11 items-center gap-2 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2">
                    <Plus className="w-3.5 h-3.5" strokeWidth={1.5} />
                    پیشنهاد کالای جدید
                  </button>
                )}
              </>
            )}
          </section>

          <section className="rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <h3 className="mb-3 text-base font-extrabold text-[#261b41]">روند فروش کالاهای تو</h3>
            {ownProductTrend.length === 0 ? (
              <p className="rounded-xl bg-slate-50 p-3 text-sm leading-6 text-slate-600">در این بازه فروشی برای نمایش روند ثبت نشده است.</p>
            ) : (
              <ul className="divide-y divide-slate-50">
                {ownProductTrend.map((item) => (
                  <li key={item.productId} className="py-2.5 flex items-center justify-between gap-2">
                    <span className="text-[11px] font-bold text-slate-700 truncate">{item.title}</span>
                    <span className="flex items-center gap-2 shrink-0">
                      <span className="text-[10px] text-slate-400">{toPersianDigits(item.units)} فروش (دوره قبل: {toPersianDigits(item.prevUnits)})</span>
                      {item.deltaPct === null ? (
                        <span className="text-[10px] font-bold bg-sky-50 text-sky-600 px-2 py-0.5 rounded-full">جدید</span>
                      ) : item.deltaPct > 0 ? (
                        <span className="text-[10px] font-bold bg-emerald-50 text-emerald-600 px-2 py-0.5 rounded-full">▲ {toPersianDigits(formatDelta(item.deltaPct))}٪</span>
                      ) : item.deltaPct < 0 ? (
                        <span className="text-[10px] font-bold bg-rose-50 text-rose-600 px-2 py-0.5 rounded-full">▼ {toPersianDigits(formatDelta(item.deltaPct))}٪</span>
                      ) : (
                        <span className="text-[10px] font-bold bg-slate-50 text-slate-500 px-2 py-0.5 rounded-full">بدون تغییر</span>
                      )}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </>
      ))}
    </div>
  );
};
