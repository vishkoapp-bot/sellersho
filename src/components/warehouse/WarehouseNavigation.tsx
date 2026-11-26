import {
  Boxes, Layers, LayoutDashboard, Package, PackageCheck, RotateCcw,
  ScanLine, Send, ShieldCheck, Truck, Undo2, Warehouse,
} from 'lucide-react';
import type { MouseEvent } from 'react';
import type { PanelNavigationGroup } from '../common/PanelNavigation';
import { pathForView } from '../../utils/routing';

const groups = [
  { id: 'home', label: 'پیشخوان', icon: LayoutDashboard, items: [
    { id: 'overview', label: 'پیشخوان انبار', icon: LayoutDashboard },
  ] },
  { id: 'intake', label: 'ورود کالا و موجودی', icon: Warehouse, items: [
    { id: 'receive', label: 'دریافت با کد کارتن', icon: PackageCheck },
    { id: 'inbound', label: 'فهرست محموله‌های ورودی', icon: Truck },
    { id: 'qc', label: 'کنترل کیفیت', icon: ShieldCheck },
    { id: 'putaway', label: 'جانمایی و ثبت موجودی', icon: Layers },
    { id: 'inventory', label: 'موجودی قفسه‌ها', icon: Boxes },
  ] },
  { id: 'fulfillment', label: 'آماده‌سازی و ارسال سفارش', icon: Send, items: [
    { id: 'picking', label: 'جمع‌آوری اقلام', icon: ScanLine },
    { id: 'packing', label: 'بسته‌بندی', icon: Package },
    { id: 'shipping', label: 'ارسال سفارش و مانیفست', icon: Send },
  ] },
  { id: 'returns', label: 'مرجوعی‌ها', icon: RotateCcw, items: [
    { id: 'customer-returns', label: 'دریافت مرجوعی مشتری', icon: PackageCheck },
    { id: 'returns', label: 'رسیدگی به مرجوعی تأمین‌کننده', icon: RotateCcw },
    { id: 'return-backs', label: 'پیگیری برگشت‌ها', icon: Undo2 },
  ] },
] as const;

export type WarehouseSection = (typeof groups)[number]['items'][number]['id'];
export const warehouseNavigation: readonly PanelNavigationGroup<WarehouseSection>[] = groups;
export const warehouseSections = warehouseNavigation.flatMap(group => group.items);

export function warehouseSectionHref(section: WarehouseSection) {
  const base = pathForView('warehouse_panel');
  return section === 'overview' ? base : `${base}?section=${section}`;
}

export function warehouseSectionFromSearch(search: string) {
  const section = new URLSearchParams(search).get('section');
  return warehouseSections.find(item => item.id === section)?.id ?? 'overview';
}

export const receivingStages = [
  { section: 'receive', label: 'دریافت' },
  { section: 'qc', label: 'کنترل کیفیت' },
  { section: 'putaway', label: 'جانمایی' },
] as const;

export const shippingStages = [
  { section: 'picking', label: 'جمع‌آوری' },
  { section: 'packing', label: 'بسته‌بندی' },
  { section: 'shipping', label: 'ارسال' },
] as const;

const receiptModes = [
  { section: 'receive', label: 'با کد کارتن' },
  { section: 'inbound', label: 'از فهرست محموله‌ها' },
] as const;

const digits = new Intl.NumberFormat('fa-IR');

export function followWarehouseLink(event: MouseEvent<HTMLAnchorElement>, action: () => void) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  action();
}

export function WarehouseWorkflow({ section, navigate }: {
  section: WarehouseSection;
  navigate: (section: WarehouseSection) => void;
}) {
  const stageSection = section === 'inbound' ? 'receive' : section;
  const intake = receivingStages.some(step => step.section === stageSection);
  const outbound = shippingStages.some(step => step.section === stageSection);
  if (!intake && !outbound) return null;
  const stages = intake ? receivingStages : shippingStages;
  return (
    <div className="space-y-2">
      <nav aria-label={intake ? 'مراحل ورود کالا' : 'مراحل آماده‌سازی و ارسال سفارش'}
        className="rounded-2xl border border-slate-200 bg-white p-2 sm:p-2.5">
        <ol className="grid grid-cols-3 gap-1">
          {stages.map((step, index) => {
            const current = step.section === stageSection;
            return (
              <li key={step.section} className="min-w-0">
                <a href={warehouseSectionHref(step.section)} aria-current={current ? 'step' : undefined}
                  onClick={event => followWarehouseLink(event, () => { if (!current) navigate(step.section); })}
                  className={`flex min-h-11 items-center justify-center gap-1 rounded-xl px-1.5 py-2 text-center text-xs font-bold leading-5 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand sm:gap-2 sm:text-sm ${current ? 'bg-brand-deep text-white' : 'text-brand-deep hover:bg-brand-soft'}`}>
                  <span aria-hidden="true" className={`grid size-5 shrink-0 place-items-center rounded-full text-[11px] ${current ? 'bg-white/15' : 'bg-brand-soft'}`}>{digits.format(index + 1)}</span>
                  {step.label}
                </a>
              </li>
            );
          })}
        </ol>
      </nav>
      {stageSection === 'receive' && (
        <nav aria-label="روش پیدا کردن محموله" className="flex flex-wrap items-center gap-1.5">
          {receiptModes.map(mode => (
            <a key={mode.section} href={warehouseSectionHref(mode.section)} aria-current={mode.section === section ? 'page' : undefined}
              onClick={event => followWarehouseLink(event, () => { if (mode.section !== section) navigate(mode.section); })}
              className={`inline-flex min-h-11 items-center rounded-xl border px-3 text-xs font-bold focus-visible:outline-2 focus-visible:outline-brand sm:text-sm ${mode.section === section ? 'border-brand-border bg-brand-soft text-brand-deep' : 'border-slate-200 bg-white text-slate-700 hover:text-brand-deep'}`}>
              {mode.label}
            </a>
          ))}
        </nav>
      )}
    </div>
  );
}
