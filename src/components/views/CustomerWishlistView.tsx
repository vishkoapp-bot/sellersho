import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { mapBackendProduct } from '../../context/ProductContext';
import { catalogApi } from '../../services/catalogApi';
import type { Product } from '../../types';
import { cardPriceOf } from '../../utils/cardPrice';
import { ArrowRight, Heart, ShoppingCart, Trash2, PackageOpen } from 'lucide-react';

export const CustomerWishlistView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const {
    products,
    wishlists,
    toggleWishlist,
    addToCart,
    openProductDetail,
    openCatalogCategory,
    formatPrice,
    toPersianDigits,
    currentStore,
    stores,
    routeStoreSlug
  } = useStore();
  const { success, info } = useToast();

  // Per-store wishlist: the view belongs to the store it was entered
  // under - the routed store of a store-scoped session, else a
  // dropshipper session store, else central - resolved exactly as
  // the PDP scopes. The ids are THAT store's list only; a dropshipper
  // store's entries resolve through the store-scoped fetch at store
  // prices (ids whose product left the store's assortment are
  // naturally omitted), central entries through the context catalogue
  // at central prices - the exact parallel.
  const scopedStoreSlug: string | null =
    routeStoreSlug ??
    (currentStore?.ownerType === 'dropshipper' && currentStore.slug
      ? currentStore.slug
      : null);
  const entryStore =
    scopedStoreSlug == null
      ? currentStore
      : currentStore?.slug === scopedStoreSlug
        ? currentStore
        : stores.find((s) => s.slug === scopedStoreSlug) ?? currentStore;
  const entryIds = entryStore ? wishlists[entryStore.id] ?? [] : [];
  const isScopedStore = entryStore?.ownerType === 'dropshipper';
  const [scopedProducts, setScopedProducts] = useState<Product[] | null>(null);
  useEffect(() => {
    if (!isScopedStore || !entryStore) {
      setScopedProducts(null);
      return;
    }
    setScopedProducts(null);
    let cancelled = false;
    // PERF-01: walk pages until every saved id is found (or the
    // catalog ends) — a first page alone could miss a wishlisted
    // product beyond page 1. Personal surface, small wanted set.
    catalogApi
      .fetchAllProductPages({ storeSlug: entryStore.slug }, { stopWhen: (items) => entryIds.every((id) => items.some((p) => p.id === id)) })
      .then(({ items }) => {
        // M-1: a partial walk still shows the wishlisted products
        // it did find, instead of throwing them away.
        if (!cancelled) setScopedProducts(items.map(mapBackendProduct));
      })
      .catch(() => {
        if (!cancelled) setScopedProducts([]);
      });
    return () => {
      cancelled = true;
    };
  }, [isScopedStore, entryStore?.id, entryStore?.slug]);

  const savedProducts = (isScopedStore ? scopedProducts ?? [] : products).filter((p) =>
    entryIds.includes(p.id),
  );

  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc] select-none pb-20">
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button onClick={onBack} className="p-1 rounded-lg hover:bg-slate-100 text-slate-700">
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <h1 className="text-xs font-bold text-slate-900">لیست علاقه‌مندی‌ها</h1>
        </div>
        <span className="text-[10px] font-mono text-slate-400">
          {toPersianDigits(savedProducts.length)} کالا
        </span>
      </header>

      <div className="p-3.5">
        {savedProducts.length === 0 ? (
          <div className="bg-white rounded-2xl p-8 text-center border border-slate-200 space-y-3">
            <div className="w-12 h-12 rounded-full bg-rose-50 text-rose-400 flex items-center justify-center mx-auto">
              <Heart className="w-6 h-6"  strokeWidth={1.5}/>
            </div>
            <h3 className="text-xs font-bold text-slate-800">لیست علاقه‌مندی‌های شما خالی است</h3>
            <p className="text-[11px] text-slate-400">
              می‌توانید کالاهای مورد علاقه خود را با زدن علامت قلب ذخیره کنید.
            </p>
            <button
              onClick={() => openCatalogCategory('all')}
              className="py-2 px-4 bg-[#007dfa] text-white font-bold text-xs rounded-xl shadow-xs"
            >
              مشاهده محصولات
            </button>
          </div>
        ) : (
          <div className="grid grid-cols-2 gap-2.5">
            {savedProducts.map((product) => (
              <div
                key={product.id}
                className="bg-white rounded-2xl border border-slate-200/90 overflow-hidden shadow-2xs flex flex-col justify-between"
              >
                <div
                  onClick={() => openProductDetail(product)}
                  className="p-3 cursor-pointer group"
                >
                  <div className="aspect-square bg-slate-50 rounded-xl overflow-hidden p-2 flex items-center justify-center">
                    <img
                      src={product.image}
                      alt={product.title}
                      className="w-full h-full object-contain group-hover:scale-105 transition-transform"
                    />
                  </div>
                  <h4 className="text-xs font-bold text-slate-800 truncate mt-2">
                    {product.title}
                  </h4>
                  <span className="text-xs font-black text-blue-600 block mt-1">
                    {formatPrice(cardPriceOf(product).price)} ت
                  </span>
                </div>

                <div className="p-2 border-t border-slate-100 grid grid-cols-2 gap-1.5 bg-slate-50/50">
                  <button
                    onClick={() => {
                      // Default-variant rule (the PDP displayProduct
                      // pattern): when the card price comes from a
                      // priced default variant, the cart line carries
                      // that variant and its price - the customer
                      // pays the price this card showed.
                      const defaultVariant = product.variants?.find((v) => v.isDefault);
                      const variantPrice = defaultVariant
                        ? product.variantPrices?.[defaultVariant.id]
                        : undefined;
                      if (defaultVariant && variantPrice && variantPrice.price != null) {
                        const triple = cardPriceOf(product);
                        addToCart(
                          {
                            ...product,
                            price: triple.price,
                            originalPrice: triple.originalPrice,
                            discountPercent: triple.discountPercent,
                          },
                          1,
                          undefined,
                          defaultVariant.id,
                        );
                      } else {
                        addToCart(product, 1);
                      }
                      success(`«${product.title}» به سبد خرید اضافه شد.`);
                    }}
                    className="py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg font-bold text-[10px] flex items-center justify-center gap-1 transition-colors"
                  >
                    <ShoppingCart className="w-3 h-3"  strokeWidth={1.5}/>
                    <span>خرید</span>
                  </button>
                  <button
                    onClick={() => {
                      toggleWishlist(product.id, entryStore ?? undefined);
                      info('کالا از علاقه‌مندی‌ها حذف شد.');
                    }}
                    className="py-1.5 bg-white border border-slate-200 text-slate-500 hover:text-rose-600 rounded-lg text-[10px] font-bold flex items-center justify-center gap-1 transition-colors"
                  >
                    <Trash2 className="w-3 h-3"  strokeWidth={1.5}/>
                    <span>حذف</span>
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
