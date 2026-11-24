import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Barcode, Printer } from 'lucide-react';
import { ApiError, apiRequest, tokenStore } from '../../services/apiClient';
import { newUuid } from '../../services/uuid';
import { isSPNCX, openSPNCXPrint, spncxBarcode } from './spncxLabelPrint';

const endpoints = {
  admin: '/admin/spncx',
  supplier: '/supplier/inventory/spncx',
  warehouse: '/warehouse/inventory/spncx',
} as const;

type StaffScope = keyof typeof endpoints;
type PrintDraft = { productId: string; variantId: string; codes: string[]; reprint: boolean };
type PrintJob = PrintDraft & { id: string; barcodeFormat: 'CODE128'; createdAt: string };
type Attempt = { key: string; draft: PrintDraft };
type Unit = { code: string; productId: string; variantId: string; printedAt: string | null; printCount: number; status: string };

const uncertainJob = 'نتیجه ثبت مشخص نیست. فقط همین درخواست را دوباره بفرستید تا ثبت تکراری نشود.';
const field = 'w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 focus-visible:outline-2 focus-visible:outline-slate-700 disabled:bg-slate-50';
const button = 'inline-flex items-center justify-center gap-2 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-bold text-slate-800 hover:bg-slate-100 disabled:opacity-50';
const primary = 'inline-flex items-center justify-center gap-2 rounded-lg bg-slate-900 px-4 py-2 text-sm font-bold text-white hover:bg-slate-700 disabled:opacity-50';
const fa = (count: number) => count.toLocaleString('fa-IR');

const withIDs = (value: unknown): value is { productId: string; variantId: string } =>
  !!value && typeof (value as PrintDraft).productId === 'string' && !!(value as PrintDraft).productId
  && typeof (value as PrintDraft).variantId === 'string' && !!(value as PrintDraft).variantId;

function approvedJob(value: unknown, draft: PrintDraft): value is PrintJob {
  if (!withIDs(value)) return false;
  const job = value as PrintJob;
  return job.productId === draft.productId && job.variantId === draft.variantId
    && typeof job.id === 'string' && !!job.id && job.barcodeFormat === 'CODE128'
    && typeof job.createdAt === 'string' && Number.isFinite(Date.parse(job.createdAt))
    && job.reprint === draft.reprint && Array.isArray(job.codes) && job.codes.length === draft.codes.length
    && new Set(job.codes).size === job.codes.length
    && job.codes.every(code => isSPNCX(code) && draft.codes.includes(code));
}

function requestError(error: unknown): string {
  if (!(error instanceof ApiError)) return 'پاسخ معتبر دریافت نشد؛ دوباره تلاش کنید.';
  switch (error.status) {
    case 401: return 'نشست شما پایان یافته است؛ دوباره وارد شوید.';
    case 403: return 'اجازهٔ دسترسی به برچسب‌های این بخش را ندارید.';
    case 404: return 'کد پیدا نشد یا در محدودهٔ دسترسی شما نیست.';
    case 409: return 'وضعیت کدها تغییر کرده است؛ برای چاپ، کدها را دوباره بررسی کنید.';
    case 400: return 'کد یا تعداد برچسب معتبر نیست.';
    default: return error.status === 0 ? error.message : 'دریافت پاسخ از سرور ممکن نبود؛ دوباره تلاش کنید.';
  }
}

