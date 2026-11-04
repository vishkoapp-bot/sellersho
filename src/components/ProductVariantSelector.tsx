import React from 'react';
import { ProductVariant, VariantPrice } from '../types';
import { useStore } from '../context/StoreContext';

interface ProductVariantSelectorProps {
  variants: ProductVariant[];
  variantPrices?: Record<string, VariantPrice>;
  selectedId: string | null;
  onSelect: (id: string) => void;
  storeCount?: number;
}

// FIX 4 (team 04 G2): live product pages offer a variant selector.
// Selecting a variant overrides the displayed price/originalPrice/
// discountPercent/stock from variantPrices[variant.id] (live bundle
// behaviour), exactly like the candidate's detail modal now does.
export const ProductVariantSelector: React.FC<ProductVariantSelectorProps> = ({
  variants,
  variantPrices,
  selectedId,
  onSelect,
  storeCount = 0,
}) => {
  const { toPersianDigits } = useStore();

  // FIX 10 (live parity, proven from the live bundle): live's selector
  // renders only non-default variants (dd = filter(!isDefault)) and only
  // shows at all when one exists (no = some(!isDefault)). The API still
  // carries the isDefault placeholder (e.g. gold bracelet "پیش‌فرض"), but
  // live shows only ساده and نگین‌دار for it. Same rule here.
  const visibleVariants = variants.some((v) => !v.isDefault)
    ? variants.filter((v) => !v.isDefault)
    : [];
  if (visibleVariants.length === 0) return null;

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
          <span className="font-bold text-slate-700">{attrKey}</span>
          <span className="text-slate-400">: {String(attrValue)}</span>
        </p>
      ) : (
        <p className="text-xs font-bold text-slate-800">{label}</p>
      )}
      <div className="flex flex-wrap gap-2">
        {visibleVariants.map((variant) => {
          const selected = variant.id === selectedId;
          const unavailable = (variant.stock || 0) <= 0;
          return (
            <button
              key={variant.id}
              type="button"
              onClick={() => onSelect(variant.id)}
              aria-pressed={selected}
              disabled={unavailable && !selected}
              className={`min-h-[36px] px-3 rounded-xl border text-[11px] font-bold transition-all ${
                selected
                  ? 'text-white shadow-xs'
                  : unavailable
                    ? 'bg-slate-50 text-slate-400 border-slate-100 opacity-40 cursor-not-allowed'
                    : 'bg-white text-slate-700 border-slate-200 hover:border-slate-300'
              }`}
              style={selected ? { backgroundColor: '#5d44f8', borderColor: '#5d44f8' } : undefined}
            >
              <span className="block">{labelOf(variant)}</span>
            </button>
          );
        })}
      </div>
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
