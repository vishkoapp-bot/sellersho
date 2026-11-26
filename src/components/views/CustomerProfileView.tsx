import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { isMockMode } from '../../services/apiClient';
import { ArrowRight, User, Package, Heart, ShieldCheck, LogOut, ChevronLeft, Wallet, RotateCcw } from 'lucide-react';
import { orderApi, type CustomerReturnDto } from '../../services/orderApi';
import { toPersianDigits } from '../../utils/formatters';
import { HelpExplainer } from '../common/HelpExplainer';

type AccountLayoutProps = React.PropsWithChildren<{
  title: string;
  onBack: () => void;
  extra?: React.ReactNode;
  central: boolean;
}>;

export const CustomerAccountLayout: React.FC<AccountLayoutProps> = ({ title, onBack, extra, central, children }) => {
  const Container = central ? 'main' : 'div';
  const tone = {
    '--account-accent': central ? '#164638' : 'var(--color-brand-deep)',
    '--account-soft': central ? '#edf2e9' : 'var(--color-brand-soft)',
  } as React.CSSProperties;
  return (
    <Container id={central ? 'main-content' : undefined} tabIndex={central ? -1 : undefined} style={tone}
      className={`flex-1 ${central ? 'bg-[#f8f6ef] text-[#172d26]' : 'bg-slate-50 text-slate-900'}`}>
      <div className="mx-auto w-full max-w-6xl px-4 pb-12 pt-5 sm:px-6 sm:pt-9 lg:px-8">
        <header className="mb-5 flex flex-wrap items-center justify-between gap-3 sm:mb-7">
          <div className="flex min-w-0 items-center gap-3">
            <button type="button" onClick={onBack} aria-label="بازگشت به فروشگاه"
              className="grid size-11 shrink-0 place-items-center rounded-xl border border-slate-200 bg-white text-[var(--account-accent)] hover:bg-[var(--account-soft)]">
              <ArrowRight className="size-5" aria-hidden="true" />
            </button>
            <h1 className="text-xl font-black sm:text-3xl">{title}</h1>
          </div>
          {extra}
        </header>
        {children}
      </div>
    </Container>
  );
};

type AccountRowProps = React.PropsWithChildren<{
  title: string;
  icon: React.ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;
  onOpen: () => void;
}>;
const AccountRow: React.FC<AccountRowProps> = ({ title, icon: Icon, onOpen, children }) => (
  <button type="button" onClick={onOpen}
    className="flex w-full items-center gap-3 px-4 py-3.5 text-start hover:bg-[var(--account-soft)] sm:px-5">
    <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-[var(--account-soft)] text-[var(--account-accent)]">
      <Icon className="size-5" aria-hidden />
    </span>
    <span className="min-w-0 flex-1">
      <span className="block text-sm font-bold">{title}</span>
      <span className="mt-0.5 block text-xs leading-6 text-slate-600">{children}</span>
    </span>
    <ChevronLeft className="size-4 shrink-0 text-slate-600" aria-hidden="true" />
  </button>
);

export const CustomerReturnsMenuRow: React.FC<{
  returnsCount: number | null;
  unavailable?: boolean;
  onOpen: () => void;
}> = ({ returnsCount, unavailable, onOpen }) => (
  <AccountRow title="مرجوعی‌های من" icon={RotateCcw} onOpen={onOpen}>
    <span className="block">{unavailable ? 'پیگیری وضعیت مرجوعی' : returnsCount === null
      ? 'در حال دریافت…'
      : returnsCount === 0 ? 'هنوز مرجوعی‌ای ثبت نکرده‌اید' : `${toPersianDigits(returnsCount)} مرجوعی ثبت شده`}</span>
    <span className="block">۷ روز مهلت تست و عودت کالا پس از تحویل</span>
  </AccountRow>
);

