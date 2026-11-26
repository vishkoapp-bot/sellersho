import { useCallback, useRef, useState } from 'react';
import { ChevronDown, Clock3 } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { apiRequest } from '../../services/apiClient';
import { SupplierResourceState } from './SupplierResourceState';
import { useSupplierResource } from './useSupplierResource';

interface StorageAgingRow {
  id: string;
  product_title: string;
  variant_name: string;
  spnc: string;
  warehouse_name: string;
  quantity: number | null;
  received_on: string;
  age_days: number;
  fee_starts_on?: string;
}

interface StorageAgingReport {
  fees_enabled: boolean;
  as_of: string;
  warning_count: number;
  warning_days: number;
  unknown_age_count: number;
  unrated_count: number;
  has_more: boolean;
  rows: StorageAgingRow[];
}

const pageButton = 'min-h-11 rounded-lg border border-brand-border px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50';
const cellClass = 'min-w-0 px-2 pb-2 sm:table-cell sm:py-2';
const phoneLabel = 'block text-xs text-slate-600 sm:hidden';
const calendar = new Intl.DateTimeFormat('fa-IR', { timeZone: 'Asia/Tehran', day: 'numeric', month: 'long', year: 'numeric' });
const localDate = (date: string) => calendar.format(new Date(`${date}T12:00:00Z`));

