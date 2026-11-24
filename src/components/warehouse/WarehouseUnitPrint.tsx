import { useState } from 'react';
import { wmsApi } from '../../services/wmsApi';
import { UnitLabelSheet } from '../common/UnitLabelSheet';
import { UnitLookup } from '../common/UnitLookup';
import { PRINT_MARK_WARNING, SERIAL_UNAVAILABLE, useUnitPrint } from '../common/useUnitPrint';

export function WarehouseUnitPrint({ receiptLineId, unitCodes = [] }: { receiptLineId: string; unitCodes?: string[] }) {
  const job = useUnitPrint();
  const [selection, setSelection] = useState<string[]>([]);
  const [selectionPrinted, setSelectionPrinted] = useState(false);
  const [lookupMode, setLookupMode] = useState<'pick' | 'reprint' | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const [validation, setValidation] = useState('');
  const reprint = selectionPrinted || job.failure?.conflict === true;

  function print(codes?: string[]) {
    const request = codes?.length ? { unit_codes: codes, ...(reprint ? { reprint: true } : {}) } : { receipt_line_id: receiptLineId };
    void job.print(JSON.stringify(request), idempotency_key => wmsApi.printUnits({ ...request, idempotency_key }));
  }

  function openLookup(mode: typeof lookupMode) {
    const knownPrinted = mode === 'reprint' && job.units?.length === 0 ? unitCodes : [];
    job.clear();
    setLookupMode(mode);
    setSelection(knownPrinted);
    setSelectionPrinted(knownPrinted.length > 0);
    setConfirmed(false);
    setValidation('');
  }

  if (job.failure?.message === SERIAL_UNAVAILABLE) return <p role="status">{SERIAL_UNAVAILABLE}</p>;

  return <div className="space-y-2 text-xs">
    <p>{PRINT_MARK_WARNING}</p>
    <div className="flex flex-wrap gap-2">
      <button type="button" disabled={job.busy} onClick={() => print()} className="rounded-lg bg-brand-soft p-2 text-brand-deep disabled:opacity-50">چاپ برچسب واحد</button>
      <button type="button" disabled={job.busy} onClick={() => openLookup('pick')} className="underline">انتخاب کد برای چاپ</button>
      <button type="button" disabled={job.busy} onClick={() => openLookup('reprint')} className="underline">چاپ مجدد برچسب واحد</button>
    </div>
    {job.failure && <p role="alert" className="text-rose-700">{job.failure.message}</p>}
    {job.units?.length === 0 && <p role="status">همه واحدهای این خط چاپ شده‌اند. <button type="button" onClick={() => openLookup('reprint')} className="underline">چاپ مجدد</button></p>}
    {lookupMode && <div className="space-y-2 rounded-lg border p-2">
      <UnitLookup compact onSelect={unit => {
        if (job.busy) return;
        if (lookupMode === 'reprint' && !unit.printed.printed_at) {
          setValidation('این واحد هنوز ثبت چاپ ندارد.');
          return;
        }
        setValidation('');
        job.clear();
        setSelection([unit.code]);
        setSelectionPrinted(!!unit.printed.printed_at);
        setConfirmed(false);
      }} />
      <div>{selection.map(code => <code dir="ltr" className="me-2" key={code}>{code}</code>)}</div>
      {validation && <p role="alert">{validation}</p>}
      {reprint && <label><input type="checkbox" checked={confirmed} onChange={event => setConfirmed(event.target.checked)} /> می‌دانم این‌ها چاپ مجدد هستند</label>}
      <button type="button" disabled={!selection.length || (reprint && !confirmed) || job.busy} onClick={() => print(selection)} className="ms-2 underline disabled:opacity-50">
        {reprint ? 'چاپ مجدد همان کدها' : 'چاپ کد انتخاب‌شده'}
      </button>
      <button type="button" disabled={job.busy} className="ms-2 underline" onClick={() => openLookup(null)}>بستن</button>
    </div>}
    {!!job.units?.length && <div className="fixed inset-0 z-[80] overflow-auto bg-slate-100 p-3" role="dialog" aria-label="برچسب‌های ثبت‌شده">
      <div className="flex gap-2">
        <button type="button" disabled={job.busy} onClick={() => openLookup(null)} className="rounded-lg border bg-white p-2">بستن برچسب‌ها</button>
        <button type="button" disabled={job.busy} onClick={() => {
          const codes = job.units?.map(unit => unit.code) ?? [];
          openLookup('reprint');
          setSelection(codes);
          setSelectionPrinted(true);
        }} className="rounded-lg border bg-white p-2">چاپ مجدد همین برچسب‌ها</button>
      </div>
      <UnitLabelSheet preview units={job.units} />
    </div>}
  </div>;
}
