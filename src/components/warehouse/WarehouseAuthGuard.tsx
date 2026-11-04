import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { authApi } from '../../services/authApi';
import { tokenStore } from '../../services/apiClient';
import { isValidIranianMobile, normalizeIranianMobile } from '../../utils/validators';
import { Warehouse, KeyRound, ArrowRight, ShieldCheck, CheckCircle2 } from 'lucide-react';

interface WarehouseAuthGuardProps {
  onBackToCustomerSite: () => void;
  children: React.ReactNode;
}

const OFFICIAL_WAREHOUSES = [
  {
    warehouseId: 'WH-TEH-CENTRAL',
    warehouseName: 'انبار مکانیزه مرکزی تهران (مجتمع غرب)',
    staffName: 'علی کارگر (سرپرست شیفت A)',
    mobile: '09000000071',
    code: 'WMS-1010',
    role: 'supervisor' as const,
  },
  {
    warehouseId: 'WH-TEH-DOCK',
    warehouseName: 'انبار بارانداز و ترخیص شورش',
    staffName: 'مهدی صادقی (متصدی بارانداز و QC)',
    mobile: '09000000007',
    code: 'WMS-2020',
    role: 'qc_inspector' as const,
  },
  {
    warehouseId: 'WH-MHD-HUB',
    warehouseName: 'هاب توزیع منطقه‌ای مشهد',
    staffName: 'علیرضا حسینی (اپراتور چیدمان و Pick)',
    mobile: '09000000071',
    code: 'WMS-3030',
    role: 'warehouse_staff' as const,
  },
];

