import React from 'react';
import { useStore } from '../../context/StoreContext';
import { Phone, Instagram, MapPin } from 'lucide-react';

// ISO-A (user-ordered storefront separation): this page renders ONLY
// contact data this store actually configured. The previous version
// showed the PLATFORM phone 021-9100 8080 (live bundle footer.phone,
// index-MERGED.js offset 354151; staging /site-content footer.phone)
// as if it were every store's phone, invented an email address
// support@{slug}.ir (zero hits in any live bundle asset), defaulted
// every store's city to Tehran, and submitted a "message to the seller"
// form that performed no API call while toasting that the message had
// been sent and registered as a support ticket. All four inventions
// are removed. A row renders only when its value exists; with nothing
// configured the page says so honestly instead of inventing contact
// details. (The template storefront path never renders this tab at
// all; this file serves only the legacy builder layout.)
export const SellerStorefrontContact: React.FC = () => {
  const { currentStore } = useStore();
  const cfg = (currentStore.templateConfig || {}) as Record<string, unknown>;
  const custom = (cfg.custom || {}) as Record<string, unknown>;
  const phone = (cfg.contactPhone as string | undefined) || (custom.contactPhone as string | undefined) || '';
  const instagram = (cfg.contactInstagram as string | undefined) || (custom.contactInstagram as string | undefined) || '';
  const instagramHandle = instagram.replace(/^https?:\/\/(www\.)?instagram\.com\//i, '').replace(/^@/, '').replace(/\/$/, '');
  const city = (currentStore.city || '').trim();
  const hasAny = phone !== '' || instagram !== '' || city !== '';

  return (
    <div className="px-3.5 py-4 space-y-3.5">
      <div className="bg-white rounded-2xl p-4 border border-slate-200/90 shadow-2xs space-y-3">
        <h2 className="text-sm font-bold text-slate-800">
          راه‌های ارتباط با {currentStore.name}
        </h2>
        {!hasAny ? (
          <p className="text-xs text-slate-500 leading-relaxed">
            برای این فروشگاه هنوز راه ارتباطی ثبت نشده است.
          </p>
        ) : (
          <div className="space-y-2.5 text-xs text-slate-600">
            {phone !== '' && (
              <div className="flex items-center gap-2.5 p-2 bg-slate-50 rounded-xl">
                <Phone className="w-4 h-4 text-blue-600 shrink-0" strokeWidth={1.5} />
                <div>
                  <span className="text-[10px] text-slate-400 block">تلفن پشتیبانی فروشگاه:</span>
                  <span className="font-bold text-slate-800 font-mono" dir="ltr">{phone}</span>
                </div>
              </div>
            )}
            {instagram !== '' && (
              <div className="flex items-center gap-2.5 p-2 bg-slate-50 rounded-xl">
                <Instagram className="w-4 h-4 text-indigo-600 shrink-0" strokeWidth={1.5} />
                <div>
                  <span className="text-[10px] text-slate-400 block">اینستاگرام فروشگاه:</span>
                  <span className="font-mono text-slate-800" dir="ltr">@{instagramHandle}</span>
                </div>
              </div>
            )}
            {city !== '' && (
              <div className="flex items-center gap-2.5 p-2 bg-slate-50 rounded-xl">
                <MapPin className="w-4 h-4 text-emerald-600 shrink-0" strokeWidth={1.5} />
                <div>
                  <span className="text-[10px] text-slate-400 block">شهر فروشگاه:</span>
                  <span className="text-slate-800">{city}</span>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
};
