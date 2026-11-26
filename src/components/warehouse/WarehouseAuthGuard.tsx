import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { ArrowRight, Warehouse } from 'lucide-react';
import { AuthRequestSupersededError, useAuth, warehouseRoleFromCapabilities } from '../../context/AuthContext';
import { isMockMode } from '../../services/apiClient';
import { authApi } from '../../services/authApi';
import { isValidIranianMobile, normalizeIranianMobile } from '../../utils/validators';
import { toLatinDigits } from '../ProvinceCityPicker';

const noWarehouseAccess = 'این شماره مجوز ورود به پنل انبار را ندارد.';
const inputClass = 'min-h-12 w-full rounded-xl border border-slate-300 bg-white px-3 text-base text-slate-900 focus-visible:outline-2 focus-visible:outline-brand disabled:bg-slate-50';
const submitClass = 'min-h-12 w-full rounded-xl bg-brand-deep px-4 text-sm font-extrabold text-white hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50';
const digits = new Intl.NumberFormat('fa-IR', { minimumIntegerDigits: 2, useGrouping: false });

export function WarehouseAuthGuard({ onBackToCustomerSite, children }: {
  onBackToCustomerSite: () => void;
  children: ReactNode;
}) {
  const { warehouseSession, currentUser, authToken, isSessionReady, authenticate } = useAuth();
  const role = warehouseRoleFromCapabilities(currentUser?.capabilities);
  const [mobile, setMobile] = useState('');
  const [verifiedMobile, setVerifiedMobile] = useState('');
  const [otp, setOtp] = useState('');
  const [countdown, setCountdown] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!verifiedMobile || countdown <= 0) return;
    const timer = window.setTimeout(() => setCountdown(value => value - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [verifiedMobile, countdown]);

  if (warehouseSession?.token && (isMockMode() ||
    (isSessionReady && role && warehouseSession.staffId === currentUser?.id && warehouseSession.token === authToken))) {
    return <>{children}</>;
  }
  if (!isSessionReady || (role && authToken)) {
    return <p role="status" className="p-6 text-center text-sm text-slate-700">در حال بررسی دسترسی انبار…</p>;
  }

  async function sendCode() {
    if (busy) return;
    const clean = normalizeIranianMobile(toLatinDigits(mobile));
    if (!isValidIranianMobile(clean)) {
      setError('شمارهٔ موبایل معتبر وارد کنید.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const response = await authApi.sendOTP(clean);
      setVerifiedMobile(clean);
      setOtp('');
      setCountdown(response.expiresIn || 120);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'ارسال کد انجام نشد. دوباره تلاش کنید.');
    } finally {
      setBusy(false);
    }
  }

  async function verifyCode(event: FormEvent) {
    event.preventDefault();
    if (busy || !verifiedMobile) return;
    if (countdown <= 0) {
      setError('اعتبار کد تمام شده است. کد تازه بگیرید.');
      return;
    }
    if (!/^\d{4}$/.test(otp)) {
      setError('کد چهاررقمی را کامل وارد کنید.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await authenticate(async () => {
        const response = await authApi.verifyOTP(verifiedMobile, otp);
        if (!warehouseRoleFromCapabilities(response.user.capabilities)) throw new Error(noWarehouseAccess);
        return response;
      });
    } catch (cause) {
      if (cause instanceof AuthRequestSupersededError) return;
      setError(cause instanceof Error ? cause.message : 'تأیید کد انجام نشد. دوباره تلاش کنید.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div dir="rtl" className="flex min-h-dvh flex-1 items-start justify-center bg-[#f7f7fa] px-3 py-6 text-slate-900 sm:items-center sm:p-8">
      <div className="w-full max-w-md space-y-4">
        <button type="button" onClick={onBackToCustomerSite}
          className="inline-flex min-h-11 items-center gap-2 rounded-xl px-2 text-sm font-bold text-slate-700 hover:bg-brand-soft focus-visible:outline-2 focus-visible:outline-brand">
          <ArrowRight className="size-4" aria-hidden="true" /> سایت اصلی
        </button>
        <section aria-labelledby="warehouse-sign-in-title" className="space-y-5 rounded-[22px] border border-slate-200 bg-white p-4 shadow-sm sm:p-6">
          <div className="flex items-center gap-3">
            <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand-deep"><Warehouse className="size-5" aria-hidden="true" /></span>
            <h1 id="warehouse-sign-in-title" className="text-base font-extrabold">ورود به سامانه مدیریت انبار</h1>
          </div>
          {currentUser && !verifiedMobile && (
            <p role="status" className="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-900">حساب فعلی مجوز پنل انبار را ندارد. می‌توانید با شمارهٔ دیگری وارد شوید.</p>
          )}
          {verifiedMobile ? (
            <form onSubmit={verifyCode} className="space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm text-slate-700">کد برای <bdi>{verifiedMobile}</bdi> ارسال شد.</p>
                <button type="button" disabled={busy} onClick={() => { setVerifiedMobile(''); setOtp(''); setError(''); }}
                  className="min-h-11 rounded-lg px-2 text-sm font-bold text-brand-deep underline underline-offset-4">ویرایش شماره</button>
              </div>
              <label htmlFor="warehouse-otp" className="block text-sm font-bold">کد چهاررقمی</label>
              <input id="warehouse-otp" type="text" inputMode="numeric" autoComplete="one-time-code" value={otp} autoFocus
                onChange={event => setOtp(toLatinDigits(event.target.value).replace(/\D/g, '').slice(0, 4))}
                className={inputClass} dir="ltr" required disabled={busy} />
              {countdown > 0 ? (
                <p className="text-xs text-slate-600">اعتبار کد <bdi>{digits.format(Math.floor(countdown / 60))}:{digits.format(countdown % 60)}</bdi></p>
              ) : (
                <button type="button" onClick={() => void sendCode()} disabled={busy}
                  className="min-h-11 rounded-lg px-2 text-sm font-bold text-brand-deep underline underline-offset-4">ارسال دوبارهٔ کد</button>
              )}
              {error && <p role="alert" className="rounded-lg bg-rose-50 p-3 text-sm text-rose-800">{error}</p>}
              <button type="submit" disabled={busy || countdown <= 0} className={submitClass}>{busy ? 'در حال بررسی…' : 'تأیید و ورود به پنل انبار'}</button>
            </form>
          ) : (
            <form onSubmit={event => { event.preventDefault(); void sendCode(); }} className="space-y-3">
              <label htmlFor="warehouse-mobile" className="block text-sm font-bold">شمارهٔ موبایل ثبت‌شده برای انبار</label>
              <input type="tel" id="warehouse-mobile" autoComplete="tel-national" inputMode="tel" value={mobile}
                onChange={event => setMobile(event.target.value)} placeholder="09xxxxxxxxx"
                className={inputClass} dir="ltr" required disabled={busy} />
              {error && <p role="alert" className="rounded-lg bg-rose-50 p-3 text-sm text-rose-800">{error}</p>}
              <button type="submit" disabled={busy} className={submitClass}>{busy ? 'در حال ارسال…' : 'دریافت کد تأیید'}</button>
            </form>
          )}
        </section>
      </div>
    </div>
  );
}
