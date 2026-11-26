import { HelpExplainer } from './common/HelpExplainer';
import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../context/StoreContext';
import { useToast } from '../context/ToastContext';
import { Store as StoreType } from '../types';
import { storeApi } from '../services/storeApi';
import { ApiError, isMockMode } from '../services/apiClient';
import { validateStoreSlug, isValidIranianMobile } from '../utils/validators';
import { SellerStep1 } from './SellerStep1';
import { SellerStep2 } from './SellerStep2';
import { SellerStep3 } from './SellerStep3';
import { X, Store, ArrowLeft, ArrowRight, ShieldCheck } from 'lucide-react';
import { motion } from 'motion/react';
import { useAuth } from '../context/AuthContext';
import { emptySellerOnboardingForm, makeSellerSignatureCode, useSellerOnboardingDraft } from '../context/SellerOnboardingContext';
import { AuthModal } from './auth/AuthModal';

export const SellerOnboardingModal: React.FC = () => {
  const { isSellerModalOpen, setIsSellerModalOpen, setCurrentStore, addStore, platformSettings, toPersianDigits } = useStore();
  const { refreshToken, isAuthenticated } = useAuth();
  const { error: toastError, success: toastSuccess } = useToast();
  const draft = useSellerOnboardingDraft();
  const [localStep, setLocalStep] = useState<1 | 2 | 3 | 4>(1);
  const [localFormData, setLocalFormData] = useState(emptySellerOnboardingForm);
  const [localContractAccepted, setLocalContractAccepted] = useState(false);
  const step = draft?.step ?? localStep;
  const setStep = draft?.setStep ?? setLocalStep;
  const formData = draft?.formData ?? localFormData;
  const setFormData = draft?.setFormData ?? setLocalFormData;
  const contractAccepted = draft?.contractAccepted ?? localContractAccepted;
  const setContractAccepted = draft?.setContractAccepted ?? setLocalContractAccepted;

  // Extract referral code from URL query (?ref=DRUP-XXXX) if present
  useEffect(() => {
    if (typeof window !== 'undefined') {
      const urlParams = new URLSearchParams(window.location.search);
      const refParam = urlParams.get('ref');
      if (refParam && refParam.trim()) {
        setFormData((prev) => ({ ...prev, referralCode: refParam.trim().toUpperCase() }));
      }
    }
  }, [setFormData]);
  const [createdStore, setCreatedStore] = useState<StoreType | null>(null);

  const [localSignatureCode, setLocalSignatureCode] = useState('');
  const signatureCode = draft?.signatureCode ?? localSignatureCode;
  useEffect(() => {
    if (isSellerModalOpen && !draft) setLocalSignatureCode(makeSellerSignatureCode());
  }, [isSellerModalOpen, draft]);

  const authSucceeded = useRef(false);
  const [isAuthModalOpen, setIsAuthModalOpen] = useState(false);
  useEffect(() => {
    if (isSellerModalOpen && !isMockMode() && !isAuthenticated) setIsAuthModalOpen(true);
  }, [isSellerModalOpen, isAuthenticated]);

  if (!isSellerModalOpen) return null;
  if (!isMockMode() && (!isAuthenticated || isAuthModalOpen)) {
    return (
      <AuthModal
        isOpen
        central
        roleTitle="برای افتتاح فروشگاه ابتدا وارد حساب کاربری شوید"
        disableAutoNavigate
        onClose={() => {
          setIsAuthModalOpen(false);
          if (!authSucceeded.current) setIsSellerModalOpen(false);
          authSucceeded.current = false;
        }}
        onSuccess={() => {
          authSucceeded.current = true;
          setIsAuthModalOpen(false);
        }}
      />
    );
  }

  const handleNext = async () => {
    if (step === 1) {
      if (!formData.storeName.trim()) {
        toastError('نام فروشگاه الزامی است.');
        return;
      }
      const slugVal = validateStoreSlug(formData.slug);
      if (!slugVal.valid) {
        toastError(slugVal.error || 'نامک فروشگاه نامعتبر است.');
        return;
      }
      if (!formData.ownerName.trim()) {
        toastError('نام و نام خانوادگی مدیر الزامی است.');
        return;
      }
      if (formData.phone && !isValidIranianMobile(formData.phone)) {
        toastError('شماره موبایل وارد شده نامعتبر است.');
        return;
      }
      setStep(2);
    } else if (step === 2) {
      setStep(3);
    } else if (step === 3) {
      if (!contractAccepted) {
        toastError('مطالعه و پذیرش شرایط قرارداد جهت افتتاح فروشگاه الزامی است.');
        return;
      }
      const newStore: StoreType = {
        id: `store-${Date.now()}`,
        name: formData.storeName,
        slug: formData.slug.toLowerCase().trim(),
        ownerType: 'dropshipper',
        ownerName: formData.ownerName,
        tagline: `ویترین اختصاصی ${formData.storeName} با ضمانت ارسال انبار مرکزی`,
        profitMarginPercent: 0,
        description: `فروشگاه مستقل ${formData.storeName} تحت بستر یکپارچه دراپ‌شیپینگ.`,
        activeCategories: formData.categories,
        totalSales: 0,
        monthlyRevenueEstimate: 0,
        activePlanId: 'trial', // Requirement 15: 1-month free trial
        signedContractVersion: platformSettings.contracts?.sellerContractVersion || 2, // Item 7: Record contract version signed at onboarding
      };
      let createdStore = newStore;
      if (!isMockMode()) {
        try {
          createdStore = await storeApi.registerStore({
            storeName: formData.storeName,
            slug: formData.slug.toLowerCase().trim(),
            ownerName: formData.ownerName,
            referralCode: formData.referralCode?.trim() || undefined,
          });
          await refreshToken();
        } catch (err: any) {
          toastError(err instanceof ApiError && err.status === 401
            ? 'برای افتتاح فروشگاه ابتدا وارد شوید'
            : err?.message || 'افتتاح فروشگاه انجام نشد. لطفاً دوباره تلاش کنید.');
          return;
        }
      }
      addStore(createdStore);
      setCurrentStore(createdStore);
      setCreatedStore(createdStore);
      toastSuccess(`فروشگاه «${createdStore.name}» با موفقیت افتتاح گردید.`);
      setStep(4);
    }
  };

  const handleFinish = () => {
    if (createdStore) {
      setCurrentStore(createdStore);
    }
    setIsSellerModalOpen(false);
    setStep(1);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-[#172d26]/55 backdrop-blur-[2px]">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 10 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        role="dialog"
        aria-modal="true"
        aria-labelledby="seller-onboarding-title"
        className="bg-white text-[var(--shop-ink,#172d26)] rounded-[var(--shop-radius-lg,24px)] max-w-xl w-full min-w-0 overflow-hidden shadow-2xl border border-[var(--shop-line,#dde2d7)] flex flex-col max-h-[calc(100dvh-2rem)]"
      >
        {/* Header */}
        <div className="flex items-center justify-between gap-3 border-b border-[var(--shop-line,#dde2d7)] bg-[var(--shop-soft,#edf2e9)] p-4 sm:p-5">
          <div className="flex items-center gap-2.5">
            <div className="grid size-11 shrink-0 place-items-center rounded-xl bg-[var(--shop-forest,#164638)] text-white">
              <Store className="w-5 h-5"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 id="seller-onboarding-title" className="font-extrabold text-base">افتتاح فروشگاه اختصاصی (دراپ‌شیپر)</h3>
              <p className="mt-1 text-sm text-[var(--shop-muted,#4f6359)]">مرحله {toPersianDigits(step)} از ۴</p>
            </div>
          </div>
          <button
            type="button"
            aria-label="بستن افتتاح فروشگاه"
            onClick={() => setIsSellerModalOpen(false)}
            className="grid size-11 shrink-0 place-items-center rounded-xl text-[var(--shop-forest,#164638)] hover:bg-white"
          >
            <X className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>

        {/* Content Body */}
        <div className="min-h-0 space-y-4 overflow-y-auto overscroll-contain p-4 sm:p-6">
          {step === 1 && <SellerStep1 formData={formData} setFormData={setFormData} />}
          {step === 2 && <SellerStep2 formData={formData} setFormData={setFormData} />}
          {step === 3 && (
            <div className="space-y-3 select-none">
              <div className="flex items-center gap-2 text-[var(--shop-forest,#164638)]">
                <ShieldCheck className="w-5 h-5"  strokeWidth={1.5}/>
                <div className="flex flex-wrap items-center gap-2">
                  <h4 className="font-bold text-xs flex items-center gap-1">مطالعه و امضای دیجیتال قرارداد همکاری تجاری<HelpExplainer text="طبق الزامات تجارت الکترونیکی، افتتاح فروشگاه و بهره‌برداری از موجودی انبار مرکزی مستلزم تأیید و امضای الکترونیکی قرارداد زیر است:" /></h4>
                  <span className="shrink-0 rounded-md bg-[var(--shop-soft,#edf2e9)] px-2 py-0.5 text-xs font-bold text-[var(--shop-forest,#164638)]">
                    نسخه {toPersianDigits(platformSettings.contracts?.sellerContractVersion || 2)}.۰
                  </span>
                </div>
              </div>

              <div className="max-h-40 overflow-y-auto rounded-xl border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] p-3 text-sm leading-7 whitespace-pre-line">
                {platformSettings.contracts.sellerContractText}
              </div>

              <div className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-soft,#edf2e9)] px-3 py-2.5 text-xs text-[var(--shop-forest,#164638)]">
                <span>مهر زمانی امضا: {new Date().toLocaleDateString('fa-IR')}</span>
                <span className="font-mono">کد رهگیری امضا: SIG-DRP-{signatureCode}</span>
              </div>

              <label className="flex items-start gap-2 p-2.5 rounded-xl border border-slate-200 bg-slate-50/50 cursor-pointer text-xs">
                <input
                  type="checkbox"
                  checked={contractAccepted}
                  onChange={(e) => setContractAccepted(e.target.checked)}
                  className="w-4 h-4 accent-[var(--shop-forest,#164638)] rounded mt-0.5 cursor-pointer"
                />
                <span className="text-[11px] text-slate-800 leading-relaxed font-bold">
                  متن کامل قرارداد همکاری تجاری دراپ‌شیپینگ را مطالعه نموده و کلیه تعهدات حقوقی و مالی آن را می‌پذیرم.
                </span>
              </label>
            </div>
          )}
          {step === 4 && createdStore && <SellerStep3 createdStore={createdStore} />}
        </div>

        {/* Footer Buttons */}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] p-4">
          {step > 1 && step < 4 ? (
            <button
              type="button"
              onClick={() => setStep((prev) => (prev - 1) as any)}
              className="central-button central-button--outline"
            >
              <ArrowRight className="w-4 h-4"  strokeWidth={1.5}/>
              <span>مرحله قبلی</span>
            </button>
          ) : <div />}

          {step < 4 ? (
            <button
              type="button"
              onClick={handleNext}
              className="central-button ms-auto"
            >
              <span>{step === 3 ? 'امضای دیجیتال و افتتاح فروشگاه' : 'مرحله بعدی'}</span>
              <ArrowLeft className="w-4 h-4"  strokeWidth={1.5}/>
            </button>
          ) : (
            <button
              type="button"
              onClick={handleFinish}
              className="central-button w-full"
            >
              ورود به فروشگاه من و شروع فروش
            </button>
          )}
        </div>
      </motion.div>
    </div>
  );
};
