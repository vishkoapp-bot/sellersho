import React from "react";
import { Product } from "../types";
import { buildProductUrl } from '../utils/routing';
import { followStoreLink } from '../utils/followStoreLink';
import { activeStoreSlugOf, useStore } from "../context/StoreContext";
import { isProductPublishedInStore } from '../utils/storeAssortment';
import { useToast } from "../context/ToastContext";
import { Layers, Package, Plus } from "lucide-react";
import { FallbackImage } from './common/FallbackImage';
import { cardPriceOf, cardPriceRangeOf } from "../utils/cardPrice";

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
  const { openProductDetail, addToCart, formatPrice, toPersianDigits, currentStore, routeStoreSlug, cart } = useStore();
  const published = isProductPublishedInStore(currentStore, product.id);
  // Card display price = the default variant's triple (owner decision, see utils/cardPrice).
  const displayPrice = cardPriceOf(product);
  const priceRange = cardPriceRangeOf(product);

  const available = product.variants?.length
    ? activeProductVariants(product).some((variant) => !!resolvePurchase(product, variant.id))
    : !!resolvePurchase(product);
  const displayChoice = product.productType === 'variable' || Boolean(product.variants?.length && !resolvePurchase(product));
  const ActionIcon = displayChoice ? Layers : Plus;
  const handleQuickAdd = () => {
    if (displayChoice) {
      openProductDetail(product);
      return false;
    }
    const purchase = resolvePurchase(product);
    if (!purchase || !cartQuantityAllowed(cart, purchase, purchase.minQty)) return false;
    addToCart(purchase.product, purchase.minQty, undefined, purchase.variant?.id);
    return true;
  };
  const { success } = useToast();
  return (
    <div
      data-testid="product-item"
      className="central-deal-card relative w-full min-h-[172px] lg:min-h-[200px] shrink-0 self-center bg-white rounded-[12px] border border-[#e2e5ee] overflow-hidden hover:shadow-md transition-shadow duration-200 flex cursor-pointer select-none group"
    >
      <div className="product-card-media relative w-[min(112px,32%)] lg:w-[160px] xl:w-[180px] shrink-0 bg-[#f8fafc] overflow-hidden flex items-center justify-center">
        <FallbackImage srcs={[product.image, ...(product.images ?? [])]} alt="" width={240} height={240}
          className="absolute inset-0 w-full h-full object-contain motion-safe:group-hover:scale-105 transition-transform duration-300"
          loading="lazy" decoding="async" fallback={<Package className="size-10 text-slate-400" strokeWidth={1.5} aria-hidden="true" />} />
      </div>
      <div className="product-card-body p-3 flex flex-col justify-between flex-1 min-w-0 gap-2">
        <div>
          <p
            data-testid="product-name"
            className="product-card-name text-[13px] lg:text-sm font-semibold text-[#252a3c] leading-[1.6] line-clamp-2"
            title={product.title}
          >
            <a className="product-title-link" href={buildProductUrl(product, activeStoreSlugOf({ currentStore, routeStoreSlug }) ?? undefined)}
              onClick={event => followStoreLink(event, () => openProductDetail(product))}>{product.title}</a>
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
            <div className="product-card-offer flex items-center gap-1.5 pb-1.5">
              <span
                data-testid="discount-badge"
                className="product-card-discount bg-[#fb2c2c] text-white text-[11px] font-bold px-2 py-0.5 rounded-full inline-block leading-none"
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
          {(!available || !published) && <p className="product-card-status hidden">{!published ? 'عرضه نمی‌شود' : 'ناموجود'}</p>}
          <div className="product-card-sale flex items-center justify-between">
            <div className="product-card-amount flex items-baseline gap-1">
              <span className="product-card-price text-[15px] lg:text-base font-bold text-[#252a3c]">
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
              aria-label={!published ? 'در این فروشگاه عرضه نمی‌شود' : !available ? 'ناموجود' : displayChoice ? `انتخاب مدل ${product.title}` : `افزودن ${product.title} به سبد خرید`}
              className="product-card-action relative z-[3] w-11 h-11 rounded-lg bg-[#5d44f8] hover:bg-[#4a33d8] disabled:bg-slate-100 text-white disabled:text-slate-400 flex items-center justify-center shadow-xs transition-all active:scale-90 shrink-0"
            >
              <span aria-hidden="true" className="absolute -inset-[2px]" />
              <ActionIcon className="size-5" strokeWidth={1.75} aria-hidden="true" />
            </button>
          </div>
          {/* P1(a): price range, secondary line under the price row. */}
          {priceRange.hasRange && (
            <p data-testid="price-range" className="text-[10px] text-slate-400 font-normal pt-0.5">
              از {formatPrice(priceRange.min)} تا {formatPrice(priceRange.max)} تومان
            </p>
          )}
        </div>
      </div>
    </div>
  );
};
