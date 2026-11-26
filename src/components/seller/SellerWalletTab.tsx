import React, { useState, useEffect, useId, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi, StoreBalanceResponse, WalletBreakdownResponse, EarlyWithdrawalResponse } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { isValidIranianSheba } from '../../utils/validators';
import { AccountStatement } from '../common/AccountStatement';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { Wallet, ArrowDownToLine, CheckCircle2, Eye, ShieldCheck, Crown, Info } from 'lucide-react';

const panelClass = 'min-w-0 space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5';
const summaryClass = 'min-h-11 cursor-pointer rounded-lg py-2.5 text-sm font-bold focus-visible:ring-2';
const fieldClass = 'min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm focus:bg-white focus-visible:ring-2 focus-visible:ring-brand';

export const SellerWalletTab: React.FC = () => {
  const {
    currentStore,
    stores,
    orders,
    payoutRequests: localPayoutRequests,
    requestPayout,
    verifyShebaOwnerName,
    purchaseServiceWithWallet,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, error, info } = useToast();
  const [shabaNumber, setShabaNumber] = useState('');
  const [withdrawAmount, setWithdrawAmount] = useState('');
  const [isVerifying, setIsVerifying] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [statementRefresh, setStatementRefresh] = useState(0);
  const [breakdownType, setBreakdownType] = useState<'pending' | 'reserved' | null>(null);
  const shabaId = useId();
  const amountId = useId();
  const breakdownTitleId = useId();
  const [serverBalance, setServerBalance] = useState<StoreBalanceResponse | null>(null);
  const [balanceLoadFailed, setBalanceLoadFailed] = useState(false);
  const mockMode = isMockMode();
  // FIX 14: wallet breakdown + early-withdrawal list, wired to the
  // monthly-settlement endpoints (previously unwired).
  const [walletBreakdown, setWalletBreakdown] = useState<WalletBreakdownResponse | null>(null);
  const [breakdownFailedFor, setBreakdownFailedFor] = useState<string | null>(null);
  const refreshWalletBreakdown = useRef<(() => Promise<void>) | null>(null);
  const breakdownLoaded = mockMode || (walletBreakdown !== null && walletBreakdown.storeId === currentStore.id);
  const breakdownFailed = breakdownFailedFor === currentStore.id;
  const minimum = mockMode ? 500000 : (walletBreakdown?.settlementSettings?.earlyWithdrawalMinimumToman ?? 1000000);
  const earlyWithdrawalFeePct = walletBreakdown?.settlementSettings?.earlyWithdrawalFeePct;
  const [earlyWithdrawals, setEarlyWithdrawals] = useState<EarlyWithdrawalResponse[]>([]);
  const [serverPayouts, setServerPayouts] = useState<typeof localPayoutRequests>([]);
  const [serverOrders, setServerOrders] = useState<typeof orders | null>(null);
  const payoutRequests = isMockMode() ? localPayoutRequests : serverPayouts;

  // Fetch real authoritative store balance from server
  useEffect(() => {
    if (!mockMode && currentStore.id) {
      let active = true;
      setServerBalance(null);
      setBalanceLoadFailed(false);
      setEarlyWithdrawals([]);
      setServerOrders(null);
      setServerPayouts([]);
      storeApi.getStoreOrders(currentStore.slug).then(setServerOrders).catch(() => setServerOrders(null));
      storeApi.getPayouts(currentStore.slug).then(setServerPayouts).catch((err) => error(err.message));
      storeApi.getOwnedBalance(currentStore.slug)
        .then((res) => {
          if (!active) return;
          if (!res || typeof res.availableAmount !== 'number' || res.storeId !== currentStore.id) {
            throw new Error('دریافت موجودی کیف پول انجام نشد.');
          }
          setServerBalance(res);
        })
        .catch((err) => {
          if (!active) return;
          setBalanceLoadFailed(true);
          error(err.message);
        });
      const readBreakdown = async () => {
        if (!active) return;
        setWalletBreakdown(null);
        setBreakdownFailedFor(null);
        try {
          const res = await storeApi.getWalletBreakdown(currentStore.slug);
          if (active) setWalletBreakdown(res);
        } catch (err) {
          if (!active) return;
          setBreakdownFailedFor(currentStore.id);
          throw err;
        }
      };
      refreshWalletBreakdown.current = readBreakdown;
      void readBreakdown().catch(() => {
        // The withdrawal section renders the settings error.
      });
      storeApi.getEarlyWithdrawals(currentStore.slug).then(setEarlyWithdrawals).catch(() => {});
      return () => { active = false; };
    }
  }, [currentStore.id]);

  // Derive actual balances based on real released vs pending orders
  // Live kn: server orders when fetched, otherwise context orders.
  // Defence in depth (C1): getStoreOrders unwraps the envelope, but never
  // let a non-array server payload reach .filter again — fall back to the
  // context orders instead of crashing the whole tab.
  const storeOrders = (Array.isArray(serverOrders) ? serverOrders : null) ?? orders.filter((o) => o.storeId === currentStore.id);
  const storePayouts = payoutRequests.filter((p) => p.storeId === currentStore.id);

  const releasedOrders = storeOrders.filter((o) => o.profitReleased);
  const releasedProfit = releasedOrders.reduce((sum, o) => sum + (o.dropshipperProfit || 0), 0);
  const totalApprovedPayouts = storePayouts.filter((p) => p.status === 'approved').reduce((sum, p) => sum + p.amount, 0);

  // Fallback calculations for mock mode (strictly based on bonusStatus === 'released')
  const referredStores = stores.filter((s) => s.referrerSellerId === currentStore.id);
  const mockReleasedBonus = referredStores.filter((s) => (s as any).bonusStatus === 'released').length * 100000;
  const mockLockedBonus = referredStores.filter((s) => (s as any).bonusStatus === 'locked' || (s as any).bonusStatus === undefined).length * 100000;
  const mockAvailable = Math.max(0, releasedProfit + mockReleasedBonus - totalApprovedPayouts);

  // If server balance is available, use exact database figures; otherwise use local calculation (no fake 2450000 baseline)
  const availableBalance = serverBalance ? serverBalance.availableAmount : mockMode ? mockAvailable : 0;
  const balanceLoaded = mockMode || (serverBalance !== null && serverBalance.storeId === currentStore.id);
  const releasedBonusTotal = serverBalance ? serverBalance.bonusAvailable : mockReleasedBonus;
  const lockedBonusTotal = serverBalance ? serverBalance.bonusLocked : mockLockedBonus;
  const referralCommissionEarned = serverBalance ? serverBalance.referralCommissionEarned : 0;

  // Orders awaiting customer delivery confirmation or pending review
  const pendingOrders = storeOrders.filter((o) => !o.profitReleased && o.status !== 'cancelled');
  const pendingBalance = pendingOrders.reduce((sum, o) => sum + (o.dropshipperProfit || 0), 0);

  // Payouts currently pending in queue
  const pendingPayoutReqs = storePayouts.filter((p) => p.status === 'pending');
  const reservedBalance = serverBalance ? serverBalance.unsettledAmount : pendingPayoutReqs.reduce((sum, p) => sum + p.amount, 0);

  const releaseText = ({ returnWindowDaysLeft: days }: { returnWindowDaysLeft?: unknown }): { text: string; urgent: boolean } => {
    if (typeof days !== 'number') return { text: 'مشتری هنوز نگفته بسته رسیده', urgent: false };
    if (days > 0) return { text: `${toPersianDigits(days)} روز دیگر سودش آزاد می‌شود`, urgent: days <= 2 };
    return { text: 'مهلتش تمام شده؛ سود به‌زودی آزاد می‌شود', urgent: false };
  };

  const handleServicePurchase = async (type: 'plan_renewal' | 'catalog_upgrade', cost: number) => {
    if (!isMockMode()) {
      try {
        await storeApi.purchaseService(currentStore.slug, type === 'plan_renewal' ? 'standard' : type, currentStore.id);
      } catch (err: any) {
        error(err.message || 'تراکنش انجام نشد.');
        return;
      }
      success('خدمت ثبت و مبلغ از کیف پول کسر شد.');
      setStatementRefresh(value => value + 1);
      try {
        setServerBalance(await storeApi.getOwnedBalance(currentStore.slug));
      } catch {
        info('خرید ثبت شد؛ برای دیدن موجودی تازه، صفحه را دوباره بارگذاری کن.');
      }
      return;
    }
    if (availableBalance < cost) {
      error(`موجودی قطعی قابل برداشت شما (${formatPrice(availableBalance)} ت) کمتر از مبلغ این خدمت (${formatPrice(cost)} ت) است. درآمدهای موقت در حال ارسال پس از تحویل قطعی آزاد خواهند شد.`);
      return;
    }
    const ok = purchaseServiceWithWallet(currentStore.id, type, cost);
    if (ok) {
      success('خدمت ثبت و مبلغ از کیف پول کسر شد.');
    } else {
      error('خطا در انجام تراکنش کیف پول.');
    }
  };

  const handleWithdraw = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isValidIranianSheba(shabaNumber)) {
      error('این شماره شبا درست نیست؛ باید با IR شروع شود و ۲۴ رقم داشته باشد.');
      return;
    }
    const amountNum = Number(withdrawAmount);
    if (amountNum < minimum) {
      const label = mockMode ? 'حداقل مبلغ قابل برداشت' : 'حداقل مبلغ برداشت زودهنگام';
      error(`${label} ${formatPrice(minimum)} تومان است.`);
      return;
    }
    if (amountNum > availableBalance) {
      error('این مبلغ از پول آماده برداشتت بیشتر است.');
      return;
    }
    if (!mockMode) {
      if (isVerifying) return;
      const refreshBreakdown = refreshWalletBreakdown.current;
      setIsVerifying(true);
      try {
        await storeApi.requestEarlyWithdrawal(currentStore.slug, amountNum, shabaNumber, currentStore.id);
        success('درخواست برداشت زودهنگام ثبت شد؛ تسویه حداکثر تا ۲۴ ساعت آینده انجام می‌شود.');
        setWithdrawAmount('');
        setStatementRefresh(value => value + 1);
        try {
          setServerBalance(await storeApi.getOwnedBalance(currentStore.slug));
          setServerPayouts(await storeApi.getPayouts(currentStore.slug));
          await refreshBreakdown?.();
          setEarlyWithdrawals(await storeApi.getEarlyWithdrawals(currentStore.slug));
        } catch {
          info('درخواست ثبت شد؛ برای دیدن مانده و وضعیت تازه، صفحه را دوباره بارگذاری کن.');
        }
      }
      catch (err: any) { error(err.message || 'ثبت برداشت انجام نشد.'); }
      finally { setIsVerifying(false); }
      return;
    }

    // Section 8: Real Name Matching Inquiry via Sheba verification
    setIsVerifying(true);
    info('در حال استعلام تطبیق نام صاحب شماره شبا از شاپرک...');
    const result = await verifyShebaOwnerName(shabaNumber, currentStore.ownerName || '');
    setIsVerifying(false);

    if (!result.success) {
      error(result.error || 'شماره شبا متعلق به شخص دیگری است و با مشخصات صاحب پنل تطابق ندارد.');
      return;
    }

    const ok = requestPayout(currentStore.id, amountNum, shabaNumber, result.bankName);
    if (ok) {
      setSubmitted(true);
      setStatementRefresh(value => value + 1);
      success(`استعلام تطبیق شبا به نام «${result.ownerName}» (${result.bankName}) تایید شد و درخواست واریز به صف مالی رفت.`);
      setTimeout(() => setSubmitted(false), 3000);
    }
  };

  const closeBreakdown = () => setBreakdownType(null);
  const breakdownTitle = breakdownType === 'pending' ? 'سفارش‌هایی که سودشان هنوز آزاد نشده' : 'واریزهایی که در راه حساب توست';
  const hasWithdrawalForm = balanceLoaded && breakdownLoaded && !breakdownFailed && availableBalance >= minimum;

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="کیف پول" description="موجودی آمادهٔ برداشت، وضعیت واریزها و گردش حساب فروشگاهت را ببین." icon={<Wallet />} />

      <div className="grid items-start gap-4 xl:grid-cols-2">
        <section className="min-w-0 rounded-[24px] bg-[#2c2050] p-5 text-white shadow-lg shadow-[#2c2050]/15 sm:p-6">
          <h3 className="text-sm font-bold text-[#e8e0fb]">پولی که همین حالا می‌توانی برداشت کنی</h3>
          {balanceLoaded ? (
            <strong className="mt-2 block break-words text-3xl font-black tabular-nums sm:text-4xl">{formatPrice(availableBalance)} <span className="text-base font-bold text-[#e8e0fb]">تومان</span></strong>
          ) : <p className="mt-2 text-sm text-[#e8e0fb]">{balanceLoadFailed ? 'دریافت موجودی انجام نشد.' : 'در حال دریافت موجودی…'}</p>}
          <p className="mt-2 text-xs leading-6 text-[#e8e0fb]">سود آزادشده پس از پایان مهلت ۷ روزهٔ مرجوعی</p>

          <details className="mt-3 border-t border-white/10 pt-2">
            <summary className={summaryClass + ' focus-visible:ring-white'}>جزئیات موجودی</summary>
            <div className="divide-y divide-white/10 text-sm">
              <div className="py-1">
                <button type="button" aria-haspopup="dialog" onClick={() => setBreakdownType('pending')} className="flex min-h-11 w-full flex-wrap items-center justify-between gap-x-3 rounded-lg px-2 text-start hover:bg-white/10 focus-visible:ring-2 focus-visible:ring-white">
                  <span className="flex items-center gap-2 text-[#e8e0fb]"><Eye aria-hidden="true" className="size-4 shrink-0" />سود سفارش‌های در راه (برآورد محلی):</span>
                  <strong className="tabular-nums">{formatPrice(pendingBalance)} تومان</strong>
                </button>
                {!serverBalance && <p className="px-2 pb-2 text-xs leading-6 text-[#e8e0fb]">این برآورد تا پایان مهلت مرجوعی در موجودی قابل برداشت حساب نمی‌شود.</p>}
              </div>

              <div className="py-1">
                <button type="button" aria-haspopup="dialog" onClick={() => setBreakdownType('reserved')} className="flex min-h-11 w-full flex-wrap items-center justify-between gap-x-3 rounded-lg px-2 text-start hover:bg-white/10 focus-visible:ring-2 focus-visible:ring-white">
                  <span className="flex items-center gap-2 text-[#e8e0fb]"><Eye aria-hidden="true" className="size-4 shrink-0" />پول در راه حساب بانکی‌ات</span>
                  <strong className="tabular-nums">{formatPrice(reservedBalance)} تومان</strong>
                </button>
              </div>

              <dl className="divide-y divide-white/10">
                <div className="flex flex-wrap justify-between gap-x-3 px-2 py-2">
                  <dt className="text-[#e8e0fb]">هدیه‌های آماده برداشت</dt>
                  <dd className="font-bold tabular-nums">{formatPrice(releasedBonusTotal)} تومان</dd>
                  {referralCommissionEarned > 0 && <dd className="w-full text-xs leading-6 text-[#e8e0fb]">{formatPrice(referralCommissionEarned)} تومان پاداش دعوت از همکار</dd>}
                </div>
                <div className="flex flex-wrap justify-between gap-x-3 px-2 py-2">
                  <dt className="text-[#e8e0fb]">هدیه‌های قفل‌شده</dt>
                  <dd className="font-bold tabular-nums">{formatPrice(lockedBonusTotal)} تومان</dd>
                  {lockedBonusTotal > 0 && <dd className="w-full text-xs leading-6 text-[#e8e0fb]">هدیه با اولین فروش فروشگاهی که دعوت کرده‌ای آزاد می‌شود.</dd>}
                </div>
              </dl>
            </div>
          </details>
        </section>

        <section className={panelClass}>
          <h3 className="flex items-center gap-2 text-base font-extrabold text-[#261b41]"><ArrowDownToLine aria-hidden="true" className="size-4 text-brand-deep" strokeWidth={1.5} /> برداشت پول به حساب بانکی‌ات</h3>
          {breakdownFailed && <p role="status" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">دریافت تنظیمات برداشت انجام نشد.</p>}
          {(!balanceLoaded || !breakdownLoaded) && !balanceLoadFailed && !breakdownFailed && (
            <p role="status" className="rounded-xl bg-slate-50 p-3 text-sm text-slate-600">در حال دریافت موجودی…</p>
          )}
          {balanceLoadFailed && <p className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">برای بررسی امکان برداشت، موجودی کیف پول لازم است.</p>}
          {balanceLoaded && breakdownLoaded && !breakdownFailed && availableBalance < minimum && (
            <p className="rounded-xl bg-slate-50 p-3 text-sm leading-7 text-slate-700">
              موجودی قابل برداشت شما {formatPrice(availableBalance)} تومان است. حداقل مبلغ برداشت زودهنگام {formatPrice(minimum)} تومان است؛ برداشت زودهنگام فعلاً ممکن نیست.
            </p>
          )}

          {hasWithdrawalForm && (
            <form onSubmit={handleWithdraw} className="space-y-3">
              <div className="space-y-1">
                <label htmlFor={shabaId} className="block text-sm font-bold text-slate-700">شماره شبای حسابت</label>
                <input
                  id={shabaId}
                  type="text"
                  name="shaba"
                  value={shabaNumber}
                  onChange={(e) => setShabaNumber(e.target.value)}
                  placeholder="IRxxxxxxxxxxxxxxxxxxxxxxxx"
                  aria-describedby={shabaId + '-help'}
                  className={fieldClass + ' font-mono tracking-wider'}
                  dir="ltr"
                  required
                />
                <p id={shabaId + '-help'} className="text-xs leading-6 text-slate-600"><bdi dir="ltr">IR</bdi> و سپس ۲۴ رقم شمارهٔ شبای حساب بانکی‌ات را وارد کن.</p>
                <div className="flex flex-wrap items-start justify-between gap-x-3 text-xs leading-6 text-slate-600">
                  {currentStore.ownerName ? <span>به نام مالک فروشگاه ({currentStore.ownerName})</span> : <span>نام مالک فروشگاه ثبت نشده است</span>}
                  <span className="flex items-center gap-1 font-bold text-emerald-700"><ShieldCheck aria-hidden="true" className="size-3.5" strokeWidth={1.5} /> درستی شبا را بررسی می‌کنیم</span>
                </div>
              </div>

              <div className="space-y-1">
                <label htmlFor={amountId} className="block text-sm font-bold text-slate-700">مبلغ برداشت (تومان)</label>
                <input
                  id={amountId}
                  type="number"
                  inputMode="numeric"
                  name="amount"
                  min={minimum}
                  max={availableBalance}
                  value={withdrawAmount}
                  onChange={(e) => setWithdrawAmount(e.target.value)}
                  aria-describedby={amountId + '-help'}
                  className={fieldClass + ' font-bold tabular-nums'}
                  required
                />
                <p id={amountId + '-help'} className="text-xs leading-6 text-slate-600">
                  {mockMode
                    ? 'کمترین برداشت: ' + formatPrice(minimum) + ' تومان'
                    : 'حداقل مبلغ برداشت زودهنگام: ' + formatPrice(minimum) + ' تومان (' + (earlyWithdrawalFeePct != null ? 'کارمزد ' + toPersianDigits(earlyWithdrawalFeePct) + '٪' : 'کارمزد طبق تنظیمات مدیر') + ')'}
                </p>
                <button type="button" onClick={() => setWithdrawAmount(String(availableBalance))} className="min-h-11 rounded-lg bg-brand-soft px-3 text-sm font-bold text-brand-deep hover:bg-brand-border focus-visible:ring-2 focus-visible:ring-brand">
                  برداشت همه موجودی ({formatPrice(availableBalance)} تومان)
                </button>
                {!mockMode && earlyWithdrawalFeePct != null && Number(withdrawAmount) >= minimum && (
                  <p className="text-xs leading-6 text-slate-700">
                    کارمزد برداشت زودهنگام ({toPersianDigits(earlyWithdrawalFeePct)}٪): {formatPrice(Math.round(Number(withdrawAmount) * earlyWithdrawalFeePct / 100))} تومان
                    <br />مبلغ خالص دریافتی: {formatPrice(Number(withdrawAmount) - Math.round(Number(withdrawAmount) * earlyWithdrawalFeePct / 100))} تومان
                  </p>
                )}
              </div>

              <button type="submit" disabled={isVerifying}
                className="flex min-h-12 w-full items-center justify-center gap-1.5 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50">
                {isVerifying ? 'در حال بررسی شبا…' : submitted ? (
                  <><CheckCircle2 aria-hidden="true" className="size-4" strokeWidth={1.5} /> درخواستت ثبت شد</>
                ) : 'ثبت درخواست برداشت'}
              </button>
            </form>
          )}
        </section>
      </div>

      {breakdownType && (
        <SupplierDialog titleId={breakdownTitleId} onDismiss={closeBreakdown}>
          <SupplierDialogHeading id={breakdownTitleId} title={breakdownTitle} onClose={closeBreakdown} />
          {breakdownType === 'pending' ? (
            pendingOrders.length === 0 ? (
              <p className="rounded-xl bg-slate-50 p-4 text-sm text-slate-600">سفارشی در انتظار آزاد شدن سود نیست.</p>
            ) : (
              <ul className="space-y-2">
                {pendingOrders.map(order => {
                  const release = releaseText(order as typeof order & { returnWindowDaysLeft?: unknown });
                  return (
                    <li key={order.id} className="space-y-1 rounded-xl bg-[#f6f3fb] p-3">
                      <div className="flex flex-wrap justify-between gap-x-3 text-sm font-bold text-slate-800">
                        <span>سفارش <bdi dir="ltr" className="break-all">{order.orderNumber}</bdi></span>
                        <span className="text-emerald-700">{formatPrice(order.dropshipperProfit || 0)} تومان</span>
                      </div>
                      <div className="flex flex-wrap justify-between gap-x-3 text-xs leading-6 text-slate-600">
                        <time dateTime={order.createdAt}>{new Date(order.createdAt).toLocaleDateString('fa-IR')}</time>
                        <span className={release.urgent ? 'font-bold text-rose-700' : 'font-bold text-amber-800'}>{release.text}</span>
                      </div>
                    </li>
                  );
                })}
              </ul>
            )
          ) : pendingPayoutReqs.length === 0 ? (
            <p className="rounded-xl bg-slate-50 p-4 text-sm text-slate-600">واریزی در راه نداری.</p>
          ) : (
            <ul className="space-y-2">
              {pendingPayoutReqs.map(payout => (
                <li key={payout.id} className="space-y-1 rounded-xl bg-[#f6f3fb] p-3">
                  <div className="flex flex-wrap justify-between gap-x-3 text-sm font-bold text-slate-800">
                    <span>درخواست واریز</span>
                    <span className="text-brand-deep">{formatPrice(payout.amount)} تومان</span>
                  </div>
                  <div className="flex flex-wrap justify-between gap-x-3 text-xs leading-6 text-slate-600">
                    <span>تاریخ درخواست: {payout.requestDate}</span>
                    <span className="font-bold text-brand-deep">در انتظار تأیید پلتفرم</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </SupplierDialog>
      )}

      {!mockMode && earlyWithdrawals.length > 0 && (
        <section className={panelClass}>
          <h3 className="text-base font-extrabold text-[#261b41]">برداشت‌های زودهنگام</h3>
          <ul className="divide-y divide-slate-100">
            {earlyWithdrawals.map(withdrawal => (
              <li key={withdrawal.id} className="flex flex-wrap items-center justify-between gap-x-3 py-3 text-sm first:pt-0 last:pb-0">
                <span className="text-slate-600">{formatPrice(withdrawal.grossAmount)} تومان (کارمزد {formatPrice(withdrawal.feeAmount)}، خالص {formatPrice(withdrawal.netAmount)})</span>
                <span className="rounded-lg bg-brand-soft px-2 py-1 text-xs font-bold text-brand-deep">{withdrawal.status}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {!mockMode && currentStore?.slug && currentStore.id && (
        <AccountStatement key={currentStore.id} role="seller" slug={currentStore.slug} storeId={currentStore.id} refresh={statementRefresh} />
      )}

      {!mockMode && breakdownLoaded && walletBreakdown && (
        <details className={panelClass}>
          <summary className={summaryClass + ' text-[#261b41] focus-visible:ring-brand'}>تفکیک موجودی کیف پول</summary>
          <dl className="grid grid-cols-2 gap-2 text-sm">
            <div className="rounded-xl bg-slate-50 p-3"><dt className="text-slate-600">سود سفارش‌های در راه</dt><dd className="font-bold text-slate-800">{formatPrice(walletBreakdown.pendingProfit)} تومان</dd></div>
            <div className="rounded-xl bg-slate-50 p-3"><dt className="text-slate-600">سود آزادشده قابل برداشت</dt><dd className="font-bold text-slate-800">{formatPrice(walletBreakdown.releasedProfit)} تومان</dd></div>
            <div className="rounded-xl bg-slate-50 p-3"><dt className="text-slate-600">برداشت‌های زودهنگام در انتظار</dt><dd className="font-bold text-slate-800">{formatPrice(walletBreakdown.pendingEarlyGross)} تومان</dd></div>
            {walletBreakdown.settlementSettings?.settlementDay != null && (
              <div className="rounded-xl bg-slate-50 p-3"><dt className="text-slate-600">روز تسویه ماهانه</dt><dd className="font-bold text-slate-800">{toPersianDigits(walletBreakdown.settlementSettings.settlementDay)}م هر ماه</dd></div>
            )}
          </dl>
        </details>
      )}

      <details className={panelClass}>
        <summary className={summaryClass + ' text-[#261b41] focus-visible:ring-brand'}>خرید خدمات با پول فروشگاه</summary>
        <p className="text-sm leading-6 text-slate-600">هزینهٔ خدمات از موجودی قابل برداشت کم می‌شود.</p>
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-[#f6f3fb] p-3">
          <div className="flex min-w-0 items-start gap-2">
            <div className="grid size-9 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand-deep"><Crown aria-hidden="true" className="size-4" strokeWidth={1.5} /></div>
            <div className="text-sm">
              <h3 className="font-bold text-slate-800">تمدید اشتراک استاندارد</h3>
              <p className="text-slate-600">۳ ماه دیگر با ظرفیت ۱۰۰ کالا</p>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-extrabold tabular-nums text-slate-900">۸۹۰٬۰۰۰ تومان</span>
            <button type="button" onClick={() => void handleServicePurchase('plan_renewal', 890000)}
              className="min-h-11 rounded-xl bg-brand-deep px-3 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2">خرید با کیف پول</button>
          </div>
        </div>
        <p className="flex items-start gap-2 rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-900">
          <Info aria-hidden="true" className="mt-1 size-4 shrink-0" strokeWidth={1.5} /> فروش بسته پیامک فعلاً متوقف است.
        </p>
      </details>
    </div>
  );
};
