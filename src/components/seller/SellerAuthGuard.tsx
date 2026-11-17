import React, { useState, useEffect } from 'react';
import { useStore } from '../../context/StoreContext';
import { AuthRequestSupersededError, useAuth } from '../../context/AuthContext';
import { useToast } from '../../context/ToastContext';
import { isValidIranianMobile, validateStoreSlug } from '../../utils/validators';
import {
  Store,
  Smartphone,
  KeyRound,
  ArrowRight,
  RefreshCw,
  AlertTriangle,
  CheckCircle2,
  ShieldAlert,
  Sparkles,
  Send,
  LogOut,
  UserPlus,
  Check,
} from 'lucide-react';
import { authApi } from '../../services/authApi';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';

interface SellerAuthGuardProps {
  onBackToCustomerSite: () => void;
  children: React.ReactNode;
}

export const SellerAuthGuard: React.FC<SellerAuthGuardProps> = ({
  onBackToCustomerSite,
  children,
}) => {
  const {
    sellerSession,
    setSellerSession,
    logoutSeller,
    stores,
    addStore,
    setCurrentStore,
    storeStatuses,
    toPersianDigits,
  } = useStore();
  const { authenticate } = useAuth();
  const { success, error, warning, info } = useToast();

  // Login Step States
  const [step, setStep] = useState<'mobile' | 'otp'>('mobile');
  const [mobile, setMobile] = useState(isMockMode() ? '09121112233' : '');
  const [otpCode, setOtpCode] = useState(isMockMode() ? '1234' : '');
  const [countdown, setCountdown] = useState(115);
  const [otpError, setOtpError] = useState<string | null>(null);
  const [isOtpExpired, setIsOtpExpired] = useState(false);

  // Onboarding Form States (for newly registered seller without a store)
  const [newStoreName, setNewStoreName] = useState('');
  const [newStoreSlug, setNewStoreSlug] = useState('');
  const [newStoreTagline, setNewStoreTagline] = useState('منتخب بهترین کالاهای دیجیتال و سبک زندگی');

  // Suspended Appeal State
  const [appealText, setAppealText] = useState('');
  const [appealSent, setAppealSent] = useState(false);

  useEffect(() => {
    if (step !== 'otp' || countdown <= 0) {
      if (step === 'otp' && countdown === 0) {
        setIsOtpExpired(true);
      }
      return;
    }
    const timer = setInterval(() => {
      setCountdown((prev) => Math.max(0, prev - 1));
    }, 1000);
    return () => clearInterval(timer);
  }, [step, countdown]);

  const formatTimer = (sec: number) => {
    const m = Math.floor(sec / 60);
    const s = sec % 60;
    return `${toPersianDigits(m.toString().padStart(2, '0'))}:${toPersianDigits(
      s.toString().padStart(2, '0')
    )}`;
  };

  const handleSendOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isValidIranianMobile(mobile)) {
      error('لطفاً شماره موبایل معتبر ۱۱ رقمی ایران (با پیش‌شماره ۰۹) وارد نمایید.');
      return;
    }
    const cleanMobile = mobile.trim();
    if (!isMockMode()) {
      try {
        const res = await authApi.sendOTP(cleanMobile);
        setStep('otp');
        setCountdown(res.expiresIn || 120);
        setIsOtpExpired(false);
        setOtpError(null);
        info(`کد یکبارمصرف به شماره ${toPersianDigits(cleanMobile)} پیامک شد.`);
      } catch (err: any) {
        error(err.message || 'خطا در ارسال کد تایید');
      }
    } else {
      setStep('otp');
      setCountdown(115);
      setIsOtpExpired(false);
      setOtpError(null);
      setOtpCode('1234');
      info(`کد آزمایشی ۴ رقمی (۱۲۳۴) است؛ در حالت نمایشی پیامکی ارسال نمی‌شود.`);
    }
  };

  const handleResendOtp = async () => {
    const cleanMobile = mobile.trim();
    if (!isMockMode()) {
      try {
        const res = await authApi.sendOTP(cleanMobile);
        setCountdown(res.expiresIn || 120);
        setIsOtpExpired(false);
        setOtpError(null);
        success(`کد تایید جدید مجدداً به شماره ${toPersianDigits(cleanMobile)} ارسال گردید.`);
      } catch (err: any) {
        error(err.message || 'خطا در ارسال مجدد کد');
      }
    } else {
      setCountdown(120);
      setIsOtpExpired(false);
      setOtpError(null);
      setOtpCode('1234');
      success(`کد آزمایشی همان ۱۲۳۴ است؛ در حالت نمایشی پیامکی ارسال نمی‌شود.`);
    }
  };

  const handleVerifyOtp = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!otpCode.trim()) {
      setOtpError('لطفاً کد یکبارمصرف را وارد کنید.');
      error('لطفاً کد یکبارمصرف را وارد کنید.');
      return;
    }
    if (otpCode.trim().length < 4) {
      setOtpError('کد یکبارمصرف باید ۴ رقم باشد.');
      error('کد یکبارمصرف باید ۴ رقم باشد.');
      return;
    }
    if (isOtpExpired || countdown === 0) {
      setOtpError('کد یکبارمصرف منقضی شده است. لطفاً دکمه «ارسال مجدد کد» را بزنید.');
      error('کد وارد شده منقضی شده است.');
      return;
    }

    const cleanMobile = mobile.trim();
    let token = '';
    let userFullName = '';

    if (!isMockMode()) {
      let requestIsCurrent = () => true;
      try {
        // Seller capability is granted only after the store onboarding flow
        // succeeds. OTP authenticates the existing normalized identity.
        const res = await authenticate(() => authApi.verifyOTP(cleanMobile, otpCode.trim()));
        requestIsCurrent = res.isCurrent;
        if (!requestIsCurrent()) return;
        token = res.token;
        userFullName = res.user?.fullName || '';
        const owned = await storeApi.getOwnedStores();
        if (!requestIsCurrent()) return;
        const selected = owned[0];
        if (selected) { addStore(selected); setCurrentStore(selected); }
        setSellerSession({ token, mobile: cleanMobile, fullName: userFullName, status: selected ? 'active' : 'onboarding', storeId: selected?.id, loginAt: new Date().toISOString() });
        return;
      } catch (err: any) {
        if (err instanceof AuthRequestSupersededError || !requestIsCurrent()) return;
        setOtpError(err.message || 'کد تایید واردشده صحیح نیست.');
        error(err.message || 'کد یکبارمصرف نامعتبر است.');
        return;
      }
    } else {
      if (otpCode.trim() !== '1234') {
        setOtpError('کد تایید واردشده صحیح نیست. (کد صحیح آزمایشی: ۱۲۳۴)');
        error('کد یکبارمصرف نامعتبر است.');
        return;
      }
      token = 'mock-seller';
      userFullName = 'فروشنده نمایشی';
    }

    if (!token || !userFullName) {
      setOtpError('اطلاعات حساب فروشنده از سرور دریافت نشد.');
      return;
    }

    // Mock-only preset scenarios
    if (isMockMode() && cleanMobile === '09129998877') {
      setSellerSession({
        token,
        mobile: cleanMobile,
        fullName: 'کامران فرهادی (ویترین تعلیق‌شده)',
        status: 'suspended',
        storeId: 'store-techhub',
        suspensionReason:
          'عدم رعایت سقف قیمت رقابتی مصوب و ثبت تبلیغات خارج از ضوابط شبکه (بند ۶ و ۱۸ پروپوزال).',
        loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
      });
      warning('حساب شما دارای محدودیت تعلیق ویترین است.');
      return;
    }

    if (isMockMode() && cleanMobile === '09120000000') {
      setSellerSession({
        token,
        mobile: cleanMobile,
        fullName: 'فروشنده جدید سلرشو',
        status: 'onboarding',
        loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
      });
      info('خوش آمدید! لطفاً مشخصات اولیه ویترین اختصاصی خود را تکمیل فرمایید.');
      return;
    }

    // Default active seller (Bibi Style)
    const bibiStore = stores.find((s) => s.id === 'store-bibi' || s.slug === 'bibi') || stores[1] || stores[0];
    const currentAdminStatus = (bibiStore && storeStatuses[bibiStore.id]) || 'active';
    if (bibiStore) {
      setCurrentStore(bibiStore);
    }

    setSellerSession({
      token,
      mobile: cleanMobile,
      fullName: userFullName,
      status: currentAdminStatus === 'suspended' ? 'suspended' : 'active',
      storeId: bibiStore ? bibiStore.id : 'store-bibi',
      suspensionReason:
        currentAdminStatus === 'suspended'
          ? 'این ویترین توسط مدیریت ارشد پلتفرم به حالت تعلیق موقت درآمده است.'
          : undefined,
      loginAt: new Date().toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }),
    });
    success('ورود به پنل مدیریت دراپ‌شیپر با موفقیت انجام شد.');
  };

  const handleCompleteOnboarding = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newStoreName.trim()) {
      error('لطفاً نام فارسی ویترین خود را وارد کنید.');
      return;
    }
    const slugValidation = validateStoreSlug(newStoreSlug);
    if (!slugValidation.valid) {
      error(slugValidation.error || 'نامک (Slug) انگلیسی معتبر نیست.');
      return;
    }

    let createdStore: any = {
      id: `store-${Date.now()}`,
      name: newStoreName.trim(),
      slug: newStoreSlug.trim().toLowerCase(),
      ownerType: 'dropshipper' as const,
      ownerName: sellerSession?.fullName || 'دراپ‌شیپر جدید',
      tagline: newStoreTagline.trim() || 'ویترین رسمی منتخب کالاها',
      profitMarginPercent: 0,
      description: 'ویترین اختصاصی راه‌اندازی‌شده در شبکه سلرشو با ارسال مستقیم از انبار مرکزی.',
      activeCategories: ['digital', 'fashion', 'beauty', 'gold'],
      totalSales: 0,
      monthlyRevenueEstimate: 18500000,
    };

    if (!isMockMode()) {
      try {
        const backendStore = await storeApi.registerStore({
          storeName: newStoreName.trim(),
          slug: newStoreSlug.trim().toLowerCase(),
          ownerName: sellerSession?.fullName || 'فروشنده جدید',
        });
        createdStore = {
          ...createdStore,
          ...backendStore,
        };
      } catch (err: any) {
        error(err.message || 'خطا در ثبت ویترین در سرور');
        return;
      }
    }

    addStore(createdStore);
    setCurrentStore(createdStore);
    if (sellerSession) {
      setSellerSession({
        ...sellerSession,
        status: 'active',
        storeId: createdStore.id,
      });
    }
    success(`ویترین «${createdStore.name}» با موفقیت ساخته شد و داشبورد شما فعال گردید!`);
  };

  // ============================================================================
  // CASE 1: NOT LOGGED IN -> RENDER DEDICATED MOBILE + OTP LOGIN PAGE
  // ============================================================================
  if (!sellerSession || !sellerSession.token) {
    return (
      <div className="flex-1 flex flex-col bg-[#f8fafc] select-none min-h-[82vh]">
        <header className="sticky top-0 z-30 bg-white border-b border-slate-200/80 px-3.5 py-2.5 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <button
              onClick={onBackToCustomerSite}
              className="p-1.5 rounded-xl hover:bg-slate-100 text-slate-600 transition-colors"
            >
              <ArrowRight className="w-5 h-5"  strokeWidth={1.5}/>
            </button>
            <div>
              <h1 className="text-xs font-bold text-slate-800">ورود به پنل همکاری دراپ‌شیپر</h1>
              <span className="text-[10px] text-slate-400 block">
                احراز هویت با شماره همراه و رمز یکبارمصرف (OTP)
              </span>
            </div>
          </div>
        </header>

        <div className="p-4 flex-1 flex flex-col justify-center space-y-4">
          <div className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-sm space-y-4">
            <div className="flex items-center gap-2.5">
              <div className="w-10 h-10 rounded-2xl bg-brand-deep text-white flex items-center justify-center shadow-sm">
                <Store className="w-5 h-5"  strokeWidth={1.5}/>
              </div>
              <div>
                <h2 className="text-sm font-extrabold text-slate-900">
                  {step === 'mobile' ? 'ورود / ثبت‌نام فروشندگان (سلرشو)' : 'تأیید کد ۴ رقمی پیامک‌شده'}
                </h2>
                <p className="text-[11px] text-slate-500">
                  {step === 'mobile'
                    ? 'شماره موبایل خود را جهت دریافت کد یکبارمصرف وارد کنید'
                    : `کد ارسال‌شده به شماره ${toPersianDigits(mobile)} را وارد نمایید`}
                </p>
              </div>
            </div>

            {step === 'mobile' ? (
              <>
                {/* Quick Demo Scenario Switcher for Employer */}
                {isMockMode() && <div className="space-y-1.5 bg-slate-50 p-3 rounded-2xl border border-slate-200/80">
                  <span className="text-[10px] font-bold text-slate-500 block">
                    انتخاب سناریوی آزمایشی (جهت تست Session Guard):
                  </span>
                  <div className="grid grid-cols-1 gap-1.5">
                    <button
                      type="button"
                      onClick={() => setMobile('09121112233')}
                      className={`p-2 rounded-xl border text-right text-[11px] flex items-center justify-between ${
                        mobile === '09121112233'
                          ? 'bg-blue-50 border-blue-500 text-blue-900 font-bold'
                          : 'bg-white border-slate-200 text-slate-600'
                      }`}
                    >
                      <span>۱. سلر فعال (فروشگاه بی‌بی استایل)</span>
                      <span className="font-mono text-[10px]" dir="ltr">09121112233</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setMobile('09120000000')}
                      className={`p-2 rounded-xl border text-right text-[11px] flex items-center justify-between ${
                        mobile === '09120000000'
                          ? 'bg-emerald-50 border-emerald-500 text-emerald-900 font-bold'
                          : 'bg-white border-slate-200 text-slate-600'
                      }`}
                    >
                      <span>۲. کاربر تازه‌وارد (هدایت به راه‌اندازی ویترین)</span>
                      <span className="font-mono text-[10px]" dir="ltr">09120000000</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => setMobile('09129998877')}
                      className={`p-2 rounded-xl border text-right text-[11px] flex items-center justify-between ${
                        mobile === '09129998877'
                          ? 'bg-rose-50 border-rose-500 text-rose-900 font-bold'
                          : 'bg-white border-slate-200 text-slate-600'
                      }`}
                    >
                      <span>۳. سلر تعلیق‌شده (تست Guard تعلیق حساب)</span>
                      <span className="font-mono text-[10px]" dir="ltr">09129998877</span>
                    </button>
                  </div>
                </div>}

                <form onSubmit={handleSendOtp} className="space-y-3.5">
                  <div>
                    <label className="text-xs font-bold text-slate-700 block mb-1.5">
                      شماره موبایل (ایران)
                    </label>
                    <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-2xl px-3.5 py-2.5 focus-within:border-brand focus-within:bg-white">
                      <Smartphone className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
                      <input
                        type="tel"
                        value={mobile}
                        onChange={(e) => setMobile(e.target.value)}
                        placeholder="۰۹۱۲۳۴۵۶۷۸۹"
                        maxLength={11}
                        className="w-full bg-transparent text-xs font-mono font-bold text-slate-800 focus:outline-hidden text-left"
                        dir="ltr"
                        required
                      />
                    </div>
                    {mobile && !isValidIranianMobile(mobile) && (
                      <span className="text-[10px] text-rose-500 mt-1 block">
                        شماره موبایل باید ۱۱ رقم و با ۰۹ آغاز شود.
                      </span>
                    )}
                  </div>

                  <button
                    type="submit"
                    className="w-full py-3 bg-brand hover:bg-brand-deep text-white font-extrabold text-xs rounded-2xl shadow-xs transition-all"
                  >
                    دریافت کد یکبارمصرف (OTP)
                  </button>
                </form>
              </>
            ) : (
              <form onSubmit={handleVerifyOtp} className="space-y-4">
                {otpError && (
                  <div className="p-3 rounded-2xl bg-rose-50 border border-rose-200 text-rose-700 text-[11px] flex items-center gap-2">
                    <AlertTriangle className="w-4 h-4 text-rose-600 shrink-0"  strokeWidth={1.5}/>
                    <span>{otpError}</span>
                  </div>
                )}

                <div>
                  <div className="flex items-center justify-between mb-1.5">
                    <label className="text-xs font-bold text-slate-700">
                      کد ۴ رقمی پیامک‌شده
                    </label>
                    <div className="flex items-center gap-2 text-[10px]">
                      <button
                        type="button"
                        onClick={() => {
                          setOtpCode('9999');
                          setOtpError(null);
                        }}
                        className="text-amber-600 hover:underline font-bold"
                      >
                        تست کد خطا
                      </button>
                      <span>•</span>
                      <button
                        type="button"
                        onClick={() => {
                          setCountdown(0);
                          setIsOtpExpired(true);
                          setOtpError('کد یکبارمصرف منقضی شده است. لطفاً ارسال مجدد را بزنید.');
                        }}
                        className="text-rose-600 hover:underline font-bold"
                      >
                        تست انقضای تایمر
                      </button>
                    </div>
                  </div>

                  <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-2xl px-3.5 py-2.5 focus-within:border-brand focus-within:bg-white">
                    <KeyRound className="w-4 h-4 text-slate-400 shrink-0 ml-2"  strokeWidth={1.5}/>
                    <input
                      type="text"
                      value={otpCode}
                      onChange={(e) => setOtpCode(e.target.value.replace(/[۰-۹]/g, (d) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(d))).replace(/\D/g, '').slice(0, 4))}
                      placeholder="1234"
                      maxLength={4}
                      className="w-full bg-transparent text-sm font-mono font-black tracking-[0.4em] text-center text-slate-900 focus:outline-hidden"
                      dir="ltr"
                      required
                    />
                  </div>
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
                      className="text-blue-600 font-bold flex items-center gap-1 hover:underline"
                    >
                      <RefreshCw className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>ارسال مجدد کد یکبارمصرف</span>
                    </button>
                  )}

                  <button
                    type="button"
                    onClick={() => setStep('mobile')}
                    className="text-slate-500 hover:text-slate-800 font-medium"
                  >
                    ویرایش شماره
                  </button>
                </div>

                <button
                  type="submit"
                  className="w-full py-3 bg-brand hover:bg-brand-deep text-white font-extrabold text-xs rounded-2xl shadow-xs transition-all"
                >
                  تأیید کد و ورود به حساب دراپشیپر
                </button>
              </form>
            )}
          </div>
        </div>
      </div>
    );
  }

  // ============================================================================
  // CASE 2: SUSPENDED STORE GUARD -> INTERCEPT BEFORE DASHBOARD
  // ============================================================================
  const linkedStoreId = sellerSession.storeId;
  const isSuspended =
    sellerSession.status === 'suspended' ||
    (linkedStoreId && storeStatuses[linkedStoreId] === 'suspended');

  if (isSuspended) {
    return (
      <div className="flex-1 flex flex-col bg-[#f8fafc] select-none min-h-[82vh]">
        <header className="sticky top-0 z-30 bg-white border-b border-slate-200 px-3.5 py-2.5 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <ShieldAlert className="w-5 h-5 text-rose-600"  strokeWidth={1.5}/>
            <h1 className="text-xs font-extrabold text-slate-900">وضعیت حساب: تعلیق موقت ویترین</h1>
          </div>
          <button
            onClick={logoutSeller}
            className="text-[11px] font-bold text-rose-600 bg-rose-50 px-2.5 py-1 rounded-xl flex items-center gap-1"
          >
            <LogOut className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>خروج از حساب</span>
          </button>
        </header>

        <div className="p-4 space-y-4">
          <div className="bg-white rounded-3xl p-5 border border-rose-200 shadow-2xs space-y-3">
            <div className="w-12 h-12 rounded-2xl bg-rose-50 text-rose-600 flex items-center justify-center">
              <ShieldAlert className="w-6 h-6"  strokeWidth={1.5}/>
            </div>
            <h2 className="text-sm font-extrabold text-slate-900">
              دسترسی به داشبورد دراپ‌شیپر به صورت موقت محدود شده است
            </h2>
            <p className="text-xs text-slate-600 leading-relaxed">
              کاربر گرامی <strong>{sellerSession.fullName}</strong>، ویترین شما به علت زیر در وضعیت تعلیق قرار دارد:
            </p>
            <div className="p-3 rounded-2xl bg-rose-50/70 border border-rose-200/80 text-xs text-rose-800 font-medium leading-relaxed">
              {sellerSession.suspensionReason ||
                'تعلیق موقت توسط واحد نظارت و بازرسی پلتفرم سلرشو.'}
            </div>
            <p className="text-[11px] text-slate-500 leading-relaxed">
              توجه: موجودی قطعی کیف پول شما محفوظ است و سفارش‌های قبلی ثبت‌شده در انبار مرکزی بدون توقف پردازش و ارسال می‌شوند.
            </p>
          </div>

          <div className="bg-white rounded-3xl p-4 border border-slate-200/90 shadow-2xs space-y-3">
            <h3 className="text-xs font-bold text-slate-800">
              ثبت درخواست بررسی مجدد و رفع تعلیق
            </h3>
            {appealSent ? (
              <div className="p-3 rounded-2xl bg-emerald-50 border border-emerald-200 text-emerald-700 text-xs flex items-center gap-2 font-bold">
                <CheckCircle2 className="w-4 h-4 shrink-0"  strokeWidth={1.5}/>
                <span>درخواست رفع تعلیق شما در صف واحد نظارت مدیریت ثبت شد.</span>
              </div>
            ) : (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setAppealSent(true);
                  success('درخواست رفع تعلیق برای مدیر ارشد پلتفرم ارسال گردید.');
                }}
                className="space-y-2.5"
              >
                <textarea
                  rows={3}
                  value={appealText}
                  onChange={(e) => setAppealText(e.target.value)}
                  placeholder="توضیحات خود را جهت تعهد به ضوابط قیمت‌گذاری و رفع مسدودی وارد نمایید..."
                  className="w-full text-xs p-3 rounded-2xl bg-slate-50 border border-slate-200 focus:outline-hidden"
                  required
                />
                <button
                  type="submit"
                  className="w-full py-2.5 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl flex items-center justify-center gap-1.5"
                >
                  <Send className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                  <span>ارسال تعهدنامه و درخواست بازگشایی ویترین</span>
                </button>
              </form>
            )}
          </div>
        </div>
      </div>
    );
  }

  // ============================================================================
  // CASE 3: NEW SELLER ONBOARDING GUARD -> DIRECT TO STORE CREATION WIZARD
  // ============================================================================
  if (sellerSession.status === 'onboarding') {
    return (
      <div className="flex-1 flex flex-col bg-[#f8fafc] select-none min-h-[82vh]">
        <header className="sticky top-0 z-30 bg-white border-b border-slate-200 px-3.5 py-2.5 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <UserPlus className="w-4 h-4 text-blue-600"  strokeWidth={1.5}/>
            <div>
              <h1 className="text-xs font-extrabold text-slate-900">راه‌اندازی اولیه ویترین اختصاصی</h1>
              <span className="text-[10px] text-slate-400 block">
                تکمیل مشخصات پایه قبل از ورود به داشبورد کامل سلر
              </span>
            </div>
          </div>
          <button
            onClick={logoutSeller}
            className="text-[11px] font-bold text-slate-500 hover:text-rose-600 px-2 py-1 rounded-lg"
          >
            خروج
          </button>
        </header>

        <div className="p-4">
          <form
            onSubmit={handleCompleteOnboarding}
            className="bg-white rounded-3xl p-5 border border-slate-200/90 shadow-2xs space-y-4"
          >
            <div className="p-3 rounded-2xl bg-blue-50/70 border border-blue-100 flex items-start gap-2.5">
              <Sparkles className="w-5 h-5 text-blue-600 shrink-0 mt-0.5"  strokeWidth={1.5}/>
              <div className="text-[11px] text-blue-900 leading-relaxed">
                شماره همراه <strong>{toPersianDigits(sellerSession.mobile)}</strong> با موفقیت تأیید شد. برای فعال‌سازی داشبورد، نام و آدرس ویترین خود را انتخاب کنید:
              </div>
            </div>

            <div>
              <label className="text-xs font-bold text-slate-700 block mb-1">
                نام برند یا ویترین فروشگاهی شما *
              </label>
              <input
                type="text"
                value={newStoreName}
                onChange={(e) => setNewStoreName(e.target.value)}
                placeholder="مثال: گالری مدرن آریا"
                className="w-full text-xs p-3 rounded-2xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-blue-500 outline-hidden"
                required
              />
            </div>

            <div>
              <label className="text-xs font-bold text-slate-700 block mb-1">
                نامک انگلیسی ویترین (Slug) *
              </label>
              <div className="flex items-center rounded-2xl bg-slate-50 border border-slate-200 px-3" dir="ltr">
                <span className="text-[11px] text-slate-400 font-mono">sellershow.ir/store/</span>
                <input
                  type="text"
                  value={newStoreSlug}
                  onChange={(e) =>
                    setNewStoreSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))
                  }
                  placeholder="arya-shop"
                  className="w-full text-xs p-3 bg-transparent font-mono focus:outline-hidden"
                  required
                />
              </div>
            </div>

            <div>
              <label className="text-xs font-bold text-slate-700 block mb-1">
                شعار کوتاه ویترین
              </label>
              <input
                type="text"
                value={newStoreTagline}
                onChange={(e) => setNewStoreTagline(e.target.value)}
                className="w-full text-xs p-3 rounded-2xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-blue-500 outline-hidden"
              />
            </div>

            <button
              type="submit"
              className="w-full py-3 bg-[#007dfa] hover:bg-blue-600 text-white font-extrabold text-xs rounded-2xl shadow-xs flex items-center justify-center gap-1.5"
            >
              <Check className="w-4 h-4"  strokeWidth={1.5}/>
              <span>ایجاد ویترین و ورود به داشبورد دراپ‌شیپر</span>
            </button>
          </form>
        </div>
      </div>
    );
  }

  // ============================================================================
  // CASE 4: AUTHENTICATED & ACTIVE -> RENDER FULL SELLER DASHBOARD
  // ============================================================================
  return <>{children}</>;
};
