import {
  AlertTriangle, BarChart3, Boxes, CircleHelp, FileText,
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
