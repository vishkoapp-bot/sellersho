import type { Product, ProductVariant, CartItem } from '../types';
import { cartItemKey, cartKeyOf } from './cartKey';

export interface PurchaseResolution {
  product: Product;
  variant?: ProductVariant;
  minQty: number;
  maxQty: number;
}

export const mergeCartPurchase = (cart: CartItem[], purchase: PurchaseResolution, quantity: number, details: Pick<CartItem, 'selectedAddonIds' | 'selectedColor' | 'storeId' | 'storeName'>): CartItem[] => {
  if (!cartQuantityAllowed(cart, purchase, quantity)) return cart;
  const variantId = purchase.variant?.id;
  const key = cartKeyOf(purchase.product.id, variantId, details.selectedAddonIds);
  const existingIndex = cart.findIndex((item) => cartItemKey(item) === key);
  if (existingIndex >= 0) {
    // An existing cart line is a price snapshot. Increasing its quantity
    // must not silently replace a historical legacy price with a new quote.
    return cart.map((item, index) => index === existingIndex ? { ...item, quantity: item.quantity + quantity } : item);
  }
  return [...cart, { ...details, product: purchase.product, selectedVariantId: variantId, quantity }];
};

export const activeProductVariants = (product: Product): ProductVariant[] =>
  (product.variants ?? []).filter((variant) => variant.isActive !== false);

export const purchaseLimits = (product: Product, variant?: ProductVariant) => {
  const minQty = Number.isSafeInteger(variant?.minCartQty) && variant!.minCartQty > 0 ? variant!.minCartQty : 1;
  const tracked = variant && (variant.inventoryMode === 'tracked' || product.schemaVersion === 2 || product.productType === 'variable');
  const rawStock = tracked
    ? variant.stock : variant && variant.stock > 0 ? variant.stock : product.stock;
  const stock = Number.isSafeInteger(rawStock) ? Math.max(0, rawStock) : 0;
  const maximum = variant?.maxCartQty == null ? Infinity : Math.max(0, variant.maxCartQty);
  const maxQty = Math.min(stock, Number.isNaN(maximum) ? 0 : maximum);
  return { minQty, maxQty };
};

// A snapshot always carries the same SKU price as the selection. Legacy
// snapshots are left as supplied when there is no authoritative SKU price.
export const productForVariant = (product: Product, variant?: ProductVariant): Product => {
  const schema = product.productSchema;
  const base = schema?.version === 2 ? {
    ...product,
    specs: {
      ...Object.fromEntries(Object.entries(product.specs ?? {}).filter(([name]) => !schema.attributes.some(attribute => attribute.name === name))),
      ...Object.fromEntries(schema.attributes.filter(attribute => attribute.role === 'fixed').map(attribute => [attribute.name, attribute.values.map(value => value.label).join('، ')])),
    },
  } : product;
  if (!variant) return base;
  const price = product.variantPrices?.[variant.id];
  return {
    ...base,
    ...(price ? { price: price.price, originalPrice: price.originalPrice, discountPercent: price.discountPercent, campaignName: price.campaignName ?? product.campaignName } : {}),
    stock: purchaseLimits(product, variant).maxQty,
  };
};

const variantPurchasable = (product: Product, variant: ProductVariant): boolean => {
  if (product.isActive === false || variant.isActive === false) return false;
  const price = product.variantPrices?.[variant.id];
  if ((product.schemaVersion === 2 || product.productType === 'variable')
    && (!price || !Number.isFinite(price.price) || price.price <= 0)) return false;
  const limits = purchaseLimits(product, variant);
  return limits.maxQty >= limits.minQty;
};

export const resolvePurchase = (product: Product, variantId?: string): PurchaseResolution | undefined => {
  if (product.isActive === false) return undefined;
  const variants = activeProductVariants(product);
  if (product.productType === 'variable' && !variantId) return undefined;
  const variant = variantId
    ? variants.find((candidate) => candidate.id === variantId)
    : variants.find((candidate) => candidate.isDefault) ?? (variants.length === 1 ? variants[0] : undefined);
  if (variantId && !variant) return undefined;
  if ((product.schemaVersion === 2 || product.productType === 'variable') && !variant) return undefined;
  if (variant && !variantPurchasable(product, variant)) return undefined;
  const limits = purchaseLimits(product, variant);
  if (limits.maxQty < limits.minQty) return undefined;
  return { product: productForVariant(product, variant), variant, ...limits };
};

export const initialPurchaseVariant = (product: Product, spnc?: string | null): ProductVariant | undefined => {
  const variants = activeProductVariants(product);
  const linked = spnc ? variants.find((variant) => variant.spnc === spnc) : undefined;
  return linked ?? variants.find((variant) => variant.isDefault && variantPurchasable(product, variant))
    ?? variants.find((variant) => variantPurchasable(product, variant)) ?? variants[0];
};

// Earlier axes constrain later axes. Changing an earlier choice selects a
// real in-stock completion, preserving later choices whenever possible.
// This also lets shoppers traverse a sparse diagonal matrix without a dead end.
export const axisOptionVariant = (product: Product, selectedId: string | null, axisId: string, valueId: string): ProductVariant | undefined => {
  const axes = product.productSchema?.attributes.filter((attribute) => attribute.role === 'axis') ?? [];
  const index = axes.findIndex((axis) => axis.id === axisId);
  if (index < 0) return undefined;
  const selected = activeProductVariants(product).find((variant) => variant.id === selectedId)?.selection ?? {};
  const candidates = activeProductVariants(product).filter((variant) =>
    variant.selection?.[axisId] === valueId && variantPurchasable(product, variant)
    && axes.slice(0, index).every((axis) => !selected[axis.id] || variant.selection?.[axis.id] === selected[axis.id]));
  return candidates.find((variant) => axes.slice(index + 1).every((axis) => variant.selection?.[axis.id] === selected[axis.id])) ?? candidates[0];
};

export const cartQuantityAllowed = (cart: CartItem[], purchase: PurchaseResolution, quantity: number): boolean => {
  if (!Number.isSafeInteger(quantity) || quantity <= 0) return false;
  const variantOf = (line: CartItem) => line.selectedVariantId
    ?? activeProductVariants(line.product).find((variant) => variant.isDefault)?.id
    ?? (line.product.variants?.length === 1 ? line.product.variants[0].id : undefined);
  const existing = cart.filter((line) => line.product.id === purchase.product.id && variantOf(line) === purchase.variant?.id)
    .reduce((sum, line) => sum + line.quantity, 0);
  const total = existing + quantity;
  return Number.isSafeInteger(total) && total >= purchase.minQty && total <= purchase.maxQty;
};

export const sellerVariantPreview = (product: Product, variant: ProductVariant, platformPct: number, margin: number): { baseCost?: number; finalPrice?: number; profit?: number } => {
  // Public catalog prices cannot reveal a SKU's wholesale cost. Never fall
  // back to the product's cost (which public mapping can only approximate).
  if (variant.supplierBaseCost != null && variant.supplierBaseCost > 0) {
    const round = (value: number) => Math.round(value / 100) * 100;
    const baseCost = round(variant.supplierBaseCost * (1 + platformPct / 100));
    const finalPrice = round(baseCost * (1 + margin / 100));
    return { baseCost, finalPrice, profit: finalPrice - baseCost };
  }
  return { finalPrice: product.variantPrices?.[variant.id]?.price };
};
