import { WarehouseInboundLineReceipt } from './WarehouseInboundLineReceipt';
import React, { useCallback, useEffect, useState } from 'react';
import { ArrowRight, CheckCircle2, RefreshCw, ScanBarcode, Truck } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { InboundShipmentDto, wmsApi } from '../../services/wmsApi';

interface Props { onGoToQC: (inboundId: string) => void; }

export const WarehouseInboundTab: React.FC<Props> = ({ onGoToQC }) => {
  const { success, error } = useToast();
  const [inbounds, setInbounds] = useState<InboundShipmentDto[]>([]);
  const [scannedCode, setScannedCode] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [lastReceived, setLastReceived] = useState<string | null>(null);

  const reload = useCallback(async () => {
    setLoading(true); setLoadError('');
    try { setInbounds(await wmsApi.getInbounds()); }
    catch (cause) { const message = cause instanceof Error ? cause.message : 'صف ورودی دریافت نشد.'; setLoadError(message); error(message); }
    finally { setLoading(false); }
  }, [error]);
  useEffect(() => { void reload(); }, [reload]);

  const handleScan = async (event: React.FormEvent) => {
    event.preventDefault();
    const query = scannedCode.trim().toUpperCase();
    if (!query) return;
    const inbound = inbounds.find((candidate) => candidate.id.toUpperCase() === query);
    if (!inbound) { error('کد واردشده در مانیفست واقعی صف ورودی پیدا نشد.'); return; }
    if (inbound.items?.some(item => item.line_id ?? item.lineId)) {
      error('برای این محموله، دریافت را در هر ردیف ورودی ثبت کنید.');
      return;
    }
    setBusy(inbound.id);
    try {
      await wmsApi.startReceiving(inbound.id);
      const firstItem = inbound.items?.[0];
      await wmsApi.scanInboundItem(inbound.id, { productId: firstItem?.productId || firstItem?.product_id, barcode: query, quantity: firstItem?.quantity || inbound.itemsCount });
      await wmsApi.completeReceiving(inbound.id);
      setLastReceived(inbound.id); setScannedCode(''); success(`دریافت محموله ${inbound.id} در backend ثبت شد.`); await reload();
    } catch (cause) { error(cause instanceof Error ? cause.message : 'ثبت دریافت انجام نشد.'); }
    finally { setBusy(null); }
  };

  return <div className="space-y-3.5 pb-6">
    <form onSubmit={handleScan} className="space-y-2.5 rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs">
      <div className="flex items-center gap-1.5 text-blue-600"><ScanBarcode className="h-4 w-4"  strokeWidth={1.5}/><h3 className="text-xs font-bold text-slate-800">ثبت دریافت با کد مانیفست واقعی</h3></div>
      <div className="flex items-center gap-2"><input value={scannedCode} onChange={(event) => setScannedCode(event.target.value)} placeholder="کد محموله را اسکن یا وارد کنید" className="flex-1 rounded-xl border border-slate-200 bg-slate-50 p-2.5 text-xs font-mono tracking-wider focus:outline-hidden" dir="ltr" /><button disabled={busy !== null} type="submit" className="shrink-0 rounded-xl bg-blue-600 px-4 py-2.5 text-xs font-bold text-white disabled:opacity-50">ثبت دریافت</button></div>
      {lastReceived && <div className="flex items-center gap-1.5 rounded-lg bg-emerald-50 p-2 text-[11px] font-bold text-emerald-700"><CheckCircle2 className="h-4 w-4"  strokeWidth={1.5}/>آخرین دریافت تکمیل‌شده: {lastReceived}</div>}
    </form>
    <div className="space-y-2.5"><div className="flex items-center justify-between"><h4 className="text-xs font-bold text-slate-800">صف واقعی محموله‌های ورودی</h4><button onClick={() => void reload()} disabled={loading} className="rounded-xl border border-slate-200 p-2 text-slate-600 disabled:opacity-50" title="به‌روزرسانی"><RefreshCw className={loading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'}  strokeWidth={1.5}/></button></div>
      {loading ? <div className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">در حال دریافت صف...</div> : loadError ? <div className="rounded-2xl bg-rose-50 p-4 text-center text-xs text-rose-700">{loadError}<button onClick={() => void reload()} className="mt-2 block mx-auto font-bold underline">تلاش مجدد</button></div> : inbounds.length === 0 ? <div className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">محموله‌ای در صف ورودی نیست.</div> : inbounds.map((item) => <section key={item.id} className="space-y-2 rounded-2xl border border-slate-200/90 bg-white p-3 shadow-2xs"><div className="flex items-center justify-between border-b border-slate-100 pb-1.5"><span className="font-mono text-xs font-black text-slate-800">{item.id}</span><span className="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[9px] font-bold text-amber-700">{item.status}</span></div><div className="space-y-1 text-[11px] text-slate-600"><div className="flex justify-between"><span className="text-slate-400">تأمین‌کننده:</span><span className="font-bold text-slate-800">{item.supplierName}</span></div><div className="flex items-center justify-between"><span className="text-slate-400">حامل:</span><span className="flex items-center gap-1"><Truck className="h-3.5 w-3.5"  strokeWidth={1.5}/>{item.carrier || 'ثبت نشده'}</span></div><div className="flex justify-between"><span className="text-slate-400">کارتن / کالا:</span><span>{item.cartonsCount} / {item.itemsCount}</span></div></div>{item.items?.map(line => {
      const lineId = line.line_id ?? line.lineId;
      return lineId && <WarehouseInboundLineReceipt key={lineId} lineId={lineId} expected={line.quantity ?? 0} />;
    })}
      <button onClick={() => onGoToQC(item.id)} className="flex w-full items-center justify-center gap-1 rounded-xl bg-blue-50 py-2 text-[11px] font-bold text-blue-700"><span>مشاهده در میز QC</span><ArrowRight className="h-3.5 w-3.5"  strokeWidth={1.5}/></button></section>)}
    </div>
  </div>;
};