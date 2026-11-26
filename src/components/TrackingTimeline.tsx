import { CheckCircle2, MapPin, PackageCheck, Truck } from 'lucide-react';
import type { Order } from '../types';

const stages = [
  { title: 'ثبت سفارش', icon: CheckCircle2 },
  { title: 'آماده‌سازی و بسته‌بندی', icon: PackageCheck },
  { title: 'ارسال مرسوله', icon: Truck },
  { title: 'تحویل به گیرنده', icon: MapPin },
];

const states: Record<string, { completed: number; text: string }> = {
  pending: { completed: 1, text: 'در انتظار پرداخت' },
  processing: { completed: 1, text: 'در حال آماده‌سازی' },
  picking: { completed: 1, text: 'در حال آماده‌سازی' },
  picked: { completed: 1, text: 'در حال آماده‌سازی' },
  packed: { completed: 2, text: 'آمادهٔ ارسال' },
  shipped: { completed: 3, text: 'در مسیر تحویل' },
  delivered_unconfirmed: { completed: 3, text: 'در انتظار تأیید دریافت' },
  delivered_confirmed: { completed: 4, text: 'تحویل تأیید شد' },
  delivered: { completed: 4, text: 'تحویل شده' },
  delivery_disputed: { completed: 3, text: 'گزارش عدم دریافت در حال بررسی است' },
  cancelled: { completed: 0, text: 'سفارش لغو شده' },
};

export function TrackingTimeline({ status }: { status: Order['status'] }) {
  const state = states[status];
  return (
    <section className="tracking-card" aria-label="مراحل ارسال">
      <div className="tracking-timeline-heading">
        <h3>وضعیت مرسوله</h3>
        <strong role="status">{state?.text ?? 'وضعیت در دست بررسی'}</strong>
      </div>
      {state && status !== 'cancelled' && (
        <ol className="tracking-timeline">
          {stages.map(({ title, icon: Icon }, index) => {
            const reached = index < state.completed;
            return (
              <li key={title} data-reached={reached} aria-current={index === state.completed ? 'step' : undefined}>
                <span><Icon size={17} aria-hidden="true" /></span>
                {title}
              </li>
            );
          })}
        </ol>
      )}
    </section>
  );
}
