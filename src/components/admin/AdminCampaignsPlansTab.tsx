import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { isMockMode } from '../../services/apiClient';
import { SubscriptionPlan } from '../../types';
import { resolveSubscriptionPlans, resolveTrialQuota } from '../seller/SellerSubscriptionTab';
import { Crown, Plus, Edit2, Trash2, X, Sparkles, Check, Sliders } from 'lucide-react';

export const AdminCampaignsPlansTab: React.FC = () => {
  if (isMockMode()) return <SampleCampaignSettings />;
  return (
    <section aria-labelledby="campaign-settings-heading" className="space-y-2 rounded-2xl border border-slate-200 bg-white p-4">
      <h3 id="campaign-settings-heading" className="text-base font-bold text-slate-900">مدیریت کمپین‌ها و طرح‌ها</h3>
      <p className="text-sm leading-6 text-slate-700">تغییر طرح‌های اشتراک، سهمیه آزمایشی و بودجه تبلیغاتی در این پنل در دسترس نیست.</p>
    </section>
  );
};

function SampleCampaignSettings() {
  const { platformSettings, updatePlatformSettings, updateSubscriptionPlans, updateTrialCatalogQuota, formatPrice, toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [dealBudget, setDealBudget] = useState((platformSettings.centralCampaignBudget ?? 0).toString());
  const [savedBudget, setSavedBudget] = useState(false);
  const [editingPlan, setEditingPlan] = useState<SubscriptionPlan | null>(null);
  const [isNew, setIsNew] = useState(false);
  // M-3: plans through the shared resolver (non-array -> []), and the
  // trial-quota initialiser through resolveTrialQuota — the bare
  // subscriptionPlanLimits.trial read sat one line above the ?? []
  // guard, so it would have thrown first if limits were missing.
  const plans = resolveSubscriptionPlans(platformSettings);
  const [trialQuotaInput, setTrialQuotaInput] = useState(String(resolveTrialQuota(platformSettings)));
  const handleSaveTrialQuota = (event: React.FormEvent) => {
    event.preventDefault();
    const quota = Number(trialQuotaInput);
    if (!Number.isInteger(quota) || quota < 1 || quota > 1000) return error('سقف تریال باید عددی بین ۱ و ۱۰۰۰ باشد.');
    updateTrialCatalogQuota(quota);
    success('سقف تریال ذخیره شد.');
  };
  const handleOpenAdd = () => {
    setIsNew(true);
    setEditingPlan({ id: `plan-${Date.now()}`, name: '', durationDays: 30, durationLabel: 'یک ماه', price: 0, productLimit: 25, allowedCustomDomains: 0, maxStores: 1, features: [] });
  };
  const handleOpenEdit = (plan: SubscriptionPlan) => { setIsNew(false); setEditingPlan({ ...plan }); };
  const handleDelete = (id: string) => {
    if (!window.confirm('این پلن حذف شود؟')) return;
    updateSubscriptionPlans(plans.filter((plan) => plan.id !== id));
    success('پلن حذف شد.');
  };
  const handleSavePlan = (event: React.FormEvent) => {
    event.preventDefault();
    if (!editingPlan) return;
    if (!editingPlan.name.trim() || !Number.isFinite(editingPlan.price) || editingPlan.price < 0 || !Number.isInteger(editingPlan.durationDays) || editingPlan.durationDays < 1 || !Number.isInteger(editingPlan.productLimit) || editingPlan.productLimit < 1 || !Number.isInteger(editingPlan.allowedCustomDomains) || editingPlan.allowedCustomDomains < 0) return error('اطلاعات پلن معتبر نیست.');
    const plan = { ...editingPlan, name: editingPlan.name.trim() };
    updateSubscriptionPlans(isNew ? [...plans, plan] : plans.map((item) => item.id === plan.id ? plan : item));
    setEditingPlan(null);
    success('پلن ذخیره شد.');
  };

  return (
    <div className="space-y-3.5 pb-6 select-none">
      {/* Item 4: Dedicated Trial Catalog Quota Direct Configuration Card */}
      <form onSubmit={handleSaveTrialQuota} className="bg-gradient-to-r from-amber-50 to-orange-50/60 rounded-2xl p-3.5 border border-amber-200/90 shadow-2xs space-y-2.5">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-amber-800">
            <Sliders className="w-4 h-4 text-amber-600"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold">تنظیم سقف کاتالوگ پلن تریال رایگان (شروع یک‌ماهه)</h3>
          </div>
          <span className="text-[10px] bg-amber-200/60 text-amber-900 font-bold px-2 py-0.5 rounded-full">
            تنظیم بدون نیاز به کدنویسی
          </span>
        </div>
        <p className="text-[10px] text-slate-600 leading-relaxed">
          سقف انتخاب محصول دراپ‌شیپرهای تازه‌وارد در دوره تریال رایگان ۳۰ روزه را مستقیماً تعیین کنید. کارفرما می‌تواند این عدد را آزادانه تغییر داده و ذخیره نماید:
        </p>
        <div className="flex items-center gap-2 max-w-sm">
          <div className="relative flex-1">
            <input
              type="number"
              min="1"
              max="1000"
              value={trialQuotaInput}
              onChange={(e) => setTrialQuotaInput(e.target.value)}
              className="w-full text-xs p-2 bg-white border border-amber-300 rounded-xl font-bold font-mono text-center focus:outline-hidden"
              placeholder="مثال: ۲۵"
              required
            />
            <span className="absolute left-2.5 top-2 text-[10px] text-slate-400">قلم کالا</span>
          </div>
          <button
            type="submit"
            className="py-2 px-3.5 bg-amber-600 hover:bg-amber-700 text-white font-bold text-xs rounded-xl shadow-2xs transition-colors shrink-0 flex items-center gap-1"
          >
            <Check className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>ذخیره سقف تریال</span>
          </button>
        </div>
      </form>

      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-amber-600">
            <Crown className="w-4 h-4"  strokeWidth={1.5}/>
            <h3 className="text-xs font-bold text-slate-800">مدیریت پلن‌های عضویت دراپ‌شیپرها</h3>
          </div>
          <button
            type="button"
            onClick={handleOpenAdd}
            className="flex items-center gap-1 bg-amber-500 hover:bg-amber-600 text-white font-bold text-[10px] px-2.5 py-1.5 rounded-xl shadow-2xs"
          >
            <Plus className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>ایجاد پلن جدید</span>
          </button>
        </div>

        <p className="text-[10px] text-slate-500 leading-relaxed">
          پلن‌های اشتراک کاملاً قابل تعریف، ویرایش و حذف هستند (سقف کاتالوگ، تعداد دامنه‌های اختصاصی مجاز، قیمت و طول دوره).
        </p>

        <div className="space-y-2">
          {plans.map((p) => (
            <div key={p.id} className="p-3 bg-slate-50 rounded-xl border border-slate-200/80 flex items-center justify-between text-xs">
              <div>
                <span className="font-bold text-slate-900 block">{p.name}</span>
                <span className="text-[10px] text-slate-500 block mt-0.5">
                  مدت: {p.durationLabel} • سقف: {p.productLimit === 9999 ? 'نامحدود' : `${toPersianDigits(p.productLimit)} کالا`} • دامنه: {toPersianDigits(p.allowedCustomDomains)}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="font-extrabold text-blue-700 font-mono text-xs">
                  {p.price === 0 ? 'رایگان' : `${formatPrice(p.price)} ت`}
                </span>
                <button onClick={() => handleOpenEdit(p)} className="p-1 text-slate-400 hover:text-blue-600">
                  <Edit2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                </button>
                <button onClick={() => handleDelete(p.id)} className="p-1 text-slate-400 hover:text-rose-600">
                  <Trash2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Edit/Create Modal */}
      {editingPlan && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs">
          <form onSubmit={handleSavePlan} className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl space-y-3 text-xs">
            <div className="flex items-center justify-between border-b pb-2">
              <h4 className="font-bold">{isNew ? 'تعریف پلن جدید' : 'ویرایش پلن'}</h4>
              <button type="button" onClick={() => setEditingPlan(null)}><X className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/></button>
            </div>
            <div className="space-y-2">
              <div>
                <label className="text-[10px] font-bold block mb-1">نام پلن:</label>
                <input
                  type="text"
                  value={editingPlan.name}
                  onChange={(e) => setEditingPlan({ ...editingPlan, name: e.target.value })}
                  className="w-full p-2 bg-slate-50 border rounded-xl"
                  required
                />
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className="text-[10px] font-bold block mb-1">عنوان مدت:</label>
                  <input
                    type="text"
                    value={editingPlan.durationLabel}
                    onChange={(e) => setEditingPlan({ ...editingPlan, durationLabel: e.target.value })}
                    className="w-full p-2 bg-slate-50 border rounded-xl"
                    required
                  />
                </div>
                <div>
                  <label className="text-[10px] font-bold block mb-1">قیمت (تومان):</label>
                  <input
                    type="number"
                    value={editingPlan.price}
                    onChange={(e) => setEditingPlan({ ...editingPlan, price: Number(e.target.value) })}
                    className="w-full p-2 bg-slate-50 border rounded-xl font-mono"
                    required
                  />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className="text-[10px] font-bold block mb-1">سقف کالا:</label>
                  <input
                    type="number"
                    value={editingPlan.productLimit}
                    onChange={(e) => setEditingPlan({ ...editingPlan, productLimit: Number(e.target.value) })}
                    className="w-full p-2 bg-slate-50 border rounded-xl font-mono"
                    required
                  />
                </div>
                <div>
                  <label className="text-[10px] font-bold block mb-1">سقف دامنه:</label>
                  <input
                    type="number"
                    value={editingPlan.allowedCustomDomains}
                    onChange={(e) => setEditingPlan({ ...editingPlan, allowedCustomDomains: Number(e.target.value) })}
                    className="w-full p-2 bg-slate-50 border rounded-xl font-mono"
                    required
                  />
                </div>
                <div>
                  <label className="text-[10px] font-bold block mb-1">تعداد فروشگاه:</label>
                  <input
                    type="number"
                    min="1"
                    value={editingPlan.maxStores}
                    onChange={(e) => setEditingPlan({ ...editingPlan, maxStores: Number(e.target.value) })}
                    className="w-full p-2 bg-slate-50 border rounded-xl font-mono"
                    required
                  />
                </div>
              </div>
            </div>
            <div className="flex gap-2 pt-2 border-t">
              <button type="submit" className="flex-1 py-2 bg-blue-600 text-white font-bold rounded-xl">ذخیره پلن</button>
              <button type="button" onClick={() => setEditingPlan(null)} className="px-3 py-2 bg-slate-100 rounded-xl">انصراف</button>
            </div>
          </form>
        </div>
      )}

      {/* 2. Central Campaign Deals Budget (Section 15) */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-2">
          <div className="w-7 h-7 rounded-lg bg-blue-50 flex items-center justify-center text-blue-600">
            <Sparkles className="w-4 h-4"  strokeWidth={1.5}/>
          </div>
          <div>
            <h3 className="text-xs font-bold text-slate-900">بودجه تخفیف‌های پیشنهاد شگفت‌انگیز مرکزی</h3>
            <p className="text-[10px] text-slate-400">تأمین هزینه تخفیف بدون کسر از سود دراپ‌شیپر</p>
          </div>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          تخفیف‌های پیشنهاد شگفت‌انگیز مرکزی از محل این بودجه تأمین شده و بدون کاهش سود دراپ‌شیپر، فروش شبکه را تقویت می‌کند.
        </p>

        <div>
          <label className="text-[10px] text-slate-600 block mb-1">سقف بودجه تخفیف این دوره (تومان)</label>
          <input
            type="number"
            value={dealBudget}
            onChange={(e) => setDealBudget(e.target.value)}
            className="w-full text-xs p-2 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden"
          />
        </div>

        <button
          onClick={() => {
            updatePlatformSettings({ centralCampaignBudget: Number(dealBudget) });
            setSavedBudget(true);
            setTimeout(() => setSavedBudget(false), 2000);
          }}
          className="w-full py-2 bg-blue-600 hover:bg-blue-700 text-white font-bold text-xs rounded-xl shadow-xs transition-colors"
        >
          {savedBudget ? 'بودجه کمپین ثبت و قفل شد' : 'ثبت سقف بودجه کمپین شگفت‌انگیز'}
        </button>
      </div>
    </div>
  );
}
