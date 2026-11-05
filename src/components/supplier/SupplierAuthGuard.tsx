import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { authApi } from '../../services/authApi';
import { SupplierAccountStateScreen, SupplierAccountStateInfo, supplierAccountStateFromError } from './SupplierAccountStateScreen';
import { tokenStore } from '../../services/apiClient';
import { Building2, KeyRound, ArrowRight, ShieldCheck, X, CheckCircle2, FileText } from 'lucide-react';

interface SupplierAuthGuardProps {
  onBackToCustomerSite: () => void;
  children: React.ReactNode;
}

const OFFICIAL_SUPPLIERS = [
  {
    supplierId: 'sup-ali',
    companyName: 'علی تأمین‌کننده (ارسال مستقیم و دایرکت دراپ‌شیپ)',
    managerName: 'علی تأمین‌کننده',
    code: 'SUP-DIRECT',
    warehouseCity: 'تهران / انبار تأمین مستقیم',
  },
  {
    supplierId: 'sup-technolife',
    companyName: 'شرکت بازرگانی فناوری نوین (تکنولایف)',
    managerName: 'مهندس حسینی',
    code: 'SUP-8820',
    warehouseCity: 'انبار مرکزی تهران',
  },
  {
    supplierId: 'sup-mashhad-leather',
    companyName: 'صنایع چرم مشهد',
    managerName: 'آقای شریفی',
    code: 'SUP-7714',
    warehouseCity: 'مشهد / هاب شرق',
  },
  {
    supplierId: 'sup-khanoumi',
    companyName: 'تأمین زیبایی و سلامت خانومی',
    managerName: 'خانم دکتر صادقی',
    code: 'SUP-9912',
    warehouseCity: 'انبار مرکزی تهران',
  },
];

export const SupplierAuthGuard: React.FC<SupplierAuthGuardProps> = ({
  onBackToCustomerSite,
  children,
}) => {
  const { supplierSession, setSupplierSession, platformSettings, toPersianDigits } = useStore();
  const { success, error } = useToast();

  const [selectedSupplierId, setSelectedSupplierId] = useState(OFFICIAL_SUPPLIERS[0].supplierId);
  const [mobile, setMobile] = useState('');
  const [otp, setOtp] = useState('');
  const [otpSent, setOtpSent] = useState(false);
  const [countdown, setCountdown] = useState(120);
  const [isOtpExpired, setIsOtpExpired] = useState(false);
  const [contractAccepted, setContractAccepted] = useState(false);
  const [showContractModal, setShowContractModal] = useState(false);
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
    if (!contractAccepted) {
      error('لطفاً شرایط و تعهدات قرارداد تأمین را تأیید فرمایید.');
      return;
    }

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

      const res = await authApi.verifyOTP(mobile.trim(), otp.trim(), 'supplier');
      tokenStore.setToken(res.token);
      const profile = await authApi.getSupplierProfile();
      setSupplierSession({
        token: res.token,
        supplierId: profile.id,
        companyName: profile.company_name,
        managerName: res.user.fullName,
        warehouseCity: profile.warehouse_city,
        warehouseCityConfirmed: profile.warehouse_city_confirmed !== false,
        permissions: profile.permissions || [],
        loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
        signedContractVersion: profile.signedContractVersion || platformSettings.contracts?.supplierContractVersion || 2,
      });
      setIsSubmitting(false);
      success(`ورود با حساب «${profile.company_name}» انجام شد.`);
    } catch (err: any) {
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
              <label className="text-xs font-bold text-slate-700 block mb-1.5">
                انتخاب سریع شرکت یا تأمین‌کننده:
              </label>
              <select
                value={selectedSupplierId}
                onChange={(e) => {
                  const id = e.target.value;
                  setSelectedSupplierId(id);
                  setOtp('');
                  setOtpSent(false);
                  setCountdown(120);
                  setIsOtpExpired(false);
                }}
                className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:outline-hidden"
              >
                {OFFICIAL_SUPPLIERS.map((s) => (
                  <option key={s.supplierId} value={s.supplierId}>
                    {s.companyName}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label className="text-xs font-bold text-slate-700 block mb-1.5">
                شماره موبایل
              </label>
              <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-2xl px-3.5 py-2.5 focus-within:border-indigo-500 focus-within:bg-white">
                <input
                  type="tel"
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
                <label className="text-xs font-bold text-slate-700 block mb-1.5">کد یکبارمصرف</label>
                <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-2xl px-3.5 py-2.5 focus-within:border-indigo-500 focus-within:bg-white">
                  <KeyRound className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
                  <input
                    type="text"
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

            <div className="space-y-1.5 pt-1">
              <label className="flex items-start gap-2 p-2.5 rounded-xl border border-slate-200 bg-slate-50/70 cursor-pointer hover:bg-slate-50">
                <input
                  type="checkbox"
                  checked={contractAccepted}
                  onChange={(e) => setContractAccepted(e.target.checked)}
                  className="w-4 h-4 accent-indigo-600 rounded mt-0.5 shrink-0"
                />
                <span className="text-[11px] font-bold text-slate-800 leading-relaxed">
                  قرارداد همکاری تأمین و انبارش کالا (نسخه {toPersianDigits(platformSettings.contracts?.supplierContractVersion || 0)}.٫۰) را خوانده‌ام و قبول دارم.
                </span>
              </label>

              <button
                type="button"
                onClick={() => setShowContractModal(true)}
                className="text-[10px] text-indigo-600 hover:text-indigo-800 font-bold flex items-center gap-1 pr-1"
              >
                <FileText className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                <span>مشاهده متن کامل قرارداد دیجیتال</span>
              </button>
            </div>

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

      {showContractModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
          <div className="bg-white rounded-2xl max-w-md w-full p-4 shadow-2xl border border-slate-100 space-y-3 text-xs max-h-[85vh] overflow-y-auto no-scrollbar">
            <div className="flex items-center justify-between border-b pb-2">
              <div className="flex items-center gap-1.5 text-indigo-700 font-bold">
                <ShieldCheck className="w-4 h-4"  strokeWidth={1.5}/>
                <div className="flex items-center gap-1.5">
                  <span>متن کامل قرارداد رسمی همکاری تأمین کالا</span>
                  <span className="text-[9px] bg-indigo-100 text-indigo-800 px-1.5 py-0.2 rounded font-mono font-bold">
                    نسخه {platformSettings.contracts?.supplierContractVersion || 2}.۰
                  </span>
                </div>
              </div>
              <button onClick={() => setShowContractModal(false)}><X className="w-4 h-4 text-slate-400"  strokeWidth={1.5}/></button>
            </div>

            <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl max-h-48 overflow-y-auto text-[11px] text-slate-700 leading-relaxed font-mono whitespace-pre-line no-scrollbar">
              {platformSettings.contracts?.supplierContractText || 'کلیه ضوابط تأمین کالا، پایبندی به اصالت محصول و استانداردهای ارسال مستقیم (Direct Dropship) مورد تایید می‌باشد.'}
            </div>

            <div className="flex gap-2 pt-2 border-t">
              <button
                type="button"
                onClick={() => {
                  setContractAccepted(true);
                  setShowContractModal(false);
                }}
                className="flex-1 py-2 bg-indigo-600 hover:bg-indigo-700 text-white font-bold rounded-xl"
              >
                تأیید شرایط و بازگشت به فرم
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
