import React, { useEffect, useState } from 'react';
import { apiRequest } from '../../services/apiClient';

// Team 08 live parity (D46): /store/<slug>/unsubscribe route (live regex
// /^\/store\/([^/]+)\/unsubscribe\/?$/). The store-scoped unsubscribe
// endpoint itself is NOT confirmed in the live evidence: the backend in
// this tree only has POST /newsletter/unsubscribe (token, or
// mobile+storeId). When a token query param is present it is posted there;
// otherwise the page states what it can do and does not fake a success.
// Supervisor note: confirm the store unsubscribe endpoint with Team 01/05
// before wiring this page to anything else.
export const StoreUnsubscribeView: React.FC<{
  slug: string;
  onBack: () => void;
}> = ({ slug, onBack }) => {
  const [status, setStatus] = useState<'idle' | 'working' | 'done' | 'error'>('idle');
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const token = new URLSearchParams(window.location.search).get('token');
    if (!token) return;
    setStatus('working');
    apiRequest<{ success: boolean }>('/newsletter/unsubscribe', {
      method: 'POST',
      body: JSON.stringify({ token }),
    })
      .then(() => setStatus('done'))
      .catch(() => setStatus('error'));
  }, []);
  return (
    <div className="flex-1 flex flex-col items-center justify-center p-6 text-center bg-slate-50 min-h-[480px] space-y-3.5">
      <h2 className="text-sm font-bold text-slate-900">لغو عضویت در خبرنامه فروشگاه «{slug}»</h2>
      {status === 'working' && <p className="text-xs text-slate-500">در حال بررسی درخواست لغو عضویت…</p>}
      {status === 'done' && (
        <p className="text-xs text-emerald-700 leading-6">عضویت شما در خبرنامه این فروشگاه لغو شد.</p>
      )}
      {status === 'error' && (
        <p className="text-xs text-rose-600 leading-6">
          لغو عضویت انجام نشد. لطفاً از لینک کامل پیام دریافتی استفاده کنید یا با پشتیبانی تماس بگیرید.
        </p>
      )}
      {status === 'idle' && (
        <p className="text-xs text-slate-500 leading-6 max-w-[280px]">
          برای لغو عضویت، از لینک لغو عضویت داخل پیام یا ایمیل دریافتی از این فروشگاه استفاده کنید.
        </p>
      )}
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
