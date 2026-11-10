import React, { useState, useEffect, useRef } from 'react';
import { catalogApi, CategoryDto } from '../../services/catalogApi';
import { Search, Loader2, X, ChevronDown } from 'lucide-react';

// Live category picker (Rl): search combobox over the category tree.
// Roots come from getCategories (parentId == null); search hits get their
// path by walking parentId through the full list.
export interface CategorySelection {
  slug: string;
  title: string;
  path: { id: string; title: string; slug: string }[];
  pathDisplay: string;
}

interface PickerOption extends CategorySelection {
  key: string;
}

const buildPath = (cat: CategoryDto, byId: Map<string, CategoryDto>): CategorySelection => {
  const chain: { id: string; title: string; slug: string }[] = [];
  let cur: CategoryDto | undefined = cat;
  const seen = new Set<string>();
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id);
    chain.unshift({ id: cur.id, title: cur.title, slug: cur.slug });
    cur = cur.parentId ? byId.get(cur.parentId) : undefined;
  }
  return { slug: cat.slug, title: cat.title, path: chain, pathDisplay: chain.map((c) => c.title).join(' / ') };
};

export const SupplierCategoryPicker: React.FC<{ value: CategorySelection | null; onSelect: (cat: CategorySelection | null) => void }> = ({ value, onSelect }) => {
  const [text, setText] = useState(value?.title ?? '');
  const [open, setOpen] = useState(false);
  const [roots, setRoots] = useState<PickerOption[]>([]);
  const [rootsLoaded, setRootsLoaded] = useState(false);
  const [results, setResults] = useState<PickerOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);
  const [allById, setAllById] = useState<Map<string, CategoryDto>>(new Map());
  const boxRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const seqRef = useRef(0);

  useEffect(() => { setText(value?.title ?? ''); }, [value]);

  useEffect(() => {
    if (!open) return;
    const handler = (event: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open ]);

  const loadRoots = async () => {
    if (rootsLoaded) return;
    setLoading(true);
    setError(null);
    try {
      const all = await catalogApi.getCategories();
      const byId = new Map(all.map((c) => [c.id, c]));
      setAllById(byId);
      setRoots(all.filter((c) => c.parentId == null).map((c) => ({ ...buildPath(c, byId), key: `root-${c.slug}` })));
      setRootsLoaded(true);
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : 'بارگذاری دسته‌ها انجام نشد.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    const q = text.trim();
    if (q === '') {
      setResults(roots);
      setActiveIndex(0);
      setLoading(false);
      setError(null);
      return;
    }
    setLoading(true);
    setError(null);
    const seq = ++seqRef.current;
    const timer = window.setTimeout(async () => {
      try {
        let byId = allById;
        if (byId.size === 0) {
          const all = await catalogApi.getCategories();
          byId = new Map(all.map((c) => [c.id, c]));
          setAllById(byId);
        }
        const hits = await catalogApi.searchCategories(q);
        if (seqRef.current !== seq) return;
        setResults(hits.slice(0, 10).map((c) => ({ ...buildPath(c, byId), key: `hit-${c.slug}` })));
        setActiveIndex(0);
      } catch (cause: unknown) {
        if (seqRef.current !== seq) return;
        setResults([]);
        setError(cause instanceof Error ? cause.message : 'جستجوی دسته‌ها انجام نشد.');
      } finally {
        if (seqRef.current === seq) setLoading(false);
      }
    }, 250);
    return () => window.clearTimeout(timer);
  }, [text, roots, allById]);

  const shown = text.trim() === '' ? roots : results;
  const activeId = open && shown.length > 0 ? `category-picker-option-${shown[Math.min(activeIndex, shown.length - 1)].key}` : undefined;

  const pick = (opt: PickerOption) => {
    const children = [...allById.values()].filter(c => c.parentId === opt.path[opt.path.length - 1]?.id);
    if (children.length) {
      setRoots(children.map(c => ({ ...buildPath(c, allById), key: `child-${c.slug}` })));
      setText(''); setResults([]); setActiveIndex(0); setOpen(true); return;
    }
    onSelect({ slug: opt.slug, title: opt.title, path: opt.path, pathDisplay: opt.pathDisplay });
    setText(opt.title);
    setOpen(false);
    inputRef.current?.blur();
  };

  const clear = () => {
    onSelect(null);
    setText('');
    setResults([]);
    setActiveIndex(0);
    setError(null);
    inputRef.current?.focus();
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      if (!open) { setOpen(true); return; }
      if (shown.length > 0) setActiveIndex((i) => (i + 1) % shown.length);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      if (open && shown.length > 0) setActiveIndex((i) => (i - 1 + shown.length) % shown.length);
    } else if (event.key === 'Enter') {
      if (open && shown.length > 0) {
        event.preventDefault();
        pick(shown[Math.min(activeIndex, shown.length - 1)]);
      }
    } else if (event.key === 'Escape' && open) {
      event.preventDefault();
      setOpen(false);
    }
  };

  const onFocus = () => {
    setOpen(true);
    if (text.trim() === '') void loadRoots();
  };

  return (
    <div ref={boxRef} className="relative">
      <div className="relative">
        <Search className="w-3.5 h-3.5 absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none"  strokeWidth={1.5}/>
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-expanded={open}
          aria-controls="category-picker-listbox"
          aria-activedescendant={activeId}
          aria-autocomplete="list"
          aria-label="دسته‌بندی کالا"
          placeholder="دسته را جستجو کن… (مثلاً: گوشی موبایل)"
          value={text}
          onChange={(event) => { const v = event.target.value; if (value && v !== value.title) onSelect(null); setText(v); setOpen(true); }}
          onFocus={onFocus}
          onKeyDown={onKeyDown}
          autoComplete="off"
          className="w-full text-xs p-2.5 pr-8 pl-8 rounded-xl bg-slate-50 border border-slate-200 outline-hidden placeholder:text-slate-400"
        />
        {loading ? (
          <Loader2 className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-400 animate-spin"  strokeWidth={1.5}/>
        ) : text !== '' ? (
          <button type="button" onClick={clear} aria-label="پاک‌کردن دسته‌بندی انتخاب‌شده" className="absolute left-2 top-1/2 -translate-y-1/2 p-1 rounded-full text-slate-400 hover:text-slate-600 hover:bg-slate-200">
            <X className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          </button>
        ) : (
          <ChevronDown className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none"  strokeWidth={1.5}/>
        )}
      </div>
      {value && (
        <p className="text-[10px] leading-5 text-slate-600 mt-1.5 flex items-start gap-1">
          <span className="font-bold shrink-0">مسیر انتخاب‌شده:</span>
          <span className="break-words">{value.pathDisplay}</span>
        </p>
      )}
      {open && (
        <ul id="category-picker-listbox" role="listbox" aria-label="نتایج جستجوی دسته‌بندی" className="absolute z-20 mt-1 w-full max-h-64 overflow-y-auto bg-white border border-slate-200 rounded-xl shadow-lg py-1">
          {text.trim() === '' && !rootsLoaded && !loading && !error && (
            <li className="px-3 py-2.5 text-[11px] text-slate-500">در حال بارگذاری دسته‌های اصلی…</li>
          )}
          {text.trim() === '' && rootsLoaded && roots.length === 0 && (
            <li className="px-3 py-2.5 text-[11px] text-slate-500">هنوز دسته‌بندی‌ای ثبت نشده است.</li>
          )}
          {text.trim() !== '' && !loading && !error && shown.length === 0 && (
            <li className="px-3 py-2.5 text-[11px] text-slate-500">دسته‌ای با نام «{text.trim()}» پیدا نشد؛ عبارت دیگری را امتحان کن.</li>
          )}
          {error && (
            <li className="px-3 py-2.5 text-[11px] text-rose-600">{error} دوباره تلاش کن.</li>
          )}
          {shown.map((opt, i) => (
            <li
              key={opt.key}
              id={`category-picker-option-${opt.key}`}
              role="option"
              aria-selected={i === activeIndex}
              onMouseDown={(event) => { event.preventDefault(); pick(opt); }}
              onMouseEnter={() => setActiveIndex(i)}
              className={`px-3 py-2 cursor-pointer ${i === activeIndex ? 'bg-violet-50' : ''}`}
            >
              <span className="block text-xs font-bold text-slate-800">{opt.title}</span>
              {opt.path.length > 1 && (
                <span className="block text-[10px] leading-4 text-slate-400 mt-0.5 break-words">{opt.pathDisplay}</span>
              )}
            </li>
          ))}
          {loading && (
            <li className="px-3 py-2.5 text-[11px] text-slate-500 flex items-center gap-1.5">
              <Loader2 className="w-3.5 h-3.5 animate-spin"  strokeWidth={1.5}/>
              در حال جستجو…
            </li>
          )}
        </ul>
      )}
    </div>
  );
};
