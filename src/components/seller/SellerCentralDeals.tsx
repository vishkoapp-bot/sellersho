import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { getCentralDealCandidates, getCentralDeals } from '../../utils/centralDeals';

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

  return <div className="space-y-3 pt-2">
    <p className="text-xs leading-6 text-slate-600">از کالاهای موجود با نشان پیشنهاد ویژه یا تخفیف بیشتر از ۱۰٪ انتخاب کنید. این تنظیم قیمت کالا را تغییر نمی‌دهد.</p>
    <div className="grid grid-cols-3 gap-2">
      {([{ id: 'my_niches', label: 'فقط نیچ‌های من' }, { id: 'manual', label: 'انتخاب دستی' }, { id: 'all', label: 'انتخاب همه' }] as const).map((item) =>
        <button key={item.id} type="button" aria-pressed={mode === item.id} onClick={() => { updateCentralDealsFilter(item.id); setSearch(''); setSelectedOnly(false); setLimit(12); }} className={`rounded-xl border px-2 py-3 text-xs font-bold ${mode === item.id ? 'border-rose-500 bg-rose-50 text-rose-700' : 'border-slate-200'}`}>{item.label}</button>)}
    </div>
    <p role="status" className="rounded-xl bg-slate-50 p-3 text-xs">{toPersianDigits(preview.length)} کالا در ویترین نمایش داده می‌شود.</p>
    <label className="block text-xs">جست‌وجوی پیشنهاد سراسری<input aria-label="جست‌وجوی پیشنهاد سراسری" value={search} onChange={(event) => { setSearch(event.target.value); setLimit(12); }} className="mt-2 w-full rounded-xl border p-3" /></label>
    {mode === 'manual' && <div className="flex flex-wrap items-center justify-between gap-3 text-xs">
      <label className="flex items-center gap-2"><input type="checkbox" checked={selectedOnly} onChange={(event) => { setSelectedOnly(event.target.checked); setLimit(12); }} />فقط منتخب‌های سراسری</label>
      <button type="button" onClick={() => updateCentralDealsFilter('manual', [])} className="text-rose-700">پاک‌کردن انتخاب‌ها</button>
    </div>}
    {visible.length === 0 && <p className="rounded-xl border border-dashed p-5 text-xs leading-6">{candidates.length === 0 ? 'در حال حاضر پیشنهاد سراسری موجودی‌دار نداریم.' : search || selectedOnly ? 'کالایی مطابق فیلتر پیدا نشد؛ جست‌وجو یا فیلتر منتخب‌ها را پاک کنید.' : 'پیشنهادی برای نیچ‌های فروشگاه پیدا نشد. نیچ‌های کاتالوگ را تنظیم کنید یا «انتخاب همه» را بزنید.'}</p>}
    <div className="grid grid-cols-2 gap-2">{visible.slice(0, limit).map((product) => <label key={product.id} className={`min-w-0 space-y-2 rounded-xl border p-3 ${mode === 'manual' && ids.includes(product.id) ? 'border-rose-400 bg-rose-50' : 'border-slate-200'}`}>
      <img src={product.image} alt={product.title} loading="lazy" className="h-24 w-full rounded-lg bg-white object-contain" />
      <span className="block text-xs font-bold leading-5">{product.title}</span>
      <span className="block text-xs text-emerald-700">{formatPrice(product.price)} تومان</span>
      {mode === 'manual' && staleIds.has(product.id) && <span className="block w-fit rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-bold text-amber-700 border border-amber-200">دیگر پیشنهاد ویژه نیست — در ویترین نمایش داده نمی‌شود</span>}
      {mode === 'manual' && <span className="flex items-center gap-2 text-xs"><input type="checkbox" aria-label={`پیشنهاد سراسری: ${product.title}`} checked={ids.includes(product.id)} onChange={(event) => updateCentralDealsFilter('manual', event.target.checked ? [...new Set([...ids, product.id])] : ids.filter((id) => id !== product.id))} />نمایش این کالا</span>}
    </label>)}</div>
    {visible.length > limit && <button type="button" onClick={() => setLimit((value) => value + 12)} className="w-full rounded-xl border p-3 text-xs">نمایش کالاهای بیشتر</button>}
    {mode === 'manual' && preview.length === 0 && <p className="text-xs text-rose-700">حداقل یک کالا انتخاب کنید؛ تا آن زمان این بخش در ویترین پنهان است.</p>}
    <p className="text-[11px] leading-6 text-amber-800">تغییرات فوراً در پیش‌نمایش همین بخش اعمال و در سرور ذخیره می‌شوند؛ پس از بارگذاری دوبارهٔ پنل هم باقی می‌مانند.</p>
  </div>;
}