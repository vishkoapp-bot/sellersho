import React, { useState } from 'react';
import { ArrowRight, ArrowLeft, Building2, CheckCircle2, ChevronDown, HelpCircle, Package, Sparkles, TrendingUp } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { roundTo100 } from '../../utils/pricingEngine';
import { useToast } from '../../context/ToastContext';
import { authApi } from '../../services/authApi';
import { isMockMode, tokenStore } from '../../services/apiClient';

interface Props { onBack: () => void; }
const steps = [['۱','ثبت‌نام و پیشنهاد محصولات','محصولات خود را با قیمت عمده و مشخصات کامل پیشنهاد دهید.'],['۲','تأیید مدیر و انتشار','تیم ما محصولات را بررسی و در کاتالوگ مرکزی منتشر می‌کند.'],['۳','سفارش‌ها و تسویه','دراپ‌شیپرها می‌فروشند، شما ارسال می‌کنید و تسویه انجام می‌شود.']];
const faqs = [['چطور قیمت‌گذاری پلکانی تعریف کنم؟','در پنل تأمین‌کننده برای هر محصول، بازه تعداد و قیمت مربوط به آن را اضافه کنید.'],['زمان تسویه چقدر است؟','تسویه طبق چرخه مالی پلتفرم و پس از تأیید تحویل سفارش انجام می‌شود.'],['آیا می‌توانم مستقیم ارسال کنم؟','بله، گزینه ارسال مستقیم از انبار تأمین‌کننده در پنل قابل فعال‌سازی است.']];

