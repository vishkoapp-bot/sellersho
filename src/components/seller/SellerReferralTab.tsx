import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi, StoreReferralItem } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { Users, Copy, Check, Gift, Share2, Lock, CheckCircle2 } from 'lucide-react';

export const SellerReferralTab: React.FC = () => {
  const { currentStore, stores, platformSettings, formatPrice, toPersianDigits } = useStore();
  const { success, info } = useToast();
  const [copiedLink, setCopiedLink] = useState(false);
  const [copiedCode, setCopiedCode] = useState(false);
  const [serverReferrals, setServerReferrals] = useState<StoreReferralItem[]>([]);

  useEffect(() => {
    // ISO-C: clear stale referrals on store switch - never show
    // the previous store's referrals under the new store.
    setServerReferrals([]);
    if (!isMockMode() && currentStore.ownerType === 'dropshipper' && currentStore.slug) {
      storeApi.getStoreReferrals(currentStore.slug)
        .then((data) => {
          if (Array.isArray(data)) {
            setServerReferrals(data);
          }
        })
        .catch((err) => { console.warn('Failed to load store referrals:', err);
      setServerReferrals([]); });

      // Query server for released bonus notifications without repeated toasts
      storeApi.getReferralNotifications(currentStore.slug)
        .then((notifs) => {
          if (Array.isArray(notifs) && notifs.length > 0) {
            const lastNotifiedKey = `drup_last_notified_bonus_${currentStore.slug}`;
            const lastNotifiedAt = localStorage.getItem(lastNotifiedKey) || '';
            const freshNotifs = notifs.filter((n) => n.releasedAt && n.releasedAt > lastNotifiedAt);
            if (freshNotifs.length > 0) {
              const latest = freshNotifs[0];
              info(latest.amount
                ? `هدیه ${formatPrice(latest.amount)} تومانی برای فروشگاه «${latest.referredStoreName}» قابل برداشت شد.`
                : `پاداش معرفی برای فروشگاه «${latest.referredStoreName}» قابل برداشت شد.`);
              localStorage.setItem(lastNotifiedKey, latest.releasedAt);
            }
          }
        })
        .catch(() => {});
    }
  }, [currentStore.ownerType, currentStore.slug]);

  const referralCode = currentStore.referralCode || `DRUP-${currentStore.id.slice(-4).toUpperCase()}`;
  const referralLink = `${window.location.origin}/?ref=${referralCode}`;
  const commissionPercent = platformSettings.referralProgram?.sellerCommissionPercent || 2;

  const localReferredStores: StoreReferralItem[] = stores
    .filter((s) => s.referrerSellerId === currentStore.id)
    .map((s) => ({
      id: s.id,
      name: s.name,
      slug: s.slug,
      ownerName: s.ownerName,
      totalSales: s.totalSales,
      isActivated: (s.totalSales || 0) > 0,
      bonusStatus: (s.totalSales || 0) > 0 ? 'released' : 'locked',
      // No bonusAmount here: the local fallback has no server amount
      // to show, and the platform amount must never be hardcoded.
    }));

  const activeReferrals = (!isMockMode() && serverReferrals.length > 0) ? serverReferrals : localReferredStores;

  const displayStores: StoreReferralItem[] = activeReferrals;

  const totalLockedBonus = displayStores
    .filter((s) => s.bonusStatus === 'locked')
    .reduce((sum, s) => sum + (s.bonusAmount ?? 0), 0);

  const totalReleasedBonus = displayStores
    .filter((s) => s.bonusStatus === 'released')
    .reduce((sum, s) => sum + (s.bonusAmount ?? 0), 0);

  // Calculate real 2% referral commission directly from server items (no local 0.1 GMV approximation)
  const totalNetworkPlatformF = displayStores.reduce((sum, s) => sum + (s.totalPlatformFee || 0), 0);
  const totalEarnedCommission = displayStores.reduce((sum, s) => sum + (s.earnedCommission || 0), 0);

  return (
    <div className="space-y-3 pb-6 select-none">
      <div className="bg-gradient-to-l from-brand-deep to-brand-deep rounded-2xl p-4 text-white shadow-xs space-y-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Gift className="w-5 h-5 text-indigo-300"  strokeWidth={1.5}/>
            <h2 className="text-sm font-black">همکار دعوت کن، پاداش بگیر</h2>
          </div>
          <span className="text-[10px] bg-emerald-500/20 text-emerald-300 px-2 py-0.5 rounded-full font-bold">پاداش را پلتفرم می‌دهد</span>
        </div>
        <p className="text-[11px] text-brand-border/90 leading-relaxed">
          وقتی کسی با لینک تو فروشگاه بسازد و شروع به فروش کند، از هر فروش او{' '}
          <strong className="text-amber-300">{toPersianDigits(commissionPercent)}٪ از کارمزد پلتفرم</strong>
          {' '}به کیف پول تو می‌آید. از جیب آن همکار چیزی کم نمی‌شود؛ پلتفرم از سهم خودش می‌دهد.
        </p>

        {/* Numerical Formula Explanation Box (Item 2) */}
        <div className="p-2 bg-white/10 rounded-xl text-[10px] text-brand-border border border-white/10 leading-relaxed">
          <strong className="text-white">چطور حساب می‌شود؟</strong> پاداش تو {toPersianDigits(commissionPercent)}٪ از کارمزد پلتفرمِ فروش همکارت است، نه از کل مبلغ سفارش.
          <div className="text-[10px] text-indigo-200 mt-0.5">
            مثال: در یک سفارش ۵۰۰ هزار تومانی، پاداش تو ۲ هزار تومان می‌شود.
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2 pt-1 border-t border-brand-deep/60 text-xs">
          <div className="bg-white/10 p-2 rounded-xl flex items-center justify-between">
            <span className="font-mono font-bold text-amber-300" dir="ltr">{referralCode}</span>
            <button
              onClick={() => {
                navigator.clipboard.writeText(referralCode);
                setCopiedCode(true);
                success('کد معرف کپی شد.');
                setTimeout(() => setCopiedCode(false), 2000);
              }}
              className="p-1 bg-white/20 rounded-md"
            >
              {copiedCode ? <Check className="w-3.5 h-3.5 text-emerald-400"  strokeWidth={1.5}/> : <Copy className="w-3.5 h-3.5"  strokeWidth={1.5}/>}
            </button>
          </div>
          <div className="bg-white/10 p-2 rounded-xl flex items-center justify-between">
            <span className="text-[10px] truncate text-brand-border" dir="ltr">{referralLink}</span>
            <button
              onClick={() => {
                navigator.clipboard.writeText(referralLink);
                setCopiedLink(true);
                success('لینک دعوت کپی شد.');
                setTimeout(() => setCopiedLink(false), 2000);
              }}
              className="p-1 bg-brand rounded-md text-[10px] font-bold flex items-center gap-1"
            >
              <Share2 className="w-3 h-3"  strokeWidth={1.5}/>
              <span>{copiedLink ? 'کپی شد' : 'کپی'}</span>
            </button>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-2 text-center text-xs">
        <div className="bg-white p-2.5 rounded-xl border border-slate-200">
          <span className="text-[10px] text-slate-400 block">هدیه آماده برداشت</span>
          <span className="font-extrabold text-emerald-600 font-mono text-[12px]">+{formatPrice(totalReleasedBonus)} ت</span>
          <span className="text-[9px] text-emerald-600 block mt-0.5">می‌توانی برداشتش کنی</span>
        </div>
        <div className="bg-white p-2.5 rounded-xl border border-slate-200">
          <span className="text-[10px] text-slate-400 block">هدیه در انتظار</span>
          <span className="font-extrabold text-amber-600 font-mono text-[12px]">{formatPrice(totalLockedBonus)} ت</span>
          <span className="text-[9px] text-amber-600 block mt-0.5">تا اولین فروش همکارت قفل است</span>
        </div>
        <div className="bg-emerald-50 p-2.5 rounded-xl border border-emerald-200">
          <span className="text-[10px] text-emerald-700 block font-bold">پاداش از فروش همکارها</span>
          <span className="font-extrabold text-emerald-800 font-mono text-[12px]">+{formatPrice(totalEarnedCommission)} ت</span>
          <span className="text-[9px] text-emerald-600 block mt-0.5">{toPersianDigits(commissionPercent)}٪ از کارمزد پلتفرم</span>
        </div>
      </div>

      <div className="bg-white rounded-2xl p-3.5 border border-slate-200 shadow-2xs space-y-2.5">
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <h3 className="text-xs font-bold text-slate-800 flex items-center gap-1.5">
            <Users className="w-4 h-4 text-indigo-600"  strokeWidth={1.5}/>
            <span>فروشگاه‌هایی که با دعوت تو ساخته شده‌اند ({toPersianDigits(displayStores.length)} فروشگاه)</span>
          </h3>
          <span className="text-[10px] text-slate-400 font-medium">برای هر فروشگاه، هدیه می‌گیری</span>
        </div>
        <div className="space-y-2">
          {displayStores.length === 0 && (
            <div className="p-5 text-center space-y-1.5">
              <p className="text-xs font-bold text-slate-600">هنوز کسی با دعوت تو فروشگاه نساخته است</p>
              <p className="text-[11px] text-slate-400 leading-6">لینک یا کد دعوت بالای همین صفحه را برای دوست‌هایی بفرست که می‌خواهند فروشگاه بزنند؛ وقتی شروع به فروش کنند، هم هدیه می‌گیری، هم از فروش‌هایشان پاداش.</p>
            </div>
          )}
          {displayStores.map((st) => {
            const isReleased = st.bonusStatus === 'released';
            return (
              <div key={st.id} className="p-3 bg-slate-50/80 border border-slate-200/70 rounded-xl flex items-center justify-between text-xs transition-all hover:bg-slate-50">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="font-bold text-slate-800">{st.name}</span>
                    {st.isActivated ? (
                      <span className="inline-flex items-center gap-1 text-[9px] font-bold px-2 py-0.5 bg-emerald-100 text-emerald-800 rounded-full border border-emerald-200">
                        <CheckCircle2 className="w-3 h-3 text-emerald-600"  strokeWidth={1.5}/>
                        <span>شروع به فروش کرده</span>
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 text-[9px] font-bold px-2 py-0.5 bg-amber-100 text-amber-800 rounded-full border border-amber-200">
                        <Lock className="w-3 h-3 text-amber-600"  strokeWidth={1.5}/>
                        <span>هنوز فروشی نداشته</span>
                      </span>
                    )}
                  </div>
                  <span className="text-[10px] text-slate-500 block">
                    مدیر: {st.ownerName} • فروش کل: {formatPrice(st.totalSales)} ت
                  </span>
                </div>
                <div className="text-left shrink-0 space-y-0.5">
                  <span className="text-[10px] text-slate-400 block">هدیه این فروشگاه:</span>
                  {st.bonusStatus === 'none' ? (
                    <span className="inline-block text-slate-400 bg-slate-100 border border-slate-200 px-2 py-0.5 rounded-lg text-[10px]">
                      هدیه ندارد
                    </span>
                  ) : isReleased ? (
                    <span className="inline-block font-extrabold text-emerald-700 bg-emerald-50 border border-emerald-200 px-2 py-0.5 rounded-lg font-mono text-[11px]">
                      {st.bonusAmount != null ? `+${formatPrice(st.bonusAmount)} ت (قابل برداشت)` : 'قابل برداشت'}
                    </span>
                  ) : (
                    <span className="inline-block font-bold text-amber-700 bg-amber-50 border border-amber-200 px-2 py-0.5 rounded-lg font-mono text-[11px]">
                      {st.bonusAmount != null ? `${formatPrice(st.bonusAmount)} ت (هنوز قفل است)` : 'هنوز قفل است'}
                    </span>
                  )}
                  {st.earnedCommission ? (
                    <span className="text-[9px] text-emerald-600 block">
                      +{formatPrice(st.earnedCommission)} ت پاداش از فروش‌هایش
                    </span>
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};