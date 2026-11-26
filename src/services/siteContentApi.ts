import { apiRequest } from './apiClient';

export interface TrustFeature {
  icon: string;
  title: string;
  description: string;
  color: string;
}

// Live /api/v1/site-content trust_features (verified 2026-11-03) —
// used as the fallback when the API is unreachable; staging
// site-content already holds these same 5 values.
export const FALLBACK_TRUST_FEATURES: TrustFeature[] = [
  { icon: 'CreditCard', title: 'پرداخت پس از ثبت سفارش', description: 'درگاه آنلاین به‌زودی فعال می‌شود؛ پس از ثبت سفارش، نحوه پرداخت به شما اعلام می‌شود', color: 'violet' },
  { icon: 'Truck', title: 'ارسال به سراسر کشور', description: 'از انبار مرکزی یا مستقیم از تأمین‌کننده، با بسته‌بندی ایمن', color: 'violet' },
  { icon: 'ShieldCheck', title: 'کنترل کیفیت پیش از ارسال', description: 'کالاها پیش از ارسال بازرسی و کنترل کیفیت می‌شوند', color: 'emerald' },
  { icon: 'RotateCcw', title: 'مهلت تست و مرجوعی', description: 'شرایط مرجوعی را در راهنمای خرید ببینید.', color: 'amber' },
];

export const siteContentApi = {
  getTrustFeatures: async (): Promise<TrustFeature[]> => {
    try {
      const data = await apiRequest<{ content?: Record<string, unknown> }>('/site-content');
      const raw = data?.content?.trust_features;
      const list = typeof raw === 'string' ? JSON.parse(raw) : raw;
      if (Array.isArray(list) && list.length > 0) return list as TrustFeature[];
    } catch {
      // fall through to the live fallback below
    }
    return FALLBACK_TRUST_FEATURES;
  },
};
