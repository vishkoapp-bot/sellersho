import { HelpExplainer } from '../common/HelpExplainer';
import React from 'react';
import { Crown, Check, Store, Plus } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi } from '../../services/storeApi';
import type { PlatformSettings, SubscriptionPlan } from '../../types';

// G7 hardening as exported pure helpers (Batch 3 fix, M-3), pinned
// by SellerSubscriptionTab.test.ts on the Batch 1 pattern (cf.
// SellerDomainsPanel.test.tsx). The tab itself cannot be rendered
// in this harness — useStore() throws 'A store must be selected…'
// (StoreContext.tsx:1343-1345) before the tab body runs — so a
// render test with currentStore = null is untestable at tab level;
// these helpers pin the reachable part of the hardening instead.
// In-repo, platformSettings always comes from
// INITIAL_PLATFORM_SETTINGS (FinanceContext.tsx:236,103,105) and
// every write is a spread-merge, so the undefined cases below are
// defensive only — but they are now proven, not assumed.
export const resolveSubscriptionPlans = (settings: PlatformSettings | null | undefined): SubscriptionPlan[] => {
  const plans = (settings as { subscriptionPlans?: unknown } | null | undefined)?.subscriptionPlans;
  return Array.isArray(plans) ? (plans as SubscriptionPlan[]) : [];
};

export const resolvePlanFeatures = (plan: SubscriptionPlan | null | undefined): string[] => {
  const features = (plan as { features?: unknown } | null | undefined)?.features;
  return Array.isArray(features) ? (features as string[]) : [];
};

// AdminCampaignsPlansTab trial-quota guard (M-3): that tab's input
// initialiser read subscriptionPlanLimits.trial unguarded, one line
// above the ?? [] guard — if limits were ever missing it threw
// first. INITIAL_PLATFORM_SETTINGS sets trial: 25
// (FinanceContext.tsx:105).
export const resolveTrialQuota = (settings: PlatformSettings | null | undefined): number => {
  const trial = (settings as { subscriptionPlanLimits?: { trial?: unknown } } | null | undefined)?.subscriptionPlanLimits?.trial;
  return typeof trial === 'number' && Number.isFinite(trial) ? trial : 25;
};

// Live parity (SellerDashboardView-MERGED.js, component Sn, ~bytes 215300-219700):
// informal second-person copy verbatim. Live has no mock-mode purchase
// branch and no seller-session filter on owned stores: every dropshipper
// store counts, VIP anywhere lifts the cap to 10. Purchase is
// POST /stores/:slug/services (storeApi.purchaseService) then a reload.
export const SellerSubscriptionTab: React.FC<{ onManageStores: () => void }> = ({ onManageStores }) => {
  const { currentStore, stores, platformSettings, toPersianDigits, formatPrice } = useStore();
  const { success, error } = useToast();
  // G7 hardening (reachable part only): plans/features fall back to [].
  // NOTE — no null-store guard here on purpose: useStore() throws
  // 'A store must be selected…' (StoreContext.tsx:1344) before this
  // body runs, and StoreContextType types currentStore non-null
  // (storeContextTypes.ts:115), so a tab-level null check could never
  // fire. If the live repro stack (window.__sellershoLastError)
  // implicates the transient-null window, the targeted fix belongs in
  // useStore/StoreProvider, not in this tab.
  const activePlanId = currentStore.activePlanId || 'trial';
  const plans = resolveSubscriptionPlans(platformSettings);
  const activePlan = plans.find((plan) => plan.id === activePlanId);
  const ownedStores = stores.filter((store) => store.ownerType === 'dropshipper').map((store) => store.id === currentStore.id ? currentStore : store);
  const maxStores = ownedStores.some((store) => store.activePlanId === 'vip') ? 10 : 1;
  const handlePurchase = async (planId: string) => {
    try {
      const updated = await storeApi.purchaseService(currentStore.slug, planId, currentStore.id);
      success('اشتراک با موفقیت فعال شد.');
      window.location.reload();
      return updated;
    } catch (err: any) { error(err.message || 'خرید اشتراک انجام نشد.'); }
  };
  return (
    <div className="space-y-3.5 pb-6">
      <HelpExplainer text="اشتراک یعنی پولی که برای فعال‌ماندن فروشگاهت می‌دهی. این‌جا اشتراک فعلی‌ات را می‌بینی و اگر خواستی، آن را بهتر می‌کنی." />
      <div className="rounded-2xl bg-amber-500 p-3.5 text-white">
        <div className="flex items-center gap-2"><Crown className="h-5 w-5" strokeWidth={1.5} /><div>
          <h3 className="text-xs font-bold">اشتراک فعلی تو: {activePlan?.name || activePlanId}</h3>
          <p className="mt-1 text-[11px]">فروشگاه فعال: {currentStore.name}</p>
        </div></div>
      </div>
      <section className="space-y-3 rounded-2xl border border-emerald-200 bg-emerald-50 p-4">
        <h3 className="flex items-center gap-2 text-sm font-bold text-emerald-900"><Store className="h-5 w-5" strokeWidth={1.5} /> چند فروشگاه با یک حساب کاربری</h3>
        <p className="text-xs leading-6 text-emerald-900">با اشتراک ویژه (VIP) می‌توانی تا ۱۰ فروشگاه با اسم و آدرس جدا بسازی و همه را با همین حساب مدیریت کنی. اشتراک‌های عادی فقط برای یک فروشگاه‌اند.</p>
        <p className="text-xs font-bold">فروشگاه‌های شما: {toPersianDigits(ownedStores.length)} از {toPersianDigits(maxStores)}</p>
        <button type="button" onClick={onManageStores} className="flex items-center gap-2 rounded-xl bg-emerald-700 px-4 py-2.5 min-h-[44px] text-xs font-bold text-white"><Plus className="h-4 w-4" strokeWidth={1.5} /> مدیریت و ساخت فروشگاه</button>
      </section>
      <div className="space-y-3">
        {plans.map((plan) => (
          <section key={plan.id} className={`space-y-3 rounded-2xl border p-4 ${plan.id === activePlanId ? 'border-brand-deep bg-brand-soft' : 'border-slate-200 bg-white'}`}>
            <div className="flex items-center justify-between gap-2">
              <div><h3 className="text-sm font-bold">{plan.name}</h3><p className="mt-1 text-xs text-brand-deep">{formatPrice(plan.price)} تومان · {plan.durationLabel}</p></div>
              {plan.id === activePlanId ? <span className="text-xs font-bold text-brand-deep">اشتراک فعلی تو</span> : <button type="button" onClick={() => handlePurchase(plan.id)} className="rounded-xl bg-brand-deep px-3 py-2.5 min-h-[44px] text-xs font-bold text-white">خرید با پول کیف پول</button>}
            </div>
            <p className="rounded-xl bg-slate-100 p-2 text-xs font-bold">{plan.id === 'vip' ? 'ساخت و مدیریت تا ۱۰ فروشگاه مستقل با یک حساب' : 'ساخت و مدیریت ۱ فروشگاه با یک حساب'}</p>
            {resolvePlanFeatures(plan).map((feature) => <p key={feature} className="flex items-start gap-2 text-xs leading-5 text-slate-600"><Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-600" strokeWidth={1.5} />{feature}</p>)}
          </section>
        ))}
      </div>
    </div>
  );
};
