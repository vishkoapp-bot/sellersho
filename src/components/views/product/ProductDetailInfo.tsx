import React, { useState, useEffect } from 'react';
import { apiRequest } from '../../../services/apiClient';
import { Product } from '../../../types';
import { useStore } from '../../../context/StoreContext';
import { Truck, ShieldCheck, RotateCcw, Star } from 'lucide-react';
import { CampaignDiscountBadge } from '../../../components/common/CampaignDiscountBadge';

// FIX 13: tabs live INSIDE the info column, directly after the info
// block and BEFORE the variant selector (live bundle order: info with
// tabs at its end -> variant box -> buy bar). The Fix 12 full-width
// placement below the grid was wrong.
export const ProductDetailTabs: React.FC<{ product: Product; fashion?: boolean }> = ({ product, fashion = false }) => {
  const [tab, setTab] = useState<'specs' | 'reviews'>('specs');
  return (
      <div className="pt-1">
        <div className={`flex border-b ${fashion ? 'border-[#e5e0d8]' : 'border-slate-200'} text-xs lg:text-sm font-bold mb-2`}>
          <button
            onClick={() => setTab('specs')}
            className={`pb-1.5 pt-2.5 px-3 border-b-2 ${
              tab === 'specs'
                ? fashion
                  ? 'border-[#1c1c1a] text-[#1c1c1a]'
                  : 'border-brand-deep text-brand-deep'
                : fashion
                  ? 'border-transparent text-[#737373]'
                  : 'border-transparent text-slate-500'
            }`}
          >
            مشخصات فنی
          </button>
          <button
            onClick={() => setTab('reviews')}
            className={`pb-1.5 pt-2.5 px-3 border-b-2 ${
              tab === 'reviews'
                ? fashion
                  ? 'border-[#1c1c1a] text-[#1c1c1a]'
                  : 'border-brand-deep text-brand-deep'
                : fashion
                  ? 'border-transparent text-[#737373]'
                  : 'border-transparent text-slate-500'
            }`}
          >
            نظرات خریداران
          </button>
        </div>

        {tab === 'specs' ? (
          <div className="space-y-2 text-xs lg:text-sm">
            <p className={`${fashion ? 'text-[#737373]' : 'text-slate-600'} text-[11px] lg:text-sm leading-relaxed`}>{product.description}</p>
            {Object.entries(product.specs || {}).length > 0 ? (
              <div className={fashion ? 'bg-white border border-[#e5e0d8] rounded-none p-2.5 divide-y divide-[#e5e0d8]' : 'bg-slate-50 rounded-xl p-2.5 divide-y divide-slate-200'}>
                {Object.entries(product.specs || {}).map(([k, v]) => (
                  <div key={k} className="py-1.5 lg:py-2.5 flex justify-between text-[11px] lg:text-sm">
                    <span className={fashion ? 'text-[#737373]' : 'text-slate-400'}>{k}</span>
                    <span className={fashion ? 'font-bold text-[#1c1c1a]' : 'font-bold text-slate-800'}>{v}</span>
                  </div>
                ))}
              </div>
            ) : (
              <p className={`${fashion ? 'text-[#737373]' : 'text-slate-500'} text-[11px]`}>مشخصاتی برای این کالا ثبت نشده است.</p>
            )}
          </div>
        ) : (
          <ReviewsTab productId={product.id} fashion={fashion} />
        )}
      </div>
  );
};

