import React, { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { adminRolesForCapabilities, panelsForCapabilities, useAuth, type AuthenticatedPanel } from '../../context/AuthContext';
import { isMockMode } from '../../services/apiClient';
import { ArrowRight, User, Package, Heart, ShieldCheck, LogOut, ChevronLeft, Wallet, RotateCcw, Store, Building2, Warehouse } from 'lucide-react';
import { orderApi, type CustomerReturnDto } from '../../services/orderApi';
import { toPersianDigits } from '../../utils/formatters';
import { HelpExplainer } from '../common/HelpExplainer';
import { pathForView } from '../../utils/routing';
import { followStoreLink } from '../../utils/followStoreLink';
import { RouteLoading } from '../common/RouteLoading';

const AccountOrders = lazy(() => import('./CustomerOrdersView').then(module => ({ default: module.CustomerOrdersView })));
const AccountWishlist = lazy(() => import('./CustomerWishlistView').then(module => ({ default: module.CustomerWishlistView })));
const AccountTerms = lazy(() => import('./StaticPageView').then(module => ({ default: module.StaticPageView })));

type AccountSection = 'orders' | 'returns' | 'wishlist' | 'terms';

function readAccountSection(): AccountSection | null {
  const section = new URLSearchParams(window.location.search).get('section');
  return section === 'orders' || section === 'returns' || section === 'wishlist' || section === 'terms' ? section : null;
}

const workspaceLinks = {
  seller_dashboard: { title: 'پنل دراپ‌شیپر', icon: Store },
  supplier_dashboard: { title: 'پنل تأمین‌کننده', icon: Building2 },
  warehouse_panel: { title: 'پنل انبار', icon: Warehouse },
  admin_panel: { title: 'پنل مدیریت', icon: ShieldCheck },
} satisfies Record<Exclude<AuthenticatedPanel, 'customer'>, { title: string; icon: typeof Store }>;

type AccountLayoutProps = React.PropsWithChildren<{
  title: string;
  onBack: () => void;
  extra?: React.ReactNode;
  central: boolean;
  embedded?: boolean;
  backLabel?: string;
}>;

export const CustomerAccountLayout: React.FC<AccountLayoutProps> = ({
  title, onBack, extra, central, embedded = false, backLabel = 'بازگشت به فروشگاه', children,
}) => {
  const Container = central ? 'main' : 'div';
  const tone = {
    '--account-accent': central ? 'var(--shop-forest, #164638)' : 'var(--color-brand-deep)',
    '--account-soft': central ? 'var(--shop-soft, #edf2e9)' : 'var(--color-brand-soft)',
    '--account-bg': central ? 'var(--shop-bg, #f8f6ef)' : 'var(--color-slate-50)',
    '--account-ink': central ? 'var(--shop-ink, #172d26)' : 'var(--color-slate-900)',
    '--account-line': central ? 'var(--shop-line, #dde2d7)' : 'var(--color-slate-200)',
    '--account-muted': central ? 'var(--shop-muted, #4f6359)' : 'var(--color-slate-600)',
    '--account-radius': central ? 'var(--shop-radius, 16px)' : '1rem',
  } as React.CSSProperties;
  if (embedded) {
    return (
      <div className="min-w-0">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-extrabold sm:text-lg">{title}</h2>
          {extra}
        </div>
        {children}
      </div>
    );
  }
  return (
    <Container id={central ? 'main-content' : undefined} tabIndex={central ? -1 : undefined} style={tone}
      className="flex-1 bg-[var(--account-bg)] text-[var(--account-ink)]">
      <div className={central ? 'central-wrap pb-12 pt-5 sm:pt-8' : 'mx-auto w-full max-w-6xl px-4 pb-12 pt-5 sm:px-6 sm:pt-9 lg:px-8'}>
        <header className={`mb-5 flex flex-wrap items-center justify-between gap-3 sm:mb-7 ${central ? 'border-b border-[var(--account-line)] pb-4 sm:pb-5' : ''}`}>
          <div className="flex min-w-0 items-center gap-3">
            <button type="button" onClick={onBack} aria-label={backLabel}
              className="grid size-11 shrink-0 place-items-center rounded-xl border border-[var(--account-line)] bg-white text-[var(--account-accent)] hover:bg-[var(--account-soft)]">
              <ArrowRight className="size-5" aria-hidden="true" />
            </button>
            <h1 className={central ? 'central-page-title' : 'min-w-0 break-words text-xl font-black sm:text-3xl'}>{title}</h1>
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
    className="flex w-full items-center gap-3 px-4 py-3.5 text-start hover:bg-[var(--account-soft)] focus-visible:outline-offset-[-3px] sm:px-5 sm:py-4">
    <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-[var(--account-soft)] text-[var(--account-accent)]">
      <Icon className="size-5" aria-hidden />
    </span>
    <span className="min-w-0 flex-1">
      <span className="block text-sm font-bold">{title}</span>
      <span className="mt-0.5 block text-xs leading-6 text-[var(--account-muted)]">{children}</span>
    </span>
    <ChevronLeft className="size-4 shrink-0 text-[var(--account-muted)]" aria-hidden="true" />
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
    <span className="block">مهلت مرجوعی را در جزئیات سفارش ببینید.</span>
  </AccountRow>
);

export const CustomerProfileView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const {
    customerUser, logoutCustomer, orders, wishlistIds, currentStore, routeStoreSlug,
    setActiveView, formatPrice,
  } = useStore();
  const { success } = useToast();
  const { currentUser, isAuthenticated, isSessionReady, updateProfileName } = useAuth();
  const [section, setSection] = useState(readAccountSection);
  const accountContentRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const restoreSection = () => setSection(readAccountSection());
    window.addEventListener('popstate', restoreSection);
    return () => window.removeEventListener('popstate', restoreSection);
  }, []);
  useEffect(() => {
    if (section) accountContentRef.current?.focus({ preventScroll: true });
  }, [section]);

  const openSection = (next: AccountSection) => {
    const url = new URL(window.location.href);
    url.searchParams.set('section', next);
    if (section) window.history.replaceState(window.history.state, '', url);
    else window.history.pushState({ ...window.history.state, fromAccountMenu: true }, '', url);
    setSection(next);
  };

  const backToAccountMenu = () => {
    if (window.history.state?.fromAccountMenu) {
      window.history.back();
      return;
    }
    const url = new URL(window.location.href);
    url.searchParams.delete('section');
    window.history.replaceState(window.history.state, '', url);
    setSection(null);
  };
  const central = routeStoreSlug == null && currentStore?.ownerType !== 'dropshipper';
  const savedName = (currentUser?.fullName ?? customerUser.fullName).trim();
  const fullName = savedName === 'کاربر جدید' ? '' : savedName;
  const [editingName, setEditingName] = useState(false);
  const [nameDraft, setNameDraft] = useState('');
  const [savingName, setSavingName] = useState(false);
  const [nameError, setNameError] = useState('');
  const [nameSaved, setNameSaved] = useState(false);
  const capabilities = currentUser?.capabilities;
  const canShop = !!capabilities?.includes('customer') || (isMockMode() && customerUser.isLoggedIn);
  const accountSection = canShop ? section : null;
  const mobile = currentUser?.mobile || customerUser.mobile;
  const panels = central && isAuthenticated && isSessionReady && currentUser
    ? panelsForCapabilities(capabilities).filter(panel => panel !== 'customer')
    : [];
  const isSuperAdmin = adminRolesForCapabilities(capabilities).includes('super_admin');

  const saveName = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (savingName) return;
    const name = nameDraft.trim();
    const length = Array.from(name).length;
    if (length < 2 || length > 100 || name === 'کاربر جدید') {
      setNameError('نام و نام خانوادگی را بین ۲ تا ۱۰۰ نویسه وارد کنید.');
      return;
    }
    setNameError('');
    setSavingName(true);
    try {
      await updateProfileName(name);
      setNameSaved(true);
      setEditingName(false);
    } catch {
      setNameError('ذخیرهٔ نام ممکن نشد. دوباره تلاش کنید.');
    } finally {
      setSavingName(false);
    }
  };
  const storeId = currentStore?.id;
  const orderCount = storeId ? orders.filter(order => order.storeId === storeId).length : 0;

  const [myReturns, setMyReturns] = useState<CustomerReturnDto[] | null>(null);
  const [returnsUnavailable, setReturnsUnavailable] = useState(false);
  useEffect(() => {
    if (!canShop) return;
    let active = true;
    orderApi.getMyReturns()
      .then((list) => { if (active) setMyReturns(list); })
      .catch(() => { if (active) setReturnsUnavailable(true); });
    return () => { active = false; };
  }, [canShop, mobile]);

  const returnsCount = myReturns && myReturns.filter(row => orders.some(order => order.id === row.orderId && order.storeId === storeId)).length;
  const logout = () => {
    logoutCustomer();
    success('از حساب کاربری خارج شدید.');
    onBack();
  };

  return (
    <CustomerAccountLayout title="پروفایل کاربری من" onBack={accountSection ? backToAccountMenu : onBack} central={central}
      backLabel={accountSection ? 'بازگشت به منوی حساب' : undefined}
      extra={<button type="button" onClick={logout} className="inline-flex min-h-11 items-center gap-2 rounded-xl border border-rose-200 bg-white px-3 text-sm font-bold text-rose-800 hover:bg-rose-50">
        <LogOut className="size-4" aria-hidden="true" /> خروج
      </button>}>
      <div className={`grid items-start gap-4 ${central ? 'lg:grid-cols-[minmax(260px,1fr)_minmax(0,1.8fr)] lg:gap-6' : ''}`}>
        <div className="space-y-4">
          <section className="rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white p-4 sm:p-6" aria-label="مشخصات حساب">
            <div className="flex flex-wrap items-center gap-3">
              <span className="grid size-12 shrink-0 place-items-center rounded-xl bg-[var(--account-soft)] text-[var(--account-accent)]">
                <User className="size-6" aria-hidden="true" />
              </span>
              <div className="min-w-0">
                <h2 className="break-words text-base font-extrabold">{fullName || 'مشخصات حساب'}</h2>
                <bdi dir="ltr" className="inline-block select-text text-sm font-semibold tabular-nums text-[var(--account-muted)]">{toPersianDigits(mobile)}</bdi>
              </div>
            </div>
            <p className="mt-4 flex items-center gap-2 text-xs font-semibold text-[var(--account-accent)]">
              <ShieldCheck className="size-4" aria-hidden="true" /> شمارهٔ موبایل تأیید شده
            </p>
            {customerUser.nationalCode && <p className="mt-2 border-t border-slate-100 pt-2 text-xs text-slate-600">
              کد ملی <bdi dir="ltr" className="ms-1 select-text font-bold">{toPersianDigits(customerUser.nationalCode)}</bdi>
            </p>}
            {(editingName || !fullName) ? (
              <form onSubmit={saveName} className="mt-4 space-y-2 border-t border-[var(--account-line)] pt-4">
                <label htmlFor="account-full-name" className="block text-sm font-bold">نام و نام خانوادگی</label>
                <input id="account-full-name" name="fullName" autoComplete="name" value={nameDraft}
                  onChange={event => setNameDraft(event.target.value)} required disabled={savingName}
                  aria-invalid={nameError ? true : undefined} aria-describedby={nameError ? 'account-name-error' : undefined}
                  className="min-h-11 w-full rounded-xl border border-[var(--account-line)] px-3 text-base" />
                {nameError && <p id="account-name-error" role="alert" className="text-sm text-rose-800">{nameError}</p>}
                <div className="flex flex-wrap gap-2">
                  <button type="submit" disabled={savingName} className="min-h-11 rounded-xl bg-[var(--account-accent)] px-4 text-sm font-bold text-white disabled:opacity-60">
                    {savingName ? 'در حال ذخیره…' : 'ذخیرهٔ نام'}
                  </button>
                  {fullName && <button type="button" disabled={savingName} onClick={() => setEditingName(false)} className="min-h-11 px-3 text-sm">انصراف</button>}
                </div>
              </form>
            ) : (
              <button type="button" onClick={() => {
                setNameDraft(fullName);
                setNameError('');
                setNameSaved(false);
                setEditingName(true);
              }} className="mt-2 min-h-11 text-sm font-bold text-[var(--account-accent)] underline underline-offset-4">ویرایش نام</button>
            )}
            {nameSaved && <p role="status" className="mt-2 text-sm text-[var(--account-muted)]">نام ذخیره شد.</p>}
          </section>
          {panels.length > 0 && (
            <nav aria-labelledby="account-panels-title" className="overflow-hidden rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white">
              <h2 id="account-panels-title" className="px-4 pb-2 pt-4 text-base font-extrabold sm:px-6">پنل‌های من</h2>
              {panels.map(panel => {
                const { title, icon: Icon } = workspaceLinks[panel];
                return (
                  <a key={panel} href={pathForView(panel)} onClick={event => followStoreLink(event, () => setActiveView(panel))}
                    className="flex min-h-12 items-center gap-3 border-t border-[var(--account-line)] px-4 py-3 text-sm font-bold hover:bg-[var(--account-soft)] sm:px-6">
                    <Icon className="size-5 shrink-0 text-[var(--account-accent)]" aria-hidden="true" />
                    <span className="flex-1">{panel === 'admin_panel' && isSuperAdmin ? 'پنل سوپر ادمین' : title}</span>
                    <ChevronLeft className="size-4 shrink-0" aria-hidden="true" />
                  </a>
                );
              })}
            </nav>
          )}
          {!central && <div className="flex flex-wrap items-center justify-between gap-2 rounded-2xl border border-slate-200 bg-white p-4 text-xs">
            <span className="flex items-center gap-1.5 font-bold">
              <Wallet className="size-4 text-[var(--account-accent)]" aria-hidden="true" /> کیف پول و پاداش خرید
              <HelpExplainer text="مبالغ کش‌بک و بازگشت وجه خریدهای قبلی شما به صورت خودکار در فاکتورهای بعدی اعمال می‌شود." />
            </span>
            <strong>{formatPrice(isMockMode() ? 340000 : 0)} تومان</strong>
          </div>}
        </div>

        {canShop && (section ? (
          <div tabIndex={-1} ref={accountContentRef} className="order-first min-w-0 lg:order-none">
            <Suspense fallback={<RouteLoading label="در حال باز کردن بخش حساب…" />}>
              {section === 'orders' || section === 'returns'
                ? <AccountOrders onBack={backToAccountMenu} accountSection={section} onAccountSectionChange={openSection} />
                : section === 'wishlist' ? <AccountWishlist onBack={backToAccountMenu} embedded />
                : <AccountTerms onBack={backToAccountMenu} embedded />}
            </Suspense>
          </div>
        ) : (
        <nav aria-label="امکانات حساب" className="divide-y divide-[var(--account-line)] overflow-hidden rounded-[var(--account-radius)] border border-[var(--account-line)] bg-white">
          <AccountRow title="سفارش‌های من" icon={Package} onOpen={() => openSection('orders')}>
            {orderCount === 0 ? 'وضعیت خریدهای این فروشگاه' : `${toPersianDigits(orderCount)} سفارش در این فروشگاه`}
          </AccountRow>
          <CustomerReturnsMenuRow returnsCount={returnsCount} unavailable={returnsUnavailable} onOpen={() => openSection('returns')} />
          <AccountRow title="علاقه‌مندی‌ها" icon={Heart} onOpen={() => openSection('wishlist')}>
            {wishlistIds.length === 0 ? 'کالاهای ذخیره‌شده برای خرید بعدی' : `${toPersianDigits(wishlistIds.length)} کالای ذخیره‌شده`}
          </AccountRow>
          <AccountRow title="شرایط خرید و مرجوعی" icon={ShieldCheck} onOpen={() => openSection('terms')}>
            قوانین خرید و ضمانت کالا
          </AccountRow>
        </nav>
        ))}
      </div>
    </CustomerAccountLayout>
  );
};
