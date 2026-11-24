import { useState } from 'react';
import { wmsApi, type UnitReceiptResult } from '../../services/wmsApi';
import { unitsApi } from '../../services/unitsApi';
import { completeScan, unitMutationError, type ScannedUnit } from '../../utils/unitScan';
import { UnitScanField } from '../common/UnitScanField';
import { UnitStatusBadge } from '../common/UnitLookup';
import { CustomerReturnBinEntry } from './CustomerReturnBinEntry';

interface ReturnItem {
  order_item_id?: string;
  orderItemId?: string;
  qty?: number;
  quantity?: number;
  productTitle?: string;
}
interface ReturnLine {
  id: string;
  quantity: number;
  title?: string;
}

export function WarehouseReturnReceipt({ returnId, orderId, items }: { returnId: string; orderId?: string; items: ReturnItem[] }) {
  const [scans, setScans] = useState<Record<string, ScannedUnit[]>>({});
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<UnitReceiptResult[] | null>(null);
  const [outcomes, setOutcomes] = useState<Record<string, ScannedUnit['status']>>({});
  const [message, setMessage] = useState('');
  const lines = items.flatMap<ReturnLine>(item => {
    const id = item.order_item_id ?? item.orderItemId;
    const quantity = item.qty ?? item.quantity;
    return id && quantity && quantity > 0 ? [{ id, quantity, title: item.productTitle }] : [];
  });
  if (!lines.length) return null;
  const incomplete = lines.length !== items.length;

  async function submit() {
    if (busy || incomplete || Object.values(pending).some(Boolean)) return;
    const payload = lines.map(({ id, quantity }) => {
      const lineScans = scans[id] ?? [];
      return {
        order_item_id: id,
        qty: quantity,
        ...(lineScans.length ? { unit_codes: lineScans.map(unit => unit.code) } : {}),
      };
    });
    if (payload.some(line => !Number.isSafeInteger(line.qty) || line.qty < 1 ||
      !completeScan(scans[line.order_item_id] ?? [], line.qty))) {
      setMessage('تعداد هر قلم و تعداد واحدهای اسکن‌شده را بررسی کنید.');
      return;
    }
    setBusy(true);
    setMessage('');
    try {
      const data = await wmsApi.receiveReturn(returnId, { items: payload });
      const receipts = data && 'line_id' in data ? [data] : data?.items ?? [];
      setResult(receipts);
      await Promise.all(receipts.flatMap(line => (line.unit_codes ?? []).map(async code => {
        try {
          const unit = await unitsApi.getUnit(code);
          setOutcomes(current => ({ ...current, [code]: unit.status }));
        } catch { /* The receipt remains recorded if its status cannot be refreshed. */ }
      })));
    } catch (cause) {
      setMessage(unitMutationError(cause, 'ثبت دریافت مرجوعی انجام نشد.'));
    } finally { setBusy(false); }
  }

  if (result) return <div className="space-y-2">
    <p role="status">دریافت مرجوعی ثبت شد.</p>
    {result.map(line => <ul key={line.line_id}>{line.unit_codes?.map(code => <li key={code} className="flex gap-2">
      <code dir="ltr">{code}</code>
      {outcomes[code] && <UnitStatusBadge status={outcomes[code]} />}
    </li>)}</ul>)}
    <CustomerReturnBinEntry returnID={returnId} />
  </div>;

  return <div className="space-y-2">
    {lines.map(line => {
      return <div key={line.id} className="space-y-2">
        <label>{line.title || 'تعداد دریافتی'} <input type="number" value={line.quantity} readOnly className="rounded-lg border p-1" /></label>
        <UnitScanField scans={scans[line.id] ?? []} quantity={line.quantity} disabled={busy || incomplete || !orderId}
          onChange={units => setScans(current => ({ ...current, [line.id]: units }))}
          onPending={value => setPending(current => ({ ...current, [line.id]: value }))}
          validate={unit => unit.order?.order_id !== orderId ? 'این واحد متعلق به سفارش دیگری است.' :
            unit.status !== 'sold' ? 'این واحد در وضعیت فروخته‌شده نیست.' : undefined} />
      </div>;
    })}
    {incomplete && <p role="alert" className="text-rose-700">اطلاعات همه اقلام مرجوعی برای ثبت دریافت کامل نیست.</p>}
    {message && <p role="alert" className="text-rose-700">{message}</p>}
    <button type="button" disabled={busy || incomplete || Object.values(pending).some(Boolean)} onClick={() => void submit()}
      className="rounded-lg bg-brand-deep p-2 text-xs text-white disabled:opacity-50">ثبت دریافت مرجوعی</button>
  </div>;
}
