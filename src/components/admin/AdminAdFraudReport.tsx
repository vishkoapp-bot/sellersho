import React, { useEffect, useState } from 'react';
import { Megaphone, RefreshCw, ShieldAlert } from 'lucide-react';
import { adminAdNetworkApi, AdNetworkReport, AdReportWindow } from '../../services/adminAdNetworkApi';

const nf = new Intl.NumberFormat('fa-IR');
const eventNames = new Map([['click', 'کلیک'], ['impression', 'بازدید'], ['challenge', 'اعتبارسنجی مرورگر']]);
const reasonNames = new Map([['bot_detected', 'نشانه‌های مرورگر خودکار یا ربات'], ['invalid_fingerprint', 'اثر انگشت نامعتبر'], ['invalid_challenge', 'اعتبارسنجی نامعتبر'], ['expired_challenge', 'انقضای اعتبارسنجی'], ['too_fast', 'کلیک زودتر از حد مجاز']]);
const eventLabel = (event: string) => eventNames.get(event) || 'سایر درخواست‌ها';
const reasonLabel = (reason: string, event: string) => {
  if (reason === 'duplicate_click') return `${eventLabel(event)} تکراری`;
  return reasonNames.get(reason) || reason || 'دلیل ثبت نشده';
};
const dateLabel = (value: string) => new Date(value).toLocaleString('fa-IR');

