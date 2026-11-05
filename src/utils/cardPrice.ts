// OWNER DECISION (Nov 5, 2026): a product CARD displays the DEFAULT
// variant's price triple (price / originalPrice / discountPercent)
// whenever the product has an isDefault variant with a priced entry —
// the PDP opens preselected on that variant, so the card price is the
// price the customer actually pays for the default pick (gold
// bracelet: the card showed the product-level 83,520,000 while the PDP
// opened on the default variant at 80,040,000). Every other case — no
// variants, no isDefault flag, the default variant missing from
// variantPrices — falls back to the product-level triple, exactly as
// cards rendered before.
//
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

interface CardPriceSource {
  price: number;
  originalPrice: number;
  discountPercent?: number;
  variants?: { id: string; isDefault?: boolean }[];
  variantPrices?: Record<string, { price: number; originalPrice: number; discountPercent: number }>;
}

export const cardPriceOf = (product: CardPriceSource): CardPriceTriple => {
  const fallback: CardPriceTriple = {
    price: product.price,
    originalPrice: product.originalPrice,
    discountPercent: product.discountPercent ?? 0,
  };
  const defaultVariant = product.variants?.find((v) => v.isDefault);
  if (!defaultVariant) return fallback;
  const variantPrice = product.variantPrices?.[defaultVariant.id];
  if (!variantPrice || variantPrice.price == null) return fallback;
  const price = variantPrice.price;
  const originalPrice = variantPrice.originalPrice ?? fallback.originalPrice;
  let discountPercent = variantPrice.discountPercent ?? 0;
  if (!(discountPercent > 0) && originalPrice > price) {
    discountPercent = Math.round((1 - price / originalPrice) * 100);
  }
  return { price, originalPrice, discountPercent };
};
