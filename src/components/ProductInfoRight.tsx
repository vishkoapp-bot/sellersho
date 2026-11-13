import React from 'react';
import { Product } from '../types';
import { useStore } from '../context/StoreContext';
import { Truck, ShieldCheck, Building2, Star, RotateCcw, AlertTriangle, Package } from 'lucide-react';
import { productWeightLabel } from '../utils/productPurchase';

interface ProductInfoRightProps {
  product: Product;
}

export const ProductInfoRight: React.FC<ProductInfoRightProps> = ({ product }) => {
  const { formatPrice, toPersianDigits, currentStore, routeStoreSlug, stores } = useStore();
  // Seller of record resolves from the ROUTED store (routeStoreSlug),
  // never the ambient currentStore: a routed dropshipper store sells
  // under its own name; with no store scope, the supplier name shows
  // only when the API actually served one (post-gate anonymous
  // responses carry none, so no line renders rather than a
  // fabricated name).
  const routedStore = routeStoreSlug
    ? currentStore?.slug === routeStoreSlug
      ? currentStore
      : stores.find((s) => s.slug === routeStoreSlug)
    : undefined;
  const supplierName = (product.supplierName || '').trim();
  const brandLabel =
    routedStore?.ownerType === 'dropshipper'
      ? `فروشگاه: ${routedStore.name}`
      : supplierName
        ? `تأمین‌کننده: ${supplierName}`
        : null;

  return (
    <div className="space-y-4">
      <div>
        <div className="flex items-center justify-between mb-1.5">
          {brandLabel ? (
            <span className="text-xs font-bold text-blue-600 flex items-center gap-1">
              <Building2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
              <span>{brandLabel}</span>
            </span>
          ) : null}
          <div className="flex items-center gap-1 text-xs text-amber-500 font-bold">
            <Star className="w-3.5 h-3.5 fill-current"  strokeWidth={1.5}/>
            <span>{toPersianDigits(product.rating)}</span>
            <span className="text-slate-400 font-normal">
              ({toPersianDigits(product.reviewsCount)} نظر)
            </span>
          </div>
        </div>

        {/* Requirement 23: Non-original legal authenticity badge */}
        {product.isOriginal === false && (
          <div className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-amber-50 border border-amber-300 text-amber-900 rounded-xl text-xs font-bold mb-1.5">
            <AlertTriangle className="w-3.5 h-3.5 text-amber-600 shrink-0"  strokeWidth={1.5}/>
            <span>کالای غیر اصل (High Copy)</span>
          </div>
        )}

        <h2 className="text-base md:text-lg font-black text-slate-900 leading-snug">
          {product.title}
        </h2>
      </div>

      <div className="bg-slate-50 rounded-2xl p-3.5 space-y-1">
        {product.discountPercent > 0 && (
          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-400 line-through">
              {formatPrice(product.originalPrice)}
            </span>
            <span className="bg-rose-500 text-white font-black text-[10px] px-2 py-0.5 rounded-full">
              {toPersianDigits(product.discountPercent)}٪ تخفیف
            </span>
          </div>
        )}
        <div className="flex items-baseline gap-1.5">
          <span className="text-2xl font-black text-slate-900">
            {formatPrice(product.price)}
          </span>
          <span className="text-xs font-bold text-slate-500">تومان</span>
        </div>
      </div>


      <div className="space-y-2 text-xs text-slate-600 bg-white border border-slate-100 rounded-2xl p-3">
        <div className="flex items-center gap-2 text-emerald-700">
          <Truck className="w-4 h-4 text-emerald-600 shrink-0"  strokeWidth={1.5}/>
          <span>{product.warehouse} (آماده ارسال سریع)</span>
        </div>
        <div className="flex items-center gap-2">
          <ShieldCheck className="w-4 h-4 text-blue-600 shrink-0"  strokeWidth={1.5}/>
          <span>{product.supplierWarranty}</span>
        </div>
        <div className="flex items-center gap-2">
          <RotateCcw className="w-4 h-4 text-amber-600 shrink-0"  strokeWidth={1.5}/>
          <span>۷ روز مهلت تست و بازگشت کالا</span>
        </div>
        {/* P0: effective weight/dims of the selected variant; hidden when empty. */}
        {productWeightLabel(product) && (
          <div className="flex items-center gap-2 flex-wrap">
            <Package className="w-4 h-4 text-slate-500 shrink-0" strokeWidth={1.5} />
            <span>{productWeightLabel(product)}</span>
          </div>
        )}
      </div>
    </div>
  );
};
