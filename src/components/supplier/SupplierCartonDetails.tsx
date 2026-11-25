import { useCallback, useState } from 'react';
import { Printer } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { supplierCartonApi, type SupplierCartonLine } from '../../services/supplierCartonApi';
import { UnitLabelSheet } from '../common/UnitLabelSheet';
import { PRINT_MARK_WARNING, useUnitPrint } from '../common/useUnitPrint';
import { SupplierResourceState } from './SupplierResourceState';
import { useSupplierResource } from './useSupplierResource';

const buttonClass = 'inline-flex min-h-11 items-center gap-1.5 rounded-xl border border-[#dfd8ef] bg-white px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';
const unitPageSize = 50;
const maxPrint = 500;

function receiptNotes(line: SupplierCartonLine, status: string, quantity: (amount: number) => string) {
  if (status === 'cancelled' && !line.received_qty) return ['این ردیف برای دریافت باز نیست.'];
  const notes: string[] = [];
  const outstanding = line.expected_qty - line.received_qty - line.cancelled_qty;
  if (line.qc_shortage) notes.push(`کسری ثبت‌شده ${quantity(line.qc_shortage)} واحد`);
  if (line.qc_failed_qty) notes.push(`ردشده در کنترل کیفیت ${quantity(line.qc_failed_qty)} واحد`);
  if (line.qc_failed_qty === null && line.qc_status === 'failed') notes.push('رد در کنترل کیفیت ثبت شده است.');
  if (line.qc_reason && (line.qc_shortage || line.qc_failed_qty)) notes.push(line.qc_reason);
  if (line.cancelled_qty) notes.push(`لغوشده ${quantity(line.cancelled_qty)} واحد`);
  if (line.received_qty > line.expected_qty) notes.push(`مازاد دریافت ${quantity(line.received_qty - line.expected_qty)} واحد`);
  if (outstanding > 0 && status !== 'draft' && status !== 'cancelled' && !line.qc_shortage) {
    if (['sent', 'created', 'in_transit', 'partially_received'].includes(status)) {
      notes.push(`${quantity(outstanding)} واحد در انتظار ثبت دریافت`);
    } else {
      notes.push(`تعداد دریافت‌شده ${quantity(outstanding)} واحد کمتر از تعداد اعلام‌شده است.`);
    }
  }
  if (line.putaway_qty === null) notes.push('چیدمان این ردیف در سوابق قدیمی قابل تأیید نیست.');
  else if (line.received_qty > line.putaway_qty && status !== 'cancelled') {
    const pendingQC = line.received_qty - (line.qc_passed_qty ?? 0) - (line.qc_failed_qty ?? 0);
    const pendingPlacement = (line.qc_passed_qty ?? 0) - line.putaway_qty;
    if (pendingQC > 0 && (line.qc_failed_qty !== null || line.qc_status !== 'failed')) notes.push(`${quantity(pendingQC)} واحد در انتظار تکمیل کنترل کیفیت انبار`);
    if (pendingPlacement > 0) notes.push(`${quantity(pendingPlacement)} واحد تأییدشده در انتظار چیدمان در انبار`);
  }
  return notes;
}

