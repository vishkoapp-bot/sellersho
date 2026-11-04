import React from 'react';
import { Order } from '../types';
import { CheckCircle2, PackageCheck, Truck, MapPin } from 'lucide-react';

interface Props {
  status?: Order['status'];
}

export const TrackingTimeline: React.FC<Props> = ({ status = 'processing' }) => {
  const isPaid = true;
  const isPacked = status === 'processing' || status === 'shipped' || status === 'delivered_unconfirmed' || status === 'delivered_confirmed' || status === 'delivered';
  const isShipped = status === 'shipped' || status === 'delivered_unconfirmed' || status === 'delivered_confirmed' || status === 'delivered';
  const isDelivered = status === 'delivered_confirmed' || status === 'delivered';
  const isPendingConfirm = status === 'delivered_unconfirmed';

  const steps = [
    {
      title: 'ثبت و پرداخت سفارش',
      desc: 'سفارش در سیستم ثبت و پرداخت با موفقیت انجام شد',
      icon: CheckCircle2,
      done: isPaid,
    },
    {
      title: 'کنترل کیفیت و بسته‌بندی سفارش',
      desc: isPacked ? 'کنترل فیزیکی انجام شد و بسته با فاکتور و برچسب رسمی پلمب گردید' : 'در نوبت کنترل کیفی و بسته‌بندی',
      icon: PackageCheck,
      done: isPacked,
    },
    {
      title: 'تحویل به ناوگان حمل و نقل',
      desc: isShipped ? 'بارنامه پستی صادر شد و مرسوله در مسیر مقصد است' : 'در انتظار تحویل به مأمور شرکت حمل',
      icon: Truck,
      done: isShipped,
    },
    {
      title: 'تحویل مرسوله به گیرنده',
      desc: isDelivered
        ? 'مرسوله با موفقیت تحویل خریدار گردید و دریافت آن تایید شد'
        : isPendingConfirm
        ? 'بسته به نشانی رسیده است؛ در انتظار تایید نهایی دریافت توسط خریدار'
        : 'تحویل نهایی در نشانی خریدار',
      icon: MapPin,
      done: isDelivered || isPendingConfirm,
    },
  ];

  return (
    <div className="space-y-3 pt-2">
      <h4 className="text-xs font-bold text-slate-800 mb-3">مراحل پردازش و ارسال مرسوله:</h4>
      {steps.map((st, i) => {
        const Icon = st.icon;
        return (
          <div key={i} className="flex items-start gap-3 relative">
            {i < steps.length - 1 && (
              <div
                className={`absolute right-4 top-8 w-0.5 h-8 ${
                  st.done ? 'bg-emerald-500' : 'bg-slate-200'
                }`}
              />
            )}
            <div
              className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 z-10 ${
                st.done ? 'bg-emerald-500 text-white' : 'bg-slate-100 text-slate-400'
              }`}
            >
              <Icon className="w-4 h-4" />
            </div>
            <div>
              <h5 className={`text-xs font-bold ${st.done ? 'text-slate-900' : 'text-slate-400'}`}>
                {st.title}
              </h5>
              <p className="text-[11px] text-slate-500 mt-0.5">{st.desc}</p>
            </div>
          </div>
        );
      })}
    </div>
  );
};
