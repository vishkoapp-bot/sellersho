import React, { ReactNode } from 'react';
import { ReceiptText, Truck } from 'lucide-react';

export const CheckoutSummaryColumn: React.FC<{
  shippingSlot?: ReactNode;
  children: ReactNode;
}> = ({ shippingSlot, children }) => (
  <aside className="space-y-4 lg:sticky lg:top-0 lg:self-start" aria-label="جزئیات سفارش">
    <section className="space-y-3 rounded-[var(--checkout-radius)] border border-[var(--checkout-line)] bg-white p-4 sm:p-5" aria-labelledby="checkout-shipping-details">
      <h4 id="checkout-shipping-details" className="flex items-center gap-2.5 text-base font-extrabold">
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-[var(--checkout-soft)] text-[var(--checkout-accent)]"><Truck className="size-4" aria-hidden="true" /></span>
        ارسال سفارش
      </h4>
      {shippingSlot}
    </section>
    <section className="space-y-3 rounded-[var(--checkout-radius)] border border-[var(--checkout-line)] bg-white p-4 sm:p-5" aria-labelledby="checkout-cost-details">
      <h4 id="checkout-cost-details" className="flex items-center gap-2.5 text-base font-extrabold">
        <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-[var(--checkout-soft)] text-[var(--checkout-accent)]"><ReceiptText className="size-4" aria-hidden="true" /></span>
        جزئیات مبلغ
      </h4>
      <div className="space-y-2.5 text-sm">{children}</div>
    </section>
  </aside>
);