export function SupplierCartonDetails({ shipmentId }: { shipmentId: string }) {
  const { toPersianDigits } = useStore();
  const load = useCallback(() => supplierCartonApi.details(shipmentId), [shipmentId]);
  const detail = useSupplierResource(load);
  const [printLineId, setPrintLineId] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [visibleUnits, setVisibleUnits] = useState(unitPageSize);
  const [printNote, setPrintNote] = useState('');
  const job = useUnitPrint();
  const carton = detail.data;
  const format = (n: number) => toPersianDigits(n.toLocaleString('fa-IR'));

  function selectLine(line: SupplierCartonLine) {
    job.clear();
    setSelected([]);
    setVisibleUnits(unitPageSize);
    setPrintNote('');
    setPrintLineId(printLineId === line.id ? '' : line.id ?? '');
  }

  async function print(line: SupplierCartonLine) {
    if (!line.id || selected.length === 0 || job.busy) return;
    const codes = line.units.filter(unit => selected.includes(unit.code)).map(unit => unit.code);
    if (codes.length !== selected.length) return;
    const lineId = line.id;
    const reprint = job.failure?.conflict === true || line.units.some(unit => unit.print_count > 0 && codes.includes(unit.code));
    const intent = JSON.stringify([shipmentId, lineId, codes, reprint]);
    setPrintNote('');
    const result = await job.print(intent, key => supplierCartonApi.printUnits(shipmentId, {
      line_id: lineId, unit_codes: codes, idempotency_key: key, reprint,
    }));
    if (result?.length) {
      setPrintNote('درخواست چاپ ثبت شد. اگر چاپگر برچسب‌ها را تحویل نداد، همان کدها را دوباره چاپ کن.');
      void detail.reload();
    }
  }

  return (
    <section aria-label="جزئیات کارتن" className="space-y-3 rounded-xl border border-brand-border bg-white p-3 sm:p-4">
      <SupplierResourceState loading={detail.loading} error={detail.error} retry={detail.reload} />
      {carton && !detail.loading && !detail.error && <>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h4 className="text-sm font-extrabold text-[#261b41]">محتویات کارتن و رسید انبار</h4>
            {(carton.warehouse_name || carton.received_at) && <p className="mt-1 text-xs leading-6 text-slate-600">
              {carton.warehouse_name && <span>مقصد {carton.warehouse_name}</span>}
              {carton.warehouse_name && carton.received_at && ' · '}
              {carton.received_at && <span>اولین ثبت دریافت {new Date(carton.received_at).toLocaleDateString('fa-IR', { timeZone: 'Asia/Tehran' })}</span>}
            </p>}
          </div>
          <button type="button" disabled={job.busy} onClick={() => { job.clear(); setPrintNote(''); void detail.reload(); }} className={buttonClass}>به‌روزرسانی رسید</button>
        </div>
        {!carton.lines.length && <p className="text-sm text-slate-600">اقلام این کارتن در سوابق ثبت نشده‌اند.</p>}
        {carton.lines.map(line => {
          const status = carton.status;
          const notes = receiptNotes(line, status, format);
          const expanded = !!line.id && line.id === printLineId;
          const variantTitle = line.variant_name || line.product_title;
          const reprint = expanded && (job.failure?.conflict || line.units.some(unit => unit.print_count > 0 && selected.includes(unit.code)));
          return (
            <div key={line.variant_id} className="space-y-2 rounded-xl border border-slate-200 bg-slate-50/70 p-3">
              <div className="flex flex-wrap items-baseline justify-between gap-1.5">
                <div><p className="text-sm font-bold text-slate-900">{line.product_title}</p>{line.variant_name && <p className="mt-0.5 text-xs text-slate-600">{line.variant_name}</p>}</div>
                {line.spnc && <bdi dir="ltr" className="rounded-lg bg-white px-2 py-1 font-mono text-xs font-bold text-brand-deep">SPNC {line.spnc}</bdi>}
              </div>
              <dl className="grid grid-cols-3 gap-1.5 text-center">
                {([
                  ['تعداد اعلام‌شده', line.expected_qty],
                  ['تعداد دریافت‌شده', line.received_qty],
                  ['تعداد چیدمان‌شده', line.putaway_qty],
                ] as const).map(([label, quantity]) => (
                  <div key={label} className="rounded-lg bg-white px-1 py-2">
                    <dt className="text-[11px] leading-5 text-slate-600">{label}</dt>
                    <dd className="font-extrabold text-[#261b41]">{typeof quantity === 'number' ? format(quantity) : 'نامشخص'}</dd>
                  </div>
                ))}
              </dl>
              {notes.length > 0 && <ul className="space-y-1 text-xs leading-6 text-slate-700">
                {notes.map(note => <li key={note}>{note}</li>)}
              </ul>}
              {line.units.length > 0 && line.id ? (
                <button type="button" disabled={job.busy} className={buttonClass} aria-expanded={expanded} onClick={() => selectLine(line)}>
                  <Printer className="size-4" aria-hidden="true" />
                  {expanded ? 'بستن برچسب‌های واحد' : `دیدن برچسب‌های واحد ${variantTitle}`}
                </button>
              ) : status !== 'cancelled' && (
                <p className="text-xs leading-5 text-slate-600">
                  {line.source_kind === 'external_replenishment' && !line.received_qty ? 'انبار هنگام ثبت دریافت، کدهای SPNCX این ردیف را صادر می‌کند.' : 'کد SPNCX قابل چاپ برای این ردیف کارتن ثبت نشده است.'}
                </p>
              )}
              {expanded && <div className="space-y-2 rounded-lg border border-slate-200 bg-white p-3">
                <h5 className="text-xs font-bold text-slate-900">کدهای یکتای واحد <bdi dir="ltr">SPNCX</bdi></h5>
                <p className="text-xs leading-6 text-slate-600">{PRINT_MARK_WARNING}</p>
                <div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
                  {line.units.slice(0, visibleUnits).map(unit => {
                    const checked = selected.includes(unit.code);
                    return <label key={unit.code} className="flex min-h-11 items-center gap-2 rounded-lg bg-slate-50 px-2 text-xs">
                      <input type="checkbox" aria-label={`انتخاب ${unit.code}`} disabled={job.busy || (!checked && selected.length === maxPrint)} checked={checked}
                        onChange={event => {
                          const nextChecked = event.target.checked;
                          job.clear();
                          setPrintNote('');
                          setSelected(previous => nextChecked ? [...previous, unit.code] : previous.filter(code => code !== unit.code));
                        }} />
                      <bdi dir="ltr" className="break-all font-mono font-bold">{unit.code}</bdi>
                      {unit.print_count > 0 && <span className="text-slate-600">چاپ‌شده</span>}
                    </label>;
                  })}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <button type="button" className={buttonClass} onClick={() => void print(line)} disabled={job.busy || !selected.length}>
                    {job.busy ? 'در حال ثبت چاپ…' : reprint ? 'چاپ مجدد کدهای انتخاب‌شده' : 'چاپ کدهای انتخاب‌شده'}
                    {selected.length > 0 && <span>({format(selected.length)})</span>}
                  </button>
                  {line.units.length > visibleUnits && <button type="button" className={buttonClass} onClick={() => setVisibleUnits(current => current + unitPageSize)}>کدهای بیشتر</button>}
                </div>
                {selected.length === maxPrint && <p className="text-xs leading-5 text-slate-600">در هر درخواست می‌توانی حداکثر {format(maxPrint)} برچسب چاپ کنی.</p>}
                {job.failure && <p role="alert" className="rounded-lg bg-rose-50 p-2 text-sm text-rose-800">{job.failure.message}</p>}
                {printNote && <p role="status" className="text-xs leading-6 text-slate-700">{printNote}</p>}
              </div>}
            </div>
          );
        })}
      </>}
      {job.units && <UnitLabelSheet units={job.units} />}
    </section>
  );
}
