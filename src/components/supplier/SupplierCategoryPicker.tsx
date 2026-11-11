import React, { useEffect, useState } from 'react';
import { catalogApi, CategoryDto } from '../../services/catalogApi';
export interface CategorySelection {
  isLeaf?: boolean;
  slug: string;
  title: string;
  path: { id: string; title: string; slug: string }[];
  pathDisplay: string;
}


export const SupplierCategoryPicker: React.FC<{ value: CategorySelection | null; onSelect: (cat: CategorySelection | null) => void }> = ({ value, onSelect }) => {
  const [categories, setCategories] = useState<CategoryDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let active = true; setLoading(true); setError(false);
    catalogApi.getCategories().then(data => { if (active) setCategories(data); }).catch(() => { if (active) setError(true); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [retry]);
  const selectedPath = value?.path ?? [];
  const levels: CategoryDto[][] = [];
  let parent: string | null = null;
  const visited = new Set<string>();
  for (let depth = 0; depth <= selectedPath.length; depth++) {
    const options = categories.filter(c => (c.parentId ?? null) === parent).sort((a,b) => a.title.localeCompare(b.title, 'fa'));
    if (!options.length) break;
    levels.push(options);
    const selected = options.find(c => c.id === selectedPath[depth]?.id);
    if (!selected || visited.has(selected.id)) break;
    visited.add(selected.id); parent = selected.id;
  }
  const pick = (depth: number, id: string) => {
    const item = categories.find(c => c.id === id);
    const path = selectedPath.slice(0, depth);
    if (item) path.push({id: item.id, title: item.title, slug: item.slug});
    const last = path[path.length - 1];
    onSelect(last ? {slug: last.slug, title: last.title, path, pathDisplay: path.map(c => c.title).join(' / '), isLeaf: !categories.some(c => c.parentId === last.id)} : null);
  };
  return <div className="rounded-xl border border-slate-200 bg-slate-50 p-3 space-y-3" dir="rtl">
    {loading && <p className="text-xs" role="status">در حال بارگذاری دسته‌ها…</p>}
    {error && <p role="alert" className="text-xs text-rose-700">دریافت دسته‌ها ناموفق بود. <button type="button" onClick={() => setRetry(n => n+1)}>تلاش مجدد</button></p>}
    {!loading && !error && levels.map((options, depth) => <label key={depth} className="block text-xs font-bold space-y-1">
      <span>{depth === 0 ? 'دستهٔ اصلی' : `زیردستهٔ ${depth}`}</span>
      <select aria-label={depth === 0 ? 'دستهٔ اصلی' : `زیردستهٔ ${depth}`} value={selectedPath[depth]?.id ?? ''} onChange={e => pick(depth, e.target.value)} className="w-full min-h-11 rounded-xl border border-slate-200 bg-white p-2.5 text-xs">
        <option value="">انتخاب کنید</option>
        {options.map(c => <option key={c.id} value={c.id}>{c.title}</option>)}
      </select>
    </label>)}
    {value && <div role="status" className="rounded-lg bg-white p-2 text-xs leading-6"><strong>مسیر انتخاب‌شده: </strong>{value.pathDisplay}<p className={value.isLeaf ? 'text-emerald-700' : 'text-amber-700'}>{value.isLeaf ? 'دسته‌بندی کامل شد؛ ویژگی‌های همین دسته در پایین نمایش داده می‌شوند.' : 'زیردسته را انتخاب کنید تا به نوع دقیق کالا برسید.'}</p></div>}
  </div>;
};
