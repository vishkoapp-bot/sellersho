import type { Product, ProductVariant, VariantPrice, CartItem } from '../types';
import { cartItemKey, cartKeyOf } from './cartKey';
import { toPersianDigits } from './formatters';

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

// P1(c) sale resolution (shared by PDP, cards and quick view so the
// sale is never applied twice). The backend already folds an active
// sale into VariantPrice.price (onSale=true, price=S, originalPrice=P);
// the frontend only applies the sale itself when the backend has NOT
// (no onSale flag and salePrice below the resolved price). A sale is
// never stacked on a campaign: when onSale=false the campaign/resolved
// price stands, and a configured sale outside its window is ignored.
export interface ResolvedSale {
  price: number;
  originalPrice: number;
  discountPercent: number;
  saleFrom: string | null;
  saleTo: string | null;
  onSale: boolean;
}

export const variantSaleOf = (vp?: VariantPrice, now: Date = new Date()): ResolvedSale | null => {
  if (!vp || vp.salePrice == null || vp.salePrice <= 0) return null;
  const from = vp.saleFrom ?? null;
  const to = vp.saleTo ?? null;
  if (from && new Date(from).getTime() > now.getTime()) return null; // not started yet
  if (to && new Date(to).getTime() <= now.getTime()) return null; // window ended
  if (vp.onSale === false) return null; // backend: campaign won, or sale suspended (S < R)
  if (vp.onSale === true) {
    return { price: vp.price, originalPrice: vp.originalPrice, discountPercent: vp.discountPercent, saleFrom: from, saleTo: to, onSale: true };
  }
  // No onSale flag (pre-sale backend): apply client-side only when the
  // configured sale is actually below the resolved price.
  if (!(vp.salePrice < vp.price)) return null;
  const originalPrice = vp.price;
  return {
    price: vp.salePrice,
    originalPrice,
    discountPercent: Math.round((1 - vp.salePrice / originalPrice) * 100),
    saleFrom: from,
    saleTo: to,
    onSale: true,
  };
};

// Variant image/weight/dims inheritance (P0): a variant photo replaces
// the product gallery (never merges into it); a variant without photos
// falls back to the product gallery. Weight inherits per-field; dims
// inherit as an all-or-none triple — a partial variant triple never
// mixes with the product's dims.
export const variantImageOf = (variant: ProductVariant, product: Product): { image: string; images: string[] } => {
  const variantImages = (variant.images ?? []).filter((src): src is string => typeof src === 'string' && src.trim().length > 0);
  const hero = (typeof variant.imageUrl === 'string' && variant.imageUrl.trim() ? variant.imageUrl : null) ?? variantImages[0] ?? null;
  if (!hero) return { image: product.image, images: product.images };
  return { image: hero, images: variantImages.length > 0 ? variantImages : [hero] };
};

// Display string for the effective weight/dims, e.g.
// «۱٬۲۰۰ گرم · ۴۰×۳۰×۲۰ سانتی‌متر». null when neither the variant nor
// the product carries any of them (the row is then not rendered).
const formatCm = (mm: number): string => {
  const cm = mm / 10;
  return toPersianDigits(Number.isInteger(cm) ? String(cm) : cm.toFixed(1)).replace('.', '٫');
};

export const variantWeightLabel = (variant: ProductVariant | undefined, product: Product): string | null => {
  const weight = variant?.weightGrams ?? product.weightGrams;
  const variantDims = variant && variant.lengthMm != null && variant.widthMm != null && variant.heightMm != null
    ? [variant.lengthMm, variant.widthMm, variant.heightMm] : null;
  const productDims = product.lengthMm != null && product.widthMm != null && product.heightMm != null
    ? [product.lengthMm, product.widthMm, product.heightMm] : null;
  const dims = variantDims ?? productDims;
  const parts: string[] = [];
  if (weight != null && weight > 0) parts.push(`${toPersianDigits(weight.toLocaleString('en-US')).replace(/,/g, '٬')} گرم`);
  if (dims) parts.push(`${formatCm(dims[0])}×${formatCm(dims[1])}×${formatCm(dims[2])} سانتی‌متر`);
  return parts.length > 0 ? parts.join(' · ') : null;
};

// The PDP/quick-view info boxes receive the already variant-adjusted
// displayProduct, so its own fields ARE the effective ones.
export const productWeightLabel = (product: Product): string | null => variantWeightLabel(undefined, product);

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
  const sale = variantSaleOf(price);
  const { image, images } = variantImageOf(variant, base);
  const dimsComplete = variant.lengthMm != null && variant.widthMm != null && variant.heightMm != null;
  return {
    ...base,
    ...(price ? {
      price: sale ? sale.price : price.price,
      originalPrice: sale ? sale.originalPrice : price.originalPrice,
      discountPercent: sale ? sale.discountPercent : price.discountPercent,
      campaignName: price.campaignName ?? product.campaignName,
    } : {}),
    ...(sale ? { saleFrom: sale.saleFrom, saleTo: sale.saleTo } : {}),
    image,
    images,
    weightGrams: variant.weightGrams ?? base.weightGrams,
    lengthMm: dimsComplete ? variant.lengthMm! : base.lengthMm,
    widthMm: dimsComplete ? variant.widthMm! : base.widthMm,
    heightMm: dimsComplete ? variant.heightMm! : base.heightMm,
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

// B1(b): the PDP stock badge must agree with the buy bar. The badge
// used to read the raw stock field, so an inactive product with stock
// 500 showed «۵۰۰ عدد موجود در انبار» while the buy bar (which
// resolves through resolvePurchase) showed it as unbuyable. The badge
// is now derived from the SAME resolvePurchase call: it shows a count
// only when the purchase resolves, «ناموجود» when it does not, and the
// helper keeps 'inactive' distinct so the UI can name it («غیرفعال»)
// instead of pretending the stock ran out.
export type StockBadgeState =
  | { kind: 'in-stock'; stock: number }
  | { kind: 'out-of-stock' }
  | { kind: 'inactive' };

export const resolveStockBadge = (product: Product, variantId?: string): StockBadgeState => {
  if (product.isActive === false) return { kind: 'inactive' };
  const resolution = resolvePurchase(product, variantId);
  if (!resolution) return { kind: 'out-of-stock' };
  return { kind: 'in-stock', stock: resolution.product.stock };
};

// P2(c) fallback chain (single source of truth, shared with cardPriceOf):
// ?spnc= → store default → product isDefault → first purchasable → first.
// A store default that is hidden/inactive/unpriced falls through silently.
export const initialPurchaseVariant = (product: Product, spnc?: string | null, storeDefaultVariantId?: string | null): ProductVariant | undefined => {
  const variants = activeProductVariants(product);
  const linked = spnc ? variants.find((variant) => variant.spnc === spnc) : undefined;
  const storeDefault = storeDefaultVariantId
    ? variants.find((variant) => variant.id === storeDefaultVariantId && variantPurchasable(product, variant))
    : undefined;
  return linked ?? storeDefault
    ?? variants.find((variant) => variant.isDefault && variantPurchasable(product, variant))
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
