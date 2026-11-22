import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { isMockMode } from '../../services/apiClient';
import { ArrowRight, User, Package, Heart, ShieldCheck, LogOut, ChevronLeft, Wallet, RotateCcw } from 'lucide-react';
import { orderApi, CustomerReturnDto } from '../../services/orderApi';
import { toPersianDigits } from '../../utils/formatters';

// F1: «مرجوعی‌های من» menu row — same pattern as the Orders row. The
// count comes from orderApi.getMyReturns() (one extra GET /returns on
// profile open); null = still loading. The window note is part of the row
// so it never implies returns are possible outside the 7-day window.
export const CustomerReturnsMenuRow: React.FC<{
  returnsCount: number | null;
  onOpen: () => void;
}> = ({ returnsCount, onOpen }) => (
  <button
    onClick={onOpen}
    className="w-full p-3.5 flex items-center justify-between hover:bg-slate-50 transition-colors text-right"
  >
    <div className="flex items-center gap-2.5">
      <RotateCcw className="w-4 h-4 text-amber-600" strokeWidth={1.5}/>
      <div>
        <span className="font-bold text-slate-800 block">مرجوعی‌های من</span>
        <span className="text-[10px] text-slate-400 block">
          {returnsCount === null
            ? 'در حال دریافت…'
            : returnsCount === 0
              ? 'هنوز مرجوعی‌ای ثبت نکرده‌اید'
              : `${toPersianDigits(returnsCount)} مرجوعی ثبت شده`}
        </span>
        <span className="text-[10px] text-slate-400 block">۷ روز مهلت تست و عودت کالا پس از تحویل</span>
      </div>
    </div>
    <ChevronLeft className="w-4 h-4 text-slate-400" strokeWidth={1.5}/>
  </button>
);

