import { useEffect, useState } from 'react';
import { Boxes, ChevronDown, ChevronLeft, Layers, Package, PackageCheck, ScanLine, Send, ShieldCheck, Warehouse } from 'lucide-react';
import { apiRequest } from '../../services/apiClient';
import { wmsApi, type WarehouseOverviewDto } from '../../services/wmsApi';
import { PanelWorkQueue } from '../common/PanelWorkQueue';
import { followWarehouseLink, receivingStages, shippingStages, warehouseSectionHref, type WarehouseSection } from './WarehouseNavigation';

const digits = new Intl.NumberFormat('fa-IR');
const work = [
  { section: 'receive', label: 'محموله‌های منتظر دریافت', icon: PackageCheck },
  { section: 'qc', label: 'ردیف‌های کنترل کیفیت', icon: ShieldCheck },
  { section: 'putaway', label: 'کارهای جانمایی', icon: Layers },
  { section: 'picking', label: 'سفارش‌های آمادهٔ جمع‌آوری', icon: ScanLine },
  { section: 'packing', label: 'سفارش‌های آمادهٔ بسته‌بندی', icon: Package },
  { section: 'shipping', label: 'سفارش‌های آمادهٔ ارسال', icon: Send },
] as const;

type WorkSection = (typeof work)[number]['section'];
interface OverviewData {
  overview?: WarehouseOverviewDto;
  counts: Partial<Record<WorkSection, number>>;
  failed: boolean;
}

async function ordersWaiting(phase: 'pick' | 'pack' | 'ship', signal: AbortSignal) {
  const result = await apiRequest<{ orders: unknown[] } | unknown[]>(`/warehouse/orders/ready-to-${phase}`, { auth: true, signal });
  const rows = Array.isArray(result) ? result : result?.orders;
  return queueCount(rows);
}

function queueCount(rows: unknown[] | undefined) {
  if (!Array.isArray(rows)) throw new Error('Warehouse queue missing');
  return rows.length;
}

function formatVolume(value: number) {
  return `${digits.format(value)} سانتی‌متر مکعب`;
}

