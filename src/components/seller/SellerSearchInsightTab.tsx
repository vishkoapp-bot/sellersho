import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { apiRequest } from '../../services/apiClient';
import { Search, Plus, Check, RotateCcw } from 'lucide-react';

// Live parity (SellerDashboardView-MERGED.js, component _n, ~bytes 282100-288000;
// API index-MERGED.js: GET /seller/search-insight?store_id=<storeId>).
// Live's response shape is {topSearches, unmetInStore, unmetCatalog} — NOT
// the candidate's {searches, zeroResultSearches, terms[]} shape, so this tab
// reads the live shape directly. Live has NO time ranges or filters here:
// three fixed sections. Unmet-in-store rows offer a quick-add into the
// showcase (activeProductIds, defaulting to the first 8 products when the
// store has no explicit showcase yet).
interface InsightTerm {
  term: string;
  searches: number;
  zeroResultSearches: number;
}

interface CatalogMatch {
  productId: string;
  title: string;
  imageUrl?: string;
}

interface UnmetTerm extends InsightTerm {
  catalogMatches: CatalogMatch[];
}

interface LiveSearchInsight {
  topSearches: InsightTerm[];
  unmetInStore: UnmetTerm[];
  unmetCatalog: InsightTerm[];
}

export const SellerSearchInsightTab: React.FC = () => {
  const { currentStore, products, updateStoreActiveProducts, toPersianDigits } = useStore();
  const [data, setData] = useState<LiveSearchInsight | null>(null);
  const [failed, setFailed] = useState(false);
  const [added, setAdded] = useState<Set<string>>(new Set());

  useEffect(() => {
    let cancelled = false;
    const storeId = currentStore?.id;
    if (!storeId) { setFailed(true); return; }
    apiRequest<LiveSearchInsight>(`/seller/search-insight?store_id=${encodeURIComponent(storeId)}`, { method: 'GET', auth: true })
      .then((res) => { if (!cancelled) setData(res); })
      .catch(() => { if (!cancelled) setFailed(true); });
    return () => { cancelled = true; };
  }, [currentStore?.id]);

  const showcaseIds = (currentStore?.activeProductIds?.length ? currentStore.activeProductIds : products.slice(0, 8).map((p) => p.id));
  const isInShowcase = (productId: string) => showcaseIds.includes(productId) || added.has(productId);
  const handleQuickAdd = (productId: string) => {
    if (isInShowcase(productId)) return;
    const next = Array.from(new Set([...showcaseIds, ...Array.from(added), productId]));
    updateStoreActiveProducts(next);
    setAdded((prev) => new Set(prev).add(productId));
  };

  if (failed) {
    return (
      <div className="bg-white rounded-2xl border border-slate-100 p-8 text-center">
        <p className="text-xs font-bold text-slate-700 mb-1">بارگذاری اینسایت جستجو انجام نشد</p>
        <p className="text-[11px] text-slate-400 mb-4">اتصال اینترنت را بررسی کنید و دوباره تلاش کنید.</p>
        <button type="button" onClick={() => window.location.reload()} className="inline-flex items-center gap-1.5 rounded-xl bg-brand-deep px-4 py-2 text-xs font-bold text-white hover:bg-brand">
          <RotateCcw className="w-3.5 h-3.5" strokeWidth={1.5} />
          تلاش دوباره
        </button>
      </div>
    );
  }
  if (!data) {
    return (
      <div className="space-y-3 pb-6 animate-pulse" aria-label="در حال بارگذاری اینسایت جستجو">
        <div className="bg-white rounded-2xl border border-slate-100 h-32" />
        <div className="bg-white rounded-2xl border border-slate-100 h-32" />
      </div>
    );
  }

  const topSearches = data.topSearches ?? [];
  const unmetInStore = data.unmetInStore ?? [];
  const unmetCatalog = data.unmetCatalog ?? [];
  const hasAny = topSearches.length > 0 || unmetInStore.length > 0 || unmetCatalog.length > 0;
  if (!hasAny) {
    return (
      <div className="bg-white rounded-2xl border border-slate-100 p-10 text-center">
        <div className="w-14 h-14 rounded-2xl bg-brand-soft border border-brand-border flex items-center justify-center mx-auto mb-4">
          <Search className="w-7 h-7 text-brand" strokeWidth={1.5} />
        </div>
        <p className="text-sm font-extrabold text-slate-800 mb-1.5">هنوز داده‌ای جمع نشده است</p>
        <p className="text-[11px] leading-6 text-slate-400 max-w-[280px] mx-auto">هنوز داده‌ای جمع نشده است؛ ثبت جستجوها از امروز شروع شده و این بخش با جستجوی مشتری‌ها پر می‌شود.</p>
      </div>
    );
  }

  return (
    <div className="space-y-3 pb-6">
      <section className="bg-white rounded-2xl border border-slate-100 p-4">
        <h3 className="text-xs font-extrabold text-slate-800 mb-3">جستجوهای پرتکرار فروشگاهت</h3>
        {topSearches.length === 0 ? (
          <p className="text-[11px] text-slate-400 text-center py-4">هنوز جستجوی پرتکراری ثبت نشده است.</p>
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
                {topSearches.map((term) => (
                  <tr key={term.term}>
                    <td className="py-2.5 pl-2 font-bold text-slate-800">{term.term}</td>
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
        <h3 className="text-xs font-extrabold text-slate-800 mb-1">تقاضایی که فروشگاهت جواب نداده</h3>
        <p className="text-[10px] text-slate-400 mb-3">مشتری‌ها این‌ها را در فروشگاهت جستجو کرده‌اند و چیزی پیدا نکرده‌اند. اگر محصولی در کاتالوگ هست، می‌توانی همان‌جا به فروشگاهت اضافه‌اش کنی.</p>
        {unmetInStore.length === 0 ? (
          <p className="text-[11px] text-slate-400 text-center py-4">تقاضای برآورده‌نشده‌ای ثبت نشده است.</p>
        ) : (
          <div className="space-y-3">
            {unmetInStore.map((term) => (
              <div key={term.term} className="rounded-xl border border-slate-100 p-3">
                <div className="flex items-center justify-between gap-2 mb-2">
                  <span className="text-[11px] font-bold text-slate-800">{term.term}</span>
                  <span className="text-[10px] text-slate-400 shrink-0">{toPersianDigits(term.searches)} جستجو • {toPersianDigits(term.zeroResultSearches)} بی‌نتیجه</span>
                </div>
                {(term.catalogMatches ?? []).length === 0 ? (
                  <p className="text-[10px] text-slate-400">در کل کاتالوگ هم محصولی برای این عبارت نیست.</p>
                ) : (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    {(term.catalogMatches ?? []).map((match) => {
                      const inStore = isInShowcase(match.productId);
                      return (
                        <div key={match.productId} className="flex items-center gap-2 rounded-xl bg-slate-50 p-2">
                          {match.imageUrl ? (
                            <img src={match.imageUrl} alt={match.title} className="w-10 h-10 rounded-lg object-contain bg-white border border-slate-100 p-0.5 shrink-0" />
                          ) : (
                            <div className="w-10 h-10 rounded-lg bg-white border border-slate-100 flex items-center justify-center shrink-0">
                              <Search className="w-4 h-4 text-slate-300" strokeWidth={1.5} />
                            </div>
                          )}
                          <span className="flex-1 min-w-0 text-[11px] font-bold text-slate-700 truncate">{match.title}</span>
                          <button
                            type="button"
                            disabled={inStore}
                            onClick={() => handleQuickAdd(match.productId)}
                            className={`shrink-0 inline-flex items-center gap-1 rounded-lg px-2.5 py-1.5 text-[10px] font-bold ${inStore ? 'bg-emerald-50 text-emerald-600 cursor-default' : 'bg-brand-deep text-white hover:bg-brand'}`}
                          >
                            {inStore ? (
                              <>
                                <Check className="w-3 h-3" strokeWidth={1.5} />
                                در فروشگاهت هست
                              </>
                            ) : (
                              <>
                                <Plus className="w-3 h-3" strokeWidth={1.5} />
                                افزودن به فروشگاه
                              </>
                            )}
                          </button>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      <section className="bg-white rounded-2xl border border-slate-100 p-4">
        <h3 className="text-xs font-extrabold text-slate-800 mb-1">در فروشگاه مرکزی هم پیدا نشده</h3>
        <p className="text-[10px] text-slate-400 mb-3">این عبارت‌ها در فروشگاه مرکزی هم بی‌نتیجه مانده‌اند؛ فقط برای اطلاع توست.</p>
        {unmetCatalog.length === 0 ? (
          <p className="text-[11px] text-slate-400 text-center py-4">موردی ثبت نشده است.</p>
        ) : (
          <ul className="divide-y divide-slate-50">
            {unmetCatalog.map((term) => (
              <li key={term.term} className="py-2 flex items-center justify-between gap-2">
                <span className="text-[11px] font-bold text-slate-700">{term.term}</span>
                <span className="text-[10px] text-slate-400">{toPersianDigits(term.searches)} جستجو • {toPersianDigits(term.zeroResultSearches)} بی‌نتیجه</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
};
