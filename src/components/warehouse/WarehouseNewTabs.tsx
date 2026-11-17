import React, { useState, useEffect } from 'react';
import { CustomerReturnBinEntry } from './CustomerReturnBinEntry';
import { wmsApi } from '../../services/wmsApi';
import { apiRequest } from '../../services/apiClient';
import { useToast } from '../../context/ToastContext';
import { RotateCcw, Undo2, ScanLine, CheckCircle2, XCircle } from 'lucide-react';

// Live contract (live bundles, read-only) for the receive tab:
// - GET /warehouse/shipments/{code} -> { shipment } (skuApi-MERGED.js, getShipmentByCode)
// - POST /warehouse/shipments/{code}/receive { items: [{ variant_id, received_qty,
//   qc_status }] } (skuApi-MERGED.js, receiveShipment)
// Shipment items render only server fields: variant_name, product_title, spnc,
// expected_qty. QC values are live's own "pass"/"fail"; the candidate backend
// stores them as "passed"/"failed" (see report).

interface ShipmentItem {
  variant_id: string;
  variant_name?: string;
  product_title?: string;
  spnc?: string;
  expected_qty?: number;
}

interface Shipment {
  shipment_code: string;
  supplier_name?: string;
  status?: string;
  items?: ShipmentItem[];
}