// FIX 14: live's reviews tab is data-driven (GET /products/{id}/reviews);
// the empty text renders only when items is actually empty.
const ReviewsTab: React.FC<{ productId: string; fashion?: boolean }> = ({ productId, fashion = false }) => {
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
      <div className={fashion ? 'p-2.5 bg-white border border-[#e5e0d8] rounded-none text-xs lg:text-sm space-y-1' : 'p-2.5 bg-slate-50 rounded-xl text-xs lg:text-sm space-y-1'}>
        <p className={`${fashion ? 'text-[#737373]' : 'text-slate-500'} text-[11px]`}>در حال بارگذاری نظرات…</p>
      </div>
    );
  }
  // Live rv error state (bundle-proven): exact copy + button.
  if (state.error) {
    return (
      <div className="py-2 space-y-2">
        <p className={`text-[11px] ${fashion ? 'text-[#737373]' : 'text-slate-500'}`}>بارگذاری نظرات ناموفق بود.</p>
        <button type="button" onClick={() => setRetry((r) => r + 1)} className={fashion ? 'text-[11px] font-bold px-3 py-1.5 rounded-none border border-[#e5e0d8] text-[#1c1c1a] hover:bg-[#f2f2f2]' : 'text-[11px] font-bold px-3 py-1.5 rounded-lg border border-slate-200 text-slate-700 hover:bg-slate-50'}>تلاش مجدد</button>
      </div>
    );
  }
  if (state.items.length === 0) {
    return (
      <div className={fashion ? 'p-2.5 bg-white border border-[#e5e0d8] rounded-none text-xs lg:text-sm space-y-1' : 'p-2.5 bg-slate-50 rounded-xl text-xs lg:text-sm space-y-1'}>
        <p className={`${fashion ? 'text-[#737373]' : 'text-slate-500'} text-[11px]`}>هنوز نظری برای این کالا ثبت نشده است.</p>
      </div>
    );
  }
  return (
    <div className="space-y-2">
      {state.items.map((r: any, i: number) => (
        <div key={r.id || i} className={fashion ? 'p-2.5 bg-white border border-[#e5e0d8] rounded-none text-xs lg:text-sm space-y-1' : 'p-2.5 bg-slate-50 rounded-xl text-xs lg:text-sm space-y-1'}>
          {(r.rating || r.score) && <p className="font-bold text-amber-500">★ {r.rating || r.score}</p>}
          <p className={`${fashion ? 'text-[#737373]' : 'text-slate-600'} text-[11px] lg:text-sm leading-relaxed`}>{r.text || r.comment || r.content || ''}</p>
          {(r.isVerifiedBuyer || r.verified) && <p className="text-emerald-600 text-[10px] font-bold">خریدار تأییدشده</p>}
        </div>
      ))}
    </div>
  );
};

