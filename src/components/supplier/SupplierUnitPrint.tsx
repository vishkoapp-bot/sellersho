import { useEffect, useState } from 'react';
import { useToast } from '../../context/ToastContext';
import { ApiError } from '../../services/apiClient';
import { supplierApi } from '../../services/supplierApi';
import type { PrintUnitsRequest, UnitLabelListResponse, UnitLabelRow } from '../../services/unitsApi';
import { UNIT_STATUS_LABELS, type UnitStatus } from '../../utils/unitStatus';
import { UnitLabelSheet } from '../common/UnitLabelSheet';
import { PRINT_MARK_WARNING, SERIAL_UNAVAILABLE, useUnitPrint } from '../common/useUnitPrint';

const PRINTABLE_STATUSES = ['in_stock', 'defined', 'reserved', 'quarantined'] as const;
const PAGE_LIMIT = 100;
const MAX_PRINT = 500;
type Filters = { variant_id: string; status: typeof PRINTABLE_STATUSES[number]; unprinted_only: boolean };
const defaults: Filters = { variant_id: '', status: 'in_stock', unprinted_only: true };

export function SupplierUnitPrint({ productId, variants, onInitialAvailability }: {
  productId: string;
  variants: { id: string; variant_name: string }[];
  onInitialAvailability: (available: boolean) => void;
}) {
  const { success } = useToast();
  const job = useUnitPrint();
  const [filters, setFilters] = useState(defaults);
  const [result, setResult] = useState<UnitLabelListResponse | null>(null);
  const [selected, setSelected] = useState<UnitLabelRow[]>([]);
  const [confirmed, setConfirmed] = useState(false);
  const [count, setCount] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [message, setMessage] = useState('');
  const [refresh, setRefresh] = useState(0);
  const [hasUnfiltered, setHasUnfiltered] = useState(false);

  const getPage = (query: Filters, cursor?: string) => supplierApi.getUnitLabels(productId, {
    ...query, variant_id: query.variant_id || undefined, limit: PAGE_LIMIT, cursor,
  });
  const showError = (cause: unknown) => setMessage(cause instanceof ApiError && [404, 501].includes(cause.status)
    ? SERIAL_UNAVAILABLE : cause instanceof Error ? cause.message : 'دریافت برچسب‌ها انجام نشد.');

  useEffect(() => {
    let current = true;
    setLoading(true);
    setMessage('');
    setLoadFailed(false);
    setResult(null);
    setHasUnfiltered(false);
    async function load() {
      try {
        const page = await getPage(filters);
        let unfilteredTotal = page.total_for_filter;
        if (!unfilteredTotal && filters.unprinted_only) {
          unfilteredTotal = (await getPage({ ...filters, unprinted_only: false })).total_for_filter;
        }
        if (!current) return;
        setHasUnfiltered(unfilteredTotal > 0);
        setResult(page);
        const initial = filters === defaults && refresh === 0;
        if (initial && unfilteredTotal === 0) {
          const other = await Promise.all(PRINTABLE_STATUSES.slice(1).map(status => getPage({ ...filters, status, unprinted_only: false })));
          if (current) onInitialAvailability(other.some(page => page.total_for_filter > 0));
        } else if (initial) onInitialAvailability(true);
      } catch (cause) {
        if (!current) return;
        showError(cause);
        setLoadFailed(true);
        if (filters === defaults && refresh === 0) onInitialAvailability(false);
      } finally {
        if (current) setLoading(false);
      }
    }
    void load();
    return () => { current = false; };
  }, [productId, filters, refresh, onInitialAvailability]);

  function choose(rows: UnitLabelRow[]) {
    job.clear();
    setSelected(rows);
    setConfirmed(false);
  }

  function changeFilters(patch: Partial<Filters>) {
    choose([]);
    setCount('');
    setFilters(previous => ({ ...previous, ...patch }));
  }

  async function more(selectAll: boolean) {
    if (!result || loading) return;
    if (selectAll && result.total_for_filter > MAX_PRINT) {
      setMessage('برای هر درخواست حداکثر ۵۰۰ واحد انتخاب کنید.');
      return;
    }
    setLoading(true);
    setMessage('');
    try {
      let page = result;
      while (page.next_cursor) {
        const next = await getPage(filters, page.next_cursor);
        page = { ...next, units: [...page.units, ...next.units] };
        if (!selectAll) break;
      }
      setResult(page);
      if (selectAll) choose(page.units);
    } catch (cause) {
      showError(cause);
    } finally { setLoading(false); }
  }

  const printed = selected.filter(row => row.print_count > 0).length;
  const conflict = job.failure?.conflict === true;
  const reprint = printed > 0 || conflict;
  const nextCount = count === '' ? Math.min(result?.units.length ?? 0, MAX_PRINT) : Number(count);
  const nextAllowed = filters.variant_id && filters.unprinted_only && Number.isInteger(nextCount) && nextCount > 0 && nextCount <= MAX_PRINT;

  async function print(selection: Omit<PrintUnitsRequest, 'idempotency_key'>) {
    const units = await job.print(JSON.stringify(selection), idempotency_key => supplierApi.printUnitLabels(productId, { ...selection, idempotency_key }));
    if (!units) return;
    setSelected([]);
    setConfirmed(false);
    setRefresh(value => value + 1);
    if (units.length) success(selection.count ? `${units.length} برچسب بعدی چاپ شد` : `چاپ ${units.length} برچسب ثبت شد.`);
    else setMessage('واحدی برای چاپ وجود ندارد.');
  }

  const badCodes = job.failure?.codes ?? [];
  const codesToPrint = selected.map(row => row.code).filter(code => !badCodes.includes(code));
  const shown = job.units ?? selected.map(row => ({ ...row, barcode_value: row.code }));
  const printCounts = Object.fromEntries(selected.map(row => [row.code, row.print_count]));

  if (message === SERIAL_UNAVAILABLE || job.failure?.message === SERIAL_UNAVAILABLE) return <p role="status" className="p-4">{SERIAL_UNAVAILABLE}</p>;

  return <div className="space-y-3 p-4">
    <fieldset disabled={loading || job.busy} className="space-y-3 disabled:opacity-70">
      <div className="grid grid-cols-2 gap-2 text-sm">
        <label>تنوع<select className="block w-full rounded-lg border p-2" value={filters.variant_id} onChange={event => changeFilters({ variant_id: event.target.value })}>
          <option value="">همه تنوع‌ها</option>
          {variants.map(variant => <option key={variant.id} value={variant.id}>{variant.variant_name}</option>)}
        </select></label>
        <label>وضعیت<select className="block w-full rounded-lg border p-2" value={filters.status} onChange={event => changeFilters({ status: event.target.value as Filters['status'] })}>
          {PRINTABLE_STATUSES.map(status => <option key={status} value={status}>{UNIT_STATUS_LABELS[status]}</option>)}
        </select></label>
      </div>
      <label className="block"><input type="checkbox" checked={filters.unprinted_only} onChange={event => changeFilters({ unprinted_only: event.target.checked })} /> فقط چاپ‌نشده</label>
      {result && <>
        <p>{result.total_for_filter} واحد</p>
        {result.units.length === 0 && (filters.unprinted_only && hasUnfiltered ? <p>
          همه واحدها چاپ شده‌اند. <button type="button" className="underline" onClick={() => changeFilters({ unprinted_only: false })}>نمایش واحدهای چاپ‌شده</button>
        </p> : <p>بدون واحد</p>)}
        {result.units.length > 0 && <>
          <div className="flex flex-wrap gap-3">
            <button type="button" className="underline" onClick={() => choose(result.units.slice(-PAGE_LIMIT))}>انتخاب همه (صفحه)</button>
            <button type="button" className="underline" onClick={() => void more(true)}>انتخاب همه ({result.total_for_filter})</button>
          </div>
          <ul className="space-y-1">
            {result.units.map(row => <li key={row.code}><label className="flex flex-wrap gap-2">
              <input type="checkbox" checked={selected.some(unit => unit.code === row.code)} onChange={event => {
                if (event.target.checked && selected.length === MAX_PRINT) {
                  setMessage('برای هر درخواست حداکثر ۵۰۰ واحد انتخاب کنید.');
                  return;
                }
                const codes = new Set(selected.map(unit => unit.code));
                if (event.target.checked) codes.add(row.code);
                else codes.delete(row.code);
                choose(result.units.filter(unit => codes.has(unit.code)));
              }} />
              <code dir="ltr">{row.code}</code> {row.variant_name} · {UNIT_STATUS_LABELS[row.status as UnitStatus]}
              {row.print_count > 0 && <span>تعداد ثبت چاپ {row.print_count}{row.printed_at && <> · اولین ثبت چاپ <bdi>{row.printed_at}</bdi></>}</span>}
            </label></li>)}
          </ul>
          {result.next_cursor && <button type="button" className="underline" onClick={() => void more(false)}>واحدهای بیشتر</button>}
        </>}
      </>}
      <label className="block">تعداد برچسب‌های بعدی <input type="number" disabled={!filters.variant_id} min={1} max={MAX_PRINT} value={count || nextCount || ''} onChange={event => { job.clear(); setCount(event.target.value); }} className="w-20 rounded-lg border p-1" /></label>
      <button type="button" className="rounded-lg border p-2 disabled:opacity-50" disabled={!nextAllowed} onClick={() => void print({ variant_id: filters.variant_id, count: nextCount, unprinted_only: true })}>چاپ {nextCount || ''} برچسب بعدی</button>
      <p>{selected.length} انتخاب‌شده · {printed} دارای ثبت چاپ</p>
      <p>{PRINT_MARK_WARNING}</p>
      {reprint && <label className="block"><input type="checkbox" checked={confirmed} onChange={event => setConfirmed(event.target.checked)} /> می‌دانم این‌ها چاپ مجدد هستند</label>}
      <button data-unit-print type="button" className="rounded-lg bg-brand-deep p-2 text-white disabled:opacity-50" disabled={!codesToPrint.length || (reprint && !confirmed)} onClick={() => void print({ unit_codes: codesToPrint, ...(reprint ? { reprint: true } : {}) })}>
        {reprint ? 'چاپ مجدد همان کدها' : 'چاپ'}
      </button>
    </fieldset>
    {loading && <p role="status">در حال دریافت واحدها…</p>}
    {message && <p role="status">{message}</p>}
    {loadFailed && !loading && message !== SERIAL_UNAVAILABLE && <button type="button" onClick={() => setRefresh(value => value + 1)}>تلاش مجدد</button>}
    {job.failure && <p role="alert" className="text-rose-700">{job.failure.message}</p>}
    {shown.length > 0 && <UnitLabelSheet preview printable={job.units !== null} units={shown} printCountByCode={printCounts} />}
  </div>;
}
