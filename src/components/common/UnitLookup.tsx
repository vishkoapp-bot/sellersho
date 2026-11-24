import { normalizeUnitScan } from '../../utils/unitScan';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { ScanLine } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { ApiError } from '../../services/apiClient';
import { unitsApi, type ListUnitsParams, type ListUnitsResponse, type UnitLookupResponse } from '../../services/unitsApi';
import { UNIT_STATUS_LABELS, UNIT_STATUS_TONES, unitEventLabel, type UnitStatus } from '../../utils/unitStatus';
import { UNIT_CODE_PATTERN } from '../../vendor/code128';

interface UnitLookupProps {
  compact?: boolean;
  onSelect?: (unit: UnitLookupResponse) => void;
  getOrderHref?: (order: NonNullable<UnitLookupResponse['order']>) => string | undefined;
}

const NOT_FOUND = 'واحد یافت نشد.';
const UNAVAILABLE = 'قابلیت سریال واحد هنوز فعال نشده است.';
const UNIT_PREFIX = /^[0-9A-Z-]+-X[0-9]{0,3}$/;
const VARIANT_CODE = /^[0-9A-Z]+(?:-[0-9A-Z]+)*$/;
const BADGE_CLASSES = {
  slate: 'bg-slate-100 text-slate-700',
  green: 'bg-green-100 text-green-800',
  amber: 'bg-amber-100 text-amber-800',
  blue: 'bg-blue-100 text-blue-800',
  orange: 'bg-orange-100 text-orange-800',
  red: 'bg-red-100 text-red-800',
  grey: 'bg-gray-100 text-gray-800',
};

export function UnitStatusBadge({ status }: { status: UnitStatus }) {
  const tone = UNIT_STATUS_TONES[status];
  return <span data-unit-status className={`rounded-full px-2 py-1 text-xs ${BADGE_CLASSES[tone]}`}>{UNIT_STATUS_LABELS[status]}</span>;
}

function statusLabel(status: string | null): string {
  if (status === null) return '—';
  return Object.hasOwn(UNIT_STATUS_LABELS, status) ? UNIT_STATUS_LABELS[status as UnitStatus] : status;
}