export function SupplierStorageAging() {
  const { toPersianDigits } = useStore();
  const [page, setPage] = useState(0);
  const [expanded, setExpanded] = useState(false);
  const reportSummary = useRef<HTMLElement>(null);
  const load = useCallback(async () => {
    const result = await apiRequest<StorageAgingReport>(`/supplier/inventory/storage-aging?page=${page}`, { auth: true });
    if (!result || !Array.isArray(result.rows)) throw new Error('سوابق ماندگاری انبار کامل دریافت نشد.');
    return result;
  }, [page]);
  const resource = useSupplierResource(load);
  const report = resource.loading || resource.error ? null : resource.data;
  const showReport = !!report || expanded || page > 0;
  const movePage = (offset: number) => {
    reportSummary.current?.focus();
    setPage(index => index + offset);
  };

  return (
    <section dir="rtl" aria-label="ماندگاری موجودی انبار مرکزی" className="space-y-3 rounded-2xl border border-brand-border bg-white p-3.5 sm:p-4">
      <h3 className="flex items-center gap-2 text-sm font-extrabold text-[#261b41]">
        <Clock3 aria-hidden="true" className="size-4 text-brand-deep" />
        ماندگاری موجودی انبار مرکزی
      </h3>
      {report && report.warning_count > 0 && <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-bold leading-6 text-amber-900">
        {toPersianDigits(report.warning_count)} ردیف به تاریخ شروع ارزیابی هزینه رسیده یا حداکثر {toPersianDigits(report.warning_days)} روز تا آن فاصله دارد.
      </p>}
      {!showReport && <SupplierResourceState loading={resource.loading} error={resource.error} retry={resource.reload} />}
      <details hidden={!showReport} open={expanded} onToggle={event => setExpanded(event.currentTarget.open)} className="group rounded-xl border border-slate-200 px-3">
        <summary ref={reportSummary} className="flex min-h-11 cursor-pointer list-none scroll-mt-24 items-center justify-between gap-2 rounded-lg py-2.5 text-xs font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
          تاریخ دریافت و ماندگاری کالاها
          <ChevronDown aria-hidden="true" className="size-4 shrink-0 group-open:rotate-180" />
        </summary>
        <div className="space-y-3 pb-3">
          {showReport && <SupplierResourceState loading={resource.loading} error={resource.error} retry={resource.reload} />}
          {report && <>
            {report.rows.length > 0 ? (
              <table role="table" className="block w-full border-separate border-spacing-y-1 text-start text-xs sm:table">
                <thead role="rowgroup" className="sr-only text-slate-600 sm:not-sr-only sm:table-header-group">
                  <tr role="row">
                    <th role="columnheader" scope="col" className="px-2 py-1 text-start">کالا و انبار</th>
                    <th role="columnheader" scope="col" className="px-2 py-1 text-start">تعداد در انبار</th>
                    <th role="columnheader" scope="col" className="px-2 py-1 text-start">تاریخ دریافت</th>
                    <th role="columnheader" scope="col" className="px-2 py-1 text-start">مدت ماندگاری</th>
                    {report.fees_enabled && <th role="columnheader" scope="col" className="px-2 py-1 text-start">آغاز بازه ارزیابی هزینه</th>}
                  </tr>
                </thead>
                <tbody role="rowgroup" className="block space-y-2 sm:table-row-group sm:space-y-0">
                  {report.rows.map(row => <tr role="row" key={row.id} className="grid grid-cols-2 gap-1 rounded-lg bg-slate-50 text-slate-800 sm:table-row">
                    <th role="rowheader" scope="row" className="col-span-2 min-w-0 border-b border-slate-200 px-2 py-2 text-start sm:table-cell sm:rounded-s-lg sm:border-0">
                      <span className="block break-words">{row.product_title || 'کالای ثبت‌شده'}</span>
                      <span className="block break-words font-normal text-slate-600">{[row.variant_name, row.warehouse_name].filter(Boolean).join(' · ')}</span>
                      {row.spnc && <bdi dir="ltr" className="break-all font-mono text-[11px]">{row.spnc}</bdi>}
                    </th>
                    <td role="cell" className={cellClass}><span aria-hidden="true" className={phoneLabel}>تعداد در انبار</span>{row.quantity === null ? 'نامشخص' : toPersianDigits(row.quantity)}</td>
                    <td role="cell" className={cellClass}><span aria-hidden="true" className={phoneLabel}>تاریخ دریافت</span><time dateTime={row.received_on}>{localDate(row.received_on)}</time></td>
                    <td role="cell" className={`${cellClass} ${report.fees_enabled ? '' : 'sm:rounded-e-lg'}`}><span aria-hidden="true" className={phoneLabel}>مدت ماندگاری</span>{toPersianDigits(row.age_days)} روز</td>
                    {report.fees_enabled && <td role="cell" className={`${cellClass} sm:rounded-e-lg ${row.fee_starts_on && row.fee_starts_on <= report.as_of ? 'font-bold text-amber-900' : ''}`}>
                      <span aria-hidden="true" className={phoneLabel}>آغاز بازه ارزیابی هزینه</span>
                      {row.fee_starts_on ? <time dateTime={row.fee_starts_on}>{localDate(row.fee_starts_on)}</time> : '—'}
                    </td>}
                  </tr>)}
                </tbody>
              </table>
            ) : report.unknown_age_count === 0 && <p className="text-xs leading-6 text-slate-600">برای موجودی فعلی، ردیف ماندگاری ثبت نشده است.</p>}
            <ul className="space-y-1 text-xs leading-6 text-slate-600">
              <li>{report.fees_enabled ? 'تاریخ جدول آغاز بازه ارزیابی است. مبالغ ثبت‌شده در گزارش مالی دیده می‌شوند.' : 'ارزیابی هزینه انبارداری غیرفعال است.'}</li>
              {report.unknown_age_count > 0 && <li>تاریخ دریافت {toPersianDigits(report.unknown_age_count)} ردیف در سوابق قابل تأیید نیست.</li>}
              {report.unrated_count > 0 && <li>برای {toPersianDigits(report.unrated_count)} ردیف هنوز اطلاعات قابل تأیید برای ارزیابی هزینه کامل نیست.</li>}
            </ul>
          </>}
          {(page > 0 || report?.has_more) && <nav aria-label="صفحه‌های ماندگاری موجودی" className="flex flex-wrap items-center gap-2">
            <button type="button" className={pageButton} onClick={() => movePage(-1)} disabled={page === 0 || resource.loading}>صفحه قبل</button>
            <span role="status" className="px-1 text-xs text-slate-600">صفحه {toPersianDigits(page + 1)}</span>
            <button type="button" className={pageButton} onClick={() => movePage(1)} disabled={!report?.has_more}>صفحه بعد</button>
          </nav>}
        </div>
      </details>
    </section>
  );
}
