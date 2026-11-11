import React, { useRef, useState } from "react";
import { Product } from "../types";
import { useStore } from "../context/StoreContext";
import { isProductPublishedInStore } from '../utils/storeAssortment';
import { useToast } from "../context/ToastContext";
import { Package, Plus } from "lucide-react";
import { cardPriceOf } from "../utils/cardPrice";

import { activeProductVariants, cartQuantityAllowed, resolvePurchase } from '../utils/productPurchase';

interface DealProductCardProps {
  product: Product;
}

// FIX 27: live deals-band horizontal card (bundle component ab in
// index-MERGED.js), used by the deals band only when it holds 1 product
// or as the first card when it holds at most 3; the common band (>3 deals)
// renders vertical ProductCards inside the carousel instead. This replaces
// the previous vertical-card wrapper, which never matched live.
// NOTE: live uses a quickAdd(product, 1, cb) with added/failed outcomes;
// the candidate store only exposes addToCart, and the button is disabled
// at stock <= 0, so the failed path is unreachable here and only the
// success toast is reachable, matching live in every reachable state.
export const DealProductCard: React.FC<DealProductCardProps> = ({ product }) => {
  const { openProductDetail, addToCart, formatPrice, toPersianDigits, currentStore, cart } = useStore();
  const published = isProductPublishedInStore(currentStore, product.id);
  // Card display price = the default variant's triple (owner decision, see utils/cardPrice).
  const displayPrice = cardPriceOf(product);

  const available = product.variants?.length
    ? activeProductVariants(product).some((variant) => !!resolvePurchase(product, variant.id))
    : !!resolvePurchase(product);
  const handleQuickAdd = () => {
    if (product.productType === 'variable' || (product.variants?.length && !resolvePurchase(product))) {
      openProductDetail(product);
      return false;
    }
    const purchase = resolvePurchase(product);
    if (!purchase || !cartQuantityAllowed(cart, purchase, purchase.minQty)) return false;
    addToCart(purchase.product, purchase.minQty, undefined, purchase.variant?.id);
    return true;
  };
  const { success } = useToast();
  const [imageFailed, setImageFailed] = useState(false);
  const retried = useRef(false);
  const handleImageError = (e: React.SyntheticEvent<HTMLImageElement>) => {
    const img = e.currentTarget;
    if (retried.current) {
      setImageFailed(true);
      return;
    }
    retried.current = true;
    window.setTimeout(() => {
      img.src = img.src + (img.src.includes("?") ? "&" : "?") + "retry=1";
    }, 300);
  };
  return (
    <div
      onClick={() => openProductDetail(product)}
      data-testid="product-item"
      className="w-full h-[172px] lg:h-auto lg:min-h-[200px] shrink-0 self-center bg-white rounded-[12px] border border-[#e2e5ee] overflow-hidden hover:shadow-md transition-all duration-200 flex cursor-pointer select-none group"
    >
      <div className="relative w-[112px] lg:w-[160px] xl:w-[180px] shrink-0 bg-[#f8fafc] overflow-hidden flex items-center justify-center">
        {product.image && !imageFailed ? (
          <img
            src={product.image}
            alt={product.title}
            className="absolute inset-0 w-full h-full object-contain group-hover:scale-105 transition-transform duration-300"
            loading="lazy"
            decoding="async"
            onError={handleImageError}
          />
        ) : (
          <Package className="w-10 h-10 text-slate-300"  strokeWidth={1.5}/>
        )}
      </div>
      <div className="p-3 flex flex-col justify-between flex-1 min-w-0 gap-2">
        <div>
          <p
            data-testid="product-name"
            className="text-[13px] lg:text-sm font-semibold text-[#252a3c] leading-[1.6] line-clamp-2"
            title={product.title}
          >
            {product.title}
          </p>
          {product.isOriginal === false && (
            <div className="flex mt-1">
              <span className="bg-amber-500 text-white font-extrabold text-[9px] px-1.5 py-0.5 rounded shadow-xs">
                غیر اصل
              </span>
            </div>
          )}
        </div>
        <div>
          {displayPrice.discountPercent > 0 && (
            <div className="flex items-center gap-1.5 pb-1.5">
              <span
                data-testid="discount-badge"
                className="bg-[#fb2c2c] text-white text-[11px] font-bold px-2 py-0.5 rounded-full inline-block leading-none"
              >
                {toPersianDigits(displayPrice.discountPercent)}٪
              </span>
              <span
                data-testid="old-price"
                className="text-[11px] text-slate-400 line-through font-normal"
              >
                {formatPrice(displayPrice.originalPrice)}
              </span>
            </div>
          )}
          <div className="flex items-center justify-between">
            <div className="flex items-baseline gap-1">
              <span className="text-[15px] lg:text-base font-bold text-[#252a3c]">
                {formatPrice(displayPrice.price)}
              </span>
              <span className="text-[10px] text-slate-500 font-light">تومان</span>
            </div>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                if (!available || !published) return;
                if (handleQuickAdd()) success("«" + product.title + "» به سبد خرید اضافه شد.");
              }}
              disabled={!available || !published}
              title={!published ? "در این فروشگاه عرضه نمی‌شود" : available ? "افزودن به سبد خرید" : "ناموجود"}
              className="relative w-10 h-10 rounded-lg bg-[#5d44f8] hover:bg-[#4a33d8] disabled:bg-slate-100 text-white disabled:text-slate-400 flex items-center justify-center shadow-xs transition-all active:scale-90 shrink-0"
            >
              <span aria-hidden="true" className="absolute -inset-[2px]" />
              <Plus className="w-4 h-4 stroke-[2.5]"  strokeWidth={1.5}/>
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