function UnitResult({
  unit, onSelect, getOrderHref, onViewVariant,
}: Pick<UnitLookupProps, 'onSelect' | 'getOrderHref'> & {
  unit: UnitLookupResponse;
  onViewVariant: (variantId: string) => void;
}) {
  const { variant, product, location, printed, order } = unit;
  const orderHref = order && getOrderHref?.(order);
  return (
    <article className="space-y-3 rounded-xl border border-slate-200 p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <code dir="ltr" className="font-mono font-bold">{unit.code}</code>
        <UnitStatusBadge status={unit.status} />
      </div>
      <dl className="space-y-2">
        <div>
          <dt className="font-bold">محصول</dt>
          <dd>{product.title} <bdi dir="ltr" className="font-mono">{product.spn}</bdi></dd>
        </div>
        <div>
          <dt className="font-bold">تنوع</dt>
          <dd>{variant.name} <bdi dir="ltr" className="font-mono">{variant.spnc}</bdi></dd>
        </div>
        <div>
          <dt className="font-bold">موقعیت</dt>
          <dd>
            {unit.status === 'removed' ? UNIT_STATUS_LABELS.removed : location ? (
              <span className="flex flex-wrap gap-x-2">
                {location.warehouse_name && <span>{location.warehouse_name}</span>}
                {location.bin_code && <bdi dir="ltr">{location.bin_code}</bdi>}
                <bdi>{location.lot_status}</bdi>
                <bdi dir="ltr" className="break-all font-mono">{location.location_key}</bdi>
              </span>
            ) : '—'}
          </dd>
        </div>
        <div>
          <dt className="font-bold">چاپ</dt>
          <dd>{printed.printed_at ? (
            <>
              <div>اولین ثبت چاپ <bdi><time>{printed.printed_at}</time></bdi></div>
              <div>تعداد ثبت چاپ {printed.print_count}</div>
              {printed.last_printed_at && <div>آخرین ثبت چاپ <bdi><time>{printed.last_printed_at}</time></bdi></div>}
            </>
          ) : 'هنوز چاپ نشده'}</dd>
        </div>
        {order && (
          <div>
            <dt className="font-bold">سفارش</dt>
            <dd>
              {orderHref ? (
                <a href={orderHref} className="text-brand-deep underline" aria-label={`مشاهده سفارش ${order.order_number}`}>
                  <bdi>{order.order_number}</bdi>
                </a>
              ) : <bdi>{order.order_number}</bdi>}
              <div>ثبت تخصیص <bdi><time>{order.captured_at}</time></bdi></div>
              {order.restored_at && <div>بازگشت به موجودی <bdi><time>{order.restored_at}</time></bdi></div>}
            </dd>
          </div>
        )}
      </dl>
      {unit.history.length > 0 && (
        <div>
          <h3 className="font-bold">تاریخچه</h3>
          <ol className="divide-y divide-slate-100 border-s border-slate-200 ps-3">
            {unit.history.map((event, index) => (
              <li key={index} className="space-y-1 py-2">
                <strong>{unitEventLabel(event.event_type)}</strong>
                <div>{statusLabel(event.from_status)} ← {statusLabel(event.to_status)}</div>
                <div className="flex flex-wrap gap-x-2 text-xs text-slate-600">
                  <bdi>{event.reference_type}</bdi>
                  {event.reference_id && <bdi>{event.reference_id}</bdi>}
                  {event.actor_user_id && <span>کاربر <bdi>{event.actor_user_id}</bdi></span>}
                  <bdi><time>{event.created_at}</time></bdi>
                </div>
              </li>
            ))}
          </ol>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        {onSelect && <button type="button" className="rounded-lg bg-brand-soft px-3 py-2 text-brand-deep" onClick={() => onSelect(unit)}>چاپ برچسب این واحد</button>}
        <button type="button" className="rounded-lg bg-slate-100 px-3 py-2" onClick={() => onViewVariant(variant.id)}>مشاهده واحدهای همین تنوع</button>
      </div>
    </article>
  );
}

export function UnitLookup({ compact = false, onSelect, getOrderHref }: UnitLookupProps) {
  const { error: toastError } = useToast();
  const [input, setInput] = useState('');
  const [recent, setRecent] = useState<string[]>([]);
  const [unit, setUnit] = useState<UnitLookupResponse | null>(null);
  const [list, setList] = useState<(ListUnitsResponse & { filters: ListUnitsParams }) | null>(null);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const requestVersion = useRef(0);

  useEffect(() => () => { requestVersion.current += 1; }, []);

  async function lookup(code: string, filters?: ListUnitsParams) {
    const version = ++requestVersion.current;
    setUnit(null);
    setList(null);
    setMessage('');
    setBusy(false);

    if (!filters && !UNIT_CODE_PATTERN.test(code)) {
      if (VARIANT_CODE.test(code)) {
        setMessage(`${NOT_FOUND} این کد تنوع است، نه کد واحد — کد واحد به شکل SPNC-XNNNN است.`);
      } else {
        setMessage('قالب کد واحد درست نیست');
      }
      return;
    }

    setBusy(true);
    try {
      if (filters) {
        const result = await unitsApi.listUnits(filters);
        if (version !== requestVersion.current) return;
        if (result.units.length) setList({ ...result, filters });
        else setMessage(filters.q ? 'واحدی با این پیشوند یافت نشد.' : 'واحدی برای این تنوع یافت نشد.');
      } else {
        const result = await unitsApi.getUnit(code);
        if (version !== requestVersion.current) return;
        setUnit(result);
        setRecent(previous => [result.code, ...previous.filter(value => value !== result.code)].slice(0, 10));
      }
    } catch (error) {
      if (version !== requestVersion.current) return;
      if (error instanceof ApiError && (error.status === 501 || (filters && error.status === 404))) {
        setMessage(UNAVAILABLE);
      } else if (error instanceof ApiError && error.status === 404) {
        setMessage(NOT_FOUND);
      } else if (!(error instanceof ApiError && error.status === 401)) {
        // Spec §1.1: surface the server `error` code verbatim in the toast.
        // ApiError.message is now a mapped Persian string (SUP-8), so read the
        // raw code from the response body when the server sent one.
        const rawCode = error instanceof ApiError && error.body && typeof error.body === 'object' && typeof error.body.error === 'string'
          ? error.body.error
          : null;
        toastError(rawCode ?? (error instanceof Error ? error.message : 'جستجوی واحد انجام نشد.'));
      }
    } finally {
      if (version === requestVersion.current) setBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const value = normalizeUnitScan(input);
    void lookup(value, UNIT_PREFIX.test(value) ? { q: value } : undefined);
  }

  function selectCode(code: string) {
    setInput(code);
    void lookup(code);
  }

  return (
    <section dir="rtl" className={`space-y-3 ${compact ? '' : 'rounded-xl border border-slate-200 p-4'}`}>
      <form className="flex flex-wrap items-end gap-2" onSubmit={submit}>
        <label className="min-w-0 flex-1 text-sm font-bold">
          کد واحد
          <span className="mt-1 flex items-center gap-2 rounded-lg border border-slate-300 px-3">
            <ScanLine aria-hidden="true" className="size-4 shrink-0 text-slate-500" />
            <input
              type="text"
              dir="ltr"
              className="min-w-0 flex-1 py-2 font-mono"
              value={input}
              onChange={event => setInput(event.target.value)}
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
            />
          </span>
        </label>
        <button type="submit" className="rounded-lg bg-brand-soft px-3 py-2 text-sm text-brand-deep">جستجو</button>
      </form>
      {(busy || message) && <p role="status" className="text-sm">{busy ? 'در حال جستجو…' : message}</p>}
      {unit && <UnitResult unit={unit} onSelect={onSelect} getOrderHref={getOrderHref} onViewVariant={variant_id => void lookup('', { variant_id })} />}
      {list && (
        <>
          <ul className="divide-y divide-slate-100 text-sm">
            {list.units.map(row => (
              <li key={row.code} className="flex flex-wrap items-center gap-2 py-2">
                <button type="button" dir="ltr" className="font-mono text-brand-deep underline" onClick={() => selectCode(row.code)}>{row.code}</button>
                <UnitStatusBadge status={row.status} />
              </li>
            ))}
          </ul>
          {list.next_cursor && (
            <button type="button" className="rounded-lg bg-slate-100 px-3 py-2 text-sm" onClick={() => void lookup('', { ...list.filters, cursor: list.next_cursor! })}>صفحه بعد</button>
          )}
        </>
      )}
      {recent.length > 0 && (
        <div className="space-y-1 text-xs">
          <p>جستجوهای اخیر</p>
          <ul className="flex flex-wrap gap-2">
            {recent.map(code => (
              <li key={code}>
                <button type="button" dir="ltr" className="rounded-md bg-slate-100 px-2 py-1 font-mono" onClick={() => selectCode(code)}>{code}</button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
}
