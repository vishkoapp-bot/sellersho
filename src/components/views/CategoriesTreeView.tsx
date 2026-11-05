import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useStore, useActiveStoreCategorySlugs, expandCategorySlugsWithAncestors } from '../../context/StoreContext';
import { ArrowRight, ChevronDown, ChevronLeft, LayoutGrid, Package } from 'lucide-react';
import type { Category } from '../../types';

interface TreeNode {
  cat: Category;
  children: TreeNode[];
}

const buildTree = (categories: Category[]): TreeNode[] => {
  const byId = new Map<string, TreeNode>();
  categories.forEach((c) => byId.set(c.id, { cat: c, children: [] }));
  const roots: TreeNode[] = [];
  categories.forEach((c) => {
    const node = byId.get(c.id)!;
    if (c.parentId && byId.has(c.parentId)) byId.get(c.parentId)!.children.push(node);
    else roots.push(node);
  });
  return roots;
};

// Live tree view (bundle component f3): sticky header with count, card
// grid, recursive indented rows, all roots expanded on first load, and an
// empty state when the tree is empty. The card icon in live is an internal
// category-icon component (Jm) that is not exported from the excluded
// CategoriesBar.tsx, so the card uses the category image or a LayoutGrid
// fallback — the one approximation in this file.
export const CategoriesTreeView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { openCatalogCategory, toPersianDigits, categories, catalogLoading, currentStore } = useStore();
  // While a store context is active, this tree is the STORE's tree:
  // only the categories the store actually sells (ancestors
  // included) are listed, and clicks route through
  // openCatalogCategory, which lands them inside the store. Central
  // browsing (no store context) is untouched. While the store's set
  // loads, a loading row replaces the tree — the full platform tree
  // never flashes.
  const { storeSlug, categorySlugs } = useActiveStoreCategorySlugs();
  const visibleCategories = useMemo(() => {
    if (!storeSlug) return categories;
    if (!categorySlugs) return null;
    const included = expandCategorySlugsWithAncestors(categories, categorySlugs);
    return categories.filter((c) => included.has(c.slug));
  }, [categories, storeSlug, categorySlugs]);
  const roots = useMemo(() => buildTree(visibleCategories ?? []), [visibleCategories]);
  const [expandedSlugs, setExpandedSlugs] = useState<Set<string>>(new Set());
  const initialized = useRef(false);

  useEffect(() => {
    if (!initialized.current && roots.length > 0) {
      initialized.current = true;
      setExpandedSlugs(new Set(roots.map((r) => r.cat.slug)));
    }
  }, [roots]);

  const toggle = (slug: string) => {
    setExpandedSlugs((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) next.delete(slug);
      else next.add(slug);
      return next;
    });
  };

  const renderNode = (node: TreeNode, depth: number): React.ReactNode => {
    const kids = node.children;
    const expanded = expandedSlugs.has(node.cat.slug);
    return (
      <div key={node.cat.id}>
        <div className="flex items-center gap-1 min-w-0" style={{ marginRight: depth * 14 }}>
          {kids.length > 0 ? (
            <button
              type="button"
              onClick={() => toggle(node.cat.slug)}
              aria-expanded={expanded}
              aria-label={`زیردسته‌های ${node.cat.title}`}
              className="shrink-0 p-1 rounded-md hover:bg-slate-100 text-slate-500"
            >
              <ChevronDown className={`w-4 h-4 transition-transform ${expanded ? 'rotate-180' : ''}`} strokeWidth={1.5} />
            </button>
          ) : (
            <span aria-hidden="true" className="w-6 shrink-0 text-center text-[8px] text-slate-300">●</span>
          )}
          <button
            type="button"
            onClick={() => openCatalogCategory(node.cat.slug)}
            className="min-w-0 flex-1 text-right truncate py-1 px-1 rounded-lg text-[11px] lg:text-sm font-medium text-slate-700 hover:text-brand-deep hover:bg-brand-soft/50 transition-colors"
          >
            {node.cat.title}
          </button>
          {kids.length > 0 && (
            <span className="shrink-0 text-[10px] text-slate-400">{toPersianDigits(kids.length)}</span>
          )}
        </div>
        {expanded && kids.length > 0 && (
          <div className="mt-0.5 space-y-0.5">
            {kids.map((ch) => renderNode(ch, depth + 1))}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="flex-1 flex flex-col bg-white select-none pb-20 lg:pb-8">
      <header className="sticky top-0 z-30 lg:static bg-white border-b border-slate-200 px-3.5 lg:px-8 xl:px-10 2xl:px-12 py-2.5 lg:py-3.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button onClick={onBack} className="p-1 rounded-lg hover:bg-slate-100 text-slate-700">
            <ArrowRight className="w-5 h-5" strokeWidth={1.5} />
          </button>
          <div className="flex items-center gap-1.5">
            <LayoutGrid className="w-4 h-4 text-brand-deep" strokeWidth={1.5} />
            <h1 className="text-xs lg:text-lg xl:text-xl font-bold text-slate-900">{storeSlug ? `دسته‌بندی‌های ${currentStore.name}` : 'دسته‌بندی‌های کاتالوگ مرکزی'}</h1>
          </div>
        </div>
        <span className="text-[10px] lg:text-xs text-slate-400">
          {storeSlug && !visibleCategories ? 'در حال بارگذاری' : catalogLoading ? 'در حال بارگذاری' : `${toPersianDigits((visibleCategories ?? categories).length)} دسته‌بندی`}
        </span>
      </header>

      {storeSlug && !visibleCategories ? (
        <div className="p-6 lg:py-16 text-center space-y-3">
          <p className="text-[11px] lg:text-sm text-slate-500">در حال بارگذاری دسته‌بندی‌های فروشگاه…</p>
        </div>
      ) : !catalogLoading && roots.length === 0 ? (
        <div className="p-6 lg:py-16 text-center space-y-3">
          <div className="w-12 h-12 rounded-full bg-slate-100 text-slate-400 flex items-center justify-center mx-auto">
            <Package className="w-6 h-6" strokeWidth={1.5} />
          </div>
          <h3 className="text-xs lg:text-base font-bold text-slate-800">دسته‌بندی‌ای ثبت نشده است</h3>
          <p className="text-[11px] lg:text-sm text-slate-500">به‌محض ثبت دسته‌بندی، اینجا نمایش داده می‌شود.</p>
        </div>
      ) : (
        <div className="p-3.5 space-y-3 lg:px-8 lg:py-6 xl:px-10 2xl:px-12 sm:grid sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-4 lg:gap-6 lg:space-y-0">
          {roots.map((root) => {
            const expanded = expandedSlugs.has(root.cat.slug);
            return (
              <div key={root.cat.id} className="bg-white rounded-2xl border border-slate-200/90 p-3 lg:p-5 shadow-2xs space-y-2.5">
                <div className="flex items-center justify-between gap-2">
                  <button
                    type="button"
                    onClick={() => openCatalogCategory(root.cat.slug)}
                    className="flex items-center gap-3 min-w-0 flex-1 text-right group"
                  >
                    <div className="shrink-0 w-12 h-12 lg:w-16 lg:h-16 flex items-center justify-center group-hover:scale-105 transition-transform">
                      {root.cat.image ? (
                        <img src={root.cat.image} alt={root.cat.title} className="w-full h-full object-contain" />
                      ) : (
                        <LayoutGrid className="w-8 h-8 text-slate-300" strokeWidth={1.5} />
                      )}
                    </div>
                    <h3 className="text-xs lg:text-base font-bold text-slate-900 group-hover:text-brand-deep transition-colors truncate">{root.cat.title}</h3>
                  </button>
                  {root.children.length > 0 ? (
                    <button
                      type="button"
                      onClick={() => toggle(root.cat.slug)}
                      aria-expanded={expanded}
                      aria-label={`زیردسته‌های ${root.cat.title}`}
                      className="shrink-0 p-1.5 rounded-lg hover:bg-slate-100 text-slate-500"
                    >
                      <ChevronDown className={`w-4 h-4 transition-transform ${expanded ? 'rotate-180' : ''}`} strokeWidth={1.5} />
                    </button>
                  ) : (
                    <span className="shrink-0 flex items-center gap-1 text-[11px] font-bold text-brand">
                      <span>مشاهده</span>
                      <ChevronLeft className="w-4 h-4" strokeWidth={1.5} />
                    </span>
                  )}
                </div>
                {expanded && root.children.length > 0 && (
                  <div className="pt-2 border-t border-slate-100 space-y-0.5">
                    {root.children.map((ch) => renderNode(ch, 1))}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
};
