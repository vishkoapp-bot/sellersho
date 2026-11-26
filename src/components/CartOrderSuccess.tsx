import { HelpExplainer } from './common/HelpExplainer';
import React from 'react';
import { useStore } from '../context/StoreContext';
import { useToast } from '../context/ToastContext';
import { Order } from '../types';
import { computeOrderBreakdown } from '../utils/cartPricing';
import { CheckCircle2, Truck, Copy, Printer, ReceiptText } from 'lucide-react';

interface CartOrderSuccessProps {
  order: Order;
  recovered?: boolean;
  onClose: () => void;
}

export const CartOrderSuccess: React.FC<CartOrderSuccessProps> = ({ order, onClose, recovered = false }) => {
  const { setIsTrackingModalOpen, setActiveView, formatPrice } = useStore();
  // CHECKOUT-03: the same five-row breakdown as cart / checkout, derived
  // from the server snapshot only (never from catalog prices). When the
  // snapshot is incoherent (legacy rows), breakdown is null and the
  // previous single-total display is kept instead of inventing numbers.
  const breakdown = computeOrderBreakdown(order);
  const { success, error } = useToast();

  const copyTracking = () => {
    if (!order.trackingCode || !navigator.clipboard) {
      error('کپی خودکار در دسترس نیست. می‌توانید کد را دستی کپی کنید.');
      return;
    }
    navigator.clipboard.writeText(order.trackingCode)
      .then(() => success(`کد پیگیری ${order.trackingCode} در حافظه کپی شد.`))
      .catch(() => error('کپی کد پیگیری ممکن نشد؛ لطفاً کد را به‌صورت دستی یادداشت کنید.'));
  };

  const handlePrint = () => {
    window.print();
  };

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <section className="rounded-2xl border border-slate-200 bg-white p-4 sm:p-6">
        <div className="flex items-center gap-3">
          <div className="grid size-12 shrink-0 place-items-center rounded-xl bg-[var(--checkout-soft,_#ecfdf5)] text-[var(--checkout-accent,_#047857)]">
            <CheckCircle2 className="size-6" aria-hidden="true" />
          </div>
          <div>
            <h3 className="text-xl font-black text-[var(--checkout-ink,_#172d26)]">{recovered ? 'سفارش قبلی بازیابی شد' : 'سفارش شما ثبت شد'}</h3>
            {order.status === 'pending' && <div className="mt-1 flex items-center gap-1 text-sm font-semibold text-amber-900"><span>در انتظار پرداخت</span>
              <HelpExplainer text={`- نحوه پرداخت این سفارش از طرف فروشگاه به شما اعلام می‌شود. هنگام پیگیری، شماره سفارش ${order.orderNumber} را اعلام کنید تا پرداخت شما دقیقاً برای همین سفارش ثبت شود.
- وضعیت پرداخت را در بخش «سفارش‌های من» دنبال کنید؛ به‌محض این‌که تیم مالی دریافت وجه را ثبت کند، وضعیت سفارش به «پرداخت شد» تغییر می‌کند و سفارش وارد صف ارسال می‌شود. اگر منصرف شدید، سفارش پرداخت‌نشده را از همان بخش می‌توانید لغو کنید.`} />
            </div>}
          </div>
        </div>
        <p className="mt-4 text-sm leading-7 text-slate-700">
          {recovered
            ? 'رسید زیر برای سفارش قبلی شماست. برای ثبت سفارش دیگری، بعد از بررسی این رسید خرید تازه‌ای شروع کنید.'
            : order.status === 'pending'
            ? 'نحوهٔ پرداخت از طرف فروشگاه اعلام می‌شود. وضعیت سفارش و پرداخت را در «سفارش‌های من» دنبال کنید.'
            : 'جزئیات و آخرین وضعیت ارسال سفارش را در «سفارش‌های من» ببینید.'}
        </p>
        <button type="button" onClick={() => { onClose(); setActiveView('customer_orders'); }}
          className="no-print mt-4 inline-flex min-h-12 w-full items-center justify-center gap-2 rounded-xl bg-[var(--checkout-accent,_#4a33d8)] px-4 text-sm font-bold text-white hover:brightness-90 sm:w-auto">
          <ReceiptText className="size-4" aria-hidden="true" />
          مشاهدهٔ سفارش‌های من
        </button>
      </section>

      <section aria-labelledby="checkout-receipt-details" className="space-y-3 rounded-2xl border border-slate-200 bg-white p-4 sm:p-6">
        <h4 id="checkout-receipt-details" className="text-base font-black">جزئیات رسید</h4>
        <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
          <span className="text-slate-600">شماره سفارش</span>
          <bdi className="select-text font-bold text-slate-800">{order.orderNumber}</bdi>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
          <span className="text-slate-600">کد رهگیری پستی</span>
          {order.trackingCode ? (
            <div className="flex items-center gap-1 font-bold text-[var(--checkout-accent,_#4a33d8)]">
              <bdi className="select-text">{order.trackingCode}</bdi>
              <button type="button" onClick={copyTracking} aria-label="کپی کد رهگیری پستی" className="no-print grid size-11 place-items-center rounded-xl hover:bg-slate-100">
                <Copy className="size-4" aria-hidden="true" />
              </button>
            </div>
          ) : <span className="text-slate-600">هنوز صادر نشده</span>}
        </div>
        {breakdown ? (
          <>
            <div className="flex justify-between items-center text-sm">
              <span className="text-slate-600">قیمت کالاها:</span>
              <span className="font-bold text-slate-800">{formatPrice(breakdown.merchandiseOriginal)} تومان</span>
            </div>
            {breakdown.merchandiseDiscount > 0 && (
              <div className="flex justify-between items-center text-sm">
                <span className="text-rose-600 font-bold">تخفیف ویژه:</span>
                <span className="font-bold text-rose-600">−{formatPrice(breakdown.merchandiseDiscount)} تومان</span>
              </div>
            )}
            {breakdown.addonsTotal > 0 && (
              <div className="flex justify-between items-center text-sm">
                <span className="text-slate-600">محصولات مکمل:</span>
                <span className="font-bold text-slate-800">{formatPrice(breakdown.addonsTotal)} تومان</span>
              </div>
            )}
          </>
        ) : (
          <div className="flex justify-between items-center text-sm">
            <span className="text-slate-600">جمع اقلام:</span>
            <span className="font-bold text-slate-800">{formatPrice(Math.max(0, order.payableAmount - (order.shippingCost || 0)))} تومان</span>
          </div>
        )}
        <div className="flex justify-between items-center text-sm">
          <span className="text-slate-600">هزینه ارسال:</span>
          <span className="font-bold text-slate-800">{`${formatPrice(order.shippingCost || 0)} تومان`}</span>
        </div>
        <div className="flex justify-between items-center text-sm">
          <span className="text-slate-600">روش پرداخت:</span>
          <span className="font-bold text-slate-800">پرداخت پس از ثبت سفارش</span>
        </div>
        <div className="flex justify-between items-center text-sm">
          <span className="text-slate-600">مبلغ قابل پرداخت:</span>
          <span className="font-bold text-slate-800">{formatPrice(order.payableAmount)} تومان</span>
        </div>
        <div className="flex justify-between items-center text-xs border-t border-slate-200 pt-2">
          <span className="text-slate-600">فروشگاه:</span>
          <span className="font-bold text-slate-700">{order.storeName || '—'}</span>
        </div>
      </section>

      <div className="no-print flex flex-wrap items-center gap-2">
        <button type="button" onClick={handlePrint} className="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-slate-300 bg-white px-3 text-sm font-bold hover:bg-slate-100">
          <Printer className="size-4" aria-hidden="true" />
          چاپ رسید سفارش
        </button>
        {order.trackingCode && (
          <button type="button" onClick={() => { onClose(); setIsTrackingModalOpen(true); }}
            className="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border border-slate-300 bg-white px-3 text-sm font-bold hover:bg-slate-100">
            <Truck className="size-4" aria-hidden="true" />
            رهگیری مرسوله
          </button>
        )}
        <button type="button" onClick={onClose} className="min-h-11 px-3 text-sm font-bold text-slate-700 hover:underline">
          بازگشت به فروشگاه
        </button>
      </div>
    </div>
  );
};
