import {
  Activity, ArrowDownToLine, BadgePercent, Building2, CalendarClock, ClipboardCheck, Clock,
  Cpu, Crown, DollarSign, FileEdit, FileText, GitFork, Headphones, Home, Image, LayoutDashboard,
  Mail, MessageSquare, Package, Receipt, RefreshCw, RotateCcw, ShieldAlert, ShieldCheck, Star,
  Store, Truck, Users, Warehouse,
} from 'lucide-react';
import type { AdminRole } from '../../context/storeContextTypes';
import type { PanelNavigationGroup } from '../common/PanelNavigation';
import { pathForView } from '../../utils/routing';

type RoleItem<Section extends string> = PanelNavigationGroup<Section>['items'][number] & {
  roles: readonly AdminRole[];
  globalWarehouse?: boolean;
};
type RoleGroup<Section extends string> = Omit<PanelNavigationGroup<Section>, 'items'> & {
  items: readonly RoleItem<Section>[];
};

const administrators = ['super_admin', 'admin'] as const;
const logistics = [...administrators, 'logistics_specialist'] as const;
const finance = [...administrators, 'finance_specialist'] as const;
const support = [...administrators, 'support_specialist'] as const;
const supervisors = ['super_admin'] as const;
const helpdesk = [...support, 'finance_specialist'] as const;
const caseReviewers = [...helpdesk, 'logistics_specialist'] as const;

const groups = [
  { id: 'home', label: 'پیشخوان', icon: LayoutDashboard, items: [
    { id: 'overview', label: 'پیشخوان مدیریت', icon: LayoutDashboard, roles: administrators },
  ] },
  { id: 'commerce', label: 'کالا، تأمین و سفارش', icon: Package, items: [
    { id: 'catalog', label: 'بررسی کالاها', icon: Package, roles: administrators },
    { id: 'suppliers', label: 'تأمین‌کنندگان', icon: Building2, roles: administrators },
    { id: 'orders', label: 'رهگیری سفارش‌ها', icon: Truck, roles: administrators },
    { id: 'warehouses', label: 'انبارها و تعرفه‌ها', icon: Warehouse, roles: administrators, globalWarehouse: true },
    { id: 'historical-reconciliation', label: 'تطبیق سوابق انبار', icon: ClipboardCheck, roles: logistics, globalWarehouse: true },
  ] },
  { id: 'storefronts', label: 'فروشگاه‌ها و محتوای سایت', icon: Store, items: [
    { id: 'stores', label: 'ویترین‌های دراپ‌شیپر', icon: Store, roles: administrators },
    { id: 'stores-list', label: 'لیست فروشگاه‌ها', icon: Store, roles: supervisors },
    { id: 'homepage', label: 'صفحه اصلی', icon: Home, roles: administrators },
    { id: 'banners', label: 'بنرها', icon: Image, roles: administrators },
    { id: 'site-content', label: 'محتوای سایت و صفحات', icon: FileText, roles: administrators },
  ] },
  { id: 'support', label: 'پشتیبانی و مرجوعی', icon: Headphones, items: [
    { id: 'support-tickets', label: 'تیکت‌های پشتیبانی', icon: Headphones, roles: helpdesk },
    { id: 'support-returns', label: 'مرجوعی‌ها', icon: RotateCcw, roles: administrators },
    { id: 'support-disputes', label: 'داوری و مرجوعی', icon: ShieldAlert, roles: caseReviewers },
    { id: 'reviews', label: 'نظرات مشتریان', icon: Star, roles: administrators },
  ] },
  { id: 'finance', label: 'مالی و تسویه', icon: Receipt, items: [
    { id: 'finance-overview', label: 'تراز و دفتر کل', icon: BadgePercent, roles: finance },
    { id: 'finance-pending-payments', label: 'تأیید دستی پرداخت', icon: Clock, roles: finance },
    { id: 'finance-reconciliation', label: 'تطبیق شاپرک', icon: Receipt, roles: finance },
    { id: 'finance-callback-retries', label: 'پرداخت‌های ناموفق', icon: RefreshCw, roles: administrators },
    { id: 'finance-payouts', label: 'تسویه شبا', icon: ArrowDownToLine, roles: finance },
    { id: 'finance-adjustments', label: 'تعدیل و برگشتی', icon: FileEdit, roles: finance },
    { id: 'commission', label: 'سهم پلتفرم', icon: DollarSign, roles: administrators },
    { id: 'settlement-settings', label: 'تنظیمات تسویه ماهانه', icon: CalendarClock, roles: supervisors },
    { id: 'storage-fee-settings', label: 'هزینه انبارداری', icon: Warehouse, roles: supervisors },
    { id: 'shipping-settings', label: 'تنظیمات حمل‌ونقل', icon: Truck, roles: supervisors },
  ] },
  { id: 'marketing', label: 'بازاریابی و ارتباطات', icon: Crown, items: [
    { id: 'campaigns', label: 'کمپین‌ها و بودجه', icon: Crown, roles: administrators },
    { id: 'ad-network', label: 'تبلیغات و ضدتقلب', icon: Activity, roles: support },
    { id: 'referral-network', label: 'دعوت همکاران', icon: GitFork, roles: administrators },
    { id: 'newsletter', label: 'مشترکان خبرنامه', icon: Mail, roles: administrators },
    { id: 'sms', label: 'پیامک و اعتبار', icon: MessageSquare, roles: administrators },
  ] },
  { id: 'system', label: 'عملیات و دسترسی‌ها', icon: ShieldCheck, items: [
    { id: 'staff', label: 'کارکنان انبار', icon: Users, roles: supervisors },
    { id: 'operations', label: 'پایش عملیات خودکار', icon: Activity, roles: logistics, globalWarehouse: true },
    { id: 'jobs-queue', label: 'صف کارها', icon: Cpu, roles: administrators },
    { id: 'spncx-backfill', label: 'سریال واحد (SPNCX)', icon: Package, roles: supervisors },
    { id: 'audit-logs', label: 'گزارش حسابرسی', icon: ShieldCheck, roles: administrators },
  ] },
] as const satisfies readonly RoleGroup<string>[];

