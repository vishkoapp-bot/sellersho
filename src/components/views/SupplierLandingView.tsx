import { markedUpPrice } from '../../utils/money';
import React, { useState } from 'react';
import { ArrowRight, ArrowLeft, Building2, CheckCircle2, Package, ChevronDown } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { authApi } from '../../services/authApi';

const steps = [
  ['ثبت‌نام و پیشنهاد محصولات', 'محصولات خود را با قیمت عمده و مشخصات کامل پیشنهاد دهید.'],
  ['بررسی و انتشار', 'تیم ما محصولات را بررسی می‌کند و موارد تأییدشده در کاتالوگ مرکزی منتشر می‌شوند.'],
  ['ارسال و تسویه', 'دراپ‌شیپرها می‌فروشند و پس از ارسال و تأیید تحویل، تسویه انجام می‌شود.'],
];
const benefits = ['عرضهٔ کالا در شبکهٔ فروشندگان', 'قیمت‌گذاری شفاف و کنترل موجودی', 'ارسال مستقیم یا از انبار مرکزی', 'گزارش فروش، موجودی و تسویه'];
const faqs = [
  ['چطور قیمت‌گذاری پلکانی تعریف کنم؟', 'در پنل تأمین‌کننده برای هر محصول، بازهٔ تعداد و قیمت مربوط به آن را اضافه کنید.'],
  ['زمان تسویه چقدر است؟', 'تسویه طبق چرخهٔ مالی پلتفرم و پس از تأیید تحویل سفارش انجام می‌شود.'],
  ['آیا می‌توانم مستقیم ارسال کنم؟', 'بله، گزینهٔ ارسال مستقیم از انبار تأمین‌کننده در پنل قابل فعال‌سازی است.'],
];
const fields = [
  { key: 'companyName', label: 'نام شرکت یا برند', type: 'text', autoComplete: 'organization', required: true },
  { key: 'managerName', label: 'نام مدیر (اختیاری)', type: 'text', autoComplete: 'name' },
  { key: 'phone', label: 'شمارهٔ موبایل', type: 'tel', autoComplete: 'tel-national', required: true },
  { key: 'email', label: 'ایمیل (اختیاری)', type: 'email', autoComplete: 'email' },
  { key: 'warehouseCity', label: 'شهر انبار', type: 'text', autoComplete: 'off' },
] as const;
const panel = 'rounded-[var(--shop-radius-lg)] border border-[var(--shop-line)] bg-white p-4 sm:p-6';

