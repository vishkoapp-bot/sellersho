import React, { useState } from 'react';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { validateStoreSlug } from '../../utils/validators';
import { storeApi } from '../../services/storeApi';
import { SellerDomainsPanel } from './SellerDomainsPanel';
import { isMockMode } from '../../services/apiClient';
import { Globe, Check, Save, Plus, Clock, CheckCircle2, XCircle, Copy, Loader2, Server, ChevronDown, Store } from 'lucide-react';

// Name and slug changes require approval. Storefront descriptions save directly.
// The live domain panel uses the DNS API, and the mock flow simulates review locally.
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
    <div className="space-y-4">
      <SupplierSectionHeading title="مشخصات فروشگاه" description="نام، آدرس و توضیحی را تنظیم کن که مشتری در فروشگاه می‌بیند." icon={<Store />} />
      <form onSubmit={handleSave} className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <h3 className="text-base font-extrabold text-[#261b41]">اطلاعات ویترین</h3>

        <div className="grid gap-4 xl:grid-cols-2">
          <div>
            <div className="mb-1 flex flex-wrap items-center justify-between gap-1">
              <label htmlFor="seller-store-name" className="text-sm font-bold text-slate-700">نام فروشگاهت</label>
              {pendingNameRequest && (
                <span className="text-[10px] font-bold bg-amber-50 text-amber-700 border border-amber-200 px-2 py-0.5 rounded-full flex items-center gap-1">
                  <Clock className="w-3 h-3 text-amber-600" strokeWidth={1.5} />
                  <span>در انتظار تأیید مدیر: «{pendingNameRequest.requestedName}»</span>
                </span>
              )}
            </div>
            <input
              id="seller-store-name"
              aria-describedby="seller-store-name-hint"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
              required
            />
            <span id="seller-store-name-hint" className="mt-1 block text-xs leading-5 text-slate-600">
              نام جدید پس از تأیید مدیر در فروشگاه نمایش داده می‌شود.
            </span>
          </div>

          <div>
            <div className="mb-1 flex flex-wrap items-center justify-between gap-1">
              <label htmlFor="seller-store-slug" className="text-sm font-bold text-slate-700">آدرس فروشگاهت (فقط انگلیسی)</label>
              {pendingSlugRequest && (
                <span className="text-[10px] font-bold bg-amber-50 text-amber-700 border border-amber-200 px-2 py-0.5 rounded-full flex items-center gap-1">
                  <Clock className="w-3 h-3 text-amber-600" strokeWidth={1.5} />
                  <span>در انتظار تأیید مدیر: «/{pendingSlugRequest.requestedSlug}»</span>
                </span>
              )}
            </div>
            <div className="flex items-center overflow-hidden rounded-xl border border-slate-200 bg-slate-50 ps-2.5 focus-within:border-brand" dir="ltr">
              <span className="font-mono text-xs text-slate-600">sellershow.ir/store/</span>
              <input
                id="seller-store-slug"
                aria-describedby="seller-store-slug-hint"
                type="text"
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                className="min-w-0 flex-1 bg-transparent py-3 pe-3 font-mono text-sm focus:outline-hidden"
                dir="ltr"
                required
              />
            </div>
            <span id="seller-store-slug-hint" className="mt-1 block text-xs leading-5 text-slate-600">
              تغییر آدرس به تأیید مدیر نیاز دارد. بعد از تغییر، لینک قبلی از کار می‌افتد.
            </span>
          </div>

          <div>
            <label htmlFor="seller-store-tagline" className="mb-1 block text-sm font-bold text-slate-700">شعار فروشگاهت</label>
            <input
              id="seller-store-tagline"
              aria-describedby="seller-store-tagline-hint"
              type="text"
              value={tagline}
              onChange={(e) => setTagline(e.target.value)}
              placeholder="مثال: مرجع تخصصی لوازم جانبی و دیجیتال"
              className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
            />
            <span id="seller-store-tagline-hint" className="mt-1 block text-xs leading-5 text-slate-600">
              یک جمله کوتاه که زیر اسم فروشگاهت به مشتری نشان داده می‌شود.
            </span>
          </div>

          <div>
            <label htmlFor="seller-store-description" className="mb-1 block text-sm font-bold text-slate-700">
              درباره فروشگاهت (کوتاه)
            </label>
            <textarea
              id="seller-store-description"
              rows={2}
              value={shortDescription}
              onChange={(e) => setShortDescription(e.target.value)}
              placeholder="یک یا دو جمله بنویس: چه چیزی می‌فروشی و چرا از تو بخرند…"
              className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden leading-relaxed"
            />
          </div>
        </div>

        <details className="group rounded-xl border border-slate-200 px-3">
          <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-2 rounded-lg text-sm font-bold text-slate-700 focus-visible:ring-2 focus-visible:ring-brand">
            عنوان بخش «محصولات مکمل»
            <ChevronDown aria-hidden="true" className="size-4 text-brand-deep group-open:rotate-180" />
          </summary>
          <div className="max-w-xl space-y-1 pb-3">
            <label htmlFor="seller-store-addon-title" className="sr-only">عنوان بخش «محصولات مکمل»</label>
            <input
              id="seller-store-addon-title"
              aria-describedby="seller-store-addon-title-hint"
              type="text"
              value={addonSectionTitle}
              maxLength={200}
              onChange={(e) => setAddonSectionTitle(e.target.value)}
              placeholder="محصولات مکمل"
              className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-brand outline-hidden"
            />
            <span id="seller-store-addon-title-hint" className="block text-xs leading-5 text-slate-600">
              عنوانی که بالای مکمل‌ها در صفحه کالا نمایش داده می‌شود؛ خالی بگذاری همان «محصولات مکمل» نشان داده می‌شود.
            </span>
          </div>
        </details>

        <button type="submit" className={`flex min-h-11 w-full items-center justify-center gap-2 rounded-xl px-4 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand sm:w-auto ${saved ? 'bg-emerald-700' : 'bg-brand-deep hover:bg-brand-deep/90'}`}>
          {saved ? <Check aria-hidden="true" className="size-4" /> : <Save aria-hidden="true" className="size-4" />}
          {saved ? 'ذخیره شد' : 'ذخیره تغییرات'}
        </button>
      </form>

      {!isMockMode() && <SellerDomainsPanel key={currentStore.id} slug={currentStore.slug} storeId={currentStore.id} />}

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

      {showDomainModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <form onSubmit={handleDomainConnect} role="dialog" aria-modal="true" aria-labelledby="seller-domains-modal-title" className="bg-white rounded-2xl max-w-sm w-full p-4 shadow-2xl border border-slate-100 space-y-3">
            <div className="flex items-center gap-2 text-brand-deep">
              <Server className="w-4 h-4" strokeWidth={1.5} />
              <h4 id="seller-domains-modal-title" className="text-xs font-bold text-slate-900">وصل کردن دامنه شخصی</h4>
            </div>
            <p className="text-[10px] text-slate-500 leading-relaxed">
              ابتدا نیم‌سرورهای <strong className="font-mono text-brand-deep">ns1.drup.ir</strong> و <strong className="font-mono text-brand-deep">ns2.drup.ir</strong> را روی دامنه خود تنظیم کنید؛ سپس نام دامنه را وارد نمایید تا درخواست اتصال برای بررسی و تأیید به مدیر پلتفرم ارسال شود.
            </p>
            <input
              type="text"
              aria-label="نام دامنه"
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
                type="submit"
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
          </form>
        </div>
      )}
    </div>
  );
};
