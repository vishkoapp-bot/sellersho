import {
  BarChart3, Crown, DollarSign, Headphones, LayoutDashboard, LayoutTemplate, Link2, Mail,
  Megaphone, Package, Palette, Percent, RotateCcw, Search, ShoppingBag, Store, Tag, Users, Wallet,
} from 'lucide-react';
import type { PanelNavigationGroup } from '../common/PanelNavigation';
import { pathForView, type SellerDashboardTab } from '../../utils/routing';

export type SellerWorkFilter = 'attention' | 'follow-up';
export const sellerNavigation: readonly PanelNavigationGroup<SellerDashboardTab>[] = [
  { id: 'home', label: 'پیشخوان', icon: LayoutDashboard, items: [
    { id: 'overview', label: 'پیشخوان', icon: LayoutDashboard },
  ] },
  { id: 'sales', label: 'سفارش و مرجوعی', icon: ShoppingBag, items: [
    { id: 'orders', label: 'سفارش‌ها', icon: ShoppingBag },
    { id: 'returns', label: 'مرجوعی‌ها', icon: RotateCcw },
  ] },
  { id: 'catalogue', label: 'کالا و سود', icon: Package, items: [
    { id: 'catalog', label: 'انتخاب کالا', icon: Package },
    { id: 'pricing', label: 'تنظیم سود', icon: DollarSign },
  ] },
  { id: 'store', label: 'فروشگاه من', icon: Store, items: [
    { id: 'store-setup', label: 'مشخصات و آدرس فروشگاه', icon: Store },
    { id: 'template', label: 'قالب و ظاهر فروشگاه', icon: LayoutTemplate },
    { id: 'builder', label: 'صفحه‌ساز', icon: Palette },
    { id: 'links', label: 'لینک و کد QR', icon: Link2 },
  ] },
  { id: 'money', label: 'مالی و اشتراک', icon: Wallet, items: [
    { id: 'wallet', label: 'کیف پول و تسویه', icon: Wallet },
    { id: 'subscription', label: 'اشتراک فروشگاه', icon: Crown },
  ] },
  { id: 'reports', label: 'گزارش‌های فروش', icon: BarChart3, items: [
    { id: 'analytics', label: 'تحلیل فروش', icon: BarChart3 },
    { id: 'search-insight', label: 'جست‌وجوی مشتریان', icon: Search },
  ] },
  { id: 'growth', label: 'بازاریابی و رشد', icon: Megaphone, items: [
    { id: 'discount-campaigns', label: 'کمپین‌های تخفیف', icon: Percent },
    { id: 'discount-codes', label: 'کدهای تخفیف', icon: Tag },
    { id: 'ads', label: 'تبلیغات', icon: Megaphone },
    { id: 'subscribers', label: 'مشترکان فروشگاه', icon: Mail },
    { id: 'referral', label: 'دعوت و پاداش', icon: Users },
  ] },
  { id: 'help', label: 'پشتیبانی', icon: Headphones, items: [
    { id: 'support', label: 'پشتیبانی', icon: Headphones },
  ] },
];

export function sellerSectionHref(tab: SellerDashboardTab, work?: SellerWorkFilter): string {
  const path = pathForView('seller_dashboard', { sellerTab: tab });
  if ((tab === 'orders' && work === 'follow-up') || (tab === 'catalog' && work === 'attention')) {
    return `${path}&work=${work}`;
  }
  return path;
}
