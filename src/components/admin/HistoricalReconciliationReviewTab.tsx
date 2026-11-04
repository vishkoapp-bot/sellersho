import React, { useEffect, useMemo, useState } from 'react';
import { ClipboardCheck, FileSearch, RefreshCw, Save, Search } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { historicalReviewApi, HistoricalInventoryReportRow, HistoricalReviewStatus } from '../../services/adminApi';

const REVIEW_STATUSES: Array<{ value: HistoricalReviewStatus; label: string }> = [
  { value: 'needs_review', label: 'نیازمند بررسی' },
  { value: 'in_review', label: 'در حال بررسی' },
  { value: 'awaiting_evidence', label: 'منتظر مدرک' },
  { value: 'approved_for_correction', label: 'تأیید برای اصلاح' },
  { value: 'rejected', label: 'رد شده' },
  { value: 'resolved', label: 'حل‌شده' },
  { value: 'indeterminate', label: 'غیرقابل تعیین تکلیف' },
];

const reviewLabel = (value: string) => REVIEW_STATUSES.find((item) => item.value === value)?.label || value;

export const HistoricalReconciliationReviewTab: React.FC = () => {
  const { success, error } = useToast();
  const [rows, setRows] = useState<HistoricalInventoryReportRow[]>([]);
  const [query, setQuery] = useState('');
  const [classification, setClassification] = useState('all');
  const [reviewStatus, setReviewStatus] = useState('all');
  const [selected, setSelected] = useState<HistoricalInventoryReportRow | null>(null);
  const [nextStatus, setNextStatus] = useState<HistoricalReviewStatus>('in_review');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);

  const load = async () => {
    setBusy(true);
    try {
      const response = await historicalReviewApi.report();
      setRows(response.rows || []);
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'گزارش تطبیق دریافت نشد.');
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const classifications = useMemo(() => Array.from(new Set(rows.flatMap((row) => row.classifications))).sort(), [rows]);
  const filtered = useMemo(() => rows.filter((row) => {
    const haystack = `${row.productId} ${row.productTitle} ${row.supplierId}`.toLowerCase();
    return (!query || haystack.includes(query.toLowerCase()))
      && (classification === 'all' || row.classifications.includes(classification))
      && (reviewStatus === 'all' || row.reviewStatus === reviewStatus);
  }), [rows, query, classification, reviewStatus]);

  const submitReview = async () => {
    if (!selected || !reason.trim()) {
      error('دلیل تصمیم انسانی الزامی است.');
      return;
    }
    setBusy(true);
    try {
      await historicalReviewApi.review(selected.productId, { status: nextStatus, reason, evidence: selected.evidenceIds });
      success('وضعیت بررسی انسانی ثبت شد.');
      setSelected(null);
      setReason('');
      await load();
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت تصمیم بررسی انجام نشد.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-3.5 pb-6">
      <section className="rounded-2xl border border-slate-200/90 bg-white p-3.5 shadow-2xs space-y-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <ClipboardCheck className="h-4 w-4 text-amber-600"  strokeWidth={1.5}/>
            <div>
              <h2 className="text-sm font-extrabold text-slate-900">بررسی انسانی تطبیق موجودی تاریخی</h2>
              <p className="text-[10px] text-slate-500">طبقه‌بندی داده از وضعیت بررسی انسانی جداست. این صفحه هیچ موجودی تاریخی را تغییر نمی‌دهد.</p>
            </div>
          </div>
          <button type="button" onClick={() => void load()} disabled={busy} title="به‌روزرسانی گزارش" className="rounded-xl border border-slate-200 p-2 text-slate-600 disabled:opacity-50">
            <RefreshCw className={`h-4 w-4 ${busy ? 'animate-spin' : ''}`}  strokeWidth={1.5}/>
          </button>
        </div>
        <div className="grid grid-cols-1 gap-2 md:grid-cols-4">
          <label className="relative md:col-span-2">
            <Search className="absolute right-2 top-2.5 h-3.5 w-3.5 text-slate-400"  strokeWidth={1.5}/>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="جستجوی محصول، تأمین‌کننده یا شناسه" className="w-full rounded-xl border border-slate-200 bg-slate-50 py-2 pl-2 pr-8 text-xs" />
          </label>
          <select value={classification} onChange={(event) => setClassification(event.target.value)} className="rounded-xl border border-slate-200 bg-slate-50 p-2 text-xs">
            <option value="all">همه طبقه‌بندی‌ها</option>
            {classifications.map((item) => <option key={item} value={item}>{item}</option>)}
          </select>
          <select value={reviewStatus} onChange={(event) => setReviewStatus(event.target.value)} className="rounded-xl border border-slate-200 bg-slate-50 p-2 text-xs">
            <option value="all">همه وضعیت‌های بررسی</option>
            {REVIEW_STATUSES.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
          </select>
        </div>
        <div className="grid grid-cols-2 gap-2 text-[10px] text-slate-600 md:grid-cols-4">
          <span>کل ردیف: <b>{rows.length}</b></span>
          <span>نمایش فعلی: <b>{filtered.length}</b></span>
          <span>نیازمند تصمیم: <b>{rows.filter((row) => row.reviewStatus === 'needs_review').length}</b></span>
          <span>بدون mutation: <b>بله</b></span>
        </div>
      </section>

      <section className="space-y-2">
        {filtered.map((row) => (
          <article key={row.productId} className="rounded-2xl border border-slate-200/90 bg-white p-3 shadow-2xs space-y-2">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h3 className="text-xs font-bold text-slate-900">{row.productTitle}</h3>
                <p className="font-mono text-[10px] text-slate-500">{row.productId} · {row.supplierId || 'مالک اثبات‌نشده'}</p>
              </div>
              <span className="rounded-full bg-amber-50 px-2 py-1 text-[10px] font-bold text-amber-700">{reviewLabel(row.reviewStatus)}</span>
            </div>
            <div className="grid grid-cols-2 gap-1 text-[10px] text-slate-600 md:grid-cols-4">
              <span>legacy: <b>{row.legacyStock}</b></span>
              <span>sellable canonical: <b>{row.canonicalSellableQuantity}</b></span>
              <span>کل canonical: <b>{row.canonicalQuantity}</b></span>
              <span>اختلاف: <b>{row.difference}</b></span>
            </div>
            <div className="flex flex-wrap gap-1">
              {row.classifications.map((item) => <span key={item} className="rounded-full bg-slate-100 px-2 py-1 text-[9px] font-bold text-slate-700">{item}</span>)}
              {row.measurementClassification !== 'VERIFIED' && <span className="rounded-full bg-violet-50 px-2 py-1 text-[9px] font-bold text-violet-700">{row.measurementClassification}</span>}
            </div>
            <div className="flex items-center justify-between gap-2 border-t border-slate-100 pt-2">
              <span className="text-[10px] text-slate-500">location/status: {row.locationBreakdown.length} · reservation فعال: {row.activeReservations.length} · movement: {row.movements.length}</span>
              <button type="button" onClick={() => { setSelected(row); setNextStatus('in_review'); }} className="flex items-center gap-1 rounded-xl bg-amber-600 px-3 py-2 text-[10px] font-bold text-white"><FileSearch className="h-3.5 w-3.5"  strokeWidth={1.5}/>بررسی مورد</button>
            </div>
          </article>
        ))}
      </section>

      {selected && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/50 p-3">
          <div className="max-h-[90vh] w-full max-w-lg overflow-auto rounded-2xl bg-white p-4 shadow-2xl space-y-3" dir="rtl">
            <div className="flex items-center justify-between border-b border-slate-100 pb-2"><h3 className="text-sm font-bold">جزئیات بررسی: {selected.productId}</h3><button type="button" onClick={() => setSelected(null)} className="text-xs text-slate-500">بستن</button></div>
            <div className="grid grid-cols-2 gap-1 text-[10px] text-slate-600"><span>طبقه‌بندی: {selected.classifications.join('، ') || 'ندارد'}</span><span>measurement: {selected.measurementClassification}</span><span>evidence IDs: {selected.evidenceIds.length}</span><span>نیازمند تصمیم انسانی: {selected.evidenceInsufficient ? 'بله' : 'خیر'}</span></div>
            <div className="space-y-1 rounded-xl bg-slate-50 p-2 text-[10px]">{selected.locationBreakdown.map((item) => <p key={`${item.locationKey}-${item.status}`}>{item.locationKey} · {item.status} · {item.quantity} · {item.reconciled ? 'reconciled' : 'unreconciled'}</p>)}</div>
            <select value={nextStatus} onChange={(event) => setNextStatus(event.target.value as HistoricalReviewStatus)} className="w-full rounded-xl border border-slate-200 p-2 text-xs">{REVIEW_STATUSES.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select>
            <textarea value={reason} onChange={(event) => setReason(event.target.value)} placeholder="دلیل تصمیم و شواهد بررسی‌شده" className="min-h-24 w-full rounded-xl border border-slate-200 p-2 text-xs" />
            <button type="button" disabled={busy} onClick={() => void submitReview()} className="flex w-full items-center justify-center gap-1 rounded-xl bg-emerald-600 py-2 text-xs font-bold text-white disabled:opacity-50"><Save className="h-3.5 w-3.5"  strokeWidth={1.5}/>ثبت وضعیت بررسی و audit</button>
          </div>
        </div>
      )}
    </div>
  );
};