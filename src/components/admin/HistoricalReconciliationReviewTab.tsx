import React, { useEffect, useMemo, useState } from 'react';
import { ClipboardCheck, FileSearch, RefreshCw, Save, Search } from 'lucide-react';
import { useToast } from '../../context/ToastContext';
import { historicalReviewApi, HistoricalInventoryReportRow, HistoricalReviewStatus } from '../../services/adminApi';
import { toPersianDigits } from '../../utils/formatters';

// ---------------------------------------------------------------------------
// Plain-Persian labels for the backend's classification codes.
// Every code the backend can emit (classifyHistoricalRow +
// measurementClassification in historical_reconciliation_report.go)
// must have an entry here — see the test's coverage assertions.
// ---------------------------------------------------------------------------

export const CLASSIFICATION_LABELS: Record<string, string> = {
  MATCHED: 'همه‌چیز می‌خواند',
  LEGACY_ONLY: 'فقط عدد قدیمی هست؛ در دفتر جدید چیزی ثبت نشده',
  CANONICAL_ONLY: 'فقط دفتر جدید موجودی دارد؛ عدد قدیمی صفر است',
  QUANTITY_MISMATCH: 'عدد قدیمی با قابل‌فروشِ دفتر جدید نمی‌خواند',
  EVIDENCE_INSUFFICIENT: 'بعضی ردیف‌های دفتر تطبیق‌نیافته‌اند؛ مالک یا مکان مشخص نیست',
  MEASUREMENT_MISSING: 'کالا در انبار هست ولی اندازه و وزنش ثبت نشده',
  CATALOG_ONLY_MEASUREMENT_MISSING: 'کالا فقط در کاتالوگ هست؛ نه موجودی دارد نه اندازه',
  MEASUREMENT_UNVERIFIED: 'اندازه‌ها ثبت شده ولی مورد اختلاف است',
  PHYSICAL_PRESENCE_UNCONFIRMED: 'حضور فیزیکی کالا تأیید نشده',
};

export const MEASUREMENT_LABELS: Record<string, string> = {
  VERIFIED: 'هر چهار اندازه ثبت شده است',
  PHYSICAL_MEASUREMENT_MISSING: 'کالای فیزیکی هست ولی اندازه ندارد',
  MEASUREMENT_UNVERIFIED: 'اندازه‌ها ثبت شده ولی مورد اختلاف است',
  PHYSICAL_PRESENCE_UNCONFIRMED: 'حضور فیزیکی کالا تأیید نشده',
  CATALOG_ONLY_MEASUREMENT_MISSING: 'فقط رکورد کاتالوگ است؛ نه موجودی، نه اندازه',
};

const REVIEW_STATUSES: Array<{ value: HistoricalReviewStatus; label: string; explanation: string }> = [
  { value: 'needs_review', label: 'نیازمند بررسی', explanation: 'هنوز کسی این اختلاف را بررسی نکرده است.' },
  { value: 'in_review', label: 'در حال بررسی', explanation: 'در حال بررسی هستم؛ هنوز تصمیم نهایی نگرفته‌ام.' },
  { value: 'awaiting_evidence', label: 'منتظر مدرک', explanation: 'برای تصمیم‌گیری به مدرک یا اطلاعات بیشتری نیاز دارم.' },
  { value: 'approved_for_correction', label: 'تأیید برای اصلاح', explanation: 'بررسی کردم و موافق اصلاح جداگانه هستم؛ با ثبت آن هیچ عددی عوض نمی‌شود.' },
  { value: 'rejected', label: 'رد شده', explanation: 'این اختلاف را قبول ندارم؛ دلیلم را می‌نویسم.' },
  { value: 'resolved', label: 'حل‌شده', explanation: 'اختلاف بیرون از این صفحه برطرف شده و دیگر چیزی برای بررسی نیست.' },
  { value: 'indeterminate', label: 'غیرقابل تعیین تکلیف', explanation: 'با اطلاعات موجود نمی‌شود تکلیف این اختلاف را مشخص کرد.' },
];

const reviewLabel = (value: string) => REVIEW_STATUSES.find((item) => item.value === value)?.label || value;
const reviewExplanation = (value: string) => REVIEW_STATUSES.find((item) => item.value === value)?.explanation || '';

