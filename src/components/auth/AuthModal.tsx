import React, { useState, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useToast } from '../../context/ToastContext';
import { isValidIranianMobile, normalizeIranianMobile } from '../../utils/validators';
import { toPersianDigits } from '../../utils/formatters';
import { Phone, KeyRound, X, ArrowLeft, CheckCircle2 } from 'lucide-react';
import { motion } from 'motion/react';
import { authApi } from '../../services/authApi';
import { isMockMode } from '../../services/apiClient';
import { adminRolesForCapabilities, useAuth } from '../../context/AuthContext';
import { useStore } from '../../context/StoreContext';
import { storeApi } from '../../services/storeApi';

interface Props {
  isOpen: boolean;
  onClose: () => void;
  onSuccess?: (user: { id: string; mobile: string; fullName: string; role: string; capabilities?: string[] }) => void;
  disableAutoNavigate?: boolean;
  roleTitle?: string;
}

export const AuthModal: React.FC<Props> = ({
  isOpen,
  onClose,
  onSuccess,
  roleTitle = 'ورود سریع به حساب کاربری',
  disableAutoNavigate = false,
}) => {
  const { success, error, info } = useToast();
  const { loginWithOTP } = useAuth();
  const { currentStore, setActiveView, setCurrentStore } = useStore();
  const [step, setStep] = useState<'phone' | 'otp'>('phone');
  const [mobile, setMobile] = useState('');
  const [otp, setOtp] = useState('');
  const [timer, setTimer] = useState(120);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (isOpen) {
      document.body.style.overflow = 'hidden';
      const scrollbarWidth = window.innerWidth - document.documentElement.clientWidth;
      document.body.style.paddingRight = `${scrollbarWidth}px`;
    } else {
      document.body.style.overflow = '';
      document.body.style.paddingRight = '';
    }
    return () => {
      document.body.style.overflow = '';
      document.body.style.paddingRight = '';
    };
  }, [isOpen]);

  useEffect(() => {
    let interval: any;
    if (step === 'otp' && timer > 0) {
      interval = setInterval(() => setTimer((t) => t - 1), 1000);
    }
    return () => clearInterval(interval);
  }, [step, timer]);

  if (!isOpen) return null;

  const handleSendOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!mobile.trim()) {
      error('شماره موبایل را وارد کنید.');
      return;
    }
    if (!isValidIranianMobile(mobile)) {
      error('شماره موبایل نامعتبر است (مانند ۰۹۱۲۳۴۵۶۷۸۹).');
      return;
    }
    const cleanMobile = normalizeIranianMobile(mobile);

    if (!isMockMode()) {
      setIsLoading(true);
      try {
        const res = await authApi.sendOTP(cleanMobile);
        setStep('otp');
        setTimer(res.expiresIn || 120);
        success(`کد یکبارمصرف به شماره ${toPersianDigits(cleanMobile)} پیامک شد.`);
      } catch (err: any) {
        error(err.message || 'خطا در ارسال کد تایید');
      } finally {
        setIsLoading(false);
      }
    } else {
      setStep('otp');
      setTimer(120);
      setOtp('1234');
      success(`کد آزمایشی ۴ رقمی (۱۲۳۴) است؛ در حالت نمایشی پیامکی ارسال نمی‌شود.`);
    }
  };

  const handleResendOtp = async () => {
    const cleanMobile = normalizeIranianMobile(mobile);
    if (!isMockMode()) {
      setIsLoading(true);
      try {
        const res = await authApi.sendOTP(cleanMobile);
        setTimer(res.expiresIn || 120);
        setOtp('');
        success(`کد یکبارمصرف جدید به شماره ${toPersianDigits(cleanMobile)} پیامک شد.`);
      } catch (err: any) {
        error(err.message || 'خطا در ارسال مجدد کد');
      } finally {
        setIsLoading(false);
      }
    } else {
      setTimer(120);
      setOtp('');
      success(`کد آزمایشی همان ۱۲۳۴ است؛ در حالت نمایشی پیامکی ارسال نمی‌شود.`);
    }
  };

  const handleVerifyOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!otp.trim()) {
      error('لطفاً کد یکبارمصرف را وارد کنید.');
      return;
    }
    if (otp.length < 4) {
      error('کد تأیید باید ۴ رقم باشد.');
      return;
    }
    const cleanMobile = normalizeIranianMobile(mobile);

    if (!isMockMode()) {
      setIsLoading(true);
      try {
        // Fix83: carry the storefront the buyer signs up from; omitted when
        // no store context is present (never a default store).
        const user = await loginWithOTP(cleanMobile, otp.trim(), currentStore?.slug);
        success('احراز هویت با موفقیت انجام شد.');
        if (onSuccess) onSuccess(user);
        // Live parity (live bundle AuthModal): post-login auto-navigation
        // runs AFTER onSuccess, unless the caller opts out with
        // disableAutoNavigate (live checkout / quick-buy / storefront
        // callers pass it). Admin capabilities go to admin_panel; a
        // seller goes to their own storefront (first owned store, when
        // it has a slug). Decisions use the active server memberships.
        if (!disableAutoNavigate) {
          const capabilities = user.capabilities || [];
          if (adminRolesForCapabilities(capabilities).length) {
            setActiveView('admin_panel');
          } else if (capabilities.includes('seller')) {
            try {
              const stores = await storeApi.getOwnedStores();
              const firstStore = stores[0];
              if (firstStore && firstStore.slug) {
                setCurrentStore(firstStore);
                setActiveView('seller_storefront', { storeSlug: firstStore.slug });
              }
            } catch (err) {
              console.error('[AuthModal] post-login storefront navigation failed:', err);
            }
          }
        }
        onClose();
        setStep('phone');
        setOtp('');
      } catch (err: any) {
        error(err.message || 'کد یکبارمصرف نادرست یا منقضی شده است.');
      } finally {
        setIsLoading(false);
      }
    } else {
      success('احراز هویت با موفقیت انجام شد.');
      if (onSuccess) onSuccess({ id: '', mobile: cleanMobile, fullName: '', role: 'customer' });
      onClose();
      setStep('phone');
      setOtp('');
    }
  };

  return createPortal(
    <div 
      className="fixed inset-0 z-[9999] flex items-center justify-center overflow-y-auto p-3 bg-slate-950/70 backdrop-blur-md select-none"
      onClick={(e) => {
        if (e.target === e.currentTarget) {
          onClose();
        }
      }}
    >
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 10 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[380px] w-full max-h-[calc(100dvh-1.5rem)] overflow-y-auto p-4 shadow-2xl border border-slate-100 space-y-3"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center">
              {step === 'phone' ? <Phone className="w-4 h-4"  strokeWidth={1.5}/> : <KeyRound className="w-4 h-4"  strokeWidth={1.5}/>}
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">{roleTitle}</h3>
              <span className="text-[10px] text-slate-400 block">ارسال پیامک کد اعتبارسنجی</span>
            </div>
          </div>
          <button onClick={onClose} aria-label="بستن" className="w-11 h-11 rounded-lg text-slate-400 hover:bg-slate-100 flex items-center justify-center">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>

        {step === 'phone' ? (
          <form onSubmit={handleSendOtp} className="space-y-3">
            <div>
              <label className="text-[11px] font-bold text-slate-700 block mb-1">شماره موبایل</label>
              <input
                type="tel"
                placeholder="۰۹۱۲۳۴۵۶۷۸۹"
                value={mobile}
                onChange={(e) => setMobile(e.target.value)}
                className="w-full text-base p-2.5 rounded-xl bg-slate-50 border border-slate-200 tracking-wider focus:outline-hidden focus:border-brand focus:ring-2 focus:ring-brand/20"
                dir="ltr"
                autoFocus
                disabled={isLoading}
              />
            </div>
            <button
              type="submit"
              disabled={isLoading}
              className="w-full py-3.5 bg-brand hover:bg-brand-deep disabled:opacity-50 text-white font-bold text-xs rounded-xl shadow-xs flex items-center justify-center gap-1.5"
            >
              <span>{isLoading ? 'در حال ارسال…' : 'دریافت کد تأیید'}</span>
              <ArrowLeft className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            </button>
          </form>
        ) : (
          <form onSubmit={handleVerifyOtp} className="space-y-3">
            <div>
              <div className="flex justify-between items-center mb-1 text-[11px]">
                <label className="font-bold text-slate-700">کد ۴ رقمی</label>
                <button
                  type="button"
                  onClick={() => setStep('phone')}
                  className="text-brand-deep text-xs font-bold hover:underline py-1 px-1 -my-1"
                >
                  ویرایش شماره
                </button>
              </div>
              <input
                type="text"
                maxLength={4}
                placeholder="• • • •"
                value={otp}
                onChange={(e) => setOtp(e.target.value.replace(/[۰-۹]/g, (d) => String(d.charCodeAt(0) - 1776)).replace(/[٠-٩]/g, (d) => String(d.charCodeAt(0) - 1632)).replace(/\D/g, '').slice(0, 4))}
                className="w-full text-center text-lg tracking-widest p-2 rounded-xl bg-slate-50 border border-slate-200 focus:outline-hidden focus:border-brand focus:ring-2 focus:ring-brand/20"
                dir="ltr"
                autoFocus
                disabled={isLoading}
              />
              {timer > 0 ? (
                <span className="text-[10px] text-slate-400 block text-left mt-1">
                  زمان: {toPersianDigits(Math.floor(timer / 60))}:{toPersianDigits(String(timer % 60).padStart(2, '0'))}
                </span>
              ) : (
                <div className="flex items-center justify-between mt-1.5">
                  <span className="text-[10px] text-rose-500 font-bold">کد منقضی شد.</span>
                  <button type="button" onClick={handleResendOtp} disabled={isLoading} className="text-[11px] text-brand-deep font-bold hover:underline disabled:opacity-50">
                    ارسال مجدد کد
                  </button>
                </div>
              )}
            </div>
            <button
              type="submit"
              disabled={isLoading}
              className="w-full py-3.5 bg-brand hover:bg-brand-deep disabled:opacity-50 text-white font-bold text-xs rounded-xl shadow-xs flex items-center justify-center gap-1.5"
            >
              <span>{isLoading ? 'در حال بررسی…' : 'تأیید و ورود'}</span>
              <CheckCircle2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            </button>
          </form>
        )}
      </motion.div>
    </div>,
    document.body,
  );
};