export const CustomerProfileView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const {
    customerUser, logoutCustomer, orders, wishlistIds, currentStore, routeStoreSlug,
    setActiveView, openStaticPage, formatPrice,
  } = useStore();
  const { success } = useToast();
  const central = routeStoreSlug == null && currentStore?.ownerType !== 'dropshipper';
  const storeId = currentStore?.id;
  const orderCount = storeId ? orders.filter(order => order.storeId === storeId).length : 0;

  const [myReturns, setMyReturns] = useState<CustomerReturnDto[] | null>(null);
  const [returnsUnavailable, setReturnsUnavailable] = useState(false);
  useEffect(() => {
    if (central && !customerUser.isLoggedIn) return;
    let active = true;
    orderApi.getMyReturns()
      .then((list) => { if (active) setMyReturns(list); })
      .catch(() => { if (active) setReturnsUnavailable(true); });
    return () => { active = false; };
  }, [central, customerUser.isLoggedIn, customerUser.mobile]);

  const goToReturns = () => {
    setActiveView('customer_orders');
    const url = new URL(window.location.href);
    url.hash = 'returns';
    window.history.replaceState(window.history.state, '', url);
  };

  const returnsCount = myReturns && myReturns.filter(row => orders.some(order => order.id === row.orderId && order.storeId === storeId)).length;
  const logout = () => {
    logoutCustomer();
    success('از حساب کاربری خارج شدید.');
    onBack();
  };

  return (
    <CustomerAccountLayout title="پروفایل کاربری من" onBack={onBack} central={central}
      extra={<button type="button" onClick={logout} className="inline-flex min-h-11 items-center gap-2 rounded-xl border border-rose-200 bg-white px-3 text-sm font-bold text-rose-800 hover:bg-rose-50">
        <LogOut className="size-4" aria-hidden="true" /> خروج
      </button>}>
      <div className={`grid items-start gap-4 ${central ? 'lg:grid-cols-[minmax(260px,1fr)_minmax(0,1.8fr)] lg:gap-6' : ''}`}>
        <div className="space-y-4">
          <section className="rounded-2xl border border-slate-200 bg-white p-4 sm:p-6" aria-label="مشخصات حساب">
            <div className="flex flex-wrap items-center gap-3">
              <span className="grid size-12 shrink-0 place-items-center rounded-xl bg-[var(--account-soft)] text-[var(--account-accent)]">
                <User className="size-6" aria-hidden="true" />
              </span>
              <div className="min-w-0">
                {customerUser.fullName && <h2 className="break-words text-base font-extrabold">{customerUser.fullName}</h2>}
                <bdi dir="ltr" className="inline-block select-text text-sm font-semibold">{toPersianDigits(customerUser.mobile)}</bdi>
              </div>
            </div>
            <p className="mt-4 flex items-center gap-2 text-xs font-semibold text-[var(--account-accent)]">
              <ShieldCheck className="size-4" aria-hidden="true" /> شمارهٔ موبایل تأیید شده
            </p>
            {customerUser.nationalCode && <p className="mt-2 border-t border-slate-100 pt-2 text-xs text-slate-600">
              کد ملی <bdi dir="ltr" className="ms-1 select-text font-bold">{toPersianDigits(customerUser.nationalCode)}</bdi>
            </p>}
          </section>
          {!central && <div className="flex flex-wrap items-center justify-between gap-2 rounded-2xl border border-slate-200 bg-white p-4 text-xs">
            <span className="flex items-center gap-1.5 font-bold">
              <Wallet className="size-4 text-[var(--account-accent)]" aria-hidden="true" /> کیف پول و پاداش خرید
              <HelpExplainer text="مبالغ کش‌بک و بازگشت وجه خریدهای قبلی شما به صورت خودکار در فاکتورهای بعدی اعمال می‌شود." />
            </span>
            <strong>{formatPrice(isMockMode() ? 340000 : 0)} تومان</strong>
          </div>}
        </div>

        <nav aria-label="امکانات حساب" className="divide-y divide-slate-100 overflow-hidden rounded-2xl border border-slate-200 bg-white">
          <AccountRow title="سفارش‌های من" icon={Package} onOpen={() => setActiveView('customer_orders')}>
            {orderCount === 0 ? 'وضعیت خریدهای این فروشگاه' : `${toPersianDigits(orderCount)} سفارش در این فروشگاه`}
          </AccountRow>
          <CustomerReturnsMenuRow returnsCount={returnsCount} unavailable={returnsUnavailable} onOpen={goToReturns} />
          <AccountRow title="علاقه‌مندی‌ها" icon={Heart} onOpen={() => setActiveView('customer_wishlist')}>
            {wishlistIds.length === 0 ? 'کالاهای ذخیره‌شده برای خرید بعدی' : `${toPersianDigits(wishlistIds.length)} کالای ذخیره‌شده`}
          </AccountRow>
          <AccountRow title="شرایط خرید و مرجوعی" icon={ShieldCheck} onOpen={() => openStaticPage('terms')}>
            قوانین خرید و ضمانت کالا
          </AccountRow>
        </nav>
      </div>
    </CustomerAccountLayout>
  );
};
