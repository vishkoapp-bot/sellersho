import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { AuthRequestSupersededError, useAuth } from '../../context/AuthContext';
import { useToast } from '../../context/ToastContext';
import { authApi } from '../../services/authApi';
import { SupplierAccountStateScreen, SupplierAccountStateInfo, supplierAccountStateFromError } from './SupplierAccountStateScreen';
import { Building2, KeyRound, ArrowRight } from 'lucide-react';

interface SupplierAuthGuardProps {
  onBackToCustomerSite: () => void;
  children: React.ReactNode;
}

export const SupplierAuthGuard: React.FC<SupplierAuthGuardProps> = ({
  onBackToCustomerSite,
  children,
}) => {
  const { supplierSession, setSupplierSession, toPersianDigits } = useStore();
  const { authenticate } = useAuth();
  const { success, error } = useToast();

  const [mobile, setMobile] = useState('');
  const [otp, setOtp] = useState('');
  const [otpSent, setOtpSent] = useState(false);
  const [countdown, setCountdown] = useState(120);
  const [isOtpExpired, setIsOtpExpired] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [accountState, setAccountState] = useState<SupplierAccountStateInfo | null>(null);

  useEffect(() => {
    if (!otpSent || countdown <= 0) {
      if (otpSent && countdown === 0) {
        setIsOtpExpired(true);
      }
      return;
    }
    const timer = setInterval(() => {
      setCountdown((prev) => Math.max(0, prev - 1));
    }, 1000);
    return () => clearInterval(timer);
  }, [otpSent, countdown]);

  const formatTimer = (sec: number) => {
    const m = Math.floor(sec / 60);
    const s = sec % 60;
    return `${toPersianDigits(m.toString().padStart(2, '0'))}:${toPersianDigits(
      s.toString().padStart(2, '0')
    )}`;
  };

  if (supplierSession && supplierSession.token) {
    return <>{children}</>;
  }

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!mobile.trim()) {
      error('شماره موبایل را وارد کنید.');
      return;
    }
    if (otpSent && !otp.trim()) {
      error('کد یکبارمصرف را وارد کنید.');
      return;
    }
    if (otpSent && (isOtpExpired || countdown === 0)) {
      error('کد یکبارمصرف منقضی شده است. لطفاً «ارسال مجدد کد» را بزنید.');
      return;
    }

    setIsSubmitting(true);
    let requestIsCurrent = () => true;
    try {
      if (!otpSent) {
        const res = await authApi.sendOTP(mobile.trim());
        setOtpSent(true);
        setCountdown(res.expiresIn || 120);
        setIsOtpExpired(false);
        setIsSubmitting(false);
        success('کد یکبارمصرف ارسال شد.');
        return;
      }

      const res = await authenticate(async () => {
        const response = await authApi.verifyOTP(mobile.trim(), otp.trim(), 'supplier');
        if (!response.user.capabilities?.includes('supplier')) throw new Error('این شماره مجوز پنل تأمین‌کننده را ندارد.');
        return response;
      });
      requestIsCurrent = res.isCurrent;
      if (!requestIsCurrent()) return;
      const profile = await authApi.getSupplierProfile();
      if (!requestIsCurrent()) return;
      setSupplierSession({
        token: res.token,
        supplierId: profile.id,
        companyName: profile.company_name,
        managerName: res.user.fullName,
        warehouseCity: profile.warehouse_city,
        warehouseCityConfirmed: profile.warehouse_city_confirmed !== false,
        permissions: profile.permissions || [],
        loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
        signedContractVersion: profile.signedContractVersion ?? 0,
      });
      setIsSubmitting(false);
      success(`ورود با حساب «${profile.company_name}» انجام شد.`);
    } catch (err: any) {
      if (err instanceof AuthRequestSupersededError || !requestIsCurrent()) return;
      setIsSubmitting(false);
      // GAP 19b: if the login response carries a supplier
      // account-state code (pending/rejected), show the dedicated
      // state screen instead of the generic error toast.
      const state = supplierAccountStateFromError(err);
      if (state) {
        setAccountState(state);
        return;
      }
      error(err.message || 'احراز هویت یا دریافت پروفایل تأمین‌کننده با خطا مواجه شد.');
    } finally {
      setIsSubmitting(false);
    }
  };

  // FIX 69 (G3): expiry-gated resend, same send-OTP path as the seller
  // guard's handleResendOtp (authApi.sendOTP) — no new OTP endpoint.
  const handleResendOtp = async () => {
    try {
      const res = await authApi.sendOTP(mobile.trim());
      setOtp('');
      setCountdown(res.expiresIn || 120);
      setIsOtpExpired(false);
      success(`کد تایید جدید مجدداً به شماره ${toPersianDigits(mobile.trim())} ارسال گردید.`);
    } catch (err: any) {
      error(err.message || 'خطا در ارسال مجدد کد');
    }
  };

  if (accountState) {
    return <SupplierAccountStateScreen {...accountState} onBack={() => setAccountState(null)} />;
  }

  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc] select-none min-h-[82vh]">
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200/80 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={onBackToCustomerSite}
            className="p-1.5 rounded-xl hover:bg-slate-100 text-slate-600 transition-colors"
          >
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <div>
            <h1 className="text-xs font-bold text-slate-800">ورود به پنل شرکای تأمین کالا</h1>
            <span className="text-[10px] text-slate-400 block">احراز هویت سازمانی طرف قرارداد</span>
          </div>
        </div>
      </header>

      <div className="p-4 flex-1 flex flex-col justify-center space-y-4 max-w-md mx-auto w-full">
        <div className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-sm space-y-4">
          <div className="flex items-center gap-2.5">
            <div className="w-10 h-10 rounded-2xl bg-indigo-600 text-white flex items-center justify-center shadow-sm">
              <Building2 className="w-5 h-5"  strokeWidth={1.5}/>
            </div>
            <div>
              <h2 className="text-sm font-extrabold text-slate-900">
                احراز هویت شرکت تأمین‌کننده
              </h2>
              <p className="text-[11px] text-slate-500">
                ورود امن با شماره همراه و کد یکبارمصرف (OTP)
              </p>
            </div>
          </div>

          <form onSubmit={handleLogin} className="space-y-3.5">
            <div>
              <label htmlFor="supplier-sign-in-mobile" className="text-xs font-bold text-slate-700 block mb-1.5">
                شماره موبایل
              </label>
              <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-2xl px-3.5 py-2.5 focus-within:border-indigo-500 focus-within:bg-white">
                <input
                  type="tel"
                  id="supplier-sign-in-mobile"
                  autoComplete="tel-national"
                  value={mobile}
                  onChange={(e) => setMobile(e.target.value)}
                  placeholder="۰۹۱۲۳۴۵۶۷۸۹"
                  className="w-full bg-transparent text-xs font-bold text-slate-900 focus:outline-hidden"
                  dir="ltr"
                  required
                  disabled={otpSent}
                />
              </div>
            </div>

            {otpSent && (
              <div>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <label htmlFor="supplier-sign-in-otp" className="text-xs font-bold text-slate-700">کد یکبارمصرف</label>
                  <button type="button" disabled={isSubmitting} onClick={() => { setOtpSent(false); setOtp(''); setIsOtpExpired(false); }} className="min-h-11 px-2 text-xs font-bold text-brand-deep">ویرایش شماره</button>
                </div>
                <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-2xl px-3.5 py-2.5 focus-within:border-indigo-500 focus-within:bg-white">
                  <KeyRound className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
                  <input
                    type="text"
                    id="supplier-sign-in-otp"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    maxLength={4}
                    value={otp}
                    onChange={(e) => setOtp(e.target.value.replace(/[۰-۹]/g, (d) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(d))).replace(/\D/g, '').slice(0, 4))}
                    placeholder="کد ۴ رقمی"
                    className="w-full bg-transparent text-xs font-bold text-slate-900 focus:outline-hidden"
                    dir="ltr"
                    required
                  />
                </div>
                <div className="flex items-center justify-between text-[11px] bg-slate-50 px-3 py-2 rounded-xl border border-slate-100">
                  {countdown > 0 ? (
                    <span className="text-slate-500">
                      زمان باقی‌مانده تا ارسال مجدد:{' '}
                      <strong className="font-mono text-slate-800">{formatTimer(countdown)}</strong>
                    </span>
                  ) : (
                    <button
                      type="button"
                      onClick={handleResendOtp}
                      className="text-indigo-600 font-bold flex items-center gap-1 hover:underline"
                    >
                      <span>ارسال مجدد کد یکبارمصرف</span>
                    </button>
                  )}
                </div>
              </div>
            )}

            <button
              type="submit"
              disabled={isSubmitting}
              className="w-full py-3 bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 text-white font-extrabold text-xs rounded-2xl shadow-xs transition-all flex items-center justify-center gap-2"
            >
              {isSubmitting ? 'در حال احراز هویت...' : otpSent ? 'تأیید و ورود' : 'دریافت کد تأیید'}
            </button>
          </form>
        </div>
      </div>

    </div>
  );
};
