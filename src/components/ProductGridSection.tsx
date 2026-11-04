import React, { useState, useEffect } from 'react';
import { catalogApi } from '../services/catalogApi';
import { useStore } from '../context/StoreContext';
import { ProductCard } from './ProductCard';
import { PriceRangeSlider } from './PriceRangeSlider';
import { ProductCarousel } from './ProductCarousel';
import { SlidersHorizontal, ShoppingBag, RotateCcw, PackageX, Check } from 'lucide-react';

export const ProductGridSection: React.FC = () => {
  const { products, selectedCategory, setSelectedCategory, searchQuery, setSearchQuery, toPersianDigits } = useStore();
  const [sortBy, setSortBy] = useState<'featured' | 'cheapest' | 'expensive' | 'discount'>('featured');
  const [onlyInStock, setOnlyInStock] = useState(false);
  const [onlyOriginal, setOnlyOriginal] = useState(false);
  const [priceMin, setPriceMin] = useState('');
  const [priceMax, setPriceMax] = useState('');
  const [showAll, setShowAll] = useState(false);

  // FIX 14: live price bounds come from the server endpoint
  // (GET /products/price-bounds); client bounds are only a fallback.
  const [serverBounds, setServerBounds] = useState<{ minPrice: number; maxPrice: number } | null>(null);
  useEffect(() => {
    let on = true;
    catalogApi.getPriceBounds().then((b) => { if (on) setServerBounds(b); }).catch(() => {});
    return () => { on = false; };
  }, []);
  const clientMin = products.length ? Math.min(...products.map((p) => p.price)) : 0;
  const clientMax = products.length ? Math.max(...products.map((p) => p.price)) : 0;
  const dataMin = serverBounds?.minPrice ?? clientMin;
  const dataMax = serverBounds?.maxPrice ?? clientMax;
  const effMin = priceMin === '' ? dataMin : Number(priceMin);
  const effMax = priceMax === '' ? dataMax : Number(priceMax);

  // Filter products by selectedCategory, searchQuery, stock, originality, and price
  let filtered = products.filter((p) => {
    const matchesCategory = selectedCategory === 'all' || p.category === selectedCategory;
    const matchesSearch = !searchQuery || 
      p.title.includes(searchQuery) || 
      p.description.includes(searchQuery) ||
      p.supplierName.includes(searchQuery);
    const matchesStock = !onlyInStock || (p.stock || 0) > 0;
    const matchesOriginal = !onlyOriginal || p.isOriginal === true;
    
    let matchesPrice = true;
    if (priceMin !== '' && p.price < Number(priceMin)) matchesPrice = false;
    if (priceMax !== '' && p.price > Number(priceMax)) matchesPrice = false;

    return matchesCategory && matchesSearch && matchesStock && matchesOriginal && matchesPrice;
  });

  // Sort products — live grid buttons have no 'newest' (that option
  // lives only in the catalog dropdown).
  filtered = [...filtered].sort((a, b) => {
    if (sortBy === 'cheapest') return a.price - b.price;
    if (sortBy === 'expensive') return b.price - a.price;
    if (sortBy === 'discount') return b.discountPercent - a.discountPercent;
    return (b.salesCount || 0) - (a.salesCount || 0);
  });

  // FIX 13: live shows the first 15 as a CAROUSEL when unfiltered, and
  // the full grid when filtered or expanded (bundle: $ = !G && !T ?
  // _.slice(0,15) : _, with z = !G && !T selecting the carousel). The
  // 12-per-page pagination from Fix 12 was a misreading of the carousel.
  const hasFilter =
    searchQuery !== '' ||
    selectedCategory !== 'all' ||
    onlyInStock ||
    onlyOriginal ||
    priceMin !== '' ||
    priceMax !== '' ||
    sortBy !== 'featured';
  const carouselMode = !hasFilter && !showAll;
  const visibleProducts = carouselMode ? filtered.slice(0, 15) : filtered;
  const hiddenCount = filtered.length - visibleProducts.length;

  const handleResetFilters = () => {
    setSelectedCategory('all');
    setSearchQuery('');
    setOnlyInStock(false);
    setOnlyOriginal(false);
    setPriceMin('');
    setPriceMax('');
    setSortBy('featured');
    setShowAll(false);
  };

  return (
    <section id="products-section" className="px-3.5 py-2 lg:px-6 lg:py-4 xl:px-10 2xl:px-12">
      {/* Header with Title & Sorting Pills */}
      <div className="flex flex-col gap-2 mb-3">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-xs lg:text-lg xl:text-xl font-black text-slate-800 flex items-center gap-1.5">
              <ShoppingBag className="w-4 h-4 lg:w-5 lg:h-5 text-brand-deep"  strokeWidth={1.5}/>
              <span>منتخب محصولات</span>
              <span className="text-[10px] lg:text-xs font-bold text-slate-400">
                ({toPersianDigits(filtered.length)} کالا)
              </span>
            </h2>
            <p className="text-[10px] lg:text-sm text-slate-400 mt-0.5">کالاها پیش از ارسال کنترل کیفیت می‌شوند؛ ارسال به سراسر کشور</p>
          </div>

          {(onlyInStock || priceMin !== '' || priceMax !== '' || selectedCategory !== 'all' || searchQuery) && (
            <button
              onClick={handleResetFilters}
              className="text-[10px] lg:text-xs text-rose-500 font-bold hover:underline flex items-center gap-1 shrink-0 bg-rose-50 px-2 py-1 min-h-[36px] rounded-lg"
            >
              <RotateCcw className="w-3 h-3"  strokeWidth={1.5}/>
              <span>حذف فیلترها</span>
            </button>
          )}
        </div>

        {/* Filter & Sort Bar */}
        <div className="flex items-center gap-1.5 overflow-x-auto no-scrollbar py-0.5 text-[10px] lg:text-sm">
          <button
            onClick={() => setOnlyInStock(!onlyInStock)}
            className={`px-2.5 lg:px-3.5 py-1 min-h-[36px] lg:min-h-[40px] rounded-lg font-bold flex items-center gap-1 transition-all shrink-0 ${
              onlyInStock
                ? 'bg-brand text-white shadow-2xs'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
            }`}
          >
            {onlyInStock && <Check className="w-3 h-3"  strokeWidth={1.5}/>}
            <span>فقط موجودها</span>
          </button>

          {/* Item 4: Only Original Products Filter */}
          <button
            onClick={() => setOnlyOriginal(!onlyOriginal)}
            className={`px-2.5 lg:px-3.5 py-1 min-h-[36px] lg:min-h-[40px] rounded-lg font-bold flex items-center gap-1 transition-all shrink-0 ${
              onlyOriginal
                ? 'bg-brand-deep text-white shadow-2xs'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
            }`}
          >
            {onlyOriginal && <Check className="w-3 h-3"  strokeWidth={1.5}/>}
            <span>فقط کالای اصل</span>
          </button>


          <div className="h-4 w-px bg-slate-200 shrink-0 mx-0.5" />
          <SlidersHorizontal className="w-3 h-3 text-slate-400 shrink-0"  strokeWidth={1.5}/>
          {[
            { id: 'featured', label: 'محبوب‌ترین' },
            { id: 'discount', label: 'بیشترین تخفیف' },
            { id: 'cheapest', label: 'ارزان‌ترین' },
            { id: 'expensive', label: 'گران‌ترین' },
          ].map((sort) => (
            <button
              key={sort.id}
              onClick={() => setSortBy(sort.id as typeof sortBy)}
              className={`px-2.5 lg:px-3.5 py-1 min-h-[36px] lg:min-h-[40px] inline-flex items-center justify-center rounded-lg font-bold whitespace-nowrap transition-colors shrink-0 ${
                sortBy === sort.id
                  ? 'bg-brand-deep text-white shadow-2xs'
                  : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
              }`}
            >
              {sort.label}
            </button>
          ))}
        </div>
      </div>

      {/* FIX 13: live's Qy dual-thumb slider in its max-w-md box. */}
      {dataMax > dataMin && (
        <div className="w-full max-w-md bg-slate-50 border border-slate-100 rounded-xl px-3 py-2">
          <PriceRangeSlider
            boundMin={dataMin}
            boundMax={dataMax}
            value={[effMin, effMax]}
            onChange={([nextMin, nextMax]) => {
              if (nextMin === dataMin && nextMax === dataMax) {
                setPriceMin('');
                setPriceMax('');
              } else {
                setPriceMin(String(nextMin));
                setPriceMax(String(nextMax));
              }
            }}
          />
        </div>
      )}


      {/* Products Grid */}
      {filtered.length === 0 ? (
        <div className="bg-white rounded-2xl p-6 text-center border border-slate-100 space-y-2.5">
          <div className="w-12 h-12 rounded-full bg-slate-100 text-slate-400 flex items-center justify-center mx-auto">
            <PackageX className="w-6 h-6"  strokeWidth={1.5}/>
          </div>
          <div>
            <h3 className="text-xs font-bold text-slate-800">کالایی با این مشخصات یافت نشد</h3>
            <p className="text-[10px] text-slate-400 mt-0.5">
              می‌توانید فیلترهای اعمال‌شده را حذف کرده یا جستجو را تغییر دهید.
            </p>
          </div>
          <button
            onClick={handleResetFilters}
            className="py-2 px-4 bg-brand hover:bg-brand-deep text-white font-bold text-[11px] lg:text-sm rounded-xl shadow-xs inline-flex items-center gap-1.5"
          >
            <RotateCcw className="w-3 h-3"  strokeWidth={1.5}/>
            <span>مشاهده همه محصولات</span>
          </button>
        </div>
      ) : (
        <>
          {carouselMode ? (
            <ProductCarousel products={visibleProducts} />
          ) : (
            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6 gap-2.5 lg:gap-4 xl:gap-5">
              {visibleProducts.map((product) => (
                <ProductCard key={product.id} product={product} />
              ))}
            </div>
          )}
          {hiddenCount > 0 && (
            <button
              onClick={() => setShowAll(true)}
              className="w-full mt-3 py-2.5 bg-white border border-brand/30 text-brand-deep font-bold text-[11px] lg:text-sm rounded-xl shadow-xs hover:bg-brand-soft transition-colors"
            >
              مشاهده همه {toPersianDigits(filtered.length)} کالا
            </button>
          )}
        </>
      )}
    </section>
  );

};