export const ProductDetailInfo: React.FC<{ product: Product; fashion?: boolean }> = ({ product, fashion = false }) => {
  const { formatPrice, toPersianDigits, currentStore, categories, openCatalogCategory, openCatalogBrand, setActiveView, routeStoreSlug } = useStore();
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
        <nav aria-label="مسیر صفحه" className={`flex flex-wrap items-center gap-x-1 text-[11px] lg:text-sm ${fashion ? 'text-[#737373]' : 'text-slate-500'}`}>
          <button type="button" onClick={() => { if (routeStoreSlug) setActiveView('seller_storefront', { storeSlug: routeStoreSlug }); else setActiveView('customer'); if (typeof window !== 'undefined') window.scrollTo({ top: 0, behavior: 'smooth' }); }} className="hover:underline">خانه</button>
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
      <div className={`flex items-center justify-between text-[11px] lg:text-sm ${fashion ? 'text-[#737373]' : 'text-slate-500'}`}>
        <span>دسته: {categoryTitle}</span>
        {product.brand ? <button type="button" onClick={() => openCatalogBrand(product.brand!)} className={fashion ? 'hover:underline hover:text-[#1c1c1a] font-bold cursor-pointer' : 'hover:underline hover:text-brand-deep font-bold cursor-pointer'}>برند: {product.brand}</button> : null}
      </div>

      <h1 className={fashion ? 'text-xl lg:text-[32px] font-extrabold text-[#1c1c1a] leading-[1.4] tracking-normal' : 'text-base lg:text-2xl font-extrabold text-slate-900 leading-snug'}>{product.title}</h1>

      {/* FIX 14: live renders no rating/sales line at all when the values
          are zero (rating only if rating>0 && reviews>0, sales only if >0). */}
      {(((product.rating ?? 0) > 0 && (product.reviewsCount ?? 0) > 0) || (product.salesCount ?? 0) > 0) && (
        <div className={`flex items-center gap-2 ${fashion ? 'text-[#737373]' : 'text-slate-500'} text-[11px]`}>
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

      <div className={fashion ? 'bg-white rounded-none p-2.5 space-y-1.5 text-[11px] text-[#737373] border border-[#e5e0d8]' : 'bg-slate-50 rounded-xl p-2.5 space-y-1.5 text-[11px] text-slate-600 border border-slate-200'}>
        <div className={fashion ? 'flex items-center gap-1.5 text-[#1c1c1a] font-bold' : 'flex items-center gap-1.5 text-brand-deep font-bold'}>
          <Truck className={fashion ? 'w-4 h-4 text-[#1c1c1a]' : 'w-4 h-4 text-brand-deep'}  strokeWidth={1.5}/>
          <span>
            {isDropshipperStore
              ? 'ارسال با بسته‌بندی ایمن پستی به سراسر کشور'
              : 'ارسال از انبار مرکزی با بسته‌بندی ایمن'}
          </span>
        </div>
        <div className={`grid grid-cols-2 gap-1 pt-1 border-t ${fashion ? 'border-[#e5e0d8]' : 'border-slate-200'}`}>
          <span className="flex items-center gap-1">
            <ShieldCheck className="w-3.5 h-3.5 text-emerald-600"  strokeWidth={1.5}/>
            {product.supplierWarranty || 'ضمانت اصالت و سلامت'}
          </span>
          <span className="flex items-center gap-1">
            <RotateCcw className={fashion ? 'w-3.5 h-3.5 text-[#737373]' : 'w-3.5 h-3.5 text-blue-600'}  strokeWidth={1.5}/>
            {returnWindowDays != null ? `${toPersianDigits(returnWindowDays)} روز مهلت تست و بازگشت` : 'مهلت تست و بازگشت'}
          </span>
        </div>
      </div>

            {product.discountPercent > 0 && (
        <CampaignDiscountBadge
          campaignName={product.campaignName || 'جشنواره تخفیف فروشگاه'}
          discountPercent={product.discountPercent}
          size="lg"
        />
      )}

      <div className={fashion ? 'bg-white border border-[#e5e0d8] rounded-none p-3 flex items-center justify-between' : 'bg-blue-50/50 border border-blue-100 rounded-xl p-3 flex items-center justify-between'}>
        <div>
          <span className={`text-[10px] ${fashion ? 'text-[#737373]' : 'text-slate-400'} block`}>قیمت مصرف‌کننده:</span>
          {/* FIX 22: live PDP price box shows the strikethrough price
              ABOVE the big price (DOM-proven both variants). */}
          {product.originalPrice > product.price && (
            <span className={`text-[11px] ${fashion ? 'text-[#737373]' : 'text-slate-400'} line-through block`}>
              {formatPrice(product.originalPrice)} تومان
            </span>
          )}
          <span className={fashion ? 'text-base font-black text-[#1c1c1a]' : 'text-base font-black text-blue-600'}>
            {formatPrice(product.price)} <span className="text-xs font-normal">تومان</span>
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          {(product.stock || 0) <= 0 ? (
            <span className={fashion ? 'text-xs bg-rose-100 text-rose-700 font-bold px-2 py-0.5 rounded-none border border-rose-200' : 'text-xs bg-rose-100 text-rose-700 font-bold px-2 py-0.5 rounded-lg border border-rose-200'}>
              ناموجود در انبار
            </span>
          ) : (
            <span className={fashion ? 'text-[10px] text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded-none font-bold border border-emerald-200' : 'text-[10px] text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded-md font-bold border border-emerald-200'}>
              {toPersianDigits(product.stock)} عدد موجود در انبار
            </span>
          )}
          {product.discountPercent > 0 && (
            <span className={fashion ? 'text-xs bg-rose-500 text-white font-bold px-2 py-0.5 rounded-none' : 'text-xs bg-rose-500 text-white font-bold px-2 py-0.5 rounded-lg'}>
              {product.campaignName || '٪' + toPersianDigits(product.discountPercent) + ' تخفیف'}
            </span>
          )}
        </div>
      </div>

    </div>
  );
};
