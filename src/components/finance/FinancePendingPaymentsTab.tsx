import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
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
  const paymentCellClass = 'min-w-0 [overflow-wrap:anywhere] md:table-cell md:p-2';
  const mobileLabelClass = 'block text-xs font-bold text-slate-600 md:hidden';

  return (
    <section aria-labelledby="unpaid-order-heading" className="bg-white border border-slate-200/90 rounded-2xl shadow-2xs p-4 space-y-3">
      <h3 id="unpaid-order-heading" className="text-sm font-bold text-slate-900">سفارش‌های پرداخت‌نشده</h3>
      {loading && <p role="status" className="text-sm text-slate-600">در حال بارگذاری سفارش‌ها…</p>}
      {error === 'unavailable' && <p role="status" className="text-sm text-slate-600">فهرست سفارش‌های پرداخت‌نشده هنوز در دسترس نیست.</p>}
      {error === 'failed' && (
        <div className="rounded-xl bg-rose-50 p-3 text-sm text-rose-800">
          <p role="alert">سفارش‌های پرداخت‌نشده بارگذاری نشد.</p>
          <button type="button" className="min-h-11 rounded-lg px-2 font-bold focus-visible:outline-2 focus-visible:outline-rose-700" onClick={() => load(request.cursor)}>تلاش دوباره</button>
        </div>
      )}
      {!loading && !error && orders.length === 0 && <p className="text-sm text-slate-600">سفارش پرداخت‌نشده‌ای نیست.</p>}
      {orders.length > 0 && (
          <table role="table" aria-labelledby="unpaid-order-heading" className="block w-full text-start text-sm md:table md:table-fixed">
            <thead role="rowgroup" className="sr-only md:not-sr-only md:table-header-group">
              <tr role="row" className="border-b border-slate-200 text-slate-600">
                <th role="columnheader" scope="col" className="p-2 text-start">شماره سفارش</th>
                <th role="columnheader" scope="col" className="p-2 text-start">نام مشتری</th>
                <th role="columnheader" scope="col" className="p-2 text-start">مبلغ (تومان)</th>
                <th role="columnheader" scope="col" className="p-2 text-start">زمان انتظار</th>
                {canConfirm && <th role="columnheader" scope="col" className="p-2 text-start">تأیید پرداخت</th>}
              </tr>
            </thead>
            <tbody role="rowgroup" className="block space-y-2 md:table-row-group md:space-y-0">
              {orders.map(order => (
                <tr role="row" key={order.id} className="grid grid-cols-2 gap-2 rounded-xl border border-slate-200 p-3 align-top md:table-row md:rounded-none md:border-0 md:border-b md:p-0">
                  <td role="cell" className={`col-span-2 ${paymentCellClass}`}><span aria-hidden="true" className={mobileLabelClass}>شماره سفارش</span><bdi>{order.orderNumber || order.id}</bdi></td>
                  <td role="cell" className={`col-span-2 ${paymentCellClass}`}><span aria-hidden="true" className={mobileLabelClass}>نام مشتری</span>{order.customerName || '—'}</td>
                  <td role="cell" className={paymentCellClass}><span aria-hidden="true" className={mobileLabelClass}>مبلغ (تومان)</span>{formatPrice(order.amountToman)}</td>
                  <td role="cell" className={paymentCellClass}><span aria-hidden="true" className={mobileLabelClass}>زمان انتظار</span>{toPersianDigits(order.ageHours)} ساعت</td>
                  {canConfirm && (
                    <td role="cell" className={`col-span-2 border-t border-slate-200 pt-2 md:border-0 ${paymentCellClass}`}>
                      <details>
                        <summary className="min-h-11 cursor-pointer rounded-lg py-2.5 font-bold text-teal-800 focus-visible:outline-2 focus-visible:outline-teal-700">تأیید دستی پرداخت</summary>
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
      )}
      {nextCursor && !error && (
        <button type="button" disabled={loading} onClick={() => load(nextCursor)} className="min-h-11 rounded-lg px-2 text-sm font-bold text-teal-800 focus-visible:outline-2 focus-visible:outline-teal-700 disabled:opacity-50">
          بارگذاری سفارش‌های بیشتر
        </button>
      )}
    </section>
  );
};

export const FinancePendingPaymentsTab: React.FC = () => {
  const { adminSession } = useStore();
  const canRefund = adminSession?.role === 'admin' || adminSession?.role === 'super_admin';
  return (
    <div className="space-y-4 pb-6" dir="rtl">
      <PendingPaymentsList />
      {canRefund && <AdminRefundForm />}
    </div>
  );
};

function AdminRefundForm() {
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
    <>
      <form onSubmit={handleSubmit} className="bg-white border border-slate-200/90 rounded-2xl shadow-2xs p-4 space-y-3">
        <div className="flex items-center gap-2 text-rose-700">
          <h3 className="text-sm font-bold">استرداد وجه سفارش پرداخت‌شده (بازگشت وجه)</h3>
        </div>
        <p className="text-sm leading-6 text-slate-600">
          درخواست بازپرداخت به روش پرداخت اولیه ثبت می‌شود. بازگشت وجه پس از تأیید پرداخت مشخص می‌شود. مبلغ خالی = کل مبلغ باقی‌مانده قابل بازپرداخت.
        </p>
        <fieldset disabled={refunding} className="grid gap-3 md:grid-cols-3">
          <label className="min-w-0 space-y-1">
            <span className="block text-sm font-bold">شناسه سفارش</span>
            <input type="text" value={orderId} onChange={(e) => setOrderId(e.target.value)} placeholder="شناسه سفارش (الزامی)" className="min-h-11 w-full rounded-xl border border-slate-300 px-3 text-base focus-visible:outline-2 focus-visible:outline-brand" dir="ltr" />
          </label>
          <label className="min-w-0 space-y-1">
            <span className="block text-sm font-bold">مبلغ (تومان)</span>
            <input type="text" inputMode="numeric" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="مبلغ (تومان) — خالی = کل" className="min-h-11 w-full rounded-xl border border-slate-300 px-3 text-base focus-visible:outline-2 focus-visible:outline-brand" />
          </label>
          <label className="min-w-0 space-y-1">
            <span className="block text-sm font-bold">دلیل استرداد</span>
            <input type="text" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="دلیل استرداد (الزامی)" className="min-h-11 w-full rounded-xl border border-slate-300 px-3 text-base focus-visible:outline-2 focus-visible:outline-brand" />
          </label>
        </fieldset>
        <button type="submit" disabled={refunding} className="min-h-11 w-full rounded-xl bg-rose-700 px-4 text-sm font-bold text-white hover:bg-rose-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-rose-700 disabled:opacity-50 sm:w-auto">
          {refunding ? 'در حال ثبت…' : 'درخواست بازپرداخت'}
        </button>
      </form>
      {confirmOpen && (
        <SupplierDialog titleId="refund-confirm-heading" descriptionId="refund-confirm-description" onDismiss={() => setConfirmOpen(false)}>
          <SupplierDialogHeading id="refund-confirm-heading" title="ثبت درخواست بازپرداخت؟" onClose={() => setConfirmOpen(false)} />
          <p id="refund-confirm-description" className="text-sm leading-6 text-slate-700 [overflow-wrap:anywhere]">{confirmMessage}</p>
          <div className="mt-4 grid grid-cols-2 gap-2">
            <button type="button" onClick={() => setConfirmOpen(false)} className="min-h-11 rounded-xl bg-slate-100 px-2 text-sm font-bold focus-visible:outline-2 focus-visible:outline-brand">انصراف</button>
            <button type="button" onClick={() => void handleConfirmRefund()} className="min-h-11 rounded-xl bg-rose-700 px-2 text-sm font-bold text-white focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-rose-700">ثبت درخواست</button>
          </div>
        </SupplierDialog>
      )}
    </>
  );
}
