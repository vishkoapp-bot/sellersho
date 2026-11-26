import React from 'react';

interface SellerStep1Props {
  formData: {
    storeName: string;
    slug: string;
    ownerName: string;
    phone: string;
  };
  setFormData: React.Dispatch<React.SetStateAction<any>>;
}

export const SellerStep1: React.FC<SellerStep1Props> = ({ formData, setFormData }) => {
  return (
    <div className="space-y-4">
      <div>
        <label htmlFor="onboarding-store-name" className="block text-xs font-bold text-slate-700 mb-1.5">
          نام فروشگاه یا برند شما: *
        </label>
        <input id="onboarding-store-name"
          type="text"
          placeholder="مثلاً: آریا استور، تک‌شاپ..."
          value={formData.storeName}
          onChange={(e) => setFormData((prev: any) => ({ ...prev, storeName: e.target.value }))}
          className="min-h-12 w-full rounded-xl border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] px-3 text-base focus:bg-white focus-visible:outline-2 focus-visible:outline-[var(--shop-focus,#98601c)]"
        />
      </div>

      <div>
        <label htmlFor="onboarding-store-slug" className="block text-xs font-bold text-slate-700 mb-1.5">
          آدرس اختصاصی ویترین (Slug انگلیسی): *
        </label>
        <div className="flex min-w-0 items-center rounded-xl border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] px-3 focus-within:bg-white focus-within:outline-2 focus-within:outline-[var(--shop-focus,#98601c)]">
          <span className="shrink-0 font-mono text-xs text-[var(--shop-muted,#4f6359)]" dir="ltr">sellershow.ir/store/</span>
          <input id="onboarding-store-slug"
            type="text"
            placeholder="my-store"
            value={formData.slug}
            onChange={(e) =>
              setFormData((prev: any) => ({
                ...prev,
                slug: e.target.value.replace(/[^a-zA-Z0-9-]/g, ''),
              }))
            }
            className="min-h-12 min-w-0 flex-1 bg-transparent px-2 font-mono text-left text-base outline-hidden"
            dir="ltr"
          />
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label htmlFor="onboarding-owner-name" className="block text-xs font-bold text-slate-700 mb-1.5">نام مدیر فروشگاه: *</label>
          <input id="onboarding-owner-name"
            type="text"
            placeholder="نام و نام خانوادگی"
            value={formData.ownerName}
            onChange={(e) => setFormData((prev: any) => ({ ...prev, ownerName: e.target.value }))}
            className="min-h-12 w-full rounded-xl border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] px-3 text-base focus:bg-white focus-visible:outline-2 focus-visible:outline-[var(--shop-focus,#98601c)]"
          />
        </div>
        <div>
          <label htmlFor="onboarding-owner-phone" className="block text-xs font-bold text-slate-700 mb-1.5">شماره همراه (تسویه حساب):</label>
          <input id="onboarding-owner-phone"
            type="tel"
            placeholder="۰۹۱۲۳۴۵۶۷۸۹"
            value={formData.phone}
            onChange={(e) => setFormData((prev: any) => ({ ...prev, phone: e.target.value }))}
            className="min-h-12 w-full rounded-xl border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] px-3 text-base focus:bg-white focus-visible:outline-2 focus-visible:outline-[var(--shop-focus,#98601c)]"
            dir="ltr"
          />
        </div>
      </div>
    </div>
  );
};
