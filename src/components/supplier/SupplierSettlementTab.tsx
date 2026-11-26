import { Wallet } from 'lucide-react';
import { SupplierSectionHeading } from './SupplierSectionHeading';
import React, { useRef, useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { isValidIranianSheba } from '../../utils/validators';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { AccountStatement } from '../common/AccountStatement';

// Live settlement tab (on): supplier balance, manual bank-transfer
// withdrawal requests, storage-fee charges (separate endpoint), payout
// history. The backend computes the balance on read: earned (orders paid
// AND delivered) - reserved (payout requests still pending review)
// - paid - return charges - storage fees = available. Supplier money has NO
// return-window hold; it becomes withdrawable once the order is delivered.
const latinDigits = (raw: string) =>
  raw.replace(/[۰-۹٠-٩]/g, digit => String('۰۱۲۳۴۵۶۷۸۹٠١٢٣٤٥٦٧٨٩'.indexOf(digit) % 10));

function parseSupplierSettlementAmount(raw: string): number {
  const text = latinDigits(raw.trim());
  if (!/^(?:\d+|\d{1,3}(?:[٬, ]\d{3})+)$/.test(text)) return NaN;
  return Number(text.replace(/[٬, ]/g, ''));
}

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

const summaryClass = 'min-h-11 cursor-pointer rounded-md py-2.5 text-sm font-bold focus-visible:ring-2';

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
  const submitting = useRef(false);
  const [statementRefresh, setStatementRefresh] = useState(0);
  const [fieldError, setFieldError] = useState<{ name: 'holder' | 'shaba' | 'amount'; message: string } | null>(null);

  const editField = (name: 'holder' | 'shaba' | 'amount', value: string) => {
    if (fieldError?.name === name) setFieldError(null);
    if (name === 'holder') setHolder(value);
    else if (name === 'shaba') setShaba(value);
    else setAmount(value);
  };

  const submit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (submitting.current || balance.loading || !balance.data || balance.error) return;
    const value = parseSupplierSettlementAmount(amount);
    const iban = latinDigits(shaba.replace(/[\s-]/g, '').toUpperCase());
    const invalid = !holder.trim()
      ? { name: 'holder', message: 'نام صاحب حساب را وارد کن.' } as const
      : !isValidIranianSheba(iban)
        ? { name: 'shaba', message: 'شماره شبا را با IR و ۲۴ رقم بعد از آن وارد کن.' } as const
        : !Number.isSafeInteger(value) || value <= 0
          ? { name: 'amount', message: 'مبلغ برداشت را به‌صورت عدد صحیح و مثبت وارد کن.' } as const
          : null;
    setFieldError(invalid);
    if (invalid) {
      (event.currentTarget.elements.namedItem(invalid.name) as HTMLInputElement | null)?.focus();
      return;
    }
    submitting.current = true;
    setSaving(true);
    try {
      await supplierApi.requestSettlement(value, iban, '', holder.trim());
      success('درخواست برداشتت ثبت شد؛ تیم مالی بررسی می‌کند.');
      setAmount('');
      setStatementRefresh(value => value + 1);
      await Promise.all([balance.reload(), history.reload()]);
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ثبت تسویه انجام نشد.');
    } finally {
      submitting.current = false;
      setSaving(false);
    }
  };

  const b = balance.data;
  const charges = fees.data?.charges ?? [];
  const breakdown = b ? [
    { label: 'درآمد قطعی', value: b.earned, detail: 'مبلغ پایهٔ سفارش‌های پرداخت‌شده و تحویل‌شده.' },
    { label: 'برداشت‌های در انتظار بررسی', value: b.reserved, detail: 'این مبلغ تا مشخص‌شدن نتیجه از موجودی قابل برداشت کم می‌شود.' },
    { label: 'برداشت‌های تأییدشده و واریزشده', value: b.paid, detail: 'وضعیت انتقال هر درخواست را در سوابق برداشت ببین.' },
    { label: 'هزینهٔ مرجوعی', value: b.returnCharges, hideZero: true },
    { label: 'مبلغ پایهٔ کالاهای مرجوعی بازپرداخت‌شده', value: b.returnClawbacks, hideZero: true },
    { label: 'تعدیل‌های مالی', value: b.adjustments, detail: 'عدد مثبت به موجودی اضافه و عدد منفی از آن کم می‌شود.', hideZero: true },
    { label: 'هزینهٔ انبارداری', value: b.storageFees, hideZero: true },
  ].filter(row => !row.hideZero || row.value) : [];

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="تسویه‌حساب" description="موجودی‌ات را ببین، برداشت ثبت کن و وضعیت واریز را پیگیری کن." icon={<Wallet />} />
      <SupplierResourceState loading={balance.loading} error={balance.error} retry={balance.reload} />
      {b && !balance.error && (
        <div className="grid items-start gap-4 xl:grid-cols-2">
          <section className="min-w-0 rounded-[24px] bg-[#2c2050] p-5 text-white shadow-lg shadow-[#2c2050]/15 sm:p-6">
            <h3 className="text-sm font-bold text-[#e8e0fb]">موجودی قابل برداشت</h3>
            <strong className="mt-2 block break-words text-3xl font-black tabular-nums sm:text-4xl">{formatPrice(b.available)} <span className="text-base font-bold text-[#e8e0fb]">تومان</span></strong>
            <p className="mt-2 text-xs leading-6 text-[#e8e0fb]">درآمد سفارش‌های پرداخت‌شده پس از تحویل قابل برداشت می‌شود.</p>
            <details className="mt-3 border-t border-white/10 pt-2">
              <summary className={`${summaryClass} focus-visible:ring-white`}>جزئیات موجودی</summary>
              <dl className="divide-y divide-white/10">
                {breakdown.map(row => (
                  <div key={row.label} className="flex flex-wrap items-start justify-between gap-x-3 gap-y-1 py-2">
                    <dt className="text-sm text-[#e8e0fb]">{row.label}</dt>
                    <dd className="text-sm font-bold tabular-nums">{formatPrice(row.value || 0)} تومان</dd>
                    {row.detail && <dd className="w-full text-xs leading-5 text-[#e8e0fb]">{row.detail}</dd>}
                  </div>
                ))}
              </dl>
            </details>
          </section>
          <form onSubmit={submit} noValidate className="min-w-0 space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <h3 className="text-base font-extrabold text-[#261b41]">درخواست برداشت</h3>
            {b.available <= 0 && <p className="rounded-xl bg-slate-50 p-3 text-sm leading-6 text-slate-700">موجودی قابل برداشت نداری. برای پیگیری درخواست قبلی، «سوابق برداشت» را ببین. اگر پاسخ ثبت را دریافت نکردی، همان مبلغ و مشخصات را دوباره بفرست.</p>}
            <fieldset disabled={saving} className="min-w-0 space-y-3">
            <label className="block text-sm font-bold text-slate-700">نام صاحب حساب
              <input required maxLength={100} name="holder" value={holder} onChange={event => editField('holder', event.target.value)} aria-invalid={fieldError?.name === 'holder'} aria-describedby={fieldError?.name === 'holder' ? 'supplier-payout-holder-error' : undefined} placeholder="مثلاً: علی رضایی" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus:bg-white focus-visible:ring-2 focus-visible:ring-brand aria-invalid:border-rose-500" />
              {fieldError?.name === 'holder' && <span id="supplier-payout-holder-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            <label className="block text-sm font-bold text-slate-700">شماره شبا
              <input required name="shaba" value={shaba} onChange={event => editField('shaba', event.target.value)} aria-invalid={fieldError?.name === 'shaba'} aria-describedby={fieldError?.name === 'shaba' ? 'supplier-payout-shaba-error supplier-payout-shaba-help' : 'supplier-payout-shaba-help'} placeholder="IR…" dir="ltr" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus:bg-white focus-visible:ring-2 focus-visible:ring-brand aria-invalid:border-rose-500" />
              <span id="supplier-payout-shaba-help" className="mt-1 block text-xs font-normal leading-6 text-slate-600"><bdi dir="ltr">IR</bdi> و سپس ۲۴ رقم شمارهٔ شبا را وارد کن.</span>
              {fieldError?.name === 'shaba' && <span id="supplier-payout-shaba-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            <label className="block text-sm font-bold text-slate-700">مبلغ برداشت (تومان)
              <input required type="text" inputMode="numeric" name="amount" value={amount} onChange={event => editField('amount', event.target.value)} aria-invalid={fieldError?.name === 'amount'} aria-describedby={fieldError?.name === 'amount' ? 'supplier-payout-amount-error' : undefined} placeholder="چقدر می‌خواهی برداشت کنی؟" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus:bg-white focus-visible:ring-2 focus-visible:ring-brand aria-invalid:border-rose-500" />
              {fieldError?.name === 'amount' && <span id="supplier-payout-amount-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            </fieldset>
            <button disabled={saving || balance.loading} className="min-h-12 w-full rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50">{saving ? 'در حال ثبت…' : 'ثبت درخواست برداشت'}</button>
            <p className="text-xs leading-6 text-slate-600">پس از تأیید، تیم مالی واریز بانکی را انجام می‌دهد و شمارهٔ پیگیری را در سوابق برداشت ثبت می‌کند.</p>
          </form>
        </div>
      )}
      <section aria-labelledby="supplier-settlement-history" className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <h3 id="supplier-settlement-history" className="text-base font-extrabold text-[#261b41]">سوابق برداشت</h3>
        <SupplierResourceState loading={history.loading} error={history.error} retry={history.reload} />
        {!history.loading && !history.error && !history.data?.length && <p className="text-sm text-slate-600">هنوز درخواستی برای برداشت ثبت نشده است.</p>}
        {!history.loading && !history.error && !!history.data?.length && (
          <ul className="divide-y divide-slate-100">
            {history.data.map(item => {
              const reference = item.transfer_reference || (item as typeof item & { transferReference?: string }).transferReference;
              return (
                <li key={item.id} className="space-y-1 py-3 text-sm first:pt-0 last:pb-0">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <strong className="tabular-nums text-[#261b41]">{formatPrice(item.amount)} تومان</strong>
                    <span className="rounded-lg bg-brand-soft px-2 py-1 text-xs font-bold text-brand-deep">{statusLabel(item.status)}</span>
                  </div>
                  <time dateTime={item.created_at} className="block text-xs text-slate-600">{new Date(item.created_at).toLocaleDateString('fa-IR')}</time>
                  {reference && <p className="text-slate-700">شمارهٔ پیگیری <bdi dir="ltr" className="break-all">{reference}</bdi></p>}
                </li>
              );
            })}
          </ul>
        )}
      </section>
      <AccountStatement role="supplier" refresh={statementRefresh} />
      {(fees.loading || fees.error || charges.length > 0) && (
        <details className="rounded-[22px] border border-[#e6e2f0] bg-white px-4 py-2 shadow-sm sm:px-5">
          <summary className={`${summaryClass} text-[#261b41] focus-visible:ring-brand`}>هزینهٔ انبارداری</summary>
          <SupplierResourceState loading={fees.loading} error={fees.error} retry={fees.reload} />
          {!fees.error && charges.length > 0 && (
            <ul className="divide-y divide-slate-100 pb-2 text-sm">
              {charges.map(charge => (
                <li key={charge.id} className="space-y-1 py-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="font-bold text-slate-800">{charge.productTitle || 'کالا'}</span>
                    <span className="font-extrabold tabular-nums text-slate-900">{formatPrice(charge.amountToman)} تومان</span>
                  </div>
                  <p className="text-slate-600">{charge.warehouseName || 'انبار مرکزی'} · {new Date(charge.feeDate).toLocaleDateString('fa-IR')} · {toPersianDigits(charge.qty)} واحد × {formatPrice(charge.feeUnitDayToman)} تومان در روز</p>
                  <p className="text-xs text-slate-600">{charge.goodsClass === 'returned' ? 'کالای مرجوعی' : charge.goodsClass === 'stagnant' ? 'کالای راکد' : 'کالای عادی'}{charge.estimated ? ' · وزن یا ابعاد تخمینی' : ''}</p>
                </li>
              ))}
            </ul>
          )}
        </details>
      )}
    </div>
  );
};
