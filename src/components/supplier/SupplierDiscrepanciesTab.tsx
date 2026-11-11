import React, { useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';

// Live discrepancies tab (rn): QC delivery discrepancies + supplier response.
export const SupplierDiscrepanciesTab: React.FC = () => {
  const resource = useSupplierResource(supplierApi.getDiscrepancies);
  const { success, error } = useToast();
  const { toPersianDigits } = useStore();
  const [selectedId, setSelectedId] = useState('');
  const [response, setResponse] = useState('');
  const [resolution, setResolution] = useState('');
  const [saving, setSaving] = useState(false);
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
  return (
    <div className="space-y-3 pb-6">
      <h2 className="text-sm font-bold">اختلاف در تحویل کالا</h2>
      <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">گاهی انبار می‌گوید تعدادی که رسیده با تعدادی که تو گفتی فرق دارد، یا چند تایی خراب بوده. این اختلاف‌ها اینجا می‌آید؛ نگاه کن و اگر اشتباه شده، جوابت را بنویس.</div>
      <SupplierResourceState loading={resource.loading} error={resource.error} retry={resource.reload} />
      {!resource.loading && !resource.error && !(resource.data || []).length && (
        <div className="rounded-2xl border bg-white p-6 text-center space-y-1.5">
          <p className="text-xs font-bold text-emerald-700">اختلافی ثبت نشده است.</p>
          <p className="text-[11px] leading-5 text-slate-400">یعنی همه بارهایت درست و کامل تحویل گرفته شده‌اند.</p>
        </div>
      )}
      {!resource.error && (resource.data || []).map((item) => (
        <section key={item.id} className="space-y-2 rounded-2xl border bg-white p-3 text-xs">
          <h3 className="font-bold">{item.product_title || item.product_id.slice(0, 8)}</h3>
          <p className="text-[10px] text-slate-400">کد پیگیری: {item.id} · کد بار: {item.inbound_id}</p>
          <p>تعدادی که تو گفتی: <b>{toPersianDigits(item.expected_quantity)}</b> · تعدادی که انبار تحویل گرفت: <b>{toPersianDigits(item.received_quantity)}</b> · خراب و معیوب: <b>{toPersianDigits(item.defect_count)}</b></p>
          <p>{item.reason}</p>
          <p>وضعیت: {item.status === 'open' ? 'منتظر جواب توست' : item.status === 'resolved' ? 'جواب دادی' : item.status}</p>
          {item.supplier_response && <p className="rounded-xl bg-slate-50 p-2">جواب تو: {item.supplier_response}</p>}
          {item.response_due_at && <p className={item.escalated_at || Date.parse(item.response_due_at) <= Date.now() ? 'text-rose-700' : 'text-amber-700'}>{item.escalated_at || (!item.responded_at && Date.parse(item.response_due_at) <= Date.now()) ? 'مهلت پاسخ تمام شده؛ نیازمند بررسی مدیر' : `مهلت پاسخ: ${new Date(item.response_due_at).toLocaleString('fa-IR')}`}</p>}
          {item.status === 'open' && !item.escalated_at && Date.parse(item.response_due_at) > Date.now() && (selectedId === item.id ? (
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