export const CustomerProfileView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { 
    customerUser, logoutCustomer, orders, wishlistIds, 
    setActiveView, openStaticPage, formatPrice, toPersianDigits 
  } = useStore();
  const { success } = useToast();

  const handleLogout = () => {
    logoutCustomer();
    success('با موفقیت از حساب کاربری خارج شدید.');
    onBack(); // بازگشت به صفحه اصلی
  };

  // OrderContext contains only this authenticated account's backend-owned list.
  const myOrders = orders;

  // F1: one extra GET /returns on profile open for the returns count.
  // (CustomerOrdersView fetches the same list again on mount — the store
  // context does not hold it, so sharing would need a lift; the spec
  // allows the single extra fetch.)
  const [myReturns, setMyReturns] = useState<CustomerReturnDto[] | null>(null);
  useEffect(() => {
    let active = true;
    orderApi.getMyReturns()
      .then((list) => { if (active) setMyReturns(list); })
      .catch(() => { if (active) setMyReturns([]); });
    return () => { active = false; };
  }, []);

  // F1 destination: the existing «مرجوعی‌های من» section inside the
  // Orders view — zero new backend, reuses the tested flow. Scroll to the
  // section once the view has mounted.
  const goToReturns = () => {
    setActiveView('customer_orders');
    setTimeout(() => {
      if (typeof document === 'undefined') return;
      const heading = Array.from(document.querySelectorAll('h2'))
        .find((h) => h.textContent?.includes('مرجوعی‌های من'));
      heading?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }, 250);
  };

  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc] select-none pb-20">
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button onClick={onBack} className="p-1 rounded-lg hover:bg-slate-100 text-slate-700">
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <h1 className="text-xs font-bold text-slate-900">پروفایل کاربری من</h1>
        </div>
        <button onClick={handleLogout} className="text-rose-600 hover:text-rose-700 text-xs font-bold flex items-center gap-1">
          <LogOut className="w-3.5 h-3.5"  strokeWidth={1.5}/>
          <span>خروج</span>
        </button>
      </header>

      <div className="p-3.5 space-y-3">
        {/* User Card */}
        <div className="bg-white rounded-2xl border border-slate-200/90 p-4 shadow-2xs space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="w-12 h-12 rounded-2xl bg-blue-50 text-blue-600 flex items-center justify-center font-bold text-base">
                <User className="w-6 h-6"  strokeWidth={1.5}/>
              </div>
              <div>
                <h2 className="text-xs font-bold text-slate-900">{customerUser.fullName}</h2>
                <span className="text-[11px] text-slate-500 font-mono" dir="ltr">
                  {toPersianDigits(customerUser.mobile)}
                </span>
              </div>
            </div>
            <span className="text-[9px] bg-emerald-50 text-emerald-700 border border-emerald-200 font-bold px-2 py-0.5 rounded-full">
              تایید هویت پیامکی
            </span>
          </div>

          <div className="grid grid-cols-2 gap-2 pt-2 border-t border-slate-100 text-[10px] text-slate-500">
            <div>
              <span>کد ملی:</span>
              <strong className="font-mono text-slate-700 mr-1">{toPersianDigits(customerUser.nationalCode)}</strong>
            </div>
            <div>
              <span>عضویت:</span>
              <strong className="text-slate-700 mr-1">۱۴۰۵ (پذیرش فعال)</strong>
            </div>
          </div>
        </div>

        {/* Balance Card */}
        <div className="bg-gradient-to-l from-slate-900 to-slate-800 rounded-2xl p-3.5 text-white shadow-xs space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Wallet className="w-4 h-4 text-emerald-400"  strokeWidth={1.5}/>
              <span className="text-xs font-bold">کیف پول و پاداش خرید</span>
            </div>
            <span className="text-sm font-black text-emerald-400">{formatPrice(isMockMode() ? 340000 : 0)} تومان</span>
          </div>
          <p className="text-[10px] text-slate-400 leading-relaxed">
            مبالغ کش‌بک و بازگشت وجه خریدهای قبلی شما به صورت خودکار در فاکتورهای بعدی اعمال می‌شود.
          </p>
        </div>

        {/* Menu Items */}
        <div className="bg-white rounded-2xl border border-slate-200/90 divide-y divide-slate-100 shadow-2xs overflow-hidden text-xs">
          <button
            onClick={() => setActiveView('customer_orders')}
            className="w-full p-3.5 flex items-center justify-between hover:bg-slate-50 transition-colors text-right"
          >
            <div className="flex items-center gap-2.5">
              <Package className="w-4 h-4 text-blue-600"  strokeWidth={1.5}/>
              <div>
                <span className="font-bold text-slate-800 block">سفارش‌های من</span>
                <span className="text-[10px] text-slate-400">{toPersianDigits(myOrders.length)} سفارش ثبت شده</span>
              </div>
            </div>
            <ChevronLeft className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/>
          </button>

          <CustomerReturnsMenuRow
            returnsCount={myReturns === null ? null : myReturns.length}
            onOpen={goToReturns}
          />

          <button
            onClick={() => setActiveView('customer_wishlist')}
            className="w-full p-3.5 flex items-center justify-between hover:bg-slate-50 transition-colors text-right"
          >
            <div className="flex items-center gap-2.5">
              <Heart className="w-4 h-4 text-rose-500"  strokeWidth={1.5}/>
              <div>
                <span className="font-bold text-slate-800 block">لیست علاقه‌مندی‌ها</span>
                <span className="text-[10px] text-slate-400">{toPersianDigits(wishlistIds.length)} کالای نشان‌شده</span>
              </div>
            </div>
            <ChevronLeft className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/>
          </button>

          <button
            onClick={() => openStaticPage('terms')}
            className="w-full p-3.5 flex items-center justify-between hover:bg-slate-50 transition-colors text-right"
          >
            <div className="flex items-center gap-2.5">
              <ShieldCheck className="w-4 h-4 text-emerald-600"  strokeWidth={1.5}/>
              <div>
                <span className="font-bold text-slate-800 block">قوانین و ضمانت انبار مرکزی</span>
                <span className="text-[10px] text-slate-400">۷ روز مهلت تست و عودت کالا</span>
              </div>
            </div>
            <ChevronLeft className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/>
          </button>
        </div>
      </div>
    </div>
  );
};