export const AdminAdFraudReport: React.FC = () => {
  const [days, setDays] = useState<AdReportWindow>(7);
  const [refresh, setRefresh] = useState(0);
  const [report, setReport] = useState<AdNetworkReport | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(''); setReport(null);
    adminAdNetworkApi.report(days, controller.signal).then((value) => {
      if (!controller.signal.aborted) setReport(value);
    }).catch((e: unknown) => {
      if (!controller.signal.aborted) setError(e instanceof Error ? e.message : 'دریافت گزارش انجام نشد.');
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [days, refresh]);
  const summary = report?.summary;
  const clickAttempts = summary ? summary.acceptedClicks + summary.rejectedClicks : 0;
  const metrics = [
    { label: 'بازدید ثبت‌شده', value: summary?.acceptedImpressions, tone: 'bg-slate-50 text-slate-800' },
    { label: 'کلیک پذیرفته‌شده', value: summary?.acceptedClicks, tone: 'bg-emerald-50 text-emerald-800' },
    { label: 'کلیک ردشده', value: summary?.rejectedClicks, tone: 'bg-rose-50 text-rose-800' },
    { label: 'بازدید ردشده', value: summary?.rejectedImpressions, tone: 'bg-amber-50 text-amber-900' },
    { label: 'اعتبارسنجی ردشده', value: summary?.rejectedChallenges, tone: 'bg-slate-100 text-slate-800' },
  ];
  return <section aria-label="گزارش واقعی تبلیغات و ضدتقلب" aria-busy={loading} className="space-y-3.5">
    <div className="space-y-4 rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2"><Megaphone className="size-4 shrink-0 text-brand-deep" strokeWidth={1.5} aria-hidden="true" /><h3 className="text-sm font-bold text-slate-900">آمار تبلیغات و درخواست‌های ردشده</h3></div>
        <div className="flex flex-wrap items-end gap-2">
          <label className="space-y-1 text-sm text-slate-700">
            <span className="block">بازهٔ گزارش</span>
            <select aria-label="بازهٔ گزارش" value={days} onChange={event => setDays(Number(event.target.value) as AdReportWindow)}
              className="min-h-11 rounded-lg border border-slate-300 bg-white px-2 text-base focus-visible:outline-2 focus-visible:outline-brand">
              {([7, 30, 90] as const).map(value => <option key={value} value={value}>{nf.format(value)} روز اخیر</option>)}
            </select>
          </label>
          <button type="button" disabled={loading} onClick={() => setRefresh(value => value + 1)}
            className="flex min-h-11 items-center gap-1 rounded-lg border border-slate-300 px-2 text-sm font-bold focus-visible:outline-2 focus-visible:outline-brand disabled:opacity-50">
            <RefreshCw className={`size-4 shrink-0 ${loading ? 'motion-safe:animate-spin' : ''}`} aria-hidden="true" />به‌روزرسانی
          </button>
        </div>
      </div>
      <p className="text-sm leading-6 text-slate-600">آمار رویدادهای شبکه شامل درخواست‌های تکراری، اعتبارسنجی ناموفق و موارد مشکوک به تقلب است.</p>
      {error && (
        <div className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">
          <p role="alert" className="[overflow-wrap:anywhere]">{error}</p>
          <button type="button" onClick={() => setRefresh(value => value + 1)} className="mt-1 min-h-11 rounded-lg px-2 font-bold focus-visible:outline-2 focus-visible:outline-rose-700">تلاش دوباره</button>
        </div>
      )}
      {loading && <p role="status" className="text-sm text-slate-600">در حال دریافت آمار تبلیغات…</p>}
      <div className="grid grid-cols-2 gap-2 text-center sm:grid-cols-3 xl:grid-cols-5">{metrics.map((metric) => <div key={metric.label} className={`space-y-2 rounded-xl p-3 ${metric.tone}`}><span className="block text-[10px]">{metric.label}</span><strong className="block text-lg tabular-nums">{metric.value === undefined ? '—' : nf.format(metric.value)}</strong></div>)}</div>
      {report && <div className="flex flex-wrap justify-between gap-2 border-t border-slate-100 pt-3 text-[10px] leading-5 text-slate-500"><span>کل ردهای ثبت‌شده: {nf.format(report.summary.totalRejectedEvents)}{report.summary.otherRejectedEvents > 0 && ` · سایر درخواست‌های ردشده: ${nf.format(report.summary.otherRejectedEvents)}`}{clickAttempts > 0 && ` · نرخ رد کلیک: ${new Intl.NumberFormat('fa-IR', { maximumFractionDigits: 1 }).format(report.summary.rejectedClicks * 100 / clickAttempts)}٪`}</span><span>آخرین دریافت: {dateLabel(report.to)}</span></div>}
    </div>
    {report && <>
      <div className="rounded-2xl border border-slate-200/90 bg-white p-3.5 text-xs"><h4 className="mb-3 font-bold text-slate-800">دلایل رد در این بازه</h4>{report.reasons.length === 0 ? <p className="text-slate-500">در این بازه درخواست ردشده‌ای ثبت نشده است.</p> : <ul className="divide-y divide-slate-100">{report.reasons.map((item) => <li key={JSON.stringify([item.eventType, item.reason])} className="flex items-start justify-between gap-3 py-2"><span className="min-w-0 break-words leading-6">{reasonLabel(item.reason, item.eventType)}<small className="mr-2 text-slate-400">{eventLabel(item.eventType)}</small></span><b className="shrink-0 rounded-md bg-slate-50 px-2 py-1">{nf.format(item.count)}</b></li>)}</ul>}</div>
      <div className="space-y-2.5"><div className="flex items-center gap-1.5 text-rose-600"><ShieldAlert className="h-4 w-4" strokeWidth={1.5} /><h4 className="text-xs font-bold text-slate-800">آخرین درخواست‌های ردشده</h4></div>
        {report.incidents.length === 0 ? <p className="rounded-xl border border-dashed border-slate-200 p-4 text-xs text-slate-500">گزارشی برای نمایش در این بازه وجود ندارد.</p> : <><p className="text-[10px] text-slate-500">نمایش {nf.format(report.incidents.length)} مورد اخیر از {nf.format(report.summary.totalRejectedEvents)} رد ثبت‌شده در بازهٔ انتخابی.</p>{report.incidents.map((item) => <article key={item.id} className="space-y-2 rounded-2xl border border-rose-100 bg-white p-3 text-xs shadow-2xs"><div className="flex flex-wrap justify-between gap-2 text-[10px]"><bdi className="font-mono text-rose-700">#{item.id}</bdi><time dateTime={item.occurredAt} className="text-slate-400">{dateLabel(item.occurredAt)}</time></div><p className="break-words font-bold leading-6 text-slate-800">{reasonLabel(item.reason, item.eventType)}</p><div className="flex flex-wrap gap-x-4 gap-y-1 border-t border-slate-100 pt-2 text-[10px] leading-6 text-slate-500 [&>span]:min-w-0 [&>span]:max-w-full [&>span]:[overflow-wrap:anywhere]"><span>نوع: {eventLabel(item.eventType)}</span><span>فروشگاه: {item.storeName || 'مشخص نیست'}</span><span>کمپین: {item.campaignName || 'کمپین موجود نیست'}</span><span className="text-rose-700">در شمارش پذیرفته‌شده ثبت نشده</span></div></article>)}</>}
      </div>
    </>}
  </section>;
};
