import { activeProductVariants, cheapestPurchaseVariant, isHiddenSyntheticVariant, variantPurchasable, variantSaleOf } from './productPurchase';

// Nov 5: the gold bracelet's default held 5 units. Rename the SKU to keep that stock purchasable.
// Batch 9 excludes stale synthetic names only for variable products. Simple SKUs keep «پیش‌فرض».
// The card uses the priced, purchasable store default, then the cheapest purchasable
// variant by effective price, then the first active priced variant.
// With no priced variant, it uses the product-level price triple.
// Display-only: this helper never mutates the product. Cart/checkout
// keep pricing from the cart line's own snapshot (the PDP passes its
// variant-adjusted displayProduct), and seller-panel surfaces that
// intentionally read the product-level price (margin caps, catalog
// pricing) do not use this helper.
//
// The parameter is structural so both the mapped Product and the raw
// catalog DTO (header search rows) are priced the same way. When a
// variant entry carries no positive percent but a higher compare-at,
// the percent is derived from the pair — the same derivation the
// backend (deriveDiscountPercent) and the product mapper apply at
// product level — so campaign compare-at rendering stays consistent
// (variantPrices already include deals-widget discounts per variant).
export interface CardPriceTriple {
  price: number;
  originalPrice: number;
  discountPercent: number;
}


type CardPriceSource = Parameters<typeof cheapestPurchaseVariant>[0] & {
  price: number;
  originalPrice: number;
  discountPercent?: number;
  priceMin?: number;
  priceMax?: number;
  storeDefaultVariantId?: string | null;
};

const pickCardPriceVariant = (product: CardPriceSource) => {
  const active = activeProductVariants(product).filter((variant) => !isHiddenSyntheticVariant(product, variant));
  const priced = (v: { id: string }) => {
    const vp = product.variantPrices?.[v.id];
    return vp != null && vp.price != null && Number.isFinite(vp.price);
  };
  const storeDefault = product.storeDefaultVariantId
    ? active.find((v) => v.id === product.storeDefaultVariantId && priced(v) && variantPurchasable(product, v))
    : undefined;
  return storeDefault ?? cheapestPurchaseVariant(product)
    ?? active.find(priced);
};

export const cardPriceOf = (product: CardPriceSource): CardPriceTriple => {
  const fallback: CardPriceTriple = {
    price: product.price,
    originalPrice: product.originalPrice,
    discountPercent: product.discountPercent ?? 0,
  };
  const defaultVariant = pickCardPriceVariant(product);
  if (!defaultVariant) return fallback;
  const variantPrice = product.variantPrices![defaultVariant.id];
  // P1(c): a valid sale replaces the resolved price (never stacks).
  const sale = variantSaleOf(variantPrice);
  const price = sale ? sale.price : variantPrice.price;
  const originalPrice = sale ? sale.originalPrice : (variantPrice.originalPrice ?? fallback.originalPrice);
  let discountPercent = sale ? sale.discountPercent : (variantPrice.discountPercent ?? 0);
  if (!(discountPercent > 0) && originalPrice > price) {
    discountPercent = Math.round((1 - price / originalPrice) * 100);
  }
  return { price, originalPrice, discountPercent };
};

// B7: the variant the card price came from, with its display label
// (variantName || name, the pattern SellerCatalogTab uses). variant is
// null when the triple is the product-level fallback — the label must
// then show no variant name at all, never a guessed one.
export const cardPriceVariantOf = (product: CardPriceSource): { triple: CardPriceTriple; variant: { id: string; label: string } | null } => {
  const triple = cardPriceOf(product);
  const picked = pickCardPriceVariant(product);
  if (!picked) return { triple, variant: null };
  return { triple, variant: { id: picked.id, label: picked.variantName || picked.name || '' } };
};

// P1(a) price range: a SECONDARY line under the card's main price, never
// a replacement for the main variant price. The backend's
// priceMin/priceMax win when present; otherwise the range is computed
// from the active priced variants' effective (post-sale) prices.
// hasRange only for variable products with >=2 distinctly-priced
// active variants.
export interface CardPriceRange { min: number; max: number; hasRange: boolean; }

export const cardPriceRangeOf = (product: CardPriceSource): CardPriceRange => {
  const noRange: CardPriceRange = { min: 0, max: 0, hasRange: false };
  if (product.productType !== 'variable') return noRange;
  let min: number | undefined;
  let max: number | undefined;
  if (product.priceMin != null && product.priceMax != null) {
    min = product.priceMin;
    max = product.priceMax;
  } else {
    const active = activeProductVariants(product).filter((variant) => !isHiddenSyntheticVariant(product, variant));
    const prices = active
      .map((v) => product.variantPrices?.[v.id])
      .filter((vp): vp is NonNullable<typeof vp> => vp != null && Number.isFinite(vp.price) && vp.price > 0)
      .map((vp) => variantSaleOf(vp)?.price ?? vp.price);
    if (prices.length < 2) return noRange;
    min = Math.min(...prices);
    max = Math.max(...prices);
  }
  // min !== max already implies >=2 distinctly-priced variants (the
  // backend range is computed over the same candidate set).
  if (min == null || max == null || min === max) return noRange;
  return { min, max, hasRange: true };
};
