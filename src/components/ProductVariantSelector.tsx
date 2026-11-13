import React from 'react';
import { Product, ProductVariant, VariantPrice } from '../types';
import { toPersianDigits } from '../utils/formatters';
import { activeProductVariants, axisOptionVariant, purchaseLimits } from '../utils/productPurchase';

interface ProductVariantSelectorProps {
  product?: Product;
  variants: ProductVariant[];
  variantPrices?: Record<string, VariantPrice>;
  selectedId: string | null;
  onSelect: (id: string) => void;
  storeCount?: number;
  // PDP template bridge: 'fashion' restyles the chips with the
  // Aastin token set (ink selection, hairline borders, square
  // corners). Default keeps the platform look byte-for-byte, so the
  // detail modal and every other consumer are untouched.
  theme?: 'default' | 'fashion';
}

// FIX 4 (team 04 G2): live product pages offer a variant selector.
// Selecting a variant overrides the displayed price/originalPrice/
// discountPercent/stock from variantPrices[variant.id] (live bundle
// behaviour), exactly like the candidate's detail modal now does.
export const ProductVariantSelector: React.FC<ProductVariantSelectorProps> = ({
  product,
  variants,
  variantPrices,
  selectedId,
  onSelect,
  storeCount = 0,
  theme = 'default',
}) => {
  const fashion = theme === 'fashion';

  // USER DECISION (Nov 5, 2026 — supersedes FIX 10 live parity): the
  // isDefault variant is a normal selectable option like any other.
  // It can carry its own stock and seller margin (the gold bracelet's
  // «پیش‌فرض» has stock 5 and its own 15% margin), so hiding it made
  // that stock unpurchasable. The selector shows whenever a real
  // choice exists (2+ variants), with every variant as a chip.
  const visibleVariants = variants.filter((variant) => variant.isActive !== false);
  const axes = product?.schemaVersion === 2 && product.productType === 'variable'
    ? product.productSchema?.attributes.filter((attribute) => attribute.role === 'axis') ?? [] : [];
  const chipClass = (selected: boolean, unavailable: boolean) => `min-h-[36px] px-3 ${fashion ? 'rounded-none' : 'rounded-xl'} border text-[11px] font-bold transition-all ${selected ? 'text-white' : unavailable ? 'bg-slate-50 text-slate-400 border-slate-100 opacity-40 cursor-not-allowed' : fashion ? 'bg-white text-[#1c1c1a] border-[#e5e0d8] hover:border-[#1c1c1a]' : 'bg-white text-slate-700 border-slate-200 hover:border-slate-300'}`;
  const selectedStyle = { backgroundColor: fashion ? '#1c1c1a' : '#5d44f8', borderColor: fashion ? '#1c1c1a' : '#5d44f8' };
  if (product && axes.length > 0) {
    const selection = activeProductVariants(product).find((variant) => variant.id === selectedId)?.selection ?? {};
    // P2(c): the selected variant is this store's default — a quiet line
    // under the chips (the global isDefault carries no extra label).
    const selectedVariantV2 = activeProductVariants(product).find((variant) => variant.id === selectedId);
    const isStoreDefaultV2 = !!product.storeDefaultVariantId && selectedVariantV2?.id === product.storeDefaultVariantId;
    return <div data-testid="variant-selector" className="space-y-4">
      {axes.map((axis) => <fieldset key={axis.id} className="space-y-2" data-axis-id={axis.id}>
        <legend className={`text-xs font-bold ${fashion ? 'text-[#1c1c1a]' : 'text-slate-800'}`}>{axis.name}</legend>
        <div className="flex flex-wrap gap-2">
          {axis.values.map((value) => {
            const target = axisOptionVariant(product, selectedId, axis.id, value.id);
            const selected = selection[axis.id] === value.id;
            // P0: on the colour axis only, a chip whose target variant
            // has a photo carries a 20×20 thumbnail (an indicator — the
            // main gallery still swaps on selection).
            const thumb = axis.id === 'color'
              ? (target?.imageUrl ?? target?.images?.[0] ?? null)
              : null;
            return <button key={value.id} type="button" aria-pressed={selected}
              disabled={!target} onClick={() => target && onSelect(target.id)}
              className={chipClass(selected, !target)} style={selected ? selectedStyle : undefined}>
              <span className="flex items-center gap-1.5">
                {thumb && <img src={thumb} alt="" className="w-5 h-5 rounded-full object-cover shrink-0" onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }} />}
                <span>{value.label}</span>
              </span>
            </button>;
          })}
        </div>
      </fieldset>)}
      {isStoreDefaultV2 && (
        <p className="text-[10px] text-slate-400">پیش‌فرض این فروشگاه</p>
      )}
    </div>;
  }
  if (visibleVariants.length < 2) return null;

  const labelOf = (variant: ProductVariant): string => {
    const attrs = Object.values(variant.attributes || {})
      .filter((v) => typeof v === 'string' || typeof v === 'number')
      .map(String)
      .join(' • ');
    const name = variant.variantName || variant.name;
    if (attrs && attrs !== name) return `${name} (${attrs})`;
    return name;
  };

  // Live label format (proven from the live product page): the variant
  // attribute key plus the selected value, e.g. "style: ساده" — not a
  // generic "choose model" heading, and buttons carry no price/stock.
  const selectedVariant =
    visibleVariants.find((v) => v.id === selectedId) ?? visibleVariants[0];
  const attrKey = (() => {
    const keys = visibleVariants.flatMap((v) => Object.keys(v.attributes || {}));
    return keys.find((k) => k) || null;
  })();
  const attrValue = selectedVariant && attrKey
    ? (selectedVariant.attributes as Record<string, unknown>)[attrKey]
    : null;
  const label = attrKey && attrValue != null
    ? `${attrKey}: ${String(attrValue)}`
    : labelOf(selectedVariant);
  return (
    <div data-testid="variant-selector" className="space-y-3">
      {attrKey && attrValue != null ? (
        <p className="flex items-center gap-1.5 text-[11px]">
          <span className={fashion ? 'font-bold text-[#1c1c1a]' : 'font-bold text-slate-700'}>{attrKey}</span>
          <span className={fashion ? 'text-[#737373]' : 'text-slate-400'}>: {String(attrValue)}</span>
        </p>
      ) : (
        <p className={fashion ? 'text-xs font-bold text-[#1c1c1a]' : 'text-xs font-bold text-slate-800'}>{label}</p>
      )}
      <div className="flex flex-wrap gap-2">
        {visibleVariants.map((variant) => {
          const selected = variant.id === selectedId;
          const unavailable = variant.isActive === false || (product ? purchaseLimits(product, variant).maxQty < purchaseLimits(product, variant).minQty : variant.inventoryMode === 'tracked' && variant.stock <= 0);
          // P0: legacy colour variants get the same thumbnail indicator
          // as the V2 colour axis (only when a colour attribute exists).
          const isColourVariant = Object.keys(variant.attributes || {}).some((k) => k === 'رنگ' || k.toLowerCase() === 'color');
          const thumb = isColourVariant ? (variant.imageUrl ?? variant.images?.[0] ?? null) : null;
          return (
            <button
              key={variant.id}
              type="button"
              onClick={() => onSelect(variant.id)}
              aria-pressed={selected}
              disabled={unavailable && !selected}
              className={`min-h-[36px] px-3 ${fashion ? 'rounded-none' : 'rounded-xl'} border text-[11px] font-bold transition-all ${
                selected
                  ? fashion
                    ? 'text-white'
                    : 'text-white shadow-xs'
                  : unavailable
                    ? fashion
                      ? 'bg-[#f2f2f2] text-[#737373] border-[#e5e0d8] opacity-40 cursor-not-allowed'
                      : 'bg-slate-50 text-slate-400 border-slate-100 opacity-40 cursor-not-allowed'
                    : fashion
                      ? 'bg-white text-[#1c1c1a] border-[#e5e0d8] hover:border-[#1c1c1a]'
                      : 'bg-white text-slate-700 border-slate-200 hover:border-slate-300'
              }`}
              style={selected ? { backgroundColor: fashion ? '#1c1c1a' : '#5d44f8', borderColor: fashion ? '#1c1c1a' : '#5d44f8' } : undefined}
            >
              <span className="flex items-center gap-1.5">
                {thumb && <img src={thumb} alt="" className="w-5 h-5 rounded-full object-cover shrink-0" onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = 'none'; }} />}
                <span className="block">{labelOf(variant)}</span>
              </span>
            </button>
          );
        })}
      </div>
      {product?.storeDefaultVariantId && selectedVariant?.id === product.storeDefaultVariantId && (
        <p className="text-[10px] text-slate-400">پیش‌فرض این فروشگاه</p>
      )}
      {selectedVariant && selectedVariant.stock > 0 && selectedVariant.stock <= 3 && (
        <p className="text-[10px] font-bold text-rose-600">
          {storeCount > 0
            ? `از این تنوع ${toPersianDigits(selectedVariant.stock)} عدد در ${toPersianDigits(storeCount)} فروشگاه باقی مانده است.`
            : `از این تنوع ${toPersianDigits(selectedVariant.stock)} عدد باقی مانده است.`}
        </p>
      )}
    </div>
  );
};
