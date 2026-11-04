import React from 'react';
import { Store } from '../../types';

// Team 08 live parity (D45): the store_suspended page. Live shows the
// store logo and ownerPhone.
// TODO(U42): the origin of ownerPhone is unverified (the live public store
// response's 12 keys in D52 do not include it). It is rendered ONLY when
// the API actually provides it; otherwise the phone row is omitted and
// nothing is invented.
export const StoreSuspendedView: React.FC<{
  store: Store;
  onBack: () => void;
}> = ({ store, onBack }) => {
  return (
    <div className="flex-1 flex flex-col items-center justify-center p-6 text-center bg-slate-50 min-h-[480px] space-y-3.5">
      {store.logo ? (
        <img src={store.logo} alt={store.name} className="w-20 h-20 rounded-2xl object-contain bg-white border border-slate-200 p-2" />
      ) : null}
      <div>
        <h2 className="text-sm font-bold text-slate-900">ویترین «{store.name}» در حال حاضر تعلیق شده است</h2>
        <p className="text-xs text-slate-500 mt-1 max-w-[280px] leading-6">
          این فروشگاه به‌طور موقت توسط مدیریت پلتفرم تعلیق شده است. لطفاً بعداً مراجعه فرمایید.
        </p>
      </div>
      {store.ownerPhone ? (
        <p className="text-xs text-slate-600" dir="ltr">{store.ownerPhone}</p>
      ) : null}
      <button
        type="button"
        onClick={onBack}
        className="rounded-xl bg-blue-600 px-4 py-2.5 text-xs font-bold text-white"
      >
        بازگشت به صفحه اصلی
      </button>
    </div>
  );
};
