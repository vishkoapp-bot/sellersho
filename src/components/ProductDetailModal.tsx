import React, { useState, useEffect } from 'react';
import { useStore } from '../context/StoreContext';
import { isProductPublishedInStore } from '../utils/storeAssortment';
import { ProductGallery } from './ProductGallery';
import { ProductSpecsList } from './ProductSpecsList';
import { ProductInfoRight } from './ProductInfoRight';
import { ProductVariantSelector } from './ProductVariantSelector';
import { X, ShoppingCart } from 'lucide-react';
import { motion } from 'motion/react';

export const ProductDetailModal: React.FC = () => {
  const { selectedProduct, setSelectedProduct, addToCart, toPersianDigits, formatPrice, currentStore } = useStore();
  const [quantity, setQuantity] = useState(1);
  const [selectedVariantId, setSelectedVariantId] = useState<string | null>(null);

  // FIX 4 (team 04 G2): pick the default variant on product change,
  // honouring a ?spnc= deep link when it matches a variant, and reset
  // the quantity. Live carries the same ?spnc= route contract.
  useEffect(() => {
    if (!selectedProduct) {
      setSelectedVariantId(null);
      return;
    }
    const variants = selectedProduct.variants ?? [];
    if (variants.length === 0) {
      setSelectedVariantId(null);
      return;
    }
    let spnc: string | null = null;
    try {
      spnc = new URLSearchParams(window.location.search).get('spnc');
    } catch {}
    const bySpnc = spnc ? variants.find((v) => v.spnc === spnc) : undefined;
    // USER DECISION (Nov 5, 2026): initial selection is the isDefault variant.
    const next = bySpnc ?? variants.find((v) => v.isDefault) ?? variants[0];
    setSelectedVariantId(next.id);
    setQuantity(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedProduct?.id]);

  if (!selectedProduct) return null;

  // FIX 4: live variant behaviour — the selected variant overrides the
  // displayed price/originalPrice/discountPercent/stock from
  // variantPrices[variant.id]. FIX 5: the cart now keys on
  // product::variant, so two variants of one product no longer merge.
  const variants = selectedProduct.variants ?? [];
  const showVariantSelector = variants.length > 1;
  const selectedVariant =
    variants.find((v) => v.id === selectedVariantId) ??
    variants.find((v) => v.isDefault) ??
    variants[0];
  const variantPrice = selectedVariant
    ? selectedProduct.variantPrices?.[selectedVariant.id]
    : undefined;
  const displayProduct =
    selectedVariant && variantPrice
      ? {
          ...selectedProduct,
          price: variantPrice.price,
          originalPrice: variantPrice.originalPrice ?? selectedProduct.originalPrice,
          discountPercent: variantPrice.discountPercent ?? selectedProduct.discountPercent,
          stock: selectedVariant.stock,
        }
      : selectedProduct;
  const published = isProductPublishedInStore(currentStore, displayProduct.id);
  const minQty =
    selectedVariant?.minCartQty && selectedVariant.minCartQty > 0
      ? selectedVariant.minCartQty
      : 1;
  const maxQty =
    selectedVariant?.maxCartQty ?? selectedVariant?.stock ?? selectedProduct.stock;
  const outOfStock = (displayProduct.stock || 0) <= 0;

  const handleSelectVariant = (id: string) => {
    setSelectedVariantId(id);
    const v = variants.find((item) => item.id === id);
    const max = v?.maxCartQty ?? v?.stock ?? displayProduct.stock;
    const min = v?.minCartQty && v.minCartQty > 0 ? v.minCartQty : 1;
    setQuantity((q) => {
      const clamped = Math.max(min, q);
      return max > 0 ? Math.min(clamped, max) : clamped;
    });
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-2 sm:p-4 bg-slate-900/60 backdrop-blur-xs">
      <motion.div
        initial={{ opacity: 0, scale: 0.96, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[480px] w-full overflow-hidden shadow-2xl border border-slate-100 flex flex-col max-h-[92vh]"
      >
        {/* Header */}
        <div className="p-3.5 border-b border-slate-100 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-xs font-bold text-slate-700">جزئیات و مشخصات کالا</span>
            <span className="text-[10px] font-semibold bg-slate-100 text-slate-500 px-2 py-0.5 rounded-md">
              کد: {selectedProduct.id}
            </span>
          </div>
          <button
            onClick={() => setSelectedProduct(null)}
            className="p-1.5 rounded-full hover:bg-slate-100 text-slate-500 transition-colors"
          >
            <X className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>

        {/* Scrollable Content inside mobile shell */}
        <div className="p-4 overflow-y-auto no-scrollbar space-y-4">
          <ProductGallery
            images={selectedProduct.images}
            mainImage={selectedProduct.image}
            title={selectedProduct.title}
          />

          {showVariantSelector && selectedVariant && (
            <ProductVariantSelector
              variants={variants}
              variantPrices={selectedProduct.variantPrices}
              selectedId={selectedVariant.id}
              onSelect={handleSelectVariant}
            />
          )}

          <ProductInfoRight product={displayProduct} />

          <ProductSpecsList
            description={selectedProduct.description}
            specs={selectedProduct.specs}
          />
        </div>

        {/* Bottom Sticky Action Bar */}
        <div className="p-3 bg-slate-50 border-t border-slate-200/80 flex items-center justify-between gap-3">
          {/* Quantity Stepper */}
          <div className="flex items-center bg-white rounded-xl border border-slate-200 overflow-hidden shadow-2xs">
            <button
              onClick={() => setQuantity(Math.max(minQty, quantity - 1))}
              disabled={outOfStock || quantity <= minQty}
              className="px-2.5 py-1.5 text-slate-600 hover:bg-slate-100 text-sm font-bold active:scale-95 disabled:opacity-40"
            >
              -
            </button>
            <span className="px-2.5 py-1 text-xs font-bold text-slate-800 min-w-[24px] text-center">
              {toPersianDigits(quantity)}
            </span>
            <button
              onClick={() => setQuantity(maxQty > 0 ? Math.min(maxQty, quantity + 1) : quantity + 1)}
              disabled={outOfStock || (maxQty > 0 && quantity >= maxQty)}
              className="px-2.5 py-1.5 text-slate-600 hover:bg-slate-100 text-sm font-bold active:scale-95 disabled:opacity-40"
            >
              +
            </button>
          </div>

          {/* Add to Cart with Installment Info */}
          {(outOfStock) ? (
            <div className="flex-1 py-2.5 px-4 bg-slate-200 text-slate-500 font-bold text-xs rounded-xl text-center">
              ناموجود در انبار مرکزی
            </div>
          ) : (
            <button
              onClick={() => {
                if (!published) return;
                addToCart(displayProduct, quantity, undefined, selectedVariantId ?? undefined);
                setSelectedProduct(null);
              }}
              disabled={!published}
              title={published ? undefined : 'در این فروشگاه عرضه نمی‌شود'}
              className="flex-1 flex items-center justify-between gap-2 bg-[#007dfa] hover:bg-[#0069d9] text-white font-bold text-xs py-2.5 px-4 rounded-xl shadow-md shadow-blue-500/20 active:scale-98 transition-all"
            >
              <div className="flex items-center gap-1.5">
                <ShoppingCart className="w-4 h-4"  strokeWidth={1.5}/>
                <span>افزودن به سبد خرید</span>
              </div>
              <div className="text-[11px] font-black">
                {formatPrice(displayProduct.price * quantity)} تومان
              </div>
            </button>
          )}
        </div>
      </motion.div>
    </div>
  );
};

