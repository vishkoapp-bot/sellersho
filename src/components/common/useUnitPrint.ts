import { useEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { ApiError, tokenStore } from '../../services/apiClient';
import type { PrintLabelPayload } from '../../services/unitsApi';
import { UNIT_STATUS_LABELS, type UnitStatus } from '../../utils/unitStatus';

export const SERIAL_UNAVAILABLE = 'قابلیت سریال واحد هنوز فعال نشده است.';
export const PRINT_MARK_WARNING = 'چاپ، برچسب‌ها را به‌عنوان چاپ‌شده ثبت می‌کند؛ اگر چاپ انجام نشد، با «چاپ مجدد» همان کدها را دوباره چاپ کنید.';

type PrintRequest = (key: string) => Promise<{ units: PrintLabelPayload[] }>;

export function useUnitPrint() {
  const [busy, setBusy] = useState(false);
  const [units, setUnits] = useState<PrintLabelPayload[] | null>(null);
  const [failure, setFailure] = useState<{ message: string; conflict: boolean; codes: string[]; retryable: boolean } | null>(null);
  const inFlight = useRef(false);
  const retry = useRef<{ intent: string; key: string; generation: number; post: PrintRequest } | null>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  function clear() {
    retry.current = null;
    setUnits(null);
    setFailure(null);
  }

  async function print(intent: string, post: PrintRequest) {
    if (inFlight.current) return;
    const generation = tokenStore.getGeneration();
    inFlight.current = true;
    setBusy(true);
    setFailure(null);
    setUnits(null);
    try {
      const pending = retry.current?.intent === intent && retry.current.generation === generation
        ? retry.current : { intent, key: crypto.randomUUID(), generation, post };
      retry.current = pending;
      const result = await pending.post(pending.key);
      retry.current = null;
      if (!mounted.current || tokenStore.getGeneration() !== generation) return;
      flushSync(() => setUnits(result.units));
      if (result.units.length) window.print();
      return result.units;
    } catch (cause) {
      const uncertainResponse = cause instanceof ApiError && (cause.status === 0 || cause.status === 408 || cause.status >= 500);
      if (!uncertainResponse) retry.current = null;
      if (!mounted.current || tokenStore.getGeneration() !== generation) return;
      const body = cause instanceof ApiError ? cause.body : null;
      const errors: { code: string; error: string; status?: UnitStatus }[] = Array.isArray(body?.errors) ? body.errors : [];
      const conflict = body?.error === 'already_printed_in_job';
      let message = typeof body?.error === 'string' ? body.error : cause instanceof Error ? cause.message : 'ثبت چاپ انجام نشد.';
      if (body?.error === 'receipt_line_not_found') message = 'این ردیف برای چاپ در دسترس نیست. جزئیات دریافت را به‌روزرسانی کنید.';
      else if (cause instanceof ApiError && [404, 501].includes(cause.status)) message = SERIAL_UNAVAILABLE;
      if (body?.error === 'idempotency_key_mismatch') message = 'شناسه درخواست چاپ با انتخاب ثبت‌شده مطابقت ندارد (idempotency_key_mismatch).';
      if (conflict) message = 'این کدها قبلاً در درخواست دیگری ثبت شده‌اند. انتخاب را تغییر دهید یا همین کدها را دوباره چاپ کنید (already_printed_in_job).';
      if (errors.length) message += ' — ' + errors.map(item => `${item.code}: ${(item.status && UNIT_STATUS_LABELS[item.status]) || item.error}`).join('، ');
      setFailure({ message, conflict, codes: body?.error === 'unit_not_printable' ? errors.map(item => item.code) : [], retryable: uncertainResponse });
    } finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  return { busy, units, failure, clear, print, hideSheet: () => setUnits(null) };
}
