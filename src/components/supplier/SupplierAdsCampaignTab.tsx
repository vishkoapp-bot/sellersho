import { SupplierSectionHeading } from './SupplierSectionHeading';
import React from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useStore } from '../../context/StoreContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import { AlertTriangle, Megaphone } from 'lucide-react';

// Live ads tab (dn): click advertising is NOT active yet — a coming-soon
// page. Past campaigns (if any) are listed read-only, all inactive, with
// no create form and no activation toggle.
export const SupplierAdsCampaignTab: React.FC = () => {
  const { formatPrice, toPersianDigits } = useStore();
  const campaigns = useSupplierResource(supplierApi.getCampaigns);
  const list = campaigns.data ?? [];
  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="تبلیغات" description="وضعیت تبلیغات کلیکی و سابقهٔ کمپین‌هایت را ببین." icon={<Megaphone />} />
      <div className="flex items-start gap-2 rounded-2xl border border-amber-200 bg-amber-50 px-3.5 py-3">
        <AlertTriangle className="w-4 h-4 text-amber-700 shrink-0 mt-0.5" strokeWidth={1.5} />
        <div className="space-y-1">
          <p className="text-sm font-bold leading-6 text-amber-900">تبلیغات کلیکی هنوز فعال نیست</p>
          <p className="text-sm leading-6 text-amber-800">پس از راه‌اندازی، بودجه و قیمت کلیک را همین‌جا تعیین می‌کنی.</p>
        </div>
      </div>
      <SupplierResourceState loading={campaigns.loading} error={campaigns.error} retry={campaigns.reload} />
      {!campaigns.loading && !campaigns.error && list.length === 0 && <SupplierEmptyState icon={<Megaphone />} title="هنوز کمپینی ثبت نشده است." />}
      {!campaigns.loading && !campaigns.error && list.map((item) => (
        <section key={item.id} className="space-y-2 rounded-2xl border bg-white p-3 text-xs">
          <div className="flex justify-between items-center gap-2">
            <strong>{item.campaign_name}</strong>
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-500">غیرفعال</span>
          </div>
          <p>بودجهٔ کمپین {formatPrice(item.budget)} تومان · هزینهٔ ثبت‌شده {formatPrice(item.spent_budget)} تومان</p>
          <p>دیده‌شدن ثبت‌شده: {toPersianDigits(item.impressions)} بار · کلیک ثبت‌شده: {toPersianDigits(item.clicks)} بار</p>
        </section>
      ))}
    </div>
  );
};
