import { shippingExplainer } from './common/helpTexts';
import { HelpExplainer } from './common/HelpExplainer';
import React, { ReactNode } from 'react';
import { Truck } from 'lucide-react';

// Worker A — live parity (index-MERGED.js cart drawer wC, checkout step).
// Live renders this column ONLY on desktop: hidden below lg, sticky at
// lg. Its three blocks, in order: the shipping box (the slot passed in,
// or live's exact "محاسبه در مرحله بعد" fallback), the wallet slot, and
// the summary box holding the same pricing breakdown as the footer.
// The footer breakdown itself is hidden on lg in the checkout step, so
// the totals live here — not twice — on desktop.
export const CheckoutSummaryColumn: React.FC<{
  shippingSlot?: ReactNode;
  walletSlot?: ReactNode;
  // CHECKOUT-02 (D-13/G-07): when no slot is rendered, the fallback
  // must never misstate a loading / warning / error state as
  // "محاسبه در مرحله بعد". The drawer always passes a slot during
  // checkout, so this is defence in depth for the cart step.
  shippingState?: 'idle' | 'loading' | 'quoted' | 'free' | 'warning' | 'error';
  children: ReactNode;
}> = ({ shippingSlot, walletSlot, shippingState, children }) => {
  const fallbackCopy =
    shippingState === 'loading'
      ? 'هزینه بسته‌بندی و ارسال: در حال محاسبه…'
      : shippingState === 'error'
        ? 'هزینه بسته‌بندی و ارسال: محاسبه نشد — ثبت سفارش ممکن نیست'
        : shippingState === 'warning'
          ? 'هزینه بسته‌بندی و ارسال: تکمیل نشانی لازم است'
          : 'هزینه بسته‌بندی و ارسال: محاسبه در مرحله بعد';
  return (
    <div className="hidden lg:block lg:sticky lg:top-4 lg:self-start">
      <div className="space-y-4">
        <div className="p-3 bg-brand-soft/70 border border-brand-border/80 rounded-2xl">
          {shippingSlot ?? (
            <>
              <div className="flex items-center gap-1.5 text-brand-deep font-bold">
                <Truck className="w-4 h-4" strokeWidth={1.5} />
                <span>{fallbackCopy}</span>
                <HelpExplainer text={shippingExplainer} />
              </div>
            </>
          )}
        </div>
        {walletSlot}
        <div className="p-4 bg-slate-50 border border-slate-100 rounded-2xl">
          <div className="space-y-1.5 text-xs">{children}</div>
        </div>
      </div>
    </div>
  );
};
