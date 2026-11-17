import React, { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { adminApi } from '../../services/adminApi';
import type { StorageFeeRunSummaryDto } from '../../services/adminApi';

// Storage-fee settings (gap 7): super-admin UI for the storage-fee
// system. It calls GET/PUT /admin/settings/storage-fees (admin role)
// and POST /admin/storage-fees/run for a manual assessment pass.
//
// Honesty notes shown in the UI itself:
// - The feature ships OFF by default; activation is a management
//   decision. While off, the run is refused and nothing is charged.
// - Tariffs: Toman per kg per day + Toman per m3 per day, applied to
//   both components; the first «free days» of storage are not charged.
// - Charges land on the supplier's settlement balance as a deduction
//   (visible to the supplier in the settlement tab) once assessed.
export const AdminStorageFeeSettingsTab: React.FC = () => {
  const [enabled, setEnabled] = useState(false);
  const [freeDays, setFreeDays] = useState('30');
  const [perKgDay, setPerKgDay] = useState('2000');
  const [perM3Day, setPerM3Day] = useState('8000');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [runBusy, setRunBusy] = useState(false);
  const [runSummary, setRunSummary] = useState<StorageFeeRunSummaryDto | null>(null);

  const load = async () => {
    try {
      setError('');
      const s = await adminApi.getStorageFeeSettings();
      setEnabled(!!s.storageFeeEnabled);
      setFreeDays(String(s.storageFeeFreeDays ?? 30));
      setPerKgDay(String(s.storageFeePerKgDay ?? 2000));
      setPerM3Day(String(s.storageFeePerM3Day ?? 8000));
    } catch (e: any) {
      setError(e?.message || 'دریافت تنظیمات هزینه انبارداری انجام نشد.');
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const parseField = (raw: string): number | null => {
    const v = raw.trim();
    if (v === '') return null;
    const n = Number(v);
    return Number.isSafeInteger(n) ? n : null;
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    setError('');
    setNotice('');
    const free = parseField(freeDays);
    const kg = parseField(perKgDay);
    const m3 = parseField(perM3Day);
    if (free === null || free < 0 || free > 365) {
      setError('روزهای رایگان باید عدد صحیح بین ۰ تا ۳۶۵ باشد.');
      return;
    }
    if (kg === null || kg < 0) {
      setError('تعرفه هر کیلوگرم در روز باید عدد صحیح نامنفی (تومان) باشد.');
      return;
    }
    if (m3 === null || m3 < 0) {
      setError('تعرفه هر مترمکعب در روز باید عدد صحیح نامنفی (تومان) باشد.');
      return;
    }
    setBusy(true);
    try {
      await adminApi.saveStorageFeeSettings({
        storageFeeEnabled: enabled,
        storageFeeFreeDays: free,
        storageFeePerKgDay: kg,
        storageFeePerM3Day: m3,
      });
      setNotice('تنظیمات هزینه انبارداری ذخیره شد.');
      await load();
    } catch (e: any) {
      setError(e?.message || 'ذخیره تنظیمات ناموفق بود.');
    } finally {
      setBusy(false);
    }
  };

  const runNow = async () => {
    setError('');
    setNotice('');
    setRunSummary(null);
    setRunBusy(true);
    try {
      const summary = await adminApi.runStorageFees();
      setRunSummary(summary);
      setNotice(summary.moreWork ? 'بخشی از محاسبه انجام شد؛ برای تکمیل روزهای باقی‌مانده دوباره اجرا کنید.' : 'محاسبه هزینه انبارداری اجرا شد.');
    } catch (e: any) {
      setError(e?.message || 'اجرای محاسبه هزینه انبارداری ناموفق بود.');
    } finally {
      setRunBusy(false);
    }
  };

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-extrabold">هزینه انبارداری</h2>
          <p className="text-xs text-slate-500">تعرفه نگهداری کالای تأمین‌کنندگان در انبار مرکزی و اجرای محاسبه</p>
        </div>
        <button onClick={() => void load()} className="p-2 rounded-lg border" title="به‌روزرسانی">
          <RefreshCw className="w-4 h-4" strokeWidth={1.5} />
        </button>
      </div>
      {!enabled && (
        <div role="status" className="rounded-lg bg-amber-50 p-3 text-xs text-amber-800">
          هزینه انبارداری در حال حاضر خاموش است؛ پیش‌فرض خاموش است و فعال‌سازی آن با تصمیم مدیریت انجام می‌شود. تا وقتی خاموش است هیچ هزینه‌ای از تأمین‌کنندگان کسر نمی‌شود و اجرای محاسبه هم پذیرفته نمی‌شود.
        </div>
      )}
      {notice && <div role="status" className="rounded-lg bg-emerald-50 p-3 text-xs text-emerald-700">{notice}</div>}
      <form onSubmit={save} className="space-y-3 rounded-xl border bg-white p-4">
        <label className="flex items-center justify-between gap-3 text-xs font-semibold">
          فعال‌بودن هزینه انبارداری
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
            className="h-4 w-4 accent-teal-700"
          />
        </label>
        <label className="block text-xs font-semibold">
          روزهای رایگان نگهداری (۰ تا ۳۶۵)
          <input
            value={freeDays}
            onChange={(e) => setFreeDays(e.target.value)}
            inputMode="numeric"
            placeholder="مثلاً ۳۰"
            className="mt-1 w-full rounded-lg border p-2 text-xs"
            dir="ltr"
          />
        </label>
        <label className="block text-xs font-semibold">
          تعرفه هر کیلوگرم در روز (تومان؛ نامنفی)
          <input
            value={perKgDay}
            onChange={(e) => setPerKgDay(e.target.value)}
            inputMode="numeric"
            placeholder="مثلاً ۲۰۰۰"
            className="mt-1 w-full rounded-lg border p-2 text-xs"
            dir="ltr"
          />
        </label>
        <label className="block text-xs font-semibold">
          تعرفه هر مترمکعب در روز (تومان؛ نامنفی)
          <input
            value={perM3Day}
            onChange={(e) => setPerM3Day(e.target.value)}
            inputMode="numeric"
            placeholder="مثلاً ۸۰۰۰"
            className="mt-1 w-full rounded-lg border p-2 text-xs"
            dir="ltr"
          />
        </label>
        <p className="text-xs text-slate-500">
          هزینه هر روز برای هر واحد کالا برابر است با: (وزن به کیلوگرم × تعرفه کیلوگرم) + (حجم به مترمکعب × تعرفه مترمکعب)، ضربدر تعداد واحدهای آن محموله. کالایی که وزن یا ابعاد ثبت‌شده نداشته باشد هزینه‌ای برایش محاسبه نمی‌شود.
        </p>
        <button disabled={busy} className="rounded-lg bg-teal-700 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">
          {busy ? 'در حال ذخیره...' : 'ذخیره تنظیمات'}
        </button>
      </form>
      <div className="space-y-3 rounded-xl border bg-white p-4">
        <h3 className="text-sm font-bold text-slate-800">اجرای دستی محاسبه</h3>
        <p className="text-xs text-slate-600">
          هزینهٔ هر محموله از تعداد موجود در ابتدای هر روز کامل، پس از پایان مهلت رایگان، محاسبه می‌شود. هزینهٔ ثبت‌نشدهٔ روزهای گذشته پس از خروج کالا هم حفظ می‌شود. فقط سابقه‌های تأییدشده محاسبه می‌شوند و اجرای دوباره مبلغ تکراری اضافه نمی‌کند.
        </p>
        <button onClick={() => void runNow()} disabled={runBusy} className="rounded-lg bg-slate-900 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">
          {runBusy ? 'در حال اجرا...' : 'اجرای محاسبه هزینه انبارداری'}
        </button>
        {runSummary && (
          <ul className="list-disc space-y-1 pr-4 text-xs text-slate-600">
            <li>محموله‌های مشمول هزینه در این اجرا: {runSummary.positionsCharged}</li>
            <li>ردیف هزینه ساخته‌شده: {runSummary.chargesCreated}</li>
            <li>محموله‌های نیازمند تأیید وزن/ابعاد: {runSummary.unresolvedSampleLimited ? 'حداقل ' : ''}{runSummary.positionsSkippedNoMeasurement}</li>
            {runSummary.verifiedReceiptHistoryOnly && <li>سابقه‌های قدیمی یا ناقص، هزینهٔ تازه ایجاد نکرده‌اند و به بررسی نیاز دارند.</li>}
            <li>مجموع مبلغ کسرشده در این اجرا: {runSummary.totalCharged} تومان</li>
          </ul>
        )}
      </div>
      {error && <div className="rounded-lg bg-rose-50 p-3 text-xs text-rose-700">{error}</div>}
    </section>
  );
};
