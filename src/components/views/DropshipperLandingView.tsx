import { markedUpPrice } from '../../utils/money';
import React from 'react';
import { ArrowRight, ArrowLeft, Store, CheckCircle2, ChevronDown } from 'lucide-react';
import { useStore } from '../../context/StoreContext';

const steps = [
  ['فروشگاه خود را بسازید', 'با شمارهٔ موبایل ثبت‌نام کنید و نام و نشانی فروشگاه خود را انتخاب کنید.'],
  ['محصولات را انتخاب کنید', 'از کاتالوگ مرکزی محصول انتخاب کنید و سود فروش خود را در محدودهٔ مجاز تنظیم کنید.'],
  ['فروش را آغاز کنید', 'لینک فروشگاه را منتشر کنید. تأمین و ارسال سفارش طبق روش محصول انجام می‌شود.'],
];
const tools = ['کاتالوگ مشترک تأمین‌کنندگان', 'فروشگاه با نشانی اختصاصی', 'مدیریت سفارش‌ها و گزارش فروش', 'تنظیم سود و مشاهدهٔ درآمد'];
const faqs = [
  ['آیا باید کالا را از قبل بخرم؟', 'در مدل دراپ‌شیپینگ نیازی به خرید موجودی اولیه نیست. هزینهٔ پلن و سایر خدمات را هنگام انتخاب بررسی کنید.'],
  ['چه کسی سفارش را ارسال می‌کند؟', 'بر اساس روش تأمین محصول، سفارش از انبار تأمین‌کننده یا انبار مرکزی ارسال می‌شود.'],
  ['سود چه زمانی قابل برداشت است؟', 'پس از تأیید تحویل و پایان دورهٔ نگهداری وجه، مطابق قواعد مالی پلتفرم. وضعیت درآمد در پنل قابل مشاهده است.'],
  ['آیا درآمد تضمین شده است؟', 'فروش به انتخاب محصول، بازاریابی، قیمت‌گذاری و تجربهٔ مشتری بستگی دارد.'],
];
const panel = 'rounded-[var(--shop-radius-lg)] border border-[var(--shop-line)] bg-white p-4 sm:p-6';

export const openSellerOnboarding = (setIsSellerModalOpen: (open: boolean) => void) => {
  setIsSellerModalOpen(true);
};

