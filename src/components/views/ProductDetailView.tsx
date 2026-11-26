import React, { useState, useEffect, useRef, useId } from 'react';
import { useStore } from '../../context/StoreContext';
import { isProductPublishedInStore } from '../../utils/storeAssortment';
import { useToast } from '../../context/ToastContext';
import { Product } from '../../types';
import { activeProductVariants, initialDetailPurchaseVariant, isHiddenSyntheticVariant, productForVariant, purchaseLimits, resolvePurchase, cartQuantityAllowed, requiresProductVariant, variantPurchasable } from '../../utils/productPurchase';
import { ProductDetailRelated } from './product/ProductDetailRelated';
import { ProductDetailGallery } from './product/ProductDetailGallery';
import { ProductDetailAssurances, ProductDetailInfo, ProductDetailTabs } from './product/ProductDetailInfo';
import { catalogApi } from '../../services/catalogApi';
import { buildProductUrl } from '../../utils/routing';
import { NotFoundView } from '../NotFoundView';
import { resolvePdpTheme } from './product/pdpTheme';
import { mapBackendProduct } from '../../context/ProductContext';
import { ProductVariantSelector } from '../ProductVariantSelector';
import { ArrowRight, Share2, Heart, ShoppingCart, Minus, Plus, PackageCheck, PackageX, Zap } from 'lucide-react';
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
    cart,
    products,
    openProductDetail,
    routeStoreSlug,
    routeStoreResolved,
    routeStoreInvalid,
    stores,
  } = useStore();
  const { success, info, error } = useToast();
  // PDP store scope: the route scope (a /store/<slug>/product/ URL or
  // a storefront click) wins; otherwise a dropshipper current store
  // scopes its own PDP. Derived BEFORE the detail fetch so a cold
  // store-product URL is store-priced from the very first request -
  // keying the fetch on currentStore alone flashed the central price
  // until the context alignment installed the routed store.
  const scopedStoreSlug: string | null =
    routeStoreSlug ??
    (currentStore?.ownerType === 'dropshipper' && currentStore.slug
      ? currentStore.slug
      : null);
  const central = scopedStoreSlug == null;
  const buyBarRef = useRef<HTMLDivElement>(null);
  const purchaseRef = useRef<HTMLElement>(null);
  const purchaseId = useId();
  const [purchaseInView, setPurchaseInView] = useState(true);
  const [showFloatingBuyBar, setShowFloatingBuyBar] = useState(false);
  useEffect(() => {
    if (central || typeof IntersectionObserver === 'undefined') return;
    let previousY = window.scrollY;
    let observedBar: HTMLDivElement | null = null;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some(entry => entry.isIntersecting)) setShowFloatingBuyBar(false);
    });
    setShowFloatingBuyBar(false);
    const updateBuyBar = () => {
      const currentY = window.scrollY;
      const bar = buyBarRef.current;
      if (bar !== observedBar) {
        observer.disconnect();
        if (bar) observer.observe(bar);
        observedBar = bar;
      }
      const hasPassedBuyBar = Boolean(bar && bar.getBoundingClientRect().bottom <= 0);
      if (currentY !== previousY || !hasPassedBuyBar) {
        setShowFloatingBuyBar(hasPassedBuyBar && currentY > previousY);
      }
      previousY = currentY;
    };
    window.addEventListener('scroll', updateBuyBar, { passive: true });
    window.addEventListener('resize', updateBuyBar);
    return () => {
      observer.disconnect();
      window.removeEventListener('scroll', updateBuyBar);
      window.removeEventListener('resize', updateBuyBar);
    };
  }, [central, product.id, routeStoreSlug]);

  const [quantity, setQty] = useState(1);
  const [variantChoice, setVariantChoice] = useState<{ productId: string; id: string; linkedSpnc: string | null }>();
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
  // P0-2: a scoped detail fetch that fails must surface, never silently
  // keep the unscoped product prop as if it were store-priced.
  const [detailFailed, setDetailFailed] = useState(false);
  const [detailNonce, setDetailNonce] = useState(0);
  // M6 (round 2): the PDP loading gate must never spin forever — if
  // the routed store stays unresolved for 15s, show an error with a
  // retry instead of an infinite spinner (a full reload recovers:
  // cold load re-resolves the slug).
  const [storeResolveTimedOut, setStoreResolveTimedOut] = useState(false);
  useEffect(() => {
    setStoreResolveTimedOut(false);
    if (!routeStoreSlug || routeStoreResolved) return;
    const t = setTimeout(() => setStoreResolveTimedOut(true), 15000);
    return () => clearTimeout(t);
  }, [routeStoreSlug, routeStoreResolved]);
  useEffect(() => {
    const purchase = buyBarRef.current;
    if (!central || !purchase || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(([entry]) => {
      setPurchaseInView(entry.isIntersecting);
    }, { rootMargin: '0px 0px -68px 0px' });
    observer.observe(purchase);
    return () => observer.disconnect();
  }, [central, product.id]);

  useEffect(() => {
    setDetailProduct(null);
    setDetailFailed(false);
    let cancelled = false;
    // ISO-E: the detail endpoint is store-priced (?storeSlug): the
    // unscoped fetch showed the central price on every storefront
    // (staging prod-vitamin-serum: 462000/32% unscoped vs
    // 483000/29% for storeSlug=mone). Re-fetch when the store
    // changes, even for the same product id.
    // Anonymous-safe by construction: catalogApi attaches an
    // Authorization header only when a token exists, and the detail
    // route is OptionalAuth (curl-proven 200 anonymous, 2026-11-13).
    catalogApi.getProductById(product.id, scopedStoreSlug ?? currentStore?.slug)
      .then((dto) => { if (!cancelled) setDetailProduct(mapBackendProduct(dto)); })
      .catch(() => { if (!cancelled) setDetailFailed(true); });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [product.id, scopedStoreSlug, currentStore?.slug, detailNonce]);

  // Related-rail scope: when this PDP is bound to a store (the scope
  // derived above), the rail must be priced by THAT store. The
  // context catalogue is unscoped (central margins) while the order
  // path charges store prices, so a scoped PDP fetches the
  // store-scoped list with the same mechanism as the detail refetch
  // above (catalogApi keyed by the store slug, mapped the same way).
  // While the scoped list is in flight the rail stays empty - it
  // must never flash central-priced cards first; a failed fetch
  // leaves it empty too. Central PDPs keep the context list below.
  // PDP template bridge: a store-bound PDP adopts that store's
  // template identity (product/pdpTheme.ts) instead of the central
  // design. The themed store is the one the scope names - the aligned
  // currentStore when it already is that store, else the store-list
  // entry (the context alignment effect installs it as currentStore
  // a tick later and this derivation follows). No scope -> the
  // default theme -> every class below is today's string.
  const pdpStore =
    scopedStoreSlug == null
      ? undefined
      : currentStore?.slug === scopedStoreSlug
        ? currentStore
        : stores.find((s) => s.slug === scopedStoreSlug);
  const pdpTheme = resolvePdpTheme(pdpStore);
  const fashion = pdpTheme.fashion;
  const [scopedRelated, setScopedRelated] = useState<Product[] | null>(null);
  useEffect(() => {
    if (!scopedStoreSlug) {
      setScopedRelated(null);
      return;
    }
    setScopedRelated(null);
    let cancelled = false;
    // PERF-01: related row is one first page (limit 12).
    catalogApi
      .getProductsPage({ storeSlug: scopedStoreSlug, category: product.category, limit: 12 })
      .then((page) => { if (!cancelled) setScopedRelated(page.items.map(mapBackendProduct)); })
      .catch(() => { if (!cancelled) setScopedRelated([]); });
    return () => { cancelled = true; };
  }, [scopedStoreSlug, product.id, product.category]);

  const displayBase = detailProduct?.id === product.id ? detailProduct : product;
  const variants = activeProductVariants(displayBase);
  const visibleVariants = variants.filter(variant => !isHiddenSyntheticVariant(displayBase, variant));
  const linkedSpnc = typeof window === 'undefined' ? null : new URLSearchParams(window.location.search).get('spnc');
  const manualVariant = variantChoice?.productId === displayBase.id && variantChoice.linkedSpnc === linkedSpnc
    ? visibleVariants.find(variant => variant.id === variantChoice.id)
    : undefined;
  const selectedVariant = manualVariant ?? initialDetailPurchaseVariant(displayBase, linkedSpnc);
  const requiresChoice = requiresProductVariant(displayBase);
  const selectionNeeded = requiresChoice && !selectedVariant && visibleVariants.some(variant => variantPurchasable(displayBase, variant));
  useEffect(() => {
    setSelectedAddonIds([]);
    setQty(1);
  }, [product.id]);

  const wishlisted = isWishlisted(product.id, pdpStore);
  const showVariantSelector = requiresChoice && visibleVariants.length > 0;
  const displayProduct = productForVariant(displayBase, selectedVariant);
  const { minQty, maxQty } = purchaseLimits(displayBase, selectedVariant);
  const qty = Math.max(minQty, Math.min(quantity, maxQty));
  const purchase = resolvePurchase(displayBase, selectedVariant?.id);
  const outOfStock = !purchase;
  // FIX 14b: live buy-now price is (price + sum of selected addon
  // fees) x qty; with no addons this is exactly the old price x qty.
  const detailAddons = displayBase.addons ?? [];
  const addonFeePerUnit = detailAddons
    .filter((a) => selectedAddonIds.includes(a.id))
    .reduce((sum, a) => sum + (a.feePerUnit || 0), 0);
  const buyNowUnit = displayProduct.price + addonFeePerUnit;

  const handleSelectVariant = (id: string) => {
    const variant = variants.find((item) => item.id === id);
    if (!variant || !resolvePurchase(displayBase, id)) return;
    setVariantChoice({ productId: displayBase.id, id, linkedSpnc });
    const { minQty: min, maxQty: max } = purchaseLimits(displayBase, variant);
    setQty(Math.max(min, Math.min(qty, max)));
  };
  const related = (scopedStoreSlug ? scopedRelated ?? [] : products)
    .filter((p) => p.category === product.category && p.id !== product.id && isProductPublishedInStore(currentStore, p.id))
    .slice(0, 4);

  const handleShare = async () => {
    const url = window.location.origin + buildProductUrl(displayBase, scopedStoreSlug ?? undefined)
      + (selectedVariant?.spnc ? `?spnc=${encodeURIComponent(selectedVariant.spnc)}` : '');
    if (navigator.share) {
      try {
        await navigator.share({ title: displayBase.title, url });
        return;
      } catch (failure) {
        if (failure instanceof DOMException && failure.name === 'AbortError') return;
      }
    }
    try {
      await navigator.clipboard.writeText(url);
      success('لینک کالا کپی شد.');
    } catch {
      error('اشتراک‌گذاری انجام نشد. می‌توانید نشانی صفحه را از نوار مرورگر کپی کنید.');
    }
  };

  const publishedInCurrentStore = isProductPublishedInStore(currentStore, product.id);
  const handleAddToCart = () => {
    // P0-2: never add while the routed store is still resolving — the
    // line would be stamped with the wrong store's cart bucket.
    if (routeStoreSlug && currentStore?.slug !== routeStoreSlug) return;
    if (!publishedInCurrentStore) {
      error(`این کالا در «${currentStore?.name}» عرضه نمی‌شود.`);
      return;
    }
    if (!purchase || !cartQuantityAllowed(cart, purchase, qty)) {
      error('تعداد انتخاب‌شده با موجودی یا محدودیت خرید این تنوع سازگار نیست.');
      return;
    }
    addToCart(displayProduct, qty, undefined, selectedVariant?.id, selectedAddonIds);
    success(`«${product.title}» (${toPersianDigits(qty)} عدد) به سبد خرید افزوده شد.`);
  };
  // Worker A (buy-now): live's خرید فوری does NOT add to cart. It
  // stages a direct-buy payload (index-MERGED.js PDP _e) and opens
  // the address modal; the drawer then checks out that line only.
  const handleBuyNow = () => {
    if (!purchase || !cartQuantityAllowed([], purchase, qty)) return;
    if (!currentStore) return;
    // P0-2: same guard as add-to-cart — a direct buy must not be
    // staged under the wrong store while the route store resolves.
    if (routeStoreSlug && currentStore.slug !== routeStoreSlug) return;
    if (!publishedInCurrentStore) {
      error(`این کالا در «${currentStore.name}» عرضه نمی‌شود.`);
      return;
    }
    startDirectBuy({
      product: displayProduct,
      qty,
      variantId: selectedVariant?.id,
      variantLabel: selectedVariant?.name,
      addonIds: selectedAddonIds,
      addonSnapshot: detailAddons,
      store: { id: currentStore.id, name: currentStore.name, slug: currentStore.slug },
    });
    setBuyNowModalOpen(true);
  };

  // P0-2: render gates for a store-scoped PDP. While the routed store
  // is unresolved the page must show a loading state — header, price
  // and add-to-cart included — never the central store's chrome with
  // central prices. A terminally failed resolve (unknown/suspended
  // slug) renders the invalid-store view, never a central PDP.
  if (routeStoreSlug && routeStoreInvalid) {
    return <NotFoundView variant="store" onBack={onBack} />;
  }
  if (routeStoreSlug && !routeStoreResolved) {
    if (storeResolveTimedOut) {
      return (
        <div role="alert" className="flex-1 flex flex-col items-center justify-center gap-3 p-6 text-center">
          <p className="text-sm font-bold text-slate-700">بارگذاری فروشگاه انجام نشد.</p>
          <p className="text-xs text-slate-500">فروشگاه مورد نظر در دسترس نیست؛ لطفاً دوباره تلاش کنید.</p>
          <button
            type="button"
            onClick={() => window.location.reload()}
            className="rounded-full bg-brand px-4 py-2 text-xs font-bold text-white hover:bg-brand-deep transition-colors"
          >
            تلاش دوباره
          </button>
        </div>
      );
    }
    return (
      <div role="status" className="flex-1 flex items-center justify-center p-6 text-xs text-slate-500">
        در حال بارگذاری فروشگاه…
      </div>
    );
  }
  // P0-2: while the scoped detail fetch has not succeeded, the
  // unscoped product prop must not stand in as the store-priced PDP
  // (its price is the central one). Show a loading state for the
  // price/add-to-cart area instead; on failure, an explicit error
  // with a retry — never a silent central-priced render.
  if (scopedStoreSlug && detailFailed) {
    return (
      <div role="alert" className="flex-1 flex flex-col items-center justify-center gap-3 p-6 text-center">
        <p className="text-sm font-bold text-slate-700">بارگذاری مشخصات کالا انجام نشد.</p>
        <p className="text-xs text-slate-500">قیمت فروشگاه دریافت نشد؛ لطفاً دوباره تلاش کنید.</p>
        <button
          type="button"
          onClick={() => setDetailNonce((n) => n + 1)}
          className="rounded-full bg-brand px-4 py-2 text-xs font-bold text-white hover:bg-brand-deep transition-colors"
        >
          تلاش دوباره
        </button>
      </div>
    );
  }
  if (scopedStoreSlug && !detailProduct) {
    return (
      <div role="status" className="flex-1 flex items-center justify-center p-6 text-xs text-slate-500">
        در حال بارگذاری کالا…
      </div>
    );
  }

  // Store-scoped route, wrong store: the URL names a store that does
  // not publish this product. Render the store 404 (bare shell, no
  // central chrome - App frames it with the store's header/footer)
  // instead of a PDP shell, and never redirect to the central PDP:
  // that redirect is the leak the store-carrying URL exists to
  // close. The check runs against the ALIGNED currentStore - the
  // context installs the routed store as currentStore from the
  // single-store DTO whose activeProductIds the publication rule
  // trusts - so it cannot fire on transient pre-alignment state.
  if (
    routeStoreSlug &&
    currentStore?.slug === routeStoreSlug &&
    currentStore.ownerType === 'dropshipper' &&
    !publishedInCurrentStore
  ) {
    return <NotFoundView variant="store" onBack={onBack} />;
  }

  const buyBarClasses = `border-t p-2.5 flex items-center justify-between gap-2 lg:border-0 lg:px-0 lg:py-4 lg:flex-col lg:items-stretch lg:gap-3 ${fashion ? 'bg-[#faf8f5] border-[#e5e0d8]' : 'bg-white border-slate-200'}`;
  const buyBarControls = (
    <>
      <div className={fashion ? 'flex items-center bg-white rounded-none border border-[#e5e0d8] shrink-0 lg:justify-between' : 'flex items-center bg-slate-100 rounded-xl border border-slate-200 shrink-0 lg:justify-between'}>
        <button
          onClick={() => setQty(Math.max(minQty, qty - 1))}
          disabled={outOfStock}
          aria-label="کاهش تعداد"
          className={fashion ? 'min-w-[44px] min-h-[44px] flex items-center justify-center text-[#1c1c1a] font-bold disabled:opacity-40 hover:bg-[#f2f2f2] rounded-none transition-colors' : 'min-w-[44px] min-h-[44px] flex items-center justify-center text-slate-700 font-bold disabled:opacity-40 hover:bg-slate-200 rounded-r-xl transition-colors'}
        >
          -
        </button>
        <span className="px-2.5 font-bold text-xs">{toPersianDigits(qty)}</span>
        <button
          onClick={() => setQty(maxQty > 0 ? Math.min(maxQty, qty + 1) : qty + 1)}
          disabled={outOfStock || (maxQty > 0 && qty >= maxQty)}
          aria-label="افزایش تعداد"
          className={fashion ? 'min-w-[44px] min-h-[44px] flex items-center justify-center text-[#1c1c1a] font-bold disabled:opacity-40 hover:bg-[#f2f2f2] rounded-none transition-colors' : 'min-w-[44px] min-h-[44px] flex items-center justify-center text-slate-700 font-bold disabled:opacity-40 hover:bg-slate-200 rounded-l-xl transition-colors'}
        >
          +
        </button>
      </div>
      {outOfStock ? (
        <div className={fashion ? 'flex-1 py-3.5 bg-[#f2f2f2] text-[#737373] font-bold text-xs rounded-none text-center' : 'flex-1 py-3.5 bg-slate-200 text-slate-500 font-bold text-xs rounded-xl text-center'}>
          {selectionNeeded ? 'برای خرید، مدل کالا را انتخاب کنید.' : scopedStoreSlug ? 'ناموجود' : 'ناموجود در انبار مرکزی'}
        </div>
      ) : !publishedInCurrentStore ? (
        <div className={fashion ? 'flex-1 py-3.5 bg-[#f2f2f2] text-[#737373] font-bold text-xs rounded-none text-center' : 'flex-1 py-3.5 bg-slate-100 text-slate-500 font-bold text-xs rounded-xl text-center'}>
          این کالا در فروشگاه «{currentStore?.name}» عرضه نمی‌شود
        </div>
      ) : (
        <div className="flex-1 flex items-center gap-1.5">
          <button
            onClick={handleAddToCart}
            title="افزودن کالا به سبد خرید"
            className={fashion ? 'flex-1 py-3.5 lg:py-4 bg-white hover:bg-[#f2f2f2] text-[#1c1c1a] border border-[#1c1c1a] font-bold text-xs lg:text-sm xl:text-base rounded-none flex items-center justify-center gap-1 transition-colors' : 'flex-1 py-3.5 lg:py-4 bg-brand-soft hover:bg-brand-border text-brand border border-brand-border font-bold text-xs lg:text-sm xl:text-base rounded-xl shadow-2xs flex items-center justify-center gap-1 transition-colors'}
          >
            <ShoppingCart className="w-4 h-4"  strokeWidth={1.5}/>
            <span>افزودن به سبد</span>
          </button>
          <button
            onClick={handleBuyNow}
            title="خرید فوری بدون سبد خرید"
            className={fashion ? 'flex-1 py-3.5 lg:py-4 bg-[#3a4441] hover:bg-[#1c1c1a] text-[#faf8f5] font-extrabold text-xs lg:text-sm xl:text-base rounded-none flex items-center justify-center gap-1 transition-colors' : 'flex-1 py-3.5 lg:py-4 bg-brand hover:bg-brand-deep text-white font-extrabold text-xs lg:text-sm xl:text-base rounded-xl shadow-xs flex items-center justify-center gap-1 transition-colors'}
          >
            <span>خرید فوری</span>
            <span className="text-[10px] lg:text-xs opacity-90">
              {/* FIX 22: live buy button uses full «تومان», not abbreviated «ت». */}
              ({formatPrice(buyNowUnit * qty)} تومان)
            </span>
          </button>
        </div>
      )}
    </>
  );

  const gallery = (
    <ProductDetailGallery
      mainImage={displayProduct.image}
      images={displayProduct.images}
      title={displayProduct.title}
      discountPercent={displayProduct.discountPercent}
      fashion={fashion}
      central={central}
      fallbackImage={displayBase.image}
      saleEndsAt={displayProduct.saleTo}
      saleStartsAt={displayProduct.saleFrom}
    />
  );
  const selectionControls = (
    <>
      {showVariantSelector && (
        <div className={central ? 'central-pdp-options' : fashion ? 'bg-white border border-[#e5e0d8] rounded-none p-3' : 'bg-white border border-slate-200 rounded-2xl p-3'}>
          {central && <h3>انتخاب مدل کالا</h3>}
          <ProductVariantSelector
            product={displayBase}
            variants={variants}
            variantPrices={displayProduct.variantPrices}
            selectedId={selectedVariant?.id ?? null}
            onSelect={handleSelectVariant}
            storeCount={scopedStoreSlug ? 0 : displayProduct.storeCount}
            theme={fashion ? 'fashion' : 'default'}
          />
        </div>
      )}
      {central && !showVariantSelector && displayBase.productType === 'variable' && selectedVariant && (
        <p className="text-sm text-[#53665e]">مدل کالا <strong className="text-[#172d26]">{selectedVariant.variantName || selectedVariant.name}</strong></p>
      )}
      {detailAddons.length > 0 && (
        <div className={central ? 'central-pdp-extras space-y-2' : fashion ? 'bg-white border border-[#e5e0d8] rounded-none p-3 space-y-2' : 'bg-white border border-slate-200 rounded-2xl p-3 space-y-2'}>
          <h3 className={fashion ? 'text-xs font-bold text-[#1c1c1a]' : 'text-xs font-bold text-slate-800'}>{displayBase.addonSectionTitle || 'محصولات مکمل'}{central && <span className="font-normal text-[#53665e]"> · اختیاری</span>}</h3>
          {detailAddons.map((addon) => {
            const checked = selectedAddonIds.includes(addon.id);
            return (
              <label key={addon.id} className={central ? 'central-pdp-addon' : fashion ? 'flex items-start gap-2 rounded-none border border-[#e5e0d8] px-3 py-2 cursor-pointer hover:bg-[#faf8f5]' : 'flex items-start gap-2 rounded-xl border border-slate-200 px-3 py-2 cursor-pointer hover:bg-slate-50'}>
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={() => setSelectedAddonIds((prev) => checked ? prev.filter((id) => id !== addon.id) : [...prev, addon.id])}
                  className={central ? 'size-4 shrink-0 accent-[#164638]' : fashion ? 'w-4 h-4 mt-0.5 shrink-0 accent-[#1c1c1a]' : 'w-4 h-4 mt-0.5 shrink-0 accent-brand'}
                />
                <span className="flex-1 min-w-0">
                  <span className={fashion ? 'block text-xs font-bold text-[#1c1c1a]' : 'block text-xs font-bold text-slate-800'}>{addon.title}</span>
                  {addon.description ? <span className={fashion ? 'block text-[11px] text-[#737373]' : 'block text-[11px] text-slate-500'}>{addon.description}</span> : null}
                </span>
                <span className={fashion ? 'text-xs font-bold text-[#1c1c1a] shrink-0' : 'text-xs font-bold text-brand-deep shrink-0'}>+{formatPrice(addon.feePerUnit)} {central ? 'تومان' : 'ت'}</span>
              </label>
            );
          })}
        </div>
      )}
      <BuyNowAddressModal
        central={central}
        fashion={fashion}
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
    </>
  );

  if (central) {
    const available = !outOfStock && publishedInCurrentStore;
    const canAddToCart = Boolean(purchase && cartQuantityAllowed(cart, purchase, qty));
    const subtotal = formatPrice(buyNowUnit * qty);
    const showQuantity = maxQty > minQty || minQty > 1;
    const productIsInCart = cart.some(line => line.product.id === product.id);
    const purchaseShortcut = () => {
      purchaseRef.current?.focus({ preventScroll: true });
      purchaseRef.current?.scrollIntoView({ block: 'start', behavior: 'instant' });
    };

    return (
      <main id="main-content" tabIndex={-1} className="central-pdp" dir="rtl">
        <div className="central-wrap central-pdp-shell">
          <header className="central-pdp-toolbar">
            <button type="button" onClick={onBack} className="central-pdp-back">
              <ArrowRight size={18} aria-hidden="true" />
              <span>بازگشت</span>
            </button>
            <div className="flex items-center gap-2">
              <button type="button" onClick={handleShare} aria-label="اشتراک‌گذاری" className="central-pdp-icon">
                <Share2 size={18} aria-hidden="true" />
              </button>
              <button
                type="button"
                aria-label="علاقه‌مندی"
                aria-pressed={wishlisted}
                onClick={() => {
                  toggleWishlist(product.id, pdpStore);
                  if (wishlisted) info('از لیست علاقه‌مندی‌ها حذف شد.');
                  else success('به لیست علاقه‌مندی‌ها اضافه شد.');
                }}
                className="central-pdp-icon"
              >
                <Heart size={18} fill={wishlisted ? 'currentColor' : 'none'} aria-hidden="true" />
              </button>
            </div>
          </header>

          <div className="central-pdp-grid">
            <ProductDetailInfo product={displayProduct} central />
            <div className="central-pdp-gallery">{gallery}</div>
            <section
              className="central-pdp-purchase"
              aria-labelledby={purchaseId}
              ref={purchaseRef}
              tabIndex={-1}
            >
              <div className="central-pdp-price">
                <div>
                  <h2 id={purchaseId}>{selectionNeeded ? 'قیمت از' : 'قیمت کالا'}</h2>
                  {!selectionNeeded && displayProduct.originalPrice > displayProduct.price && (
                    <del>{formatPrice(displayProduct.originalPrice)} تومان</del>
                  )}
                  <strong>{formatPrice(selectionNeeded ? displayBase.priceMin ?? displayProduct.price : displayProduct.price)} <small>تومان</small></strong>
                </div>
                <div className="central-pdp-stock" data-available={available} data-needs-selection={selectionNeeded || undefined} role="status">
                  {available ? <PackageCheck size={16} aria-hidden="true" /> : !selectionNeeded && <PackageX size={16} aria-hidden="true" />}
                  {available ? 'موجود در انبار' : selectionNeeded ? 'مدل انتخاب نشده' : 'ناموجود'}
                </div>
              </div>
              {selectionControls}
              <div ref={buyBarRef} className="central-pdp-buy">
                {available ? (
                  <>
                    {showQuantity && (
                      <div className="central-pdp-quantity">
                        <span>تعداد</span>
                        <div className="central-pdp-stepper">
                          <button type="button" onClick={() => setQty(Math.max(minQty, qty - 1))} disabled={qty <= minQty} aria-label="کاهش تعداد">
                            <Minus size={16} aria-hidden="true" />
                          </button>
                          <output aria-label="تعداد انتخاب‌شده">{toPersianDigits(qty)}</output>
                          <button type="button" onClick={() => setQty(Math.min(maxQty, qty + 1))} disabled={qty >= maxQty} aria-label="افزایش تعداد">
                            <Plus size={16} aria-hidden="true" />
                          </button>
                        </div>
                      </div>
                    )}
                    <div className="central-pdp-subtotal">
                      <span>{selectedAddonIds.length > 0 ? 'جمع کالا و گزینه‌های انتخابی' : 'جمع کالا'}</span>
                      <strong>{subtotal} تومان</strong>
                    </div>
                    <p className="central-pdp-shipping-note">هزینهٔ ارسال پس از انتخاب نشانی مشخص می‌شود.</p>
                    <div className="central-pdp-actions">
                      <button type="button" onClick={handleAddToCart} disabled={!canAddToCart} className="central-pdp-primary">
                        <ShoppingCart size={18} aria-hidden="true" /> افزودن به سبد
                      </button>
                      <button type="button" onClick={handleBuyNow} className="central-pdp-secondary">
                        <Zap size={17} aria-hidden="true" /> خرید فوری
                      </button>
                    </div>
                    {productIsInCart && <button type="button" className="central-pdp-cart-link" onClick={() => setIsCartOpen(true)}>مشاهدهٔ سبد خرید</button>}
                    {!canAddToCart && <p className="text-sm text-[#9e3434]" role="status">با احتساب تعداد داخل سبد، موجودی برای افزودن این تعداد کافی نیست.</p>}
                  </>
                ) : <p className="central-pdp-unavailable">{selectionNeeded ? 'برای خرید، یکی از مدل‌های بالا را انتخاب کنید.' : 'در حال حاضر امکان خرید این گزینه وجود ندارد.'}</p>}
              </div>
              <ProductDetailAssurances product={displayProduct} central />
            </section>
          </div>
          <ProductDetailTabs key={product.id} product={displayProduct} central />
          <ProductDetailRelated products={related} fashion={false} central onOpen={openProductDetail} />
        </div>
        {available && !purchaseInView && (
          <div className="central-pdp-mobile-buy lg:hidden">
            <span><small>{selectedAddonIds.length > 0 ? 'جمع کالا و گزینه‌ها' : 'جمع کالا'}</small><strong>{subtotal} تومان</strong></span>
            <button type="button" onClick={purchaseShortcut} className="central-pdp-primary">گزینه‌های خرید</button>
          </div>
        )}
      </main>
    );
  }

  return (
    <div
      className={fashion ? 'flex-1 flex flex-col bg-[#faf8f5] text-[#1c1c1a] select-none pb-36 lg:pb-8' : 'flex-1 flex flex-col bg-white select-none pb-36 lg:pb-8'}
      style={fashion ? ({ ['--tpl-accent' as string]: pdpTheme.accent } as React.CSSProperties) : undefined}
    >
      {/* Header Bar */}
      <header className={fashion ? 'sticky top-0 z-30 lg:static bg-[#faf8f5] border-b border-[#e5e0d8] px-3 py-2 lg:py-3 flex items-center justify-between lg:px-8 xl:px-10 2xl:px-12' : 'sticky top-0 z-30 lg:static bg-white border-b border-slate-200 px-3 py-2 lg:py-3 flex items-center justify-between lg:px-8 xl:px-10 2xl:px-12'}>
        <div className="flex items-center gap-2">
          <button
            onClick={onBack}
            aria-label="بازگشت"
            className={fashion ? 'w-11 h-11 rounded-none hover:bg-[#f2f2f2] text-[#1c1c1a] transition-colors flex items-center justify-center' : 'w-11 h-11 rounded-lg hover:bg-slate-100 text-slate-700 transition-colors flex items-center justify-center'}
          >
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <span className={fashion ? 'text-xs font-bold text-[#1c1c1a] truncate max-w-[220px] sm:max-w-[320px] md:max-w-[440px] lg:max-w-none' : 'text-xs font-bold text-slate-800 truncate max-w-[220px] sm:max-w-[320px] md:max-w-[440px] lg:max-w-none'}>
            {product.title}
          </span>
        </div>

        <div className="flex items-center gap-1">
          <button
            onClick={handleShare}
            aria-label="اشتراک‌گذاری"
            className={fashion ? 'w-11 h-11 text-[#737373] hover:bg-[#f2f2f2] rounded-none flex items-center justify-center' : 'w-11 h-11 text-slate-600 hover:bg-slate-100 rounded-lg flex items-center justify-center'}
          >
            <Share2 className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
          <button
            onClick={() => {
              toggleWishlist(product.id, pdpStore);
              if (!wishlisted) success('به لیست علاقه‌مندی‌ها اضافه شد.');
              else info('از لیست علاقه‌مندی‌ها حذف شد.');
            }}
            aria-label="علاقه‌مندی"
            className={`w-11 h-11 ${fashion ? 'rounded-none' : 'rounded-lg'} flex items-center justify-center ${
              wishlisted
                ? 'text-rose-500 bg-rose-50'
                : fashion
                  ? 'text-[#737373] hover:bg-[#f2f2f2]'
                  : 'text-slate-600 hover:bg-slate-100'
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
      <div className="lg:self-stretch">{gallery}</div>

      {/* Info column — live order: info (with tabs at its end) ->
          variant box -> buy bar. No sticky bottom bar on desktop. */}
      <div className="p-3.5 space-y-4 lg:p-0">
        <ProductDetailInfo product={displayProduct} fashion={fashion} selectedVariantId={selectedVariant?.id ?? null} />
        <ProductDetailTabs product={displayProduct} fashion={fashion} />
        {selectionControls}
        <div ref={buyBarRef} className={buyBarClasses}>
          {buyBarControls}
        </div>
        {showFloatingBuyBar && (
          <div className={`fixed ${scopedStoreSlug ? 'bottom-0' : 'bottom-16'} inset-x-0 max-w-[504px] mx-auto z-50 shadow-[0_-4px_16px_rgba(0,0,0,0.06)] lg:hidden ${buyBarClasses}`}>
            {buyBarControls}
          </div>
        )}

      </div>
      <ProductDetailRelated products={related} fashion={fashion} onOpen={openProductDetail} />
      </div>

    </div>
  );
};
