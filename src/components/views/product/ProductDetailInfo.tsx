import React, { useState, useEffect } from 'react';
import { apiRequest } from '../../../services/apiClient';
import { Product } from '../../../types';
import { useStore } from '../../../context/StoreContext';
import { Truck, ShieldCheck, RotateCcw, Star } from 'lucide-react';

// FIX 13: tabs live INSIDE the info column, directly after the info
// block and BEFORE the variant selector (live bundle order: info with
// tabs at its end -> variant box -> buy bar). The Fix 12 full-width
// placement below the grid was wrong.
export const ProductDetailTabs: React.FC<{ product: Product }> = ({ product }) => {
  const [tab, setTab] = useState<'specs' | 'reviews'>('specs');
  return (
      <div className="pt-1">
        <div className="flex border-b border-slate-200 text-xs lg:text-sm font-bold mb-2">
          <button
            onClick={() => setTab('specs')}
            className={`pb-1.5 pt-2.5 px-3 border-b-2 ${
              tab === 'specs' ? 'border-brand-deep text-brand-deep' : 'border-transparent text-slate-500'
            }`}
          >
            مشخصات فنی
          </button>
          <button
            onClick={() => setTab('reviews')}
            className={`pb-1.5 pt-2.5 px-3 border-b-2 ${
              tab === 'reviews' ? 'border-brand-deep text-brand-deep' : 'border-transparent text-slate-500'
            }`}
          >
            نظرات خریداران
          </button>
        </div>

        {tab === 'specs' ? (
          <div className="space-y-2 text-xs lg:text-sm">
            <p className="text-slate-600 text-[11px] lg:text-sm leading-relaxed">{product.description}</p>
            {Object.entries(product.specs || {}).length > 0 ? (
              <div className="bg-slate-50 rounded-xl p-2.5 divide-y divide-slate-200">
                {Object.entries(product.specs || {}).map(([k, v]) => (
                  <div key={k} className="py-1.5 lg:py-2.5 flex justify-between text-[11px] lg:text-sm">
                    <span className="text-slate-400">{k}</span>
                    <span className="font-bold text-slate-800">{v}</span>
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-slate-500 text-[11px]">مشخصاتی برای این کالا ثبت نشده است.</p>
            )}
          </div>
        ) : (
          <ReviewsTab productId={product.id} />
        )}
      </div>
  );
};

// FIX 14: live's reviews tab is data-driven (GET /products/{id}/reviews);
// the empty text renders only when items is actually empty.
const ReviewsTab: React.FC<{ productId: string }> = ({ productId }) => {
  const [state, setState] = useState<{ loading: boolean; error: boolean; items: any[] }>({ loading: true, error: false, items: [] });
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let on = true;
    setState({ loading: true, error: false, items: [] });
    apiRequest<{ items?: any[] }>('/products/' + productId + '/reviews')
      .then((res) => { if (on) setState({ loading: false, error: false, items: res.items || [] }); })
      .catch(() => { if (on) setState({ loading: false, error: true, items: [] }); });
    return () => { on = false; };
  }, [productId, retry]);
  if (state.loading) {
    return (
      <div className="p-2.5 bg-slate-50 rounded-xl text-xs lg:text-sm space-y-1">
        <p className="text-slate-500 text-[11px]">در حال بارگذاری نظرات…</p>
      </div>
    );
  }
  // Live rv error state (bundle-proven): exact copy + button.
  if (state.error) {
    return (
      <div className="py-2 space-y-2">
        <p className="text-[11px] text-slate-500">بارگذاری نظرات ناموفق بود.</p>
        <button type="button" onClick={() => setRetry((r) => r + 1)} className="text-[11px] font-bold px-3 py-1.5 rounded-lg border border-slate-200 text-slate-700 hover:bg-slate-50">تلاش مجدد</button>
      </div>
    );
  }
  if (state.items.length === 0) {
    return (
      <div className="p-2.5 bg-slate-50 rounded-xl text-xs lg:text-sm space-y-1">
        <p className="text-slate-500 text-[11px]">هنوز نظری برای این کالا ثبت نشده است.</p>
      </div>
    );
  }
  return (
    <div className="space-y-2">
      {state.items.map((r: any, i: number) => (
        <div key={r.id || i} className="p-2.5 bg-slate-50 rounded-xl text-xs lg:text-sm space-y-1">
          {(r.rating || r.score) && <p className="font-bold text-amber-500">★ {r.rating || r.score}</p>}
          <p className="text-slate-600 text-[11px] lg:text-sm leading-relaxed">{r.text || r.comment || r.content || ''}</p>
          {(r.isVerifiedBuyer || r.verified) && <p className="text-emerald-600 text-[10px] font-bold">خریدار تأییدشده</p>}
        </div>
      ))}
    </div>
  );
};

export const ProductDetailInfo: React.FC<{ product: Product }> = ({ product }) => {
  const { formatPrice, toPersianDigits, currentStore, categories, openCatalogCategory, openCatalogBrand, setActiveView } = useStore();
  // Live parity (bundle t3): the return window comes from
  // GET /public/return-window-days (valid 1..365, else null);
  // when the value is missing/failed live renders no number.
  const [returnWindowDays, setReturnWindowDays] = useState<number | null>(null);
  useEffect(() => {
    let on = true;
    apiRequest<{ days?: unknown }>('/public/return-window-days')
      .then((res) => {
        const n = Number(res?.days);
        if (on && Number.isFinite(n) && n >= 1 && n <= 365) setReturnWindowDays(Math.round(n));
      })
      .catch(() => {});
    return () => { on = false; };
  }, []);
  const isDropshipperStore = currentStore.ownerType === 'dropshipper';
  // FIX 13 (sweep-corrected): live branches delivery copy on the
  // store's ownerType (ownerType === 'dropshipper'), NOT on the
  // product warehouse — the Fix 12 warehouse branch was wrong for
  // dropshipper storefronts.
  // Category: live resolves the slug to the category TITLE via the
  // categories list (the bracelet's "gold-silver" renders "طلا و نقره").
  const categoryTitle =
    categories.find((c) => c.slug === product.category || c.id === product.category)?.title ||
    product.category;
  // FIX 14: live breadcrumb — خانه / category chain / product title.
  const breadcrumb: { slug: string; title: string }[] = [];
  {
    let cur = categories.find((c) => c.slug === product.category || c.id === product.category);
    const guard = new Set<string>();
    while (cur && !guard.has(cur.id)) {
      guard.add(cur.id);
      breadcrumb.unshift({ slug: cur.slug, title: cur.title });
      cur = categories.find((c) => c.id === cur!.parentId);
    }
  }

  return (
    <div className="space-y-3.5 text-xs">
      {breadcrumb.length > 0 && (
        <nav aria-label="مسیر صفحه" className="flex flex-wrap items-center gap-x-1 text-[11px] lg:text-sm text-slate-500">
          <button type="button" onClick={() => { setActiveView('customer'); if (typeof window !== 'undefined') window.scrollTo({ top: 0, behavior: 'smooth' }); }} className="hover:underline">خانه</button>
          {breadcrumb.map((b) => (
            <React.Fragment key={b.slug}>
              <span aria-hidden="true">/</span>
              <button type="button" onClick={() => openCatalogCategory(b.slug)} className="hover:underline">{b.title}</button>
            </React.Fragment>
          ))}
          <span aria-hidden="true">/</span>
          <span aria-current="page" className="font-bold">{product.title}</span>
        </nav>
      )}
      <div className="flex items-center justify-between text-[11px] lg:text-sm text-slate-500">
        <span>دسته: {categoryTitle}</span>
        {product.brand ? <button type="button" onClick={() => openCatalogBrand(product.brand!)} className="hover:underline hover:text-brand-deep font-bold cursor-pointer">برند: {product.brand}</button> : null}
      </div>

      <h1 className="text-base lg:text-2xl font-extrabold text-slate-900 leading-snug">{product.title}</h1>

      {/* FIX 14: live renders no rating/sales line at all when the values
          are zero (rating only if rating>0 && reviews>0, sales only if >0). */}
      {(((product.rating ?? 0) > 0 && (product.reviewsCount ?? 0) > 0) || (product.salesCount ?? 0) > 0) && (
        <div className="flex items-center gap-2 text-slate-500 text-[11px]">
          {(product.rating ?? 0) > 0 && (product.reviewsCount ?? 0) > 0 && (
            <>
              <div className="flex items-center text-amber-500 font-bold">
                <Star className="w-3.5 h-3.5 fill-amber-400"  strokeWidth={1.5}/>
                <span>{toPersianDigits(product.rating ?? 0)}</span>
              </div>
              <span>•</span>
              <span>{toPersianDigits(product.reviewsCount ?? 0)} نظر</span>
              {(product.salesCount ?? 0) > 0 && <span>•</span>}
            </>
          )}
          {(product.salesCount ?? 0) > 0 && (
            <span>{toPersianDigits(product.salesCount ?? 0)} فروش</span>
          )}
        </div>
      )}

      <div className="bg-slate-50 rounded-xl p-2.5 space-y-1.5 text-[11px] text-slate-600 border border-slate-200">
        <div className="flex items-center gap-1.5 text-brand-deep font-bold">
          <Truck className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>
          <span>
            {isDropshipperStore
              ? 'ارسال با بسته‌بندی ایمن پستی به سراسر کشور'
              : 'ارسال از انبار مرکزی با بسته‌بندی ایمن'}
          </span>
        </div>
        <div className="grid grid-cols-2 gap-1 pt-1 border-t border-slate-200">
          <span className="flex items-center gap-1">
            <ShieldCheck className="w-3.5 h-3.5 text-emerald-600"  strokeWidth={1.5}/>
            {product.supplierWarranty || 'ضمانت اصالت و سلامت'}
          </span>
          <span className="flex items-center gap-1">
            <RotateCcw className="w-3.5 h-3.5 text-blue-600"  strokeWidth={1.5}/>
            {returnWindowDays != null ? `${toPersianDigits(returnWindowDays)} روز مهلت تست و بازگشت` : 'مهلت تست و بازگشت'}
          </span>
        </div>
      </div>

      <div className="bg-blue-50/50 border border-blue-100 rounded-xl p-3 flex items-center justify-between">
        <div>
          <span className="text-[10px] text-slate-400 block">قیمت مصرف‌کننده:</span>
          {/* FIX 22: live PDP price box shows the strikethrough price
              ABOVE the big price (DOM-proven both variants). */}
          {product.originalPrice > product.price && (
            <span className="text-[11px] text-slate-400 line-through block">
              {formatPrice(product.originalPrice)} تومان
            </span>
          )}
          <span className="text-base font-black text-blue-600">
            {formatPrice(product.price)} <span className="text-xs font-normal">تومان</span>
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          {(product.stock || 0) <= 0 ? (
            <span className="text-xs bg-rose-100 text-rose-700 font-bold px-2 py-0.5 rounded-lg border border-rose-200">
              ناموجود در انبار
            </span>
          ) : (
            <span className="text-[10px] text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded-md font-bold border border-emerald-200">
              {toPersianDigits(product.stock)} عدد موجود در انبار
            </span>
          )}
          {product.discountPercent > 0 && (
            <span className="text-xs bg-rose-500 text-white font-bold px-2 py-0.5 rounded-lg">
              ٪{toPersianDigits(product.discountPercent)} تخفیف
            </span>
          )}
        </div>
      </div>

    </div>
  );
};