export type AdminSection = (typeof groups)[number]['items'][number]['id'];

const navigation: readonly RoleGroup<AdminSection>[] = groups;
const allSections = navigation.flatMap(group => group.items);

export const adminDefaultSection: Record<AdminRole, AdminSection> = {
  admin: 'overview',
  super_admin: 'overview',
  finance_specialist: 'finance-overview',
  logistics_specialist: 'warehouses',
  support_specialist: 'support-tickets',
};

const aliases: Readonly<Partial<Record<string, AdminSection>>> = {
  payouts: 'finance-payouts',
  disputes: 'support-disputes',
  reconciliation: 'finance-reconciliation',
  adjustments: 'finance-adjustments',
  tickets: 'support-tickets',
};

export function adminSectionAllowed(item: RoleItem<AdminSection>, roles: readonly AdminRole[], staffScoped: boolean) {
  return !(item.globalWarehouse && staffScoped) && item.roles.some(role => roles.includes(role));
}

export function adminNavigationFor(roles: AdminRole | readonly AdminRole[], staffScoped = false): PanelNavigationGroup<AdminSection>[] {
  const allowed = typeof roles === 'string' ? [roles] : roles;
  return navigation.map(group => ({ ...group, items: group.items.filter(item => adminSectionAllowed(item, allowed, staffScoped)) }))
    .filter(group => group.items.length > 0);
}

export function findAdminSection(value: string | null) {
  if (!value) return undefined;
  return allSections.find(item => item.id === value || item.id === aliases[value]);
}

export function adminSectionHref(section: AdminSection) {
  const base = pathForView('admin_panel');
  return section === 'overview' ? base : `${base}?section=${section}`;
}

export function requestedAdminSection() {
  if (typeof window === 'undefined') return null;
  const { pathname, search } = window.location;
  const pathSection = /^\/admin\/([^/]+)\/?$/.exec(pathname)?.[1];
  return new URLSearchParams(search).get('section') ?? pathSection ?? null;
}
