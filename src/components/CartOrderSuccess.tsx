import { HelpExplainer } from './common/HelpExplainer';
import React from 'react';
import { useStore } from '../context/StoreContext';
import { useToast } from '../context/ToastContext';
import { Order } from '../types';
import { computeOrderBreakdown } from '../utils/cartPricing';
import { CheckCircle2, Truck, Copy, ArrowRight, Printer, Receipt } from 'lucide-react';

interface CartOrderSuccessProps {
  order: Order;
  onClose: () => void;
}

export const CartOrderSuccess: React.FC<CartOrderSuccessProps> = ({ order, onClose }) => {
  const { setIsTrackingModalOpen, setActiveView, formatPrice } = useStore();
  // CHECKOUT-03: the same five-row breakdown as cart / checkout, derived
  // from the server snapshot only (never from catalog prices). When the
  // snapshot is incoherent (legacy rows), breakdown is null and the
  // previous single-total display is kept instead of inventing numbers.
  const breakdown = computeOrderBreakdown(order);
  const { success, error } = useToast();

  const copyTracking = () => {
    if (!order.trackingCode) {
      error('کد رهگیری هنوز صادر نشده است.');
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
    <div className="text-center py-6 space-y-4 lg:max-w-xl lg:mx-auto">
      <div className="w-16 h-16 rounded-full bg-emerald-100 text-emerald-600 flex items-center justify-center mx-auto shadow-md">
        <CheckCircle2 className="w-8 h-8"  strokeWidth={1.5}/>
      </div>

      <div>
        <h3 className="text-base font-extrabold text-slate-900">سفارش شما با موفقیت ثبت شد!</h3>
        <p className="text-xs text-amber-700 mt-1 font-bold">
          سفارش ثبت شد، اما هنوز پرداختی انجام نشده است؛ سفارش در وضعیت «در انتظار پرداخت» است و پس از ثبت پرداخت، آماده ارسال می‌شود.
        </p>
      </div>

      <div className="bg-amber-50 border border-amber-200 rounded-2xl p-4 text-right space-y-2">
        <div className="flex items-center gap-1.5 text-amber-800">
          <span className="text-xs font-extrabold">برای تکمیل پرداخت چه کار کنید؟</span>
          <HelpExplainer text={`- نحوه پرداخت این سفارش از طرف فروشگاه به شما اعلام می‌شود. هنگام پیگیری، شماره سفارش ${order.orderNumber} را اعلام کنید تا پرداخت شما دقیقاً برای همین سفارش ثبت شود.
- وضعیت پرداخت را در بخش «سفارش‌های من» دنبال کنید؛ به‌محض این‌که تیم مالی دریافت وجه را ثبت کند، وضعیت سفارش به «پرداخت شد» تغییر می‌کند و سفارش وارد صف ارسال می‌شود. اگر منصرف شدید، سفارش پرداخت‌نشده را از همان بخش می‌توانید لغو کنید.`} />
        </div>
        <button
          onClick={() => {
            onClose();
            setActiveView('customer_orders');
          }}
          className="w-full flex items-center justify-center gap-2 bg-white hover:bg-amber-100 text-amber-800 font-bold text-xs py-2.5 rounded-xl transition-colors border border-amber-200"
        >
          <span>مشاهده سفارش‌های من</span>
        </button>
      </div>

      {/* Tracking Card */}
      <div className="bg-slate-50 border border-slate-200 rounded-2xl p-4 text-right space-y-2">
        <div className="flex justify-between items-center text-xs">
          <span className="text-slate-500">شماره سفارش:</span>
          <span className="font-mono font-bold text-slate-800">{order.orderNumber}</span>
        </div>
        <div className="flex justify-between items-center text-xs">
          <span className="text-slate-500">کد رهگیری پستی:</span>
          <div className="flex items-center gap-1 font-mono font-bold text-brand-deep">
            <span>{order.trackingCode || '—'}</span>
            <button onClick={copyTracking} className="p-1 hover:bg-slate-200 rounded text-slate-400">
              <Copy className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            </button>
          </div>
        </div>
        {breakdown ? (
          <>
            <div className="flex justify-between items-center text-xs">
              <span className="text-slate-500">قیمت کالاها:</span>
              <span className="font-bold text-slate-800">{formatPrice(breakdown.merchandiseOriginal)} تومان</span>
            </div>
            {breakdown.merchandiseDiscount > 0 && (
              <div className="flex justify-between items-center text-xs">
                <span className="text-rose-600 font-bold">تخفیف ویژه:</span>
                <span className="font-bold text-rose-600">−{formatPrice(breakdown.merchandiseDiscount)} تومان</span>
              </div>
            )}
            {breakdown.addonsTotal > 0 && (
              <div className="flex justify-between items-center text-xs">
                <span className="text-slate-500">محصولات مکمل:</span>
                <span className="font-bold text-slate-800">{formatPrice(breakdown.addonsTotal)} تومان</span>
              </div>
            )}
          </>
        ) : (
          <div className="flex justify-between items-center text-xs">
            <span className="text-slate-500">جمع اقلام:</span>
            <span className="font-bold text-slate-800">{formatPrice(Math.max(0, order.payableAmount - (order.shippingCost || 0)))} تومان</span>
          </div>
        )}
        <div className="flex justify-between items-center text-xs">
          <span className="text-slate-500">هزینه ارسال:</span>
          <span className="font-bold text-slate-800">{`${formatPrice(order.shippingCost || 0)} تومان`}</span>
        </div>
        <div className="flex justify-between items-center text-xs">
          <span className="text-slate-500">روش پرداخت:</span>
          <span className="font-bold text-slate-800">پرداخت پس از ثبت سفارش</span>
        </div>
        <div className="flex justify-between items-center text-xs">
          <span className="text-slate-500">مبلغ قابل پرداخت:</span>
          <span className="font-bold text-slate-800">{formatPrice(order.payableAmount)} تومان</span>
        </div>
        <div className="flex justify-between items-center text-xs border-t border-slate-200 pt-2">
          <span className="text-slate-500">فروشگاه:</span>
          <span className="font-bold text-slate-700">{order.storeName || '—'}</span>
        </div>
      </div>

      <div className="space-y-2 pt-2 no-print">
        <button
          onClick={handlePrint}
          className="w-full flex items-center justify-center gap-2 bg-slate-100 hover:bg-slate-200 text-slate-800 font-bold text-xs py-2.5 rounded-xl transition-all border border-slate-200"
        >
          <Printer className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>
          <span>چاپ فاکتور رسمی خرید</span>
        </button>

        <button
          onClick={handlePrint}
          className="w-full flex items-center justify-center gap-2 bg-slate-100 hover:bg-slate-200 text-slate-800 font-bold text-xs py-2.5 rounded-xl transition-all border border-slate-200"
        >
          <Receipt className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>
          <span>چاپ رسید پرداخت</span>
        </button>

        <button
          onClick={() => {
            onClose();
            setIsTrackingModalOpen(true);
          }}
          className="w-full flex items-center justify-center gap-2 bg-brand hover:bg-brand-deep text-white font-bold text-xs py-3 rounded-xl transition-all shadow-xs"
        >
          <Truck className="w-4 h-4"  strokeWidth={1.5}/>
          <span>رهگیری و مشاهده وضعیت مرسوله</span>
        </button>

        <button
          onClick={onClose}
          className="w-full text-xs font-bold text-slate-600 hover:bg-slate-100 py-2.5 rounded-xl transition-colors"
        >
          بازگشت به فروشگاه
        </button>
      </div>
    </div>
  );
};
