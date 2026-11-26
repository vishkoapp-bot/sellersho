import React, { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { adminApi, type StorageFeeRunSummaryDto, type StorageFeeSettingsDto } from '../../services/adminApi';
import { formatPrice, toPersianDigits } from '../../utils/formatters';
import { normalizePersianText } from '../../utils/routing';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';

const storageFeeInput = 'mt-1 min-h-11 w-full rounded-lg border border-slate-300 px-2 text-base focus-visible:outline-2 focus-visible:outline-brand';

const parseField = (raw: string): number | null => {
  const value = normalizePersianText(raw.trim()).replace(/[,٬]/g, '');
  if (!/^\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : null;
};

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
  const [freeDays, setFreeDays] = useState('');
  const [perKgDay, setPerKgDay] = useState('');
  const [perM3Day, setPerM3Day] = useState('');
  const [settings, setSettings] = useState<StorageFeeSettingsDto>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [runBusy, setRunBusy] = useState(false);
  const [confirmRun, setConfirmRun] = useState(false);
  const [runSummary, setRunSummary] = useState<StorageFeeRunSummaryDto | null>(null);

  const load = async () => {
    setLoading(true);
    setLoadError('');
    setSettings(undefined);
    try {
      const s = await adminApi.getStorageFeeSettings();
      if (typeof s?.storageFeeEnabled !== 'boolean' ||
          ![s.storageFeeFreeDays, s.storageFeePerKgDay, s.storageFeePerM3Day].every(Number.isSafeInteger)) {
        throw new Error('پاسخ تنظیمات هزینه انبارداری کامل دریافت نشد.');
      }
      setSettings(s);
      setEnabled(s.storageFeeEnabled);
      setFreeDays(String(s.storageFeeFreeDays));
      setPerKgDay(String(s.storageFeePerKgDay));
      setPerM3Day(String(s.storageFeePerM3Day));
    } catch (cause: unknown) {
      setLoadError(cause instanceof Error ? cause.message : 'دریافت تنظیمات هزینه انبارداری انجام نشد.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const unavailable = loading || busy || runBusy || !settings;
  const hasChanges = !!settings && (
    enabled !== settings.storageFeeEnabled ||
    parseField(freeDays) !== settings.storageFeeFreeDays ||
    parseField(perKgDay) !== settings.storageFeePerKgDay ||
    parseField(perM3Day) !== settings.storageFeePerM3Day
  );
  const canRun = !unavailable && !hasChanges && settings?.storageFeeEnabled === true;

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    if (unavailable) return;
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
    if (!canRun || !confirmRun) return;
    setConfirmRun(false);
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
      <div className="flex items-center justify-between gap-2">
        <div>
          <h2 className="text-lg font-extrabold">هزینه انبارداری</h2>
          <p className="text-sm text-slate-600">تعرفه نگهداری کالای تأمین‌کنندگان در انبار مرکزی و اجرای محاسبه</p>
        </div>
        <button type="button" onClick={() => void load()} disabled={loading || busy || runBusy}
          className="grid size-11 shrink-0 place-items-center rounded-lg border border-slate-300 focus-visible:outline-2 focus-visible:outline-brand disabled:opacity-50" aria-label="به‌روزرسانی">
          <RefreshCw aria-hidden="true" className="size-4" strokeWidth={1.5} />
        </button>
      </div>
      {loading && <p role="status" className="text-sm text-slate-600">در حال دریافت تعرفه‌های انبارداری…</p>}
      {loadError && (
        <div className="rounded-lg bg-rose-50 p-3 text-sm text-rose-800">
          <p role="alert" className="[overflow-wrap:anywhere]">{loadError}</p>
          <button type="button" onClick={() => void load()} className="mt-1 min-h-11 rounded-lg px-2 font-bold focus-visible:outline-2 focus-visible:outline-rose-700">تلاش دوباره</button>
        </div>
      )}
      {settings && !settings.storageFeeEnabled && (
        <div role="status" className="rounded-lg bg-amber-50 p-3 text-xs text-amber-800">
          هزینه انبارداری در حال حاضر خاموش است؛ پیش‌فرض خاموش است و فعال‌سازی آن با تصمیم مدیریت انجام می‌شود. تا وقتی خاموش است هیچ هزینه‌ای از تأمین‌کنندگان کسر نمی‌شود و اجرای محاسبه هم پذیرفته نمی‌شود.
        </div>
      )}
      {notice && <div role="status" className="rounded-lg bg-emerald-50 p-3 text-xs text-emerald-700">{notice}</div>}
      <form onSubmit={save} className="space-y-3 rounded-xl border bg-white p-4">
        <fieldset disabled={unavailable} className="space-y-3">
          <label className="flex min-h-11 items-center justify-between gap-3 text-sm font-semibold">
            فعال‌بودن هزینه انبارداری
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              className="h-4 w-4 accent-teal-700"
            />
          </label>
          <label className="block text-sm font-semibold">
            روزهای رایگان نگهداری (۰ تا ۳۶۵)
            <input
              value={freeDays}
              onChange={(e) => setFreeDays(e.target.value)}
              inputMode="numeric"
              placeholder="مثلاً ۳۰"
              className={storageFeeInput}
              dir="ltr"
            />
          </label>
          <label className="block text-sm font-semibold">
            تعرفه هر کیلوگرم در روز (تومان؛ نامنفی)
            <input
              value={perKgDay}
              onChange={(e) => setPerKgDay(e.target.value)}
              inputMode="numeric"
              placeholder="مثلاً ۲۰۰۰"
              className={storageFeeInput}
              dir="ltr"
            />
          </label>
          <label className="block text-sm font-semibold">
            تعرفه هر مترمکعب در روز (تومان؛ نامنفی)
            <input
              value={perM3Day}
              onChange={(e) => setPerM3Day(e.target.value)}
              inputMode="numeric"
              placeholder="مثلاً ۸۰۰۰"
              className={storageFeeInput}
              dir="ltr"
            />
          </label>
          <p className="text-xs text-slate-500">
            هزینه هر روز برای هر واحد کالا برابر است با: (وزن به کیلوگرم × تعرفه کیلوگرم) + (حجم به مترمکعب × تعرفه مترمکعب)، ضربدر تعداد واحدهای آن محموله. کالایی که وزن یا ابعاد ثبت‌شده نداشته باشد هزینه‌ای برایش محاسبه نمی‌شود.
          </p>
          <button type="submit" className="min-h-11 w-full rounded-lg bg-teal-700 px-4 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-700 disabled:opacity-50 sm:w-auto">
            {busy ? 'در حال ذخیره...' : 'ذخیره تنظیمات'}
          </button>
        </fieldset>
      </form>
      <div className="space-y-3 rounded-xl border bg-white p-4">
        <h3 className="text-sm font-bold text-slate-800">اجرای دستی محاسبه</h3>
        <p className="text-xs text-slate-600">
          هزینهٔ هر محموله از تعداد موجود در ابتدای هر روز کامل، پس از پایان مهلت رایگان، محاسبه می‌شود. هزینهٔ ثبت‌نشدهٔ روزهای گذشته پس از خروج کالا هم حفظ می‌شود. فقط سابقه‌های تأییدشده محاسبه می‌شوند و اجرای دوباره مبلغ تکراری اضافه نمی‌کند.
        </p>
        {hasChanges && <p className="text-sm text-amber-800">برای اجرای محاسبه، ابتدا تغییرات تعرفه را ذخیره کنید.</p>}
        <button type="button" onClick={() => setConfirmRun(true)} disabled={!canRun} className="min-h-11 w-full rounded-lg bg-slate-900 px-4 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-slate-800 disabled:opacity-50 sm:w-auto">
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
      {error && <div role="alert" className="rounded-lg bg-rose-50 p-3 text-sm text-rose-800 [overflow-wrap:anywhere]">{error}</div>}
      {confirmRun && settings && (
        <SupplierDialog titleId="storage-fee-run-heading" descriptionId="storage-fee-run-description" onDismiss={() => setConfirmRun(false)}>
          <SupplierDialogHeading id="storage-fee-run-heading" title="اجرای محاسبه هزینه انبارداری" onClose={() => setConfirmRun(false)} />
          <p id="storage-fee-run-description" className="text-sm leading-6 text-slate-700">این اجرا هزینه روزهای مشمول را با تعرفه‌های ذخیره‌شده روی مانده تسویه تأمین‌کنندگان ثبت می‌کند.</p>
          <ul className="my-3 space-y-1 rounded-xl bg-slate-50 p-3 text-sm text-slate-800">
            <li>روزهای رایگان: {toPersianDigits(settings.storageFeeFreeDays)}</li>
            <li>هر کیلوگرم در روز: {formatPrice(settings.storageFeePerKgDay)} تومان</li>
            <li>هر مترمکعب در روز: {formatPrice(settings.storageFeePerM3Day)} تومان</li>
          </ul>
          <div className="grid grid-cols-2 gap-2">
            <button type="button" onClick={() => setConfirmRun(false)} className="min-h-11 rounded-xl bg-slate-100 px-2 text-sm font-bold focus-visible:outline-2 focus-visible:outline-brand">انصراف</button>
            <button type="button" onClick={() => void runNow()} className="min-h-11 rounded-xl bg-slate-900 px-2 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-slate-800">تأیید و اجرای محاسبه</button>
          </div>
        </SupplierDialog>
      )}
    </section>
  );
};
