import React, { useEffect, useState } from 'react';
import { RefreshCw, MessageSquare, WalletCards } from 'lucide-react';
import { smsApi, SmsLog, IPPanelSettings } from '../../services/smsApi';

export const AdminSmsTab: React.FC = () => {
  const [credit, setCredit] = useState<Record<string, unknown> | null>(null);
  const [logs, setLogs] = useState<SmsLog[]>([]);
  const [error, setError] = useState('');
  const [settings, setSettings] = useState<IPPanelSettings>({ enabled: false, baseUrl: 'https://edge.ippanel.com/v1', apiKeyMasked: '', sender: '+983000505', patternCode: '' });
  const [apiKey, setApiKey] = useState('');
  const [recipient, setRecipient] = useState('');
  const [testCode, setTestCode] = useState('123456');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const load = async () => { try { setError(''); const [s, l] = await Promise.all([smsApi.getSettings(), smsApi.logs()]); setSettings(s); setLogs(l); try { setCredit(await smsApi.credit()); } catch { setCredit(null); } } catch (e: any) { setError(e?.message || 'دریافت تنظیمات پیامک انجام نشد.'); } };
  useEffect(() => { void load(); }, []);
  const saveSettings = async (event: React.FormEvent) => { event.preventDefault(); setBusy(true); setError(''); setNotice(''); try { await smsApi.saveSettings({ ...settings, apiKey: apiKey || undefined }); setApiKey(''); setNotice('تنظیمات ذخیره شد.'); await load(); } catch (e: any) { setError(e?.message || 'ذخیره تنظیمات ناموفق بود.'); } finally { setBusy(false); } };
  const sendTest = async () => { setBusy(true); setError(''); setNotice(''); try { const result = await smsApi.test(recipient, testCode); setNotice(`درخواست ارسال پذیرفته شد. شناسه: ${result.messageOutboxIds?.join(', ') || 'ثبت نشده'}`); await load(); } catch (e: any) { setError(e?.message || 'ارسال آزمایشی ناموفق بود.'); } finally { setBusy(false); } };
  return <section className="space-y-4">
    <div className="flex items-center justify-between"><div><h2 className="text-lg font-extrabold">تنظیمات و پایش IPPanel</h2><p className="text-xs text-slate-500">تنظیم اتصال، تست واقعی پیامک و مشاهده گزارش ارسال</p></div><button onClick={() => void load()} className="p-2 rounded-lg border" title="به‌روزرسانی"><RefreshCw className="w-4 h-4"  strokeWidth={1.5}/></button></div>
    {notice && <div role="status" className="rounded-lg bg-emerald-50 p-3 text-xs text-emerald-700">{notice}</div>}
    <form onSubmit={saveSettings} className="space-y-3 rounded-xl border bg-white p-4">
      <h3 className="text-sm font-bold">پیکربندی اتصال</h3>
      <label className="flex items-center gap-2 text-xs font-bold"><input type="checkbox" checked={settings.enabled} onChange={e => setSettings({ ...settings, enabled: e.target.checked })} /> فعال‌سازی ارسال OTP از IPPanel</label>
      <label className="block text-xs font-semibold">آدرس پایه<input value={settings.baseUrl} onChange={e => setSettings({ ...settings, baseUrl: e.target.value })} className="mt-1 w-full rounded-lg border p-2 font-mono text-xs" dir="ltr" /></label>
      <label className="block text-xs font-semibold">API key <span className="text-slate-400">{settings.apiKeyMasked ? `(ذخیره‌شده: ${settings.apiKeyMasked})` : '(هنوز ثبت نشده)'}</span><input type="password" value={apiKey} onChange={e => setApiKey(e.target.value)} placeholder="برای حفظ کلید فعلی خالی بگذارید" autoComplete="new-password" className="mt-1 w-full rounded-lg border p-2 font-mono text-xs" dir="ltr" /></label>
      <label className="block text-xs font-semibold">شماره فرستنده<input value={settings.sender} onChange={e => setSettings({ ...settings, sender: e.target.value })} className="mt-1 w-full rounded-lg border p-2 font-mono text-xs" dir="ltr" /></label>
      <label className="block text-xs font-semibold">کد پترن<input value={settings.patternCode} onChange={e => setSettings({ ...settings, patternCode: e.target.value })} className="mt-1 w-full rounded-lg border p-2 font-mono text-xs" dir="ltr" /></label>
      <button disabled={busy} className="rounded-lg bg-teal-700 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">{busy ? 'در حال ذخیره...' : 'ذخیره تنظیمات'}</button>
    </form>
    <div className="space-y-3 rounded-xl border bg-white p-4">
      <h3 className="text-sm font-bold">ارسال پیامک آزمایشی (ارسال واقعی)</h3>
      <p className="text-xs text-amber-700">این دکمه یک پیامک واقعی با پترن تنظیم‌شده می‌فرستد و ممکن است هزینه داشته باشد.</p>
      <div className="grid gap-2 sm:grid-cols-2"><input aria-label="شماره مقصد" value={recipient} onChange={e => setRecipient(e.target.value)} placeholder="شماره مقصد با کد کشور، مثل +98912..." className="rounded-lg border p-2 text-xs" dir="ltr" /><input aria-label="کد آزمایشی" value={testCode} onChange={e => setTestCode(e.target.value)} className="rounded-lg border p-2 font-mono text-xs" dir="ltr" /></div>
      <button onClick={() => void sendTest()} disabled={busy || !recipient} className="rounded-lg bg-indigo-700 px-4 py-2 text-xs font-bold text-white disabled:opacity-50">ارسال پیامک تست</button>
    </div>
    {error && <div className="rounded-lg bg-rose-50 p-3 text-xs text-rose-700">{error}</div>}
    <div className="rounded-xl border bg-white p-4"><div className="flex items-center gap-2 text-sm font-bold"><WalletCards className="w-4 h-4 text-teal-600"  strokeWidth={1.5}/> اعتبار حساب</div><pre className="mt-3 text-xs text-slate-600 whitespace-pre-wrap">{credit ? JSON.stringify(credit, null, 2) : 'در حال دریافت...'}</pre></div>
    <div className="rounded-xl border bg-white p-4"><div className="flex items-center gap-2 text-sm font-bold"><MessageSquare className="w-4 h-4 text-teal-600"  strokeWidth={1.5}/> سابقه ارسال</div><div className="mt-3 space-y-2">{logs.map(log => <div key={log.id} className="flex flex-wrap items-center justify-between gap-2 border-b pb-2 text-xs"><span dir="ltr">{log.mobile}</span><span>{log.status}</span><span className="text-slate-400">{new Date(log.createdAt).toLocaleString('fa-IR')}</span></div>)}{!logs.length && <p className="text-xs text-slate-500">رکوردی ثبت نشده است.</p>}</div></div>
  </section>;
};