export const SupplierLandingView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { platformSettings, formatPrice, toPersianDigits, setActiveView } = useStore();
  const { success, error } = useToast();
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState({ companyName: '', managerName: '', phone: '', email: '', warehouseCity: 'تهران' });
  const wholesalePrice = 1200000;
  const basePrice = markedUpPrice(wholesalePrice, platformSettings.platformPct ?? 0);
  const sellerProfit = 200000;
  const example = [
    ['قیمت عمدهٔ شما', wholesalePrice],
    ['قیمت پایه در فروشگاه‌ها', basePrice],
    ['سود فروشنده', sellerProfit],
    ['قیمت نهایی مشتری', basePrice + sellerProfit],
  ] as const;

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!form.companyName.trim()) return error('نام شرکت یا برند الزامی است.');
    if (!/^09\d{9}$/.test(form.phone.replace(/[\s-]/g, ''))) return error('شمارهٔ موبایل معتبر وارد کنید (۱۱ رقم، با ۰۹ شروع شود).');
    setLoading(true);
    try {
      const result = await authApi.registerSupplier({
        company_name: form.companyName,
        contact_person: form.managerName || form.companyName,
        phone: form.phone,
        email: form.email,
        warehouse_city: form.warehouseCity,
      });
      if (!result.success || !result.supplierId) throw new Error('پاسخ ثبت‌نام معتبر نیست.');
      success('ثبت‌نام انجام شد. با شمارهٔ ثبت‌شده و کد پیامکی وارد شوید.');
      setActiveView('supplier_dashboard');
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت‌نام انجام نشد.');
    } finally {
      setLoading(false);
    }
  };

  return (
    <main id="main-content" dir="rtl" tabIndex={-1} className="central-wrap flex-1 space-y-6 py-6 lg:space-y-8 lg:py-10">
      <button type="button" onClick={onBack} className="inline-flex min-h-11 items-center gap-2 rounded-xl px-2 text-sm font-bold text-[var(--shop-forest)] hover:bg-[var(--shop-soft)]">
        <ArrowRight size={17} aria-hidden="true" /> بازگشت به فروشگاه
      </button>
      <section className="grid items-center gap-6 rounded-[var(--shop-radius-lg)] bg-[var(--shop-forest)] p-6 text-white sm:p-9 md:grid-cols-[1fr_auto] lg:p-12" aria-labelledby="supplier-heading">
        <div>
          <h1 id="supplier-heading" className="text-3xl leading-tight font-black sm:text-5xl">همکاری با تأمین‌کنندگان</h1>
          <p className="mt-4 max-w-lg text-sm leading-7 text-[#e2ebe4] sm:text-base">محصولاتتان را به کاتالوگ سلرشو اضافه کنید و موجودی، سفارش‌ها و تسویه را از پنل تأمین‌کننده مدیریت کنید.</p>
          <a href="#supplier-form" className="mt-5 inline-flex min-h-12 items-center gap-2 rounded-xl bg-[var(--shop-gold)] px-5 text-sm font-extrabold text-[var(--shop-ink)] hover:bg-[#f8d88e]">
            ثبت‌نام تأمین‌کننده <ArrowLeft size={18} aria-hidden="true" />
          </a>
        </div>
        <div className="hidden size-28 place-items-center rounded-3xl border border-white/25 bg-white/10 text-[var(--shop-gold)] md:grid" aria-hidden="true"><Building2 size={52} strokeWidth={1.4} /></div>
      </section>

      <section className={panel}>
        <h2 className="mb-4 text-lg font-black">مراحل همکاری</h2>
        <ol className="grid gap-3 md:grid-cols-3">
          {steps.map(([title, copy], index) => (
            <li key={title} className="flex gap-3 rounded-[var(--shop-radius)] bg-[var(--shop-soft)] p-4">
              <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-white text-sm font-black text-[var(--shop-forest)]" aria-hidden="true">{toPersianDigits(index + 1)}</span>
              <div><h3 className="text-sm font-bold">{title}</h3><p className="mt-1 text-sm leading-6 text-[var(--shop-muted)]">{copy}</p></div>
            </li>
          ))}
        </ol>
      </section>

      <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.35fr)]">
        <div className="space-y-5">
          <section className={panel}>
            <h2 className="mb-4 text-lg font-black">امکانات تأمین‌کنندگان</h2>
            <ul className="grid gap-3">
              {benefits.map(benefit => <li key={benefit} className="flex items-center gap-2 text-sm leading-6"><CheckCircle2 size={17} className="shrink-0 text-[var(--shop-forest)]" aria-hidden="true" />{benefit}</li>)}
            </ul>
          </section>
          <section className={panel}>
            <h2 className="flex items-center gap-2 text-lg font-black"><Package size={20} className="text-[var(--shop-forest)]" aria-hidden="true" /> نمونهٔ محاسبهٔ یک فروش</h2>
            <p className="mt-2 text-sm leading-6 text-[var(--shop-muted)]">این ارقام نمونه‌اند.</p>
            <dl className="mt-4 divide-y divide-[var(--shop-line)] text-sm">
              {example.map(([label, value]) => <div key={label} className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 py-2.5"><dt>{label}</dt><dd className="font-bold tabular-nums">{formatPrice(value)} تومان</dd></div>)}
            </dl>
          </section>
        </div>
        <section id="supplier-form" tabIndex={-1} className={panel} aria-labelledby="supplier-form-title">
          <h2 id="supplier-form-title" className="text-xl font-black">ثبت‌نام تأمین‌کننده</h2>
          <p className="mt-2 text-sm leading-7 text-[var(--shop-muted)]">برای ورود به پنل، کد تأیید به همین شمارهٔ موبایل ارسال می‌شود.</p>
          <form onSubmit={submit} className="mt-5 space-y-4">
            <div className="grid gap-4 sm:grid-cols-2">
              {fields.map(field => (
                <label key={field.key} className="grid gap-1.5 text-sm font-bold">
                  {field.label}
                  <input name={field.key} autoComplete={field.autoComplete} type={field.type} required={'required' in field}
                    dir={field.type === 'text' ? 'rtl' : 'ltr'} value={form[field.key]}
                    onChange={event => setForm(current => ({ ...current, [field.key]: event.target.value }))}
                    className="min-h-12 w-full rounded-xl border border-[var(--shop-line)] bg-[var(--shop-bg)] px-3 text-base font-medium focus:bg-white" />
                </label>
              ))}
            </div>
            <details className="rounded-xl border border-[var(--shop-line)] bg-[var(--shop-soft)] px-3">
              <summary className="flex min-h-11 cursor-pointer items-center justify-between gap-2 text-sm font-bold">مطالعهٔ قرارداد تأمین‌کننده <ChevronDown size={16} aria-hidden="true" /></summary>
              <p className="pb-4 text-sm leading-7 whitespace-pre-wrap text-[var(--shop-muted)]">{platformSettings.contracts.supplierContractText}</p>
            </details>
            <label className="flex min-h-11 cursor-pointer items-center gap-2 text-sm leading-6">
              <input type="checkbox" required className="size-4 shrink-0 accent-[var(--shop-forest)]" /> قرارداد تأمین‌کننده را مطالعه کرده‌ام و می‌پذیرم.
            </label>
            <button type="submit" disabled={loading} className="central-button w-full disabled:cursor-not-allowed disabled:opacity-60">
              {loading ? 'در حال ثبت…' : 'ثبت‌نام و ادامه به ورود'}
            </button>
          </form>
        </section>
      </div>

      <section className={panel}>
        <h2 className="mb-2 text-lg font-black">پرسش‌های متداول</h2>
        <div className="divide-y divide-[var(--shop-line)]">
          {faqs.map(([question, answer]) => (
            <details key={question} className="group">
              <summary className="flex min-h-12 cursor-pointer items-center justify-between gap-3 py-2 text-sm font-bold">
                {question}<ChevronDown size={17} className="shrink-0 group-open:rotate-180" aria-hidden="true" />
              </summary>
              <p className="pb-4 text-sm leading-7 text-[var(--shop-muted)]">{answer}</p>
            </details>
          ))}
        </div>
      </section>
    </main>
  );
};
