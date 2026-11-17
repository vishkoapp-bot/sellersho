import React, { useEffect, useState } from 'react';
import { apiRequest } from '../../services/apiClient';
export interface JobHealth { automaticRetry?: number; job: string; running: boolean; missing: boolean; stale: boolean; disabled: boolean; error: string; lastSuccess: string | null; started: string | null; finished: string | null; cutover: string | null; runs: number; affected: number; ready: number; review: number; capped: boolean; oldestReadySeconds: number; }
interface Overview { jobHealth?: JobHealth[]; jobs: { job: string; affected: number; error: string; finished_at: string }[]; overdueQC: { id: string; supplier_id: string; reason: string; response_due_at: string }[]; supplierSLA: { supplier_id: string; name: string; score: string; samples: number; eligible: boolean }[]; }
export const AdminOperationsTab: React.FC = () => {
  const [data, setData] = useState<Overview | null>(null); const [error, setError] = useState(''); const [busy, setBusy] = useState(false); const [resolutions, setResolutions] = useState<Record<string, string>>({});
  const load = async () => { setBusy(true); setError(''); try { setData(await apiRequest('/admin/operations', { auth: true })); } catch (e) { setError(e instanceof Error ? e.message : 'دریافت گزارش انجام نشد.'); } finally { setBusy(false); } };
  useEffect(() => { void load(); }, []);
  const resolve = async (id: string) => { setBusy(true); setError(''); try { await apiRequest(`/admin/operations/qc/${encodeURIComponent(id)}/resolve`, { method: 'POST', auth: true, body: JSON.stringify({ resolution: resolutions[id] }) }); await load(); } catch (e) { setError(e instanceof Error ? e.message : 'ثبت نتیجه انجام نشد.'); } finally { setBusy(false); } };
  return <section className="space-y-4 text-xs"><div className="flex items-center justify-between"><h2 className="text-base font-bold">پایش عملیات خودکار</h2><button disabled={busy} onClick={() => void load()} className="rounded-xl border bg-white px-3 py-2 disabled:opacity-50">به‌روزرسانی</button></div>{error && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-rose-700">{error}</p>}
    <JobHealthPanel jobs={data?.jobHealth} />
    <div className="space-y-3 rounded-2xl border bg-white p-4"><h3 className="font-bold">مغایرت‌های بی‌پاسخ پس از ۴۸ ساعت</h3><p className="leading-6 text-slate-500">انقضای مهلت به معنی پذیرش مغایرت یا کسر وجه نیست. نتیجهٔ بررسی انسانی را ثبت کنید؛ تغییر مالی مسیر مستقل دارد.</p>{data?.overdueQC.length === 0 && <p className="text-emerald-700">پروندهٔ منقضیِ باز وجود ندارد.</p>}{data?.overdueQC.map((q) => <article key={q.id} className="space-y-2 rounded-xl bg-amber-50 p-3"><strong>{q.id}</strong><p>{q.supplier_id} · {q.reason}</p><p>مهلت: {new Date(q.response_due_at).toLocaleString('fa-IR')}</p><textarea aria-label={`نتیجه بررسی ${q.id}`} value={resolutions[q.id] || ''} onChange={(e) => setResolutions((v) => ({ ...v, [q.id]: e.target.value }))} className="w-full rounded-xl border p-2" placeholder="نتیجه و دلیل بررسی" /><button disabled={busy || !resolutions[q.id]?.trim()} onClick={() => void resolve(q.id)} className="rounded-xl bg-slate-800 px-3 py-2 text-white disabled:opacity-50">ثبت نتیجه و بستن پرونده</button></article>)}</div>
    <div className="space-y-2 rounded-2xl border bg-white p-4"><h3 className="font-bold">عملکرد ارسال تأمین‌کنندگان</h3>{data?.supplierSLA.map((s) => <div key={s.supplier_id} className="flex justify-between gap-3 border-b py-2"><span>{s.name}</span><span>{new Intl.NumberFormat('fa-IR', { maximumFractionDigits: 2 }).format(Number(s.score))}٪ · {s.samples} نمونه · {s.eligible ? 'واجد مجوز خودکار' : 'فاقد شرایط خودکار'}</span></div>)}</div>
    <div className="space-y-2 rounded-2xl border bg-white p-4"><h3 className="font-bold">آخرین اجراهای جاب‌ها</h3>{data?.jobs.map((j, i) => <div key={`${j.job}-${i}`} className="space-y-1 border-b py-2"><span>{jobNames[j.job] || j.job}</span><p className={j.error ? 'text-rose-700' : 'text-slate-500'}>{j.error || `${j.affected} مورد پردازش شد`} · {new Date(j.finished_at).toLocaleString('fa-IR')}</p></div>)}</div>
  </section>;
};

const jobNames: Record<string, string> = {
  'unpaid-order-expiry': 'لغو سفارش‌های پرداخت‌نشده',
  'ad-cleanup': 'پاک‌سازی داده‌های منقضی تبلیغات',
  'qc-response-deadline': 'ارجاع مغایرت‌های بی‌پاسخ',
  'supplier-sla': 'ارزیابی ارسال تأمین‌کنندگان',
  'delivery-lock-releaser': 'تأیید تحویل و آزادسازی سود',
  'monthly-settlement': 'تسویهٔ ماهانه',
};
export const JobHealthPanel: React.FC<{ jobs?: JobHealth[] }> = ({ jobs }) => <div className="space-y-3 rounded-2xl border bg-white p-4">
  <h3 className="font-bold">سلامت اجرای عملیات</h3>
  <p className="leading-6 text-slate-500">شمارنده‌ها از زمان فعال‌شدن پایش جدید هستند. موارد نیازمند بررسی مالی جدا از کار آماده نمایش داده می‌شوند. مهلت لغو سفارش، پنج دقیقه پس از پایان مهلت پرداخت است.</p>
  {!jobs?.length && <p role="alert" className="text-amber-700">وضعیت اجراها دریافت نشده است؛ سلامت عملیات تأیید نشده است.</p>}
  {jobs?.map((j) => {
    const alarm = j.review > 0 || (!j.disabled && (j.missing || j.stale || !!j.error || (j.job === 'unpaid-order-expiry' && j.oldestReadySeconds > 300)));
    const count = (n: number) => n > 1000 ? '۱۰۰۰+' : n.toLocaleString('fa-IR');
    return <article key={j.job} role={alarm ? 'alert' : undefined} className={`space-y-1 rounded-xl p-3 ${alarm ? 'bg-amber-50 text-amber-900' : 'bg-slate-50'}`}>
      <strong>{jobNames[j.job] || 'عملیات خودکار'}</strong>
      <p>{j.disabled ? 'زمان تسویه تنظیم نشده؛ اجرای خودکار غیرفعال است.' : j.running ? 'در حال اجرا' : j.error ? 'اجرای قبلی یا ثبت نتیجه ناموفق بود؛ بررسی لازم است.' : j.missing || !j.lastSuccess ? 'هنوز اجرای موفقی ثبت نشده است.' : j.stale ? 'اجرای موفق به‌روز نیست؛ بررسی لازم است.' : 'اجرای موفق به‌روز است.'}</p>
      {j.review > 0 && <p>{j.job === 'monthly-settlement' ? 'بررسی مالی لازم است؛ غیرفعال بودن اجرای خودکار، موارد نیازمند بررسی را برطرف نمی‌کند.' : 'بررسی موارد ارجاع‌شده لازم است.'}</p>}
      {j.lastSuccess && <p>آخرین موفقیت: {new Date(j.lastSuccess).toLocaleString('fa-IR')}</p>}
      <p>آمادهٔ پردازش: {count(j.ready)} · نیازمند بررسی: {count(j.review)}</p>
      {!!j.automaticRetry && <p>از آماده‌های پردازش، در انتظار تلاش مجدد خودکار: {count(j.automaticRetry)} · این موارد نیازمند بررسی مالی نیستند.</p>}
      {j.ready > 0 && <p>تأخیر قدیمی‌ترین کار آماده: {Math.ceil(j.oldestReadySeconds / 60).toLocaleString('fa-IR')} دقیقه</p>}
      <p className="text-slate-500">{count(j.runs)} اجرا · {j.job === 'monthly-settlement' ? 'شمارش مالی در گزارش تسویه نمایش داده می‌شود.' : `${count(j.affected)} مورد پردازش‌شده از آغاز پایش`}</p>
    </article>;
  })}
</div>;
