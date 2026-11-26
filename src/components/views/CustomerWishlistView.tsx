import React, { useEffect, useMemo, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { mapBackendProduct } from '../../context/ProductContext';
import { catalogApi } from '../../services/catalogApi';
import { ApiError } from '../../services/apiClient';
import type { Product } from '../../types';
import { cardPriceOf, cardPriceVariantOf } from '../../utils/cardPrice';
import { resolvePurchase, cartQuantityAllowed, type PurchaseResolution } from '../../utils/productPurchase';
import { ArrowLeft, Heart, ShoppingCart, Trash2, PackageOpen } from 'lucide-react';
import { buildProductUrl } from '../../utils/routing';
import { CustomerAccountLayout } from './CustomerProfileView';

// Resolves the wishlist quick-add the same way the card prices it:
// the card-price variant when there is one, else the no-variant
// purchase fallback (which picks the cheapest purchasable variant
// for simple products). Returns undefined when nothing is purchasable.
export const resolveWishlistQuickAdd = (product: Product): PurchaseResolution | undefined => {
  const priced = cardPriceVariantOf(product);
  return priced.variant
    ? resolvePurchase(product, priced.variant.id)
    : resolvePurchase(product);
};

export const CustomerWishlistView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const {
    products,
    wishlists,
    toggleWishlist,
    addToCart,
    cart,
    openProductDetail,
    openCatalogCategory,
    formatPrice,
    toPersianDigits,
    currentStore,
    stores,
    routeStoreSlug
  } = useStore();
  const { success, info, error } = useToast();

  const scopedStoreSlug = routeStoreSlug ??
    (currentStore?.ownerType === 'dropshipper' ? currentStore.slug : null);
  const entryStore = scopedStoreSlug
    ? stores.find((store) => store.slug === scopedStoreSlug) ??
      (currentStore?.slug === scopedStoreSlug ? currentStore : undefined)
    : currentStore?.ownerType === 'central' ? currentStore : stores.find((store) => store.ownerType === 'central');
  const entryIds = entryStore ? wishlists[entryStore.id] ?? [] : [];
  const knownProducts = useMemo(
    () => new Map(scopedStoreSlug ? [] : products.map((product) => [product.id, product] as const)),
    [scopedStoreSlug, products],
  );
  // The central catalogue can hold a filtered or paginated subset. Fetch
  // missing favourites by ID so the list is independent of the last search.
  // A scoped store resolves every item with its own assortment and prices.
  const storeKey = JSON.stringify([entryStore?.id, scopedStoreSlug]);
  const [catalog, setCatalog] = useState<{
    storeKey: string;
    products: Map<string, Product>;
    missing: string[];
    failed: string[];
  } | null>(null);
  const loaded = catalog?.storeKey === storeKey ? catalog : null;
  const pendingIds = entryIds.filter((id) => !knownProducts.has(id)
    && !loaded?.products.has(id) && !loaded?.missing.includes(id) && !loaded?.failed.includes(id));
  const queryKey = JSON.stringify(pendingIds);
  useEffect(() => {
    const ids = JSON.parse(queryKey) as string[];
    if (ids.length === 0) return;
    let active = true;
    const hydrate = async () => {
      const products = new Map<string, Product>();
      const missing: string[] = [];
      const failed: string[] = [];
      for (let index = 0; active && index < ids.length; index += 6) {
        const batch = ids.slice(index, index + 6);
        const outcomes = await Promise.allSettled(batch.map((id) => catalogApi.getProductById(encodeURIComponent(id), scopedStoreSlug ?? undefined)));
        outcomes.forEach((outcome, offset) => {
          const id = batch[offset];
          if (outcome.status === 'fulfilled') {
            const product = mapBackendProduct(outcome.value);
            if (product.id === id) products.set(id, product);
            else missing.push(id);
          } else if (outcome.reason instanceof ApiError && outcome.reason.status === 404) {
            missing.push(id);
          } else {
            failed.push(id);
          }
        });
      }
      if (active) setCatalog((previous) => previous?.storeKey === storeKey
        ? {
          storeKey, products: new Map([...previous.products, ...products]),
          missing: [...previous.missing, ...missing], failed: [...previous.failed, ...failed],
        }
        : { storeKey, products, missing, failed });
    };
    void hydrate();
    return () => { active = false; };
  }, [storeKey, scopedStoreSlug, queryKey]);

  const savedProducts = entryIds.flatMap((id) => {
    const product = knownProducts.get(id) ?? loaded?.products.get(id);
    return product ? [product] : [];
  });
  const unavailableIds = loaded?.missing.filter((id) => entryIds.includes(id)) ?? [];
  const failedIds = loaded?.failed.filter((id) => entryIds.includes(id)) ?? [];
  const remove = (id: string) => {
    toggleWishlist(id, entryStore);
    info('کالا از علاقه‌مندی‌ها حذف شد.');
  };

  return (
    <CustomerAccountLayout title="علاقه‌مندی‌ها" onBack={onBack} central={!scopedStoreSlug}
      extra={entryIds.length > 0 && <span className="rounded-full bg-[var(--account-soft)] px-3 py-1.5 text-sm font-bold text-[var(--account-accent)]">{toPersianDigits(entryIds.length)} کالای ذخیره‌شده</span>}>
      <div className="space-y-4">
        {entryIds.length === 0 ? (
          <div className="rounded-2xl border border-slate-200 bg-white px-6 py-10 text-center sm:py-14">
            <div className="mx-auto mb-4 grid size-14 place-items-center rounded-2xl bg-[var(--account-soft)] text-[var(--account-accent)]">
              <Heart className="size-7" aria-hidden="true" />
            </div>
            <h2 className="text-base font-extrabold">هنوز کالایی ذخیره نکرده‌اید</h2>
            <p className="mt-1 text-sm leading-7 text-slate-600">برای ذخیرهٔ کالا، روی علامت قلب آن بزنید.</p>
            <button type="button" onClick={() => openCatalogCategory('all')}
              className="mt-4 inline-flex min-h-11 items-center gap-2 rounded-xl bg-[var(--account-accent)] px-4 text-sm font-bold text-white hover:brightness-90">
              مشاهده محصولات <ArrowLeft className="size-4" aria-hidden="true" />
            </button>
          </div>
        ) : (
          <>
            {pendingIds.length > 0 && <p role="status" className="rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-600">در حال دریافت کالاهای ذخیره‌شده…</p>}
            {failedIds.length > 0 && (
              <div role="status" className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
                دریافت {toPersianDigits(failedIds.length)} کالا انجام نشد.
                <button type="button" onClick={() => setCatalog(previous => previous ? { ...previous, failed: [] } : null)} className="min-h-11 font-bold underline">تلاش دوباره</button>
              </div>
            )}
            {savedProducts.length > 0 && (
              <ul className={`grid grid-cols-2 gap-2.5 sm:gap-4 ${scopedStoreSlug ? 'md:grid-cols-3 lg:grid-cols-4' : 'md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5'}`}>
                {savedProducts.map((product) => {
                  const purchase = resolveWishlistQuickAdd(product);
                  return (
                    <li key={product.id} className="flex min-w-0 flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white p-2.5 sm:p-3">
                      <a href={buildProductUrl(product, scopedStoreSlug ?? undefined)} onClick={(event) => {
                        if (event.button || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
                        event.preventDefault();
                        openProductDetail(product);
                      }} className="group flex-1">
                        <div className="grid aspect-square place-items-center overflow-hidden rounded-xl bg-slate-50">
                          {product.image ? (
                            <img src={product.image} alt="" loading="lazy" decoding="async"
                              className="size-full object-contain p-1 mix-blend-multiply transition-transform group-hover:scale-105 motion-reduce:transform-none" />
                          ) : <PackageOpen className="size-8 text-slate-500" aria-hidden="true" />}
                        </div>
                        <h2 className="mt-2 line-clamp-2 min-h-10 break-words text-xs font-extrabold leading-5 sm:text-sm">{product.title}</h2>
                      </a>
                      <p className="mt-1 text-sm font-extrabold text-[var(--account-accent)]">
                        {purchase ? <>{formatPrice(cardPriceOf(product).price)} <span className="text-xs font-normal">تومان</span></> : 'ناموجود'}
                      </p>
                      <div className="mt-2 flex items-stretch gap-1.5 border-t border-slate-100 pt-2">
                        {purchase && (
                          <button type="button" aria-label={`افزودن ${product.title} به سبد خرید`} onClick={() => {
                            if (routeStoreSlug && currentStore?.slug !== routeStoreSlug) return;
                            if (!cartQuantityAllowed(cart, purchase, purchase.minQty)) {
                              error('تعداد بیشتری از این کالا در حال حاضر قابل افزودن نیست.');
                              return;
                            }
                            addToCart(purchase.product, purchase.minQty, undefined, purchase.variant?.id);
                            success('کالا به سبد خرید اضافه شد.');
                          }} className="flex min-h-11 flex-1 items-center justify-center gap-1 rounded-lg bg-[var(--account-soft)] px-1.5 text-xs font-bold text-[var(--account-accent)] hover:brightness-95">
                            <ShoppingCart className="size-3.5 shrink-0" aria-hidden="true" /> افزودن
                          </button>
                        )}
                        <button type="button" onClick={() => remove(product.id)} aria-label={`حذف ${product.title} از علاقه‌مندی‌ها`}
                          className="grid size-11 shrink-0 place-items-center rounded-lg border border-slate-200 text-slate-600 hover:bg-rose-50 hover:text-rose-800">
                          <Trash2 className="size-4" aria-hidden="true" />
                        </button>
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
            {unavailableIds.length > 0 && (
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-700">
                {toPersianDigits(unavailableIds.length)} کالا دیگر در این فروشگاه عرضه نمی‌شود.
                <button type="button" onClick={() => unavailableIds.forEach((id) => toggleWishlist(id, entryStore))}
                  className="min-h-11 font-bold text-[var(--account-accent)] underline">حذف کالاهای عرضه‌نشده</button>
              </div>
            )}
          </>
        )}
      </div>
    </CustomerAccountLayout>
  );
};
