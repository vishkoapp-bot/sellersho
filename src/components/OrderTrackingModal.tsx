import React, { useEffect, useState } from 'react';
import { useStore } from '../context/StoreContext';
import { useToast } from '../context/ToastContext';
import { TrackingTimeline } from './TrackingTimeline';
import { maskName, maskAddress, ViewerRole } from '../utils/privacyMasker';
import { X, Truck, Clock, CheckCircle2, AlertCircle, ShieldCheck } from 'lucide-react';
import { motion } from 'motion/react';
import { orderApi, OrderShippingInfo } from '../services/orderApi';
import { ReturnWindowBadge } from './ReturnWindowBadge';

interface OrderTrackingModalProps {
  viewerRole?: ViewerRole;
}

export const OrderTrackingModal: React.FC<OrderTrackingModalProps> = ({ viewerRole }) => {
  const { 
    isTrackingModalOpen, 
    setIsTrackingModalOpen, 
    orders, 
    trackingOrder, 
    setTrackingOrder, 
    confirmOrderDelivery,
    disputeOrderDelivery,
    customerUser,
    activeView,
    formatPrice 
  } = useStore();

  const { error: toastError, success: toastSuccess } = useToast();
  const [inputCode, setInputCode] = useState('');
  const [shippingInfo, setShippingInfo] = useState<OrderShippingInfo | null>(null);
  // Team 08 (D64): shipping info display only (Team 03 backend
  // GET /orders/<id>/shipping). Order creation is untouched. On any
  // failure the section simply stays hidden; nothing is fabricated.
  const trackingOrderId = (trackingOrder || (orders.length > 0 ? orders[0] : null))?.id;
  useEffect(() => {
    if (!isTrackingModalOpen || !trackingOrderId) return;
    let cancelled = false;
    setShippingInfo(null);
    orderApi.getOrderShipping(trackingOrderId)
      .then((info) => { if (!cancelled) setShippingInfo(info); })
      .catch(() => { /* shipping section stays hidden */ });
    return () => { cancelled = true; };
  }, [isTrackingModalOpen, trackingOrderId]);

  if (!isTrackingModalOpen) return null;

  const current = trackingOrder || (orders.length > 0 ? orders[0] : null);

  // Determine effective viewer role
  const effectiveRole: ViewerRole = viewerRole || (
    activeView === 'seller_dashboard'
      ? 'seller'
      : customerUser && current && customerUser.fullName === current.customerInfo.fullName
      ? 'customer'
      : 'customer'
  );

  const isMaskedForViewer = effectiveRole === 'seller' || effectiveRole === 'public';
  const displayedRecipient = current
    ? (isMaskedForViewer ? `${maskName(current.customerInfo.fullName)} (${current.customerInfo.city})` : `${current.customerInfo.fullName} (${current.customerInfo.province}، ${current.customerInfo.city})`)
    : '';

  const handleSearch = () => {
    if (!inputCode) return;
    const found = orders.find(
      (o) => o.trackingCode.toLowerCase() === inputCode.trim().toLowerCase()
    );
    if (found) {
      setTrackingOrder(found);
      toastSuccess(`اطلاعات مرسوله ${found.trackingCode} فراخوانی شد.`);
    } else {
      toastError('سفارشی با این کد رهگیری یافت نشد. لطفاً کد را مجدداً بررسی کنید.');
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 md:p-6 bg-slate-900/60 backdrop-blur-sm">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 15 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-3xl max-w-lg w-full overflow-hidden shadow-2xl border border-slate-100 flex flex-col max-h-[90vh]"
      >
        <div className="p-4 bg-slate-900 text-white flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Truck className="w-5 h-5 text-blue-400"  strokeWidth={1.5}/>
            <h3 className="font-extrabold text-sm">پیگیری لحظه‌ای مرسوله</h3>
          </div>
          <button
            onClick={() => setIsTrackingModalOpen(false)}
            className="p-1.5 rounded-full hover:bg-white/20 text-white transition-colors"
          >
            <X className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>

        <div className="p-5 overflow-y-auto no-scrollbar space-y-4">
          <div className="flex gap-2">
            <input
              type="text"
              placeholder="کد پیگیری را وارد کنید (مثلاً TRK-123456)"
              value={inputCode}
              onChange={(e) => setInputCode(e.target.value)}
              className="flex-1 text-xs p-3 rounded-xl bg-slate-50 border border-slate-200 focus:bg-white focus:border-blue-500 font-mono"
              dir="ltr"
            />
            <button
              onClick={handleSearch}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4 rounded-xl text-xs font-bold transition-colors"
            >
              رهگیری
            </button>
          </div>

          {current ? (
            <div className="space-y-4 pt-2">
              <div className="bg-slate-50 rounded-2xl p-3.5 border border-slate-100 space-y-2 text-xs">
                <div className="flex justify-between">
                  <span className="text-slate-400">کد رهگیری:</span>
                  <span className="font-mono font-bold text-blue-700">{current.trackingCode}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-400">فروشگاه:</span>
                  <span className="font-bold text-slate-800">{current.storeName}</span>
                </div>
                <div className="flex justify-between items-center">
                  <span className="text-slate-400">گیرنده مرسوله:</span>
                  <div className="flex items-center gap-1.5">
                    <span className="font-bold text-slate-800">{displayedRecipient}</span>
                    {isMaskedForViewer && (
                      <span className="text-[9px] font-bold px-1.5 py-0.2 rounded-md bg-blue-50 text-blue-700 border border-blue-200" title="نام خریدار جهت حفظ حریم خصوصی ماسک شده است">
                        حریم خصوصی
                      </span>
                    )}
                  </div>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-400">روش پرداخت:</span>
                  <span className="font-bold text-slate-800">
                    'پرداخت نقدی شتاب'
                  </span>
                </div>
                <div className="flex justify-between border-t border-slate-200 pt-2 font-bold">
                  <span className="text-slate-500">مبلغ کل سفارش:</span>
                  <span className="text-slate-900">{formatPrice(current.payableAmount)} تومان</span>
                </div>
              </div>

              <TrackingTimeline status={current.status} />

              {shippingInfo && (
                <div className="bg-slate-50 rounded-2xl p-3.5 border border-slate-100 space-y-2 text-xs">
                  <h4 className="font-bold text-slate-800">اطلاعات ارسال</h4>
                  {shippingInfo.carrier ? <div className="flex justify-between"><span className="text-slate-400">شرکت حمل:</span><span className="font-bold text-slate-800">{shippingInfo.carrier}</span></div> : null}
                  {shippingInfo.trackingNumber ? <div className="flex justify-between"><span className="text-slate-400">شماره مرسوله:</span><span className="font-mono font-bold text-slate-800" dir="ltr">{shippingInfo.trackingNumber}</span></div> : null}
                  {shippingInfo.boxSize ? <div className="flex justify-between"><span className="text-slate-400">اندازه بسته:</span><span className="font-bold text-slate-800">{shippingInfo.boxSize}</span></div> : null}
                  {shippingInfo.weightGrams !== null && shippingInfo.weightGrams !== undefined ? <div className="flex justify-between"><span className="text-slate-400">وزن:</span><span className="font-bold text-slate-800">{shippingInfo.weightGrams} گرم</span></div> : null}
                  {shippingInfo.packedAt ? <div className="flex justify-between"><span className="text-slate-400">زمان بسته‌بندی:</span><span className="font-bold text-slate-800">{shippingInfo.packedAt}</span></div> : null}
                  {shippingInfo.shippedAt ? <div className="flex justify-between"><span className="text-slate-400">زمان ارسال:</span><span className="font-bold text-slate-800">{shippingInfo.shippedAt}</span></div> : null}
                  <div className="flex justify-between items-center">
                    <span className="text-slate-400">مهلت مرجوعی:</span>
                    <ReturnWindowBadge returnWindowEndsAt={shippingInfo.returnWindowEndsAt} />
                  </div>
                </div>
              )}

              {/* Requirement 4: Delivery Confirmation & Dispute Triggers */}
              {(current.status === 'shipped' || current.status === 'delivered_unconfirmed') && (
                <div className="p-3 bg-slate-50 border border-slate-200 rounded-2xl space-y-2.5">
                  <div className="text-center">
                    <span className="text-xs font-bold text-slate-800 block">وضعیت دریافت مرسوله توسط شما:</span>
                    <span className="text-[10px] text-slate-500">
                      جهت تکمیل سفارش و آزادسازی مالی، لطفاً وضعیت بسته را اعلام فرمایید.
                    </span>
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    <button
                      type="button"
                      onClick={() => {
                        confirmOrderDelivery(current.id);
                        toastSuccess(`تحویل مرسوله سفارش ${current.orderNumber} با موفقیت توسط شما تایید گردید و مبلغ در کیف پول سلر آزاد شد.`);
                      }}
                      className="py-2.5 bg-emerald-600 hover:bg-emerald-700 text-white font-bold text-xs rounded-xl shadow-xs flex items-center justify-center gap-1 transition-all"
                    >
                      <CheckCircle2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>کالا را دریافت کردم</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        disputeOrderDelivery(current.id, 'خریدار اعلام کرد بسته پستی هنوز به دست ایشان نرسیده است.');
                        toastSuccess(`گزارش عدم دریافت کالا ثبت شد و سفارش در صف پیگیری قرار گرفت.`);
                      }}
                      className="py-2.5 bg-rose-50 hover:bg-rose-100 text-rose-700 font-bold text-xs rounded-xl border border-rose-200 flex items-center justify-center gap-1 transition-all"
                    >
                      <AlertCircle className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                      <span>هنوز دریافت نکرده‌ام</span>
                    </button>
                  </div>
                </div>
              )}

              {current.status === 'delivery_disputed' && (
                <div className="p-3 bg-amber-50 border border-amber-200 rounded-2xl space-y-1.5 text-xs text-amber-900">
                  <div className="flex items-center gap-1.5 font-bold">
                    <AlertCircle className="w-4 h-4 text-amber-600 shrink-0"  strokeWidth={1.5}/>
                    <span>سفارش در وضعیت بررسی عدم تحویل مرسوله</span>
                  </div>
                  <p className="text-[10px] text-amber-700 leading-relaxed">
                    شما اعلام کرده‌اید که کالا به دستتان نرسیده است. این سفارش تا زمان پاسخ اداره پست و استعلام تیم پشتیبانی معلق شده و مبلغ در حساب دراپ‌شیپر قفل مانده است.
                  </p>
                </div>
              )}

              {current.status === 'delivered_confirmed' && (
                <div className="p-2.5 bg-emerald-50 border border-emerald-200 rounded-xl text-center text-[11px] text-emerald-800 font-bold space-y-0.5">
                  <div>تحویل مرسوله توسط خریدار تأیید شده است {current.confirmedAt ? `(${current.confirmedAt})` : ''}</div>
                  <span className="text-[9px] text-emerald-600 font-normal block">مبلغ سفارش در کیف پول سلر آزاد و دوره مهلت تست ۷ روزه آغاز شد.</span>
                </div>
              )}
            </div>
          ) : (
            <div className="text-center py-8 text-slate-400 space-y-1">
              <Clock className="w-8 h-8 mx-auto text-slate-300"  strokeWidth={1.5}/>
              <p className="text-xs">هنوز سفارشی در این مرورگر ثبت نشده است.</p>
              <p className="text-[11px]">با ثبت اولین سفارش، وضعیت آن را به شکل زنده در اینجا ببینید.</p>
            </div>
          )}
        </div>
      </motion.div>
    </div>
  );
};
