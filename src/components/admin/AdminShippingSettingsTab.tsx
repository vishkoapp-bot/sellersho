import React, { useCallback, useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { adminApi, type ShippingSettingsDto } from '../../services/adminApi';
import { formatPrice } from '../../utils/formatters';
import { normalizePersianText } from '../../utils/routing';

const parseField = (raw: string): number | null => {
  const digits = normalizePersianText(raw.trim()).replace(/[,٬]/g, '');
  if (!/^\d+$/.test(digits)) return null;
  const value = Number(digits);
  return Number.isSafeInteger(value) ? value : null;
};

export const AdminShippingSettingsTab: React.FC = () => {
  const [charge, setCharge] = useState('');
  const [weight, setWeight] = useState('');
  const [savedCharge, setSavedCharge] = useState<number | null>();
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(true);

  const applySettings = useCallback((settings: ShippingSettingsDto) => {
    const { supplierReturnPostalCharge, defaultItemWeightGrams } = settings;
    setSavedCharge(supplierReturnPostalCharge);
    setCharge(supplierReturnPostalCharge === null ? '' : formatPrice(supplierReturnPostalCharge));
    setWeight(String(defaultItemWeightGrams));
  }, []);

  const load = useCallback(async () => {
    setBusy(true);
    setError('');
    try {
      applySettings(await adminApi.getShippingSettings());
    } catch {
      setError('دریافت تنظیمات حمل‌ونقل انجام نشد.');
    } finally {
      setBusy(false);
    }
  }, [applySettings]);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async (payload: Parameters<typeof adminApi.saveShippingSettings>[0]) => {
    setError('');
    setNotice('');
    setBusy(true);
    try {
      applySettings(await adminApi.saveShippingSettings(payload));
      setNotice('تنظیمات حمل‌ونقل ذخیره شد.');
      await load();
    } catch {
      setError('ذخیره تنظیمات ناموفق بود.');
    } finally {
      setBusy(false);
    }
  };

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    if (busy || savedCharge === undefined) return;
    setError('');
    setNotice('');

    const chargeValue = parseField(charge);
    if (charge.trim() !== '' && chargeValue === null) {
      setError('هزینه مرجوعی پستی باید عدد صحیح نامنفی (تومان) باشد.');
      return;
    }
    const weightValue = parseField(weight);
    if (weightValue === null || weightValue < 1) {
      setError('وزن پیش‌فرض هر قلم باید عدد صحیح حداقل ۱ گرم باشد.');
      return;
    }
    void save({ supplierReturnPostalCharge: chargeValue, defaultItemWeightGrams: weightValue });
  };

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-extrabold">تنظیمات حمل‌ونقل</h2>
        <button type="button" onClick={() => void load()} disabled={busy} className="rounded-lg border p-2 disabled:opacity-50" aria-label="به‌روزرسانی" title="به‌روزرسانی">
          <RefreshCw className="h-4 w-4" strokeWidth={1.5} />
        </button>
      </div>
      {notice && <div role="status" className="rounded-lg bg-emerald-50 p-3 text-xs text-emerald-700">{notice}</div>}
      <form onSubmit={handleSubmit} className="rounded-xl border bg-white p-4">
        <fieldset disabled={busy || savedCharge === undefined} className="space-y-3">
          <div className="space-y-1">
            <label className="block text-xs font-semibold">
              هزینه مرجوعی پستی تأمین‌کننده (تومان)
              <input
                type="text"
                value={charge}
                onChange={(event) => setCharge(event.target.value)}
                inputMode="numeric"
                className="mt-1 w-full rounded-lg border p-2 text-xs"
                aria-describedby="shipping-postal-charge-help"
                dir="ltr"
              />
            </label>
            <p id="shipping-postal-charge-help" className="text-xs text-slate-500">
              برای هر درخواست مرجوعی پستی همین مبلغ ثبت می‌شود. صفر یعنی مرجوعی پستی رایگان است. خالی یعنی هنوز تعیین نشده و گزینهٔ پست برای تأمین‌کننده‌ها غیرفعال می‌ماند.
            </p>
            {savedCharge === null && (
              <p role="status" className="rounded-lg bg-amber-50 p-3 text-xs text-amber-800">
                هنوز تعیین نشده است — تا وقتی تعیین نشود، تأمین‌کننده‌ها نمی‌توانند کالا را با پست برگردانند.
              </p>
            )}
            {savedCharge === 0 && <p role="status" className="text-xs text-slate-600">مرجوعی پستی رایگان است.</p>}
          </div>
          <label className="block text-xs font-semibold">
            وزن پیش‌فرض هر قلم (گرم)
            <input
              type="text"
              value={weight}
              onChange={(event) => setWeight(event.target.value)}
              inputMode="numeric"
              className="mt-1 w-full rounded-lg border p-2 text-xs"
              dir="ltr"
            />
          </label>
          <div className="flex flex-wrap gap-2">
            <button type="submit" className="rounded-lg bg-teal-700 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">
              ذخیره تنظیمات
            </button>
            <button
              type="button"
              onClick={() => void save({ supplierReturnPostalCharge: null })}
              className="rounded-lg border px-4 py-2 text-xs font-bold disabled:opacity-50"
            >
              پاک‌کردن (بازگرداندن به حالت تعیین‌نشده)
            </button>
          </div>
        </fieldset>
      </form>
      {error && <div role="alert" className="rounded-lg bg-rose-50 p-3 text-xs text-rose-700">{error}</div>}
    </section>
  );
};
