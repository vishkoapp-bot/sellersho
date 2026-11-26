import React from 'react';
import { Store as StoreType } from '../types';
import { Sparkles } from 'lucide-react';

interface SellerStep3Props {
  createdStore: StoreType;
}

export const SellerStep3: React.FC<SellerStep3Props> = ({ createdStore }) => {
  return (
    <div className="text-center py-4 space-y-3">
      <div className="w-16 h-16 rounded-full bg-[var(--shop-soft,#edf2e9)] text-[var(--shop-forest,#164638)] flex items-center justify-center mx-auto shadow-md">
        <Sparkles className="w-8 h-8"  strokeWidth={1.5}/>
      </div>
      <h4 className="text-lg font-extrabold text-slate-900">
        فروشگاه {createdStore.name} با موفقیت ساخته شد!
      </h4>
      <p className="text-xs text-slate-500 max-w-sm mx-auto leading-relaxed">
        ویترین شما فعال شد. هم‌اکنون می‌توانید وارد ویترین اختصاصی خود شوید یا لینک زیر را برای خریداران ارسال کنید:
      </p>
      <div className="select-all break-all rounded-xl bg-[var(--shop-bg,#f8f6ef)] p-3 font-mono text-sm text-[var(--shop-forest,#164638)]" dir="ltr">
        https://sellershow.ir/store/{createdStore.slug}
      </div>
    </div>
  );
};
