import React from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { ArrowRight, Heart, ShoppingCart, Trash2, PackageOpen } from 'lucide-react';

export const CustomerWishlistView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { 
    products, 
    wishlistIds, 
    toggleWishlist, 
    addToCart, 
    openProductDetail, 
    openCatalogCategory,
    formatPrice, 
    toPersianDigits 
  } = useStore();
  const { success, info } = useToast();

  const savedProducts = products.filter((p) => wishlistIds.includes(p.id));

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
                    {formatPrice(product.price)} ت
                  </span>
                </div>

                <div className="p-2 border-t border-slate-100 grid grid-cols-2 gap-1.5 bg-slate-50/50">
                  <button
                    onClick={() => {
                      addToCart(product, 1);
                      success(`«${product.title}» به سبد خرید اضافه شد.`);
                    }}
                    className="py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg font-bold text-[10px] flex items-center justify-center gap-1 transition-colors"
                  >
                    <ShoppingCart className="w-3 h-3"  strokeWidth={1.5}/>
                    <span>خرید</span>
                  </button>
                  <button
                    onClick={() => {
                      toggleWishlist(product.id);
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
