import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { isProductPublishedInStore } from '../../utils/storeAssortment';
import { Product } from '../../types';
import { ProductCard } from '../ProductCard';
import { SlidersHorizontal, Package } from 'lucide-react';

interface SellerStorefrontProductGridProps {
  // The list to render. SellerStorefrontView passes its store-scoped
  // list so this legacy grid shows STORE prices (the context catalogue
  // is central-priced while the order path charges store prices).
  // When omitted, the context catalogue is used, exactly as before.
  products?: Product[];
  // PERF-01: the storefront pages on demand; the grid renders the
  // loaded pages plus this show-more button while more remain.
  hasMore?: boolean;
  onLoadMore?: () => void;
  loadingMore?: boolean;
}

export const SellerStorefrontProductGrid: React.FC<SellerStorefrontProductGridProps> = ({ products: productsProp, hasMore, onLoadMore, loadingMore }) => {
  const { products: contextProducts, currentStore, selectedCategory, searchQuery, toPersianDigits } = useStore();
  const products = productsProp ?? contextProducts;
  const [sortBy, setSortBy] = useState<'popular' | 'cheapest' | 'expensive' | 'discount'>('popular');

  let filtered = products.filter((p) => {
    // Strictly enforce Chapter 5: Only display products active in this dropshipper's storefront.
    // ISO-A: a dropshipper with no recorded activeProductIds publishes
    // nothing, so it shows nothing - NOT the full central catalogue
    // (the preview leak class). The central store keeps the old
    // no-restriction fallback (it records [] by design).
    const isProductActive = isProductPublishedInStore(currentStore, p.id);
    const matchesCategory = selectedCategory === 'all' || p.category === selectedCategory;
    const matchesSearch = !searchQuery || 
      p.title.includes(searchQuery) || 
      p.description.includes(searchQuery);
    return isProductActive && matchesCategory && matchesSearch;
  });

  filtered = [...filtered].sort((a, b) => {
    if (sortBy === 'cheapest') return a.price - b.price;
    if (sortBy === 'expensive') return b.price - a.price;
    if (sortBy === 'discount') return b.discountPercent - a.discountPercent;
    return (b.salesCount || 0) - (a.salesCount || 0);
  });

  return (
    <section className="px-3.5 py-3">
      {/* Header and Filter Pills */}
      <div className="flex flex-col gap-2 mb-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5">
            <Package className="w-4 h-4 text-blue-600"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">
              ویترین محصولات فروشگاه
            </h3>
            <span className="text-[10px] text-slate-400 font-bold">
              ({toPersianDigits(filtered.length)} کالا)
            </span>
          </div>

          <span className="text-[9px] text-emerald-600 bg-emerald-50 px-2 py-0.5 rounded-full font-bold">
            موجود در انبار
          </span>
        </div>

        {/* Sort Options */}
        <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar py-0.5">
          <SlidersHorizontal className="w-3 h-3 text-slate-400 ml-1 shrink-0"  strokeWidth={1.5}/>
          {[
            { id: 'popular', label: 'محبوب‌ترین' },
            { id: 'discount', label: 'بیشترین تخفیف' },
            { id: 'cheapest', label: 'ارزان‌ترین' },
            { id: 'expensive', label: 'گران‌ترین' },
          ].map((sort) => (
            <button
              key={sort.id}
              onClick={() => setSortBy(sort.id as typeof sortBy)}
              className={`px-2.5 py-1 rounded-lg text-[10px] font-bold whitespace-nowrap transition-colors ${
                sortBy === sort.id
                  ? 'bg-[#007dfa] text-white shadow-xs'
                  : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
              }`}
            >
              {sort.label}
            </button>
          ))}
        </div>
      </div>

      {/* Grid */}
      {filtered.length === 0 ? (
        <div className="bg-white rounded-2xl p-8 text-center border border-slate-100">
          <p className="text-xs font-bold text-slate-600">کالایی با این مشخصات یافت نشد.</p>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-2.5">
            {filtered.map((product) => (
              <ProductCard key={product.id} product={product} />
            ))}
          </div>
          {hasMore && onLoadMore && (
            <button
              onClick={onLoadMore}
              disabled={loadingMore}
              className="w-full mt-3 py-2.5 bg-white border border-brand/30 text-brand-deep font-bold text-[11px] lg:text-sm rounded-xl shadow-xs hover:bg-brand-soft transition-colors disabled:opacity-60"
            >
              {loadingMore ? 'در حال بارگذاری…' : 'نمایش بیشتر'}
            </button>
          )}
        </>
      )}
    </section>
  );
};
