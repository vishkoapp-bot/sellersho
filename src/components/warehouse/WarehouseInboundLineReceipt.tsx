import { useRef, useState } from 'react';
import { wmsApi, type UnitReceiptResult } from '../../services/wmsApi';
import { completeScan, unitMutationError, type ScannedUnit } from '../../utils/unitScan';
import { UnitScanField } from '../common/UnitScanField';
import { WarehouseUnitPrint } from './WarehouseUnitPrint';

export function WarehouseInboundLineReceipt({ lineId, expected, title, productId, variantId, receipt, onReceived }: {
  lineId: string;
  expected: number;
  title?: string;
  productId?: string;
  variantId?: string;
  receipt?: UnitReceiptResult;
  onReceived: (result: UnitReceiptResult) => void;
}) {
  const [quantity, setQuantity] = useState(String(expected || ''));
  const [scans, setScans] = useState<ScannedUnit[]>([]);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(!receipt);
  const [message, setMessage] = useState('');
  const submitting = useRef(false);
  const value = Number(quantity);

  async function receive() {
    if (submitting.current) return;
    if (!Number.isSafeInteger(value) || value < 1 || value > 2147483647 || (expected > 0 && value > expected) ||
      (receipt && value <= receipt.quantity) || !completeScan(scans, value)) {
      setMessage('تعداد کل دریافتی و تعداد واحدهای اسکن‌شده را بررسی کنید.');
      return;
    }
    submitting.current = true;
    setBusy(true);
    setMessage('');
    try {
      const result = await wmsApi.receiveInboundLine(lineId, {
        received_quantity: value,
        ...(scans.length ? { unit_codes: scans.map(unit => unit.code) } : {}),
      });
      if (result?.line_id !== lineId || result.quantity !== value) throw new Error('پاسخ ثبت دریافت کامل نبود. همان تعداد را دوباره ثبت کنید.');
      setEditing(false);
      onReceived(result);
    } catch (cause) {
      setMessage(unitMutationError(cause, 'ثبت دریافت انجام نشد.'));
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }

  return (
    <article className="space-y-3 rounded-2xl border border-[#e6e2f0] p-3 text-sm sm:p-4">
      <div className="space-y-1">
        {title && <h4 className="font-extrabold text-[#261b41]">{title}</h4>}
        <p className="text-slate-700">ردیف ورودی <bdi dir="ltr" className="break-all font-mono font-bold">{lineId}</bdi></p>
        {productId && <p className="text-slate-600">شناسه کالا <bdi dir="ltr" className="break-all font-mono">{productId}</bdi></p>}
        {variantId && <p className="text-slate-600">SKU <bdi dir="ltr" className="break-all font-mono">{variantId}</bdi></p>}
        {expected > 0 && <p className="text-slate-600">{expected.toLocaleString('fa-IR')} واحد مورد انتظار</p>}
      </div>
      {receipt && (
        <div className="space-y-2 rounded-xl bg-emerald-50 p-3 text-emerald-900">
          <p role="status" className="font-bold">دریافت {receipt.quantity.toLocaleString('fa-IR')} واحد ثبت شد.</p>
          {!!receipt.unit_codes?.length && <ul className="flex flex-wrap gap-2">{receipt.unit_codes.map(code => <li key={code}><code dir="ltr" className="break-all">{code}</code></li>)}</ul>}
          <WarehouseUnitPrint receiptLineId={receipt.line_id} unitCodes={receipt.unit_codes} />
        </div>
      )}
      {editing ? (
        <form onSubmit={event => { event.preventDefault(); void receive(); }} className="space-y-3">
          <label className="block max-w-52 font-bold text-slate-700">
            تعداد کل دریافتی
            <input type="number" inputMode="numeric" min={receipt ? receipt.quantity + 1 : 1} max={expected || undefined} required
              value={quantity} disabled={busy} onChange={event => setQuantity(event.target.value)}
              className="mt-1 block min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base focus-visible:ring-2 focus-visible:ring-brand" />
          </label>
          <div className="[&_button]:min-h-11 [&_input]:min-h-11 [&_input]:text-base">
            <UnitScanField preprinted scans={scans} quantity={Math.max(0, value || 0)} onChange={setScans} disabled={busy} />
          </div>
          {message && <p role="alert" className="text-rose-800">{message}</p>}
          <button type="submit" disabled={busy}
            className="min-h-11 w-full rounded-xl bg-brand-deep px-4 py-2 font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50 sm:w-auto">{busy ? 'در حال ثبت…' : 'ثبت دریافت ردیف'}</button>
        </form>
      ) : receipt && receipt.quantity < expected && (
        <button type="button" onClick={() => {
          setQuantity(String(expected));
          setScans(previous => previous.length ? (receipt.unit_codes ?? []).map(code => ({ code })) : []);
          setEditing(true);
        }} className="min-h-11 rounded-xl border border-brand-border px-3 font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand">
          ثبت واحدهای باقی‌مانده
        </button>
      )}
    </article>
  );
}
