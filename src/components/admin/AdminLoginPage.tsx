import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { AdminRole } from '../../context/storeContextTypes';
import { authApi } from '../../services/authApi';
import { AuthRequestSupersededError, useAuth } from '../../context/AuthContext';
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
  const { setAdminSession, toPersianDigits } = useStore();
  const { authenticate } = useAuth();
  const { success, error } = useToast();

  const [mobile, setMobile] = useState('');
  const [otp, setOtp] = useState('');
  const [password, setPassword] = useState('');
  const [loginMode, setLoginMode] = useState<'otp' | 'password'>('otp');
  const [step, setStep] = useState<'mobile' | 'otp'>('mobile');
  const [countdown, setCountdown] = useState(120);
  const [isSubmitting, setIsSubmitting] = useState(false);

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
      setCountdown(res.expiresIn || 120);
      success('کد یکبارمصرف ارسال شد.');
    } catch (err: any) {
      error(err.message || 'خطا در ارسال مجدد کد.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handlePasswordLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!password.trim()) {
      error('رمز عبور را وارد کنید.');
      return;
    }
    setIsSubmitting(true);
    let requestIsCurrent = () => true;
    try {
      const res = await authenticate(() => authApi.passwordLogin(normalizeIranianMobile(mobile), password));
      requestIsCurrent = res.isCurrent;
      if (!requestIsCurrent()) return;
      const roleMap: Record<string, { role: AdminRole; label: string }> = {
        admin: { role: 'admin', label: 'مدیر پلتفرم' },
        super_admin: { role: 'super_admin', label: 'مدیر ارشد پلتفرم' },
        finance_specialist: { role: 'finance_specialist', label: 'کارشناس ارشد مالی' },
        finance: { role: 'finance_specialist', label: 'کارشناس ارشد مالی' },
        logistics_specialist: { role: 'logistics_specialist', label: 'کارشناس لجستیک و انبار' },
        support_specialist: { role: 'support_specialist', label: 'کارشناس پشتیبانی و نظارت' },
      };
      const resolved = res.user.capabilities?.includes('super_admin')
        ? roleMap.super_admin
        : roleMap[res.user.role];
      if (!resolved) {
        error('این شماره مجوز ورود به مدیریت یا عملیات ندارد.');
        return;
      }
      setAdminSession({ token: res.token, username: res.user.mobile, fullName: res.user.fullName || 'مدیر ارشد پلتفرم', role: resolved.role, roleLabel: resolved.label, loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }) });
      success(`ورود با نقش «${resolved.label}» انجام شد.`);
    } catch (err: any) {
      if (err instanceof AuthRequestSupersededError || !requestIsCurrent()) return;
      error(err.message || 'نام کاربری یا رمز عبور نادرست است.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleVerifyOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (otp.trim().length < 4) {
      error('کد ۴ رقمی را کامل وارد کنید.');
      return;
    }
    const clean = normalizeIranianMobile(mobile);
    setIsSubmitting(true);
    let requestIsCurrent = () => true;
    try {
      const res = await authenticate(() => authApi.verifyOTP(clean, otp.trim()));
      requestIsCurrent = res.isCurrent;
      if (!requestIsCurrent()) return;

      const roleMap: Record<string, { role: AdminRole; label: string }> = {
        admin: { role: 'admin', label: 'مدیر پلتفرم' },
        super_admin: { role: 'super_admin', label: 'مدیر ارشد پلتفرم' },
        finance_specialist: { role: 'finance_specialist', label: 'کارشناس ارشد مالی' },
        finance: { role: 'finance_specialist', label: 'کارشناس ارشد مالی' },
        logistics_specialist: { role: 'logistics_specialist', label: 'کارشناس لجستیک و انبار' },
        support_specialist: { role: 'support_specialist', label: 'کارشناس پشتیبانی و نظارت' },
      };

      const resolved = res.user.capabilities?.includes('super_admin')
        ? roleMap.super_admin
        : roleMap[res.user.role];
      if (!resolved) {
        error('این شماره مجوز ورود به مدیریت یا عملیات ندارد.');
        return;
      }

      setAdminSession({
        token: res.token,
        username: res.user.mobile,
        fullName: res.user.fullName || 'کاربر سیستم',
        role: resolved.role,
        roleLabel: resolved.label,
        loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
      });
      success(`ورود با نقش «${resolved.label}» انجام شد.`);
    } catch (err: any) {
      if (err instanceof AuthRequestSupersededError || !requestIsCurrent()) return;
      error(err.message || 'کد تایید نامعتبر یا منقضی شده است.');
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
            <span className="text-[11px] text-slate-400 block">ورود امن کارکنان با پیامک یا رمز عبور</span>
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


          <div className="flex gap-2 rounded-xl bg-slate-950 p-1">
            <button type="button" onClick={() => setLoginMode('password')} className={`flex-1 rounded-lg min-h-[44px] flex items-center justify-center text-xs font-bold ${loginMode === 'password' ? 'bg-brand-deep text-white' : 'text-slate-400'}`}>ورود با رمز عبور</button>
            <button type="button" onClick={() => { setLoginMode('otp'); setStep('mobile'); }} className={`flex-1 rounded-lg min-h-[44px] flex items-center justify-center text-xs font-bold ${loginMode === 'otp' ? 'bg-brand-deep text-white' : 'text-slate-400'}`}>ورود با کد پیامکی</button>
          </div>
          {loginMode === 'password' ? (
            <form onSubmit={handlePasswordLogin} className="space-y-3.5">
              <label className="block text-xs font-bold text-slate-300">شماره موبایل
                <input type="tel" value={mobile} onChange={(e) => setMobile(e.target.value)} className="mt-1.5 w-full min-h-[48px] bg-slate-950/80 border border-slate-800 rounded-2xl p-3 text-sm font-bold text-white" dir="ltr" required autoComplete="username" />
              </label>
              <label className="block text-xs font-bold text-slate-300">رمز عبور
                <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} className="mt-1.5 w-full min-h-[48px] bg-slate-950/80 border border-slate-800 rounded-2xl p-3 text-sm font-mono text-white" dir="ltr" required autoComplete="current-password" />
              </label>
              <button type="submit" disabled={isSubmitting} className="w-full min-h-[48px] bg-brand-deep hover:bg-brand disabled:opacity-50 text-white font-extrabold text-xs rounded-2xl">{isSubmitting ? 'در حال ورود…' : 'ورود به پنل مدیریت'}</button>
            </form>
          ) : step === 'mobile' ? (
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
                  <label className="font-bold text-slate-300">کد پیامکی</label>
                  <button type="button" onClick={() => setStep('mobile')} className="text-brand text-[11px] min-h-[44px] px-2 inline-flex items-center hover:underline">عوض کردن شماره</button>
                </div>
                <p className="text-[11px] leading-5 text-slate-400 mb-1.5">کد ۴ رقمی به شماره {toPersianDigits(mobile)} پیامک شد؛ همان را اینجا بنویس.</p>
                <input
                  type="text"
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