export function WarehouseOverviewTab({ onNavigateTab }: { onNavigateTab: (section: WarehouseSection) => void }) {
  const [data, setData] = useState<OverviewData>();
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const request = new AbortController();
    const { signal } = request;
    setData(undefined);
    void Promise.allSettled([
      wmsApi.getOverview(signal),
      wmsApi.getQCQueue(signal).then(result => queueCount(result?.queue)),
      wmsApi.getPutawayQueue(signal).then(result => queueCount(result?.queue)),
      ordersWaiting('pick', signal),
      ordersWaiting('pack', signal),
      ordersWaiting('ship', signal),
    ]).then(([summary, qc, putaway, picking, packing, shipping]) => {
      if (signal.aborted) return;
      const count = (read: PromiseSettledResult<number>) => read.status === 'fulfilled' ? read.value : undefined;
      const overview = summary.status === 'fulfilled' ? summary.value : undefined;
      setData({ overview, counts: {
        receive: overview?.inboundPending,
        qc: count(qc), putaway: count(putaway), picking: count(picking), packing: count(packing), shipping: count(shipping),
      }, failed: [summary, qc, putaway, picking, packing, shipping].some(read => read.status === 'rejected') });
    });
    return () => request.abort();
  }, [attempt]);

  const overview = data?.overview;
  const capacityPercent = overview ? Math.max(0, Math.min(100, overview.capacityPercent)) : 0;
  return (
    <div className="space-y-4 pb-6">
      <section aria-label="شروع عملیات انبار" className="grid gap-3 sm:grid-cols-2">
        {([
          { title: 'ورود کالا به انبار', steps: receivingStages, action: 'دریافت محموله', section: 'receive', Icon: Warehouse },
          { title: 'آماده‌سازی سفارش', steps: shippingStages, action: 'جمع‌آوری اقلام سفارش', section: 'picking', Icon: Send },
        ] as const).map(({ title, steps, action, section, Icon }) => (
          <div key={section} className="flex min-w-0 flex-col rounded-2xl border border-slate-200 bg-white p-4 sm:p-5">
            <div className="flex items-center gap-2.5">
              <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-brand-soft text-brand-deep"><Icon aria-hidden="true" className="size-5" /></span>
              <h2 className="text-base font-extrabold text-slate-900">{title}</h2>
            </div>
            <ol className="mt-2 flex flex-wrap gap-x-2 gap-y-1 text-xs leading-6 text-slate-700">
              {steps.map((step, index) => (
                <li key={step.section} className="inline-flex items-center gap-1">
                  {index > 0 && <ChevronLeft aria-hidden="true" className="size-3.5 text-slate-500" />}{step.label}
                </li>
              ))}
            </ol>
            <a href={warehouseSectionHref(section)} onClick={event => followWarehouseLink(event, () => onNavigateTab(section))}
              className="mt-3 inline-flex min-h-11 items-center justify-between gap-2 rounded-xl bg-brand-soft px-3 text-sm font-bold text-brand-deep hover:bg-brand-border focus-visible:outline-2 focus-visible:outline-brand">
              {action}<ChevronLeft aria-hidden="true" className="size-4 shrink-0" />
            </a>
          </div>
        ))}
      </section>

      <PanelWorkQueue items={work.map(({ section, ...item }) => ({
        ...item, count: data?.counts[section], href: warehouseSectionHref(section), open: () => onNavigateTab(section),
      }))} loading={!data} error={data?.failed ? 'دریافت بخشی از اطلاعات انبار انجام نشد.' : undefined}
        retry={() => setAttempt(value => value + 1)} />

      <details className="group rounded-2xl border border-slate-200 bg-white px-4 py-1 sm:px-5">
        <summary className="flex min-h-11 cursor-pointer list-none items-center gap-2 rounded-lg font-bold text-slate-900 focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden">
          <Boxes aria-hidden="true" className="size-5 shrink-0 text-brand-deep" />
          <span className="flex-1">ظرفیت و گردش موجودی</span>
          <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-open:rotate-180" />
        </summary>
        {overview ? (
          <div className="space-y-3 border-t border-slate-100 pb-3 pt-3">
            {overview.warehouseName && <p className="text-sm font-bold text-slate-900">{overview.warehouseName}</p>}
            {overview.capacityVolumeCm3 > 0 ? (
              <div className="space-y-1">
                <p id="warehouse-capacity-used" className="text-sm font-bold text-slate-800">ظرفیت اشغال‌شده یا رزروشده · {digits.format(overview.capacityPercent)}٪</p>
                <div role="progressbar" aria-labelledby="warehouse-capacity-used" aria-valuenow={capacityPercent} className="h-2 overflow-hidden rounded-full bg-slate-100">
                  <div aria-hidden="true" className="h-full rounded-full bg-brand-deep" style={{ width: `${capacityPercent}%` }} />
                </div>
              </div>
            ) : <p className="text-sm text-slate-600">ظرفیت تأییدشده‌ای برای قفسه‌ها ثبت نشده است.</p>}
            <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              {[
                ['ظرفیت تأییدشده', formatVolume(overview.capacityVolumeCm3)],
                ['حجم اشغال‌شده', formatVolume(overview.occupiedVolumeCm3)],
                ['حجم رزروشده', formatVolume(overview.reservedVolumeCm3)],
                ['جابه‌جایی ثبت‌شده', digits.format(overview.verifiedMovements)],
              ].map(([label, value]) => (
                <div key={label} className="min-w-0 rounded-lg bg-slate-50 px-3 py-2">
                  <dt className="text-xs leading-6 text-slate-600">{label}</dt>
                  <dd className="break-words text-sm font-bold tabular-nums text-slate-900">{value}</dd>
                </div>
              ))}
            </dl>
            <a href={warehouseSectionHref('inventory')} onClick={event => followWarehouseLink(event, () => onNavigateTab('inventory'))}
              className="inline-flex min-h-11 items-center gap-2 rounded-lg px-2 text-sm font-bold text-brand-deep hover:bg-brand-soft focus-visible:outline-2 focus-visible:outline-brand">
              مشاهدهٔ موجودی قفسه‌ها<ChevronLeft aria-hidden="true" className="size-4" />
            </a>
          </div>
        ) : <p className="pb-3 text-sm text-slate-600">{data ? 'گزارش ظرفیت در دسترس نیست.' : 'در حال دریافت گزارش ظرفیت…'}</p>}
      </details>
    </div>
  );
}
