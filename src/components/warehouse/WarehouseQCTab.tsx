import { WarehouseUnitPrint } from './WarehouseUnitPrint';
import { UnitScanField } from '../common/UnitScanField';
import { completeScan, unitMutationError, type ScannedUnit } from '../../utils/unitScan';
import React, { useCallback, useEffect, useState } from 'react';
import { CheckCircle2, RefreshCw, ShieldCheck } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { QCQueueItemDto, wmsApi } from '../../services/wmsApi';

interface Props { onGoToPutaway: () => void; }
type QCForm = { passed: string; failed: string; shortage: string; reason: string };

export const WarehouseQCTab: React.FC<Props> = ({ onGoToPutaway }) => {
  const { success, error } = useToast();
  const [queue, setQueue] = useState<QCQueueItemDto[]>([]);
  const [forms, setForms] = useState<Record<string, QCForm>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [loadError, setLoadError] = useState('');
  const [scans, setScans] = useState<Record<string, ScannedUnit[]>>({});
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [outcome, setOutcome] = useState<{ codes: string[]; failed: boolean } | null>(null);

  const reload = useCallback(async () => {
    setLoading(true); setLoadError('');
    try { setQueue((await wmsApi.getQCQueue()).queue || []); }
    catch (cause) { const message = cause instanceof Error ? cause.message : 'صف QC دریافت نشد.'; setLoadError(message); error(message); }
    finally { setLoading(false); }
  }, [error]);
  useEffect(() => { void reload(); }, [reload]);
  const valueFor = (item: QCQueueItemDto): QCForm => forms[item.id] || { passed: String(item.passedQuantity + item.failedQuantity > 0 ? item.passedQuantity : item.receivedQuantity), failed: String(item.failedQuantity), shortage: String(Math.max(0, item.expectedQuantity - item.receivedQuantity)), reason: '' };
  const update = (id: string, patch: Partial<QCForm>) => setForms((current) => ({ ...current, [id]: { ...valueFor(queue.find((item) => item.id === id)!), ...patch } }));
  const submit = async (item: QCQueueItemDto) => {
    if (busy || pending[item.id]) return;
    if (item.reconciliationRequired) { error('این دریافت به بررسی و تطبیق سوابق موجودی نیاز دارد.'); return; }
    const form = valueFor(item);
    const passed = Number(form.passed);
    const failed = Number(form.failed);
    const shortage = Number(form.shortage);
    if ([passed, failed, shortage].some((value) => !Number.isSafeInteger(value) || value < 0 || value > 2147483647) ||
      passed + failed <= 0 || passed + failed > item.receivedQuantity || !form.reason.trim()) {
      error('مقادیر QC نامعتبر است؛ نتیجه و علت را کامل کنید.');
      return;
    }
    const lineScans = scans[item.id] ?? [];
    const newFailed = failed - item.failedQuantity;
    const newPassed = passed - item.passedQuantity;
    const quantity = newFailed > 0 ? newFailed : newPassed;
    if (!completeScan(lineScans, quantity)) {
      setErrors(current => ({ ...current, [item.id]: 'همه واحدهای این نتیجه را اسکن کنید یا اسکن‌ها را پاک کنید.' }));
      return;
    }
    const unit_codes = lineScans.map(unit => unit.code);
    setBusy(item.id);
    setErrors(current => ({ ...current, [item.id]: '' }));
    try {
      // The line-pass route advances a pure pass. The existing disposition
      // command owns cumulative failures, shortage and mixed outcomes.
      const result = unit_codes.length && failed === 0 && shortage === 0 && item.receiptLineId
        ? await wmsApi.passReceiptLineQC(item.receiptLineId, { quantity: newPassed, unit_codes })
        : await wmsApi.completeQC(item.productId, {
          inboundId: item.inboundId, receiptLineId: item.receiptLineId, passedQuantity: passed,
          failedQuantity: failed, shortageQuantity: shortage, defectReason: form.reason,
          ...(unit_codes.length ? { unit_codes } : {}),
        });
      setOutcome({ codes: result.unit_codes ?? [], failed: newFailed > 0 });
      setScans(current => ({ ...current, [item.id]: [] }));
      setForms(current => { const next = { ...current }; delete next[item.id]; return next; });
      success(`نتیجه QC برای ${item.productTitle} ثبت شد.`);
      await reload();
    } catch (cause) {
      setErrors(current => ({ ...current, [item.id]: unitMutationError(cause, 'ثبت نتیجه QC انجام نشد.') }));
    } finally { setBusy(null); }
  };

  return <div className="space-y-3.5 pb-6"><div className="flex items-center justify-between rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs"><div className="flex items-center gap-1.5 text-blue-600"><ShieldCheck className="h-4 w-4"  strokeWidth={1.5}/><h3 className="text-xs font-bold text-slate-800">صف واقعی کنترل کیفیت</h3></div><button onClick={() => void reload()} disabled={loading} className="rounded-xl border border-slate-200 p-2 text-slate-600 disabled:opacity-50" title="به‌روزرسانی"><RefreshCw className={loading ? 'h-4 w-4 animate-spin' : 'h-4 w-4'}  strokeWidth={1.5}/></button></div>
    {loading ? <div className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">در حال دریافت صف QC...</div> : loadError ? <div className="rounded-2xl bg-rose-50 p-4 text-center text-xs text-rose-700">{loadError}<button onClick={() => void reload()} className="mt-2 block mx-auto font-bold underline">تلاش مجدد</button></div> : queue.length === 0 ? <div className="rounded-2xl bg-white p-6 text-center text-xs text-slate-500">قلمی در صف QC نیست.</div> : queue.map((item) => {
      const form = valueFor(item);
      const newFailed = Number(form.failed) - item.failedQuantity;
      const scanQuantity = Math.max(0, newFailed > 0 ? newFailed : Number(form.passed) - item.passedQuantity) || 0;
      return <section key={item.id} className="space-y-3 rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs"><div className="border-b border-slate-100 pb-2"><span className="font-mono text-[10px] text-slate-500">{item.inboundId} / {item.productId}</span><h4 className="mt-1 text-xs font-bold text-slate-800">{item.productTitle}</h4><p className="break-all text-[10px] text-slate-600">{item.variantId ? <>SKU: <bdi dir="ltr" className="font-mono">{item.variantId}</bdi></> : 'کالای قدیمی (بدون SKU)'}</p><p className="break-all text-[10px] text-slate-600">ردیف دریافت: <bdi dir="ltr" className="font-mono">{item.receiptLineId || 'فاقد شناسه؛ نیازمند تطبیق'}</bdi></p><p className="text-[10px] text-slate-500">مورد انتظار: {item.expectedQuantity} · دریافت فیزیکی: {item.receivedQuantity} · بررسی‌نشده: {item.remainingQuantity}</p></div><p className="text-[10px] text-amber-700">{item.reconciliationRequired ? 'ثبت QC تا بررسی و تطبیق سوابق موجودی بسته است.' : ''}</p><p className="text-[10px] text-slate-500">تعدادهای سالم و آسیب‌دیده را با احتساب نتایج قبلی وارد کنید. تأیید QC تا تکمیل جانمایی قابل فروش نیست.</p><div className="grid grid-cols-3 gap-2"><label className="text-[9px] font-bold text-emerald-700">سالم (تجمعی)<input type="number" min="0" value={form.passed} onChange={(event) => update(item.id, { passed: event.target.value })} className="mt-1 w-full rounded-xl border border-emerald-300 bg-emerald-50/50 p-2 text-xs" /></label><label className="text-[9px] font-bold text-rose-700">آسیب‌دیده (تجمعی)<input type="number" min="0" value={form.failed} onChange={(event) => update(item.id, { failed: event.target.value })} className="mt-1 w-full rounded-xl border border-rose-300 bg-rose-50/50 p-2 text-xs" /></label><label className="text-[9px] font-bold text-amber-700">کسری<input type="number" min="0" value={form.shortage} onChange={(event) => update(item.id, { shortage: event.target.value })} className="mt-1 w-full rounded-xl border border-amber-300 bg-amber-50/50 p-2 text-xs" /></label></div><textarea rows={2} value={form.reason} onChange={(event) => update(item.id, { reason: event.target.value })} placeholder="علت و توضیح بازرس QC" className="w-full rounded-xl border border-slate-200 bg-slate-50 p-2 text-xs" />{item.receiptLineId && <>
      <p className="text-xs">{newFailed > 0 ? 'اسکن واحدهای آسیب‌دیده' : 'اسکن واحدهای سالم'}</p>
      <UnitScanField scans={scans[item.id] ?? []} quantity={scanQuantity} disabled={!!busy || item.reconciliationRequired}
        onChange={value => setScans(current => ({ ...current, [item.id]: value }))}
        onPending={value => setPending(current => ({ ...current, [item.id]: value }))} />
      {!item.reconciliationRequired && <WarehouseUnitPrint receiptLineId={item.receiptLineId} />}
    </>}
    {errors[item.id] && <p role="alert" className="text-xs text-rose-700">{errors[item.id]}</p>}
    <button data-qc-submit disabled={!!busy || pending[item.id] || item.reconciliationRequired} onClick={() => void submit(item)} className="flex w-full items-center justify-center gap-1.5 rounded-xl bg-emerald-600 py-2.5 text-xs font-bold text-white disabled:opacity-50"><CheckCircle2 className="h-4 w-4"  strokeWidth={1.5}/>ثبت و تأیید نتیجه QC</button></section>; })}
    {!!outcome?.codes.length && <div role="status" className="space-y-1 text-xs">
      <p>{outcome.failed ? 'واحدهای آسیب‌دیده ثبت‌شده' : 'واحدهای سالم ثبت‌شده'}</p>
      {outcome.codes.map(code => <code key={code} dir="ltr" className="me-2">{code}</code>)}
    </div>}
    {queue.length > 0 && <button onClick={onGoToPutaway} className="w-full rounded-xl bg-blue-50 py-2 text-xs font-bold text-blue-700">رفتن به صف جانمایی</button>}
  </div>;
};
