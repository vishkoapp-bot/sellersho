import { useState } from 'react';
import { PackageCheck } from 'lucide-react';
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
  product_title?: string;
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
    return id && quantity && quantity > 0 ? [{ id, quantity, title: item.productTitle ?? item.product_title }] : [];
  });
  if (!lines.length) return <p role="alert" className="text-sm text-amber-900">اطلاعات اقلام برای ثبت دریافت در دسترس نیست.</p>;
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

  if (result) return <div className="space-y-3">
    <p role="status" className="rounded-xl bg-emerald-50 p-3 text-sm font-bold text-emerald-900">دریافت مرجوعی ثبت شد.</p>
    {result.map(line => line.unit_codes?.length > 0 && <ul key={line.line_id} className="space-y-2 rounded-xl bg-brand-bg p-3">
      {line.unit_codes.map(code => <li key={code} className="flex flex-wrap items-center gap-2 text-sm text-slate-700">
        <code dir="ltr" className="break-all">{code}</code>
        {outcomes[code] && <UnitStatusBadge status={outcomes[code]} />}
      </li>)}
    </ul>)}
    <CustomerReturnBinEntry returnID={returnId} />
  </div>;

  return <div className="space-y-3">
    {lines.map(line => (
      <div key={line.id} className="space-y-3 rounded-xl border border-brand-border bg-brand-bg p-3">
        {line.title && <p className="break-words text-sm font-bold text-slate-900">{line.title}</p>}
        <label className="flex flex-wrap items-center justify-between gap-2 text-sm font-medium text-slate-700">
          تعداد دریافتی
          <input type="number" value={line.quantity} readOnly dir="ltr" className="min-h-11 w-24 rounded-xl border border-slate-300 bg-white px-3 text-center" />
        </label>
        <UnitScanField scans={scans[line.id] ?? []} quantity={line.quantity} disabled={busy || incomplete || !orderId}
          onChange={units => setScans(current => ({ ...current, [line.id]: units }))}
          onPending={value => setPending(current => ({ ...current, [line.id]: value }))}
          validate={unit => unit.order?.order_id !== orderId ? 'این واحد متعلق به سفارش دیگری است.' :
            unit.status !== 'sold' ? 'این واحد در وضعیت فروخته‌شده نیست.' : undefined} />
      </div>
    ))}
    {incomplete && <p role="alert" className="text-sm text-rose-800">اطلاعات همه اقلام مرجوعی برای ثبت دریافت کامل نیست.</p>}
    {message && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{message}</p>}
    <button type="button" disabled={busy || incomplete || Object.values(pending).some(Boolean)} onClick={() => void submit()}
      className="inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60 sm:w-auto">
      <PackageCheck aria-hidden="true" className="size-4" />{busy ? 'در حال ثبت…' : 'ثبت دریافت مرجوعی'}
    </button>
  </div>;
}
