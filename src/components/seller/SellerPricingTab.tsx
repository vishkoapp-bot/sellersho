import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { Calculator, Sparkles } from 'lucide-react';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { resolveBaseCostR, roundTo100 } from '../../utils/pricingEngine';

// Live parity ("سود من"): the top card is the real store margin — a single
// percent value saved with PUT /stores/:slug/pricing {type: "store_margin",
// mode: "percent"} — while the calculator below it is trial-only and never
// saves. Percent-only: no fixed-amount option exists anywhere on this tab.
// Save lives in a fixed bottom bar; validation and toasts are live's.
export const SellerPricingTab: React.FC = () => {
  const { currentStore, products, platformSettings, formatPrice, toPersianDigits } = useStore();
  const { success, error } = useToast();

  // Store margin (live: input value, placeholder "۲۰").
  const minProfit = Math.max(0, platformSettings.minProfitMargin ?? 5);
  const [storeMargin, setStoreMargin] = useState<string>(
    currentStore.profitMarginPercent > 0
      ? String(currentStore.profitMarginPercent)
      : String(platformSettings.defaultMargin || 20)
  );
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [showMarginHelp, setShowMarginHelp] = useState(false);

  // Trial calculator (never saved).
  const [baseCostR, setBaseCostB] = useState<string>('800000');
  const [profitInput, setProfitInput] = useState<string>('20');

  const numMargin = Number(storeMargin);
  const marginInRange = storeMargin !== '' && !isNaN(numMargin) && numMargin >= 0 && numMargin <= 100;
  const belowMin = marginInRange && numMargin < minProfit;
  const isLocked = !!currentStore.isMarginLockedByAdmin;

  const numBase = Number(baseCostR) || 0;
  const numProfit = Number(profitInput) || 0;

  const finalPriceP = roundTo100(numBase * (1 + numProfit / 100));

  const netProfitToman = finalPriceP - numBase;

  // Live's worked example uses a real product's platform buy price when one
  // exists; without any priced product the example is omitted, not invented.
  const sampleBase = products.map((p) => resolveBaseCostR(p, platformSettings)).find((b) => isFinite(b) && b > 0);
  const exampleFinal = sampleBase !== undefined ? roundTo100(sampleBase * (1 + numMargin / 100)) : undefined;
  const exampleProfit = sampleBase !== undefined && exampleFinal !== undefined ? exampleFinal - sampleBase : undefined;

  const saveStoreMargin = async () => {
    if (!marginInRange) {
      error('درصد سود باید بین ۰ تا ۱۰۰ باشد.');
      return;
    }
    if (belowMin) {
      error(`پلتفرم اجازه نمی‌دهد سودت کمتر از ${toPersianDigits(minProfit)}٪ باشد.`);
      return;
    }
    if (numMargin > (platformSettings.maxProfitMargin || 35)) {
      error(`پلتفرم اجازه نمی‌دهد سودت بیشتر از ${toPersianDigits(platformSettings.maxProfitMargin || 35)}٪ باشد.`);
      return;
    }
    if (isLocked) return;
    if (isMockMode()) {
      setSaved(true);
      success(`سود فروشگاهت ${toPersianDigits(numMargin)}٪ ذخیره شد.`);
      return;
    }
    setSaving(true);
    try {
      await storeApi.updateStorePricing(currentStore.slug, { type: 'store_margin', value: numMargin, mode: 'percent' });
      setSaved(true);
      success(`سود فروشگاهت ${toPersianDigits(numMargin)}٪ ذخیره شد.`);
    } catch (err: any) {
      setSaved(false);
      error(err?.message || 'ذخیره سود انجام نشد. دوباره تلاش کن.');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-3.5 pb-24">
      {/* 1. Intro + "what is margin" help (live's Pe block). */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <p className="text-[11px] text-slate-600 leading-6">
          این‌جا مشخص می‌کنی از هر فروشی چقدر سود کنی. این درصد روی قیمت همه کالاهای فروشگاهت می‌آید.
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

      {/* 2. Real store margin card (live). */}
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-3">
        <div className="flex items-center gap-1.5 text-blue-600">
          <Calculator className="w-4 h-4" strokeWidth={1.5} />
          <h3 className="text-xs font-bold text-slate-800">سود فروشگاهت (درصد)</h3>
        </div>
        {isLocked && (
          <p className="rounded-xl bg-amber-50 border border-amber-200 p-2.5 text-[11px] font-bold text-amber-800">
            قیمت‌گذاری این فروشگاه از طرف مدیریت قفل شده است؛ تغییر سود در حال حاضر ممکن نیست.
          </p>
        )}
        <div className="relative flex items-center bg-slate-50 border border-slate-200 rounded-xl px-3 focus-within:bg-white focus-within:border-blue-500 min-h-[52px]">
          <input
            type="number"
            min={0}
            max={100}
            value={storeMargin}
            disabled={isLocked}
            onChange={(e) => { setStoreMargin(e.target.value); setSaved(false); }}
            className="w-full bg-transparent text-base font-extrabold text-emerald-600 focus:outline-hidden py-3"
            placeholder="۲۰"
            aria-label="درصد سود فروشگاه"
          />
          <span className="text-xs text-slate-400 font-bold mr-1 shrink-0">درصد سود روی قیمت کالا</span>
        </div>
        {belowMin && (
          <p className="text-[11px] leading-5 text-rose-600">
            پلتفرم اجازه نمی‌دهد سودت کمتر از {toPersianDigits(minProfit)}٪ باشد.
          </p>
        )}
        {sampleBase !== undefined && marginInRange && (
          <p className="text-[11px] leading-6 text-slate-500">
            مثال با سود {toPersianDigits(numMargin)}٪: کالایی که <strong className="text-slate-700">قیمت خریدش از پلتفرم {formatPrice(sampleBase)} تومان</strong> است، برای مشتری{' '}
            <strong className="text-slate-700">{formatPrice(exampleFinal as number)} تومان</strong> می‌شود و{' '}
            <strong className="text-emerald-700">{formatPrice(exampleProfit as number)} تومان</strong> آن سود توست.
          </p>
        )}
        <p className="text-[10px] text-slate-400">حداقل سود مجاز پلتفرم: {toPersianDigits(minProfit)}٪</p>
      </div>

      {/* 3. Trial calculator (live: "فقط برای امتحان است و چیزی را ذخیره نمی‌کند"). */}
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

      {/* Live's fixed bottom save bar. */}
      <div className="fixed bottom-0 inset-x-0 z-30 px-3.5 pb-3 pointer-events-none">
        <div className="max-w-[504px] mx-auto pointer-events-auto">
          <button
            type="button"
            onClick={saveStoreMargin}
            disabled={saving || !marginInRange || isLocked}
            className={`w-full min-h-[52px] py-3 font-extrabold text-sm rounded-2xl shadow-lg flex items-center justify-center gap-1.5 transition-all disabled:opacity-50 ${saved ? 'bg-emerald-600 text-white' : 'bg-blue-700 text-white'}`}
          >
            {saved ? (
              <span>ذخیره شد ✓</span>
            ) : saving ? (
              <span>در حال ذخیره…</span>
            ) : (
              <span>ذخیره سود {marginInRange ? `${toPersianDigits(numMargin)}٪` : ''} برای همه کالاها</span>
            )}
          </button>
        </div>
      </div>
    </div>
  );
};
