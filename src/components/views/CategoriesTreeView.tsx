import React, { useMemo, useRef, useState } from 'react';
import { useStore, useActiveStoreCategorySlugs, expandCategorySlugsWithAncestors } from '../../context/StoreContext';
import { ArrowRight, ChevronDown, ChevronLeft, LayoutGrid, Package, Search, X } from 'lucide-react';
import type { Category } from '../../types';
import { normalizePersianText, pathForView } from '../../utils/routing';
import { followStoreLink } from '../../utils/followStoreLink';
import { FallbackImage } from '../common/FallbackImage';

interface TreeNode {
  cat: Category;
  children: TreeNode[];
}

const buildTree = (categories: Category[]): TreeNode[] => {
  const byId = new Map(categories.map(cat => [cat.id, { cat, children: [] } as TreeNode]));
  const roots: TreeNode[] = [];
  for (const cat of categories) {
    const node = byId.get(cat.id)!;
    const parent = cat.parentId ? byId.get(cat.parentId) : undefined;
    if (parent) parent.children.push(node);
    else roots.push(node);
  }
  return roots;
};

function matchTree(node: TreeNode, query: string): TreeNode | null {
  if (normalizePersianText(node.cat.title).toLowerCase().includes(query)) return node;
  const children = node.children.map(child => matchTree(child, query)).filter(child => child !== null);
  return children.length ? { ...node, children } : null;
}

const action = 'inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl px-3 text-sm font-bold text-[var(--shop-forest,#164638)] hover:bg-[var(--shop-soft,#e5efe6)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--shop-forest,#164638)]';

