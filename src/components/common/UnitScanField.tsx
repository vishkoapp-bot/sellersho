import { useRef, useState } from 'react';
import { unitsApi, type UnitLookupResponse } from '../../services/unitsApi';
import { ApiError } from '../../services/apiClient';
import { normalizeUnitScan, type ScannedUnit } from '../../utils/unitScan';
import { UNIT_CODE_PATTERN } from '../../vendor/code128';
import { UnitStatusBadge } from './UnitLookup';

export function UnitScanField({ scans, quantity, onChange, disabled, validate, preprinted, onPending }: {
  scans: ScannedUnit[];
  quantity: number;
  onChange: (scans: ScannedUnit[]) => void;
  disabled?: boolean;
  validate?: (unit: UnitLookupResponse) => string | undefined;
  preprinted?: boolean;
  onPending?: (pending: boolean) => void;
}) {
  const [input, setInput] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const inputRef = useRef<HTMLInputElement>(null);

  async function scan() {
    if (pending.current || disabled) return;
    const code = normalizeUnitScan(input);
    if (!UNIT_CODE_PATTERN.test(code)) { setMessage('قالب کد واحد درست نیست'); return; }
    if (scans.some(unit => unit.code === code)) { setMessage('این واحد قبلاً اسکن شده است.'); return; }
    if (scans.length >= quantity) { setMessage('تعداد اسکن از تعداد این ردیف بیشتر است.'); return; }
    setMessage('');
    pending.current = true;
    setBusy(true);
    onPending?.(true);
    try {
      let unit: UnitLookupResponse | undefined;
      if (!preprinted) {
        try { unit = await unitsApi.getUnit(code); }
        catch (cause) {
          if (validate || !(cause instanceof ApiError && [404, 501].includes(cause.status))) throw cause;
        }
      }
      const validation = unit && validate?.(unit);
      if (validation) { setMessage(validation); return; }
      onChange([...scans, { code: unit?.code ?? code, status: unit?.status }]);
      setInput('');
    } catch (cause) {
      setMessage(cause instanceof ApiError && cause.status === 404 ? 'واحد یافت نشد.' : cause instanceof Error ? cause.message : 'بررسی کد انجام نشد.');
    } finally {
      pending.current = false;
      setBusy(false);
      onPending?.(false);
      inputRef.current?.focus();
    }
  }

  return <fieldset disabled={disabled} className="space-y-2 rounded-lg border border-slate-200 p-2 text-xs">
    <legend className="px-1">اسکن واحدها (اختیاری)</legend>
    <div className="flex gap-2">
      <input ref={inputRef} aria-label="اسکن کد واحد" dir="ltr" autoComplete="off" spellCheck={false}
        value={input} readOnly={busy} onChange={event => setInput(event.target.value)}
        onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); void scan(); } }}
        className="min-w-0 flex-1 rounded-lg border p-2 font-mono" />
      <button type="button" disabled={busy} onClick={() => void scan()} className="rounded-lg bg-slate-100 px-2">افزودن کد</button>
    </div>
    <p role="status">{scans.length} از {quantity} واحد اسکن شد</p>
    {message && <p role="alert" className="text-rose-700">{message}</p>}
    <ul>{scans.map(unit => <li key={unit.code} className="flex flex-wrap items-center gap-2">
      <code dir="ltr">{unit.code}</code>
      {unit.status && <UnitStatusBadge status={unit.status} />}
      <button type="button" disabled={busy} onClick={() => onChange(scans.filter(row => row.code !== unit.code))} aria-label={`حذف ${unit.code}`}>حذف</button>
    </li>)}</ul>
  </fieldset>;
}
