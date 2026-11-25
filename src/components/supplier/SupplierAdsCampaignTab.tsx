import { SupplierSectionHeading } from './SupplierSectionHeading';
import React from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useStore } from '../../context/StoreContext';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { AlertTriangle, Megaphone } from 'lucide-react';

// Live ads tab (dn): click advertising is NOT active yet — a coming-soon
// page. Past campaigns (if any) are listed read-only, all inactive, with
// no create form and no activation toggle.
export const SupplierAdsCampaignTab: React.FC = () => {
  const { formatPrice, toPersianDigits } = useStore();
  const campaigns = useSupplierResource(supplierApi.getCampaigns);
  const list = campaigns.data || [];
  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="تبلیغ کالاها" description="سوابق کمپین‌ها و وضعیت فعال‌سازی تبلیغات را ببین." icon={<Megaphone />} />
      <div className="flex items-start gap-2 rounded-2xl border border-amber-200 bg-amber-50 px-3.5 py-3">
        <AlertTriangle className="w-4 h-4 text-amber-700 shrink-0 mt-0.5" strokeWidth={1.5} />
        <div className="space-y-1">
          <p className="text-xs font-bold leading-6 text-amber-900">به‌زودی — تبلیغات کلیکی هنوز فعال نیست</p>
          <p className="text-[11px] leading-5 text-amber-800">این بخش هنوز راه‌اندازی نشده است؛ در حال حاضر هیچ تبلیغی نمایش داده نمی‌شود و هیچ مبلغی بابت تبلیغ از بودجه‌ات کم نمی‌شود. وقتی این قابلیت فعال شود، از همین‌جا می‌توانید کمپین بسازید.</p>
        </div>
      </div>
      <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">وقتی تبلیغات کلیکی فعال شود، از همین‌جا می‌توانید برای کالاهایتان کمپین بسازید و بودجه و قیمت کلیک تعیین کنید. تا آن زمان ساختن یا فعال‌کردن کمپین ممکن نیست.</div>
      <SupplierResourceState loading={campaigns.loading} error={campaigns.error} retry={() => { campaigns.reload(); }} />
      {!campaigns.error && list.length === 0 && (
        <div className="rounded-2xl border bg-white p-6 text-center space-y-1.5">
          <p className="text-xs font-bold text-slate-700">هنوز تبلیغی ثبت نشده است.</p>
          <p className="text-[11px] leading-5 text-slate-400">وقتی تبلیغات کلیکی فعال شود، از همین‌جا می‌توانید اولین کمپین‌تان را بسازید.</p>
        </div>
      )}
      {!campaigns.error && list.length > 0 && (
        <p className="text-[11px] leading-5 text-slate-500 bg-slate-50 border border-slate-100 rounded-lg px-2.5 py-2">کمپین‌های قبلی‌ات اینجاست و حذف نشده‌اند؛ اما هیچ‌کدام فعال نیستند و در حال حاضر نمایش یا کلیکی برایشان ثبت نمی‌شود.</p>
      )}
      {!campaigns.error && list.map((item) => (
        <section key={item.id} className="space-y-2 rounded-2xl border bg-white p-3 text-xs">
          <div className="flex justify-between items-center gap-2">
            <strong>{item.campaign_name}</strong>
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-bold text-slate-500">غیرفعال</span>
          </div>
          <p>وضعیت: غیرفعال — تبلیغات کلیکی هنوز فعال نشده است · بودجه: {formatPrice(item.budget)} تومان · خرج‌شده: {formatPrice(item.spent_budget)} تومان</p>
          <p>دیده‌شدن ثبت‌شده: {toPersianDigits(item.impressions)} بار · کلیک ثبت‌شده: {toPersianDigits(item.clicks)} بار</p>
        </section>
      ))}
    </div>
  );
};
