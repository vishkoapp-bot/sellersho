import React, { useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { isValidIranianSheba } from '../../utils/validators';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';

// Live settlement tab (on): supplier balance, manual bank-transfer
// withdrawal requests, storage-fee charges (separate endpoint), payout
// history. The backend computes the balance on read: earned (orders paid
// AND delivered) - reserved (payout requests still pending review)
// - paid - return charges - storage fees = available. Supplier money has NO
// return-window hold; it becomes withdrawable once the order is delivered.
const formatAmount = (raw: string): string => {
  const digits = raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');
  if (digits === '') return '';
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬').replace(/\d/g, (ch) => '۰۱۲۳۴۵۶۷۸۹'[parseInt(ch, 10)]);
};

const parseAmount = (raw: string): string =>
  raw.replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch))).replace(/[^0-9]/g, '');

const statusLabel = (status: string): string => {
  switch (status) {
    case 'pending': return 'در انتظار بررسی';
    case 'approved': return 'تأیید شد — در انتظار واریز دستی';
    case 'transfer_confirmed': return 'واریز شد';
    case 'rejected': return 'رد شد';
    case 'processing': return 'در حال پردازش';
    case 'completed':
    case 'paid': return 'واریز شد';
    default: return status;
  }
};

export const SupplierSettlementTab: React.FC = () => {
  const { formatPrice, toPersianDigits } = useStore();
  const { success, error } = useToast();
  const balance = useSupplierResource(supplierApi.getSettlementBalance);
  const history = useSupplierResource(supplierApi.getSettlementHistory);
  const fees = useSupplierResource(supplierApi.getStorageFeeCharges);
  const [shaba, setShaba] = useState('');
  const [holder, setHolder] = useState('');
  const [amount, setAmount] = useState('');
  const [saving, setSaving] = useState(false);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (saving) return;
    const value = Number(parseAmount(amount));
    if (!Number.isSafeInteger(value) || value <= 0 || value > (balance.data?.available || 0)) {
      error('مبلغ را درست وارد کن؛ بیشتر از پولی که می‌توانی برداشت کنی نباشد.');
      return;
    }
    const iban = shaba.replace(/\s/g, '').toUpperCase();
    if (!isValidIranianSheba(iban) || !holder.trim()) {
      error('شماره شبا یا نام صاحب حساب درست نیست؛ دوباره نگاه کن.');
      return;
    }
    setSaving(true);
    try {
      await supplierApi.requestSettlement(value, iban, '', holder.trim());
      success('درخواست برداشتت ثبت شد؛ تیم مالی بررسی می‌کند.');
      setAmount('');
      await Promise.all([balance.reload(), history.reload()]);
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ثبت تسویه انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  const b = balance.data;
  const returnCharges = b?.returnCharges ?? 0;
  const returnClawbacks = b?.returnClawbacks ?? 0;
  const adjustments = b?.adjustments ?? 0;
  const storageFees = b?.storageFees ?? 0;
  const charges = fees.data?.charges ?? [];

  return (
    <div className="space-y-3 pb-6">
      <h2 className="text-sm font-bold">تسویه‌حساب و برداشت پول</h2>
      <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">
        <p>پول فروش کالاهایت اینجاست. موجودی پس از تحویل سفارش قابل برداشت است؛ یعنی مبلغ هر سفارش به‌محض اینکه پرداخت شود و به دست مشتری برسد، وارد درآمدت می‌شود و لازم نیست منتظر پایان مهلت مرجوعی بمانی.</p>
        <p className="mt-2"><b>چرا موجودی قابل برداشت از کل درآمدت کمتر است؟</b> مبلغ درخواست‌های برداشتی که هنوز در انتظار بررسی‌اند و مبالغی که قبلاً برایت واریز شده‌اند از درآمدت کم شده‌اند. ریز هر بخش را در کارت زیر می‌بینی.</p>
      </div>
      <SupplierResourceState loading={balance.loading || history.loading} error={balance.error || history.error} retry={() => { void balance.reload(); void history.reload(); }} />
      {!balance.error && (
        <>
          <section className="rounded-2xl bg-slate-900 p-4 text-white">
            <span className="text-xs text-slate-300">موجودی قابل برداشت</span>
            <strong className="mt-1 block text-xl text-emerald-300">{formatPrice(b?.available || 0)} تومان</strong>
            <p className="mt-1 text-[10px] leading-relaxed text-slate-400">موجودی پس از تحویل سفارش قابل برداشت است.</p>
            <div className="mt-3 space-y-2.5 border-t border-white/10 pt-3">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-[11px] font-bold text-slate-100">درآمد قطعی‌شده از سفارش‌های تحویل‌شده</p>
                  <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">جمع مبلغ پایه کالاهای تو در سفارش‌هایی که هم پرداخت شده‌اند و هم به دست مشتری رسیده‌اند.</p>
                </div>
                <span className="shrink-0 text-[11px] font-bold">{formatPrice(b?.earned || 0)} تومان</span>
              </div>
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-[11px] font-bold text-slate-100">در انتظار بررسی برداشت</p>
                  <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">مبلغ درخواست‌های برداشتی که ثبت کرده‌ای و تیم مالی هنوز بررسی نکرده است؛ تا مشخص‌شدن نتیجه، از موجودی قابل برداشتت کم می‌شود.</p>
                </div>
                <span className="shrink-0 text-[11px] font-bold">{formatPrice(b?.reserved || 0)} تومان</span>
              </div>
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-[11px] font-bold text-slate-100">پرداخت‌شده</p>
                  <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">مجموع مبالغی که از درخواست‌های برداشتت تأیید و به حسابت واریز شده است.</p>
                </div>
                <span className="shrink-0 text-[11px] font-bold">{formatPrice(b?.paid || 0)} تومان</span>
              </div>
              {returnCharges !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-amber-200">کسر بابت مرجوعی‌ها</p>
                    <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">هزینه‌هایی که بابت مرجوعی کالاها از حسابت کسر شده و از موجودی قابل برداشتت کم می‌شود.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold text-amber-300">{formatPrice(returnCharges)} تومان</span>
                </div>
              )}
              {returnClawbacks !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-amber-200">برگشت هزینه کالا از مرجوعی‌های بازپرداخت‌شده</p>
                    <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">مبلغ پایه کالاهایی که مشتری برگردانده و پولش به مشتری بازپرداخت شده است؛ از موجودی قابل برداشتت کم می‌شود.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold text-amber-300">{formatPrice(returnClawbacks)} تومان</span>
                </div>
              )}
              {adjustments !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-slate-100">اسناد تعدیل مالی</p>
                    <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">تعدیل‌های ثبت‌شده توسط تیم مالی؛ مبلغ مثبت به موجودی قابل برداشتت اضافه و مبلغ منفی از آن کم می‌شود.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold">{formatPrice(adjustments)} تومان</span>
                </div>
              )}
              {storageFees !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-amber-200">کسر بابت انبارداری</p>
                    <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">هزینه نگهداری کالاهای ثبت‌شده در انبار مرکزی (کالاهایی که از مسیر ورود کالا به انبار ثبت شده‌اند) پس از پایان روزهای رایگان؛ ریز آن را در بخش «هزینه انبارداری» پایین همین صفحه می‌بینی.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold text-amber-300">{formatPrice(storageFees)} تومان</span>
                </div>
              )}
            </div>
          </section>
          <form onSubmit={submit} className="space-y-2 rounded-2xl border bg-white p-3">
            <label className="block text-[11px] font-bold text-slate-600">نام صاحب حساب
              <input required value={holder} onChange={(event) => setHolder(event.target.value)} placeholder="مثلاً: علی رضایی" className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs font-normal" />
            </label>
            <label className="block text-[11px] font-bold text-slate-600">شماره شبا
              <span className="block mt-1 text-[10px] font-normal text-slate-500 leading-relaxed">شماره ۲۴ رقمی حساب بانکی که با IR شروع می‌شود؛ روی کارت بانکی یا در اپ بانک می‌توانی پیدایش کنی.</span>
              <input required value={shaba} onChange={(event) => setShaba(event.target.value)} placeholder="IR…" dir="ltr" className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs font-normal" />
            </label>
            <label className="block text-[11px] font-bold text-slate-600">مبلغ برداشت (تومان)
              <input required min="1" type="text" inputMode="numeric" value={formatAmount(amount)} onChange={(event) => setAmount(event.target.value)} placeholder="چقدر می‌خواهی برداشت کنی؟" className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs font-normal" />
            </label>
            <button disabled={saving} className="w-full rounded-xl bg-emerald-600 p-2.5 min-h-[44px] text-xs font-bold text-white">{saving ? 'ثبت…' : 'درخواست برداشت'}</button>
            <p className="text-[10px] text-slate-500 leading-relaxed">واریز دستی است و با انتقال بانکی انجام می‌شود: بعد از تأیید درخواستت، تیم مالی پول را واریز می‌کند و شماره پیگیری همین‌جا ثبت می‌شود.</p>
          </form>
        </>
      )}
      {!fees.error && charges.length > 0 && (
        <>
          <h3 className="text-xs font-bold text-slate-700">هزینه انبارداری</h3>
          <div className="space-y-2">
            {charges.map((charge) => (
              <div key={charge.id} className="rounded-2xl border bg-white p-3 text-[11px] leading-5">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-bold text-slate-800">{charge.productTitle || 'کالا'}</span>
                  <span className="font-extrabold text-slate-900 font-mono">{formatPrice(charge.amountToman)} تومان</span>
                </div>
                <p className="text-slate-500">{charge.warehouseName || 'انبار مرکزی'} • {new Date(charge.feeDate).toLocaleDateString('fa-IR')} • {toPersianDigits(charge.qty)} واحد × {formatPrice(charge.feeUnitDayToman)} تومان در روز</p>
                <p className="text-slate-400 text-[10px]">{charge.goodsClass === 'returned' ? 'کالای مرجوعی' : charge.goodsClass === 'stagnant' ? 'کالای راکد' : 'کالای عادی'}{charge.estimated ? ' • وزن/ابعاد تخمینی' : ''}</p>
              </div>
            ))}
          </div>
        </>
      )}
      {!history.error && (history.data || []).length > 0 && <h3 className="text-xs font-bold text-slate-700">برداشت‌های قبلی تو</h3>}
      {!history.error && !(history.data || []).length && !history.loading && <p className="rounded-2xl border bg-white p-4 text-[11px] leading-5 text-slate-500">هنوز برداشتی نداشته‌ای. وقتی موجودی قابل برداشت داشتی، از فرم بالا اولین برداشتت را ثبت کن.</p>}
      {!history.error && (history.data || []).map((item) => (
        <section key={item.id} className="rounded-2xl border bg-white p-3 text-xs">
          <strong>{formatPrice(item.amount)} تومان</strong>
          <p className="mt-0.5">{statusLabel(item.status)} · {new Date(item.created_at).toLocaleDateString('fa-IR')}</p>
          {(item.transfer_reference || (item as any).transferReference) && <p className="text-slate-500">شماره پیگیری: <span dir="ltr">{item.transfer_reference || (item as any).transferReference}</span></p>}
        </section>
      ))}
    </div>
  );
};
