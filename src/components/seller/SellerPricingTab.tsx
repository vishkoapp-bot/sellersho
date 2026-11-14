import { markedUpPrice } from '../../utils/money';
import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { Calculator, Sparkles } from 'lucide-react';

// "سود من" tab: there is NO store-wide default margin anymore (owner
// rule) — margins exist only per product / per variant, and wherever the
// dropshipper set no percent, the maximum allowed margin applies
// automatically. This tab explains that rule and keeps the trial-only
// calculator (its own local inputs; never saves, never read the removed
// store margin).
export const SellerPricingTab: React.FC = () => {
  const { platformSettings, formatPrice, toPersianDigits } = useStore();

  const [showMarginHelp, setShowMarginHelp] = useState(false);

  // Trial calculator (never saved; own local inputs only).
  const [baseCostR, setBaseCostB] = useState<string>('800000');
  const [profitInput, setProfitInput] = useState<string>('20');

  const maxProfit = platformSettings.maxProfitMargin ?? 35;

  const numBase = Number(baseCostR) || 0;
  const numProfit = Number(profitInput) || 0;

  let finalPriceP = numBase;
  try { finalPriceP = markedUpPrice(numBase, numProfit); } catch { /* incomplete or invalid calculator draft */ }

  const netProfitToman = finalPriceP - numBase;

  return (
    <div className="space-y-3.5 pb-6">
      {/* 1. Intro + "what is margin" help (live's Pe block). */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <p className="text-[11px] text-slate-600 leading-6">
          سود فروشگاه دیگر یک درصد سراسری ندارد؛ برای هر کالا یا هر تنوع، درصد سود همان کالا را جداگانه تعیین می‌کنی. هرجا درصدی تعیین نکرده باشی، به‌صورت خودکار حداکثر سود مجاز پلتفرم ({toPersianDigits(maxProfit)}٪) اعمال می‌شود.
        </p>
        <button
          type="button"
          onClick={() => setShowMarginHelp((v) => !v)}
          aria-expanded={showMarginHelp}
          className="text-[11px] font-bold text-blue-700"
        >
          «حاشیه سود» یعنی چه؟
        </button>
        {showMarginHelp && (
          <div className="space-y-1 text-[11px] leading-6 text-slate-600">
            <p>حاشیه سود یعنی درصدی که روی قیمت کالا می‌گذاری و سود تو می‌شود.</p>
            <p>کالای ۱۰۰ هزار تومانی با سود ۲۰٪، برای مشتری ۱۲۰ هزار تومان می‌شود و ۲۰ هزار تومانش سود توست.</p>
          </div>
        )}
      </div>

      {/* 2. Trial calculator (live: "فقط برای امتحان است و چیزی را ذخیره نمی‌کند"). */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3 text-xs">
        <div className="flex items-center gap-1.5 text-slate-500">
          <Calculator className="w-4 h-4" strokeWidth={1.5} />
          <h3 className="text-xs font-bold text-slate-700">امتحان کن: با یک کالای فرضی حساب کن</h3>
        </div>
        <p className="text-[11px] text-slate-400 leading-5">این بخش فقط برای امتحان است و چیزی را ذخیره نمی‌کند.</p>

        <div>
          <label className="text-xs font-bold text-slate-700 block mb-1">
            قیمت خرید کالا از پلتفرم (تومان):
          </label>
          <input
            type="number"
            value={baseCostR}
            onChange={(e) => setBaseCostB(e.target.value)}
            className="w-full text-[13px] p-3 rounded-xl bg-slate-50 border border-slate-200 font-bold focus:outline-hidden focus:bg-white focus:border-blue-500"
            placeholder="مثال: ۸۰۰٬۰۰۰"
          />
        </div>

        <div>

          <div>
            <label className="text-[11px] font-bold text-slate-700 block mb-1">مقدار سود در ماشین‌حساب</label>
            <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-3 py-2 focus-within:bg-white focus-within:border-blue-500">
              <input
                type="number"
                value={profitInput}
                onChange={(e) => setProfitInput(e.target.value)}
                className="w-full bg-transparent text-xs font-bold text-emerald-600 font-mono focus:outline-hidden"
                placeholder='۲۰'
              />
              <span className="text-[10px] text-slate-400 font-bold mr-1 shrink-0">
                درصد سود
              </span>
            </div>
          </div>
        </div>

        <div className="p-3.5 bg-gradient-to-r from-blue-50/70 to-emerald-50/70 border border-blue-200/80 rounded-2xl space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-[11px] font-bold text-slate-700">قیمتی که مشتری می‌پردازد:</span>
            <span className="text-base font-black text-blue-800 font-mono">
              {formatPrice(finalPriceP)} تومان
            </span>
          </div>
          <div className="pt-2 border-t border-slate-200/70 text-[11px]">
            <span className="text-slate-500 text-[10px] block">سود تو از هر فروش:</span>
            <strong className="text-emerald-700 font-mono">+{formatPrice(netProfitToman)} تومان</strong>
          </div>
        </div>
      </div>

      <div className="p-3 bg-amber-50/60 border border-amber-200/70 rounded-2xl text-[10px] text-amber-800 flex items-start gap-2">
        <Sparkles className="w-4 h-4 text-amber-600 shrink-0 mt-0.5" strokeWidth={1.5} />
        <p className="leading-relaxed">
          می‌خواهی سود یک کالای خاص فرق کند؟ در بخش «انتخاب کالا» روی دکمه «سود» همان کالا بزن.
        </p>
      </div>

    </div>
  );
};
