import React, { useCallback, useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest } from '../../services/apiClient';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { Cpu, AlertTriangle, RefreshCw } from 'lucide-react';

// Live jobs-tab contract, proven from /opt/drup/dist/assets/AdminDashboardView-MERGED.js:
//   GET /admin/growth/jobs (hs.jobs, byte 334273) tried first, GET /admin/jobs
//   (Na.list, byte 60078) as fallback; both answer { jobs: [...] }.
//   POST /admin/jobs/{id}/run (Na.runNow, byte 60150) answers { job: ... }.
//   Job fields (tab normalizer, byte 445526+): id, schedule, startedAt,
//   lastRunAt, lastReleased, lastRunError, runCount, running,
//   lastRunDurationMs, consecutiveFailures, failureCount, failureRatePct,
//   recentRuns[{ at, durationMs, error, released }].
// The normalizer is applied to BOTH fetch paths here (live applies it only
// to the /admin/jobs fallback); both candidate endpoints now emit the same
// scheduler-state shape, so the displayed result is identical.
// Deliberate omission: the live normalizer also sets recentRunsNote
// ("history is read from service memory and cleared on server restart").
// Candidate history comes from persisted job_runs rows, so that wording
// would be false here and is not reproduced.

interface RecentRun {
  at: string;
  durationMs: number;
  error?: string | null;
  released: number;
}

interface ServerJob {
  id: string;
  schedule?: string;
  startedAt?: string;
  lastRunAt?: string | null;
  lastReleased?: number;
  lastRunError?: string | null;
  runCount?: number;
  running?: boolean;
  lastRunDurationMs?: number;
  consecutiveFailures?: number;
  failureCount?: number;
  failureRatePct?: number | null;
  recentRuns?: RecentRun[];
  alert?: boolean;
  alertNote?: string;
}

const normalizeJob = (y: ServerJob): ServerJob => ({
  ...y,
  lastReleased: y.lastReleased ?? 0,
  lastRunError: y.lastRunError ?? null,
  runCount: y.runCount ?? 0,
  running: y.running ?? false,
  lastRunDurationMs: y.lastRunDurationMs ?? 0,
  consecutiveFailures: y.consecutiveFailures ?? 0,
  failureCount: y.failureCount ?? 0,
  failureRatePct: y.failureRatePct ?? null,
  alert: (y.consecutiveFailures ?? 0) >= 3,
  alertNote: 'هشدار وقتی ۳ اجرای متوالی ناموفق باشد.',
  recentRuns: y.recentRuns ?? [],
});

// Live title/description map (byte 444541): only delivery-lock-releaser is
// named; any other id falls back to the id itself with no description.
const JOB_TITLES: Record<string, { title: string; desc: string }> = {
  'delivery-lock-releaser': {
    title: 'تأیید خودکار تحویل و آزادسازی قفل سود سفارش‌ها',
    desc: 'کار دو مرحله‌ای زمان‌بند: (۱) تأیید خودکار تحویل سفارش‌هایی که مشتری در مهلت قابل‌تنظیم (در تنظیمات) تأیید نکرده است — بدون جابه‌جایی پول و با آغاز مهلت مرجوعی؛ (۲) آزادسازی کامل سود هر سفارش با سند متوازن دفتری، پس از پایان مهلت مرجوعی آن سفارش.',
  },
};

// Live schedule-label map (byte 445227): only "every 60 seconds" is named.
const SCHEDULE_LABELS: Record<string, string> = {
  'every 60 seconds': 'هر ۶۰ ثانیه',
};

const formatRunAt = (t?: string | null): string => {
  if (!t) return '';
  const d = new Date(t);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString('fa-IR');
};

