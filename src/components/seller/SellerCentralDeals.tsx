import React, { useState } from 'react';
import { Package, Search } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { getCentralDealCandidates, getCentralDeals } from '../../utils/centralDeals';
import { SupplierEmptyState } from '../supplier/SupplierResourceState';

const filterModes = [
  { id: 'my_niches', label: 'موضوع‌های فروشگاه' },
  { id: 'manual', label: 'انتخاب دستی' },
  { id: 'all', label: 'همه پیشنهادها' },
] as const;
const actionClass = 'min-h-11 rounded-xl border px-3 py-2 text-sm font-bold hover:bg-brand-bg focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60';
const neutralAction = ' border-slate-200 bg-white text-slate-700';

export function SellerCentralDeals() {
  const { currentStore, products, updateCentralDealsFilter, formatPrice, toPersianDigits } = useStore();
  const [search, setSearch] = useState('');
  const [selectedOnly, setSelectedOnly] = useState(false);
  const [limit, setLimit] = useState(12);
  const mode = currentStore.centralDealsFilter || 'my_niches';
  const ids = currentStore.centralDealsProductIds || [];
  const candidates = getCentralDealCandidates(products);
  const preview = getCentralDeals(products, currentStore);
  // Live (manual mode): the list is the current candidates PLUS previously
  // picked products that are no longer central deals; those carry a stale
  // badge and are not counted in the preview status line.
  const staleProducts = products.filter((product) =>
    ids.includes(product.id) && !candidates.some((candidate) => candidate.id === product.id));
  const staleIds = new Set(staleProducts.map((product) => product.id));
  const visible = (mode === 'manual' ? [...candidates, ...staleProducts] : preview).filter((product) =>
    product.title.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()) &&
    (mode !== 'manual' || !selectedOnly || ids.includes(product.id)));

  return (
    <div className="space-y-4 pt-2">
      <fieldset className="min-w-0 space-y-2">
        <legend className="text-sm font-extrabold text-[#261b41]">پیشنهادهای قابل نمایش در ویترین</legend>
        <p className="text-sm leading-6 text-slate-600">کالاهای موجود با نشان پیشنهاد ویژه یا بیش از ۱۰٪ تخفیف را انتخاب کن. انتخاب‌ها خودکار ذخیره می‌شوند.</p>
        <div className="grid gap-2 sm:grid-cols-3">
          {filterModes.map(item => (
            <button key={item.id} type="button" aria-pressed={mode === item.id}
              onClick={() => { updateCentralDealsFilter(item.id); setSearch(''); setSelectedOnly(false); setLimit(12); }}
              className={actionClass + (mode === item.id ? ' border-brand-border bg-brand-soft text-brand-deep' : neutralAction)}>
              {item.label}
            </button>
          ))}
        </div>
      </fieldset>
      <p role="status" className="rounded-xl border border-brand-border bg-brand-bg p-3 text-sm leading-6 text-brand-deep">
        {mode === 'manual' && preview.length === 0
          ? 'با انتخاب دست‌کم یک پیشنهاد موجود، این بخش در ویترین نمایش داده می‌شود.'
          : `${toPersianDigits(preview.length)} کالا در ویترین نمایش داده می‌شود.`}
      </p>
      <div className="space-y-1.5">
        <label htmlFor="seller-deal-search" className="block text-sm font-bold text-slate-800">جست‌وجوی پیشنهاد سراسری</label>
        <div className="flex min-h-11 items-center gap-2 rounded-xl border border-slate-200 bg-slate-50 px-3 focus-within:ring-2 focus-within:ring-brand">
          <Search aria-hidden="true" className="size-4 shrink-0 text-slate-600" />
          <input id="seller-deal-search" type="search" aria-label="جست‌وجوی پیشنهاد سراسری" value={search} onChange={event => { setSearch(event.target.value); setLimit(12); }}
            className="min-h-11 min-w-0 flex-1 bg-transparent text-sm text-slate-800 focus:outline-hidden" />
        </div>
      </div>
      {mode === 'manual' && <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
        <label className="flex min-h-11 cursor-pointer items-center gap-2 text-slate-700">
          <input type="checkbox" checked={selectedOnly} onChange={event => { setSelectedOnly(event.target.checked); setLimit(12); }}
            className="size-4 shrink-0 accent-brand-deep focus-visible:ring-2 focus-visible:ring-brand" />
          فقط کالاهای انتخاب‌شده
        </label>
        <button type="button" disabled={ids.length === 0} onClick={() => updateCentralDealsFilter('manual', [])} className={actionClass + neutralAction}>پاک‌کردن انتخاب‌ها</button>
      </div>}
      {visible.length === 0 ? (
        <SupplierEmptyState icon={<Package />} title="پیشنهادی پیدا نشد"
          description={search || selectedOnly ? 'جست‌وجو یا فیلتر کالاهای انتخاب‌شده را پاک کن.' : candidates.length === 0 ? 'در حال حاضر پیشنهاد سراسری موجودی‌دار نداریم.' : 'موضوع‌های فروشگاه را تغییر بده یا «همه پیشنهادها» را انتخاب کن.'} />
      ) : (
        <ul className="grid grid-cols-2 gap-2 sm:gap-3 md:grid-cols-3">
          {visible.slice(0, limit).map(product => {
            const Card = mode === 'manual' ? 'label' : 'div';
            return <li key={product.id} className="min-w-0">
            <Card className={`block h-full space-y-2 rounded-2xl border p-2.5 sm:p-3 ${mode === 'manual' ? 'cursor-pointer focus-within:ring-2 focus-within:ring-brand' : ''} ${mode === 'manual' && ids.includes(product.id) ? 'border-brand-border bg-brand-bg' : 'border-slate-200 bg-white'}`}>
              <img src={product.image} alt="" loading="lazy" className="h-24 w-full rounded-xl bg-slate-50 object-contain" />
              <span className="block break-words text-sm font-extrabold leading-6 text-[#261b41]">{product.title}</span>
              <span className="block text-sm font-bold tabular-nums text-emerald-800">{formatPrice(product.price)} تومان</span>
              {mode === 'manual' && staleIds.has(product.id) && <span className="block rounded-lg border border-amber-200 bg-amber-50 px-2 py-1 text-xs leading-5 text-amber-900">این کالا دیگر شرایط پیشنهاد ویژه را ندارد و در این بخش نمایش داده نمی‌شود.</span>}
              {mode === 'manual' && <span className="flex min-h-11 items-center gap-2 border-t border-slate-100 pt-1 text-xs font-bold text-slate-700">
                <input type="checkbox" aria-label={`پیشنهاد سراسری: ${product.title}`} checked={ids.includes(product.id)}
                  className="size-4 shrink-0 accent-brand-deep focus-visible:ring-2 focus-visible:ring-brand"
                  onChange={event => updateCentralDealsFilter('manual', event.target.checked ? [...new Set([...ids, product.id])] : ids.filter(id => id !== product.id))} />
                نمایش این کالا
              </span>}
            </Card>
            </li>;
          })}
        </ul>
      )}
      {visible.length > limit && <button type="button" onClick={() => setLimit(value => value + 12)} className={actionClass + neutralAction}>نمایش کالاهای بیشتر</button>}
    </div>
  );
}
