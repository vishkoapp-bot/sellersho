import React from 'react';
import { useStore } from '../../context/StoreContext';
import { mockCategories } from '../../data/mockData';
import { storefrontProducts } from './templates/templateTypes';

export const SellerStorefrontCategories: React.FC = () => {
  const { currentStore, products, selectedCategory, setSelectedCategory } = useStore();

  // Enforce Chapter 5 + ISO-A (user-ordered storefront separation):
  // a tile renders only for a category that is BOTH active for this
  // store AND actually present among this store's own published
  // products. Previously, when activeCategories was undefined the
  // section showed all 19 central mock categories regardless of what
  // the store sells, and when it was set it still showed tiles for
  // categories with zero store products (clicking one then filtered
  // the grid to nothing). The template path already derives its
  // categories from the store's own products (StoreSections
  // groupProductsByCategory); this legacy section now does the same
  // within the mock-tile catalogue it renders. NOTE: mock slugs only
  // partially overlap backend product categories (e.g. backend
  // 'gold-silver' has no mock 'gold' tile), so a store product in a
  // non-mock category simply gets no tile here — it still appears in
  // the template path and in the product grid. When no tile qualifies
  // the section hides itself instead of rendering a bare heading.
  const items = storefrontProducts(currentStore, products);
  const activeCats = currentStore.ownerType === 'central'
    ? mockCategories
    : mockCategories.filter((cat) =>
        (currentStore.activeCategories || []).includes(cat.slug) &&
        items.some((p) => p.category === cat.slug)
      );
  if (activeCats.length === 0) return null;

  return (
    <section className="bg-white py-3 border-b border-slate-100">
      <div className="px-4 mb-2 flex items-center justify-between">
        <h3 className="text-xs font-bold text-slate-800">دسته‌بندی‌های این فروشگاه</h3>
        {selectedCategory !== 'all' && (
          <button
            onClick={() => setSelectedCategory('all')}
            className="text-[11px] text-[#007dfa] font-bold"
          >
            همه
          </button>
        )}
      </div>

      <div className="flex items-start gap-3.5 overflow-x-auto no-scrollbar px-4 pt-1">
        {activeCats.map((cat) => {
          const isSelected = selectedCategory === cat.slug;
          return (
            <div
              key={cat.id}
              onClick={() => setSelectedCategory(isSelected ? 'all' : cat.slug)}
              className={`flex flex-col items-center shrink-0 w-[64px] cursor-pointer group select-none transition-transform duration-150 ${
                isSelected ? 'scale-105' : 'hover:opacity-90 active:scale-95'
              }`}
            >
              <div className={`w-[54px] h-[54px] rounded-2xl flex items-center justify-center p-1.5 transition-all ${
                isSelected 
                  ? 'bg-blue-50 ring-2 ring-[#007dfa] shadow-xs' 
                  : 'bg-slate-50 border border-slate-100'
              }`}>
                <img
                  src={cat.image}
                  alt={cat.title}
                  className="w-full h-full object-contain"
                  loading="lazy"
                />
              </div>

              <span className={`text-[11px] font-medium text-center mt-1.5 truncate w-full block transition-colors ${
                isSelected ? 'text-[#007dfa] font-bold' : 'text-slate-700'
              }`}>
                {cat.title}
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
};
