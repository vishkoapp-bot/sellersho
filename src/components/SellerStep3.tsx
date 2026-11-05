import React from 'react';
import { Store as StoreType } from '../types';
import { Sparkles } from 'lucide-react';

interface SellerStep3Props {
  createdStore: StoreType;
}

export const SellerStep3: React.FC<SellerStep3Props> = ({ createdStore }) => {
  return (
    <div className="text-center py-4 space-y-3">
      <div className="w-16 h-16 rounded-full bg-emerald-100 text-emerald-600 flex items-center justify-center mx-auto shadow-md">
        <Sparkles className="w-8 h-8"  strokeWidth={1.5}/>
      </div>
      <h4 className="text-lg font-extrabold text-slate-900">
        فروشگاه {createdStore.name} با موفقیت ساخته شد!
      </h4>
      <p className="text-xs text-slate-500 max-w-sm mx-auto leading-relaxed">
        ویترین شما فعال شد. هم‌اکنون می‌توانید وارد ویترین اختصاصی خود شوید یا لینک زیر را برای خریداران ارسال کنید:
      </p>
      <div className="bg-slate-100 p-3 rounded-2xl font-mono text-xs text-blue-700 select-all" dir="ltr">
        https://sellershow.ir/store/{createdStore.slug}
      </div>
    </div>
  );
};
