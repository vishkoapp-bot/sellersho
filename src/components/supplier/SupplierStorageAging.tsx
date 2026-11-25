import { useCallback, useState } from 'react';
import { Clock3 } from 'lucide-react';
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
  quantity: number;
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
const calendar = new Intl.DateTimeFormat('fa-IR', { timeZone: 'Asia/Tehran', day: 'numeric', month: 'long', year: 'numeric' });
const localDate = (date: string) => calendar.format(new Date(`${date}T12:00:00Z`));

export function SupplierStorageAging() {
  const { toPersianDigits } = useStore();
  const [page, setPage] = useState(0);
  const [expanded, setExpanded] = useState(false);
  const load = useCallback(async () => {
    const result = await apiRequest<StorageAgingReport>(`/supplier/inventory/storage-aging?page=${page}`, { auth: true });
    if (!result || !Array.isArray(result.rows)) throw new Error('سوابق ماندگاری انبار کامل دریافت نشد.');
    return result;
  }, [page]);
  const resource = useSupplierResource(load);
  const report = resource.data;

  return (
    <section dir="rtl" aria-label="ماندگاری موجودی انبار مرکزی" className="space-y-3 rounded-2xl border border-brand-border bg-white p-3.5 sm:p-4">
      <h3 className="flex items-center gap-2 text-sm font-extrabold text-[#261b41]">
        <Clock3 aria-hidden="true" className="size-4 text-brand-deep" />
        ماندگاری موجودی انبار مرکزی
      </h3>
      <SupplierResourceState loading={resource.loading} error={resource.error} retry={resource.reload} />
      {report && !resource.loading && !resource.error && (
        <>
          {report.warning_count > 0 && <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-bold leading-6 text-amber-900">
            {toPersianDigits(report.warning_count)} ردیف به تاریخ شروع ارزیابی هزینه رسیده یا حداکثر {toPersianDigits(report.warning_days)} روز تا آن فاصله دارد.
          </p>}
          {!report.fees_enabled && <p className="text-xs leading-6 text-slate-600">ارزیابی هزینه انبارداری غیرفعال است.</p>}
          {report.unknown_age_count > 0 && <p className="text-xs leading-6 text-slate-600">تاریخ دریافت {toPersianDigits(report.unknown_age_count)} ردیف در سوابق قابل تأیید نیست.</p>}
          {report.unrated_count > 0 && <p className="text-xs leading-6 text-slate-600">برای {toPersianDigits(report.unrated_count)} ردیف هنوز اطلاعات قابل تأیید برای ارزیابی هزینه کامل نیست.</p>}
          {report.rows.length > 0 ? (
            <details open={expanded} onToggle={event => setExpanded(event.currentTarget.open)} className="rounded-xl bg-slate-50 px-3">
              <summary className="min-h-11 cursor-pointer py-3 text-xs font-bold text-brand-deep">دیدن تاریخ دریافت و ماندگاری کالاها</summary>
              <div className="space-y-2 pb-3">
                <div className="overflow-x-auto">
                  <table className="w-full min-w-[34rem] border-separate border-spacing-y-1 text-right text-xs">
                    <thead className="text-slate-600">
                      <tr>
                        <th scope="col" className="px-2 py-1">کالا و انبار</th>
                        <th scope="col" className="px-2 py-1">تعداد در انبار</th>
                        <th scope="col" className="px-2 py-1">تاریخ دریافت</th>
                        <th scope="col" className="px-2 py-1">مدت ماندگاری</th>
                        {report.fees_enabled && <th scope="col" className="px-2 py-1">آغاز بازه ارزیابی هزینه</th>}
                      </tr>
                    </thead>
                    <tbody>
                      {report.rows.map(row => <tr key={row.id} className="bg-white text-slate-800">
                        <th scope="row" className="rounded-s-lg px-2 py-2 text-right">
                          <span className="block">{row.product_title || 'کالای ثبت‌شده'}</span>
                          <span className="block font-normal text-slate-600">{[row.variant_name, row.warehouse_name].filter(Boolean).join(' · ')}</span>
                          {row.spnc && <bdi dir="ltr" className="font-mono text-[11px]">{row.spnc}</bdi>}
                        </th>
                        <td className="px-2 py-2">{toPersianDigits(row.quantity)}</td>
                        <td className="px-2 py-2">{localDate(row.received_on)}</td>
                        <td className="px-2 py-2">{toPersianDigits(row.age_days)} روز</td>
                        {report.fees_enabled && <td className={`rounded-e-lg px-2 py-2 ${row.fee_starts_on && row.fee_starts_on <= report.as_of ? 'font-bold text-amber-900' : ''}`}>
                          {row.fee_starts_on ? localDate(row.fee_starts_on) : '—'}
                        </td>}
                      </tr>)}
                    </tbody>
                  </table>
                </div>
                {report.fees_enabled && <p className="text-xs leading-6 text-slate-600">تاریخ جدول آغاز بازه ارزیابی است. مبالغ ثبت‌شده در گزارش مالی دیده می‌شوند.</p>}
                {(page > 0 || report.has_more) && <div className="flex flex-wrap gap-2">
                  <button type="button" className={pageButton} onClick={() => setPage(index => index - 1)} disabled={page === 0}>صفحه قبل</button>
                  <button type="button" className={pageButton} onClick={() => setPage(index => index + 1)} disabled={!report.has_more}>صفحه بعد</button>
                </div>}
              </div>
            </details>
          ) : (
            <div className="space-y-2">
              {report.unknown_age_count === 0 && <p className="text-xs leading-6 text-slate-600">برای موجودی فعلی، ردیف ماندگاری ثبت نشده است.</p>}
              {page > 0 && <button type="button" className={pageButton} onClick={() => setPage(index => index - 1)}>صفحه قبل</button>}
            </div>
          )}
        </>
      )}
    </section>
  );
}
