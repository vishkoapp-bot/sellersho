import React, { useEffect, useRef, useState } from 'react';
import { Download, ReceiptText } from 'lucide-react';
import { getPanelStatement, type StatementPage, type StatementRow } from '../../services/panelStatementApi';
import { ContextSupportButton } from '../support/ContextSupportButton';

const money = new Intl.NumberFormat('fa-IR');
const tehran = 'Asia/Tehran';
const dateLabel = new Intl.DateTimeFormat('fa-IR', { dateStyle: 'medium', timeZone: tehran });
const monthLabel = new Intl.DateTimeFormat('fa-IR-u-ca-gregory', { month: 'long', year: 'numeric', timeZone: tehran });
const statuses: Record<string, string> = {
  pending: 'رزرو تا بررسی برداشت', approved: 'تأییدشده، در انتظار واریز', processing: 'در حال واریز',
  transfer_confirmed: 'واریزشده', paid: 'واریزشده', completed: 'واریزشده',
};

function recentMonths(now: Date) {
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone: tehran, year: 'numeric', month: 'numeric' }).formatToParts(now);
  const year = Number(parts.find(part => part.type === 'year')?.value);
  const month = Number(parts.find(part => part.type === 'month')?.value);
  return Array.from({ length: 24 }, (_, offset) => {
    const date = new Date(Date.UTC(year, month - offset - 1, 1));
    return { value: date.toISOString().slice(0, 7), label: monthLabel.format(date) };
  });
}

function csvField(value: string) {
  const protectedText = /^[\s\u200e\u200f]*[=+@-]/u.test(value) ? `'${value}` : value;
  return `"${protectedText.replaceAll('"', '""')}"`;
}

export function statementCsv(rows: StatementRow[]) {
  return '\uFEFFتاریخ,نوع,مرجع,وضعیت,مبلغ (تومان)\r\n' + rows.map(row => [csvField(row.date), csvField(row.label), csvField(row.reference), csvField(statuses[row.status ?? ''] ?? ''), row.amount].join(',')).join('\r\n');
}

function sameStatementTotals(previous: StatementPage, next: StatementPage): boolean {
  return previous.month === next.month && previous.totalEntries === next.totalEntries &&
    previous.credits === next.credits && previous.debits === next.debits;
}

const changedStatement = 'گردش حساب هنگام دریافت تغییر کرد. گزارش را دوباره دریافت کن.';

type Props = { role: 'seller'; slug: string; storeId: string; refresh?: number } | { role: 'supplier'; refresh?: number };