export const WarehouseReceiveTab: React.FC = () => {
  const { success, error } = useToast();
  const [code, setCode] = useState('');
  const [lookingUp, setLookingUp] = useState(false);
  const [shipment, setShipment] = useState<Shipment | null>(null);
  const [receivedQty, setReceivedQty] = useState<Record<string, string>>({});
  const [qc, setQc] = useState<Record<string, 'pass' | 'fail'>>({});
  const [confirmed, setConfirmed] = useState<Record<string, boolean>>({});
  const [submitting, setSubmitting] = useState(false);

  const alreadyReceived = shipment?.status === 'received' || shipment?.status === 'qc_passed';

  const lookup = async () => {
    const shipmentCode = code.trim().toUpperCase();
    if (!shipmentCode) { error('کد محموله را وارد کنید.'); return; }
    setLookingUp(true);
    try {
      const data = await apiRequest<{ shipment: Shipment }>(`/warehouse/shipments/${encodeURIComponent(shipmentCode)}`, { method: 'GET', auth: true });
      const found = data.shipment;
      setShipment(found);
      const qty: Record<string, string> = {};
      const qcState: Record<string, 'pass' | 'fail'> = {};
      const confirmedState: Record<string, boolean> = {};
      (found.items || []).forEach((item) => {
        qty[item.variant_id] = String(item.expected_qty ?? '');
        qcState[item.variant_id] = 'pass';
        confirmedState[item.variant_id] = false;
      });
      setReceivedQty(qty);
      setQc(qcState);
      setConfirmed(confirmedState);
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'محموله‌ای با این کد پیدا نشد.');
      setShipment(null);
    } finally {
      setLookingUp(false);
    }
  };

  const submit = async () => {
    if (!shipment) return;
    const items = (shipment.items || []).filter((item) => confirmed[item.variant_id]);
    if (items.length === 0) { error('حداقل یک قلم را با «تأیید اسکن» تأیید کنید.'); return; }
    const payload = items.map((item) => {
      const parsed = Number(receivedQty[item.variant_id]);
      return {
        variant_id: item.variant_id,
        received_qty: Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : 0,
        qc_status: qc[item.variant_id] || 'pass',
      };
    });
    setSubmitting(true);
    try {
      await apiRequest(`/warehouse/shipments/${encodeURIComponent(shipment.shipment_code)}/receive`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({ items: payload }),
      });
      success('دریافت محموله ثبت شد.');
      setShipment(null);
      setCode('');
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت دریافت انجام نشد.');
    } finally {
      setSubmitting(false);
    }
  };

  const confirmedCount = (shipment?.items || []).filter((item) => confirmed[item.variant_id]).length;

  return (
    <div className="space-y-3.5 pb-6">
      <section className="rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs space-y-2.5">
        <h3 className="text-xs font-extrabold text-slate-800 flex items-center gap-1.5">
          <ScanLine className="w-4 h-4 text-amber-600" strokeWidth={1.5} />
          <span>دریافت محموله تأمین‌کننده</span>
        </h3>
        <div className="flex gap-2">
          <input
            value={code}
            onChange={(e) => setCode(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void lookup(); }}
            placeholder="کد محموله روی کارتن (اسکن یا تایپ دستی)"
            className="flex-1 rounded-xl border border-slate-200 p-2.5 text-sm font-mono font-bold tracking-wider text-center focus:outline-hidden"
            dir="ltr"
          />
          <button
            type="button"
            onClick={() => void lookup()}
            disabled={lookingUp}
            className="px-4 py-2.5 rounded-xl bg-amber-600 text-white text-xs font-bold hover:bg-amber-700 disabled:opacity-50 flex items-center gap-1.5"
          >
            <ScanLine className="w-4 h-4" strokeWidth={1.5} />
            <span>اسکن</span>
          </button>
        </div>
      </section>

      {shipment && (
        <section className="space-y-3">
          <div className="rounded-2xl border-2 border-amber-200 bg-amber-50 p-3.5 flex items-center justify-between">
            <div>
              <span className="font-mono text-lg font-black text-slate-900 tracking-widest" dir="ltr">{shipment.shipment_code}</span>
              <span className="text-[11px] text-slate-600 block mt-0.5">تأمین‌کننده: {shipment.supplier_name}</span>
            </div>
            <span className="text-[10px] font-bold bg-white text-amber-700 border border-amber-200 px-2.5 py-1 rounded-full">
              {shipment.status}
            </span>
          </div>

          <div className="space-y-2">
            {(shipment.items || []).map((item) => {
              const isConfirmed = !!confirmed[item.variant_id];
              return (
                <div
                  key={item.variant_id}
                  className={`rounded-2xl border bg-white p-3 space-y-2.5 ${isConfirmed ? 'border-emerald-300 ring-1 ring-emerald-200' : 'border-slate-200/90'}`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <span className="text-xs font-extrabold text-slate-900 block truncate">{item.variant_name}</span>
                      <span className="text-[10px] text-slate-400 block truncate">{item.product_title}</span>
                      <span className="font-mono text-[11px] font-bold text-slate-600" dir="ltr">SPNC: {item.spnc}</span>
                    </div>
                    <span className="text-[11px] font-bold text-slate-600 shrink-0 bg-slate-100 px-2 py-1 rounded-lg">
                      مورد انتظار: {item.expected_qty}
                    </span>
                  </div>

                  <button
                    type="button"
                    disabled={alreadyReceived}
                    onClick={() => setConfirmed((current) => ({ ...current, [item.variant_id]: !current[item.variant_id] }))}
                    className={`w-full flex items-center justify-center gap-1.5 py-2.5 rounded-xl text-[11px] font-extrabold border-2 border-dashed transition-colors disabled:opacity-50 ${isConfirmed ? 'bg-emerald-50 text-emerald-700 border-emerald-400 border-solid' : 'bg-slate-50 text-slate-600 border-slate-300'}`}
                  >
                    <CheckCircle2 className="w-4 h-4" strokeWidth={1.5} />
                    <span>{isConfirmed ? 'اسکن تأیید شد — برای لغو بزنید' : 'تأیید اسکن و QC این قلم'}</span>
                  </button>

                  <div className="grid grid-cols-2 gap-2">
                    <label className="block">
                      <span className="text-[10px] font-bold text-slate-500 block mb-1">تعداد دریافتی</span>
                      <input
                        type="text"
                        inputMode="numeric"
                        min="0"
                        disabled={alreadyReceived}
                        value={receivedQty[item.variant_id] ?? ''}
                        onChange={(e) => setReceivedQty((current) => ({ ...current, [item.variant_id]: e.target.value }))}
                        className="w-full rounded-xl border border-slate-200 p-2 text-sm font-mono font-bold text-center disabled:bg-slate-50"
                        dir="ltr"
                      />
                    </label>
                    <div>
                      <span className="text-[10px] font-bold text-slate-500 block mb-1">نتیجه QC</span>
                      <div className="grid grid-cols-2 gap-1.5">
                        <button
                          type="button"
                          disabled={alreadyReceived}
                          onClick={() => setQc((current) => ({ ...current, [item.variant_id]: 'pass' }))}
                          className={`flex items-center justify-center gap-1 py-2 rounded-xl text-[11px] font-bold border transition-colors disabled:opacity-50 ${qc[item.variant_id] !== 'fail' ? 'bg-emerald-600 text-white border-emerald-600' : 'bg-white text-emerald-700 border-emerald-200'}`}
                        >
                          <CheckCircle2 className="w-3.5 h-3.5" strokeWidth={1.5} />
                          <span>سالم</span>
                        </button>
                        <button
                          type="button"
                          disabled={alreadyReceived}
                          onClick={() => setQc((current) => ({ ...current, [item.variant_id]: 'fail' }))}
                          className={`flex items-center justify-center gap-1 py-2 rounded-xl text-[11px] font-bold border transition-colors disabled:opacity-50 ${qc[item.variant_id] === 'fail' ? 'bg-rose-600 text-white border-rose-600' : 'bg-white text-rose-700 border-rose-200'}`}
                        >
                          <XCircle className="w-3.5 h-3.5" strokeWidth={1.5} />
                          <span>مردود</span>
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>

          {alreadyReceived ? (
            <p className="text-center text-[11px] text-slate-500 bg-white rounded-2xl border border-slate-200 p-3">
              این محموله قبلاً دریافت شده است.
            </p>
          ) : (
            <>
              <p className="text-center text-[10px] text-slate-500">فقط اقلامی که «تأیید اسکن» شده‌اند ثبت می‌شوند.</p>
              <button
                type="button"
                onClick={() => void submit()}
                disabled={submitting || confirmedCount === 0}
                className="w-full py-3 rounded-2xl bg-amber-600 text-white text-sm font-extrabold hover:bg-amber-700 disabled:opacity-50 flex items-center justify-center gap-1.5"
              >
                <span>{submitting ? 'در حال ثبت…' : `تأیید دریافت محموله (${confirmedCount} قلم تأییدشده)`}</span>
              </button>
            </>
          )}
        </section>
      )}
    </div>
  );
};

function useList(fetch: () => Promise<any[]>) {
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  useEffect(() => {
    let on = true;
    fetch()
      .then((list) => { if (on) setItems(list); })
      .catch((err: any) => { if (on) setLoadError(err?.message || 'خطا در دریافت داده'); })
      .finally(() => { if (on) setLoading(false); });
    return () => { on = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return { items, loading, loadError };
}

function ListShell({ title, icon, loading, loadError, empty, children }: {
  title: string; icon: React.ReactNode; loading: boolean; loadError: string | null; empty: boolean; children: React.ReactNode;
}) {
  return (
    <div className="space-y-3">
      <div className="bg-white rounded-2xl border border-slate-200 p-4 flex items-center gap-2">
        {icon}
        <h3 className="text-xs font-bold text-slate-800">{title}</h3>
      </div>
      {loading && <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">در حال بارگذاری…</div>}
      {loadError && <div className="bg-white rounded-2xl border border-rose-200 p-4 text-xs text-rose-600">{loadError}</div>}
      {!loading && !loadError && empty && (
        <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">موردی یافت نشد.</div>
      )}
      {!loading && !loadError && !empty && children}
    </div>
  );
}

const pick = (item: any, keys: string[]): string => {
  for (const k of keys) {
    const v = item?.[k];
    if (v !== undefined && v !== null && v !== '') return String(v);
  }
  return '—';
};

export const WarehouseCustomerReturnsTab: React.FC = () => {
  const { items, loading, loadError } = useList(() => wmsApi.getCustomerReturns());
  return (
    <ListShell title="دریافت مرجوعی (SKU)" icon={<RotateCcw className="w-4 h-4 text-slate-500" />} loading={loading} loadError={loadError} empty={items.length === 0}>
      {items.map((r: any, i: number) => (
        <div key={r.id || i} className="bg-white rounded-2xl border border-slate-200 p-4 space-y-1">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-bold text-slate-800">سفارش {pick(r, ['orderId', 'order_id', 'orderNumber'])}</span>
            <span className="text-[10px] font-bold bg-slate-100 text-slate-700 px-2 py-0.5 rounded-full">{pick(r, ['status'])}</span>
          </div>
          {r.reason && <p className="text-[11px] text-slate-600">دلیل: {r.reason}</p>}
          {r.trackingNumber && <p className="text-[11px] text-slate-500">کد رهگیری: {r.trackingNumber}</p>}
          {(r.status === 'received' || r.status === 'refunded') && <CustomerReturnBinEntry returnID={r.id} />}
        </div>
      ))}
    </ListShell>
  );
};

export const WarehouseReturnBacksTab: React.FC = () => {
  const { items, loading, loadError } = useList(() => wmsApi.getReturnBacks());
  return (
    <ListShell title="درخواست‌های برگشت تأمین‌کننده" icon={<Undo2 className="w-4 h-4 text-slate-500" />} loading={loading} loadError={loadError} empty={items.length === 0}>
      {items.map((r: any, i: number) => (
        <div key={r.id || i} className="bg-white rounded-2xl border border-slate-200 p-4 space-y-1">
          <span className="text-xs font-bold text-slate-800 block">{pick(r, ['supplierName', 'supplier_name', 'title', 'id'])}</span>
          <span className="text-[11px] text-slate-500 block">وضعیت: {pick(r, ['status'])}</span>
          {r.reason && <p className="text-[11px] text-slate-600">دلیل: {r.reason}</p>}
        </div>
      ))}
    </ListShell>
  );
};