export const CategoriesTreeView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { openCatalogCategory, toPersianDigits, categories, catalogLoading, currentStore } = useStore();
  const { storeSlug, categorySlugs } = useActiveStoreCategorySlugs();
  const searchInput = useRef<HTMLInputElement>(null);
  const [search, setSearch] = useState('');
  const [expandedSlugs, setExpandedSlugs] = useState<Set<string>>(new Set());

  const loaded = !storeSlug || categorySlugs !== null;
  const roots = useMemo(() => {
    if (!storeSlug) return buildTree(categories);
    if (!categorySlugs) return [];
    const included = expandCategorySlugsWithAncestors(categories, categorySlugs);
    return buildTree(categories.filter(category => included.has(category.slug)));
  }, [categories, storeSlug, categorySlugs]);
  const query = normalizePersianText(search.trim()).toLowerCase();
  const displayRoots = useMemo(() => query
    ? roots.map(node => matchTree(node, query)).filter(node => node !== null)
    : roots, [roots, query]);

  const branchExpanded = (slug: string) => expandedSlugs.has(slug) !== Boolean(query);
  const toggle = (slug: string) => setExpandedSlugs(previous => {
    const next = new Set(previous);
    if (next.has(slug)) next.delete(slug);
    else next.add(slug);
    return next;
  });
  const updateSearch = (value: string) => {
    setSearch(value);
    setExpandedSlugs(new Set());
  };
  const categoryLink = (cat: Category, content: React.ReactNode, className: string) => storeSlug
    ? <button type="button" onClick={() => openCatalogCategory(cat.slug)} className={className}>{content}</button>
    : <a href={pathForView('catalog', { categorySlug: cat.slug })}
      onClick={event => followStoreLink(event, () => openCatalogCategory(cat.slug))} className={className}>{content}</a>;

  const expansionButton = (node: TreeNode) => {
    const expanded = branchExpanded(node.cat.slug);
    return (
      <button type="button" onClick={() => toggle(node.cat.slug)}
        aria-expanded={expanded} aria-label={`زیردسته‌های ${node.cat.title}`} className={`${action} size-11 shrink-0 border border-[var(--shop-line,#d5e0d7)] bg-white px-0`}>
        <ChevronDown size={18} aria-hidden className={expanded ? 'rotate-180' : ''} />
      </button>
    );
  };

  const renderNode = (node: TreeNode): React.ReactNode => (
    <li key={node.cat.id}>
      <div className="flex min-w-0 items-center gap-1.5">
        {categoryLink(node.cat, <><span className="wrap-anywhere">{node.cat.title}</span><ChevronLeft size={15} aria-hidden className="ms-auto shrink-0" /></>,
          `${action} min-w-0 flex-1 justify-start text-start font-semibold`)}
        {node.children.length > 0 && expansionButton(node)}
      </div>
      {branchExpanded(node.cat.slug) && node.children.length > 0 && (
        <ul className="ms-2 border-s border-[var(--shop-line,#dce5d9)] ps-2">{node.children.map(renderNode)}</ul>
      )}
    </li>
  );

  const loading = !loaded || (catalogLoading && roots.length === 0);
  return (
    <main id="main-content" tabIndex={-1} dir="rtl" className="flex-1 bg-[var(--shop-bg,#f8f6ef)] pb-12 text-[var(--shop-ink,#172d26)]">
      <header className="central-list-heading">
        <div className="central-wrap central-list-heading-inner">
          <button type="button" onClick={onBack} className={`${action} mb-2 px-0`}><ArrowRight size={17} aria-hidden /> خانه</button>
          <div className="grid items-end gap-4 sm:grid-cols-[1fr_minmax(250px,360px)]">
            <div>
              <h1 className="central-page-title">{storeSlug ? `دسته‌بندی‌های ${currentStore.name}` : 'دسته‌بندی کالاها'}</h1>
              {!storeSlug && (
                <a href={pathForView('catalog')} onClick={event => followStoreLink(event, () => openCatalogCategory('all'))} className={`${action} mt-1 px-0`}>مشاهدهٔ همهٔ کالاها <ChevronLeft size={16} aria-hidden /></a>
              )}
            </div>
            {roots.length > 0 && (
              <div className="flex min-h-12 items-center gap-2 rounded-xl border border-[var(--shop-line,#ccd9cd)] bg-white ps-3 focus-within:outline-2 focus-within:outline-[var(--shop-forest,#164638)]">
                <Search size={18} className="shrink-0 text-[var(--shop-muted,#506357)]" aria-hidden />
                <label className="sr-only" htmlFor="category-search">جستجو در دسته‌بندی‌ها</label>
                <input id="category-search" name="category-search" type="search" autoComplete="off" ref={searchInput} value={search} onChange={event => updateSearch(event.target.value)}
                  placeholder="نام دسته…" className="min-h-12 min-w-0 flex-1 bg-transparent pe-3 text-base outline-hidden [&::-webkit-search-cancel-button]:hidden" />
                {search && <button type="button" aria-label="پاک کردن جستجوی دسته‌ها" className={`${action} px-0 aspect-square`}
                  onClick={() => { updateSearch(''); searchInput.current?.focus(); }}><X size={16} aria-hidden /></button>}
              </div>
            )}
          </div>
        </div>
      </header>

      <div className="central-wrap pt-5 lg:pt-8">
        {loading ? (
          <p role="status" className="rounded-2xl border border-[var(--shop-line,#dce5d9)] bg-white px-5 py-10 text-center text-sm text-[var(--shop-muted,#506357)]">در حال بارگذاری دسته‌بندی‌ها…</p>
        ) : displayRoots.length === 0 ? (
          <div className="rounded-2xl border border-[var(--shop-line,#dce5d9)] bg-white px-4 py-12 text-center">
            <div className="mx-auto mb-3 grid size-14 place-items-center rounded-2xl bg-[var(--shop-soft,#edf4ee)] text-[var(--shop-forest,#164638)]"><Package size={26} aria-hidden /></div>
            <h2 className="text-lg font-black">{query ? 'دسته‌ای پیدا نشد' : 'دسته‌بندی‌ای ثبت نشده است'}</h2>
            {query && <button type="button" onClick={() => { updateSearch(''); searchInput.current?.focus(); }} className={`${action} mt-3 border border-[var(--shop-line,#d5e0d7)]`}>مشاهدهٔ همهٔ دسته‌بندی‌ها</button>}
          </div>
        ) : (
          <>
            <p role="status" className="mb-3 text-sm font-medium text-[var(--shop-muted,#506357)]">{toPersianDigits(displayRoots.length)} {query ? 'دستهٔ اصلی دارای نتیجه' : 'دستهٔ اصلی'}</p>
            <ul className="grid items-start gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:gap-4">
              {displayRoots.map(node => (
                <li key={node.cat.id} className="central-directory-card min-w-0 rounded-2xl border border-[var(--shop-line,#dce5d9)] bg-white p-3 sm:p-4">
                  <div className="flex items-center gap-2">
                    {categoryLink(node.cat, (
                      <>
                        <span className="grid size-14 shrink-0 place-items-center overflow-hidden rounded-xl bg-[var(--shop-soft,#edf4ee)] text-[var(--shop-forest,#164638)]">
                          <FallbackImage srcs={node.cat.image ? [node.cat.image] : []} alt="" loading="lazy" width={56} height={56} className="size-full object-contain"
                            fallback={<LayoutGrid size={23} aria-hidden />} />
                        </span>
                        <span className="min-w-0">
                          <span className="block wrap-anywhere text-base font-black">{node.cat.title}</span>
                          {node.children.length > 0 && <span className="mt-0.5 block text-xs font-medium text-[var(--shop-muted,#506357)]">{toPersianDigits(node.children.length)} زیردسته</span>}
                        </span>
                        {node.children.length === 0 && <ChevronLeft size={17} aria-hidden className="ms-auto shrink-0" />}
                      </>
                    ), `${action} min-w-0 flex-1 justify-start gap-3 px-1 py-1 text-start`)}
                    {node.children.length > 0 && expansionButton(node)}
                  </div>
                  {branchExpanded(node.cat.slug) && node.children.length > 0 && (
                    <ul className="mt-3 border-t border-[var(--shop-line,#e1e8de)] pt-2">{node.children.map(renderNode)}</ul>
                  )}
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </main>
  );
};
