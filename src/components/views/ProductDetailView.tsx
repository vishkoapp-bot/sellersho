import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { isProductPublishedInStore } from '../../utils/storeAssortment';
import { useToast } from '../../context/ToastContext';
import { Product } from '../../types';
import { ProductCard } from '../ProductCard';
import { ProductDetailGallery } from './product/ProductDetailGallery';
import { ProductDetailInfo, ProductDetailTabs } from './product/ProductDetailInfo';
import { catalogApi } from '../../services/catalogApi';
import { storefrontProducts } from '../storefront/templates/templateTypes';
import { mapBackendProduct } from '../../context/ProductContext';
import { ProductVariantSelector } from '../ProductVariantSelector';
import { ArrowRight, Share2, Heart, ShoppingCart, Sparkles } from 'lucide-react';
import { ReturnWindowBadge } from '../ReturnWindowBadge';
import { BuyNowAddressModal } from '../BuyNowAddressModal';

export const ProductDetailView: React.FC<{ product: Product; onBack: () => void }> = ({
  product,
  onBack,
}) => {
  const {
    addToCart,
    formatPrice,
    toPersianDigits,
    toggleWishlist,
    isWishlisted,
    setIsCartOpen,
    startDirectBuy,
    setDirectBuyAddress,
    clearDirectBuy,
    currentStore,
    cartItemsCount,
    cart,
    products,
    routeStoreSlug,
  } = useStore();
  const { success, info, error } = useToast();
  const [qty, setQty] = useState(1);
  const [selectedVariantId, setSelectedVariantId] = useState<string | null>(null);
  // FIX 14b: live PDP addon cards (variant -> addon cards -> buy bar).
  const [selectedAddonIds, setSelectedAddonIds] = useState<string[]>([]);
  const [buyNowModalOpen, setBuyNowModalOpen] = useState(false);
  // FIX 13: live's PDP resolves the product via the DETAIL endpoint, not
  // the catalog list. The old guard (fetch only when warranty is
  // missing) never fired because the mapper falls back to
  // 'ضمانت اصالت کالا', making warranty always truthy — so the prod
  // route kept rendering the fake fallback while spn showed the real
  // warranty. Always fetch the detail DTO so both routes render live's
  // exact copy (real "۱۸ ماه گارانتی شرکتی" warranty, single image,
  // empty specs — the detail DTO has no images/specs keys, like live).
  const [detailProduct, setDetailProduct] = useState<Product | null>(null);
  useEffect(() => {
    setDetailProduct(null);
    let cancelled = false;
    // ISO-E: the detail endpoint is store-priced (?storeSlug): the
    // unscoped fetch showed the central price on every storefront
    // (staging prod-vitamin-serum: 462000/32% unscoped vs
    // 483000/29% for storeSlug=mone). Re-fetch when the store
    // changes, even for the same product id.
    catalogApi.getProductById(product.id, currentStore?.slug)
      .then((dto) => { if (!cancelled) setDetailProduct(mapBackendProduct(dto)); })
      .catch(() => {});
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [product.id, currentStore?.slug]);

  // Related-rail scope: when this PDP is bound to a store (route
  // scope captured from a storefront click, or the current store is
  // itself a dropshipper), the rail must be priced by THAT store.
  // The context catalogue is unscoped (central margins) while the
  // order path charges store prices, so a scoped PDP fetches the
  // store-scoped list with the same mechanism as the detail refetch
  // above (catalogApi keyed by the store slug, mapped the same way).
  // While the scoped list is in flight the rail stays empty - it
  // must never flash central-priced cards first; a failed fetch
  // leaves it empty too. Central PDPs keep the context list below.
  const scopedStoreSlug: string | null =
    routeStoreSlug ??
    (currentStore?.ownerType === 'dropshipper' && currentStore.slug
      ? currentStore.slug
      : null);
  const [scopedRelated, setScopedRelated] = useState<Product[] | null>(null);
  useEffect(() => {
    if (!scopedStoreSlug) {
      setScopedRelated(null);
      return;
    }
    setScopedRelated(null);
    let cancelled = false;
    catalogApi
      .getProducts({ storeSlug: scopedStoreSlug, category: product.category })
      .then((dtos) => { if (!cancelled) setScopedRelated(dtos.map(mapBackendProduct)); })
      .catch(() => { if (!cancelled) setScopedRelated([]); });
    return () => { cancelled = true; };
  }, [scopedStoreSlug, product.id, product.category]);

  // FIX 7: the standalone product page previously ignored variants entirely
  // (FIX 4 wired the selector only into ProductDetailModal). Same live
  // behaviour as the modal: default variant honouring ?spnc=, displayed
  // price/originalPrice/discountPercent/stock from variantPrices[variant.id].
  useEffect(() => {
    const variants = product.variants ?? [];
    if (variants.length === 0) {
      setSelectedVariantId(null);
      setQty(1);
      setSelectedAddonIds([]);
      return;
    }
    let spnc: string | null = null;
    try {
      spnc = new URLSearchParams(window.location.search).get('spnc');
    } catch {}
    const bySpnc = spnc ? variants.find((v) => v.spnc === spnc) : undefined;
    // USER DECISION (Nov 5, 2026): the initial selection is the
    // isDefault variant — it is a real, selectable option now.
    const next = bySpnc ?? variants.find((v) => v.isDefault) ?? variants[0];
    setSelectedVariantId(next.id);
    setSelectedAddonIds([]);
    setQty(next.minCartQty && next.minCartQty > 0 ? next.minCartQty : 1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [product.id]);

  const wishlisted = isWishlisted(product.id);
  const variants = product.variants ?? [];
  const showVariantSelector = variants.length > 1;
  const selectedVariant =
    variants.find((v) => v.id === selectedVariantId) ??
    variants.find((v) => v.isDefault) ??
    variants[0];
  // FIX 33 (reverts FIX 32): live settled prod PDP shows the DETAIL
  // warranty. The 04:02 side note (fallback on live) was the transient
  // load state before the detail fetch resolved. Warranty comes from
  // displayBase again; the mapper fallback covers the transient state.
  const displayBase = detailProduct ?? product;
  const variantPrice = selectedVariant
    ? displayBase.variantPrices?.[selectedVariant.id]
    : undefined;
  const displayProduct =
    selectedVariant && variantPrice
      ? {
          ...displayBase,
          price: variantPrice.price,
          originalPrice: variantPrice.originalPrice ?? displayBase.originalPrice,
          discountPercent: variantPrice.discountPercent ?? displayBase.discountPercent,
          // Live shows the product's overall stock (5) for both variants;
          // the per-variant stock lives in the selector's stock line.
          stock: displayBase.stock,
        }
      : displayBase;
  const minQty =
    selectedVariant?.minCartQty && selectedVariant.minCartQty > 0
      ? selectedVariant.minCartQty
      : 1;
  const maxQty =
    selectedVariant?.maxCartQty ?? selectedVariant?.stock ?? product.stock;
  const outOfStock = selectedVariant ? (selectedVariant.stock || 0) <= 0 : (product.stock || 0) <= 0;
  // FIX 14b: live buy-now price is (price + sum of selected addon
  // fees) x qty; with no addons this is exactly the old price x qty.
  const detailAddons = displayBase.addons ?? [];
  const addonFeePerUnit = detailAddons
    .filter((a) => selectedAddonIds.includes(a.id))
    .reduce((sum, a) => sum + (a.feePerUnit || 0), 0);
  const buyNowUnit = displayProduct.price + addonFeePerUnit;

  const handleSelectVariant = (id: string) => {
    setSelectedVariantId(id);
    const v = variants.find((item) => item.id === id);
    const max = v?.maxCartQty ?? v?.stock ?? displayProduct.stock;
    const min = v?.minCartQty && v.minCartQty > 0 ? v.minCartQty : 1;
    setQty((q) => {
      const clamped = Math.max(min, q);
      return max > 0 ? Math.min(clamped, max) : clamped;
    });
  };
  const related = (scopedStoreSlug ? scopedRelated ?? [] : products)
    .filter((p) => p.category === product.category && p.id !== product.id && isProductPublishedInStore(currentStore, p.id))
    .slice(0, 4);

  const handleShare = () => {
    if (navigator.clipboard) {
      navigator.clipboard.writeText(window.location.href);
      success('لینک کالا در حافظه کپی شد.');
    }
  };

  const publishedInCurrentStore = isProductPublishedInStore(currentStore, product.id);
  const handleAddToCart = () => {
    if (!publishedInCurrentStore) {
      error(`این کالا در «${currentStore?.name}» عرضه نمی‌شود.`);
      return;
    }
    addToCart(displayProduct, qty, undefined, selectedVariantId ?? undefined, selectedAddonIds);
    success(`«${product.title}» (${toPersianDigits(qty)} عدد) به سبد خرید افزوده شد.`);
  };
  // Worker A (buy-now): live's خرید فوری does NOT add to cart. It
  // stages a direct-buy payload (index-MERGED.js PDP _e) and opens
  // the address modal; the drawer then checks out that line only.
  const handleBuyNow = () => {
    if (outOfStock) return;
    if (!currentStore) return;
    if (!publishedInCurrentStore) {
      error(`این کالا در «${currentStore.name}» عرضه نمی‌شود.`);
      return;
    }
    startDirectBuy({
      product: displayProduct,
      qty,
      variantId: selectedVariantId ?? undefined,
      variantLabel: selectedVariant?.name,
      addonIds: selectedAddonIds,
      addonSnapshot: detailAddons,
      store: { id: currentStore.id, name: currentStore.name, slug: currentStore.slug },
    });
    setBuyNowModalOpen(true);
  };

  return (
    <div className="flex-1 flex flex-col bg-white select-none pb-36 lg:pb-8">
      {/* Header Bar */}
      <header className="sticky top-0 z-30 lg:static bg-white border-b border-slate-200 px-3 py-2 lg:py-3 flex items-center justify-between lg:px-8 xl:px-10 2xl:px-12">
        <div className="flex items-center gap-2">
          <button
            onClick={onBack}
            aria-label="بازگشت"
            className="w-11 h-11 rounded-lg hover:bg-slate-100 text-slate-700 transition-colors flex items-center justify-center"
          >
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <span className="text-xs font-bold text-slate-800 truncate max-w-[220px] sm:max-w-[320px] md:max-w-[440px] lg:max-w-none">
            {product.title}
          </span>
        </div>

        <div className="flex items-center gap-1">
          <button
            onClick={handleShare}
            aria-label="اشتراک‌گذاری"
            className="w-11 h-11 text-slate-600 hover:bg-slate-100 rounded-lg flex items-center justify-center"
          >
            <Share2 className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
          <button
            onClick={() => {
              toggleWishlist(product.id);
              if (!wishlisted) success('به لیست علاقه‌مندی‌ها اضافه شد.');
              else info('از لیست علاقه‌مندی‌ها حذف شد.');
            }}
            aria-label="علاقه‌مندی"
            className={`w-11 h-11 rounded-lg flex items-center justify-center ${
              wishlisted ? 'text-rose-500 bg-rose-50' : 'text-slate-600 hover:bg-slate-100'
            }`}
          >
            <Heart className={`w-4 h-4 ${wishlisted ? 'fill-rose-500' : ''}`}  strokeWidth={1.5}/>
          </button>
        </div>
      </header>

      {/* Gallery + Info: live's asymmetric grid (gallery + info column
          360-440px at lg), details left in RTL, stacked on mobile. */}
      <div className="lg:grid lg:grid-cols-[minmax(0,1fr)_minmax(360px,440px)] xl:grid-cols-[minmax(0,620px)_minmax(400px,520px)] 2xl:grid-cols-[minmax(0,760px)_minmax(480px,640px)] lg:gap-8 xl:gap-16 lg:px-8 xl:px-10 2xl:px-12 lg:py-6 xl:justify-center">
      {/* Gallery */}
      <div className="lg:self-stretch">
      <ProductDetailGallery
        mainImage={displayProduct.image}
        images={displayProduct.images}
        title={displayProduct.title}
        discountPercent={displayProduct.discountPercent}
      />
      </div>

      {/* Info column — live order: info (with tabs at its end) ->
          variant box -> buy bar. No sticky bottom bar on desktop. */}
      <div className="p-3.5 space-y-4 lg:p-0">
        <ProductDetailInfo product={displayProduct} />
        <ProductDetailTabs product={displayProduct} />
        {showVariantSelector && selectedVariant && (
          <div className="bg-white border border-slate-200 rounded-2xl p-3">
            <ProductVariantSelector
              variants={variants}
              variantPrices={displayProduct.variantPrices}
              selectedId={selectedVariant.id}
              onSelect={handleSelectVariant}
              storeCount={displayProduct.storeCount}
            />
          </div>
        )}
        {detailAddons.length > 0 && (
          <div className="bg-white border border-slate-200 rounded-2xl p-3 space-y-2">
            <h4 className="text-xs font-bold text-slate-800">{displayBase.addonSectionTitle || 'محصولات مکمل'}</h4>
            {detailAddons.map((addon) => {
              const checked = selectedAddonIds.includes(addon.id);
              return (
                <label key={addon.id} className="flex items-start gap-2 rounded-xl border border-slate-200 px-3 py-2 cursor-pointer hover:bg-slate-50">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => setSelectedAddonIds((prev) => checked ? prev.filter((id) => id !== addon.id) : [...prev, addon.id])}
                    className="w-4 h-4 mt-0.5 shrink-0 accent-brand"
                  />
                  <span className="flex-1 min-w-0">
                    <span className="block text-xs font-bold text-slate-800">{addon.title}</span>
                    {addon.description ? <span className="block text-[11px] text-slate-500">{addon.description}</span> : null}
                  </span>
                  <span className="text-xs font-bold text-brand-deep shrink-0">+{formatPrice(addon.feePerUnit)} ت</span>
                </label>
              );
            })}
          </div>
        )}
        <div className="fixed bottom-16 inset-x-0 max-w-[504px] mx-auto bg-white border-t border-slate-200 p-2.5 flex items-center justify-between gap-2 z-50 shadow-[0_-4px_16px_rgba(0,0,0,0.06)] lg:static lg:max-w-none lg:shadow-none lg:border-0 lg:px-0 lg:py-4 lg:flex-col lg:items-stretch lg:gap-3">
          <div className="flex items-center bg-slate-100 rounded-xl border border-slate-200 shrink-0 lg:justify-between">
            <button
              onClick={() => setQty(Math.max(minQty, qty - 1))}
              disabled={outOfStock}
              aria-label="کاهش تعداد"
              className="min-w-[44px] min-h-[44px] flex items-center justify-center text-slate-700 font-bold disabled:opacity-40 hover:bg-slate-200 rounded-r-xl transition-colors"
            >
              -
            </button>
            <span className="px-2.5 font-bold text-xs">{toPersianDigits(qty)}</span>
            <button
              onClick={() => setQty(maxQty > 0 ? Math.min(maxQty, qty + 1) : qty + 1)}
              disabled={outOfStock || (maxQty > 0 && qty >= maxQty)}
              aria-label="افزایش تعداد"
              className="min-w-[44px] min-h-[44px] flex items-center justify-center text-slate-700 font-bold disabled:opacity-40 hover:bg-slate-200 rounded-l-xl transition-colors"
            >
              +
            </button>
          </div>
          {outOfStock ? (
            <div className="flex-1 py-3.5 bg-slate-200 text-slate-500 font-bold text-xs rounded-xl text-center">
              ناموجود در انبار مرکزی
            </div>
          ) : !publishedInCurrentStore ? (
            <div className="flex-1 py-3.5 bg-slate-100 text-slate-500 font-bold text-xs rounded-xl text-center">
              این کالا در فروشگاه «{currentStore?.name}» عرضه نمی‌شود
            </div>
          ) : (
            <div className="flex-1 flex items-center gap-1.5">
              <button
                onClick={handleAddToCart}
                title="افزودن کالا به سبد خرید"
                className="flex-1 py-3.5 lg:py-4 bg-brand-soft hover:bg-brand-border text-brand border border-brand-border font-bold text-xs lg:text-sm xl:text-base rounded-xl shadow-2xs flex items-center justify-center gap-1 transition-colors"
              >
                <ShoppingCart className="w-4 h-4"  strokeWidth={1.5}/>
                <span>افزودن به سبد</span>
              </button>
              <button
                onClick={handleBuyNow}
                title="خرید فوری بدون سبد خرید"
                className="flex-1 py-3.5 lg:py-4 bg-brand hover:bg-brand-deep text-white font-extrabold text-xs lg:text-sm xl:text-base rounded-xl shadow-xs flex items-center justify-center gap-1 transition-colors"
              >
                <span>خرید فوری</span>
                <span className="text-[10px] lg:text-xs opacity-90">
                  {/* FIX 22: live buy button uses full «تومان», not abbreviated «ت». */}
                  ({formatPrice(buyNowUnit * qty)} تومان)
                </span>
              </button>
            </div>
          )}
        </div>
        <BuyNowAddressModal
          open={buyNowModalOpen}
          item={{ title: product.title, qty, totalPrice: buyNowUnit * qty, ...(selectedVariant ? { variantLabel: selectedVariant.name } : {}) }}
          onClose={() => { setBuyNowModalOpen(false); clearDirectBuy(); }}
          onConfirm={(address) => { setDirectBuyAddress(address); setBuyNowModalOpen(false); setIsCartOpen(true); }}
        />
        {/* Team 08 (D62): return-window badge, only when Team 03's
            returnWindowEndsAt data is present on the product. */}
        {product.returnWindowEndsAt !== undefined && (
          <div>
            <ReturnWindowBadge returnWindowEndsAt={product.returnWindowEndsAt} />
          </div>
        )}

      </div>
      {related.length > 0 && (
        <div className="px-3.5 -mt-3.5 pb-3.5 lg:px-0 lg:pb-0 lg:mt-0 lg:col-span-2">
          <div className="pt-3 border-t border-slate-100 space-y-2">
          <h3 className="text-base lg:text-lg xl:text-xl font-bold text-slate-800 flex items-center gap-1">
            <Sparkles className="w-3.5 h-3.5 text-brand-deep"  strokeWidth={1.5}/>
            <span>کالاهای مرتبط و مشابه</span>
          </h3>
          <div className="grid grid-cols-2 gap-2 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
            {related.map((p) => (
              <ProductCard key={p.id} product={p} />
            ))}
          </div>
          </div>
        </div>
      )}
      </div>

    </div>
  );
};
