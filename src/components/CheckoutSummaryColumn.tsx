import React, { ReactNode } from 'react';
import { ReceiptText, Truck } from 'lucide-react';

export const CheckoutSummaryColumn: React.FC<{
  shippingSlot?: ReactNode;
  children: ReactNode;
}> = ({ shippingSlot, children }) => (
  <aside className="space-y-4 lg:sticky lg:top-0 lg:self-start" aria-label="جزئیات سفارش">
    <section className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4" aria-labelledby="checkout-shipping-details">
      <h4 id="checkout-shipping-details" className="flex items-center gap-2 font-extrabold">
        <Truck className="size-4 text-[var(--checkout-accent)]" aria-hidden="true" />
        ارسال سفارش
      </h4>
      {shippingSlot}
    </section>
    <section className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4" aria-labelledby="checkout-cost-details">
      <h4 id="checkout-cost-details" className="flex items-center gap-2 font-extrabold">
        <ReceiptText className="size-4 text-[var(--checkout-accent)]" aria-hidden="true" />
        جزئیات مبلغ
      </h4>
      <div className="space-y-2 text-sm">{children}</div>
    </section>
  </aside>
);