const classificationLabel = (code: string) => CLASSIFICATION_LABELS[code] || code;
const measurementLabel = (code: string) => MEASUREMENT_LABELS[code] || code;

// Balance-status codes from inventory_balances (unknown codes fall back
// to the raw code so nothing is ever hidden).
const BALANCE_STATUS_LABELS: Record<string, string> = {
  sellable: 'قابل‌فروش',
  quarantine: 'قرنطینه',
  damaged: 'خراب',
  reserved: 'رزروشده',
  in_transit: 'در راه',
  returned: 'مرجوعی',
  held: 'نگه‌داشته‌شده',
  expired: 'منقضی‌شده',
};

const LOCATION_TYPE_LABELS: Record<string, string> = {
  warehouse: 'انبار',
  bin: 'قفسه',
  in_transit: 'در راه',
  supplier: 'انبار تأمین‌کننده',
};

// inventory_movements.reference_type is free-form; known values are
// translated, anything else is shown as-is.
const MOVEMENT_REFERENCE_LABELS: Record<string, string> = {
  inbound: 'ورود به انبار',
  inbound_received: 'ورود به انبار',
  qc: 'کنترل کیفیت',
  qc_pass: 'کنترل کیفیت (قبول)',
  qc_passed: 'کنترل کیفیت (قبول)',
  qc_failed: 'کنترل کیفیت (رد)',
  qc_deadline_review: 'بازبینی مهلت کنترل کیفیت',
  putaway: 'جابه‌جایی به قفسه',
  putaway_confirmed: 'جابه‌جایی به قفسه',
  transfer: 'جابه‌جایی',
  transfer_confirmed: 'جابه‌جایی',
  order: 'سفارش',
  order_capture: 'ثبت سفارش',
  order_cancel: 'لغو سفارش',
  order_status: 'تغییر وضعیت سفارش',
  return_requested: 'درخواست مرجوعی',
  returned: 'مرجوعی',
  return_rejected: 'رد مرجوعی',
  adjustment: 'اصلاح دستی',
  reservation: 'رزرو',
};

const fa = (value: number | string) => toPersianDigits(value);

export const formatFaDate = (value: string) => {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString('fa-IR');
};

// ---------------------------------------------------------------------------
// Verdict: derived from the difference (legacy − sellable), not from the
// raw suggestedAction string. The backend's suggestedAction is one constant
// for every row ("manual review; no automatic mutation"), so it is never
// rendered — the standing instruction below replaces it.
// ---------------------------------------------------------------------------

export type VerdictKey = 'matched' | 'surplus' | 'shortage';

export const deriveVerdict = (row: Pick<HistoricalInventoryReportRow, 'difference' | 'classifications'>): { key: VerdictKey; label: string; pillClass: string } => {
  if (row.difference > 0) return { key: 'shortage', label: 'کسری', pillClass: 'bg-red-50 text-red-700' };
  if (row.difference < 0) return { key: 'surplus', label: 'مازاد', pillClass: 'bg-amber-50 text-amber-700' };
  return { key: 'matched', label: 'مطابقت دارد', pillClass: 'bg-emerald-50 text-emerald-700' };
};

export const differenceGloss = (row: Pick<HistoricalInventoryReportRow, 'difference'>): string => {
  if (row.difference > 0) return `عدد قدیمی ${fa(row.difference)} عدد بیشتر نشان می‌دهد`;
  if (row.difference < 0) return `دفتر جدید ${fa(-row.difference)} عدد بیشتر نشان می‌دهد`;
  return 'عدد قدیمی و قابل‌فروشِ دفتر جدید می‌خوانند';
};

export const formatSignedDifference = (difference: number): string => {
  if (difference > 0) return `+${fa(difference)}`;
  if (difference < 0) return `−${fa(-difference)}`;
  return fa(0);
};

