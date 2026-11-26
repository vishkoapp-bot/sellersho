import React from 'react';
import { Product } from '../types';
import { buildProductUrl } from '../utils/routing';
import { followStoreLink } from '../utils/followStoreLink';
import { activeStoreSlugOf, useStore } from '../context/StoreContext';
import { isProductPublishedInStore } from '../utils/storeAssortment';
import { useToast } from '../context/ToastContext';
import { Package, Plus, SlidersHorizontal } from 'lucide-react';
import { ReturnWindowBadge } from './ReturnWindowBadge';
import { CampaignDiscountBadge } from './common/CampaignDiscountBadge';
import { FallbackImage } from './common/FallbackImage';
import { cardPriceOf, cardPriceRangeOf } from '../utils/cardPrice';

import { activeProductVariants, cartQuantityAllowed, resolvePurchase } from '../utils/productPurchase';

interface ProductCardProps {
  product: Product;
  // FIX 12: catalog/search/related cards use live's purple '+' square.
  // The deals band keeps the full text button (re-check match) via
  // compactAdd={false} in DealProductCard.
  compactAdd?: boolean;
}

export const ProductCard: React.FC<ProductCardProps> = ({ product, compactAdd = true }) => {
  const { openProductDetail, addToCart, formatPrice, toPersianDigits, currentStore, routeStoreSlug, cart } = useStore();
  const published = isProductPublishedInStore(currentStore, product.id);
  // Card display price = the default variant's triple (owner decision, see utils/cardPrice).
  const displayPrice = cardPriceOf(product);
  // P1(a): secondary range line only; the main price above is untouched.
  const priceRange = cardPriceRangeOf(product);

  const available = product.variants?.length
    ? activeProductVariants(product).some((variant) => !!resolvePurchase(product, variant.id))
    : !!resolvePurchase(product);
  const displayChoice = product.productType === 'variable' || Boolean(product.variants?.length && !resolvePurchase(product));
  const actionLabel = !published ? 'در این فروشگاه عرضه نمی‌شود' : !available ? 'ناموجود'
    : displayChoice ? `انتخاب مدل ${product.title}` : `افزودن ${product.title} به سبد خرید`;
  const ActionIcon = displayChoice ? SlidersHorizontal : Plus;
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
      className="cv-card relative w-full bg-white rounded-[12px] border border-[#e2e5ee] overflow-hidden hover:shadow-md transition-shadow duration-200 flex flex-col cursor-pointer select-none group h-full"
    >
      <div className="product-card-media relative w-full aspect-square bg-[#f8fafc] overflow-hidden rounded-t-[12px] flex items-center justify-center">
        {/* P1-12: shared FallbackImage — the first loadable URL wins
            (product.image, then gallery images); when all fail the
            existing Package icon renders instead of a blank square. */}
        <FallbackImage
          srcs={[product.image, ...(product.images ?? [])].filter(Boolean)}
          alt=""
          width={360}
          height={360}
          className="w-full h-full object-cover motion-safe:group-hover:scale-105 transition-transform duration-300"
          loading="lazy"
          fallback={<Package className="w-10 h-10 text-slate-400" strokeWidth={1.5} aria-hidden="true" />}
        />

        {displayPrice.discountPercent > 0 && (
          <div className="product-card-campaign pointer-events-none absolute top-2 right-2 z-10">
            <CampaignDiscountBadge
              campaignName={product.campaignName}
              discountPercent={displayPrice.discountPercent}
              size="sm"
            />
          </div>
        )}

        {product.isOriginal === false && (
          <div className="product-card-identity pointer-events-none absolute top-2 left-2 z-10 bg-amber-500 text-white font-extrabold text-[9px] px-1.5 py-0.5 rounded shadow-xs">
            غیر اصل
          </div>
        )}

      </div>

      <div className="product-card-body p-2 flex flex-col justify-between flex-1">
        <div>
          <p
            data-testid="product-name"
            className="product-card-name text-[12px] font-normal text-[#252a3c] line-clamp-2 min-h-[2.5em] lg:min-h-12 block mt-1 mb-2 leading-tight"
            title={product.title}
          >
            <a className="product-title-link" href={buildProductUrl(product, activeStoreSlugOf({ currentStore, routeStoreSlug }) ?? undefined)}
              onClick={event => followStoreLink(event, () => openProductDetail(product))}>{product.title}</a>
          </p>
        </div>

        <div>
          {(!available || !published) && <p className="product-card-status hidden">{!published ? 'عرضه نمی‌شود' : 'ناموجود'}</p>}
          {displayPrice.discountPercent > 0 ? (
            <div className="product-card-offer flex items-center gap-1.5 pb-1">
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
          ) : (
            <div className="product-card-price-spacer h-5" />
          )}

          {/* Team 08 (D62): return-window badge, only when Team 03's
              returnWindowEndsAt data is present on the product. */}
          {product.returnWindowEndsAt !== undefined && (
            <div className="pb-1">
              <ReturnWindowBadge returnWindowEndsAt={product.returnWindowEndsAt} />
            </div>
          )}

          {/* Final Price & Cashback Row */}
          <div className="product-card-sale flex items-center justify-between pt-0.5">
            <div className="product-card-amount flex items-baseline gap-1">
              <bdi className="product-card-price text-[13px] font-bold text-[#252a3c]">
                {formatPrice(displayPrice.price)}
              </bdi>
              <span className="text-[10px] text-slate-500 font-light">تومان</span>
            </div>

            <div className="product-card-tools flex items-center gap-1.5">
              {/* Cashback Badge */}
              {product.cashbackPercent > 0 && (
                <div
                  data-testid="cash-back-badge"
                  className="product-card-cashback flex items-center gap-0.5 text-[#6148fe] text-[11px] font-bold"
                >
                  <span>{toPersianDigits(product.cashbackPercent)}٪ <span className="product-card-cashback-note hidden">پاداش خرید</span></span>
                </div>
              )}

              <button
                type="button"
                onClick={() => {
                  if (available && published && handleQuickAdd()) success(`«${product.title}» به سبد خرید اضافه شد.`);
                }}
                disabled={!available || !published}
                aria-label={actionLabel}
                className={`product-card-action relative z-[3] flex min-h-11 shrink-0 items-center justify-center gap-1 rounded-lg text-white transition-colors disabled:bg-slate-100 disabled:text-slate-400 ${compactAdd ? 'w-11 bg-[#5d44f8] hover:bg-[#4a33d8]' : 'bg-[#007dfa] px-2 text-[10px] font-bold hover:bg-blue-600'}`}
              >
                <ActionIcon className={compactAdd ? 'size-5' : 'size-3.5'} strokeWidth={1.75} aria-hidden="true" />
                {!compactAdd && <span>{displayChoice ? 'انتخاب مدل' : 'افزودن به سبد خرید'}</span>}
              </button>
            </div>
          </div>

          {/* P1(a): price range, secondary line under the main price row. */}
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