export function SPNCXLabelPanel({ scope }: { scope: StaffScope }) {
  const [mode, setMode] = useState<'first' | 'known'>('first');
  const [sku, setSKU] = useState('');
  const [count, setCount] = useState('1');
  const [codes, setCodes] = useState('');
  const [draft, setDraft] = useState<PrintDraft | null>(null);
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const [job, setJob] = useState<PrintJob | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const working = useRef<AbortController | null>(null);
  const currentPrint = useRef<Window | null>(null);
  const verifiedSession = useRef<{ generation: number; token: string } | null>(null);
  const prefix = endpoints[scope];

  function clearJob() {
    currentPrint.current?.close();
    currentPrint.current = null;
    working.current?.abort();
    working.current = null;
    verifiedSession.current = null;
    setDraft(null);
    setAttempt(null);
    setJob(null);
    setHasMore(false);
    setMessage('');
    setBusy(false);
  }

  useEffect(() => {
    function invalidate() {
      clearJob();
      setSKU('');
      setCodes('');
    }
    function checkStorage() {
      const session = verifiedSession.current;
      if (session && session.token !== tokenStore.getToken()) invalidate();
    }
    window.addEventListener('drup-session-changed', invalidate);
    window.addEventListener('drup-session-expired', invalidate);
    window.addEventListener('storage', checkStorage);
    return () => {
      window.removeEventListener('drup-session-changed', invalidate);
      window.removeEventListener('drup-session-expired', invalidate);
      window.removeEventListener('storage', checkStorage);
      working.current?.abort();
      working.current = null;
      currentPrint.current?.close();
      currentPrint.current = null;
    };
  }, [scope]);

  function sameSession() {
    const found = verifiedSession.current;
    return !!found && found.generation === tokenStore.getGeneration() && found.token === tokenStore.getToken();
  }

  function run(fn: (signal: AbortSignal, relevant: () => boolean) => Promise<void>) {
    if (working.current) return;
    const token = tokenStore.getToken();
    if (!token) {
      clearJob();
      setMessage('برای ثبت یا مشاهدهٔ برچسب ابتدا وارد پنل شوید.');
      return;
    }
    verifiedSession.current = { token, generation: tokenStore.getGeneration() };
    const controller = new AbortController();
    working.current = controller;
    setBusy(true);
    setMessage('');
    const relevant = () => working.current === controller && sameSession();
    void fn(controller.signal, relevant).catch(error => {
      if (relevant()) setMessage(requestError(error));
    }).finally(() => {
      if (working.current === controller) {
        working.current = null;
        setBusy(false);
      }
    });
  }

  function checkLabels(event: FormEvent) {
    event.preventDefault();
    if (working.current || attempt || job) return;
    setDraft(null);
    setHasMore(false);
    if (mode === 'first') {
      const spnc = sku.trim().toUpperCase();
      const limit = Number(count);
      if (!/^[0-9A-Z-]{1,12}$/.test(spnc) || !Number.isInteger(limit) || limit < 1 || limit > 100) {
        setMessage('کد تنوع (SPNC) و تعداد بین ۱ تا ۱۰۰ را وارد کنید.');
        return;
      }
      run(async (signal, relevant) => {
        const found = await apiRequest<{ codes: unknown; hasMore: boolean }>(`${prefix}/unprinted`, {
          method: 'POST', auth: true, signal, body: JSON.stringify({ spnc, limit }),
        });
        if (!relevant()) return;
        if (!found || !Array.isArray(found.codes) || found.codes.length > limit
          || new Set(found.codes).size !== found.codes.length || !found.codes.every(isSPNCX)) throw new Error('Invalid lookup');
        if (!found.codes.length) {
          setMessage('کد چاپ‌نشده‌ای برای این تنوع در محدودهٔ دسترسی شما پیدا نشد.');
          return;
        }
        if (!withIDs(found)) throw new Error('Invalid scope');
        setDraft({ productId: found.productId, variantId: found.variantId, codes: found.codes, reprint: false });
        setHasMore(found.hasMore === true);
      });
      return;
    }

    const entered = codes.trim().toUpperCase().split(/[\s,،;؛]+/).filter(Boolean);
    if (!entered.length || entered.length > 100 || !entered.every(isSPNCX) || new Set(entered).size !== entered.length) {
      setMessage('حداکثر ۱۰۰ کد یکتای SPNCX وارد کنید. هر کد را در یک خط بگذارید.');
      return;
    }
    run(async (signal, relevant) => {
      const units: Unit[] = [];
      for (let start = 0; start < entered.length && relevant(); start += 6) {
        const batch = entered.slice(start, start + 6);
        const results = await Promise.all(batch.map(code => apiRequest<Unit>(`${prefix}/lookup`, {
          method: 'POST', auth: true, signal, body: JSON.stringify({ code }),
        })));
        if (!results.every((unit, i) => withIDs(unit) && unit.code === batch[i] && Number.isInteger(unit.printCount)
          && unit.printCount >= 0 && (unit.printCount === 0 ? unit.printedAt === null : typeof unit.printedAt === 'string'))) {
          throw new Error('Invalid lookup result');
        }
        units.push(...results);
      }
      if (!relevant()) return;
      const first = units[0];
      if (!units.every(unit => unit.status === 'in_stock')) {
        setMessage('تنها کد واحدهایی که در موجودی قابل‌فروش هستند قابل چاپ است.');
        return;
      }
      if (!units.every(unit => unit.variantId === first.variantId && unit.productId === first.productId)) {
        setMessage('در هر درخواست، فقط کدهای یک تنوع را وارد کنید.');
        return;
      }
      const reprint = first.printCount > 0;
      if (!units.every(unit => (unit.printCount > 0) === reprint)) {
        setMessage('کدهای چاپ اول و چاپ مجدد را در دو درخواست جدا وارد کنید.');
        return;
      }
      setDraft({ productId: first.productId, variantId: first.variantId, codes: entered, reprint });
    });
  }

  function prepareLabelJob() {
    if (working.current || !draft || job) return;
    if (!sameSession()) {
      clearJob();
      setMessage('نشست تغییر کرده است؛ کدها را دوباره بررسی کنید.');
      return;
    }
    const pending = attempt ?? { key: newUuid(), draft };
    setAttempt(pending);
    run(async (signal, relevant) => {
      let response: unknown;
      try {
        response = await apiRequest<unknown>(`${prefix}/print-jobs`, {
          method: 'POST', auth: true, signal,
          headers: { 'Idempotency-Key': pending.key }, body: JSON.stringify(pending.draft),
        });
      } catch (error) {
        if (!relevant()) return;
        if (error instanceof ApiError && [400, 401, 403, 404, 409].includes(error.status)) {
          setAttempt(null);
          setDraft(null);
          setMessage(requestError(error));
        } else {
          setMessage(uncertainJob);
        }
        return;
      }
      if (!relevant()) return;
      if (!approvedJob(response, pending.draft)) {
        setMessage(uncertainJob);
        return;
      }
      setAttempt(null);
      setJob(response);
      setMessage('');
    });
  }

  function openPrint() {
    if (!job || !sameSession()) {
      clearJob();
      setMessage('نشست تغییر کرده است؛ کدها را دوباره بررسی کنید.');
      return;
    }
    try {
      currentPrint.current?.close();
      currentPrint.current = openSPNCXPrint(job.codes);
      setMessage(currentPrint.current ? '' : 'مرورگر پنجره چاپ را مسدود کرد. اجازه دهید و دوباره همین برچسب‌ها را باز کنید.');
    } catch {
      setMessage('پنجره چاپ باز نشد؛ دوباره همین برچسب‌ها را باز کنید.');
    }
  }

  const locked = busy || !!attempt || !!job;
  return (
    <section dir="rtl" className="mx-auto max-w-4xl space-y-4 rounded-2xl border border-slate-200 bg-white p-4 sm:p-6">
      <header className="flex items-start gap-3">
        <Barcode aria-hidden="true" className="h-7 w-7 shrink-0 text-slate-700" />
        <div>
          <h2 className="text-lg font-black text-slate-900">برچسب واحد کالا (SPNCX)</h2>
          <p className="mt-1 text-sm leading-relaxed text-slate-600">برای هر واحد، همان کد صادرشده از موجودی روی برچسب Code 128 چاپ می‌شود.</p>
        </div>
      </header>
      <div className="flex flex-wrap gap-2">
        <button type="button" data-action="first-labels" disabled={locked} aria-pressed={mode === 'first'} className={button} onClick={() => { clearJob(); setMode('first'); }}>چاپ اول با کد تنوع</button>
        <button type="button" data-action="known-labels" disabled={locked} aria-pressed={mode === 'known'} className={button} onClick={() => { clearJob(); setMode('known'); }}>چاپ یا چاپ مجدد کدهای موجود</button>
      </div>
      <form onSubmit={checkLabels} className="space-y-3">
        {mode === 'first' ? (
          <div className="grid gap-3 sm:grid-cols-[1fr_9rem]">
            <label htmlFor="spncx-sku-code" className="space-y-1 text-sm font-bold text-slate-800">
              <span>کد تنوع (SPNC)</span>
              <input id="spncx-sku-code" dir="ltr" spellCheck={false} autoComplete="off" className={field} value={sku} maxLength={12} disabled={locked} onChange={event => { setSKU(event.target.value); setDraft(null); setMessage(''); }} />
            </label>
            <label htmlFor="spncx-label-count" className="space-y-1 text-sm font-bold text-slate-800">
              <span>تعداد برچسب</span>
              <input id="spncx-label-count" type="number" dir="ltr" min={1} max={100} className={field} value={count} disabled={locked} onChange={event => { setCount(event.target.value); setDraft(null); setMessage(''); }} />
            </label>
          </div>
        ) : (
          <label htmlFor="spncx-known-codes" className="block space-y-1 text-sm font-bold text-slate-800">
            <span>کدهای صادرشده (هر کد در یک خط)</span>
            <textarea id="spncx-known-codes" dir="ltr" spellCheck={false} autoComplete="off" rows={3} maxLength={2200} className={field} value={codes} disabled={locked} onChange={event => { setCodes(event.target.value); setDraft(null); setMessage(''); }} />
          </label>
        )}
        <button type="submit" data-action={mode === 'first' ? 'discover-labels' : 'verify-labels'} disabled={locked} className={button}>{busy && !attempt ? 'در حال بررسی…' : mode === 'first' ? 'یافتن کدهای چاپ‌نشده' : 'بررسی کدها'}</button>
      </form>
      {message && <p role="alert" className="rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-slate-900">{message}</p>}
      {draft && !job && (
        <div className="space-y-3 rounded-xl bg-slate-50 p-3">
          <p className="text-sm font-bold text-slate-900">{fa(draft.codes.length)} واحد برای {draft.reprint ? 'چاپ مجدد' : 'چاپ اول'} انتخاب شد.</p>
          {hasMore && <p className="text-xs text-slate-600">پس از این دسته، کدهای چاپ‌نشدهٔ بیشتری برای همین تنوع وجود دارد.</p>}
          <div dir="ltr" className="max-h-32 overflow-auto rounded-lg bg-white p-2 font-mono text-xs text-slate-700">{draft.codes.map(code => <div key={code}>{code}</div>)}</div>
          <button type="button" data-action="prepare-labels" disabled={busy} className={primary} onClick={prepareLabelJob}>
            {busy ? 'در حال ثبت…' : attempt ? 'تلاش دوباره با همین درخواست' : draft.reprint ? 'ثبت چاپ مجدد' : 'آماده‌سازی چاپ اول'}
          </button>
        </div>
      )}
      {job && (
        <div className="space-y-3 rounded-xl border border-slate-200 p-3">
          <p role="status" className="text-sm font-bold text-slate-900">درخواست {job.reprint ? 'چاپ مجدد' : 'چاپ اول'} برای {fa(job.codes.length)} برچسب ثبت شد.</p>
          <p className="text-sm leading-relaxed text-slate-600">ثبت درخواست به معنی چاپ فیزیکی نیست. اندازهٔ پیش‌فرض هر برچسب ۸۰ × ۳۰ میلی‌متر است.</p>
          <div className="grid max-h-64 gap-2 overflow-auto sm:grid-cols-2">
            {job.codes.map(code => {
              const { path, viewBox } = spncxBarcode(code);
              return <figure key={code} className="m-0 rounded-lg border border-slate-200 bg-white p-2 text-center text-black">
                <svg aria-hidden="true" className="h-12 w-full fill-black" viewBox={viewBox} preserveAspectRatio="none" shapeRendering="crispEdges"><path d={path} /></svg>
                <figcaption dir="ltr" className="font-mono text-xs font-bold">{code}</figcaption>
              </figure>;
            })}
          </div>
          <button type="button" data-action="open-label-print" className={primary} onClick={openPrint}><Printer aria-hidden="true" size={16} />باز کردن پنجره چاپ</button>
        </div>
      )}
      {job && <button type="button" data-action="start-labels" className={button} onClick={() => { clearJob(); setCodes(''); }}>شروع درخواست جدید</button>}
    </section>
  );
}