export const AdminJobsQueueTab: React.FC = () => {
  const { toPersianDigits } = useStore();
  const { success, error: toastError } = useToast();
  const [jobs, setJobs] = useState<ServerJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [runningJobId, setRunningJobId] = useState<string | null>(null);
  const [confirmJob, setConfirmJob] = useState<ServerJob | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try {
      const growth = await apiRequest<{ jobs: ServerJob[] }>('/admin/growth/jobs', { method: 'GET', auth: true }).catch(() => null);
      if (growth) {
        setJobs((growth.jobs || []).map(normalizeJob));
      } else {
        const list = await apiRequest<{ jobs: ServerJob[] }>('/admin/jobs', { method: 'GET', auth: true });
        setJobs((list.jobs || []).map(normalizeJob));
      }
    } catch {
      setLoadError('خطا در دریافت وضعیت زمان‌بند');
      setJobs([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const handleConfirmRunNow = async () => {
    const job = confirmJob;
    setConfirmJob(null);
    if (!job || runningJobId) return;
    setRunningJobId(job.id);
    try {
      const res = await apiRequest<{ job: ServerJob }>(`/admin/jobs/${encodeURIComponent(job.id)}/run`, { method: 'POST', auth: true });
      setJobs((prev) => prev.map((j) => (j.id === job.id ? normalizeJob(res.job) : j)));
      const m = res.job.lastReleased || 0;
      if (m > 0) {
        success(`یک چرخه اجرا شد؛ ${toPersianDigits(m)} سفارش پس از پایان مهلت مرجوعی آزاد گردید.`);
      } else {
        success('یک چرخه اجرا شد؛ سفارشی برای آزادسازی وجود نداشت.');
      }
    } catch (e) {
      toastError(e instanceof Error && e.message ? e.message : 'اجرای دستی انجام نشد.');
    } finally {
      setRunningJobId(null);
    }
  };

  const formatDuration = (ms: number): string =>
    ms >= 1000 ? `${toPersianDigits((ms / 1000).toFixed(1))} ثانیه` : `${toPersianDigits(ms)} میلی‌ثانیه`;

  const confirmTitle = confirmJob ? (JOB_TITLES[confirmJob.id]?.title || confirmJob.id) : '';

  return (
    <div className="space-y-4 pb-6" dir="rtl">
      <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs space-y-2">
        <p className="text-xs text-slate-500 leading-6">
          اینجا کارهایی را می‌بینی که سرور خودش سر وقت انجام می‌دهد؛ مثلاً آزاد کردن سود سفارش‌ها بعد از تمام شدن مهلت مرجوعی. لازم نیست هر روز اینجا بیایی — فقط اگر برچسب «خطا» دیدی، موضوع را پیگیری کن.
        </p>
        <p className="text-[11px] text-slate-500 leading-6">
          «فعال» یعنی آخرین اجرا سالم تمام شده. «خطا در آخرین اجرا» یعنی دفعه آخر کار کامل انجام نشده و سرور در نوبت بعدی دوباره تلاش می‌کند. «اجرای فوری» یعنی همین حالا، بیرون از نوبت همیشگی، یک بار اجرا شود.
        </p>
      </div>

      <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-1.5 text-brand-deep">
            <Cpu className="w-4 h-4" strokeWidth={1.5} />
            <h3 className="text-xs font-bold text-slate-800">کارهای خودکار سرور (زمان‌بند پس‌زمینه)</h3>
          </div>
          <button
            type="button"
            onClick={() => void load()}
            disabled={loading}
            className="min-h-[44px] min-w-[44px] flex items-center justify-center rounded-xl border border-slate-200 text-slate-600 disabled:opacity-50"
            title="به‌روزرسانی"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} strokeWidth={1.5} />
          </button>
        </div>
        <p className="text-xs text-slate-500 leading-6">وضعیت «آخرین اجرا» مستقیم از خود سرور خوانده می‌شود؛ عدد ساختگی این‌جا نمایش داده نمی‌شود.</p>
      </div>

      {loading ? (
        <div className="p-4 bg-white rounded-2xl border border-slate-200/90 shadow-2xs text-center text-xs text-slate-500">
          در حال دریافت وضعیت کارهای خودکار…
        </div>
      ) : loadError ? (
        <div className="p-4 bg-white rounded-2xl border border-rose-200 shadow-2xs text-center space-y-3">
          <AlertTriangle className="w-6 h-6 text-rose-400 mx-auto" strokeWidth={1.5} />
          <p className="text-xs font-bold text-rose-700">{loadError}</p>
          <button
            type="button"
            onClick={() => void load()}
            className="min-h-[44px] px-4 rounded-xl bg-brand-deep text-white text-xs font-bold"
          >
            تلاش مجدد
          </button>
        </div>
      ) : jobs.length === 0 ? (
        <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs text-center space-y-2">
          <p className="text-xs font-bold text-slate-700">کار خودکاری فعال نیست</p>
          <p className="text-[11px] text-slate-500 leading-6">
            سرور در حال حاضر هیچ کار زمان‌بندی‌شده‌ای گزارش نمی‌کند. اگر سودی آزاد نشده یا کاری عقب افتاده، اول این‌جا را بعد از چند دقیقه دوباره تازه کن؛ اگر باز خالی بود، موضوع فنی است.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {jobs.map((job) => {
            const meta = JOB_TITLES[job.id] || { title: job.id, desc: '' };
            const busy = runningJobId === job.id || !!job.running;
            const lastMs = job.lastRunDurationMs ?? 0;
            const duration = lastMs >= 1000
              ? `${toPersianDigits((lastMs / 1000).toFixed(1))} ثانیه`
              : `${toPersianDigits(lastMs)} میلی‌ثانیه`;
            return (
              <div key={job.id} className="p-4 bg-white rounded-2xl border border-slate-200/90 shadow-2xs space-y-3 text-xs">
                {job.alert && (
                  <div className="flex items-start gap-2 bg-rose-50 border border-rose-300 rounded-xl p-3 text-rose-800">
                    <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" strokeWidth={1.5} />
                    <div className="text-[11px] leading-6">
                      <b>هشدار: این کار چند بار پشت سر هم خطا خورده است</b>
                      {' — '}
                      {toPersianDigits(job.consecutiveFailures ?? 0)}
                      {' اجرای پی‌درپی ناموفق.'}
                      {job.alertNote && <span className="block text-rose-600">{job.alertNote}</span>}
                    </div>
                  </div>
                )}

                <div className="flex items-center justify-between gap-2">
                  <span className="font-bold text-slate-800 leading-6">
                    {meta.title}
                    <span className="block text-[10px] font-normal text-slate-400" dir="ltr">{job.id}</span>
                  </span>
                  <span
                    className={`text-[10px] font-bold px-2 py-1 rounded-full border shrink-0 ${job.lastRunError ? 'bg-rose-50 text-rose-700 border-rose-200' : 'bg-emerald-50 text-emerald-700 border-emerald-200'}`}
                  >
                    {job.lastRunError ? 'خطا در آخرین اجرا' : 'سالم — آخرین اجرا موفق'}
                  </span>
                </div>

                {meta.desc && <p className="text-[11px] text-slate-500 leading-6">{meta.desc}</p>}

                <div className="grid grid-cols-2 gap-2 text-[11px] text-slate-500 bg-slate-50 rounded-xl p-3">
                  <div>
                    <span>
                      هر چند وقت یک بار اجرا می‌شود: <strong className="text-slate-700">{SCHEDULE_LABELS[job.schedule || ''] || job.schedule || ''}</strong>
                    </span>
                  </div>
                  <div>
                    <span>
                      تعداد اجراها از اول: <strong className="text-slate-700">{toPersianDigits(job.runCount ?? 0)}</strong>
                    </span>
                  </div>
                  <div className="col-span-2">
                    <span>
                      آخرین اجرا:{' '}
                      <strong className="text-slate-700">{job.lastRunAt ? formatRunAt(job.lastRunAt) : 'هنوز اجرا نشده'}</strong>
                      {job.lastRunAt && !job.lastRunError && (
                        <>
                          {' • آخرین چرخه '}
                          <strong className="text-emerald-700">{toPersianDigits(job.lastReleased ?? 0)}</strong>
                          {' آزادسازی'}
                        </>
                      )}
                    </span>
                  </div>
                  <div>
                    <span>
                      مدت آخرین اجرا: <strong className="text-slate-700">{job.lastRunAt ? duration : '—'}</strong>
                    </span>
                  </div>
                  <div>
                    <span>
                      چند درصد اجراها خطا خورده:{' '}
                      <strong className={job.failureRatePct != null && job.failureRatePct > 0 ? 'text-rose-600' : 'text-slate-700'}>
                        {job.failureRatePct != null ? `${toPersianDigits(job.failureRatePct.toFixed(1))}٪` : '—'}
                      </strong>
                      {' '}
                      ({toPersianDigits(job.failureCount ?? 0)} خطا از {toPersianDigits(job.runCount ?? 0)} اجرا)
                    </span>
                  </div>
                  {job.lastRunError && (
                    <div className="col-span-2 text-rose-600 font-medium break-words">
                      خطای آخرین اجرا: {job.lastRunError}
                    </div>
                  )}
                </div>

                {job.recentRuns && job.recentRuns.length > 0 && (
                  <div className="space-y-1.5">
                    <p className="text-[11px] font-bold text-slate-500">اجراهای اخیر (جدیدترین‌ها)</p>
                    <div className="space-y-1.5 max-h-36 overflow-y-auto">
                      {job.recentRuns.slice(0, 5).map((run, i) => (
                        <div key={`${run.at}-${i}`} className="flex items-center justify-between min-h-[40px] text-[11px] bg-slate-50 rounded-xl px-3 py-2">
                          <span className="text-slate-500">{formatRunAt(run.at)}</span>
                          <span className="text-slate-500">{formatDuration(run.durationMs)}</span>
                          {run.error ? (
                            <span className="text-rose-600 font-bold">ناموفق</span>
                          ) : (
                            <span className="text-emerald-600 font-bold">
                              موفق{run.released > 0 ? ` — ${toPersianDigits(run.released)} آزادسازی` : ''}
                            </span>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <div className="pt-2 border-t border-slate-100 flex items-center justify-between gap-2 flex-wrap">
                  <span className="text-[10px] text-slate-400 leading-5">اجرای فوری، نوبت همیشگی را جلو می‌اندازد؛ قبل از اجرا از تو تأیید می‌گیرد.</span>
                  <button
                    type="button"
                    onClick={() => setConfirmJob(job)}
                    disabled={busy}
                    className="min-h-[44px] px-4 rounded-xl bg-brand-deep text-white font-bold text-xs flex items-center gap-1.5 transition-colors disabled:opacity-50"
                  >
                    <RefreshCw className={`w-4 h-4 ${busy ? 'animate-spin' : ''}`} strokeWidth={1.5} />
                    <span>{busy ? 'در حال اجرا…' : 'همین حالا اجرا کن'}</span>
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <ConfirmDialog
        isOpen={confirmJob !== null}
        title="اجرای فوری زمان‌بند"
        message={`کار «${confirmTitle}» همین حالا اجرا شود؟ این کار چرخه زمان‌بند را جلو می‌اندازد و ممکن است سود سفارش‌هایی که مهلت مرجوعی‌شان تمام شده زودتر آزاد شود.`}
        confirmText="اجرای فوری"
        onConfirm={() => void handleConfirmRunNow()}
        onCancel={() => setConfirmJob(null)}
      />
    </div>
  );
};
