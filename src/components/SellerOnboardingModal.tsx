import React, { useState, useEffect } from 'react';
import { useStore } from '../context/StoreContext';
import { useToast } from '../context/ToastContext';
import { Store as StoreType } from '../types';
import { storeApi } from '../services/storeApi';
import { isMockMode } from '../services/apiClient';
import { validateStoreSlug, isValidIranianMobile } from '../utils/validators';
import { SellerStep1 } from './SellerStep1';
import { SellerStep2 } from './SellerStep2';
import { SellerStep3 } from './SellerStep3';
import { X, Store, ArrowLeft, ArrowRight, ShieldCheck } from 'lucide-react';
import { motion } from 'motion/react';
import { useAuth } from '../context/AuthContext';

export const SellerOnboardingModal: React.FC = () => {
  const { isSellerModalOpen, setIsSellerModalOpen, setCurrentStore, addStore, platformSettings, toPersianDigits } = useStore();
  const { refreshToken } = useAuth();
  const { error: toastError, success: toastSuccess } = useToast();
  const [step, setStep] = useState<1 | 2 | 3 | 4>(1);

  // Extract referral code from URL query (?ref=DRUP-XXXX) if present
  useEffect(() => {
    if (typeof window !== 'undefined') {
      const urlParams = new URLSearchParams(window.location.search);
      const refParam = urlParams.get('ref');
      if (refParam && refParam.trim()) {
        setFormData((prev) => ({ ...prev, referralCode: refParam.trim().toUpperCase() }));
      }
    }
  }, []);

  const [formData, setFormData] = useState({
    storeName: '',
    slug: '',
    ownerName: '',
    phone: '',
    referralCode: '',
    categories: ['digital', 'fashion'],
  });

  const [contractAccepted, setContractAccepted] = useState(false);
  const [createdStore, setCreatedStore] = useState<StoreType | null>(null);

  if (!isSellerModalOpen) return null;

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
          toastError(err?.message || 'افتتاح فروشگاه انجام نشد. لطفاً دوباره تلاش کنید.');
          return;
        }
      }
      addStore(createdStore);
      setCurrentStore(createdStore);
      setCreatedStore(createdStore);
      addStore(newStore);
      toastSuccess(`فروشگاه «${createdStore.name}» با موفقیت افتتاح گردید.`);
      setStep(4);
      toastSuccess(`قرارداد همکاری تایید و فروشگاه «${newStore.name}» با موفقیت افتتاح گردید.`);
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
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-sm">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 10 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-3xl max-w-lg w-full overflow-hidden shadow-2xl border border-slate-100 flex flex-col max-h-[90vh]"
      >
        {/* Header */}
        <div className="bg-gradient-to-r from-blue-700 via-indigo-700 to-blue-800 text-white p-5 flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <div className="w-10 h-10 rounded-2xl bg-white/15 flex items-center justify-center">
              <Store className="w-5 h-5"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="font-extrabold text-base">افتتاح فروشگاه اختصاصی (دراپ‌شیپر)</h3>
              <p className="text-xs text-white/80">مرحله {toPersianDigits(step)} از ۴</p>
            </div>
          </div>
          <button
            onClick={() => setIsSellerModalOpen(false)}
            className="p-1.5 rounded-full hover:bg-white/20 text-white transition-colors"
          >
            <X className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>

        {/* Content Body */}
        <div className="p-6 overflow-y-auto no-scrollbar space-y-4">
          {step === 1 && <SellerStep1 formData={formData} setFormData={setFormData} />}
          {step === 2 && <SellerStep2 formData={formData} setFormData={setFormData} />}
          {step === 3 && (
            <div className="space-y-3 select-none">
              <div className="flex items-center gap-2 text-indigo-700">
                <ShieldCheck className="w-5 h-5"  strokeWidth={1.5}/>
                <div className="flex items-center gap-2">
                  <h4 className="font-bold text-xs">مطالعه و امضای دیجیتال قرارداد همکاری تجاری</h4>
                  <span className="text-[9px] bg-indigo-100 text-indigo-800 px-1.5 py-0.2 rounded font-mono font-bold">
                    نسخه {toPersianDigits(platformSettings.contracts?.sellerContractVersion || 2)}.۰
                  </span>
                </div>
              </div>
              <p className="text-[11px] text-slate-500 leading-relaxed">
                طبق الزامات تجارت الکترونیکی، افتتاح فروشگاه و بهره‌برداری از موجودی انبار مرکزی مستلزم تأیید و امضای الکترونیکی قرارداد زیر است:
              </p>

              <div className="p-3 bg-slate-50 border border-slate-200 rounded-2xl max-h-40 overflow-y-auto text-[11px] text-slate-700 leading-relaxed font-mono whitespace-pre-line no-scrollbar">
                {platformSettings.contracts.sellerContractText}
              </div>

              <div className="p-2.5 bg-blue-50/70 border border-blue-200/80 rounded-xl flex items-center justify-between text-[10px] text-blue-900">
                <span>مهر زمانی امضا: {new Date().toLocaleDateString('fa-IR')}</span>
                <span className="font-mono">کد رهگیری امضا: SIG-DRP-{Math.floor(1000 + Math.random() * 9000)}</span>
              </div>

              <label className="flex items-start gap-2 p-2.5 rounded-xl border border-slate-200 bg-slate-50/50 cursor-pointer text-xs">
                <input
                  type="checkbox"
                  checked={contractAccepted}
                  onChange={(e) => setContractAccepted(e.target.checked)}
                  className="w-4 h-4 accent-indigo-600 rounded mt-0.5 cursor-pointer"
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
        <div className="p-4 bg-slate-50 border-t border-slate-100 flex items-center justify-between">
          {step > 1 && step < 4 ? (
            <button
              onClick={() => setStep((prev) => (prev - 1) as any)}
              className="flex items-center gap-1 text-xs font-bold text-slate-600 hover:text-slate-900 px-3 py-2 rounded-xl transition-colors"
            >
              <ArrowRight className="w-4 h-4"  strokeWidth={1.5}/>
              <span>مرحله قبلی</span>
            </button>
          ) : <div />}

          {step < 4 ? (
            <button
              onClick={handleNext}
              className="flex items-center gap-1 text-xs md:text-sm font-bold bg-blue-600 hover:bg-blue-700 text-white px-5 py-2.5 rounded-xl shadow-md shadow-blue-500/20 transition-all"
            >
              <span>{step === 3 ? 'امضای دیجیتال و افتتاح فروشگاه' : 'مرحله بعدی'}</span>
              <ArrowLeft className="w-4 h-4"  strokeWidth={1.5}/>
            </button>
          ) : (
            <button
              onClick={handleFinish}
              className="w-full text-center text-xs md:text-sm font-extrabold bg-emerald-600 hover:bg-emerald-700 text-white px-5 py-3 rounded-xl shadow-md shadow-emerald-500/20 transition-all"
            >
              ورود به فروشگاه من و شروع فروش
            </button>
          )}
        </div>
      </motion.div>
    </div>
  );
};
