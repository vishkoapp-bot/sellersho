import { apiRequest } from './apiClient';

export interface SupplierMember {
  id: string;
  supplierId: string;
  userId: string;
  fullName: string;
  username: string;
  mobile: string;
  roleKey: string;
  permissions: string[];
  isActive: boolean;
}

export const SUPPLIER_ROLES = [
  { key: 'products', label: 'مدیریت کالا', description: 'درج کالا، تصاویر، قیمت و شرایط تأمین', permissions: ['products.manage'] },
  { key: 'operations', label: 'عملیات و موجودی', description: 'موجودی، دپو، مغایرت دریافت و ارسال سفارش‌ها', permissions: ['inventory.manage', 'orders.manage'] },
  { key: 'finance', label: 'مالی و تسویه', description: 'صورت‌حساب، گزارش فروش و درخواست تسویه؛ بدون دسترسی به کالا', permissions: ['finance.view', 'finance.manage'] },
  { key: 'orders', label: 'مدیریت سفارش‌ها', description: 'مشاهده سفارش‌ها و ثبت اطلاعات ارسال', permissions: ['orders.manage'] },
  { key: 'marketing', label: 'بازاریابی', description: 'مدیریت کمپین و مشاهده آمار تبلیغات', permissions: ['marketing.manage'] },
  { key: 'manager', label: 'مدیر کامل', description: 'تمام بخش‌ها، از جمله امور مالی و مدیریت همکاران', permissions: ['supplier.manage'] },
];

export const supplierTeamApi = {
  list: async (_supplierId: string): Promise<SupplierMember[]> => apiRequest<SupplierMember[]>('/supplier/team', { method: 'GET', auth: true }),
  create: async (supplierId: string, payload: { fullName: string; username: string; mobile: string; password: string; roleKey: string }): Promise<SupplierMember> => {
    return apiRequest<SupplierMember>('/supplier/team', { method: 'POST', auth: true, body: JSON.stringify(payload) });
  },
  update: async (supplierId: string, id: string, updates: { active?: boolean; roleKey?: string }): Promise<void> => {
    await apiRequest(`/supplier/team/${encodeURIComponent(id)}`, { method: 'PUT', auth: true, body: JSON.stringify(updates) });
  },
};