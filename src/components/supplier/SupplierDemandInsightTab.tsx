import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { supplierApi } from '../../services/supplierApi';
import type { SupplierSearchInsight } from '../../services/supplierApi';
import { Search, RotateCcw, Plus } from 'lucide-react';

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
  const [data, setData] = useState<SupplierSearchInsight | null>(null);
  const [failed, setFailed] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setData(null);
    setFailed(false);
    supplierApi.getSearchInsight(days)
      .then((res) => { if (!cancelled) setData(res); })
      .catch(() => { if (!cancelled) setFailed(true); });
    return () => { cancelled = true; };
  }, [days, reloadKey]);

  if (failed) {
    return (
      <div className="bg-white rounded-2xl border border-slate-100 p-8 text-center">
        <p className="text-xs font-bold text-slate-700 mb-1">بارگذاری اینسایت تقاضا انجام نشد</p>
        <p className="text-[11px] text-slate-400 mb-4">اتصال اینترنت را بررسی کنید و دوباره تلاش کنید.</p>
        <button type="button" onClick={() => setReloadKey((k) => k + 1)} className="inline-flex items-center gap-1.5 rounded-xl bg-brand-deep px-4 py-2 text-xs font-bold text-white hover:bg-brand">
          <RotateCcw className="w-3.5 h-3.5" strokeWidth={1.5} />
          تلاش دوباره
        </button>
      </div>
    );
  }
  if (!data) {
    return (
      <div className="space-y-3 pb-6 animate-pulse" aria-label="در حال بارگذاری اینسایت تقاضا">
        <div className="bg-white rounded-2xl border border-slate-100 h-32" />
        <div className="bg-white rounded-2xl border border-slate-100 h-32" />
      </div>
    );
  }

  const topDemand = data.topDemand ?? [];
  const unmetInScope = data.unmetInScope ?? [];
  const ownProductTrend = data.ownProductTrend ?? [];
  const hasAny = topDemand.length > 0 || unmetInScope.length > 0 || ownProductTrend.length > 0;

  return (
    <div className="space-y-3 pb-6">
      <section className="bg-white rounded-2xl border border-slate-100 p-4">
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <h3 className="text-xs font-extrabold text-slate-800">اینسایت تقاضا</h3>
          <div className="flex items-center gap-1 rounded-xl bg-slate-50 border border-slate-100 p-1">
            {WINDOW_OPTIONS.map((opt) => (
              <button
                key={opt.days}
                type="button"
                onClick={() => setDays(opt.days)}
                className={`rounded-lg px-2.5 py-1.5 text-[10px] font-bold transition-colors ${days === opt.days ? 'bg-brand-deep text-white' : 'text-slate-500 hover:text-slate-700'}`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
        <p className="text-[10px] text-slate-400 mt-2 leading-5">بر اساس جستجوهای مشتریان در فروشگاه‌ها؛ فقط عبارت‌هایی که به کالاها و دسته‌های تو مربوط‌اند نمایش داده می‌شوند.</p>
      </section>

      {!hasAny ? (
        <div className="bg-white rounded-2xl border border-slate-100 p-10 text-center">
          <div className="w-14 h-14 rounded-2xl bg-brand-soft border border-brand-border flex items-center justify-center mx-auto mb-4">
            <Search className="w-7 h-7 text-brand" strokeWidth={1.5} />
          </div>
          <p className="text-[11px] leading-6 text-slate-400 max-w-[320px] mx-auto">هنوز داده‌ای جمع نشده است؛ این بخش با جستجوی مشتری‌ها برای کالاها و دسته‌های تو پر می‌شود.</p>
        </div>
      ) : (
        <>
          <section className="bg-white rounded-2xl border border-slate-100 p-4">
            <h3 className="text-xs font-extrabold text-slate-800 mb-3">تقاضا برای کالاهای تو</h3>
            {topDemand.length === 0 ? (
              <p className="text-[11px] text-slate-400 text-center py-4">هنوز جستجویی برای کالاها و دسته‌های تو ثبت نشده است.</p>
            ) : (
              <div className="overflow-x-auto -mx-4 px-4">
                <table className="w-full min-w-[420px] text-[11px]">
                  <thead>
                    <tr className="text-slate-400 text-right border-b border-slate-100">
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

          <section className="bg-white rounded-2xl border border-slate-100 p-4">
            <h3 className="text-xs font-extrabold text-slate-800 mb-1">تقاضایی که هنوز در کاتالوگ نیست — در حوزه تو</h3>
            <p className="text-[10px] text-slate-400 mb-3">مشتری‌ها این عبارت‌ها را جستجو کرده‌اند و در کل کاتالوگ هم محصولی برایشان نیست؛ این‌ها به دسته‌ها یا برندهای تو مربوط‌اند.</p>
            {unmetInScope.length === 0 ? (
              <p className="text-[11px] text-slate-400 text-center py-4">تقاضای برآورده‌نشده‌ای در حوزه تو ثبت نشده است.</p>
            ) : (
              <>
                <ul className="divide-y divide-slate-50">
                  {unmetInScope.map((term) => (
                    <li key={term.term} className="py-2.5">
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-[11px] font-bold text-slate-800">{term.term}</span>
                        <span className="text-[10px] text-slate-400 shrink-0">{toPersianDigits(term.searches)} جستجو • {toPersianDigits(term.zeroResultSearches)} بی‌نتیجه</span>
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
                  <button type="button" onClick={onPropose} className="mt-3 inline-flex items-center gap-1.5 rounded-xl bg-brand-deep px-4 py-2 text-xs font-bold text-white hover:bg-brand">
                    <Plus className="w-3.5 h-3.5" strokeWidth={1.5} />
                    پیشنهاد کالای جدید
                  </button>
                )}
              </>
            )}
          </section>

          <section className="bg-white rounded-2xl border border-slate-100 p-4">
            <h3 className="text-xs font-extrabold text-slate-800 mb-3">روند فروش کالاهای تو</h3>
            {ownProductTrend.length === 0 ? (
              <p className="text-[11px] text-slate-400 text-center py-4">هنوز فروشی برای نمایش روند ثبت نشده است.</p>
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
      )}
    </div>
  );
};
