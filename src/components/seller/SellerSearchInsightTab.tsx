import { useEffect, useState } from 'react';
import { Check, ChevronDown, Plus, Search } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { apiRequest } from '../../services/apiClient';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';

interface InsightTerm {
  term: string;
  searches: number;
  zeroResultSearches: number;
}

interface UnmetTerm extends InsightTerm {
  catalogMatches: { productId: string; title: string; imageUrl?: string }[];
}

interface LiveSearchInsight {
  topSearches: InsightTerm[];
  unmetInStore: UnmetTerm[];
  unmetCatalog: InsightTerm[];
}

function SearchCounts({ term }: { term: InsightTerm }) {
  const { toPersianDigits } = useStore();
  return <span className="text-xs leading-6 text-slate-600">{toPersianDigits(term.searches)} جست‌وجو · {toPersianDigits(term.zeroResultSearches)} بی‌نتیجه</span>;
}

function SearchDetails({ title, terms, empty }: { title: string; terms: InsightTerm[]; empty: string }) {
  return (
    <details className="group rounded-2xl border border-slate-200 bg-white px-4 py-1">
      <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-2 rounded-lg text-sm font-bold text-slate-900 focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden">
        {title}<ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-open:rotate-180" />
      </summary>
      {terms.length ? (
        <ul className="divide-y divide-slate-100 border-t border-slate-100 pb-2">
          {terms.map(term => (
            <li key={term.term} className="flex flex-wrap items-center justify-between gap-x-3 py-2">
              <bdi className="min-w-0 break-words text-sm font-bold text-slate-800">{term.term}</bdi>
              <SearchCounts term={term} />
            </li>
          ))}
        </ul>
      ) : <p className="pb-3 text-sm text-slate-600">{empty}</p>}
    </details>
  );
}

export function SellerSearchInsightTab() {
  const { currentStore, products, updateStoreActiveProducts } = useStore();
  const [data, setData] = useState<LiveSearchInsight>();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [added, setAdded] = useState<Set<string>>(new Set());

  useEffect(() => {
    const request = new AbortController();
    const id = currentStore?.id;
    setData(undefined);
    setError('');
    if (!id) {
      setError('ابتدا یک فروشگاه انتخاب کن.');
      return;
    }
    apiRequest<LiveSearchInsight>(`/seller/search-insight?store_id=${encodeURIComponent(id)}`, { auth: true, signal: request.signal })
      .then(result => { if (!request.signal.aborted) setData(result); })
      .catch(() => { if (!request.signal.aborted) setError('گزارش جست‌وجوی مشتریان دریافت نشد.'); });
    return () => request.abort();
  }, [currentStore?.id, attempt]);

  const showcaseIds = currentStore.activeProductIds?.length ? currentStore.activeProductIds : products.slice(0, 8).map(product => product.id);
  const topSearches = data?.topSearches ?? [];
  const unmetInStore = data?.unmetInStore ?? [];
  const unmetCatalog = data?.unmetCatalog ?? [];
  const hasData = topSearches.length > 0 || unmetInStore.length > 0 || unmetCatalog.length > 0;
  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="جست‌وجوی مشتریان" description="ببین مشتری‌ها چه کالاهایی می‌خواهند و موارد مرتبط را به ویترین اضافه کن." icon={<Search />} />
      <SupplierResourceState loading={!data && !error} error={error} retry={() => setAttempt(value => value + 1)} />
      {data && (hasData ? <>
        <section aria-labelledby="seller-unmet-searches" className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4">
          <h3 id="seller-unmet-searches" className="text-base font-extrabold text-slate-900">جست‌وجوهای بی‌نتیجه در فروشگاه</h3>
          {unmetInStore.length ? (
            <ul className="space-y-2">
              {unmetInStore.map(term => (
                <li key={term.term} className="rounded-xl border border-slate-200 p-3">
                  <div className="flex flex-wrap items-center justify-between gap-x-3">
                    <bdi className="min-w-0 break-words text-sm font-bold text-slate-900">{term.term}</bdi>
                    <SearchCounts term={term} />
                  </div>
                  {(term.catalogMatches ?? []).length ? (
                    <ul className="mt-2 grid gap-2 xl:grid-cols-2">
                      {term.catalogMatches.map(match => {
                        const inStore = showcaseIds.includes(match.productId) || added.has(match.productId);
                        return <li key={match.productId} className="flex flex-wrap items-center gap-2 rounded-lg bg-slate-50 p-2">
                          {match.imageUrl ? <img src={match.imageUrl} alt="" width={40} height={40} loading="lazy" className="size-10 shrink-0 rounded-lg bg-white object-contain" />
                            : <Search aria-hidden="true" className="size-5 shrink-0 text-slate-500" />}
                          <span className="min-w-0 flex-1 break-words text-sm font-bold text-slate-800">{match.title}</span>
                          {inStore ? <span className="inline-flex items-center gap-1 text-xs font-bold text-emerald-800"><Check aria-hidden="true" className="size-4" /> در ویترین</span>
                            : <button type="button" aria-label={`افزودن ${match.title} به ویترین`}
                              onClick={() => {
                                const next = [...new Set([...showcaseIds, ...added, match.productId])];
                                void updateStoreActiveProducts(next);
                                setAdded(current => new Set([...current, match.productId]));
                              }}
                              className="inline-flex min-h-11 items-center gap-1 rounded-lg bg-brand-deep px-3 text-xs font-bold text-white hover:bg-brand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand">
                              <Plus aria-hidden="true" className="size-4" /> افزودن به ویترین
                            </button>}
                        </li>;
                      })}
                    </ul>
                  ) : <p className="mt-1 text-xs leading-6 text-slate-600">کالای مرتبطی در کاتالوگ سلرشو پیدا نشد.</p>}
                </li>
              ))}
            </ul>
          ) : <p className="text-sm text-slate-600">جست‌وجوی بی‌نتیجه‌ای برای فروشگاه ثبت نشده است.</p>}
        </section>
        <SearchDetails title="عبارت‌های پرتکرار" terms={topSearches} empty="هنوز عبارت پرتکراری ثبت نشده است." />
        <SearchDetails title="جست‌وجوهای بی‌نتیجه در فروشگاه مرکزی" terms={unmetCatalog} empty="موردی ثبت نشده است." />
      </> : <SupplierEmptyState icon={<Search />} title="هنوز داده‌ای برای نمایش نیست." description="با اولین جست‌وجوهای مشتریان، گزارش این صفحه به‌روز می‌شود." />)}
    </div>
  );
}
