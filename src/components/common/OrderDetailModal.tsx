import React from 'react';
import { Order } from '../../types';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { maskName, maskPhone, maskAddress } from '../../utils/privacyMasker';
import { X, Package, Truck, Copy, Printer, User, ShieldCheck } from 'lucide-react';
import { motion } from 'motion/react';

interface Props {
  order: Order | null;
  isOpen: boolean;
  onClose: () => void;
  hideSensitiveCustomerInfo?: boolean;
}

export const OrderDetailModal: React.FC<Props> = ({ order, isOpen, onClose, hideSensitiveCustomerInfo = false }) => {
  const { formatPrice, toPersianDigits, setTrackingOrder, setIsTrackingModalOpen } = useStore();
  const { success } = useToast();

  if (!isOpen || !order) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[420px] w-full p-4 shadow-2xl border border-slate-100 flex flex-col max-h-[90vh] overflow-hidden space-y-3"
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center">
              <Package className="w-4 h-4"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">جزئیات کامل سفارش</h3>
              <span className="font-mono text-[10px] text-slate-400 block">{order.orderNumber}</span>
            </div>
          </div>
          <button onClick={onClose} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>

        <div className="overflow-y-auto no-scrollbar space-y-2.5 text-xs flex-1">
          <div className="p-2 bg-slate-50 rounded-xl flex items-center justify-between text-[11px]">
            <div>
              <span className="text-slate-400 text-[10px] block">ویترین:</span>
              <strong className="text-slate-800">{order.storeName}</strong>
            </div>
            <div className="text-left font-mono">
              <span className="text-slate-400 text-[10px] block">{order.createdAt}</span>
              <span className="text-emerald-600 font-bold">پردازش انبار</span>
            </div>
          </div>

          <div className="border border-slate-200 rounded-xl p-2.5 space-y-1 text-[11px]">
            <h4 className="font-bold text-slate-800 flex items-center gap-1">
              <User className="w-3.5 h-3.5 text-brand-deep"  strokeWidth={1.5}/>
              <span>مشخصات تحویل گیرنده:</span>
            </h4>
            <div className="flex justify-between">
              <span>مقصد:</span>
              <strong className="text-slate-800">{order.customerInfo.province}، {order.customerInfo.city}</strong>
            </div>
            {!hideSensitiveCustomerInfo ? (
              <>
                <div className="flex justify-between">
                  <span>نام خریدار:</span>
                  <strong className="text-slate-800">{order.customerInfo.fullName}</strong>
                </div>
                <div className="flex justify-between">
                  <span>شماره تماس:</span>
                  <span className="font-mono" dir="ltr">{toPersianDigits(order.customerInfo.phone)}</span>
                </div>
                <p className="text-slate-700 pt-1 border-t border-slate-100 leading-relaxed">{order.customerInfo.address}</p>
              </>
            ) : (
              <>
                <div className="flex justify-between items-center">
                  <span>نام خریدار:</span>
                  <div className="flex items-center gap-1.5">
                    <strong className="text-slate-800">{maskName(order.customerInfo.fullName)}</strong>
                    <span className="text-[9px] font-bold px-1.5 py-0.2 rounded bg-brand-soft text-brand-deep border border-brand-border">
                      حفظ حریم خصوصی
                    </span>
                  </div>
                </div>
                <div className="flex justify-between">
                  <span>شماره تماس:</span>
                  <span className="font-mono text-slate-500" dir="ltr">{maskPhone(order.customerInfo.phone)}</span>
                </div>
                <p className="text-slate-500 text-[10px] pt-1 border-t border-slate-100 leading-relaxed">
                  {maskAddress(order.customerInfo.address, order.customerInfo.city, order.customerInfo.province)}
                </p>
              </>
            )}
          </div>

          <div className="border border-slate-200 rounded-xl p-2.5 space-y-1">
            <h4 className="font-bold text-slate-800 text-[11px]">اقلام سفارش:</h4>
            <div className="divide-y divide-slate-100">
              {order.items.map((item, idx) => (
                <div key={idx} className="py-1 flex items-center justify-between text-[11px]">
                  <span className="truncate">{item.product.title}</span>
                  <span className="text-brand-deep font-bold shrink-0">{toPersianDigits(item.quantity)} عدد</span>
                </div>
              ))}
            </div>
          </div>

          <div className="p-2.5 bg-brand-soft/50 border border-brand-border rounded-xl space-y-1 text-[11px]">
            <div className="flex justify-between"><span>مبلغ فاکتور:</span><strong>{formatPrice(order.payableAmount)} تومان</strong></div>
            {order.dropshipperProfit > 0 && (
              <div className="flex justify-between text-emerald-700 font-bold">
                <span>سود سلر:</span><span>+{formatPrice(order.dropshipperProfit)} ت</span>
              </div>
            )}
            <div className="flex justify-between text-brand-deep pt-1 font-mono" dir="ltr">
              <span className="font-sans">کد رهگیری:</span>
              <div className="flex items-center gap-1 font-bold">
                <span>{order.trackingCode}</span>
                <button onClick={() => { if (navigator.clipboard) { navigator.clipboard.writeText(order.trackingCode); success('کد کپی شد'); } }} className="p-0.5 text-slate-400">
                  <Copy className="w-3 h-3"  strokeWidth={1.5}/>
                </button>
              </div>
            </div>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2 pt-2 border-t border-slate-100">
          <button
            onClick={() => { setTrackingOrder(order); onClose(); setIsTrackingModalOpen(true); }}
            className="py-2 bg-brand hover:bg-brand-deep text-white font-bold text-[11px] rounded-xl flex items-center justify-center gap-1"
          >
            <Truck className="w-3.5 h-3.5"  strokeWidth={1.5}/><span>رهگیری</span>
          </button>
          <button
            onClick={() => window.print()}
            className="py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-[11px] rounded-xl flex items-center justify-center gap-1"
          >
            <Printer className="w-3.5 h-3.5"  strokeWidth={1.5}/><span>چاپ فاکتور</span>
          </button>
        </div>
      </motion.div>
    </div>
  );
};