export const DropshipperLandingView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { setIsSellerModalOpen, platformSettings, formatPrice, toPersianDigits } = useStore();
  const start = () => openSellerOnboarding(setIsSellerModalOpen);
  const price = markedUpPrice(1200000, platformSettings.platformPct ?? 0);
  const profit = 200000;
  const example = [['قیمت خرید شما', price], ['سود شما', profit], ['قیمت فروش به مشتری', price + profit]] as const;
  const referralProgram = platformSettings.referralProgram;

  return (
    <main id="main-content" dir="rtl" tabIndex={-1} className="central-wrap flex-1 space-y-6 py-6 lg:space-y-8 lg:py-10">
      <button type="button" onClick={onBack} className="inline-flex min-h-11 items-center gap-2 rounded-xl px-2 text-sm font-bold text-[var(--shop-forest)] hover:bg-[var(--shop-soft)]">
        <ArrowRight size={17} aria-hidden="true" /> بازگشت به فروشگاه
      </button>
      <section className="grid items-center gap-6 rounded-[var(--shop-radius-lg)] bg-[var(--shop-forest)] p-6 text-white sm:p-9 md:grid-cols-[1fr_auto] lg:p-12" aria-labelledby="seller-heading">
        <div>
          <h1 id="seller-heading" className="text-3xl leading-tight font-black sm:text-5xl">فروشگاه خودتان، بدون خرید موجودی اولیه</h1>
          <p className="mt-4 max-w-xl text-sm leading-7 text-[#e2ebe4] sm:text-base">محصولات تأمین‌کنندگان را در ویترین اختصاصی خود عرضه کنید. کاتالوگ، سفارش‌ها و درآمد فروشگاه در پنل فروشنده در دسترس‌اند.</p>
          <button type="button" onClick={start} className="mt-5 inline-flex min-h-12 items-center gap-2 rounded-xl bg-[var(--shop-gold)] px-5 text-sm font-extrabold text-[var(--shop-ink)] hover:bg-[#f8d88e]">
            ساخت فروشگاه <ArrowLeft size={18} aria-hidden="true" />
          </button>
        </div>
        <div className="hidden size-28 place-items-center rounded-3xl border border-white/25 bg-white/10 text-[var(--shop-gold)] md:grid" aria-hidden="true"><Store size={52} strokeWidth={1.4} /></div>
      </section>

      <section className={panel}>
        <h2 className="mb-4 text-lg font-black">مراحل شروع فروش</h2>
        <ol className="grid gap-3 md:grid-cols-3">
          {steps.map(([title, copy], index) => (
            <li key={title} className="flex gap-3 rounded-[var(--shop-radius)] bg-[var(--shop-soft)] p-4">
              <span className="grid size-9 shrink-0 place-items-center rounded-xl bg-white text-sm font-black text-[var(--shop-forest)]" aria-hidden="true">{toPersianDigits(index + 1)}</span>
              <div><h3 className="text-sm font-bold">{title}</h3><p className="mt-1 text-sm leading-6 text-[var(--shop-muted)]">{copy}</p></div>
            </li>
          ))}
        </ol>
      </section>

      <div className="grid gap-5 lg:grid-cols-2">
        <section className={panel}>
          <h2 className="mb-4 text-lg font-black">امکانات فروشندگان</h2>
          <ul className="grid gap-3">
            {tools.map(tool => <li key={tool} className="flex items-center gap-2 text-sm leading-6"><CheckCircle2 size={17} className="shrink-0 text-[var(--shop-forest)]" aria-hidden="true" />{tool}</li>)}
          </ul>
        </section>
        <section className={panel}>
          <h2 className="text-lg font-black">سود یک سفارش چگونه محاسبه می‌شود؟</h2>
          <p className="mt-2 text-sm leading-6 text-[var(--shop-muted)]">این ارقام نمونه‌اند. سود واقعی به قیمت و شرایط سفارش بستگی دارد.</p>
          <dl className="mt-4 divide-y divide-[var(--shop-line)] text-sm">
            {example.map(([label, value]) => <div key={label} className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 py-2.5"><dt>{label}</dt><dd className="font-bold tabular-nums">{formatPrice(value)} تومان</dd></div>)}
          </dl>
        </section>
      </div>

      <section className={panel}>
        <h2 className="text-lg font-black">پلن‌های فروشگاه</h2>
        <p className="my-3 text-sm leading-6 text-[var(--shop-muted)]">سقف کاتالوگ آزمایشی {toPersianDigits(platformSettings.subscriptionPlanLimits.trial)} محصول است. امکانات هر پلن را پیش از انتخاب بررسی کنید.</p>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {platformSettings.subscriptionPlans.map(plan => (
            <div key={plan.id} className="rounded-[var(--shop-radius)] border border-[var(--shop-line)] bg-[var(--shop-bg)] p-4">
              <h3 className="flex flex-wrap justify-between gap-2 text-sm font-extrabold">
                <span>{plan.name}</span>
                <span className="text-[var(--shop-forest)]">{plan.price === 0 ? 'رایگان' : `${formatPrice(plan.price)} تومان`}</span>
              </h3>
              <p className="mt-2 text-sm leading-7 text-[var(--shop-muted)]">{plan.durationLabel} · {plan.productLimit === 9999 ? 'محصولات نامحدود' : `${toPersianDigits(plan.productLimit)} محصول`} · {toPersianDigits(plan.allowedCustomDomains)} دامنهٔ اختصاصی · {plan.maxStores > 1 ? `تا ${toPersianDigits(plan.maxStores)} فروشگاه` : 'یک فروشگاه'}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="rounded-[var(--shop-radius-lg)] border border-[var(--shop-line)] bg-[var(--shop-soft)] p-4 sm:p-6">
        <h2 className="text-lg font-black">معرفی فروشندگان دیگر</h2>
        <p className="mt-2 text-sm leading-7 text-[var(--shop-muted)]">{referralProgram.isEnabled ? `از بخش معرفی در پنل، لینک دعوت دریافت کنید. نرخ فعلی کمیسیون معرفی ${toPersianDigits(referralProgram.sellerCommissionPercent)} درصد از کارمزد پلتفرم است. پرداخت تابع شرایط برنامه و تأیید سفارش است.` : 'برنامهٔ معرفی در حال حاضر فعال نیست. وضعیت آن را در پنل بررسی کنید.'}</p>
      </section>

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
      <button type="button" onClick={start} className="central-button w-full sm:w-auto">ثبت‌نام و ساخت فروشگاه <ArrowLeft size={18} aria-hidden="true" /></button>
    </main>
  );
};
