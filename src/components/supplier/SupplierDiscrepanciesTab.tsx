import { SupplierSectionHeading } from './SupplierSectionHeading';
import { AlertTriangle } from 'lucide-react';
import React, { useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';

export function canRespondToDiscrepancy(item: { status: string; escalated_at?: string; responded_at?: string; response_due_at: string }) {
  return item.status === 'open' && !item.responded_at && !item.escalated_at && Date.parse(item.response_due_at) > Date.now();
}
export const SupplierDiscrepanciesTab: React.FC<{ initialAwaitingOnly?: boolean }> = ({ initialAwaitingOnly = false }) => {
  const resource = useSupplierResource(supplierApi.getDiscrepancies);
  const { success, error } = useToast();
  const { toPersianDigits } = useStore();
  const [selectedId, setSelectedId] = useState('');
  const [response, setResponse] = useState('');
  const [resolution, setResolution] = useState('');
  const [saving, setSaving] = useState(false);
  const [awaitingOnly, setAwaitingOnly] = useState(initialAwaitingOnly);
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!response.trim() || !selectedId || saving) return;
    setSaving(true);
    try {
      await supplierApi.respondDiscrepancy(selectedId, response.trim(), resolution.trim());
      setSelectedId('');
      setResponse('');
      setResolution('');
      success('جوابت ثبت شد.');
      await resource.reload();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ثبت پاسخ انجام نشد.');
    } finally {
      setSaving(false);
    }
  };
  const items = resource.data ?? [];
  const needsResponse = canRespondToDiscrepancy;
  const sortedItems = items.filter(item => !awaitingOnly || needsResponse(item)).sort((a, b) => Number(needsResponse(b)) - Number(needsResponse(a)));

  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="مغایرت‌های تحویل انبار" description="اختلاف تعداد یا خرابیِ ثبت‌شده را بررسی کن و تا پایان مهلت پاسخ بده." icon={<AlertTriangle />} />
      <label className="inline-flex min-h-11 items-center gap-2 text-sm font-bold"><input type="checkbox" checked={awaitingOnly} onChange={event => setAwaitingOnly(event.target.checked)} className="size-4 accent-brand-deep" /> فقط نیازمند پاسخ</label>
      <SupplierResourceState loading={resource.loading} error={resource.error} retry={resource.reload} />
      {!resource.loading && !resource.error && sortedItems.length === 0 && (
        <SupplierEmptyState icon={<AlertTriangle />} title={awaitingOnly ? 'مغایرتی منتظر پاسخ تو نیست.' : 'مغایرتی ثبت نشده است.'} description={awaitingOnly ? 'برای دیدن سابقه، فیلتر «فقط نیازمند پاسخ» را بردار.' : 'اگر انبار اختلاف تعداد یا خرابی ثبت کند، جزئیات آن همین‌جا نمایش داده می‌شود.'} />
      )}
      {!resource.error && sortedItems.map((item) => (
        <section key={item.id} className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-sm shadow-sm">
          <h3 className="font-bold">{item.product_title || item.product_id.slice(0, 8)}</h3>
          <p className="text-[10px] text-slate-400">کد پیگیری: {item.id} · کد بار: {item.inbound_id}</p>
          <p>تعدادی که تو گفتی: <b>{toPersianDigits(item.expected_quantity)}</b> · تعدادی که انبار تحویل گرفت: <b>{toPersianDigits(item.received_quantity)}</b> · خراب و معیوب: <b>{toPersianDigits(item.defect_count)}</b></p>
          <p>{item.reason}</p>
          <p>وضعیت: {item.escalated_at ? 'در دست بررسی مدیر' : item.status === 'resolved' ? 'رسیدگی شد' : item.responded_at ? 'پاسخت ثبت شد' : canRespondToDiscrepancy(item) ? 'نیازمند پاسخ تو' : item.status === 'open' ? 'مهلت پاسخ پایان یافته' : 'در حال پیگیری'}</p>
          {item.supplier_response && <p className="rounded-xl bg-slate-50 p-2">جواب تو: {item.supplier_response}</p>}
          {item.response_due_at && <p className={item.escalated_at || Date.parse(item.response_due_at) <= Date.now() ? 'text-rose-700' : 'text-amber-700'}>{item.escalated_at || (!item.responded_at && Date.parse(item.response_due_at) <= Date.now()) ? 'مهلت پاسخ تمام شده؛ نیازمند بررسی مدیر' : `مهلت پاسخ: ${new Date(item.response_due_at).toLocaleString('fa-IR')}`}</p>}
          {canRespondToDiscrepancy(item) && (selectedId === item.id ? (
            <form onSubmit={submit} className="space-y-2">
              <label className="block">جوابت را بنویس (اگر اشتباه شده، همین‌جا بگو)
                <textarea required disabled={saving} value={response} onChange={(event) => setResponse(event.target.value)} className="mt-1 w-full rounded-xl border p-2 min-h-[44px]" />
              </label>
              <label className="block">پیشنهادت برای درست‌کردنش (اختیاری)
                <input disabled={saving} value={resolution} onChange={(event) => setResolution(event.target.value)} className="mt-1 w-full rounded-xl border p-2 min-h-[40px]" />
              </label>
              <button disabled={saving} className="rounded-xl bg-brand-deep px-3 py-2 min-h-[44px] text-white font-bold">{saving ? 'ثبت…' : 'فرستادن جواب'}</button>
              <button type="button" disabled={saving} onClick={() => setSelectedId('')} className="mr-3">انصراف</button>
            </form>
          ) : (
            <button type="button" disabled={saving} onClick={() => { setSelectedId(item.id); setResponse(''); setResolution(''); }} className="rounded-xl bg-brand-soft px-3 py-2 min-h-[40px] font-bold text-brand-deep">جواب دادن</button>
          ))}
        </section>
      ))}
    </div>
  );
};
