import { markedUpPrice } from '../../utils/money';
import React, { useId, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { Calculator, ChevronDown } from 'lucide-react';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';

// "سود من" tab: there is NO store-wide default margin anymore (owner
// rule) — margins exist only per product / per variant, and wherever the
// dropshipper set no percent, the maximum allowed margin applies
// automatically. This tab explains that rule and keeps the trial-only
// calculator (its own local inputs; never saves, never read the removed
// store margin).
export const SellerPricingTab: React.FC = () => {
  const { platformSettings, formatPrice, toPersianDigits } = useStore();

  const baseCostId = useId();
  const profitId = useId();

  // Trial calculator (never saved; own local inputs only).
  const [baseCost, setBaseCost] = useState('800000');
  const [profitInput, setProfitInput] = useState<string>('20');

  const maxProfit = platformSettings.maxProfitMargin ?? 35;

  const numBase = Number(baseCost) || 0;
  const numProfit = Number(profitInput) || 0;

  let finalPriceP = numBase;
  try { finalPriceP = markedUpPrice(numBase, numProfit); } catch { /* incomplete or invalid calculator draft */ }

  const netProfitToman = finalPriceP - numBase;

  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="سود من" description="قبل از تنظیم سود هر کالا، قیمت فروش و سودت را حساب کن." icon={<Calculator />} />
      <section aria-labelledby="seller-price-calculator" className="grid gap-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5 md:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
        <div className="space-y-4">
          <div>
            <h3 id="seller-price-calculator" className="text-base font-extrabold text-[#261b41]">ماشین‌حساب سود</h3>
            <p className="mt-1 text-sm leading-6 text-slate-600">مقدارهای آزمایشی این بخش ذخیره نمی‌شوند.</p>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-1.5">
              <label htmlFor={baseCostId} className="block text-sm font-bold text-slate-800">قیمت خرید (تومان)</label>
              <input id={baseCostId} type="number" inputMode="numeric" dir="ltr" value={baseCost}
                onChange={event => setBaseCost(event.target.value)} placeholder="۸۰۰٬۰۰۰"
                className="min-h-11 w-full rounded-xl border border-slate-200 bg-slate-50 px-3 text-end text-sm font-bold tabular-nums text-slate-900 focus-visible:ring-2 focus-visible:ring-brand" />
            </div>
            <div className="space-y-1.5">
              <label htmlFor={profitId} className="block text-sm font-bold text-slate-800">درصد سود</label>
              <input id={profitId} type="number" inputMode="decimal" dir="ltr" value={profitInput}
                onChange={event => setProfitInput(event.target.value)} placeholder="۲۰"
                className="min-h-11 w-full rounded-xl border border-slate-200 bg-slate-50 px-3 text-end text-sm font-bold tabular-nums text-slate-900 focus-visible:ring-2 focus-visible:ring-brand" />
            </div>
          </div>
        </div>
        <dl aria-live="polite" className="divide-y divide-brand-border rounded-2xl border border-brand-border bg-brand-bg px-4">
          <div className="py-3">
            <dt className="text-sm text-slate-700">قیمت فروش به مشتری</dt>
            <dd className="mt-1 text-lg font-extrabold tabular-nums text-[#261b41]">{formatPrice(finalPriceP)} تومان</dd>
          </div>
          <div className="py-3">
            <dt className="text-sm text-slate-700">سود تو از هر فروش</dt>
            <dd className="mt-1 text-base font-extrabold tabular-nums text-emerald-800">{formatPrice(netProfitToman)} تومان</dd>
          </div>
        </dl>
      </section>
      <section aria-labelledby="seller-price-rules" className="space-y-2 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <h3 id="seller-price-rules" className="text-base font-extrabold text-[#261b41]">تنظیم سود کالاها</h3>
        <p className="text-sm leading-6 text-slate-600">در بخش «انتخاب کالا»، روی «تنظیم سود» همان کالا بزن. سود هر کالا یا تنوع جداگانه تنظیم می‌شود. اگر درصدی تعیین نکنی، حداکثر سود مجاز پلتفرم ({toPersianDigits(maxProfit)}٪) اعمال می‌شود.</p>
        <details className="group">
          <summary className="flex min-h-11 w-fit cursor-pointer list-none items-center gap-1 rounded-xl px-1 text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand [&::-webkit-details-marker]:hidden">
            «حاشیه سود» یعنی چه؟ <ChevronDown aria-hidden="true" className="size-4 group-open:rotate-180" />
          </summary>
          <p className="rounded-xl bg-brand-bg p-3 text-sm leading-6 text-slate-700">حاشیه سود درصدی است که روی قیمت خرید می‌گذاری. کالایی با قیمت خرید ۱۰۰ هزار تومان و سود ۲۰٪، با قیمت ۱۲۰ هزار تومان فروخته می‌شود و ۲۰ هزار تومان آن سود توست.</p>
        </details>
      </section>
    </div>
  );
};