// Next step, derived in the frontend from the classifications — the
// backend emits no per-row recommendation.
export const nextStepForRow = (row: Pick<HistoricalInventoryReportRow, 'classifications'>): string => {
  const codes = row.classifications;
  if (codes.includes('MEASUREMENT_MISSING')) return 'اندازه و وزن کالا را ثبت کنید.';
  if (codes.includes('CATALOG_ONLY_MEASUREMENT_MISSING')) return 'این کالا فقط در کاتالوگ است؛ اگر واقعاً موجودی دارد، آن را در انبار ثبت کنید و اندازه و وزنش را کامل کنید.';
  if (codes.includes('EVIDENCE_INSUFFICIENT')) return 'ردیف‌های تطبیق‌نیافته را بررسی کنید؛ مالک یا مکان آن‌ها مشخص نیست.';
  if (codes.includes('LEGACY_ONLY')) return 'کالا را در انبار بشمارید و در دفتر جدید ثبت کنید؛ فعلاً فقط عدد قدیمی وجود دارد.';
  if (codes.includes('CANONICAL_ONLY')) return 'عدد قدیمی را بررسی کنید؛ دفتر جدید موجودی نشان می‌دهد ولی عدد قدیمی صفر است.';
  if (codes.includes('QUANTITY_MISMATCH')) return 'عدد قدیمی و قابل‌فروشِ دفتر جدید را با شمارش واقعی مقایسه کنید.';
  if (codes.includes('MEASUREMENT_UNVERIFIED')) return 'اندازه‌های مورد اختلاف را بازبینی کنید.';
  if (codes.includes('PHYSICAL_PRESENCE_UNCONFIRMED')) return 'حضور فیزیکی کالا در انبار را تأیید کنید.';
  return 'نیازی به اقدام نیست؛ فقط نتیجه بررسی را ثبت کنید.';
};

const primaryClassification = (row: HistoricalInventoryReportRow): string => row.classification || row.classifications[0] || 'MATCHED';
const secondaryClassifications = (row: HistoricalInventoryReportRow): string[] => {
  const primary = primaryClassification(row);
  return row.classifications.filter((code) => code !== primary);
};

const locationLabel = (item: HistoricalInventoryReportRow['locationBreakdown'][number]): string => {
  const place = item.warehouseName || LOCATION_TYPE_LABELS[item.locationType] || item.locationKey;
  const bin = item.binCode ? `، قفسه ${item.binCode}` : '';
  return `${place}${bin} · وضعیت: ${BALANCE_STATUS_LABELS[item.status] || item.status} · ${fa(item.quantity)} عدد · ${item.reconciled ? 'تطبیق‌شده' : 'تطبیق‌نیافته'}`;
};

export interface HistoricalReconciliationRowCardProps {
  row: HistoricalInventoryReportRow;
  onReview: (row: HistoricalInventoryReportRow) => void;
}

