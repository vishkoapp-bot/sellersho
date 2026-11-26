import React from 'react';
import { useStore } from '../context/StoreContext';
import { FallbackImage } from './common/FallbackImage';
import { Trash2, Package, ShoppingBag, Minus, Plus, ArrowLeft } from 'lucide-react';
import { getCartSupplierGroups } from '../utils/cartGrouping';
import { cartItemKey } from '../utils/cartKey';
import { computeCartPricing } from '../utils/cartPricing';
import { cartQuantityAllowed, resolvePurchase } from '../utils/productPurchase';

export const CartItemList: React.FC<{ onBrowse: () => void }> = ({ onBrowse }) => {
  const { cart, updateQuantity, removeFromCart, formatPrice, toPersianDigits } = useStore();
  const supplierGroups = getCartSupplierGroups(cart);

  if (cart.length === 0) {
    return (
      <div className="mx-auto max-w-md rounded-[var(--checkout-radius)] border border-[var(--checkout-line)] bg-white px-6 py-10 text-center sm:px-10 sm:py-12">
        <div className="mx-auto mb-4 grid size-14 place-items-center rounded-2xl bg-[var(--checkout-soft)] text-[var(--checkout-accent)]">
          <ShoppingBag className="size-7" aria-hidden="true" />
        </div>
        <h4 className="text-lg font-black text-balance">سبد خرید شما در حال حاضر خالی است.</h4>
        <button type="button" onClick={onBrowse} className="mt-5 inline-flex min-h-12 items-center gap-2 rounded-xl bg-[var(--checkout-accent)] px-5 text-sm font-bold text-white hover:brightness-90">
          مشاهده محصولات <ArrowLeft className="size-4" aria-hidden="true" />
        </button>
      </div>
    );
  }

  const quantityControl = 'grid size-11 place-items-center text-[var(--checkout-accent)] hover:bg-[var(--checkout-soft)] disabled:cursor-not-allowed disabled:text-slate-400 disabled:hover:bg-transparent';

  return (
    <div className="space-y-3">
      {supplierGroups.map((group, index) => (
        <section key={group.key} className="rounded-[var(--checkout-radius)] border border-[var(--checkout-line)] bg-white">
          {supplierGroups.length > 1 && (
            <div className="flex items-center justify-between gap-3 border-b border-[var(--checkout-line)] px-4 py-3 text-sm">
              <h4 className="flex items-center gap-2 font-bold">
                <Package className="size-4 text-[var(--checkout-accent)]" aria-hidden="true" />
                بسته {toPersianDigits(index + 1)}
              </h4>
              <span className="text-xs text-slate-600">{toPersianDigits(group.itemCount)} عدد</span>
            </div>
          )}
          <ul className="divide-y divide-[var(--checkout-line)]">
            {group.items.map((item) => {
              const key = cartItemKey(item);
              const variant = item.product.variants?.find((candidate) => candidate.id === item.selectedVariantId);
              const extras = item.product.addons?.filter((addon) => item.selectedAddonIds?.includes(addon.id)) ?? [];
              const purchase = resolvePurchase(item.product, item.selectedVariantId);
              const others = cart.filter((candidate) => candidate !== item);
              const canIncrease = !!purchase && cartQuantityAllowed(others, purchase, item.quantity + 1);
              const canReduce = !!purchase && cartQuantityAllowed(others, purchase, item.quantity - 1);
              const linePricing = computeCartPricing([item]);
              return (
                <li key={key} className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 px-3 py-3 sm:grid-cols-[auto_minmax(0,1fr)_auto] sm:items-center sm:gap-4 sm:px-4">
                  <div className="grid size-16 place-items-center overflow-hidden rounded-xl bg-[var(--checkout-bg)] text-[var(--checkout-muted)] sm:size-20">
                    <FallbackImage srcs={[item.product.image]} alt="" width={80} height={80} loading="lazy"
                      className="size-full object-contain"
                      fallback={<Package className="size-6" aria-hidden="true" />} />
                  </div>
                  <div className="min-w-0 py-1">
                    <p className="break-words text-sm font-extrabold leading-6">{item.product.title}</p>
                    {variant && <p className="mt-0.5 text-xs leading-5 text-[var(--checkout-muted)]">{variant.variantName || variant.name}</p>}
                    {extras.length > 0 && <p className="mt-0.5 text-xs leading-5 text-[var(--checkout-muted)]">{extras.map((addon) => addon.title).join('، ')}</p>}
                    {!purchase && <span data-testid="stock-badge" className="mt-1 inline-block rounded-md bg-rose-50 px-2 py-1 text-xs font-bold text-rose-800">ناموجود در انبار</span>}
                  </div>
                  <div className="col-span-2 flex flex-wrap items-center justify-between gap-x-3 gap-y-1 sm:col-span-1 sm:justify-end">
                    <strong className="whitespace-nowrap text-base font-extrabold tabular-nums text-[var(--checkout-ink)]">
                      {formatPrice(linePricing.goodsPayable)} <span className="text-xs font-normal text-slate-600">تومان</span>
                    </strong>
                    <div className="flex items-center">
                      {item.quantity > 1 && (
                        <button type="button" onClick={() => removeFromCart(key)} className="grid size-11 place-items-center rounded-xl text-slate-600 hover:bg-rose-50 hover:text-rose-800" aria-label={`حذف ${item.product.title} از سبد`}>
                          <Trash2 className="size-4" aria-hidden="true" />
                        </button>
                      )}
                      <div className="flex items-center rounded-xl border border-[var(--checkout-line)] [&>button:first-child]:rounded-s-[11px] [&>button:last-child]:rounded-e-[11px]">
                        <button
                          type="button"
                          aria-label={`افزایش تعداد ${item.product.title}`}
                          disabled={!canIncrease}
                          onClick={() => updateQuantity(key, item.quantity + 1)}
                          className={quantityControl}
                        ><Plus className="size-4" aria-hidden="true" /></button>
                        <span className="min-w-5 text-center text-sm font-bold tabular-nums" aria-live="polite">{toPersianDigits(item.quantity)}</span>
                        <button
                          type="button"
                          aria-label={item.quantity === 1 ? `حذف ${item.product.title} از سبد` : `کاهش تعداد ${item.product.title}`}
                          disabled={item.quantity > 1 && !canReduce}
                          onClick={() => updateQuantity(key, item.quantity - 1)}
                          className={quantityControl}
                        >{item.quantity === 1 ? <Trash2 className="size-4" aria-hidden="true" /> : <Minus className="size-4" aria-hidden="true" />}</button>
                      </div>
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </div>
  );
};
