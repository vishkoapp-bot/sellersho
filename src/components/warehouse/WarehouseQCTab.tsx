import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, CheckCircle2, ChevronDown, RefreshCw, ShieldCheck } from 'lucide-react';
import { type QCQueueItemDto, wmsApi } from '../../services/wmsApi';
import { completeScan, unitMutationError, type ScannedUnit } from '../../utils/unitScan';
import { UnitScanField } from '../common/UnitScanField';
import { SupplierEmptyState, SupplierResourceState } from '../supplier/SupplierResourceState';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { WarehouseUnitPrint } from './WarehouseUnitPrint';

interface Props {
  initialInboundId?: string;
  initialReceiptLineIds?: string[];
  onGoToPutaway: (inboundId?: string) => void;
}

type QCForm = { passed: string; failed: string; shortage: string; reason: string };

const numberField = 'mt-1 block min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus-visible:ring-2 focus-visible:ring-brand disabled:bg-slate-50';
const primaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50';
const secondaryButton = 'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-brand-border bg-white px-3 py-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';

function defaultForm(item: QCQueueItemDto): QCForm {
  return {
    passed: String(item.passedQuantity + item.failedQuantity > 0 ? item.passedQuantity : item.receivedQuantity),
    failed: String(item.failedQuantity),
    shortage: String(Math.max(0, item.expectedQuantity - item.receivedQuantity)),
    reason: '',
  };
}

