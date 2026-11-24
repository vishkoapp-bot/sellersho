import { useState } from 'react';
import { wmsApi, type UnitReceiptResult } from '../../services/wmsApi';
import { completeScan, unitMutationError, type ScannedUnit } from '../../utils/unitScan';
import { UnitScanField } from '../common/UnitScanField';
import { WarehouseUnitPrint } from './WarehouseUnitPrint';

export function WarehouseInboundLineReceipt({ lineId, expected }: { lineId: string; expected: number }) {
  const [quantity, setQuantity] = useState(String(expected));
  const [scans, setScans] = useState<ScannedUnit[]>([]);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<UnitReceiptResult | null>(null);
  const [message, setMessage] = useState('');
  const value = Number(quantity);

  async function receive() {
    if (busy) return;
    if (!Number.isSafeInteger(value) || value < 1 || !completeScan(scans, value)) {
      setMessage('تعداد دریافتی و تعداد واحدهای اسکن‌شده را بررسی کنید.');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      const receipt = await wmsApi.receiveInboundLine(lineId, {
        received_quantity: value,
        ...(scans.length ? { unit_codes: scans.map(unit => unit.code) } : {}),
      });
      setResult(receipt);
    } catch (cause) {
      setMessage(unitMutationError(cause, 'ثبت دریافت انجام نشد.'));
    } finally { setBusy(false); }
  }

  return <div className="space-y-2 rounded-lg border p-2 text-xs">
    <p>ردیف ورودی <bdi>{lineId}</bdi></p>
    {result ? <>
      <p role="status">دریافت {result.quantity} واحد ثبت شد.</p>
      <div>{result.unit_codes?.map(code => <code key={code} dir="ltr" className="me-2">{code}</code>)}</div>
      <WarehouseUnitPrint receiptLineId={result.line_id} unitCodes={result.unit_codes} />
    </> : <>
      <label>تعداد دریافتی <input type="number" min={1} value={quantity} disabled={busy}
        onChange={event => setQuantity(event.target.value)} className="rounded-lg border p-1" /></label>
      <UnitScanField preprinted scans={scans} quantity={Math.max(0, value || 0)} onChange={setScans} disabled={busy} />
      {message && <p role="alert" className="text-rose-700">{message}</p>}
      <button type="button" disabled={busy} onClick={() => void receive()}
        className="rounded-xl bg-blue-600 px-4 py-2 text-white disabled:opacity-50">ثبت دریافت ردیف</button>
    </>}
  </div>;
}
