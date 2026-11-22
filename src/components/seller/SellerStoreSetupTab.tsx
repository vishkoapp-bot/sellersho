import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { validateStoreSlug } from '../../utils/validators';
import { storeApi } from '../../services/storeApi';
import { SellerDomainsPanel } from './SellerDomainsPanel';
import { isMockMode } from '../../services/apiClient';
import { Globe, Check, Save, Plus, Clock, CheckCircle2, XCircle, Copy, Loader2, Server, HelpCircle, ChevronDown } from 'lucide-react';

// Live parity (SellerDashboardView-MERGED.js, component Ml, ~bytes 33800-48000):
// informal second-person copy verbatim; tagline/shortDescription save at once
// while name/slug changes go to admin approval; custom domains are a
// collapsed "advanced" section whose requests go to platform-admin review
// (POST /stores/:slug/change-requests kind=custom_domain, then a local
// pending request) — live has NO automatic DNS verification flow.
// The "contract" card live renders inside this tab is covered in the
// candidate by the separate «قرارداد من» tab, so it is not duplicated here.
// Live's Store carries addonSectionTitle; the candidate Store type does not
// yet, so the field is read/written through a narrow cast (types owned
// elsewhere) while the server save carries it for real.
const Explainer: React.FC<{ text: string }> = ({ text }) => {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-label="توضیح ساده"
        className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-brand-soft border border-brand-border text-brand-deep mr-1"
      >
        <HelpCircle className="w-3.5 h-3.5" strokeWidth={1.5} />
      </button>
      {open && (
        <span className="absolute z-20 top-7 right-0 w-60 rounded-xl bg-white border border-slate-200 shadow-lg p-2.5 text-right">
          <span className="block text-[11px] leading-5 text-slate-600 font-normal">{text}</span>
        </span>
      )}
    </div>
  );
};

// G2 (Batch 4b): map a save failure to Persian copy. A 400 validation
// failure (e.g. an over-long addon-section title) must never surface
// the backend's raw English text; anything else keeps the generic
// retry copy.
export function storeSettingsSaveErrorMessage(err: unknown): string {
  const status = (err as { status?: unknown } | null | undefined)?.status;
  const message = (err as { message?: unknown } | null | undefined)?.message;
  if (status === 400 || message === 'Invalid input') {
    return 'ذخیره انجام نشد؛ مقدار نامعتبر است.';
  }
  return 'ذخیره انجام نشد. دوباره تلاش کن.';
}