export const SupplierLandingView: React.FC<Props> = ({ onBack }) => {
  const { platformSettings, formatPrice, toPersianDigits, setActiveView, setSupplierSession } = useStore();
  const { success, error } = useToast();
  const [faq, setFaq] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState({ companyName:'', managerName:'', username:'', password:'', phone:'', email:'', warehouseCity:'تهران' });
  const pct = platformSettings.platformPct ?? 0;
  const C = 1200000, B = roundTo100(C * (1 + pct / 100)), fee = B - C, D = 200000, P = B + D;
  const update = (key: keyof typeof form, value: string) => setForm((prev) => ({ ...prev, [key]: value }));
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!form.companyName || !form.username || !form.password) return error('نام شرکت، نام کاربری و کلمه عبور الزامی است.');
    setLoading(true);
    try {
      if (isMockMode()) setSupplierSession({ token:'mock-supplier-token', supplierId:`sup-${form.username}`, companyName:form.companyName, managerName:form.managerName || form.companyName, warehouseCity:form.warehouseCity, loginAt:new Date().toISOString(), signedContractVersion:platformSettings.contracts.supplierContractVersion });
      else { const result = await authApi.registerSupplier({ company_name:form.companyName, contact_person:form.managerName || form.companyName, username:form.username, password:form.password, phone:form.phone, email:form.email, warehouse_city:form.warehouseCity }); if (!result.success || !result.token || !result.supplier?.id) throw new Error('پاسخ ثبت‌نام معتبر نیست. لطفاً دوباره تلاش کنید.'); if (result.token) { tokenStore.setToken(result.token); setSupplierSession({ token:result.token, supplierId:result.supplier?.id || form.username, companyName:form.companyName, managerName:form.managerName || form.companyName, warehouseCity:form.warehouseCity, loginAt:new Date().toISOString() }); } }
      success('درخواست همکاری با موفقیت ثبت شد.'); setActiveView('supplier_dashboard');
    } catch (e: any) { error(e?.message || 'ثبت‌نام انجام نشد.'); } finally { setLoading(false); }
  };
  const scrollToForm = () => document.getElementById('supplier-form')?.scrollIntoView({ behavior:'smooth' });
  return <div dir="rtl" className="flex-1 min-h-screen bg-slate-50 text-slate-800 pb-20">
    <header className="sticky top-0 z-20 flex items-center justify-between border-b bg-white/95 px-3 py-2.5 backdrop-blur"><button onClick={onBack} className="rounded-xl p-2 hover:bg-slate-100" title="بازگشت"><ArrowRight className="h-5 w-5"  strokeWidth={1.5}/></button><div><h1 className="text-xs font-black">همکاری با تأمین‌کنندگان</h1><span className="text-[10px] text-slate-400">رشد فروش بدون هزینه بازاریابی</span></div><button onClick={scrollToForm} className="rounded-xl bg-blue-600 px-3 py-2 text-[10px] font-bold text-white">ثبت‌نام</button></header>
    <main className="space-y-4 p-3.5">
      <section className="rounded-3xl bg-gradient-to-br from-slate-950 via-blue-950 to-indigo-950 p-5 text-white shadow-xl"><Sparkles className="mb-3 h-5 w-5 text-blue-300"  strokeWidth={1.5}/><h2 className="text-lg font-black leading-relaxed">شریک تأمین ما شوید — محصولات خود را به هزاران دراپ‌شیپر برسانید</h2><p className="mt-2 text-xs leading-7 text-slate-300">محصولات شما در اختیار شبکه‌ای از فروشندگان آنلاین قرار می‌گیرد؛ شما روی تأمین و کیفیت تمرکز کنید.</p><button onClick={scrollToForm} className="mt-4 flex w-full items-center justify-center gap-2 rounded-xl bg-blue-500 py-3 text-xs font-black">شروع همکاری <ArrowLeft className="h-4 w-4"  strokeWidth={1.5}/></button></section>
      <section className="rounded-2xl border bg-white p-4 shadow-sm"><h3 className="mb-3 flex items-center gap-2 text-sm font-black"><Package className="h-5 w-5 text-blue-600"  strokeWidth={1.5}/>چطور کار می‌کند؟</h3><div className="space-y-2">{steps.map(([n,title,text]) => <div key={n} className="flex gap-3 rounded-xl bg-slate-50 p-3"><b className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs text-white">{n}</b><div><b className="text-xs">{title}</b><p className="mt-1 text-[11px] leading-6 text-slate-500">{text}</p></div></div>)}</div></section>
      <section className="rounded-2xl border bg-white p-4 shadow-sm"><h3 className="mb-3 text-sm font-black">مزایای همکاری</h3><div className="grid grid-cols-2 gap-2">{['دسترسی به هزاران دراپ‌شیپر بدون هزینه بازاریابی','قیمت‌گذاری شفاف و کنترل موجودی','ارسال مستقیم یا از انبار مرکزی','پنل گزارش فروش، موجودی و تسویه'].map((text) => <div key={text} className="rounded-xl border border-blue-100 bg-blue-50 p-3 text-[11px] font-bold leading-6"><CheckCircle2 className="mb-1 h-4 w-4 text-blue-600"  strokeWidth={1.5}/>{text}</div>)}</div></section>

      <section className="rounded-2xl border bg-white p-4"><h3 className="mb-3 flex items-center gap-2 text-sm font-black"><TrendingUp className="h-5 w-5 text-emerald-600"  strokeWidth={1.5}/>مدل درآمد شفاف</h3>{[['قیمت عمده C',C],[`سهم پلتفرم (${toPersianDigits(pct)}٪)`,fee],['قیمت پایه R = C × (۱ + سهم)',B],['سود دراپ‌شیپر D',D],['قیمت نهایی P = R + D',P]].map(([label,value]) => <div key={label} className="flex justify-between border-b py-2 text-xs"><span>{label}</span><b>{formatPrice(Number(value))} تومان</b></div>)}<p className="mt-3 text-xs text-emerald-700">سهم شما از فروش: {formatPrice(C)} تومان؛ این ارقام صرفاً مثال هستند.</p></section>
      <section id="supplier-form" className="scroll-mt-20 rounded-2xl border border-blue-200 bg-white p-4"><h3 className="mb-3 flex items-center gap-2 text-sm font-black"><Building2 className="h-5 w-5"  strokeWidth={1.5}/>ثبت‌نام تأمین‌کننده</h3><form onSubmit={submit} className="space-y-3">{([['companyName','نام شرکت یا برند'],['managerName','نام مدیر'],['username','نام کاربری'],['password','کلمه عبور'],['phone','شماره تماس'],['email','ایمیل'],['warehouseCity','شهر انبار']] as const).map(([key,label]) => <label key={key} className="block text-xs font-bold">{label}<input required={['companyName','username','password'].includes(key)} type={key === 'password' ? 'password' : key === 'email' ? 'email' : key === 'phone' ? 'tel' : 'text'} value={form[key]} onChange={(e) => update(key,e.target.value)} className="mt-1 w-full rounded-xl border bg-slate-50 p-3 focus:border-blue-500" /></label>)}<details className="rounded-xl bg-slate-50 p-3 text-xs"><summary className="cursor-pointer font-bold">مطالعه قرارداد تأمین‌کننده</summary><p className="mt-3 whitespace-pre-wrap leading-7">{platformSettings.contracts.supplierContractText}</p></details><label className="flex gap-2 text-xs"><input type="checkbox" required />قرارداد تأمین‌کننده را مطالعه کرده‌ام و می‌پذیرم.</label><button disabled={loading} className="w-full rounded-xl bg-blue-600 py-3 text-xs font-black text-white disabled:opacity-50">{loading ? 'در حال ثبت...' : 'ثبت‌نام و دریافت پنل'}</button></form></section>
      <section className="rounded-2xl border bg-white p-4"><h3 className="mb-3 flex items-center gap-2 text-sm font-black"><HelpCircle className="h-5 w-5"  strokeWidth={1.5}/>پرسش‌های متداول</h3>{faqs.map(([question,answer],index) => <div key={question} className="border-b last:border-0"><button aria-expanded={faq === index} onClick={() => setFaq(faq === index ? null : index)} className="flex w-full items-center justify-between gap-2 py-3 text-right text-xs font-bold">{question}<ChevronDown className="h-4 w-4 shrink-0"  strokeWidth={1.5}/></button>{faq === index && <p className="pb-3 text-xs leading-6 text-slate-500">{answer}</p>}</div>)}</section>
    </main>
  </div>;
};
