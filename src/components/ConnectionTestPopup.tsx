import React, { useEffect, useState } from 'react';
import { X, CheckCircle2 } from 'lucide-react';

// Connection-test popup (user request, Nov 5 2026): proof that the
// GitHub -> server pipeline is live. Shows on the homepage until the
// visitor closes it; closing hides it for the rest of the session.
const SESSION_KEY = 'sellersho_connection_popup_closed';

export const ConnectionTestPopup: React.FC = () => {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    try {
      if (!sessionStorage.getItem(SESSION_KEY)) setVisible(true);
    } catch {
      setVisible(true);
    }
  }, []);

  const close = () => {
    setVisible(false);
    try {
      sessionStorage.setItem(SESSION_KEY, '1');
    } catch {
      // storage unavailable: popup simply reappears on next visit
    }
  };

  if (!visible) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/60 backdrop-blur-sm">
      <div className="relative w-full max-w-sm rounded-2xl bg-white p-6 text-center shadow-2xl" dir="rtl">
        <button
          onClick={close}
          aria-label="بستن"
          className="absolute top-3 left-3 rounded-full p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600"
        >
          <X className="h-5 w-5" />
        </button>
        <div className="mx-auto mb-3 flex h-14 w-14 items-center justify-center rounded-full" style={{ backgroundColor: '#efeafd' }}>
          <CheckCircle2 className="h-8 w-8" style={{ color: '#5d44f8' }} />
        </div>
        <h3 className="mb-2 text-base font-extrabold text-slate-900">اتصال برقرار شد</h3>
        <p className="mb-1 text-sm leading-6 text-slate-600">
          این پاپ‌آپ، تست زندهٔ اتصال است: این تغییر اول روی گیت‌هاب ثبت شد و بعد روی سرور منتشر شد.
        </p>
        <p className="mb-5 text-xs font-bold tracking-wide text-slate-400" dir="ltr">
          SELLERSHO-LIVE-1105
        </p>
        <button
          onClick={close}
          className="w-full rounded-xl py-2.5 text-sm font-bold text-white transition-opacity hover:opacity-90"
          style={{ backgroundColor: '#5d44f8' }}
        >
          بستن
        </button>
      </div>
    </div>
  );
};
