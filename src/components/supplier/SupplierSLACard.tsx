import React, { useEffect, useState } from 'react';
import { apiRequest } from '../../services/apiClient';
export const SupplierSLACard: React.FC = () => {
  const [sla, setSla] = useState<{ samples: number; onTime: number; score: number; eligible: boolean } | null>(null);
  const [error, setError] = useState('');
  useEffect(() => { let active = true; apiRequest<typeof sla>('/supplier/shipping-sla', { auth: true }).then((v) => { if (active) setSla(v); }).catch((e) => { if (active) setError(e.message); }); return () => { active = false; }; }, []);
  return <section className="space-y-2 rounded-2xl border border-teal-100 bg-teal-50/40 p-4 text-xs"><h3 className="font-bold text-teal-900">مجوز خودکار ارسال مستقیم</h3><p className="leading-6 text-slate-600">عملکرد ارسال در ۹۰ روز اخیر بررسی می‌شود. امتیاز بالاتر از ۹۸٪ و حساب تأییدشده، مجوز خودکار ایجاد می‌کند. تصمیم صریح مدیر حفظ می‌شود؛ افت امتیاز، مجوز خودکار را متوقف می‌کند.</p>{error ? <p role="alert" className="text-rose-700">{error}</p> : sla ? <p className="font-bold">{sla.samples ? `${new Intl.NumberFormat('fa-IR', { maximumFractionDigits: 2 }).format(sla.score)}٪ · ${new Intl.NumberFormat('fa-IR').format(sla.samples)} ارسال بررسی‌شده · ${sla.eligible ? 'واجد شرایط' : 'فاقد شرایط فعلی'}` : 'هنوز سابقهٔ کافی برای اعطای خودکار وجود ندارد.'}</p> : <p>در حال دریافت عملکرد…</p>}</section>;
};
