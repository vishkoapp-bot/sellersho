import type { MouseEvent } from 'react';
import {
  AlertTriangle, BarChart3, Boxes, ChevronDown, CircleHelp, FileText,
  LayoutDashboard, Megaphone, Package, PackagePlus, Receipt, RotateCcw,
  ShoppingBag, TrendingUp, Truck, User, Users, Warehouse,
} from 'lucide-react';

const groups = [
  { id: 'home', label: 'پیشخوان', icon: LayoutDashboard, items: [
    { id: 'overview', label: 'پیشخوان', icon: LayoutDashboard, permissions: ['supplier.manage'] },
  ] },
  { id: 'sales', label: 'سفارش و مرجوعی', icon: ShoppingBag, items: [
    { id: 'orders', label: 'سفارش‌های من', icon: ShoppingBag, permissions: ['orders.manage'] },
    { id: 'customer-returns', label: 'مرجوعی‌های مشتریان', icon: RotateCcw, permissions: ['orders.manage'] },
    { id: 'shipping-methods', label: 'روش‌های ارسال', icon: Truck, permissions: ['orders.manage'] },
  ] },
  { id: 'stock', label: 'کالا و انبار', icon: Package, items: [
    { id: 'inbound', label: 'ثبت کالا و ارسال به انبار', icon: PackagePlus, permissions: ['inventory.manage', 'products.manage'] },
    { id: 'products', label: 'کالاها و تنوع‌ها', icon: Package, permissions: ['products.manage'] },
    { id: 'inventory', label: 'موجودی کالاها', icon: Boxes, permissions: ['inventory.manage'] },
    { id: 'my-warehouses', label: 'انبارهای من', icon: Warehouse, permissions: ['inventory.manage'] },
    { id: 'discrepancies', label: 'مغایرت‌های تحویل انبار', icon: AlertTriangle, permissions: ['inventory.manage'] },
  ] },
  { id: 'money', label: 'مالی', icon: Receipt, items: [
    { id: 'settlement', label: 'تسویه‌حساب', icon: Receipt, permissions: ['finance.manage'] },
  ] },
  { id: 'growth', label: 'رشد فروش', icon: TrendingUp, items: [
    { id: 'analytics', label: 'تحلیل فروش', icon: BarChart3, permissions: ['supplier.manage'] },
    { id: 'demand-insight', label: 'تقاضای بازار', icon: TrendingUp, permissions: ['supplier.manage'] },
    { id: 'ads', label: 'تبلیغات', icon: Megaphone, permissions: ['marketing.manage'] },
  ] },
  { id: 'account', label: 'حساب و پشتیبانی', icon: User, items: [
    { id: 'support', label: 'پشتیبانی', icon: CircleHelp, permissions: [] },
    { id: 'profile', label: 'پروفایل', icon: User, permissions: ['supplier.manage'] },
    { id: 'team', label: 'همکاران من', icon: Users, permissions: ['supplier.manage'] },
    { id: 'contract', label: 'قرارداد من', icon: FileText, permissions: ['supplier.manage'] },
  ] },
] as const;

type SupplierNavItem = (typeof groups)[number]['items'][number];
export type SupplierSection = SupplierNavItem['id'];

export function getSupplierNavigation(permissions: readonly string[]) {
  const isManager = permissions.includes('supplier.manage');
  return groups.map(({ items, ...group }) => ({
    ...group,
    items: (items as readonly SupplierNavItem[]).filter(item => isManager ||
      (item.id === 'support' ? permissions.length > 0 : item.permissions.some(permission => permissions.includes(permission))),
    ),
  })).filter(group => group.items.length);
}

export function SupplierNavigation({ groups, current, href, onNavigate, label }: {
  groups: ReturnType<typeof getSupplierNavigation>;
  current?: string;
  href: (section: SupplierSection) => string;
  onNavigate: (section: SupplierSection) => void;
  label: string;
}) {
  function renderItem({ id, label, icon: Icon }: SupplierNavItem) {
    const active = id === current;
    return (
      <li key={id}>
        <a href={href(id)} data-section={id} aria-current={active ? 'page' : undefined}
          onClick={(event: MouseEvent<HTMLAnchorElement>) => {
            if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
            event.preventDefault();
            onNavigate(id);
          }}
          className={`flex min-h-11 items-center gap-2.5 rounded-xl px-3 py-1.5 text-sm leading-6 outline-offset-2 focus-visible:outline-2 focus-visible:outline-brand ${active ? 'bg-brand-soft font-extrabold text-brand-deep' : 'font-medium text-slate-700 hover:bg-slate-100'}`}>
          <Icon aria-hidden="true" className="size-4 shrink-0" strokeWidth={1.75} />
          <span className="min-w-0 flex-1">{label}</span>
          {active && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-brand-deep" />}
        </a>
      </li>
    );
  }

  return (
    <nav aria-label={label}>
      <ul className="space-y-1.5">
        {groups.map(({ id, label, items, icon: Icon }) => {
          if (items.length === 1) return renderItem(items[0]);
          const isCurrent = items.some(item => item.id === current);
          return (
            <li key={id}>
              <details key={String(isCurrent)} open={isCurrent} className="group rounded-xl">
                <summary className={`flex min-h-11 cursor-pointer list-none items-center gap-2.5 rounded-xl px-3 py-1.5 text-sm font-bold outline-offset-2 hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden ${isCurrent ? 'text-brand-deep' : 'text-slate-800'}`}>
                  <Icon aria-hidden="true" className="size-[18px] shrink-0" strokeWidth={1.75} />
                  <span className="min-w-0 flex-1">{label}</span>
                  <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-500 group-open:rotate-180" />
                </summary>
                <ul className="ms-5 mt-1 space-y-0.5 border-s border-slate-200 ps-2">{items.map(renderItem)}</ul>
              </details>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
