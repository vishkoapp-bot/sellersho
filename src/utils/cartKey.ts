import { CartItem } from '../types';

// FIX 5 (from Fix 4): live keys the cart by product::variant, so two
// variants of one product are two lines instead of merging into one.
// A line with no variant keeps the empty-variant key `id::`.
// FIX 14b (live bundle $m/$t): the key gains the deduped + sorted
// addon-ids suffix -- same product/variant with different addons are
// two lines; no addons means the plain product::variant key.
export const sortedAddonIds = (ids?: string[]): string[] =>
  ids && ids.length > 0 ? [...new Set(ids.filter((id) => id !== ''))].sort() : [];

export const cartItemKey = (item: CartItem): string => {
  const base = `${item.product.id}::${item.selectedVariantId ?? ''}`;
  const addons = sortedAddonIds(item.selectedAddonIds);
  return addons.length > 0 ? `${base}::${addons.join(',')}` : base;
};

export const cartKeyOf = (productId: string, variantId?: string, addonIds?: string[]): string => {
  const base = `${productId}::${variantId ?? ''}`;
  const addons = sortedAddonIds(addonIds);
  return addons.length > 0 ? `${base}::${addons.join(',')}` : base;
};