export function AccountStatement(props: Props) {
  const [open, setOpen] = useState(false);
  const [months] = useState(() => recentMonths(new Date()));
  const [month, setMonth] = useState(months[0].value);
  const [page, setPage] = useState<StatementPage>();
  const [busy, setBusy] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState('');
  const read = useRef<AbortController | null>(null);
  const download = useRef<AbortController | null>(null);
  const role = props.role;
  const slug = props.role === 'seller' ? props.slug : '';
  const source = role === 'seller' ? { role, slug } as const : { role } as const;

  async function load(cursor = '') {
    read.current?.abort();
    const request = new AbortController();
    read.current = request;
    setBusy(true);
    setError('');
    try {
      const next = await getPanelStatement(source, month, cursor, request.signal);
      if (request.signal.aborted) return;
      if (next.month !== month || (cursor && page && !sameStatementTotals(page, next))) {
        throw new Error(changedStatement);
      }
      setPage(previous => cursor && previous ? { ...next, entries: [...previous.entries, ...next.entries] } : next);
    } catch (cause) {
      if (!request.signal.aborted) setError(cause instanceof Error ? cause.message : 'دریافت گردش حساب انجام نشد.');
    } finally {
      if (!request.signal.aborted) setBusy(false);
    }
  }

  useEffect(() => {
    setPage(undefined);
    setExporting(false);
    if (open) void load();
    return () => { read.current?.abort(); download.current?.abort(); };
  }, [open, slug, month, props.refresh]);

  async function exportPeriod() {
    if (!page || page.month !== month || download.current) return;
    const request = new AbortController();
    download.current = request;
    setExporting(true);
    setError('');
    try {
      const rows: StatementRow[] = [];
      let cursor = '';
      const cursors = new Set<string>();
      const identities = new Set<string>();
      do {
        const next = await getPanelStatement(source, month, cursor, request.signal, 200);
        if (request.signal.aborted) return;
        if (!sameStatementTotals(page, next)) throw new Error(changedStatement);
        for (const row of next.entries) {
          const identity = JSON.stringify([row.id, row.kind]);
          if (identities.has(identity)) throw new Error(changedStatement);
          identities.add(identity);
          rows.push(row);
        }
        cursor = next.nextCursor ?? '';
        if (cursor && cursors.has(cursor)) throw new Error('بخشی از گزارش تکراری بود. دوباره تلاش کن.');
        if (cursor) cursors.add(cursor);
      } while (cursor);
      if (rows.length !== page.totalEntries) throw new Error(changedStatement);
      const url = URL.createObjectURL(new Blob([statementCsv(rows)], { type: 'text/csv;charset=utf-8' }));
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `statement-${month}.csv`;
      anchor.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (cause) {
      if (!request.signal.aborted) setError(cause instanceof Error ? cause.message : 'دریافت فایل انجام نشد.');
    } finally {
      if (download.current === request) download.current = null;
      if (!request.signal.aborted) setExporting(false);
    }
  }

  return (
    <details id="account-statement" onToggle={event => setOpen(event.currentTarget.open)} className="rounded-[22px] border border-slate-200 bg-white p-4 shadow-sm sm:p-5">
      <summary className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg font-extrabold text-slate-900 focus-visible:ring-2 focus-visible:ring-brand"><ReceiptText aria-hidden="true" className="size-5 text-brand-deep" /> گردش حساب و صورت‌حساب دوره‌ای</summary>
      {open && <div className="space-y-4 pt-2">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <label className="grid gap-1 text-sm font-bold text-slate-700">ماه گزارش (میلادی)<select className="min-h-11 rounded-xl border border-slate-300 bg-white px-3 text-sm" value={month} onChange={event => setMonth(event.target.value)}>{months.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>
          <button type="button" disabled={page?.month !== month || busy || exporting || !!error} onClick={() => void exportPeriod()} className="inline-flex min-h-11 items-center gap-2 rounded-xl border border-brand-border bg-brand-soft px-3 text-sm font-bold text-brand-deep disabled:opacity-60"><Download aria-hidden="true" className="size-4" />{exporting ? 'در حال ساخت فایل…' : 'دریافت CSV دوره'}</button>
        </div>
        {busy && <p role="status" className="text-sm text-slate-600">در حال دریافت گردش حساب…</p>}
        {error && <div className="flex flex-wrap items-center gap-3 text-sm text-rose-800"><p role="alert">{error}</p><button type="button" onClick={() => void load()} className="min-h-11 rounded-lg px-2 font-bold underline">تلاش دوباره</button></div>}
        {page?.month === month && <>
          <dl className="grid grid-cols-2 gap-2 lg:grid-cols-5">
            {([
              ['موجودی قابل برداشت امروز', page.available], ['بستانکار دوره', page.credits], ['بدهکار دوره', page.debits],
              ['ماندهٔ ابتدای دوره', page.opening], [month === months[0].value ? 'ماندهٔ دوره تا امروز' : 'ماندهٔ پایان دوره', page.closing],
            ] as const).filter(([, value]) => value !== undefined).map(([label, value]) => <div key={label} className="min-w-0 rounded-xl border border-slate-200 bg-slate-50 p-3"><dt className="text-xs text-slate-600">{label}</dt><dd className="break-words pt-1 text-sm font-extrabold tabular-nums text-slate-900">{money.format(value ?? 0)} تومان</dd></div>)}
          </dl>
          {page.openingUnverified && <p className="text-xs leading-6 text-amber-900">ماندهٔ ابتدای دوره از گردش‌های قبلی محاسبه شده و هنوز تأیید تطبیق مالی ندارد.</p>}
          {role === 'supplier' && <p className="text-xs leading-6 text-slate-600">این گزارش مبالغ مؤثر بر مانده را با وضعیت فعلی سفارش‌ها و برداشت‌ها نشان می‌دهد. مبلغ برداشت از زمان ثبت درخواست در گزارش می‌آید.</p>}
          {Boolean(page.undatedCount) && <p className="text-xs leading-6 text-amber-900">{money.format(page.undatedCount ?? 0)} ردیف قدیمی بدون تاریخ قطعی، در ماندهٔ کل حساب دیده می‌شوند.</p>}
          {page.totalEntries === 0 ? <p className="rounded-xl bg-slate-50 p-3 text-sm text-slate-600">در این دوره تراکنشی ثبت نشده است.</p> : <p className="text-xs text-slate-600">{money.format(page.entries.length)} از {money.format(page.totalEntries)} ردیف</p>}
          <ul className="divide-y divide-slate-200">
            {page.entries.map(row => <li key={`${row.id}:${row.kind}`} className="grid items-center gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_auto_auto]">
              <div className="min-w-0"><p className="font-bold text-slate-900">{row.label}</p><p className="break-words text-xs leading-5 text-slate-600">{dateLabel.format(new Date(row.date))} · مرجع <bdi>{row.reference}</bdi>{row.status && statuses[row.status] ? ` · ${statuses[row.status]}` : ''}</p></div>
              <strong className={`tabular-nums ${row.amount < 0 ? 'text-rose-800' : 'text-emerald-800'}`}>{row.amount < 0 ? 'کسر' : 'افزایش'} {money.format(Math.abs(row.amount))} تومان</strong>
              {props.role === 'seller' ? <ContextSupportButton role="seller" kind="financial" referenceType="statement" referenceCode={row.kind} referenceId={row.id} storeId={props.storeId} /> : <ContextSupportButton role="supplier" kind="financial" referenceType={row.kind} referenceId={row.id} />}
            </li>)}
          </ul>
          {page.nextCursor && <button type="button" disabled={busy || !!error} onClick={() => void load(page.nextCursor)} className="min-h-11 rounded-xl bg-brand-soft px-4 text-sm font-bold text-brand-deep disabled:opacity-60">نمایش ردیف‌های بیشتر</button>}
        </>}
      </div>}
    </details>
  );
}
