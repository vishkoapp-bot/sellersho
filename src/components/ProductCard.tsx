import React from 'react';
import { Product } from '../types';
import { useStore } from '../context/StoreContext';
import { isProductPublishedInStore } from '../utils/storeAssortment';
import { useToast } from '../context/ToastContext';
import { Package, Plus } from 'lucide-react';
import { ReturnWindowBadge } from './ReturnWindowBadge';
import { CampaignDiscountBadge } from './common/CampaignDiscountBadge';
import { cardPriceOf } from '../utils/cardPrice';

import { activeProductVariants, cartQuantityAllowed, resolvePurchase } from '../utils/productPurchase';

interface ProductCardProps {
  product: Product;
  // FIX 12: catalog/search/related cards use live's purple '+' square.
  // The deals band keeps the full text button (re-check match) via
  // compactAdd={false} in DealProductCard.
  compactAdd?: boolean;
}

export const ProductCard: React.FC<ProductCardProps> = ({ product, compactAdd = true }) => {
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

  return (
    <div
      onClick={() => openProductDetail(product)}
      data-testid="product-item"
      id="product_item"
      className="cv-card w-full bg-white rounded-[12px] border border-[#e2e5ee] overflow-hidden hover:shadow-md transition-shadow duration-200 flex flex-col cursor-pointer select-none group h-full"
    >
      {/* 1. Image Container (Square, Rounded Top, Overflow Hidden) */}
      <div className="relative w-full aspect-square bg-[#f8fafc] overflow-hidden rounded-t-[12px] flex items-center justify-center">
        {product.image ? (
          <img
            src={product.image}
            alt={product.title}
            className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
            loading="lazy"
            onError={(e) => {
              (e.target as HTMLImageElement).style.display = 'none';
            }}
          />
        ) : (
          <Package className="w-10 h-10 text-slate-300"  strokeWidth={1.5}/>
        )}

        {/* FIX 12: live cards show no merchant logo circle and no store
            name anywhere (deals, catalog/search, related). */}

        {/* Requirement 23: Non-original Authenticity Badge */}
                {displayPrice.discountPercent > 0 && (
          <div className="absolute top-2 right-2 z-10">
            <CampaignDiscountBadge
              campaignName={product.campaignName}
              discountPercent={displayPrice.discountPercent}
              size="sm"
            />
          </div>
        )}

        {product.isOriginal === false && (
          <div className="absolute top-2 left-2 z-10 bg-amber-500 text-white font-extrabold text-[9px] px-1.5 py-0.5 rounded shadow-xs">
            غیر اصل
          </div>
        )}

      </div>

      {/* 2. Info Container (Padding: 8px) */}
      <div className="p-2 flex flex-col justify-between flex-1">
        <div>
          {/* Product Name (Two Line Clamp) */}
          <p
            data-testid="product-name"
            className="text-[12px] font-normal text-[#252a3c] line-clamp-2 min-h-[2.5em] lg:min-h-12 block mt-1 mb-2 leading-tight"
            title={product.title}
          >
            {product.title}
          </p>
        </div>

        <div>
          {/* Discount & Old Price Row */}
          {displayPrice.discountPercent > 0 ? (
            <div className="flex items-center gap-1.5 pb-1">
              <span
                data-testid="discount-badge"
                className="bg-[#fb2c2c] text-white text-[11px] font-bold px-2 py-0.5 rounded-full inline-block leading-none"
              >
                {product.campaignName || toPersianDigits(displayPrice.discountPercent) + '٪'}
              </span>
              <span
                data-testid="old-price"
                className="text-[11px] text-slate-400 line-through font-normal"
              >
                {formatPrice(displayPrice.originalPrice)}
              </span>
            </div>
          ) : (
            <div className="h-5" />
          )}

          {/* Team 08 (D62): return-window badge, only when Team 03's
              returnWindowEndsAt data is present on the product. */}
          {product.returnWindowEndsAt !== undefined && (
            <div className="pb-1">
              <ReturnWindowBadge returnWindowEndsAt={product.returnWindowEndsAt} />
            </div>
          )}

          {/* Final Price & Cashback Row */}
          <div className="flex items-center justify-between pt-0.5">
            <div className="flex items-baseline gap-1">
              <span className="text-[13px] font-bold text-[#252a3c]">
                {formatPrice(displayPrice.price)}
              </span>
              <span className="text-[10px] text-slate-500 font-light">تومان</span>
            </div>

            <div className="flex items-center gap-1.5">
              {/* Cashback Badge */}
              {product.cashbackPercent > 0 && (
                <div
                  data-testid="cash-back-badge"
                  className="flex items-center gap-0.5 text-[#6148fe] text-[11px] font-bold"
                >
                  <span>٪{toPersianDigits(product.cashbackPercent)}</span>
                </div>
              )}

              {/* Quick Add To Cart Button — FIX 12: live's card button
                  is a purple '+' square (w-10/11, bg #5d44f8, no text,
                  no store name). The deals band keeps the text button
                  via compactAdd={false}. */}
              {compactAdd ? (
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    if (!available || !published) return;
                    if (handleQuickAdd()) success(`«${product.title}» به سبد خرید اضافه شد.`);
                  }}
                  disabled={!available || !published}
                  title={!published ? 'در این فروشگاه عرضه نمی‌شود' : available ? 'افزودن به سبد خرید' : 'ناموجود'}
                  className="w-10 h-10 lg:w-11 lg:h-11 rounded-lg bg-[#5d44f8] hover:bg-[#4a33d8] disabled:bg-slate-100 text-white disabled:text-slate-400 flex items-center justify-center shadow-xs transition-all active:scale-90 shrink-0"
                >
                  <Plus className="w-5 h-5 stroke-[2.5]"  strokeWidth={1.5}/>
                </button>
              ) : (
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    if (!available || !published) return;
                    if (handleQuickAdd()) success(`«${product.title}» به سبد خرید اضافه شد.`);
                  }}
                  disabled={!available || !published}
                  title={!published ? 'در این فروشگاه عرضه نمی‌شود' : available ? 'افزودن به سبد خرید' : 'ناموجود'}
                  className="px-2 py-1.5 rounded-lg bg-[#007dfa] hover:bg-blue-600 disabled:bg-slate-100 text-white disabled:text-slate-400 flex items-center gap-1 shadow-xs transition-all active:scale-90 shrink-0 text-[10px] font-bold"
                >
                  <Plus className="w-3.5 h-3.5 stroke-[2.5]"  strokeWidth={1.5}/>
                  <span>افزودن به سبد خرید</span>
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};



