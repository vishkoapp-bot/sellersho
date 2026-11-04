import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi, StoreBalanceResponse, WalletBreakdownResponse, EarlyWithdrawalResponse } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { isValidIranianSheba } from '../../utils/validators';
import { Wallet, ArrowDownToLine, Clock, CheckCircle2, AlertCircle, Eye, ShieldCheck, X, Crown, MessageSquare, Sparkles, Info } from 'lucide-react';

export const SellerWalletTab: React.FC = () => {
  const {
    currentStore,
    stores,
    orders,
    payoutRequests: localPayoutRequests,
    requestPayout,
    verifyShebaOwnerName,
    purchaseServiceWithWallet,
    platformSettings,
    formatPrice,
    toPersianDigits,
  } = useStore();
  const { success, error, info } = useToast();
  const [shabaNumber, setShabaNumber] = useState('');
  const [withdrawAmount, setWithdrawAmount] = useState('');
  const [isVerifying, setIsVerifying] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [breakdownType, setBreakdownType] = useState<'pending' | 'reserved' | null>(null);
  const [serverBalance, setServerBalance] = useState<StoreBalanceResponse | null>(null);
  // FIX 14: wallet breakdown + early-withdrawal list, wired to the
  // monthly-settlement endpoints (previously unwired).
  const [walletBreakdown, setWalletBreakdown] = useState<WalletBreakdownResponse | null>(null);
  const [earlyWithdrawals, setEarlyWithdrawals] = useState<EarlyWithdrawalResponse[]>([]);
  const [serverPayouts, setServerPayouts] = useState<typeof localPayoutRequests>([]);
  const [serverOrders, setServerOrders] = useState<typeof orders | null>(null);
  const payoutRequests = isMockMode() ? localPayoutRequests : serverPayouts;

  // Fetch real authoritative store balance from server
  useEffect(() => {
    if (!isMockMode() && currentStore.id) {
      setServerBalance(null);
      setWalletBreakdown(null);
      setEarlyWithdrawals([]);
      setServerOrders(null);
      setServerPayouts([]);
      storeApi.getStoreOrders(currentStore.slug).then(setServerOrders).catch(() => setServerOrders(null));
      storeApi.getPayouts(currentStore.slug).then(setServerPayouts).catch((err) => error(err.message));
      storeApi.getOwnedBalance(currentStore.slug)
        .then((res) => {
          if (res && typeof res.availableAmount === 'number') {
            setServerBalance(res);
          }
        })
        .catch((err) => error(err.message));
      storeApi.getWalletBreakdown(currentStore.slug).then(setWalletBreakdown).catch(() => {});
      storeApi.getEarlyWithdrawals(currentStore.slug).then(setEarlyWithdrawals).catch(() => {});
    }
  }, [currentStore.id]);

  // Derive actual balances based on real released vs pending orders
  // Live kn: server orders when fetched, otherwise context orders.
  const storeOrders = serverOrders ?? orders.filter((o) => o.storeId === currentStore.id);
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
  const availableBalance = serverBalance ? serverBalance.availableAmount : isMockMode() ? mockAvailable : 0;
  const releasedBonusTotal = serverBalance ? serverBalance.bonusAvailable : mockReleasedBonus;
  const lockedBonusTotal = serverBalance ? serverBalance.bonusLocked : mockLockedBonus;
  const referralCommissionEarned = serverBalance ? serverBalance.referralCommissionEarned : 0;

  // Orders awaiting customer delivery confirmation or pending review
  const pendingOrders = storeOrders.filter((o) => !o.profitReleased && o.status !== 'cancelled');
  const pendingBalance = pendingOrders.reduce((sum, o) => sum + (o.dropshipperProfit || 0), 0);

  // Payouts currently pending in queue
  const pendingPayoutReqs = storePayouts.filter((p) => p.status === 'pending');
  const reservedBalance = serverBalance ? serverBalance.unsettledAmount : pendingPayoutReqs.reduce((sum, p) => sum + p.amount, 0);

  const totalPaidOut = totalApprovedPayouts;

  // Live parity (W1): return-window banner (live kn oe/U). Shown when
  // there is no server balance yet, or when the server reports a
  // locked-in-return-window figure; hidden when a server balance
  // exists but reports no such figure. The extra fields are read via
  // cast: StoreBalanceResponse (outside this file's scope) declares
  // none of them, and the live backend's field name is unproven, so
  // every live alias is accepted.
  const balanceExtras = serverBalance as (StoreBalanceResponse & { locked_in_return_window?: number; lockedInReturnWindow?: number; pendingRelease?: number; pending_release?: number; pendingReserve?: number; pending_reserve?: number }) | null;
  const lockedFigure = balanceExtras ? (balanceExtras.locked_in_return_window ?? balanceExtras.lockedInReturnWindow ?? balanceExtras.pendingRelease ?? balanceExtras.pending_release ?? balanceExtras.pendingReserve ?? balanceExtras.pending_reserve ?? null) : null;
  const showReturnBanner = serverBalance ? lockedFigure !== null : true;
  const returnBannerAmount = lockedFigure !== null ? lockedFigure : pendingBalance;

  const releaseText = (o: { returnWindowDaysLeft?: unknown }): { text: string; urgent: boolean } => {
    const days = (o as { returnWindowDaysLeft?: unknown }).returnWindowDaysLeft;
    if (typeof days !== 'number') return { text: 'مشتری هنوز نگفته بسته رسیده', urgent: false };
    if (days > 0) return { text: `${toPersianDigits(days)} روز دیگر سودش آزاد می‌شود`, urgent: days <= 2 };
    return { text: 'مهلتش تمام شده؛ سود به‌زودی آزاد می‌شود', urgent: false };
  };

  const handleServicePurchase = async (type: 'plan_renewal' | 'sms_500' | 'sms_2000' | 'catalog_upgrade', cost: number, title: string) => {
    if (!isMockMode()) {
      try { await storeApi.purchaseService(currentStore.slug, type === 'plan_renewal' ? 'standard' : type); setServerBalance(await storeApi.getOwnedBalance(currentStore.slug)); success('خدمت ثبت و مبلغ از کیف پول کسر شد.'); }
      catch (err: any) { error(err.message || 'تراکنش انجام نشد.'); }
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
    // FIX 14: non-mock withdrawals go through the early-withdrawal
    // endpoint (monthly-settlement rule): floor 1,000,000 تومان and a
    // 5% fee (from admin settings). The mock path keeps its old
    // 500,000 floor unchanged.
    if (!isMockMode() && amountNum < 1000000) {
      error('حداقل مبلغ برداشت زودهنگام ۱,۰۰۰,۰۰۰ تومان است.');
      return;
    }
    if (isMockMode() && amountNum < 500000) {
      error('حداقل مبلغ قابل برداشت ۵۰۰,۰۰۰ تومان است.');
      return;
    }
    if (amountNum > availableBalance) {
      error('این مبلغ از پول آماده برداشتت بیشتر است.');
      return;
    }
    if (!isMockMode()) {
      if (isVerifying) return;
      setIsVerifying(true);
      try {
        await storeApi.requestEarlyWithdrawal(currentStore.slug, amountNum, shabaNumber);
        setServerBalance(await storeApi.getOwnedBalance(currentStore.slug));
        setServerPayouts(await storeApi.getPayouts(currentStore.slug));
        setWalletBreakdown(await storeApi.getWalletBreakdown(currentStore.slug));
        setEarlyWithdrawals(await storeApi.getEarlyWithdrawals(currentStore.slug));
        success('درخواست برداشت زودهنگام ثبت شد؛ تسویه حداکثر تا ۲۴ ساعت آینده انجام می‌شود.');
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
      success(`استعلام تطبیق شبا به نام «${result.ownerName}» (${result.bankName}) تایید شد و درخواست واریز به صف مالی رفت.`);
      setTimeout(() => setSubmitted(false), 3000);
    }
  };

  return (
    <div className="space-y-3.5 pb-6">
      <div className="rounded-2xl border border-brand-border bg-brand-soft/50 px-3.5 py-2.5">
        <div className="flex items-start gap-2">
          <Info className="w-4 h-4 text-brand-deep shrink-0 mt-0.5" strokeWidth={1.5} />
          <p className="flex-1 text-[11px] leading-5 text-slate-600">این‌جا پول فروشگاهت را می‌بینی: چقدر آماده برداشت داری، چقدر هنوز در راه است، و از همین‌جا می‌توانی پولت را به حساب بانکی‌ات بفرستی.</p>
        </div>
      </div>

      {/* 1. Main Wallet Card */}
      <div className="bg-gradient-to-l from-slate-900 to-slate-800 rounded-2xl p-4 text-white shadow-xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="w-9 h-9 rounded-xl bg-white/10 flex items-center justify-center text-emerald-400">
              <Wallet className="w-5 h-5"  strokeWidth={1.5}/>
            </div>
            <div>
              <span className="text-[11px] text-slate-400 block">پولی که همین حالا می‌توانی برداشت کنی</span>
              <span className="text-xl font-black text-emerald-400">
                {formatPrice(availableBalance)}
                <span className="text-xs font-normal text-slate-300 mr-1">تومان</span>
              </span>
            </div>
          </div>
          <span className="text-[10px] bg-amber-500/20 text-amber-300 border border-amber-500/30 px-2 py-0.5 rounded-full font-bold">
            واریز به حساب بانکی
          </span>
        </div>

        {/* Section 7: Clickable breakdown cards */}
        <div className="grid grid-cols-2 gap-2 pt-2.5 border-t border-slate-700 text-[11px]">
          <div
            onClick={() => setBreakdownType('pending')}
            className="p-1.5 rounded-xl bg-white/5 hover:bg-white/10 cursor-pointer transition-colors"
            title="بزن تا سفارش‌هایش را ببینی"
          >
            <div className="flex items-center justify-between text-slate-400">
              <span>سود سفارش‌های در راه:</span>
              <Eye className="w-3 h-3 text-amber-300"  strokeWidth={1.5}/>
            </div>
            <span className="font-bold text-amber-300 block text-xs mt-0.5">{formatPrice(pendingBalance)} ت</span>
          </div>

          <div className="p-1.5 rounded-xl bg-white/5">
            <span className="text-slate-400 block">هدیه‌های آماده برداشت:</span>
            <span className="font-bold text-emerald-400 block text-xs mt-0.5">+{formatPrice(releasedBonusTotal)} ت</span>
            {referralCommissionEarned > 0 && (
              <span className="text-[9px] text-teal-300 block mt-0.5 font-mono">
                +{formatPrice(referralCommissionEarned)} ت پاداش دعوت از همکار
              </span>
            )}
          </div>

          <div className="p-1.5 rounded-xl bg-white/5">
            <span className="text-slate-400 block" title="وقتی فروشگاهی که دعوت کرده‌ای اولین فروشش را انجام دهد، آزاد می‌شود">هدیه‌های قفل‌شده:</span>
            <span className="font-bold text-amber-400 block text-xs mt-0.5">{formatPrice(lockedBonusTotal)} ت</span>
          </div>

          <div
            onClick={() => setBreakdownType('reserved')}
            className="p-1.5 rounded-xl bg-white/5 hover:bg-white/10 cursor-pointer transition-colors"
            title="بزن تا واریزهای در راه را ببینی"
          >
            <div className="flex items-center justify-between text-slate-400">
              <span>پول در راه حساب بانکی‌ات:</span>
              <Eye className="w-3 h-3 text-brand"  strokeWidth={1.5}/>
            </div>
            <span className="font-bold text-brand block text-xs mt-0.5">{formatPrice(reservedBalance)} ت</span>
          </div>
        </div>
      </div>

      {/* Live parity (W1): return-window banner. */}
      {showReturnBanner && (
        <div className="rounded-2xl border border-amber-200/80 bg-amber-50 p-3.5 space-y-2">
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 rounded-xl bg-amber-100 text-amber-700 flex items-center justify-center shrink-0">
                <Clock className="w-4 h-4" strokeWidth={1.5} />
              </div>
              <h3 className="text-xs font-bold text-slate-800">در انتظار پایان مهلت مرجوعی</h3>
            </div>
            {returnBannerAmount > 0 ? (
              <span className="text-base font-black text-amber-700">{formatPrice(returnBannerAmount)}<span className="text-[10px] font-normal mr-1">تومان</span></span>
            ) : (
              <span className="text-[11px] font-bold text-slate-400">موردی نیست</span>
            )}
          </div>
          <p className="text-[10px] text-slate-500 leading-relaxed">سود سفارش تا پایان مهلت مرجوعی مشتری نگه داشته می‌شود و سپس کامل آزاد می‌شود.</p>
        </div>
      )}

      {/* 2. Section 7: Modal Breakdown for Orders */}
      {/* Live parity (W1): real breakdown lists (live kn modal). The
          previous hard-coded example rows were fake data and are
          removed; this modal now renders real orders/payouts. */}
      {breakdownType && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl border border-slate-100 space-y-3 text-xs">
            <div className="flex items-center justify-between border-b border-slate-100 pb-2">
              <h4 className="font-bold text-slate-900">
                {breakdownType === 'pending' ? 'سفارش‌هایی که سودشان هنوز آزاد نشده' : 'واریزهایی که در راه حساب توست'}
              </h4>
              <button onClick={() => setBreakdownType(null)} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
                <X className="w-4 h-4" strokeWidth={1.5} />
              </button>
            </div>

            <div className="space-y-2 max-h-[60vh] overflow-y-auto no-scrollbar">
              {breakdownType === 'pending' ? (
                pendingOrders.length === 0 ? (
                  <div className="p-6 text-center text-slate-400">سفارشی نیست که سودش مانده باشد.</div>
                ) : (
                  pendingOrders.map((o) => {
                    const rel = releaseText(o as unknown as { returnWindowDaysLeft?: unknown });
                    return (
                      <div key={o.id} className="p-2.5 bg-slate-50 rounded-xl space-y-1 border border-slate-100">
                        <div className="flex justify-between font-bold text-slate-800">
                          <span>سفارش {o.orderNumber}</span>
                          <span className="text-emerald-700">+{formatPrice(o.dropshipperProfit || 0)} ت</span>
                        </div>
                        <div className="flex justify-between text-[10px] text-slate-400">
                          <span>تاریخ ثبت: {new Date(o.createdAt).toLocaleDateString('fa-IR')}</span>
                          <span className={`font-bold ${rel.urgent ? 'text-rose-600' : 'text-amber-600'}`}>{rel.text}</span>
                        </div>
                      </div>
                    );
                  })
                )
              ) : pendingPayoutReqs.length === 0 ? (
                <div className="p-6 text-center text-slate-400">واریزی در راه نداری.</div>
              ) : (
                pendingPayoutReqs.map((p) => (
                  <div key={p.id} className="p-2.5 bg-slate-50 rounded-xl space-y-1 border border-slate-100">
                    <div className="flex justify-between font-bold text-slate-800">
                      <span>درخواست واریز</span>
                      <span className="text-brand-deep">{formatPrice(p.amount)} تومان</span>
                    </div>
                    <div className="flex justify-between text-[10px] text-slate-400">
                      <span>تاریخ درخواست: {p.requestDate}</span>
                      <span className="text-brand-deep font-bold">در انتظار تأیید پلتفرم</span>
                    </div>
                  </div>
                ))
              )}
            </div>

            <button
              onClick={() => setBreakdownType(null)}
              className="w-full py-2 bg-slate-900 text-white font-bold text-xs rounded-xl"
            >
              بستن
            </button>
          </div>
        </div>
      )}

      {/* FIX 14: wallet breakdown (pending vs released profit) + early-withdrawal list, from the settlement endpoints. */}
      {!isMockMode() && walletBreakdown && (
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
          <h3 className="text-xs font-bold text-slate-800">تفکیک موجودی کیف پول</h3>
          <div className="grid grid-cols-2 gap-2 text-[11px]">
            <div className="bg-slate-50 rounded-xl p-2.5"><span className="text-slate-500 block">سود سفارش‌های در راه</span><span className="font-bold text-slate-800">{formatPrice(walletBreakdown.pendingProfit)} تومان</span></div>
            <div className="bg-slate-50 rounded-xl p-2.5"><span className="text-slate-500 block">سود آزادشده قابل برداشت</span><span className="font-bold text-slate-800">{formatPrice(walletBreakdown.releasedProfit)} تومان</span></div>
            <div className="bg-slate-50 rounded-xl p-2.5"><span className="text-slate-500 block">برداشت‌های زودهنگام در انتظار</span><span className="font-bold text-slate-800">{formatPrice(walletBreakdown.pendingEarlyGross)} تومان</span></div>
            {walletBreakdown.settlementSettings?.settlementDay != null && (
              <div className="bg-slate-50 rounded-xl p-2.5"><span className="text-slate-500 block">روز تسویه ماهانه</span><span className="font-bold text-slate-800">{toPersianDigits(walletBreakdown.settlementSettings.settlementDay)}م هر ماه</span></div>
            )}
          </div>
        </div>
      )}
      {!isMockMode() && earlyWithdrawals.length > 0 && (
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
          <h3 className="text-xs font-bold text-slate-800">برداشت‌های زودهنگام</h3>
          {earlyWithdrawals.map((w) => (
            <div key={w.id} className="flex items-center justify-between text-[11px] border-b border-slate-100 last:border-0 pb-2 last:pb-0">
              <span className="text-slate-600">{formatPrice(w.grossAmount)} تومان (کارمزد {formatPrice(w.feeAmount)} — خالص {formatPrice(w.netAmount)})</span>
              <span className="font-bold text-slate-800">{w.status}</span>
            </div>
          ))}
        </div>
      )}

      {/* 3. Section 8: Withdrawal Request Form with Sheba Name Matching */}
      <form onSubmit={handleWithdraw} className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-1.5 text-brand-deep">
          <ArrowDownToLine className="w-4 h-4"  strokeWidth={1.5}/>
          <h3 className="text-xs font-bold text-slate-800">برداشت پول به حساب بانکی‌ات</h3>
        </div>

        <div>
          <label className="text-[11px] font-bold text-slate-700 block mb-1">
            شماره شبای حسابت
          </label>
          <input
            type="text"
            value={shabaNumber}
            onChange={(e) => setShabaNumber(e.target.value)}
            placeholder="IRxxxxxxxxxxxxxxxxxxxxxxxx"
            className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-mono tracking-wider focus:outline-hidden placeholder:text-slate-300"
            dir="ltr"
            required
          />
          <p className="text-[10px] text-slate-400 mt-1 leading-relaxed">شبا شماره مخصوص حساب بانکی توست؛ پول فقط به حسابی می‌رود که شبایش را این‌جا می‌نویسی. شبا را در اپ بانکی‌ات یا از بانک می‌گیری؛ با IR شروع می‌شود.</p>
          <div className="flex items-center justify-between text-[9px] text-slate-400 mt-1">
            {currentStore.ownerName ? <span>به نام مالک فروشگاه ({currentStore.ownerName})</span> : <span>نام مالک فروشگاه ثبت نشده است</span>}
            <span className="text-emerald-700 flex items-center gap-0.5 font-bold">
              <ShieldCheck className="w-3 h-3"  strokeWidth={1.5}/>
              <span>درستی شبا را بررسی می‌کنیم</span>
            </span>
          </div>
        </div>

        <div>
          <label className="text-[11px] font-bold text-slate-700 block mb-1">
            چقدر برمی‌داری؟ (تومان)
          </label>
          <input
            type="number"
            min={isMockMode() ? 500000 : 1000000}
            max={availableBalance}
            value={withdrawAmount}
            onChange={(e) => setWithdrawAmount(e.target.value)}
            className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
            required
          />
          <span className="text-[9px] text-slate-400 mt-0.5 block">
            {isMockMode() ? 'کمترین برداشت: ۵۰۰٬۰۰۰ تومان' : walletBreakdown?.settlementSettings?.earlyWithdrawalFeePct != null ? `حداقل مبلغ برداشت زودهنگام: ۱,۰۰۰,۰۰۰ تومان (کارمزد ${toPersianDigits(walletBreakdown.settlementSettings.earlyWithdrawalFeePct)}٪)` : 'حداقل مبلغ برداشت زودهنگام: ۱,۰۰۰,۰۰۰ تومان (کارمزد طبق تنظیمات مدیر)'}
          </span>
          {availableBalance >= (isMockMode() ? 500000 : 1000000) && (
            <button type="button" onClick={() => setWithdrawAmount(String(availableBalance))} className="text-[11px] font-bold text-brand-deep bg-brand-soft px-2.5 py-1.5 rounded-lg">
              برداشت همه موجودی ({formatPrice(availableBalance)} تومان)
            </button>
          )}
          {!isMockMode() && walletBreakdown?.settlementSettings?.earlyWithdrawalFeePct != null && Number(withdrawAmount) >= 1000000 && (
            <span className="text-[11px] text-slate-600 mt-1 block">
              کارمزد برداشت زودهنگام ({toPersianDigits(walletBreakdown.settlementSettings.earlyWithdrawalFeePct)}٪): {formatPrice(Math.round(Number(withdrawAmount) * walletBreakdown.settlementSettings.earlyWithdrawalFeePct / 100))} تومان — مبلغ خالص دریافتی: {formatPrice(Number(withdrawAmount) - Math.round(Number(withdrawAmount) * walletBreakdown.settlementSettings.earlyWithdrawalFeePct / 100))} تومان
            </span>
          )}
        </div>

        <button
          type="submit"
          disabled={isVerifying}
          className="w-full py-3 min-h-[48px] bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-xl shadow-xs transition-colors flex items-center justify-center gap-1.5 disabled:opacity-50"
        >
          {isVerifying ? (
            <span>در حال بررسی شبا…</span>
          ) : submitted ? (
            <>
              <CheckCircle2 className="w-4 h-4"  strokeWidth={1.5}/>
              <span>درخواستت ثبت شد ✓</span>
            </>
          ) : (
            <>
              <ArrowDownToLine className="w-4 h-4"  strokeWidth={1.5}/>
              <span>ثبت درخواست برداشت</span>
            </>
          )}
        </button>
      </form>

      {/* Requirement 7: Wallet Services (Plan renewal, SMS recharge directly from available wallet balance) */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-brand-deep">
            <Sparkles className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">خرید خدمات با پول فروشگاه</h3>
          </div>
          <span className="text-[10px] text-emerald-700 font-bold bg-emerald-50 border border-emerald-200 px-2 py-0.5 rounded-full">
            پول آماده برداشت: {formatPrice(availableBalance)} تومان
          </span>
        </div>

        <p className="text-[10px] text-slate-500 leading-relaxed">
          این خدمات را می‌توانی مستقیم با پولی که در کیف پولت آماده برداشت است بخری؛ لازم نیست کارت بانکی بزنی. فقط پول آزادشده حساب می‌شود، نه سود سفارش‌هایی که هنوز در راه‌اند.
        </p>

        <div className="space-y-2 pt-1">
          {/* Service 1: Plan Renewal */}
          <div className="p-3 bg-slate-50 border border-slate-200/80 rounded-xl flex items-center justify-between gap-2">
            <div className="flex items-center gap-2.5">
              <div className="w-8 h-8 rounded-xl bg-amber-50 text-amber-600 flex items-center justify-center shrink-0">
                <Crown className="w-4 h-4"  strokeWidth={1.5}/>
              </div>
              <div>
                <span className="font-bold text-xs text-slate-800 block">تمدید اشتراک فروشگاه (پلن استاندارد ۳ ماهه)</span>
                <span className="text-[10px] text-slate-400 block mt-0.5">فروشگاهت ۳ ماه دیگر فعال می‌ماند + تا ۱۰۰ کالا</span>
              </div>
            </div>
            <div className="text-left shrink-0">
              <span className="text-xs font-black text-slate-900 block font-mono">۸۹۰٬۰۰۰ تومان</span>
              <button
                type="button"
                onClick={() => handleServicePurchase('plan_renewal', 890000, 'تمدید پلن ۳ ماهه')}
                className="mt-1 px-3 py-1 bg-brand-deep hover:bg-brand-deep text-white font-bold text-[10px] rounded-lg shadow-2xs transition-colors"
              >
                خرید با کیف پول
              </button>
            </div>
          </div>

          {/* Service 2: SMS 500 pack */}
          <div className="p-3 bg-slate-50 border border-slate-200/80 rounded-xl flex items-center justify-between gap-2">
            <div className="flex items-center gap-2.5">
              <div className="w-8 h-8 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center shrink-0">
                <MessageSquare className="w-4 h-4"  strokeWidth={1.5}/>
              </div>
              <div>
                <span className="font-bold text-xs text-slate-800 block">بسته شارژ پیامک ۵۰۰ عددی</span>
                <span className="text-[10px] text-slate-400 block mt-0.5">اطلاع‌رسانی وضعیت فاکتور و کد رهگیری به خریداران</span>
              </div>
            </div>
            <div className="text-left shrink-0">
              <span className="text-xs font-black text-slate-900 block font-mono">۱۵۰٬۰۰۰ تومان</span>
              <button
                type="button"
                onClick={() => handleServicePurchase('sms_500', 150000, 'بسته پیامک ۵۰۰ عددی')}
                className="mt-1 px-3 py-1 bg-brand-deep hover:bg-brand-deep text-white font-bold text-[10px] rounded-lg shadow-2xs transition-colors"
              >
                خرید با کیف پول
              </button>
            </div>
          </div>

          {/* Service 3: SMS 2000 pack */}
          <div className="p-3 bg-slate-50 border border-slate-200/80 rounded-xl flex items-center justify-between gap-2">
            <div className="flex items-center gap-2.5">
              <div className="w-8 h-8 rounded-xl bg-emerald-50 text-emerald-600 flex items-center justify-center shrink-0">
                <MessageSquare className="w-4 h-4"  strokeWidth={1.5}/>
              </div>
              <div>
                <span className="font-bold text-xs text-slate-800 block">بسته شارژ پیامک ۲۰۰۰ عددی اقتصادی</span>
                <span className="text-[10px] text-slate-400 block mt-0.5">همراه با پیامک اختصاصی خوشامدگویی و پیشنهاد ویژه</span>
              </div>
            </div>
            <div className="text-left shrink-0">
              <span className="text-xs font-black text-slate-900 block font-mono">۴۸۰٬۰۰۰ تومان</span>
              <button
                type="button"
                onClick={() => handleServicePurchase('sms_2000', 480000, 'بسته پیامک ۲۰۰۰ عددی')}
                className="mt-1 px-3 py-1 bg-brand-deep hover:bg-brand-deep text-white font-bold text-[10px] rounded-lg shadow-2xs transition-colors"
              >
                خرید با کیف پول
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
