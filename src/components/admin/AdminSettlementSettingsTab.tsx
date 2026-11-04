import React, { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { adminApi } from '../../services/adminApi';

// Team Settlement (Phase 3): super-admin UI for the monthly-settlement
// settings. It calls the live-parity route GET/PUT
// /admin/settings/settlement (admin role; super_admin is accepted by
// RequireRoles), which is why this tab is gated to super_admin only
// in AdminDashboardView.
//
// Honesty notes shown in the UI itself:
// - The backend GET collapses an unset (NULL) value to 0, so a
//   settlementDay of 0 is displayed as "not set yet". While the day
//   is unset, the automatic sweep stays inert.
// - The sweep is free and creates payout_requests in status 'pending'
//   for finance/bank execution; this system performs no automatic
//   bank transfer.
// - Early withdrawal: minimum 1,000,000 تومان, fee from the setting
//   below, finance review within 24 hours. (Minimum and SLA are the
//   user-approved Phase 3 rules, not verified live behaviour.)
export const AdminSettlementSettingsTab: React.FC = () => {
  const [day, setDay] = useState('');
  const [fee, setFee] = useState('');
  const [dayUnset, setDayUnset] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);

  const load = async () => {
    try {
      setError('');
      const s = await adminApi.getSettlementSettings();
      if (typeof s.settlementDay === 'number' && s.settlementDay >= 1) {
        setDay(String(s.settlementDay));
        setDayUnset(false);
      } else {
        setDay('');
        setDayUnset(true);
      }
      if (typeof s.earlyWithdrawalFeePct === 'number') {
        setFee(String(s.earlyWithdrawalFeePct));
      } else {
        setFee('');
      }
    } catch (e: any) {
      setError(e?.message || 'دریافت تنظیمات تسویه انجام نشد.');
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    setError('');
    setNotice('');
    const dayNum = day.trim() === '' ? null : Number(day);
    const feeNum = fee.trim() === '' ? null : Number(fee);
    if (dayNum !== null && (!Number.isInteger(dayNum) || dayNum < 1 || dayNum > 31)) {
      setError('روز تسویه باید عدد صحیح بین ۱ تا ۳۱ باشد.');
      return;
    }
    if (feeNum !== null && (Number.isNaN(feeNum) || feeNum < 0 || feeNum >= 100)) {
      setError('کارمزد برداشت زودهنگام باید عددی بین ۰ تا کمتر از ۱۰۰ باشد.');
      return;
    }
    setBusy(true);
    try {
      await adminApi.saveSettlementSettings({ settlementDay: dayNum, earlyWithdrawalFeePct: feeNum });
      setNotice('تنظیمات تسویه ذخیره شد.');
      await load();
    } catch (e: any) {
      setError(e?.message || 'ذخیره تنظیمات ناموفق بود.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-extrabold">تنظیمات تسویه ماهانه</h2>
          <p className="text-xs text-slate-500">روز تسویه خودکار و کارمزد برداشت زودهنگام فروشندگان</p>
        </div>
        <button onClick={() => void load()} className="p-2 rounded-lg border" title="به‌روزرسانی">
          <RefreshCw className="w-4 h-4"  strokeWidth={1.5}/>
        </button>
      </div>
      {dayUnset && (
        <div role="status" className="rounded-lg bg-amber-50 p-3 text-xs text-amber-800">
          روز تسویه هنوز تنظیم نشده است؛ تا وقتی تنظیم نشود، تسویه خودکار ماهانه اجرا نمی‌شود و برداشت زودهنگام هم پذیرفته نمی‌شود.
        </div>
      )}
      {notice && <div role="status" className="rounded-lg bg-emerald-50 p-3 text-xs text-emerald-700">{notice}</div>}
      <form onSubmit={save} className="space-y-3 rounded-xl border bg-white p-4">
        <label className="block text-xs font-semibold">
          روز تسویه ماهانه (۱ تا ۳۱؛ خالی = تنظیم‌نشده)
          <input
            value={day}
            onChange={(e) => setDay(e.target.value)}
            inputMode="numeric"
            placeholder="مثلاً ۱۰"
            className="mt-1 w-full rounded-lg border p-2 text-xs"
            dir="ltr"
          />
        </label>
        <label className="block text-xs font-semibold">
          کارمزد برداشت زودهنگام (درصد؛ ۰ تا کمتر از ۱۰۰؛ خالی = تنظیم‌نشده)
          <input
            value={fee}
            onChange={(e) => setFee(e.target.value)}
            inputMode="decimal"
            placeholder="مثلاً ۵"
            className="mt-1 w-full rounded-lg border p-2 text-xs"
            dir="ltr"
          />
        </label>
        <p className="text-xs text-slate-500">
          توجه: سامانه مقدار تنظیم‌نشده را در خواندن، ۰ نشان می‌دهد؛ پس «۰» برای کارمزد می‌تواند به‌معنای تنظیم‌نشده هم باشد. پس از ذخیره، مقدار واقعی اعمال می‌شود.
        </p>
        <button disabled={busy} className="rounded-lg bg-teal-700 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">
          {busy ? 'در حال ذخیره...' : 'ذخیره تنظیمات'}
        </button>
      </form>
      <div className="space-y-2 rounded-xl border bg-white p-4 text-xs text-slate-600">
        <h3 className="text-sm font-bold text-slate-800">این تنظیمات چه می‌کنند؟</h3>
        <ul className="list-disc space-y-1 pr-4">
          <li>در روز تسویه، موجودی قابل‌برداشت هر فروشگاه به‌صورت خودکار و بدون کارمزد تسویه می‌شود.</li>
          <li>تسویه خودکار فقط یک درخواست پرداخت با وضعیت «در انتظار» برای اجرا توسط مالی/بانک می‌سازد؛ انتقال بانکی خودکار در سامانه انجام نمی‌شود.</li>
          <li>فروشگاهی که شبا ندارد، «بدون اطلاعات بانکی» رد می‌شود و موجودی‌اش برای چرخه بعد باقی می‌ماند.</li>
          <li>برداشت زودهنگام حداقل ۱٬۰۰۰٬۰۰۰ تومان است، با کارمزد تنظیم‌شده بالا، و باید حداکثر ظرف ۲۴ ساعت توسط مالی بررسی شود؛ تا وقتی در انتظار است، مبلغ آن از موجودی قابل‌برداشت قفل می‌شود.</li>
        </ul>
      </div>
      {error && <div className="rounded-lg bg-rose-50 p-3 text-xs text-rose-700">{error}</div>}
    </section>
  );
};
