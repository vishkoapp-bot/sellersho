import React from 'react';
import { useStore } from '../../context/StoreContext';
import { X, TrendingUp, DollarSign, Download, Printer } from 'lucide-react';
import { motion } from 'motion/react';

interface Props {
  isOpen: boolean;
  onClose: () => void;
}

export const AdminFullReportModal: React.FC<Props> = ({ isOpen, onClose }) => {
  const { formatPrice, toPersianDigits } = useStore();

  if (!isOpen) return null;

  const monthlyData = [
    { month: 'فروردین', gmv: 125000000, feeProfit: 16500000, orders: 1120 },
    { month: 'اردیبهشت', gmv: 142000000, feeProfit: 18900000, orders: 1340 },
    { month: 'خرداد', gmv: 168000000, feeProfit: 22400000, orders: 1590 },
    { month: 'تیر', gmv: 184500000, feeProfit: 24650000, orders: 1780 },
  ];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 bg-slate-900/60 backdrop-blur-xs select-none">
      <motion.div
        initial={{ opacity: 0, scale: 0.95, y: 12 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        className="bg-white rounded-2xl max-w-[440px] w-full p-4 shadow-2xl border border-slate-100 flex flex-col max-h-[85vh] overflow-hidden space-y-3"
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-blue-50 text-blue-600 flex items-center justify-center">
              <TrendingUp className="w-4 h-4"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">گزارش تفصیلی گردش مالی پلتفرم (GMV)</h3>
              <span className="text-[10px] text-slate-400 block">تفکیک درآمد سهم پلتفرم (F) و حجم سفارشات</span>
            </div>
          </div>
          <button onClick={onClose} className="p-1 rounded-lg text-slate-400 hover:bg-slate-100">
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>

        <div className="overflow-y-auto no-scrollbar space-y-2 text-xs flex-1">
          {monthlyData.map((m, idx) => (
            <div key={idx} className="p-2.5 bg-slate-50 rounded-xl space-y-1.5 border border-slate-200/80">
              <div className="flex justify-between font-bold text-slate-800 text-[11px]">
                <span>عملکرد دوره {m.month} ۱۴۰۵</span>
                <span className="text-blue-600">{toPersianDigits(m.orders)} سفارش</span>
              </div>
              <div className="grid grid-cols-2 gap-2 text-[10px] text-slate-600 pt-1 border-t border-slate-200">
                <div><span>گردش (GMV):</span> <strong className="text-slate-800 mr-1">{formatPrice(m.gmv)} ت</strong></div>
                <div><span>سود پلتفرم:</span> <strong className="text-emerald-700 mr-1">+{formatPrice(m.feeProfit)} ت</strong></div>
              </div>
            </div>
          ))}
        </div>

        <div className="grid grid-cols-2 gap-2 pt-2 border-t border-slate-100">
          <button
            onClick={() => window.print()}
            className="py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-xl flex items-center justify-center gap-1"
          >
            <Printer className="w-3.5 h-3.5"  strokeWidth={1.5}/>
            <span>چاپ گزارش رسمی</span>
          </button>
          <button
            onClick={onClose}
            className="py-2 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl"
          >
            بستن
          </button>
        </div>
      </motion.div>
    </div>
  );
};
