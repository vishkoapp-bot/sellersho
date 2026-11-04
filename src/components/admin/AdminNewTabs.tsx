import React, { useState, useEffect } from 'react';
import { adminApi } from '../../services/adminApi';
import { RotateCcw } from 'lucide-react';

// FIX 17: admin tabs added to reach live's 30-item structure.
// support-returns wires to GET /admin/returns (read-only list).
// The other new tabs have no admin GET endpoint in this branch,
// so they stay honest placeholders (no fabricated rows).

export const AdminPlaceholderTab: React.FC<{ title: string; description?: string }> = ({ title, description }) => (
  <div className="space-y-3">
    <div className="bg-white rounded-2xl border border-slate-200 p-4">
      <h3 className="text-xs font-bold text-slate-800">{title}</h3>
      <p className="text-[11px] text-slate-500 mt-1">
        {description || 'محتوای این بخش پس از ورود به حساب کاربری و دریافت داده از سرور نمایش داده می‌شود.'}
      </p>
    </div>
  </div>
);

const pick = (item: any, keys: string[]): string => {
  for (const k of keys) {
    const v = item?.[k];
    if (v !== undefined && v !== null && v !== '') return String(v);
  }
  return '—';
};

export const AdminSupportReturnsTab: React.FC = () => {
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  useEffect(() => {
    let on = true;
    adminApi.getAdminReturns()
      .then((list) => { if (on) setItems(list); })
      .catch((err: any) => { if (on) setLoadError(err?.message || 'خطا در دریافت مرجوعی‌ها'); })
      .finally(() => { if (on) setLoading(false); });
    return () => { on = false; };
  }, []);
  return (
    <div className="space-y-3">
      <div className="bg-white rounded-2xl border border-slate-200 p-4 flex items-center gap-2">
        <RotateCcw className="w-4 h-4 text-slate-500"  strokeWidth={1.5}/>
        <h3 className="text-xs font-bold text-slate-800">مرجوعی‌ها</h3>
      </div>
      {loading && <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">در حال بارگذاری…</div>}
      {loadError && <div className="bg-white rounded-2xl border border-rose-200 p-4 text-xs text-rose-600">{loadError}</div>}
      {!loading && !loadError && items.length === 0 && (
        <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">مرجوعی ثبت‌شده‌ای وجود ندارد.</div>
      )}
      {!loading && !loadError && items.map((r: any, i: number) => (
        <div key={r.id || i} className="bg-white rounded-2xl border border-slate-200 p-4 space-y-1">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-bold text-slate-800">سفارش {pick(r, ['orderId', 'order_id', 'orderNumber'])}</span>
            <span className="text-[10px] font-bold bg-slate-100 text-slate-700 px-2 py-0.5 rounded-full">{pick(r, ['status'])}</span>
          </div>
          {r.reason && <p className="text-[11px] text-slate-600">دلیل: {r.reason}</p>}
          {r.trackingNumber && <p className="text-[11px] text-slate-500">کد رهگیری: {r.trackingNumber}</p>}
        </div>
      ))}
    </div>
  );
};
