import { SupplierSectionHeading } from './SupplierSectionHeading';
import { AlertTriangle, ChevronDown } from 'lucide-react';
import { useId, useLayoutEffect, useRef, useState, type FormEvent } from 'react';
import { supplierApi, type SupplierDiscrepancyDto } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';

export function canRespondToDiscrepancy(item: { status: string; escalated_at?: string; responded_at?: string; response_due_at: string }) {
  return item.status === 'open' && !item.responded_at && !item.escalated_at && Date.parse(item.response_due_at) > Date.now();
}
function discrepancyStatus(item: SupplierDiscrepancyDto, needsResponse: boolean) {
  if (item.escalated_at) return 'در دست بررسی مدیر';
  if (item.status === 'resolved') return 'رسیدگی شد';
  if (item.responded_at) return 'پاسخ ثبت شد';
  if (needsResponse) return 'نیازمند پاسخ';
  return item.status === 'open' ? 'مهلت پاسخ پایان یافته' : 'در حال پیگیری';
}

const actionClass = 'min-h-11 rounded-xl px-3 text-sm font-bold focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-60';
const fieldClass = 'mt-1 min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3 py-2 font-normal focus-visible:ring-2 focus-visible:ring-brand';

export function SupplierDiscrepanciesTab({ initialAwaitingOnly = false }: { initialAwaitingOnly?: boolean }) {
  const resource = useSupplierResource(supplierApi.getDiscrepancies);
  const { success, error } = useToast();
  const { toPersianDigits } = useStore();
  const [selectedId, setSelectedId] = useState('');
  const [response, setResponse] = useState('');
  const [resolution, setResolution] = useState('');
  const [saving, setSaving] = useState(false);
  const [awaitingOnly, setAwaitingOnly] = useState(initialAwaitingOnly);
  const filterId = useId();
  const replyField = useRef<HTMLTextAreaElement>(null);
  const replyTrigger = useRef<HTMLButtonElement | null>(null);
  useLayoutEffect(() => {
    if (selectedId) replyField.current?.focus();
    else replyTrigger.current?.focus();
  }, [selectedId]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!response.trim() || !selectedId || saving) return;
    setSaving(true);
    try {
      await supplierApi.respondDiscrepancy(selectedId, response.trim(), resolution.trim());
      setSelectedId('');
      setResponse('');
      setResolution('');
      success('پاسخت ثبت شد.');
      await resource.reload();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ثبت پاسخ انجام نشد.');
    } finally {
      setSaving(false);
    }
  };
  const items = resource.data ?? [];
  const sortedItems = items
    .map(item => ({ item, needsResponse: canRespondToDiscrepancy(item) }))
    .filter(({ needsResponse }) => !awaitingOnly || needsResponse)
    .sort((a, b) => Number(b.needsResponse) - Number(a.needsResponse));

  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="مغایرت‌های تحویل انبار" description="اختلاف تعداد یا خرابیِ ثبت‌شده را بررسی کن و تا پایان مهلت پاسخ بده." icon={<AlertTriangle />}
        action={<fieldset className="flex flex-wrap gap-1 rounded-xl border border-slate-200 bg-slate-50 p-1">
          <legend className="sr-only">فیلتر مغایرت‌ها</legend>
          {([['همه', false], ['نیازمند پاسخ', true]] as const).map(([label, only]) => (
            <label key={label} className="cursor-pointer">
              <input type="radio" name={filterId} checked={awaitingOnly === only} onChange={() => setAwaitingOnly(only)} className="peer sr-only" />
              <span className="flex min-h-11 items-center rounded-lg px-3 text-sm font-bold text-brand-deep peer-checked:bg-brand-deep peer-checked:text-white peer-focus-visible:ring-2 peer-focus-visible:ring-brand peer-focus-visible:ring-offset-2">{label}</span>
            </label>
          ))}
        </fieldset>}
      />
      <SupplierResourceState loading={resource.loading} error={resource.error} retry={resource.reload} />
      {!resource.loading && !resource.error && sortedItems.length === 0 && (
        <SupplierEmptyState icon={<AlertTriangle />} title={awaitingOnly ? 'مغایرتی منتظر پاسخ تو نیست.' : 'مغایرتی ثبت نشده است.'}
          description={awaitingOnly ? 'برای دیدن سابقه، فیلتر «همه» را انتخاب کن.' : 'اگر انبار اختلاف تعداد یا خرابی ثبت کند، جزئیات آن همین‌جا نمایش داده می‌شود.'} />
      )}
      {!resource.error && sortedItems.map(({ item, needsResponse }) => {
        const replyOpen = needsResponse && selectedId === item.id;
        const panelId = `${filterId}-reply-${item.id}`;
        return (
          <article key={item.id} className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-sm sm:p-5">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h3 className="min-w-0 break-words text-base font-extrabold text-[#261b41]">{item.product_title || <bdi dir="ltr">{item.product_id.slice(0, 8)}</bdi>}</h3>
              <span className={`rounded-lg px-2 py-1 text-xs font-bold ${needsResponse ? 'bg-amber-50 text-amber-900' : 'bg-slate-100 text-slate-700'}`}>{discrepancyStatus(item, needsResponse)}</span>
            </div>
            <dl className="grid grid-cols-3 gap-2 rounded-xl bg-slate-50 p-2.5">
              {([['تعداد اعلام‌شده', item.expected_quantity], ['تعداد دریافتی', item.received_quantity], ['خراب یا معیوب', item.defect_count]] as const).map(([label, quantity]) => (
                <div key={label} className="min-w-0 px-1">
                  <dt className="text-xs leading-5 text-slate-600">{label}</dt>
                  <dd className="text-base font-extrabold text-slate-900">{toPersianDigits(quantity)}</dd>
                </div>
              ))}
            </dl>
            {item.reason && <p className="break-words leading-7 text-slate-800">{item.reason}</p>}
            {needsResponse && <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="font-bold text-amber-900">مهلت پاسخ تا <time dateTime={item.response_due_at}>{new Date(item.response_due_at).toLocaleString('fa-IR')}</time></p>
              <button type="button" hidden={replyOpen} disabled={saving} aria-controls={panelId} aria-expanded={replyOpen}
                onClick={event => { replyTrigger.current = event.currentTarget; setSelectedId(item.id); setResponse(''); setResolution(''); }}
                className={`${actionClass} bg-brand-soft text-brand-deep hover:bg-brand-border/40`}>ثبت پاسخ</button>
            </div>}
            <div id={panelId} hidden={!replyOpen}>
              {replyOpen && <form onSubmit={submit} className="space-y-3 rounded-xl border border-brand-border bg-brand-bg p-3 sm:p-4">
                <label className="block font-bold">پاسخ به مغایرت
                  <textarea ref={replyField} required disabled={saving} rows={3} value={response} onChange={event => setResponse(event.target.value)} className={fieldClass} />
                </label>
                <label className="block font-bold">پیشنهاد برای رسیدگی (اختیاری)
                  <input disabled={saving} value={resolution} onChange={event => setResolution(event.target.value)} className={fieldClass} />
                </label>
                <div className="flex flex-wrap gap-2">
                  <button type="submit" disabled={saving} className={`${actionClass} bg-brand-deep text-white hover:bg-[#382660]`}>{saving ? 'در حال ارسال…' : 'ارسال پاسخ'}</button>
                  <button type="button" disabled={saving} onClick={() => setSelectedId('')} className={`${actionClass} text-brand-deep hover:bg-brand-soft`}>انصراف</button>
                </div>
              </form>}
            </div>
            <details className="group border-t border-slate-200">
              <summary className="flex min-h-11 w-fit cursor-pointer list-none items-center gap-1.5 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
                جزئیات مغایرت
                <ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" />
              </summary>
              <dl className="flex flex-wrap gap-x-5 gap-y-2 rounded-xl bg-slate-50 p-3 text-xs leading-6 text-slate-700">
                <div><dt className="font-bold">کد پیگیری</dt><dd><bdi dir="ltr" className="break-all font-mono">{item.id}</bdi></dd></div>
                <div><dt className="font-bold">کد بار</dt><dd><bdi dir="ltr" className="break-all font-mono">{item.inbound_id}</bdi></dd></div>
                {!needsResponse && item.response_due_at && <div><dt className="font-bold">مهلت پاسخ</dt><dd><time dateTime={item.response_due_at}>{new Date(item.response_due_at).toLocaleString('fa-IR')}</time></dd></div>}
                {item.supplier_response && <div className="min-w-0 basis-full"><dt className="font-bold">پاسخ ثبت‌شده</dt><dd className="whitespace-pre-wrap break-words">{item.supplier_response}</dd></div>}
              </dl>
            </details>
          </article>
        );
      })}
    </div>
  );
}