export const HistoricalReconciliationRowCard: React.FC<HistoricalReconciliationRowCardProps> = ({ row, onReview }) => {
  const verdict = deriveVerdict(row);
  const primary = primaryClassification(row);
  const secondary = secondaryClassifications(row);
  return (
    <article className="rounded-2xl border border-slate-200/90 bg-white p-3 shadow-2xs space-y-2">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-xs font-bold text-slate-900">{row.productTitle}</h3>
          <p className="font-mono text-[10px] text-slate-500">{row.productId} · {row.supplierId || 'مالک اثبات‌نشده'}</p>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1">
          <span className={`rounded-full px-2 py-1 text-[10px] font-bold ${verdict.pillClass}`}>{verdict.label}</span>
          <span className="rounded-full bg-amber-50 px-2 py-1 text-[10px] font-bold text-amber-700">{reviewLabel(row.reviewStatus)}</span>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-1 text-[10px] text-slate-600 md:grid-cols-5">
        <span>موجودی قدیمی: <b>{fa(row.legacyStock)}</b></span>
        <span>موجودی جدید (کل): <b>{fa(row.canonicalQuantity)}</b></span>
        <span>قابل‌فروش: <b>{fa(row.canonicalSellableQuantity)}</b></span>
        <span>موجودی فیزیکی در انبار: <b>{fa(row.physicalQuantity)}</b></span>
        <span>اختلاف: <b>{formatSignedDifference(row.difference)}</b></span>
      </div>
      <p className="text-[10px] text-slate-500">{differenceGloss(row)}</p>
      <div className="flex flex-wrap gap-1">
        <span className={`rounded-full px-2 py-1 text-[9px] font-bold ${verdict.pillClass}`}>{classificationLabel(primary)}</span>
        {secondary.map((code) => <span key={code} className="rounded-full bg-slate-100 px-2 py-1 text-[9px] font-bold text-slate-700">{classificationLabel(code)}</span>)}
        <span className="rounded-full bg-violet-50 px-2 py-1 text-[9px] font-bold text-violet-700">اندازه و وزن: {measurementLabel(row.measurementClassification)}</span>
      </div>
      {row.unreconciledRows > 0 && (
        <p className="text-[10px] text-slate-500">{fa(row.unreconciledRows)} ردیف از {fa(row.canonicalRows)} ردیف تطبیق‌نیافته‌اند.</p>
      )}
      <p className="text-[10px] text-slate-600">بررسی دستی؛ هیچ اصلاح خودکاری انجام نمی‌شود. اقدام بعدی: {nextStepForRow(row)}</p>
      <p className="text-[10px] text-slate-500">
        مدرک ورود: {row.hasInboundEvidence ? 'دارد' : 'ندارد'} · مدرک کنترل کیفیت: {row.hasQCEvidence ? 'دارد' : 'ندارد'} · مدرک جابه‌جایی: {row.hasPutawayEvidence ? 'دارد' : 'ندارد'} · مدرک مالی: {row.hasLedgerEvidence ? 'دارد' : 'ندارد'}
      </p>
      <div className="flex items-center justify-between gap-2 border-t border-slate-100 pt-2">
        <span className="text-[10px] text-slate-500">مکان‌ها: {fa(row.locationBreakdown.length)} · رزرو فعال: {fa(row.activeReservations.length)} · جابه‌جایی: {fa(row.movements.length)}</span>
        <button type="button" onClick={() => onReview(row)} className="flex items-center gap-1 rounded-xl bg-amber-600 px-3 py-2 text-[10px] font-bold text-white"><FileSearch className="h-3.5 w-3.5" strokeWidth={1.5} />بررسی مورد</button>
      </div>
    </article>
  );
};

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
            <ClipboardCheck className="h-4 w-4 shrink-0 text-amber-600" strokeWidth={1.5} />
            <div>
              <h2 className="text-sm font-extrabold text-slate-900">بررسی انسانی تطبیق موجودی تاریخی</h2>
              <p className="text-[10px] text-slate-500">این صفحه عدد قدیمی هر کالا را با دفتر جدید انبار مقایسه می‌کند؛ طبقه‌بندی داده از وضعیت بررسی انسانی جداست.</p>
            </div>
          </div>
          <button type="button" onClick={() => void load()} disabled={busy} title="به‌روزرسانی گزارش" className="rounded-xl border border-slate-200 p-2 text-slate-600 disabled:opacity-50">
            <RefreshCw className={`h-4 w-4 ${busy ? 'animate-spin' : ''}`} strokeWidth={1.5} />
          </button>
        </div>
        <div className="rounded-xl bg-amber-50/70 p-3 text-[11px] leading-5 text-slate-700 space-y-2">
          <p><b>این بخش چیست؟</b> قدیم موجودی هر کالا فقط یک عدد بود که در صفحه کالا ثبت می‌شد («موجودی قدیمی»). بعداً سیستم انبار عوض شد و حالا برای هر کالا یک «دفتر» دقیق هست: هر مقدار کالا در کدام انبار، کدام قفسه و با چه وضعیتی (قابل‌فروش، قرنطینه، خراب و…) قرار دارد («موجودی جدید»). این صفحه آن دو عدد را کنار هم می‌گذارد تا ببینیم آیا عدد قدیمی با دفتر جدید می‌خواند یا نه. اگر نخواند، یعنی یا عدد قدیمی اشتباه بوده، یا کالایی در انبار هست که در دفتر ثبت نشده، یا برعکس.</p>
          <ul className="list-disc space-y-1 pr-4">
            <li><b>موجودی قدیمی:</b> عددی که قبلاً برای کالا ثبت شده بود.</li>
            <li><b>موجودی جدید (کل):</b> جمع همه ردیف‌های دفتر انبار، با هر وضعیتی.</li>
            <li><b>قابل‌فروش:</b> بخشی از موجودی جدید که وضعیت «قابل‌فروش» دارد؛ واقعاً می‌شود فروختش.</li>
            <li><b>موجودی فیزیکی:</b> مقداری که واقعاً در انبار، قفسه یا در راه است، بدون توجه به وضعیت.</li>
            <li><b>اختلاف:</b> موجودی قدیمی منهای قابل‌فروش — مثبت یعنی عدد قدیمی بیشتر از چیزی است که دفتر می‌گوید می‌شود فروخت؛ منفی یعنی دفتر بیشتر نشان می‌دهد.</li>
          </ul>
          <p className="font-bold text-amber-800">این صفحه هیچ موجودی را تغییر نمی‌دهد؛ ثبت تصمیم فقط یک یادداشت بررسی است — حتی «تأیید برای اصلاح» به‌تنهایی چیزی را اصلاح نمی‌کند و اصلاح باید جداگانه انجام شود.</p>
        </div>
        <div className="grid grid-cols-1 gap-2 md:grid-cols-4">
          <label className="relative md:col-span-2">
            <Search className="absolute right-2 top-2.5 h-3.5 w-3.5 text-slate-400" strokeWidth={1.5} />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="جستجوی محصول، تأمین‌کننده یا شناسه" className="w-full rounded-xl border border-slate-200 bg-slate-50 py-2 pl-2 pr-8 text-xs" />
          </label>
          <select value={classification} onChange={(event) => setClassification(event.target.value)} className="rounded-xl border border-slate-200 bg-slate-50 p-2 text-xs">
            <option value="all">همه طبقه‌بندی‌ها</option>
            {classifications.map((item) => <option key={item} value={item}>{classificationLabel(item)}</option>)}
          </select>
          <select value={reviewStatus} onChange={(event) => setReviewStatus(event.target.value)} className="rounded-xl border border-slate-200 bg-slate-50 p-2 text-xs">
            <option value="all">همه وضعیت‌های بررسی</option>
            {REVIEW_STATUSES.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
          </select>
        </div>
        <div className="grid grid-cols-2 gap-2 text-[10px] text-slate-600 md:grid-cols-4">
          <span>کل ردیف: <b>{fa(rows.length)}</b></span>
          <span>نمایش فعلی: <b>{fa(filtered.length)}</b></span>
          <span>نیازمند تصمیم: <b>{fa(rows.filter((row) => row.reviewStatus === 'needs_review').length)}</b></span>
          <span>تغییر موجودی: <b>هرگز</b></span>
        </div>
      </section>

      <section className="space-y-2">
        {filtered.map((row) => (
          <HistoricalReconciliationRowCard key={row.productId} row={row} onReview={(item) => { setSelected(item); setNextStatus('in_review'); }} />
        ))}
      </section>

      {selected && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/50 p-3">
          <div className="max-h-[90vh] w-full max-w-lg overflow-auto rounded-2xl bg-white p-4 shadow-2xl space-y-3" dir="rtl">
            <div className="flex items-center justify-between border-b border-slate-100 pb-2"><h3 className="text-sm font-bold">جزئیات بررسی: {selected.productTitle}</h3><button type="button" onClick={() => setSelected(null)} className="text-xs text-slate-500">بستن</button></div>
            <div className="flex flex-wrap items-center gap-1">
              <span className={`rounded-full px-2 py-1 text-[10px] font-bold ${deriveVerdict(selected).pillClass}`}>{deriveVerdict(selected).label}</span>
              <span className="rounded-full bg-amber-50 px-2 py-1 text-[10px] font-bold text-amber-700">وضعیت فعلی بررسی: {reviewLabel(selected.reviewStatus)}</span>
            </div>
            <div className="grid grid-cols-2 gap-1 text-[10px] text-slate-600">
              <span>موجودی قدیمی: <b>{fa(selected.legacyStock)}</b></span>
              <span>موجودی جدید (کل): <b>{fa(selected.canonicalQuantity)}</b></span>
              <span>قابل‌فروش: <b>{fa(selected.canonicalSellableQuantity)}</b></span>
              <span>موجودی فیزیکی در انبار: <b>{fa(selected.physicalQuantity)}</b></span>
              <span>اختلاف: <b>{formatSignedDifference(selected.difference)}</b></span>
              <span>جابه‌جایی ثبت‌شده: <b>{fa(selected.movementQuantity)}</b></span>
            </div>
            <p className="text-[10px] text-slate-500">{differenceGloss(selected)}</p>
            <div className="space-y-1 text-[10px] text-slate-600">
              <p><b>دلیل اختلاف:</b> {classificationLabel(primaryClassification(selected))}{secondaryClassifications(selected).map((code) => `؛ ${classificationLabel(code)}`).join('')}</p>
              <p><b>اندازه و وزن:</b> {measurementLabel(selected.measurementClassification)}</p>
              {selected.unreconciledRows > 0 && <p>{fa(selected.unreconciledRows)} ردیف از {fa(selected.canonicalRows)} ردیف تطبیق‌نیافته‌اند.</p>}
              <p>بررسی دستی؛ هیچ اصلاح خودکاری انجام نمی‌شود. اقدام بعدی: {nextStepForRow(selected)}</p>
              <p>مدرک ورود: {selected.hasInboundEvidence ? 'دارد' : 'ندارد'} · مدرک کنترل کیفیت: {selected.hasQCEvidence ? 'دارد' : 'ندارد'} · مدرک جابه‌جایی: {selected.hasPutawayEvidence ? 'دارد' : 'ندارد'} · مدرک مالی: {selected.hasLedgerEvidence ? 'دارد' : 'ندارد'}</p>
            </div>
            {selected.locationBreakdown.length > 0 && (
              <div className="space-y-1 rounded-xl bg-slate-50 p-2 text-[10px]">
                <p className="font-bold text-slate-700">مکان‌ها و وضعیت‌ها:</p>
                {selected.locationBreakdown.map((item) => <p key={`${item.locationKey}-${item.status}`}>{locationLabel(item)}</p>)}
              </div>
            )}
            {selected.activeReservations.length > 0 && (
              <div className="space-y-1 rounded-xl bg-slate-50 p-2 text-[10px]">
                <p className="font-bold text-slate-700">رزروهای فعال:</p>
                {selected.activeReservations.map((item) => <p key={item.id}>رزرو فعال: {fa(item.quantity)} عدد برای سفارش {item.referenceId}</p>)}
              </div>
            )}
            {selected.movements.length > 0 && (
              <div className="space-y-1 rounded-xl bg-slate-50 p-2 text-[10px]">
                <p className="font-bold text-slate-700">جابه‌جایی‌ها:</p>
                {selected.movements.map((item) => (
                  <p key={item.id}>
                    {MOVEMENT_REFERENCE_LABELS[item.referenceType] || item.referenceType} · {fa(item.quantity)} عدد · {formatFaDate(item.createdAt)}
                    {item.fromStatus || item.toStatus ? ` · از ${BALANCE_STATUS_LABELS[item.fromStatus || ''] || item.fromStatus || '—'} به ${BALANCE_STATUS_LABELS[item.toStatus || ''] || item.toStatus || '—'}` : ''}
                  </p>
                ))}
              </div>
            )}
            {selected.measurementEvidence.length > 0 && (
              <div className="space-y-1 rounded-xl bg-slate-50 p-2 text-[10px]">
                <p className="font-bold text-slate-700">سوابق اندازه و وزن:</p>
                {selected.measurementEvidence.map((item) => (
                  <p key={item.id}>طول {fa(item.newLengthMm)} × عرض {fa(item.newWidthMm)} × ارتفاع {fa(item.newHeightMm)} میلی‌متر، وزن {fa(item.newWeightGrams)} گرم{item.reason ? ` — ${item.reason}` : ''} — {formatFaDate(item.createdAt)}</p>
                ))}
              </div>
            )}
            <select value={nextStatus} onChange={(event) => setNextStatus(event.target.value as HistoricalReviewStatus)} className="w-full rounded-xl border border-slate-200 p-2 text-xs">{REVIEW_STATUSES.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select>
            <p className="text-[11px] leading-5 text-slate-500">{reviewExplanation(nextStatus)}</p>
            <textarea value={reason} onChange={(event) => setReason(event.target.value)} placeholder="دلیل تصمیم و شواهد بررسی‌شده" className="min-h-24 w-full rounded-xl border border-slate-200 p-2 text-xs" />
            <button type="button" disabled={busy} onClick={() => void submitReview()} className="flex w-full items-center justify-center gap-1 rounded-xl bg-emerald-600 py-2 text-xs font-bold text-white disabled:opacity-50"><Save className="h-3.5 w-3.5" strokeWidth={1.5} />ثبت تصمیم و ذخیره در سوابق</button>
            <p className="text-[10px] text-slate-500">ثبت تصمیم فقط یک یادداشت بررسی ذخیره می‌کند؛ هیچ موجودی تغییر نمی‌کند.</p>
          </div>
        </div>
      )}
    </div>
  );
};
