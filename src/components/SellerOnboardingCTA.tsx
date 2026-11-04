import React from 'react';
import { useStore } from '../context/StoreContext';
import { SellerProfitCalculator } from './SellerProfitCalculator';
import { Store, ArrowLeft, Sparkles, CheckCircle2 } from 'lucide-react';

export const SellerOnboardingCTA: React.FC = () => {
  const { setIsSellerModalOpen } = useStore();

  return (
    <section id="seller-onboarding" className="px-3.5 py-2">
      <div className="relative overflow-hidden rounded-2xl bg-gradient-to-br from-slate-900 via-indigo-950 to-blue-950 p-4 text-white shadow-xl border border-indigo-900/50">
        <div className="absolute top-0 right-0 -mr-16 -mt-16 w-48 h-48 rounded-full bg-blue-600/20 blur-2xl pointer-events-none" />

        <div className="relative z-10 space-y-3">
          <div className="inline-flex items-center gap-1 bg-blue-500/20 border border-blue-400/30 px-2 py-0.5 rounded-full text-[9px] font-bold text-blue-300">
            <Sparkles className="w-3 h-3"  strokeWidth={1.5}/>
            <span>فرصت ویژه همکاری در فروش (دراپ‌شیپینگ)</span>
          </div>

          <h2 className="text-sm font-black leading-snug text-white">
            در فروشگاه اختصاصی خودت بفروش،
            <br />
            <span className="text-transparent bg-clip-text bg-gradient-to-r from-blue-400 via-indigo-300 to-emerald-400">
              بدون نیاز به سرمایه و انبارداری!
            </span>
          </h2>

          <p className="text-[11px] text-slate-300 leading-relaxed">
            محصولات را با سود دلخواه به ویترین اختصاصی خود اضافه کنید؛ ارسال سفارش‌ها از انبار مرکزی انجام شده و سود مستقیماً به حسابتان واریز می‌شود.
          </p>

          <div className="space-y-1 pt-1 text-[10px] text-slate-300">
            <div className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0"  strokeWidth={1.5}/>
              <span>کاتالوگ آماده با هزاران کالای پرفروش روز</span>
            </div>
            <div className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0"  strokeWidth={1.5}/>
              <span>تعیین حاشیه سود دلخواه (۵٪ الی ۳۵٪)</span>
            </div>
            <div className="flex items-center gap-1.5">
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0"  strokeWidth={1.5}/>
              <span>پردازش و بسته‌بندی مکانیزه از انبار مرکزی</span>
            </div>
          </div>

          {/* Profit Calculator */}
          <div className="pt-2">
            <SellerProfitCalculator />
          </div>

          <div className="pt-1">
            <button
              onClick={() => setIsSellerModalOpen(true)}
              className="w-full flex items-center justify-center gap-2 bg-gradient-to-r from-blue-500 to-indigo-600 hover:from-blue-600 hover:to-indigo-700 active:scale-98 text-white font-extrabold text-xs py-2.5 px-4 rounded-xl shadow-md shadow-blue-500/25 transition-all"
            >
              <Store className="w-4 h-4"  strokeWidth={1.5}/>
              <span>ساخت رایگان فروشگاه اختصاصی من</span>
              <ArrowLeft className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            </button>
          </div>
        </div>
      </div>
    </section>
  );
};