export const SellerStoreSetupTab: React.FC = () => {
  const {
    currentStore,
    setCurrentStore,
    storeNameChangeRequests,
    requestStoreNameChange,
    storeSlugChangeRequests,
    requestStoreSlugChange,
    customDomainRequests,
    requestCustomDomain,
    platformSettings,
    toPersianDigits,
  } = useStore();
  const { success, error, info } = useToast();
  const [name, setName] = useState(currentStore.name);
  const [slug, setSlug] = useState(currentStore.slug);
  const [tagline, setTagline] = useState(currentStore.tagline || '');
  const [shortDescription, setShortDescription] = useState(currentStore.shortDescription || '');
  const [addonSectionTitle, setAddonSectionTitle] = useState(
    (currentStore as { addonSectionTitle?: string }).addonSectionTitle || ''
  );
  const [newDomain, setNewDomain] = useState('');
  const [showDomainModal, setShowDomainModal] = useState(false);
  const [isSubmittingDomain, setIsSubmittingDomain] = useState(false);
  const [saved, setSaved] = useState(false);
  const [copiedNs, setCopiedNs] = useState<'ns1' | 'ns2' | null>(null);
  const [domainOpen, setDomainOpen] = useState(false);

  const activePlan = platformSettings.subscriptionPlans?.find((p) => p.id === (currentStore.activePlanId || 'trial')) || platformSettings.subscriptionPlans?.[0] || {
    allowedCustomDomains: 1,
    maxStores: 1,
    name: 'شروع رایگان یک‌ماهه',
  };
  const allowedCustomDomains = activePlan.allowedCustomDomains || 1;

  const pendingNameRequest = storeNameChangeRequests.find(
    (r) => r.storeId === currentStore.id && r.status === 'pending'
  );

  const pendingSlugRequest = storeSlugChangeRequests?.find(
    (r) => r.storeId === currentStore.id && r.status === 'pending'
  );

  const storeDomains = customDomainRequests.filter((r) => r.storeId === currentStore.id);

  const handleCopyNs = (val: string, type: 'ns1' | 'ns2') => {
    navigator.clipboard.writeText(val);
    setCopiedNs(type);
    success(`نیم‌سرور «${val}» در حافظه کپی شد.`);
    setTimeout(() => setCopiedNs(null), 2000);
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    const slugCheck = validateStoreSlug(slug);
    if (!slugCheck.valid) {
      error(slugCheck.error || 'نامک فروشگاه نامعتبر است.');
      return;
    }

    const cleanedSlug = slug.trim().toLowerCase().replace(/\s+/g, '-');
    if (name.trim() !== currentStore.name) {
      try {
        if (!isMockMode() && currentStore.slug) {
          await storeApi.requestStoreChange(currentStore.slug, { kind: 'store_name', value: name.trim() });
        }
        requestStoreNameChange(currentStore.id, name.trim());
        info(`اسم جدید «${name.trim()}» برای تأیید مدیر فرستاده شد؛ تا تأیید شود، اسم فعلی می‌ماند.`);
      } catch (err: any) {
        error(err?.message || 'فرستادن اسم جدید انجام نشد. دوباره تلاش کن.');
        return;
      }
    }

    if (cleanedSlug !== currentStore.slug) {
      try {
        if (!isMockMode() && currentStore.slug) {
          await storeApi.requestStoreChange(currentStore.slug, { kind: 'store_slug', value: cleanedSlug });
        }
        requestStoreSlugChange(currentStore.id, cleanedSlug);
        info(`آدرس جدید «/${cleanedSlug}» برای تأیید مدیر فرستاده شد؛ تا تأیید شود، آدرس فعلی می‌ماند.`);
      } catch (err: any) {
        error(err?.message || 'فرستادن آدرس جدید انجام نشد. دوباره تلاش کن.');
        return;
      }
    }

    setCurrentStore({
      ...currentStore,
      tagline,
      shortDescription,
      addonSectionTitle: addonSectionTitle.trim(),
    } as typeof currentStore);
    if (!isMockMode() && currentStore.slug) {
      try {
        await storeApi.saveStoreSettings(currentStore.slug, { tagline, shortDescription, addonSectionTitle: addonSectionTitle.trim() });
      } catch (err: any) {
        error(storeSettingsSaveErrorMessage(err));
        return;
      }
    }
    setSaved(true);
    success('ذخیره شد ✓');
    setTimeout(() => setSaved(false), 2500);
  };

  const handleDomainConnect = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newDomain || !newDomain.includes('.')) {
      error('لطفاً آدرس دامنه معتبر (مانند myshop.ir) وارد کنید.');
      return;
    }

    if (storeDomains.length >= allowedCustomDomains) {
      error(`سقف دامنه پلن شما (${toPersianDigits(allowedCustomDomains)} دامنه) تکمیل است. لطفاً پلن را ارتقا دهید.`);
      return;
    }

    const domain = newDomain.trim().toLowerCase();
    setIsSubmittingDomain(true);
    try {
      if (!isMockMode() && currentStore.slug) {
        await storeApi.requestStoreChange(currentStore.slug, { kind: 'custom_domain', value: domain });
      }
      requestCustomDomain(currentStore.id, domain);
      success(`درخواست اتصال دامنه «${domain}» ثبت شد و در انتظار بررسی مدیر پلتفرم است.`);
      setNewDomain('');
      setShowDomainModal(false);
    } catch (err: any) {
      error(err?.message || 'ثبت درخواست دامنه انجام نشد.');
    } finally {
      setIsSubmittingDomain(false);
    }
  };

  return (
    <form onSubmit={handleSave} className="space-y-3.5 pb-24">
      {!isMockMode() && <SellerDomainsPanel slug={currentStore.slug} storeId={currentStore.id} />}
      <Explainer text="این‌جا اسم، آدرس و توضیح فروشگاهت را عوض می‌کنی. شعار و توضیح همان لحظه ذخیره می‌شوند؛ عوض‌کردن اسم و آدرس اول باید به تأیید مدیر پلتفرم برسد." />
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <h3 className="text-xs font-bold text-slate-800">اسم و آدرس فروشگاهت</h3>

        <div>
          <div className="flex items-center justify-between mb-1">
            <label className="text-[13px] font-bold text-slate-700">نام فروشگاهت</label>
            {pendingNameRequest && (
              <span className="text-[10px] font-bold bg-amber-50 text-amber-700 border border-amber-200 px-2 py-0.5 rounded-full flex items-center gap-1">
                <Clock className="w-3 h-3 text-amber-600" strokeWidth={1.5} />
                <span>در انتظار تأیید مدیر: «{pendingNameRequest.requestedName}»</span>
              </span>
            )}
          </div>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
            required
          />
          <span className="text-[11px] text-slate-400 mt-1 block">
            اگر اسم را عوض کنی، اول مدیر پلتفرم تأیید می‌کند و بعد اسم جدید روی فروشگاهت می‌نشیند.
          </span>
        </div>

        <div>
          <div className="flex items-center justify-between mb-1">
            <label className="text-[13px] font-bold text-slate-700">آدرس فروشگاهت (فقط انگلیسی)</label>
            {pendingSlugRequest && (
              <span className="text-[10px] font-bold bg-amber-50 text-amber-700 border border-amber-200 px-2 py-0.5 rounded-full flex items-center gap-1">
                <Clock className="w-3 h-3 text-amber-600" strokeWidth={1.5} />
                <span>در انتظار تأیید مدیر: «/{pendingSlugRequest.requestedSlug}»</span>
              </span>
            )}
          </div>
          <div className="flex items-center rounded-xl bg-slate-50 border border-slate-200 overflow-hidden px-2.5">
            <span className="text-[10px] text-slate-400 font-mono" dir="ltr">sellershow.ir/store/</span>
            <input
              type="text"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              className="w-full text-[13px] p-3 bg-transparent font-mono text-left focus:outline-hidden"
              dir="ltr"
              required
            />
          </div>
          <span className="text-[11px] text-slate-400 mt-1 block">
            مشتری‌ها با این آدرس وارد فروشگاهت می‌شوند. عوض‌کردنش نیاز به تأیید مدیر دارد، چون لینک‌های قبلی‌ات از کار می‌افتند.
          </span>
        </div>

        <div>
          <label className="text-[13px] font-bold text-slate-700 block mb-1">شعار فروشگاهت</label>
          <input
            type="text"
            value={tagline}
            onChange={(e) => setTagline(e.target.value)}
            placeholder="مثال: مرجع تخصصی لوازم جانبی و دیجیتال"
            className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
          />
          <span className="text-[11px] text-slate-400 mt-1 block">
            یک جمله کوتاه که زیر اسم فروشگاهت به مشتری نشان داده می‌شود.
          </span>
        </div>

        <div>
          <label className="text-[13px] font-bold text-slate-700 block mb-1">
            درباره فروشگاهت (کوتاه)
          </label>
          <textarea
            rows={2}
            value={shortDescription}
            onChange={(e) => setShortDescription(e.target.value)}
            placeholder="یک یا دو جمله بنویس: چه چیزی می‌فروشی و چرا از تو بخرند…"
            className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden leading-relaxed"
          />
        </div>

        <div>
          <label className="text-[13px] font-bold text-slate-700 block mb-1">عنوان بخش «محصولات مکمل»</label>
          <input
            type="text"
            value={addonSectionTitle}
            maxLength={200}
            onChange={(e) => setAddonSectionTitle(e.target.value)}
            placeholder="محصولات مکمل"
            className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
          />
          <span className="text-[11px] text-slate-400 mt-1 block">
            عنوانی که بالای مکمل‌ها در صفحه کالا نمایش داده می‌شود؛ خالی بگذاری همان «محصولات مکمل» نشان داده می‌شود.
          </span>
        </div>
      </div>

      <div hidden={!isMockMode()} className="bg-white rounded-2xl border border-slate-200/90 shadow-2xs overflow-hidden">
        <button
          type="button"
          onClick={() => setDomainOpen((v) => !v)}
          aria-expanded={domainOpen}
          className="w-full p-3.5 flex items-center justify-between gap-2 text-right min-h-[52px]"
        >
          <span className="flex items-center gap-1.5">
            <Globe className="w-4 h-4 text-brand-deep" strokeWidth={1.5} />
            <span>
              <span className="text-xs font-bold text-slate-800 block">دامنه شخصی (پیشرفته)</span>
              <span className="text-[11px] text-slate-400 block mt-0.5">اگر دامنه‌ای به اسم خودت داری، این‌جا به فروشگاهت وصلش کن؛ نداری، این بخش لازم نیست.</span>
            </span>
          </span>
          <ChevronDown className={`w-4 h-4 text-slate-400 shrink-0 transition-transform ${domainOpen ? 'rotate-180' : ''}`} strokeWidth={1.5} />
        </button>
        {domainOpen && (
          <div className="px-3.5 pb-3.5 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold text-slate-800">دامنه‌های من</span>
              <button
                type="button"
                onClick={() => setShowDomainModal(true)}
                className="flex items-center gap-1 bg-brand-soft hover:bg-brand-border text-brand-deep font-bold text-[11px] px-3 py-2.5 min-h-[44px] rounded-xl transition-colors"
              >
                <Plus className="w-3.5 h-3.5" strokeWidth={1.5} />
                <span>وصل کردن دامنه جدید</span>
              </button>
            </div>

            <p className="text-[11px] text-slate-500 leading-relaxed">
              برای وصل‌کردن دامنه خودت، اول دو «نیم‌سرور» (NS) زیر را در سایتی که دامنه را از آن خریده‌ای (مثلاً ایرنیک) وارد کن و بعد این‌جا درخواست بده؛ مدیر پلتفرم بررسی می‌کند و دامنه‌ات فعال می‌شود.
            </p>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 p-2.5 bg-brand-soft/50 rounded-xl border border-brand-border">
              <div className="flex items-center justify-between p-2 bg-white rounded-lg border border-slate-200/70 text-xs">
                <div>
                  <span className="text-[9px] text-slate-400 block">نیم‌سرور اول:</span>
                  <span className="font-mono font-bold text-brand-deep" dir="ltr">ns1.drup.ir</span>
                </div>
                <button
                  type="button"
                  onClick={() => handleCopyNs('ns1.drup.ir', 'ns1')}
                  className="p-1 text-slate-400 hover:text-brand-deep rounded-md"
                  title="کپی نیم‌سرور اول"
                >
                  {copiedNs === 'ns1' ? <Check className="w-3.5 h-3.5 text-emerald-600" strokeWidth={1.5} /> : <Copy className="w-3.5 h-3.5" strokeWidth={1.5} />}
                </button>
              </div>

              <div className="flex items-center justify-between p-2 bg-white rounded-lg border border-slate-200/70 text-xs">
                <div>
                  <span className="text-[9px] text-slate-400 block">نیم‌سرور دوم:</span>
                  <span className="font-mono font-bold text-brand-deep" dir="ltr">ns2.drup.ir</span>
                </div>
                <button
                  type="button"
                  onClick={() => handleCopyNs('ns2.drup.ir', 'ns2')}
                  className="p-1 text-slate-400 hover:text-brand-deep rounded-md"
                  title="کپی نیم‌سرور دوم"
                >
                  {copiedNs === 'ns2' ? <Check className="w-3.5 h-3.5 text-emerald-600" strokeWidth={1.5} /> : <Copy className="w-3.5 h-3.5" strokeWidth={1.5} />}
                </button>
              </div>
            </div>

            <div className="flex items-center justify-between text-[10px] text-slate-500 bg-slate-50 p-2 rounded-xl border border-slate-100">
              <span>سقف دامنه اختصاصی طبق پلن ({activePlan.name}):</span>
              <span className="font-bold text-slate-800">
                {toPersianDigits(storeDomains.length)} از {toPersianDigits(allowedCustomDomains)} دامنه
              </span>
            </div>

            <div className="space-y-1.5">
              {storeDomains.length === 0 ? (
                <div className="p-3 bg-slate-50 rounded-xl text-center text-slate-400 text-xs border border-slate-100">
                  هنوز دامنه‌ای وصل نکرده‌ای.
                </div>
              ) : (
                storeDomains.map((dom) => (
                  <div
                    key={dom.id}
                    className="p-2.5 bg-slate-50 rounded-xl border border-slate-200/80 flex items-center justify-between text-xs"
                  >
                    <div>
                      <span className="font-mono font-bold text-slate-800" dir="ltr">{dom.requestedDomain}</span>
                      <span className="text-[10px] text-slate-400 block mt-0.5">
                        نیم‌سرور: ns1.drup.ir / ns2.drup.ir • ثبت: {dom.requestDate}
                      </span>
                    </div>
                    {dom.status === 'approved' ? (
                      <span className="text-[9px] font-bold bg-emerald-50 text-emerald-700 px-2 py-0.5 rounded-full flex items-center gap-1 border border-emerald-200">
                        <CheckCircle2 className="w-3 h-3 text-emerald-600" strokeWidth={1.5} />
                        <span>تأیید شده (فعال)</span>
                      </span>
                    ) : dom.status === 'rejected' ? (
                      <span className="text-[9px] font-bold bg-rose-50 text-rose-700 px-2 py-0.5 rounded-full flex items-center gap-1 border border-rose-200">
                        <XCircle className="w-3 h-3 text-rose-600" strokeWidth={1.5} />
                        <span>رد شده توسط مدیر</span>
                      </span>
                    ) : (
                      <span className="text-[9px] font-bold bg-amber-50 text-amber-700 px-2 py-0.5 rounded-full flex items-center gap-1 border border-amber-200">
                        <Clock className="w-3 h-3 text-amber-600" strokeWidth={1.5} />
                        <span>در انتظار بررسی مدیر پلتفرم</span>
                      </span>
                    )}
                  </div>
                ))
              )}
            </div>
          </div>
        )}
      </div>

      <div className="fixed bottom-0 inset-x-0 z-30 px-3.5 pb-3 pointer-events-none">
        <div className="max-w-[504px] mx-auto pointer-events-auto">
          <button
            type="submit"
            className={`w-full min-h-[52px] py-3 font-extrabold text-sm rounded-2xl shadow-lg flex items-center justify-center gap-1.5 transition-all ${saved ? 'bg-emerald-600 text-white' : 'bg-brand-deep text-white'}`}
          >
            {saved ? (
              <>
                <Check className="w-5 h-5" strokeWidth={1.5} />
                <span>ذخیره شد ✓</span>
              </>
            ) : (
              <>
                <Save className="w-4 h-4" strokeWidth={1.5} />
                <span>ذخیره تغییرات</span>
              </>
            )}
          </button>
        </div>
      </div>

      {showDomainModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl border border-slate-100 space-y-3">
            <div className="flex items-center gap-2 text-brand-deep">
              <Server className="w-4 h-4" strokeWidth={1.5} />
              <h4 className="text-xs font-bold text-slate-900">وصل کردن دامنه شخصی</h4>
            </div>
            <p className="text-[10px] text-slate-500 leading-relaxed">
              ابتدا نیم‌سرورهای <strong className="font-mono text-brand-deep">ns1.drup.ir</strong> و <strong className="font-mono text-brand-deep">ns2.drup.ir</strong> را روی دامنه خود تنظیم کنید؛ سپس نام دامنه را وارد نمایید تا درخواست اتصال برای بررسی و تأیید به مدیر پلتفرم ارسال شود.
            </p>
            <input
              type="text"
              placeholder="مثال: my-store.ir"
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
              disabled={isSubmittingDomain}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-mono focus:outline-hidden"
              dir="ltr"
              autoFocus
            />

            {storeDomains.length >= allowedCustomDomains && (
              <div className="p-2 bg-rose-50 text-rose-700 rounded-xl text-[10px]">
                سقف دامنه‌های مجاز پلن فعلی شما ({toPersianDigits(allowedCustomDomains)} دامنه) تکمیل شده است. برای افزودن دامنه بیشتر، پلن خود را ارتقا دهید.
              </div>
            )}

            <div className="flex gap-2 pt-1">
              <button
                type="button"
                onClick={handleDomainConnect}
                disabled={isSubmittingDomain || storeDomains.length >= allowedCustomDomains}
                className="flex-1 py-2 bg-brand-deep hover:bg-brand-deep text-white font-bold text-xs rounded-xl disabled:opacity-50 flex items-center justify-center gap-1.5"
              >
                {isSubmittingDomain ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 animate-spin" strokeWidth={1.5} />
                    <span>در حال ارسال درخواست…</span>
                  </>
                ) : (
                  <span>ارسال درخواست برای بررسی</span>
                )}
              </button>
              <button
                type="button"
                onClick={() => setShowDomainModal(false)}
                disabled={isSubmittingDomain}
                className="px-3 py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl"
              >
                انصراف
              </button>
            </div>
          </div>
        </div>
      )}
    </form>
  );
};
