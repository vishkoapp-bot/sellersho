import React from 'react';
import { useStore } from '../context/StoreContext';
import { Trash2, Store, Package } from 'lucide-react';
import { getCartSupplierGroups } from '../utils/cartGrouping';
import { cartItemKey } from '../utils/cartKey';

export const CartItemList: React.FC = () => {
  const { cart, updateQuantity, removeFromCart, formatPrice, toPersianDigits, openCatalogCategory } = useStore();
  const supplierGroups = getCartSupplierGroups(cart);

  if (cart.length === 0) {
    return (
      <div className="text-center py-12 space-y-2">
        <p className="text-sm font-bold text-slate-700">سبد خرید شما در حال حاضر خالی است.</p>
        <p className="text-xs text-slate-400">کالاهای مورد نظر خود را به سبد اضافه کنید.</p>
        <button type="button" onClick={() => openCatalogCategory('all')} className="mt-2 inline-flex items-center gap-1.5 rounded-xl bg-[#5d44f8] px-4 py-2 text-xs font-bold text-white">
          مشاهده محصولات
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {supplierGroups.map((group, index) => (
        <section key={group.key} className="rounded-2xl border border-slate-200 overflow-hidden">
          <div className="px-3 py-2.5 bg-slate-100 flex items-center justify-between gap-2">
            <div className="flex items-center gap-2 min-w-0">
              <Store className="w-4 h-4 text-blue-600 shrink-0"  strokeWidth={1.5}/>
              <h4 className="text-xs font-extrabold text-slate-800 truncate">بسته {toPersianDigits(index + 1)}</h4>
            </div>
            <span className="text-[10px] text-slate-500 shrink-0">{toPersianDigits(group.itemCount)} کالا</span>
          </div>
          <div className="p-2 space-y-2">
            {group.items.map((item) => (
              <div key={cartItemKey(item)} className="bg-slate-50 rounded-xl p-2.5 flex items-center gap-3">
                {item.product.image ? (
                  <img src={item.product.image} alt={item.product.title} className="w-14 h-14 rounded-lg object-cover shrink-0 bg-white" />
                ) : (
                  <div className="w-14 h-14 rounded-lg shrink-0 bg-white flex items-center justify-center" aria-label="تصویر کالا موجود نیست">
                    <Package className="w-6 h-6 text-slate-300"  strokeWidth={1.5}/>
                  </div>
                )}
                <div className="flex-1 min-w-0">
                  <h5 className="text-xs font-bold text-slate-900 line-clamp-1">{item.product.title}</h5>
                  {(item.product.stock || 0) <= 0 && (
                    <span data-testid="stock-badge" className="mt-0.5 inline-block rounded-md bg-rose-50 border border-rose-200 px-1.5 py-0.5 text-[10px] font-bold text-rose-600">ناموجود در انبار</span>
                  )}
                  {item.selectedVariantId && (() => {
                    const v = (item.product.variants ?? []).find((x) => x.id === item.selectedVariantId);
                    const label = v ? (v.variantName || v.name) : null;
                    return label ? <p className="text-[10px] text-slate-500 mt-0.5">{label}</p> : null;
                  })()}
                  <div className="flex items-center justify-between mt-2 gap-2">
                    <span className="text-xs font-extrabold text-slate-900 whitespace-nowrap">{formatPrice((item.product.price + (item.selectedAddonIds && item.product.addons ? item.selectedAddonIds.reduce((s, id) => s + (item.product.addons!.find((a) => a.id === id)?.feePerUnit || 0), 0) : 0)) * item.quantity)} <span className="text-[9px] text-slate-500">تومان</span></span>
                    <div className="flex items-center bg-white rounded-lg border border-slate-200 overflow-hidden shadow-xs">
                      <button onClick={() => updateQuantity(cartItemKey(item), item.quantity - 1)} className="px-2 py-0.5 text-slate-600 hover:bg-slate-100 text-xs font-bold" aria-label="کاهش تعداد">{item.quantity === 1 ? <Trash2 className="w-3 h-3 text-rose-500"  strokeWidth={1.5}/> : '-'}</button>
                      <span className="px-2.5 py-0.5 text-xs font-bold text-slate-800">{toPersianDigits(item.quantity)}</span>
                      <button onClick={() => updateQuantity(cartItemKey(item), item.quantity + 1)} className="px-2 py-0.5 text-slate-600 hover:bg-slate-100 text-xs font-bold" aria-label="افزایش تعداد">+</button>
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
};