export function WarehouseQCTab({ initialInboundId, initialReceiptLineIds, onGoToPutaway }: Props) {
  const [queue, setQueue] = useState<QCQueueItemDto[]>([]);
  const [selectedId, setSelectedId] = useState('');
  const [forms, setForms] = useState<Record<string, QCForm>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState('');
  const [loadError, setLoadError] = useState('');
  const [scans, setScans] = useState<Record<string, ScannedUnit[]>>({});
  const [pending, setPending] = useState<Record<string, boolean>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [outcome, setOutcome] = useState<{ title: string; inboundId: string; codes: string[]; failed: boolean; hasPass: boolean } | null>(null);
  const submitting = useRef(false);
  const activeHeading = useRef<HTMLHeadingElement>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    setLoadError('');
    try { setQueue((await wmsApi.getQCQueue()).queue || []); }
    catch (cause) { setLoadError(cause instanceof Error ? cause.message : 'صف کنترل کیفیت دریافت نشد.'); }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { void reload(); }, [reload]);
  useEffect(() => { if (selectedId) activeHeading.current?.focus(); }, [selectedId]);

  const targetRequested = !!(initialInboundId || initialReceiptLineIds?.length);
  const matchesTarget = (item: QCQueueItemDto) => initialReceiptLineIds?.includes(item.receiptLineId) || (!!initialInboundId && item.inboundId === initialInboundId);
  const targeted = targetRequested ? queue.filter(matchesTarget) : [];
  const rows = targeted.length ? [...targeted, ...queue.filter(item => !matchesTarget(item))] : queue;
  const chosen = rows.find(item => item.id === selectedId) ?? (targetRequested ? targeted[0] : rows[0]);

  const showError = (id: string, message: string) => setErrors(current => ({ ...current, [id]: message }));
  const update = (item: QCQueueItemDto, patch: Partial<QCForm>) => setForms(current => ({
    ...current, [item.id]: { ...(current[item.id] ?? defaultForm(item)), ...patch },
  }));

  async function submit(item: QCQueueItemDto) {
    if (submitting.current || pending[item.id] || item.reconciliationRequired || loadError) return;
    const form = forms[item.id] ?? defaultForm(item);
    const passed = Number(form.passed);
    const failed = Number(form.failed);
    const shortage = Number(form.shortage);
    if ([form.passed, form.failed, form.shortage].some(value => !value.trim()) ||
      [passed, failed, shortage].some(value => !Number.isSafeInteger(value) || value < 0 || value > 2147483647) ||
      passed < item.passedQuantity || failed < item.failedQuantity ||
      passed + failed <= 0 || passed + failed > item.receivedQuantity ||
      shortage > Math.max(0, item.expectedQuantity - item.receivedQuantity) || !form.reason.trim()) {
      showError(item.id, 'تعدادها را با نتیجه‌های قبلی و تعداد دریافتی تطبیق دهید و توضیح بازرسی را بنویسید.');
      return;
    }
    const lineScans = scans[item.id] ?? [];
    const newFailed = failed - item.failedQuantity;
    const newPassed = passed - item.passedQuantity;
    if (!completeScan(lineScans, newFailed > 0 ? newFailed : newPassed)) {
      showError(item.id, 'همه واحدهای این نتیجه را اسکن کنید یا اسکن‌ها را پاک کنید.');
      return;
    }

    const unit_codes = lineScans.map(unit => unit.code);
    submitting.current = true;
    setBusy(item.id);
    showError(item.id, '');
    try {
      const result = unit_codes.length && failed === 0 && shortage === 0 && item.receiptLineId
        ? await wmsApi.passReceiptLineQC(item.receiptLineId, { quantity: newPassed, unit_codes })
        : await wmsApi.completeQC(item.productId, {
          inboundId: item.inboundId, receiptLineId: item.receiptLineId, passedQuantity: passed,
          failedQuantity: failed, shortageQuantity: shortage, defectReason: form.reason,
          ...(unit_codes.length ? { unit_codes } : {}),
        });
      if (!result || ('line_id' in result && result.line_id !== item.receiptLineId)) {
        throw new Error('پاسخ ثبت کنترل کیفیت کامل نبود. همان نتیجه را دوباره ثبت کنید.');
      }
      setOutcome({ title: item.productTitle, inboundId: item.inboundId, codes: result.unit_codes ?? [], failed: newFailed > 0, hasPass: newPassed > 0 });
      setScans(current => ({ ...current, [item.id]: [] }));
      setForms(current => { const next = { ...current }; delete next[item.id]; return next; });
      await reload();
    } catch (cause) {
      showError(item.id, unitMutationError(cause, 'ثبت نتیجه کنترل کیفیت انجام نشد.'));
    } finally {
      submitting.current = false;
      setBusy('');
    }
  }

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="کنترل کیفیت" description="ردیف دریافت را باز کنید، کالا را بررسی کنید و نتیجه را ثبت کنید." icon={<ShieldCheck />}
        action={<button type="button" onClick={() => void reload()} disabled={loading || !!busy} className={secondaryButton}>
          <RefreshCw aria-hidden="true" className="size-4" /> به‌روزرسانی
        </button>} />
      <SupplierResourceState loading={loading} error={loadError} retry={() => void reload()} />
      {outcome && (
        <div role="status" className="space-y-3 rounded-2xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-900">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="flex items-center gap-2 font-bold"><CheckCircle2 aria-hidden="true" className="size-4 shrink-0" /> نتیجه کنترل کیفیت برای {outcome.title} ثبت شد.</p>
            {outcome.hasPass && <button type="button" data-qc-next className={secondaryButton} onClick={() => onGoToPutaway(outcome.inboundId)}>
              رفتن به صف جانمایی <ArrowLeft aria-hidden="true" className="size-4" />
            </button>}
          </div>
          {!!outcome.codes.length && <div className="space-y-1">
            <p className="font-bold">{outcome.failed ? 'واحدهای آسیب‌دیده ثبت‌شده' : 'واحدهای سالم ثبت‌شده'}</p>
            <ul className="flex flex-wrap gap-2">{outcome.codes.map(code => <li key={code}><code dir="ltr" className="break-all">{code}</code></li>)}</ul>
          </div>}
        </div>
      )}
      {!loading && !loadError && targetRequested && targeted.length === 0 && !outcome && (
        <p role="status" className="rounded-xl border border-brand-border bg-brand-bg p-3 text-sm text-brand-deep">برای این دریافت ردیف آماده کنترل کیفیت در صف وجود ندارد.</p>
      )}
      {!loading && !loadError && (rows.length === 0 ? (
        <SupplierEmptyState icon={<ShieldCheck />} title="قلمی در صف کنترل کیفیت نیست." />
      ) : rows.map(item => {
        const active = chosen?.id === item.id;
        const blocked = !!busy || item.reconciliationRequired;
        const form = forms[item.id] ?? defaultForm(item);
        const newFailed = Number(form.failed) - item.failedQuantity;
        const scanQuantity = Math.max(0, newFailed > 0 ? newFailed : Number(form.passed) - item.passedQuantity) || 0;
        return (
          <section key={item.id} data-qc-active={active || undefined} data-receipt-line-id={item.receiptLineId}
            className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0 space-y-1">
                <h3 ref={active ? activeHeading : undefined} tabIndex={active ? -1 : undefined}
                  className="text-sm font-extrabold text-[#261b41] sm:text-base">{item.productTitle}</h3>
                <p className="break-all text-sm text-slate-700">{item.variantId ? <>SKU <bdi dir="ltr" className="font-mono font-bold">{item.variantId}</bdi></> : 'کالای قدیمی (بدون SKU)'}</p>
                <p className="break-all text-sm text-slate-600">ردیف دریافت <bdi dir="ltr" className="font-mono">{item.receiptLineId || 'فاقد شناسه؛ نیازمند تطبیق'}</bdi></p>
                <p className="break-all text-sm text-slate-600">محموله <bdi dir="ltr" className="font-mono">{item.inboundId}</bdi></p>
              </div>
              {!active && <button type="button" data-qc-open className={secondaryButton}
                disabled={!!busy || Object.values(pending).some(Boolean)} onClick={() => setSelectedId(item.id)}>
                بررسی ردیف <ChevronDown aria-hidden="true" className="size-4" />
              </button>}
            </div>
            {item.reconciliationRequired && <p className="rounded-xl bg-amber-50 p-3 text-sm text-amber-900">ثبت کنترل کیفیت تا تطبیق سوابق موجودی بسته است.</p>}
            {active && (
              <>
                <dl className="grid grid-cols-3 gap-2 rounded-xl bg-brand-bg p-3 text-sm">
                  <div><dt className="text-slate-600">مورد انتظار</dt><dd className="font-extrabold">{item.expectedQuantity.toLocaleString('fa-IR')}</dd></div>
                  <div><dt className="text-slate-600">دریافت‌شده</dt><dd className="font-extrabold">{item.receivedQuantity.toLocaleString('fa-IR')}</dd></div>
                  <div><dt className="text-slate-600">باقی‌مانده بررسی</dt><dd className="font-extrabold">{item.remainingQuantity.toLocaleString('fa-IR')}</dd></div>
                </dl>
                {!item.reconciliationRequired && (
                  <form onSubmit={event => { event.preventDefault(); void submit(item); }} className="space-y-3" noValidate>
                    <p className="text-sm leading-6 text-slate-600">تعداد سالم و آسیب‌دیده را با احتساب نتیجه‌های قبلی وارد کنید. واحدهای سالم پس از جانمایی قابل فروش‌اند.</p>
                    <div className="grid grid-cols-3 gap-2">
                      <label className="text-sm font-bold text-slate-700">سالم<input type="number" inputMode="numeric" min={item.passedQuantity} value={form.passed} onChange={event => update(item, { passed: event.target.value })} disabled={blocked} className={numberField} /></label>
                      <label className="text-sm font-bold text-slate-700">آسیب‌دیده<input type="number" inputMode="numeric" min={item.failedQuantity} value={form.failed} onChange={event => update(item, { failed: event.target.value })} disabled={blocked} className={numberField} /></label>
                      <label className="text-sm font-bold text-slate-700">کسری<input type="number" inputMode="numeric" min={0} value={form.shortage} onChange={event => update(item, { shortage: event.target.value })} disabled={blocked} className={numberField} /></label>
                    </div>
                    <label className="block text-sm font-bold text-slate-700">
                      توضیح بازرسی
                      <textarea rows={2} value={form.reason} onChange={event => update(item, { reason: event.target.value })} disabled={blocked} className={numberField} />
                    </label>
                    {!!item.receiptLineId && (
                      <div className="space-y-2 [&_button]:min-h-11 [&_input]:min-h-11 [&_input]:text-base">
                        <p className="text-sm font-bold text-slate-700">{newFailed > 0 ? 'اسکن واحدهای آسیب‌دیده' : 'اسکن واحدهای سالم'}</p>
                        <UnitScanField scans={scans[item.id] ?? []} quantity={scanQuantity} disabled={blocked}
                          onChange={value => setScans(current => ({ ...current, [item.id]: value }))}
                          onPending={value => setPending(current => ({ ...current, [item.id]: value }))} />
                      </div>
                    )}
                    {errors[item.id] && <p role="alert" className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">{errors[item.id]}</p>}
                    <button type="submit" data-qc-submit disabled={blocked || pending[item.id]} className={primaryButton + ' w-full sm:w-auto'}>
                      <CheckCircle2 aria-hidden="true" className="size-4" /> {busy ? 'در حال ثبت…' : 'ثبت و تأیید نتیجه QC'}
                    </button>
                  </form>
                )}
                {!item.reconciliationRequired && !!item.receiptLineId && <div className="border-t border-slate-200 pt-3 [&_button]:min-h-11">
                  <WarehouseUnitPrint receiptLineId={item.receiptLineId} />
                </div>}
              </>
            )}
          </section>
        );
      }))}
    </div>
  );
}