export const WarehouseAuthGuard: React.FC<WarehouseAuthGuardProps> = ({
  onBackToCustomerSite,
  children,
}) => {
  const { warehouseSession, setWarehouseSession, toPersianDigits } = useStore();
  const { success, error } = useToast();

  const [selectedWarehouseId, setSelectedWarehouseId] = useState(OFFICIAL_WAREHOUSES[0].warehouseId);
  const [mobile, setMobile] = useState('');
  const [otp, setOtp] = useState('');
  const [step, setStep] = useState<'mobile' | 'otp'>('mobile');
  const [countdown, setCountdown] = useState(120);
  const [isResending, setIsResending] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    if (step !== 'otp' || countdown <= 0) return;
    const timer = setInterval(() => setCountdown((c) => Math.max(0, c - 1)), 1000);
    return () => clearInterval(timer);
  }, [step, countdown]);

  if (warehouseSession && warehouseSession.token) {
    return <>{children}</>;
  }

  const handleSendOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isValidIranianMobile(mobile)) {
      error('شماره موبایل معتبر ۱۱ رقمی ایران وارد نمایید.');
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

  // FIX 69 (G3): expiry-gated resend, same send-OTP path as the seller
  // guard's handleResendOtp (authApi.sendOTP) — no new OTP endpoint.
  const handleResendOtp = async () => {
    const clean = normalizeIranianMobile(mobile);
    setIsResending(true);
    try {
      const res = await authApi.sendOTP(clean);
      setOtp('');
      setCountdown(res.expiresIn || 120);
      success('کد تایید جدید مجدداً ارسال شد.');
    } catch (err: any) {
      error(err.message || 'خطا در ارسال مجدد کد');
    } finally {
      setIsResending(false);
    }
  };

  const handleVerifyOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (otp.trim().length < 4) {
      error('کد ۴ رقمی را کامل وارد کنید.');
      return;
    }
    if (countdown === 0) {
      error('کد تایید منقضی شده است. لطفاً «ارسال مجدد کد» را بزنید.');
      return;
    }
    const clean = normalizeIranianMobile(mobile);
    const target = OFFICIAL_WAREHOUSES.find((w) => w.warehouseId === selectedWarehouseId) || OFFICIAL_WAREHOUSES[0];
    setIsSubmitting(true);
    try {
      const requestedRole = target.role === 'warehouse_staff' ? 'warehouse_staff' : undefined;
      const res = await authApi.verifyOTP(clean, otp.trim(), requestedRole);
      if (!(res.user.capabilities || []).some((capability) => ['warehouse_staff', 'logistics_specialist', 'admin'].includes(capability)) && !['warehouse_staff', 'logistics_specialist', 'admin'].includes(res.user.role)) {
        error('این شماره حساب مجاز پرسنل انبار ندارد.');
        return;
      }
      tokenStore.setToken(res.token);
      setWarehouseSession({
        token: res.token,
        staffId: res.user.id,
        staffName: res.user.fullName || target.staffName,
        warehouseId: target.warehouseId,
        warehouseName: target.warehouseName,
        role: res.user.role === 'logistics_specialist' ? 'logistics_specialist' : target.role,
        loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
      });
      success(`ورود با موفقیت انجام شد: ${res.user.fullName || target.staffName}`);
    } catch (err: any) {
      error(err.message || 'کد تایید نامعتبر یا منقضی شده است.');
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="flex-1 flex flex-col bg-[#f8fafc] select-none min-h-[82vh]" dir="rtl">
      <header className="sticky top-0 z-30 bg-white/95 backdrop-blur-md border-b border-slate-200/80 px-3.5 py-2.5 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <button
            onClick={onBackToCustomerSite}
            className="p-1.5 rounded-xl hover:bg-slate-100 text-slate-600 transition-colors"
          >
            <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
          <div>
            <h1 className="text-xs font-bold text-slate-800">ورود به سامانه مدیریت انبار (WMS) با OTP</h1>
            <span className="text-[10px] text-slate-400 block">احراز هویت پرسنل لجستیک و انبارداری با شماره همراه</span>
          </div>
        </div>
      </header>

      <div className="p-4 flex-1 flex flex-col justify-center space-y-4 max-w-md mx-auto w-full">
        <div className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-sm space-y-4">
          <div className="flex items-center gap-2.5">
            <div className="w-10 h-10 rounded-2xl bg-amber-600 text-white flex items-center justify-center shadow-sm">
              <Warehouse className="w-5 h-5"  strokeWidth={1.5}/>
            </div>
            <div>
              <h2 className="text-sm font-extrabold text-slate-900">احراز هویت پیامکی پرسنل انبار</h2>
              <p className="text-[11px] text-slate-500">شماره موبایل ثبت‌شده در پرونده پرسنلی</p>
            </div>
          </div>

          <div>
            <label className="text-xs font-bold text-slate-700 block mb-1.5">ایستگاه کاری و سالن انبار:</label>
            <select
              value={selectedWarehouseId}
              onChange={(e) => {
                const id = e.target.value;
                setSelectedWarehouseId(id);
                const w = OFFICIAL_WAREHOUSES.find((x) => x.warehouseId === id);
                if (w) setMobile(w.mobile);
              }}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:outline-hidden"
            >
              {OFFICIAL_WAREHOUSES.map((w) => (
                <option key={w.warehouseId} value={w.warehouseId}>
                  {w.warehouseName} — {w.staffName} ({w.mobile})
                </option>
              ))}
            </select>
          </div>

          {step === 'mobile' ? (
            <form onSubmit={handleSendOtp} className="space-y-3.5">
              <div>
                <label className="text-xs font-bold text-slate-700 block mb-1.5">شماره موبایل پرسنلی:</label>
                <input
                  type="tel"
                  value={mobile}
                  onChange={(e) => setMobile(e.target.value)}
                  placeholder="۰۹۰۰۰۰۰۰۰۷۱"
                  className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 font-mono font-bold focus:bg-white focus:outline-hidden"
                  dir="ltr"
                  required
                  disabled={isSubmitting}
                />
              </div>
              <button
                type="submit"
                disabled={isSubmitting}
                className="w-full py-3 bg-amber-600 hover:bg-amber-700 disabled:opacity-50 text-white font-extrabold text-xs rounded-2xl shadow-xs transition-all flex items-center justify-center gap-2"
              >
                {isSubmitting ? 'در حال ارسال...' : 'دریافت کد تایید'}
              </button>
            </form>
          ) : (
            <form onSubmit={handleVerifyOtp} className="space-y-3.5">
              <div>
                <div className="flex justify-between items-center mb-1 text-xs">
                  <label className="font-bold text-slate-700">کد ۴ رقمی تایید:</label>
                  <button type="button" onClick={() => setStep('mobile')} className="text-amber-700 text-[10px] hover:underline">ویرایش شماره</button>
                </div>
                <input
                  type="text"
                  maxLength={4}
                  value={otp}
                  onChange={(e) => setOtp(e.target.value)}
                  placeholder="• • • •"
                  className="w-full text-center text-base tracking-widest font-mono font-bold p-2.5 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:outline-hidden"
                  dir="ltr"
                  required
                  disabled={isSubmitting}
                />
                {countdown > 0 ? (
                  <span className="text-[10px] text-slate-400 block text-left mt-1 font-mono">
                    اعتبار: {toPersianDigits(Math.floor(countdown / 60))}:{toPersianDigits(String(countdown % 60).padStart(2, '0'))}
                  </span>
                ) : (
                  <button
                    type="button"
                    onClick={handleResendOtp}
                    disabled={isResending}
                    className="text-amber-700 text-[11px] font-bold hover:underline disabled:opacity-50 mt-1"
                  >
                    {isResending ? 'در حال ارسال...' : 'ارسال مجدد کد'}
                  </button>
                )}
              </div>
              <button
                type="submit"
                disabled={isSubmitting}
                className="w-full py-3 bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 text-white font-extrabold text-xs rounded-2xl shadow-xs transition-all flex items-center justify-center gap-2"
              >
                {isSubmitting ? 'در حال بررسی...' : 'تأیید و ورود به پنل انبار'}
              </button>
            </form>
          )}

          <div className="p-2.5 bg-amber-50 border border-amber-200 rounded-2xl text-[11px] text-amber-800 space-y-1">
            <div className="flex items-center gap-1.5 font-bold">
              <ShieldCheck className="w-3.5 h-3.5 text-amber-600"  strokeWidth={1.5}/>
              <span>احراز هویت امن با OTP</span>
            </div>
            <p className="text-[10px] text-amber-700/90 leading-relaxed">
              تمامی ورودها به انبارداری منحصراً با شماره موبایل ثبت‌شده پرسنل انجام می‌پذیرد.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
};
