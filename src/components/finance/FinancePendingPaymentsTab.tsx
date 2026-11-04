import React, { useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { apiRequest } from '../../services/apiClient';

// Live parity: the refund form inside live's finance-pending-payments tab
// (AdminDashboardView-MERGED.js, Md component). Only the refund form is
// wired here: live's pending-payment list and manual-confirm button use
// GET /admin/orders/pending-payment and POST
// /admin/payments/<id>/confirm-manual, neither of which exists in this
// candidate backend, so they are NOT rebuilt or invented here (see report).
// The refund call itself matches live exactly: POST
// /admin/payments/<id>/refund with {reason, amount?} (amount omitted for
// a full refund), the same validation order and messages, the same
// confirm dialog, the same success toast, and the same field clearing.
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
    if (amount.trim() && (!Number.isFinite(parsed) || parsed <= 0)) {
      error('مبلغ استرداد باید عددی بزرگ‌تر از صفر باشد (یا خالی برای کل مبلغ).');
      return;
    }
    setConfirmOpen(true);
  };

  const confirmMessage = `سفارش: ${orderId.trim()}؛ مبلغ: ${amount.trim() ? `${formatPrice(Math.round(Number(amount.trim())))} تومان` : 'کل مبلغ سفارش'}؛ دلیل: ${reason.trim()}. ابتدا درگاه PSP استرداد را انجام می‌دهد و سپس سند حسابداری ثبت می‌شود؛ فقط سفارش‌های پرداخت‌شده قابل استرداد هستند.`;

  const handleConfirmRefund = async () => {
    setConfirmOpen(false);
    setRefunding(true);
    try {
      const refundAmount = amount.trim() ? Math.round(Number(amount.trim())) : undefined;
      await apiRequest(`/admin/payments/${encodeURIComponent(orderId.trim())}/refund`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({
          reason: reason.trim(),
          ...(refundAmount !== undefined ? { amount: refundAmount } : {}),
        }),
      });
      success('استرداد وجه در درگاه انجام و سند حسابداری ثبت شد.');
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
      <form onSubmit={handleSubmit} className="bg-white border border-slate-200/90 rounded-2xl shadow-2xs p-4 space-y-3">
        <div className="flex items-center gap-2 text-rose-700">
          <h3 className="text-sm font-bold">استرداد وجه سفارش پرداخت‌شده (بازگشت وجه)</h3>
        </div>
        <p className="text-[11px] text-slate-500 leading-relaxed">
          فقط سفارش‌های پرداخت‌شده قابل استرداد هستند. ابتدا درگاه PSP استرداد را انجام می‌دهد و سپس سند حسابداری ثبت می‌شود. مبلغ خالی = استرداد کل مبلغ سفارش.
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
          <input type="text" value={orderId} onChange={(e) => setOrderId(e.target.value)} placeholder="شناسه سفارش (الزامی)" className="min-h-[44px] px-3 border border-slate-200 rounded-xl text-sm font-mono" dir="ltr" />
          <input type="text" inputMode="numeric" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="مبلغ (تومان) — خالی = کل" className="min-h-[44px] px-3 border border-slate-200 rounded-xl text-sm" />
          <input type="text" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="دلیل استرداد (الزامی)" className="min-h-[44px] px-3 border border-slate-200 rounded-xl text-sm" />
        </div>
        <button type="submit" disabled={refunding} className="min-h-[44px] px-4 bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white rounded-xl text-xs font-bold flex items-center justify-center gap-2">
          {refunding ? 'در حال استرداد…' : 'بازگشت وجه'}
        </button>
      </form>
      <ConfirmDialog
        isOpen={confirmOpen}
        title="استرداد قطعی وجه؟"
        message={confirmMessage}
        confirmText="استرداد قطعی"
        isDanger={true}
        onConfirm={handleConfirmRefund}
        onCancel={() => setConfirmOpen(false)}
      />
    </div>
  );
};
