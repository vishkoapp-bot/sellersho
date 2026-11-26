import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { RefreshCw } from 'lucide-react';
import { smsApi, type SmsLog, type IPPanelSettings } from '../../services/smsApi';
import { SupplierResourceState } from '../supplier/SupplierResourceState';

const card = 'min-w-0 space-y-3 rounded-2xl border border-slate-200 bg-white p-4';
const field = 'mt-1 min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 text-base font-normal focus-visible:outline-2 focus-visible:outline-brand';
const action = 'min-h-11 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50';
const errorText = (cause: unknown, fallback: string) => cause instanceof Error ? cause.message : fallback;

function validSettings(value: IPPanelSettings | null): value is IPPanelSettings {
  return value != null && typeof value.enabled === 'boolean' &&
    [value.baseUrl, value.sender, value.patternCode, value.apiKeyMasked].every(item => typeof item === 'string');
}

export function AdminSmsTab() {
  const [credit, setCredit] = useState<Record<string, unknown> | null>(null);
  const [logs, setLogs] = useState<SmsLog[] | null>(null);
  const [settings, setSettings] = useState<IPPanelSettings | null>(null);
  const [savedEnabled, setSavedEnabled] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [actionError, setActionError] = useState('');
  const [notice, setNotice] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [recipient, setRecipient] = useState('');
  const [testCode, setTestCode] = useState('123456');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const reload = useCallback(async () => {
    const current = ++generation.current;
    setLoading(true);
    setLoadError('');
    const [configuration, history, balance] = await Promise.allSettled([smsApi.getSettings(), smsApi.logs(), smsApi.credit()]);
    if (generation.current !== current) return;
    if (configuration.status === 'fulfilled' && validSettings(configuration.value)) {
      setSettings(configuration.value);
      setSavedEnabled(configuration.value.enabled);
    } else {
      setSettings(null);
      setSavedEnabled(false);
      setLoadError(configuration.status === 'rejected'
        ? errorText(configuration.reason, 'دریافت تنظیمات پیامک انجام نشد.')
        : 'پاسخ تنظیمات پیامک کامل نیست.');
    }
    setLogs(history.status === 'fulfilled' ? history.value : null);
    setCredit(balance.status === 'fulfilled' ? balance.value : null);
    setLoading(false);
  }, []);
  useEffect(() => { void reload(); return () => { ++generation.current; }; }, [reload]);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!settings || loading || busy) return;
    const current = generation.current;
    setBusy(true);
    setNotice('');
    setActionError('');
    try {
      const { enabled, baseUrl, sender, patternCode } = settings;
      const result = await smsApi.saveSettings({ enabled, baseUrl, sender, patternCode, apiKey: apiKey.trim() || undefined });
      if (generation.current !== current) return;
      if (result?.success !== true) throw new Error('ذخیرهٔ تنظیمات پیامک تأیید نشد.');
      setApiKey('');
      setNotice('تنظیمات ذخیره شد.');
      await reload();
    } catch (cause) {
      if (generation.current === current) setActionError(errorText(cause, 'ذخیرهٔ تنظیمات انجام نشد.'));
    } finally {
      setBusy(false);
    }
  }

  async function sendTest(event: FormEvent) {
    event.preventDefault();
    if (!savedEnabled || loading || busy || !recipient.trim()) return;
    const current = generation.current;
    setBusy(true);
    setNotice('');
    setActionError('');
    try {
      const result = await smsApi.test(recipient.trim(), testCode.trim());
      if (generation.current !== current) return;
      if (result?.success !== true) throw new Error('ارسال پیامک آزمایشی تأیید نشد.');
      const identifiers = result.messageOutboxIds?.join('، ');
      setNotice(identifiers ? `درخواست ارسال پذیرفته شد. شناسهٔ ارسال ${identifiers}` : 'درخواست ارسال پذیرفته شد.');
      await reload();
    } catch (cause) {
      if (generation.current === current) setActionError(errorText(cause, 'ارسال آزمایشی انجام نشد.'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="space-y-3" aria-label="مدیریت پیامک">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-base font-extrabold text-slate-900">اتصال پیامک و سابقهٔ ارسال</h2>
        <button type="button" disabled={busy || loading} onClick={() => void reload()}
          className="inline-flex min-h-11 items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-sm font-bold text-brand-deep disabled:opacity-50">
          <RefreshCw aria-hidden="true" className="size-4" /> به‌روزرسانی
        </button>
      </div>
      {notice && <p role="status" className="rounded-xl bg-emerald-50 p-3 text-sm text-emerald-800 [overflow-wrap:anywhere]">{notice}</p>}
      {actionError && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800 [overflow-wrap:anywhere]">{actionError}</p>}
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {settings && !loading && (
        <>
          <form onSubmit={save} className={card}>
            <h3 className="text-sm font-extrabold">تنظیمات اتصال IPPanel</h3>
            <fieldset disabled={busy} className="grid min-w-0 gap-3 sm:grid-cols-2">
              <label className="flex min-h-11 items-center gap-2 text-sm font-bold sm:col-span-2">
                <input type="checkbox" className="size-5" checked={settings.enabled} onChange={event => setSettings({ ...settings, enabled: event.target.checked })} />
                فعال‌سازی پیامک ورود
              </label>
              <label className="block min-w-0 text-sm font-bold">آدرس سرویس
                <input value={settings.baseUrl} onChange={event => setSettings({ ...settings, baseUrl: event.target.value })} dir="ltr" className={field} />
              </label>
              <label className="block min-w-0 text-sm font-bold">کلید اتصال
                <input type="password" value={apiKey} onChange={event => setApiKey(event.target.value)} dir="ltr" autoComplete="new-password" className={field} />
                {settings.apiKeyMasked && <span className="mt-1 block text-xs font-normal text-slate-600">برای حفظ کلید فعلی این فیلد را خالی بگذارید.</span>}
              </label>
              <label className="block min-w-0 text-sm font-bold">شمارهٔ فرستنده
                <input value={settings.sender} onChange={event => setSettings({ ...settings, sender: event.target.value })} inputMode="tel" dir="ltr" className={field} />
              </label>
              <label className="block min-w-0 text-sm font-bold">کد الگو
                <input value={settings.patternCode} onChange={event => setSettings({ ...settings, patternCode: event.target.value })} dir="ltr" className={field} />
              </label>
            </fieldset>
            <button id="sms-settings-save" type="submit" disabled={busy} className={action}>ذخیره تنظیمات</button>
          </form>
          <form onSubmit={sendTest} className={card}>
            <h3 className="text-sm font-extrabold">ارسال پیامک آزمایشی</h3>
            <p className="text-sm leading-6 text-slate-600">ارسال به شمارهٔ واردشده واقعی است. از تنظیمات ذخیره‌شده استفاده می‌شود.</p>
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <label className="block min-w-0 text-sm font-bold">شماره مقصد
                <input type="tel" value={recipient} onChange={event => setRecipient(event.target.value)} dir="ltr" className={field} required />
              </label>
              <label className="block min-w-0 text-sm font-bold">کد آزمایشی
                <input value={testCode} onChange={event => setTestCode(event.target.value)} inputMode="numeric" dir="ltr" className={field} />
              </label>
            </div>
            <button type="submit" disabled={busy || !savedEnabled || !recipient.trim()} className={action}>ارسال پیامک آزمایشی</button>
          </form>
        </>
      )}
      {!loading && (
        <>
          <section className={card} aria-label="اعتبار حساب پیامک">
            <h3 className="text-sm font-extrabold">اعتبار حساب</h3>
            {credit ? <pre dir="ltr" className="whitespace-pre-wrap text-xs text-slate-700 [overflow-wrap:anywhere]">{JSON.stringify(credit, null, 2)}</pre> : <p className="text-sm text-slate-600">اعتبار در دسترس نیست.</p>}
          </section>
          <section className={card} aria-label="سابقهٔ ارسال پیامک">
            <h3 className="text-sm font-extrabold">سابقهٔ ارسال</h3>
            {logs?.map(log => <div key={log.id} className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 pb-2 text-sm"><bdi>{log.mobile}</bdi><span>{log.status}</span><time dateTime={log.createdAt}>{new Date(log.createdAt).toLocaleString('fa-IR')}</time></div>)}
            {!logs?.length && <p className="text-sm text-slate-600">{logs ? 'پیامکی ثبت نشده است.' : 'دریافت سابقهٔ ارسال انجام نشد.'}</p>}
          </section>
        </>
      )}
    </section>
  );
}
