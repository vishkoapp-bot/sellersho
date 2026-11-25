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
  const [fieldError, setFieldError] = useState<{ name: 'holder' | 'shaba' | 'amount'; message: string } | null>(null);

  const editField = (name: 'holder' | 'shaba' | 'amount', value: string) => {
    if (fieldError?.name === name) setFieldError(null);
    if (name === 'holder') setHolder(value);
    else if (name === 'shaba') setShaba(value);
    else setAmount(value);
  };

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (saving || balance.loading || !balance.data || balance.error) return;
    const value = Number(parseAmount(amount));
    const iban = shaba.replace(/[\s-]/g, '').toUpperCase()
      .replace(/[۰-۹٠-٩]/g, digit => String('۰۱۲۳۴۵۶۷۸۹٠١٢٣٤٥٦٧٨٩'.indexOf(digit) % 10));
    const invalid = !holder.trim()
      ? { name: 'holder', message: 'نام صاحب حساب را وارد کن.' } as const
      : !isValidIranianSheba(iban)
        ? { name: 'shaba', message: 'شماره شبا را با IR و ۲۴ رقم بعد از آن وارد کن.' } as const
        : !Number.isSafeInteger(value) || value <= 0 || value > balance.data.available
          ? { name: 'amount', message: `مبلغ باید بین ۱ و ${formatPrice(balance.data.available)} تومان باشد.` } as const
          : null;
    setFieldError(invalid);
    if (invalid) {
      (event.currentTarget.elements.namedItem(invalid.name) as HTMLInputElement | null)?.focus();
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
      <SupplierResourceState loading={balance.loading || history.loading} error={balance.error || history.error} retry={() => { void balance.reload(); void history.reload(); }} />
      {b && !balance.error && (
        <>
          <section className="rounded-2xl bg-slate-900 p-4 text-white">
            <span className="text-xs text-slate-300">موجودی قابل برداشت</span>
            <strong className="mt-1 block text-xl text-emerald-300">{formatPrice(b?.available || 0)} تومان</strong>
            <p className="mt-1 text-[10px] leading-relaxed text-slate-400">موجودی پس از تحویل سفارش قابل برداشت است.</p>
            <details className="mt-3 border-t border-white/10 pt-2">
              <summary className="cursor-pointer rounded-md py-2 text-xs font-bold focus-visible:ring-2 focus-visible:ring-white">جزئیات درآمد، برداشت‌ها و کسورات</summary>
              <div className="space-y-2.5 pt-2">
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
                  <p className="text-[11px] font-bold text-slate-100">تسویه‌های تأییدشده</p>
                  <p className="mt-0.5 text-[10px] leading-relaxed text-slate-400">شامل تسویه‌های تأییدشده، در حال انتقال و واریزشده است؛ تأیید درخواست به‌تنهایی به معنی واریز قطعی نیست. وضعیت هر درخواست را در سوابق ببین.</p>
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
            </details>
          </section>
          {b.available <= 0 ? (
            <p className="rounded-xl border border-slate-200 bg-white p-4 text-sm text-slate-700">فعلاً موجودی قابل برداشت نداری. با قطعی‌شدن درآمد و افزایش موجودی، فرم درخواست برداشت همین‌جا ظاهر می‌شود.</p>
          ) : (
          <form onSubmit={submit} noValidate className="space-y-3 rounded-2xl border bg-white p-4">
            <h3 className="text-sm font-bold">درخواست برداشت</h3>
            <label className="block text-[11px] font-bold text-slate-600">نام صاحب حساب
              <input required name="holder" value={holder} onChange={event => editField('holder', event.target.value)} aria-invalid={fieldError?.name === 'holder'} aria-describedby={fieldError?.name === 'holder' ? 'supplier-payout-holder-error' : undefined} placeholder="مثلاً: علی رضایی" className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs font-normal" />
              {fieldError?.name === 'holder' && <span id="supplier-payout-holder-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            <label className="block text-[11px] font-bold text-slate-600">شماره شبا
              <span className="block mt-1 text-[10px] font-normal text-slate-500 leading-relaxed">شماره ۲۴ رقمی حساب بانکی که با IR شروع می‌شود؛ روی کارت بانکی یا در اپ بانک می‌توانی پیدایش کنی.</span>
              <input required name="shaba" value={shaba} onChange={event => editField('shaba', event.target.value)} aria-invalid={fieldError?.name === 'shaba'} aria-describedby={fieldError?.name === 'shaba' ? 'supplier-payout-shaba-error' : undefined} placeholder="IR…" dir="ltr" className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs font-normal" />
              {fieldError?.name === 'shaba' && <span id="supplier-payout-shaba-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            <label className="block text-[11px] font-bold text-slate-600">مبلغ برداشت (تومان)
              <input required min="1" type="text" inputMode="numeric" name="amount" value={formatAmount(amount)} onChange={event => editField('amount', event.target.value)} aria-invalid={fieldError?.name === 'amount'} aria-describedby={fieldError?.name === 'amount' ? 'supplier-payout-amount-error' : undefined} placeholder="چقدر می‌خواهی برداشت کنی؟" className="mt-1 w-full rounded-xl border p-2.5 min-h-[44px] text-xs font-normal" />
              {fieldError?.name === 'amount' && <span id="supplier-payout-amount-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            <button disabled={saving || balance.loading} className="w-full rounded-xl bg-emerald-600 p-2.5 min-h-[44px] text-xs font-bold text-white">{saving ? 'در حال ثبت…' : 'ثبت درخواست برداشت'}</button>
            <p className="text-[10px] text-slate-500 leading-relaxed">واریز دستی است و با انتقال بانکی انجام می‌شود: بعد از تأیید درخواستت، تیم مالی پول را واریز می‌کند و شماره پیگیری همین‌جا ثبت می‌شود.</p>
          </form>
          )}
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
