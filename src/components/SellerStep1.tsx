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
        <label className="block text-xs font-bold text-slate-700 mb-1.5">
          نام فروشگاه یا برند شما: *
        </label>
        <input
          type="text"
          placeholder="مثلاً: آریا استور، تک‌شاپ..."
          value={formData.storeName}
          onChange={(e) => setFormData((prev: any) => ({ ...prev, storeName: e.target.value }))}
          className="w-full text-xs md:text-sm p-3 rounded-2xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-blue-500 focus:ring-2 focus:ring-blue-100 outline-hidden"
        />
      </div>

      <div>
        <label className="block text-xs font-bold text-slate-700 mb-1.5">
          آدرس اختصاصی ویترین (Slug انگلیسی): *
        </label>
        <div className="flex items-center bg-slate-50 border border-slate-200 rounded-2xl px-3 focus-within:bg-white focus-within:border-blue-500">
          <span className="text-xs text-slate-400 font-mono">sellershow.ir/store/</span>
          <input
            type="text"
            placeholder="my-store"
            value={formData.slug}
            onChange={(e) =>
              setFormData((prev: any) => ({
                ...prev,
                slug: e.target.value.replace(/[^a-zA-Z0-9-]/g, ''),
              }))
            }
            className="w-full text-xs md:text-sm p-3 bg-transparent outline-hidden font-mono text-left"
            dir="ltr"
          />
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-bold text-slate-700 mb-1.5">نام مدیر فروشگاه: *</label>
          <input
            type="text"
            placeholder="نام و نام خانوادگی"
            value={formData.ownerName}
            onChange={(e) => setFormData((prev: any) => ({ ...prev, ownerName: e.target.value }))}
            className="w-full text-xs md:text-sm p-3 rounded-2xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-blue-500 outline-hidden"
          />
        </div>
        <div>
          <label className="block text-xs font-bold text-slate-700 mb-1.5">شماره همراه (تسویه حساب):</label>
          <input
            type="tel"
            placeholder="۰۹۱۲۳۴۵۶۷۸۹"
            value={formData.phone}
            onChange={(e) => setFormData((prev: any) => ({ ...prev, phone: e.target.value }))}
            className="w-full text-xs md:text-sm p-3 rounded-2xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-blue-500 outline-hidden"
            dir="ltr"
          />
        </div>
      </div>
    </div>
  );
};
