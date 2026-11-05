import React from 'react';
import { ArrowRight, ArrowLeft, Store, CheckCircle2 } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { roundTo100 } from '../../utils/pricingEngine';

export const openSellerOnboarding = (setIsSellerModalOpen: (open: boolean) => void) => {
  setIsSellerModalOpen(true);
};

export const DropshipperLandingView: React.FC<{ onBack: () => void }> = ({ onBack }) => {
  const { setIsSellerModalOpen, platformSettings, formatPrice, toPersianDigits } = useStore();
  const start = () => openSellerOnboarding(setIsSellerModalOpen);
  const cost = 1200000;
  const profit = 200000;
  const pct = platformSettings.platformPct ?? 0;
  const fee = roundTo100(cost * (1 + pct / 100)) - cost;
  const steps = [
    ['فروشگاه خود را بسازید', 'با شماره موبایل ثبت‌نام کنید و نام و نشانی فروشگاه خود را انتخاب کنید.'],
    ['محصولات را انتخاب کنید', 'از کاتالوگ مرکزی محصول بردارید و سود فروش خود را در محدوده مجاز تنظیم کنید.'],
    ['فروش را آغاز کنید', 'لینک فروشگاه را منتشر کنید؛ تأمین و ارسال سفارش طبق روش محصول انجام می‌شود.'],
  ];
  return (
    <div dir="rtl" className="min-h-screen flex-1 bg-slate-50 pb-20 text-slate-800">
      <header className="sticky top-0 z-20 flex items-center justify-between border-b bg-white/95 px-3 py-3 backdrop-blur">
        <button onClick={onBack} aria-label="بازگشت" className="rounded-xl p-2 hover:bg-slate-100"><ArrowRight className="h-5 w-5"  strokeWidth={1.5}/></button>
        <h1 className="text-xs font-black">راهنمای شروع دراپ‌شیپینگ</h1>
        <button onClick={start} className="rounded-xl bg-emerald-700 px-3 py-2 text-xs font-bold text-white">شروع</button>
      </header>
      <main className="space-y-4 p-3.5">
        <section className="relative overflow-hidden rounded-3xl bg-emerald-950 p-5 text-white">
          <Store className="mb-6 h-10 w-10 text-lime-300"  strokeWidth={1.5}/>
          <p className="mb-2 text-xs font-bold text-lime-300">از انتخاب محصول تا اولین سفارش</p>
          <h2 className="text-2xl font-black leading-relaxed">فروشگاه شما،<br />بدون خرید موجودی اولیه</h2>
          <p className="mt-3 text-xs leading-7 text-emerald-100">روی معرفی محصولات و ارتباط با مشتری تمرکز کنید. محصولات تأمین‌کنندگان را در فروشگاه خود عرضه کنید و از هر فروش سود بگیرید.</p>
          <button onClick={start} className="mt-5 flex w-full items-center justify-center gap-2 rounded-xl bg-lime-300 py-3 text-xs font-black text-emerald-950">ساخت فروشگاه <ArrowLeft className="h-4 w-4"  strokeWidth={1.5}/></button>
        </section>
        <section className="rounded-2xl border bg-white p-4">
          <h3 className="mb-4 text-sm font-black">سه قدم تا شروع فروش</h3>
          <ol className="space-y-4">{steps.map(([title, text], index) => <li key={title} className="flex gap-3"><span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-emerald-100 text-sm font-black text-emerald-800">{toPersianDigits(index + 1)}</span><div><h4 className="text-xs font-bold">{title}</h4><p className="mt-1 text-xs leading-6 text-slate-500">{text}</p></div></li>)}</ol>
        </section>
        <section className="rounded-2xl border bg-white p-4">
          <h3 className="mb-3 text-sm font-black">ابزارهای همراه شما</h3>
          <div className="grid grid-cols-2 gap-3">{['کاتالوگ مشترک تأمین‌کنندگان', 'فروشگاه با نشانی اختصاصی', 'مدیریت سفارش‌ها و گزارش فروش', 'تنظیم سود و مشاهده درآمد'].map(text => <div key={text} className="rounded-xl bg-slate-50 p-3 text-xs leading-6"><CheckCircle2 className="mb-2 h-4 w-4 text-emerald-700"  strokeWidth={1.5}/>{text}</div>)}</div>
        </section>
        <section className="rounded-2xl border bg-white p-4">
          <h3 className="mb-2 text-sm font-black">سود یک سفارش چگونه محاسبه می‌شود؟</h3>
          <p className="mb-3 text-xs leading-6 text-slate-500">مثال آموزشی؛ سود واقعی به محصول، قیمت انتخابی و شرایط سفارش بستگی دارد.</p>
          {([['قیمت خرید شما', cost + fee], ['سود شما', profit], ['قیمت فروش به مشتری', cost + fee + profit]] as const).map(([label, value]) => <div key={label} className="flex justify-between gap-2 border-b py-2 text-xs last:border-0"><span>{label}</span><b>{formatPrice(value)} تومان</b></div>)}
        </section>

        <section className="rounded-2xl border bg-white p-4">
          <h3 className="mb-3 text-sm font-black">پلن مناسب شروع شما</h3>
          <p className="mb-3 text-xs leading-6 text-slate-500">سقف فعلی کاتالوگ تریال: {toPersianDigits(platformSettings.subscriptionPlanLimits.trial)} محصول. امکانات هر پلن را پیش از انتخاب بررسی کنید.</p>
          <div className="space-y-2">{platformSettings.subscriptionPlans.map(plan => <div key={plan.id} className="rounded-xl bg-slate-50 p-3"><div className="flex justify-between gap-2 text-xs font-bold"><span>{plan.name}</span><span>{plan.price === 0 ? 'رایگان' : `${formatPrice(plan.price)} تومان`}</span></div><p className="mt-2 text-xs leading-6 text-slate-500">{plan.durationLabel} · {plan.productLimit === 9999 ? 'محصولات نامحدود' : `${toPersianDigits(plan.productLimit)} محصول`} · {toPersianDigits(plan.allowedCustomDomains)} دامنه اختصاصی · {plan.maxStores > 1 ? `تا ${toPersianDigits(plan.maxStores)} فروشگاه` : 'یک فروشگاه'}</p></div>)}</div>
        </section>
        <section className="rounded-2xl bg-emerald-100 p-4 text-emerald-950">
          <h3 className="text-sm font-black">معرفی فروشندگان دیگر</h3>
          <p className="mt-2 text-xs leading-7">{platformSettings.referralProgram.isEnabled ? `از بخش معرفی در پنل، لینک دعوت دریافت کنید. نرخ فعلی کمیسیون معرفی ${toPersianDigits(platformSettings.referralProgram.sellerCommissionPercent)} درصد از کارمزد پلتفرم است؛ پرداخت تابع شرایط برنامه و تأیید سفارش است.` : 'برنامه معرفی در حال حاضر فعال نیست. وضعیت آن را در پنل بررسی کنید.'}</p>
        </section>
        <section className="rounded-2xl border bg-white p-4">
          <h3 className="mb-2 text-sm font-black">پرسش‌های متداول</h3>
          {[
            ['آیا باید کالا را از قبل بخرم؟', 'در مدل دراپ‌شیپینگ نیازی به خرید موجودی اولیه نیست. هزینه پلن و سایر خدمات را هنگام انتخاب بررسی کنید.'],
            ['چه کسی سفارش را ارسال می‌کند؟', 'بر اساس روش تأمین محصول، سفارش از انبار تأمین‌کننده یا انبار مرکزی ارسال می‌شود.'],
            ['سود چه زمانی قابل برداشت است؟', 'پس از تأیید تحویل و پایان دوره نگهداری وجه، مطابق قواعد مالی پلتفرم. وضعیت درآمد در پنل قابل مشاهده است.'],
            ['آیا درآمد تضمین شده است؟', 'خیر؛ فروش به انتخاب محصول، بازاریابی، قیمت‌گذاری و تجربه مشتری بستگی دارد.'],
          ].map(([question, answer]) => <details key={question} className="border-b py-3 text-xs last:border-0"><summary className="cursor-pointer font-bold">{question}</summary><p className="mt-2 leading-7 text-slate-500">{answer}</p></details>)}
        </section>
        <button onClick={start} className="w-full rounded-xl bg-emerald-700 py-4 text-sm font-black text-white">ثبت‌نام و ساخت فروشگاه</button>
      </main>
    </div>
  );
};
