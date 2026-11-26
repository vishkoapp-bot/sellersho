import { SupplierSectionHeading } from './SupplierSectionHeading';
import React from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useStore } from '../../context/StoreContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import { Megaphone } from 'lucide-react';

// Live ads tab (dn): click advertising is NOT active yet — a coming-soon
// page. Past campaigns (if any) are listed read-only, all inactive, with
// no create form and no activation toggle.
export const SupplierAdsCampaignTab: React.FC = () => {
  const { formatPrice, toPersianDigits } = useStore();
  const campaigns = useSupplierResource(supplierApi.getCampaigns);
  const list = campaigns.data ?? [];
  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="تبلیغات" description="تبلیغات کلیکی هنوز فعال نیست. سابقهٔ کمپین‌ها از همین بخش در دسترس است." icon={<Megaphone />} />
      <SupplierResourceState loading={campaigns.loading} error={campaigns.error} retry={campaigns.reload} />
      {!campaigns.loading && !campaigns.error && list.length === 0 && <SupplierEmptyState icon={<Megaphone />} title="هنوز کمپینی ثبت نشده است." />}
      {!campaigns.loading && !campaigns.error && list.map((item) => (
        <section key={item.id} className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="break-words text-base font-extrabold text-[#261b41]">{item.campaign_name}</h3>
            <span className="rounded-lg bg-slate-100 px-2 py-1 text-xs font-bold text-slate-700">غیرفعال</span>
          </div>
          <dl className="grid grid-cols-2 gap-3 rounded-xl bg-slate-50 p-3 text-sm sm:grid-cols-4">
            <div><dt className="text-slate-600">بودجه</dt><dd className="font-bold text-slate-900">{formatPrice(item.budget)} تومان</dd></div>
            <div><dt className="text-slate-600">هزینه</dt><dd className="font-bold text-slate-900">{formatPrice(item.spent_budget)} تومان</dd></div>
            <div><dt className="text-slate-600">تعداد نمایش</dt><dd className="font-bold text-slate-900">{toPersianDigits(item.impressions)}</dd></div>
            <div><dt className="text-slate-600">تعداد کلیک</dt><dd className="font-bold text-slate-900">{toPersianDigits(item.clicks)}</dd></div>
          </dl>
        </section>
      ))}
    </div>
  );
};
