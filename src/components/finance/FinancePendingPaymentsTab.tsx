import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { financeOperation } from '../../services/financeOperation';

import { ApiError } from '../../services/apiClient';
import { financeApi, type PendingPaymentOrder } from '../../services/financeApi';
import { toPersianDigits } from '../../utils/formatters';

const PendingPaymentsList: React.FC = () => {
  const { adminSession, formatPrice } = useStore();
  const canConfirm = adminSession?.role === 'admin' || adminSession?.role === 'super_admin';
  const [orders, setOrders] = useState<PendingPaymentOrder[]>([]);
  const [request, setRequest] = useState<{ cursor?: string }>({});
  const [nextCursor, setNextCursor] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<'unavailable' | 'failed'>();

  useEffect(() => {
    let cancelled = false;
    financeApi.getPendingPayments(request.cursor)
      .then((page) => {
        if (cancelled) return;
        setOrders(current => request.cursor ? [...current, ...page.orders] : page.orders);
        setNextCursor(page.hasMore ? page.nextCursor : undefined);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof ApiError && err.status === 404 ? 'unavailable' : 'failed');
        setLoading(false);
      });
    return () => { cancelled = true; };
  }, [request]);

  const load = (cursor?: string) => {
    setError(undefined);
    setLoading(true);
    setRequest({ cursor });
  };

  return (
    <section aria-labelledby="unpaid-order-heading" className="bg-white border border-slate-200/90 rounded-2xl shadow-2xs p-4 space-y-3">
      <h3 id="unpaid-order-heading" className="text-sm font-bold text-slate-900">سفارش‌های پرداخت‌نشده</h3>
      {loading && <p role="status" className="text-xs text-slate-500">در حال بارگذاری سفارش‌ها…</p>}
      {error === 'unavailable' && <p role="status" className="text-xs text-slate-500">فهرست سفارش‌های پرداخت‌نشده هنوز در دسترس نیست.</p>}
      {error === 'failed' && (
        <p role="alert" className="text-xs text-rose-700">
          سفارش‌های پرداخت‌نشده بارگذاری نشد.
          <button type="button" className="mr-2 underline" onClick={() => load(request.cursor)}>تلاش دوباره</button>
        </p>
      )}
      {!loading && !error && orders.length === 0 && <p className="text-xs text-slate-500">سفارش پرداخت‌نشده‌ای نیست.</p>}
      {orders.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-right text-xs">
            <thead>
              <tr className="border-b border-slate-200 text-slate-500">
                <th scope="col" className="p-2">شماره سفارش</th>
                <th scope="col" className="p-2">نام مشتری</th>
                <th scope="col" className="p-2">مبلغ (تومان)</th>
                <th scope="col" className="p-2">زمان انتظار</th>
                {canConfirm && <th scope="col" className="p-2">تأیید پرداخت</th>}
              </tr>
            </thead>
            <tbody>
              {orders.map(order => (
                <tr key={order.id} className="border-b border-slate-100 align-top">
                  <td className="p-2"><bdi>{order.orderNumber || order.id}</bdi></td>
                  <td className="p-2">{order.customerName || '—'}</td>
                  <td className="p-2">{formatPrice(order.amountToman)}</td>
                  <td className="p-2">{toPersianDigits(order.ageHours)} ساعت</td>
                  {canConfirm && (
                    <td className="p-2">
                      <details>
                        <summary className="cursor-pointer text-teal-700">تأیید دستی پرداخت</summary>
                        <form data-operation-id={`admin-confirm-manual:${order.id}`} onSubmit={event => event.preventDefault()} className="mt-2 space-y-2">
                          <p className="text-slate-500">نوع مدرک موردنیاز هنوز از طرف مالک مشخص نشده است.</p>
                          <fieldset disabled className="space-y-2">
                            <label className="block">
                              نوع مدرک
                              <input type="text" className="block w-full rounded border border-slate-200 bg-slate-50" />
                            </label>
                            <label className="block">
                              مرجع مدرک
                              <input type="text" className="block w-full rounded border border-slate-200 bg-slate-50" />
                            </label>
                            <button type="submit" disabled className="rounded px-2 py-1 bg-slate-100 text-slate-500">تأیید پرداخت</button>
                          </fieldset>
                        </form>
                      </details>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {nextCursor && !error && (
        <button type="button" disabled={loading} onClick={() => load(nextCursor)} className="text-xs font-bold text-teal-700 disabled:opacity-50">
          بارگذاری سفارش‌های بیشتر
        </button>
      )}
    </section>
  );
};

export const FinancePendingPaymentsTab: React.FC = () => {
  const { formatPrice } = useStore();
  const { success, error } = useToast();
  const [orderId, setOrderId] = useState('');
  const [amount, setAmount] = useState('');
  const [reason, setReason] = useState('');
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [refunding, setRefunding] = useState(false);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!orderId.trim()) {
      error('شناسه سفارش برای استرداد الزامی است.');
      return;
    }
    if (!reason.trim()) {
      error('دلیل استرداد الزامی است.');
      return;
    }
    const parsed = amount.trim() ? Number(amount.trim()) : NaN;
    if (amount.trim() && (!Number.isSafeInteger(parsed) || parsed <= 0)) {
      error('مبلغ استرداد باید عددی بزرگ‌تر از صفر باشد (یا خالی برای کل مبلغ).');
      return;
    }
    setConfirmOpen(true);
  };

  const confirmMessage = `سفارش: ${orderId.trim()}؛ مبلغ: ${amount.trim() ? `${formatPrice(Number(amount.trim()))} تومان` : 'کل مبلغ سفارش'}؛ دلیل: ${reason.trim()}. درخواست به روش پرداخت اولیه ثبت می‌شود؛ نتیجه پس از بررسی و تأیید پرداخت مشخص خواهد شد.`;

  const handleConfirmRefund = async () => {
    setConfirmOpen(false);
    setRefunding(true);
    try {
      const refundAmount = amount.trim() ? Number(amount.trim()) : undefined;
      const result = await financeOperation<{success:boolean;status:string;requestedAmountToman:number}>(
        `/admin/payments/${encodeURIComponent(orderId.trim())}/refund`,
        {reason:reason.trim(),...(refundAmount !== undefined ? {amount:refundAmount} : {})},
        `admin-refund:${orderId.trim()}`,
        value=>value.success===true && ['pending','unknown','succeeded','failed'].includes(value.status) && Number.isSafeInteger(value.requestedAmountToman) && value.requestedAmountToman>0,
      );
      success(result.status==='unknown' ? 'درخواست بازپرداخت ثبت شد و نیازمند بررسی است؛ بازگشت وجه هنوز تأیید نشده است.' : 'درخواست بازپرداخت ثبت شد؛ نتیجه را در وضعیت سفارش پیگیری کنید.');
      setOrderId('');
      setAmount('');
      setReason('');
    } catch (err: any) {
      error(err?.message || 'استرداد انجام نشد.');
    } finally {
      setRefunding(false);
    }
  };

  return (
    <div className="space-y-4 pb-6" dir="rtl">
      <PendingPaymentsList />
      <form onSubmit={handleSubmit} className="bg-white border border-slate-200/90 rounded-2xl shadow-2xs p-4 space-y-3">
        <div className="flex items-center gap-2 text-rose-700">
          <h3 className="text-sm font-bold">استرداد وجه سفارش پرداخت‌شده (بازگشت وجه)</h3>
        </div>
        <p className="text-[11px] text-slate-500 leading-relaxed">
          درخواست بازپرداخت به روش پرداخت اولیه ثبت می‌شود. بازگشت وجه پس از تأیید پرداخت مشخص می‌شود. مبلغ خالی = کل مبلغ باقی‌مانده قابل بازپرداخت.
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
          <input type="text" value={orderId} onChange={(e) => setOrderId(e.target.value)} placeholder="شناسه سفارش (الزامی)" className="min-h-[44px] px-3 border border-slate-200 rounded-xl text-sm font-mono" dir="ltr" />
          <input type="text" inputMode="numeric" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="مبلغ (تومان) — خالی = کل" className="min-h-[44px] px-3 border border-slate-200 rounded-xl text-sm" />
          <input type="text" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="دلیل استرداد (الزامی)" className="min-h-[44px] px-3 border border-slate-200 rounded-xl text-sm" />
        </div>
        <button type="submit" disabled={refunding} className="min-h-[44px] px-4 bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white rounded-xl text-xs font-bold flex items-center justify-center gap-2">
          {refunding ? 'در حال ثبت…' : 'درخواست بازپرداخت'}
        </button>
      </form>
      <ConfirmDialog
        isOpen={confirmOpen}
        title="ثبت درخواست بازپرداخت؟"
        message={confirmMessage}
        confirmText="ثبت درخواست"
        isDanger={true}
        onConfirm={handleConfirmRefund}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
};
