import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { authApi } from '../../services/authApi';
import { adminRoleLabels, adminRolesForCapabilities, AuthRequestSupersededError, useAuth, type AuthenticationResponse } from '../../context/AuthContext';
import { isValidIranianMobile, normalizeIranianMobile } from '../../utils/validators';
import {
  ShieldCheck,
  Lock,
  ArrowRight,
} from 'lucide-react';

interface AdminLoginPageProps {
  onBackToCustomerSite: () => void;
}


export const normalizeOtpInput = (value: string): string =>
  value
    .replace(/[۰-۹]/g, (d) => String(d.charCodeAt(0) - 1776))
    .replace(/[٠-٩]/g, (d) => String(d.charCodeAt(0) - 1632))
    .replace(/\D/g, '')
    .slice(0, 4);

export const AdminLoginPage: React.FC<AdminLoginPageProps> = ({ onBackToCustomerSite }) => {
  const { toPersianDigits } = useStore();
  const { authenticate } = useAuth();
  const { success, error } = useToast();

  const [mobile, setMobile] = useState('');
  const [otp, setOtp] = useState('');
  const [step, setStep] = useState<'mobile' | 'otp'>('mobile');
  const [countdown, setCountdown] = useState(120);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [loginError, setLoginError] = useState('');

  useEffect(() => {
    if (step !== 'otp' || countdown <= 0) return;
    const timer = setInterval(() => setCountdown((c) => Math.max(0, c - 1)), 1000);
    return () => clearInterval(timer);
  }, [step, countdown]);

  const handleSendOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isValidIranianMobile(mobile)) {
      error('شماره موبایل معتبر ایران (مثال: ۰۹۱۲۳۴۵۶۷۸۹) وارد کنید.');
      return;
    }
    const clean = normalizeIranianMobile(mobile);
    setIsSubmitting(true);
    try {
      const res = await authApi.sendOTP(clean);
      setOtp('');
      setLoginError('');
      setStep('otp');
      setCountdown(res.expiresIn || 120);
      success('کد یکبارمصرف ارسال شد.');
    } catch (err: any) {
      error(err.message || 'خطا در ارسال کد تایید');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleResendOtp = async () => {
    setIsSubmitting(true);
    try {
      const res = await authApi.sendOTP(normalizeIranianMobile(mobile));
      setOtp('');
      setLoginError('');
      setCountdown(res.expiresIn || 120);
      success('کد یکبارمصرف ارسال شد.');
    } catch (err: any) {
      error(err.message || 'خطا در ارسال مجدد کد.');
    } finally {
      setIsSubmitting(false);
    }
  };

  function withAdminMembership<T extends AuthenticationResponse>(response: T): T {
    if (!adminRolesForCapabilities(response.user.capabilities).length) {
      throw new Error('این شماره مجوز ورود به پنل مدیریت را ندارد.');
    }
    return response;
  }

  const handleVerifyOtp = async (event: React.FormEvent) => {
    event.preventDefault();
    if (isSubmitting) return;
    if (otp.length !== 4 || countdown <= 0) {
      setLoginError(countdown <= 0 ? 'اعتبار کد تمام شده است. کد تازه بگیرید.' : 'کد ۴ رقمی را کامل وارد کنید.');
      return;
    }
    setLoginError('');
    setIsSubmitting(true);
    try {
      const result = await authenticate(async () => withAdminMembership(
        await authApi.verifyOTP(normalizeIranianMobile(mobile), otp),
      ));
      const [role] = adminRolesForCapabilities(result.user.capabilities);
      if (result.isCurrent()) success(`ورود با نقش «${adminRoleLabels[role]}» انجام شد.`);
    } catch (cause) {
      if (cause instanceof AuthRequestSupersededError) return;
      setLoginError(cause instanceof Error ? cause.message : 'کد پیامکی معتبر نیست.');
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="min-h-screen w-full bg-slate-950 text-slate-100 flex flex-col justify-between p-4 md:p-8 select-none relative overflow-hidden" dir="rtl">
      <div className="max-w-5xl w-full mx-auto flex items-center justify-between z-10">
        <div className="flex items-center gap-2.5">
          <div className="w-10 h-10 rounded-2xl bg-brand-deep flex items-center justify-center text-white shadow-lg shadow-brand-deep/25">
            <ShieldCheck className="w-5 h-5" strokeWidth={1.5} />
          </div>
          <div>
            <span className="font-extrabold text-sm md:text-base text-white tracking-tight flex items-center gap-2">
              <span>پنل مدیریت سلرشو</span>
              <span className="text-[10px] font-mono bg-brand/20 text-brand border border-brand/30 px-2 py-0.5 rounded-full">/admin</span>
            </span>
            <span className="text-[11px] text-slate-400 block">ورود کارکنان با کد پیامکی</span>
          </div>
        </div>

        <button
          onClick={onBackToCustomerSite}
          className="flex items-center gap-1.5 px-3.5 min-h-[44px] rounded-xl bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 text-xs font-bold transition-colors"
        >
          <ArrowRight className="w-4 h-4 text-brand" strokeWidth={1.5} />
          <span>بازگشت به فروشگاه مرکزی</span>
        </button>
      </div>

      <div className="max-w-md w-full mx-auto my-8 z-10">
        <div className="bg-slate-900/90 backdrop-blur-xl border border-slate-800 rounded-3xl p-6 md:p-7 shadow-2xl space-y-5">
          <div className="flex justify-center -mb-1">
            <img src="/static/uploads/logo/logo-icon.png" alt="سلرشو" width={64} height={64} decoding="async" className="w-16 h-16 object-contain" />
          </div>
          <div className="flex items-center justify-between border-b border-slate-800 pb-4">
            <div>
              <h1 className="text-base font-extrabold text-white flex items-center gap-2">
                <Lock className="w-4 h-4 text-brand" strokeWidth={1.5} />
                <span>ورود به پنل مدیریت</span>
              </h1>
              <p className="text-[11px] text-slate-400 mt-1">
                با شماره موبایلی که برایت ثبت شده وارد شو؛ کد ورود به همان شماره پیامک می‌شود
              </p>
            </div>
          </div>


          {loginError && <p role="alert" className="rounded-xl bg-rose-950 p-3 text-sm leading-6 text-rose-200">{loginError}</p>}
          {step === 'mobile' ? (
            <form onSubmit={handleSendOtp} className="space-y-3.5">
              <p className="text-[11px] font-bold text-slate-400">قدم ۱ از ۲: شماره موبایلت را بنویس</p>
              <label className="block text-xs font-bold text-slate-300">شماره موبایل
                <input type="tel" value={mobile} onChange={(e) => setMobile(e.target.value)} placeholder="۰۹۱۲۳۴۵۶۷۸۹" className="mt-1.5 w-full min-h-[48px] bg-slate-950/80 border border-slate-800 rounded-2xl p-3 text-sm text-white" dir="ltr" required disabled={isSubmitting} />
              </label>
              <p className="text-[11px] leading-5 text-slate-400">وقتی دکمه را بزنی، یک کد ۴ رقمی با پیامک به همین شماره می‌آید.</p>
              <button type="submit" disabled={isSubmitting} className="w-full min-h-[48px] bg-brand-deep text-white font-extrabold text-xs rounded-2xl">{isSubmitting ? 'در حال ارسال…' : 'دریافت کد پیامکی'}</button>
            </form>
          ) : (
            <form onSubmit={handleVerifyOtp} className="space-y-3.5">
              <p className="text-[11px] font-bold text-slate-400">قدم ۲ از ۲: کد پیامکی را بنویس</p>
              <div>
                <div className="flex justify-between items-center mb-1 text-xs">
                  <label htmlFor="admin-otp" className="font-bold text-slate-300">کد پیامکی</label>
                  <button type="button" disabled={isSubmitting} onClick={() => { setLoginError(''); setStep('mobile'); }} className="text-brand text-[11px] min-h-[44px] px-2 inline-flex items-center hover:underline">عوض کردن شماره</button>
                </div>
                <p className="text-[11px] leading-5 text-slate-400 mb-1.5">کد ۴ رقمی به شماره {toPersianDigits(mobile)} پیامک شد؛ همان را اینجا بنویس.</p>
                <input
                  id="admin-otp"
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={4}
                  value={otp}
                  onChange={(e) => setOtp(normalizeOtpInput(e.target.value))}
                  placeholder="• • • •"
                  className="w-full bg-slate-950/80 border border-slate-800 rounded-2xl p-2.5 text-center text-base tracking-widest font-mono font-bold text-white focus:outline-hidden"
                  dir="ltr"
                  required
                  disabled={isSubmitting}
                />
                <div className="flex items-center justify-between mt-1">
                  {countdown > 0 ? (
                    <span className="text-[11px] text-slate-400">کد نیامد؟ بعد از {toPersianDigits(Math.floor(countdown / 60))}:{toPersianDigits(String(countdown % 60).padStart(2, '0'))} می‌توانی دوباره بفرستی</span>
                  ) : (
                    <button type="button" onClick={handleResendOtp} disabled={isSubmitting} className="text-[11px] text-brand font-bold min-h-[44px] px-2 inline-flex items-center hover:underline disabled:opacity-50">فرستادن دوباره کد</button>
                  )}
                </div>
              </div>
              <button
                type="submit"
                disabled={isSubmitting}
                className="w-full min-h-[48px] bg-brand-deep hover:bg-brand disabled:opacity-50 text-white font-extrabold text-xs rounded-2xl shadow-lg shadow-brand-deep/25 transition-all flex items-center justify-center gap-2"
              >
                {isSubmitting ? 'در حال بررسی…' : 'ورود به پنل'}
              </button>
            </form>
          )}
        </div>
      </div>
      <div />
    </div>
  );
};
