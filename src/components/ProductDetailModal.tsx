import React, { useState, useEffect } from 'react';
import { useStore } from '../context/StoreContext';
import { isProductPublishedInStore } from '../utils/storeAssortment';
import { ProductGallery } from './ProductGallery';
import { ProductSpecsList } from './ProductSpecsList';
import { ProductInfoRight } from './ProductInfoRight';
import { ProductVariantSelector } from './ProductVariantSelector';
import { X, ShoppingCart } from 'lucide-react';
import { motion } from 'motion/react';
import { activeProductVariants, initialDetailPurchaseVariant, productForVariant, purchaseLimits, resolvePurchase, cartQuantityAllowed, requiresProductVariant, isHiddenSyntheticVariant, variantPurchasable } from '../utils/productPurchase';

export const ProductDetailModal: React.FC = () => {
  const { selectedProduct, setSelectedProduct, addToCart, toPersianDigits, formatPrice, currentStore, routeStoreSlug, cart } = useStore();
  const [quantity, setQuantity] = useState(1);
  const [selectedVariantId, setSelectedVariantId] = useState<string | null>(null);

  // Store scope mirrors the PDP derivation (ProductDetailView): a
  // routed store slug, or a dropshipper current store. In scope the
  // quick view speaks as the store - the central warehouse is never
  // named on a store surface.
  const inStoreScope = routeStoreSlug != null || currentStore?.ownerType === 'dropshipper';

  // FIX 4 (team 04 G2): pick the default variant on product change,
  // honouring a ?spnc= deep link when it matches a variant, and reset
  // the quantity. Live carries the same ?spnc= route contract.
  useEffect(() => {
    if (!selectedProduct) {
      setSelectedVariantId(null);
      return;
    }
    const variants = activeProductVariants(selectedProduct);
    if (variants.length === 0) {
      setSelectedVariantId(null);
      return;
    }
    let spnc: string | null = null;
    try {
      spnc = new URLSearchParams(window.location.search).get('spnc');
    } catch {}
    const next = initialDetailPurchaseVariant(selectedProduct, spnc);
    setSelectedVariantId(next?.id ?? null);
    setQuantity(next ? purchaseLimits(selectedProduct, next).minQty : 1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedProduct?.id, selectedProduct?.variants]);

  if (!selectedProduct) return null;

  const variants = activeProductVariants(selectedProduct);
  const requiresChoice = requiresProductVariant(selectedProduct);
  const selectedVariant = variants.find((variant) => variant.id === selectedVariantId && !isHiddenSyntheticVariant(selectedProduct, variant))
    ?? initialDetailPurchaseVariant(selectedProduct);
  const selectionNeeded = requiresChoice && !selectedVariant && variants.some(variant =>
    !isHiddenSyntheticVariant(selectedProduct, variant) && variantPurchasable(selectedProduct, variant));
  const displayProduct = productForVariant(selectedProduct, selectedVariant);
  const published = isProductPublishedInStore(currentStore, displayProduct.id);
  const { minQty, maxQty } = purchaseLimits(selectedProduct, selectedVariant);
  const purchase = resolvePurchase(selectedProduct, selectedVariant?.id);
  const outOfStock = !purchase;

  const handleSelectVariant = (id: string) => {
    const variant = variants.find((item) => item.id === id);
    if (!variant || !resolvePurchase(selectedProduct, id)) return;
    setSelectedVariantId(id);
    const { minQty: min, maxQty: max } = purchaseLimits(selectedProduct, variant);
    setQuantity((quantity) => Math.max(min, Math.min(quantity, max)));
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
          {/* P0 fix: the quick view used to pass the base product's
              images, so a variant swap never changed the gallery. The
              variant-adjusted displayProduct carries the right ones. */}
          <ProductGallery
            images={displayProduct.images}
            mainImage={displayProduct.image}
            title={displayProduct.title}
          />

          {requiresChoice && (
            <ProductVariantSelector
              product={selectedProduct}
              variants={variants}
              variantPrices={selectedProduct.variantPrices}
              selectedId={selectedVariant?.id ?? null}
              onSelect={handleSelectVariant}
            />
          )}

          <ProductInfoRight product={displayProduct} />

          <ProductSpecsList
            description={selectedProduct.description}
            specs={displayProduct.specs}
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
              {selectionNeeded ? 'برای خرید، مدل کالا را انتخاب کنید.' : inStoreScope ? 'ناموجود' : 'ناموجود در انبار مرکزی'}
            </div>
          ) : (
            <button
              onClick={() => {
                if (!published || !purchase || !cartQuantityAllowed(cart, purchase, quantity)) return;
                addToCart(displayProduct, quantity, undefined, selectedVariant?.id);
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

