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
  children: ReactNode;
}> = ({ shippingSlot, walletSlot, children }) => {
  return (
    <div className="hidden lg:block lg:sticky lg:top-4 lg:self-start">
      <div className="space-y-4">
        <div className="p-3 bg-brand-soft/70 border border-brand-border/80 rounded-2xl">
          {shippingSlot ?? (
            <>
              <div className="flex items-center gap-1.5 text-brand-deep font-bold">
                <Truck className="w-4 h-4" strokeWidth={1.5} />
                <span>هزینه بسته‌بندی و ارسال: محاسبه در مرحله بعد</span>
              </div>
              <p className="text-[10px] text-slate-500 mt-1 leading-5">
                مبلغ دقیق ارسال هنگام ثبت سفارش، بر اساس وزن مرسوله و مقصد محاسبه و به فاکتور اضافه می‌شود.
              </p>
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
