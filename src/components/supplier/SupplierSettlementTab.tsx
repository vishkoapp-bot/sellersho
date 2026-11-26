import { Wallet } from 'lucide-react';
import { SupplierSectionHeading } from './SupplierSectionHeading';
import React, { useRef, useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { isValidIranianSheba } from '../../utils/validators';
import { useSupplierResource } from './useSupplierResource';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
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
  const returnCharges = b?.returnCharges ?? 0;
  const returnClawbacks = b?.returnClawbacks ?? 0;
  const adjustments = b?.adjustments ?? 0;
  const storageFees = b?.storageFees ?? 0;
  const charges = fees.data?.charges ?? [];

  return (
    <div className="space-y-4 pb-6 sm:space-y-5">
      <SupplierSectionHeading title="تسویه‌حساب" description="موجودی قابل برداشت، درخواست برداشت و سوابق واریز را در همین صفحه ببین." icon={<Wallet />} />
      <SupplierResourceState loading={balance.loading || history.loading} error={balance.error || history.error} retry={() => { void balance.reload(); void history.reload(); }} />
      {b && !balance.error && (
        <div className="grid items-start gap-4 xl:grid-cols-2">
          <section className="min-w-0 rounded-[24px] bg-[#2c2050] p-5 text-white shadow-lg shadow-[#2c2050]/15 sm:p-6">
            <h3 className="text-sm font-bold text-[#e8e0fb]">موجودی قابل برداشت</h3>
            <strong className="mt-2 block break-words text-3xl font-black tabular-nums sm:text-4xl">{formatPrice(b.available)} <span className="text-base font-bold text-[#e8e0fb]">تومان</span></strong>
            <p className="mt-2 text-xs leading-6 text-[#e8e0fb]">درآمد سفارش‌های پرداخت‌شده پس از تحویل قابل برداشت می‌شود.</p>
            <details className="mt-3 border-t border-white/10 pt-2">
              <summary className="cursor-pointer rounded-md py-2 text-xs font-bold focus-visible:ring-2 focus-visible:ring-white">جزئیات درآمد، برداشت‌ها و کسورات</summary>
              <div className="space-y-2.5 pt-2">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-[11px] font-bold text-slate-100">درآمد قطعی‌شده از سفارش‌های تحویل‌شده</p>
                  <p className="mt-0.5 text-xs leading-6 text-[#e8e0fb]">جمع مبلغ پایه کالاهای تو در سفارش‌هایی که هم پرداخت شده‌اند و هم به دست مشتری رسیده‌اند.</p>
                </div>
                <span className="shrink-0 text-[11px] font-bold">{formatPrice(b?.earned || 0)} تومان</span>
              </div>
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-[11px] font-bold text-slate-100">در انتظار بررسی برداشت</p>
                  <p className="mt-0.5 text-xs leading-6 text-[#e8e0fb]">مبلغ درخواست‌های برداشتی که ثبت کرده‌ای و تیم مالی هنوز بررسی نکرده است؛ تا مشخص‌شدن نتیجه، از موجودی قابل برداشتت کم می‌شود.</p>
                </div>
                <span className="shrink-0 text-[11px] font-bold">{formatPrice(b?.reserved || 0)} تومان</span>
              </div>
              <div className="flex items-start justify-between gap-2">
                <div>
                  <p className="text-[11px] font-bold text-slate-100">تسویه‌های تأییدشده</p>
                  <p className="mt-0.5 text-xs leading-6 text-[#e8e0fb]">شامل تسویه‌های تأییدشده، در حال انتقال و واریزشده است؛ تأیید درخواست به‌تنهایی به معنی واریز قطعی نیست. وضعیت هر درخواست را در سوابق ببین.</p>
                </div>
                <span className="shrink-0 text-[11px] font-bold">{formatPrice(b?.paid || 0)} تومان</span>
              </div>
              {returnCharges !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-amber-200">کسر بابت مرجوعی‌ها</p>
                    <p className="mt-0.5 text-xs leading-6 text-[#e8e0fb]">هزینه‌هایی که بابت مرجوعی کالاها از حسابت کسر شده و از موجودی قابل برداشتت کم می‌شود.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold text-amber-300">{formatPrice(returnCharges)} تومان</span>
                </div>
              )}
              {returnClawbacks !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-amber-200">برگشت هزینه کالا از مرجوعی‌های بازپرداخت‌شده</p>
                    <p className="mt-0.5 text-xs leading-6 text-[#e8e0fb]">مبلغ پایه کالاهایی که مشتری برگردانده و پولش به مشتری بازپرداخت شده است؛ از موجودی قابل برداشتت کم می‌شود.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold text-amber-300">{formatPrice(returnClawbacks)} تومان</span>
                </div>
              )}
              {adjustments !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-slate-100">اسناد تعدیل مالی</p>
                    <p className="mt-0.5 text-xs leading-6 text-[#e8e0fb]">تعدیل‌های ثبت‌شده توسط تیم مالی؛ مبلغ مثبت به موجودی قابل برداشتت اضافه و مبلغ منفی از آن کم می‌شود.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold">{formatPrice(adjustments)} تومان</span>
                </div>
              )}
              {storageFees !== 0 && (
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <p className="text-[11px] font-bold text-amber-200">کسر بابت انبارداری</p>
                    <p className="mt-0.5 text-xs leading-6 text-[#e8e0fb]">هزینه نگهداری کالاهای ثبت‌شده در انبار مرکزی (کالاهایی که از مسیر ورود کالا به انبار ثبت شده‌اند) پس از پایان روزهای رایگان؛ ریز آن را در بخش «هزینه انبارداری» پایین همین صفحه می‌بینی.</p>
                  </div>
                  <span className="shrink-0 text-[11px] font-bold text-amber-300">{formatPrice(storageFees)} تومان</span>
                </div>
              )}
              </div>
            </details>
          </section>
          <form onSubmit={submit} noValidate className="min-w-0 space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
            <h3 className="text-base font-extrabold text-[#261b41]">درخواست برداشت</h3>
            {b.available <= 0 && <p className="rounded-xl bg-slate-50 p-3 text-sm leading-6 text-slate-700">فعلاً موجودی قابل برداشت نداری. اگر نتیجهٔ درخواست قبلی را ندیدی، همان مبلغ و مشخصات حساب را دوباره وارد کن.</p>}
            <fieldset disabled={saving} className="min-w-0 space-y-3">
            <label className="block text-sm font-bold text-slate-700">نام صاحب حساب
              <input required maxLength={100} name="holder" value={holder} onChange={event => editField('holder', event.target.value)} aria-invalid={fieldError?.name === 'holder'} aria-describedby={fieldError?.name === 'holder' ? 'supplier-payout-holder-error' : undefined} placeholder="مثلاً: علی رضایی" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus:bg-white focus-visible:ring-2 focus-visible:ring-brand aria-invalid:border-rose-500" />
              {fieldError?.name === 'holder' && <span id="supplier-payout-holder-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            <label className="block text-sm font-bold text-slate-700">شماره شبا
              <span className="block mt-1 text-[10px] font-normal text-slate-500 leading-relaxed">شماره ۲۴ رقمی حساب بانکی که با IR شروع می‌شود؛ روی کارت بانکی یا در اپ بانک می‌توانی پیدایش کنی.</span>
              <input required name="shaba" value={shaba} onChange={event => editField('shaba', event.target.value)} aria-invalid={fieldError?.name === 'shaba'} aria-describedby={fieldError?.name === 'shaba' ? 'supplier-payout-shaba-error' : undefined} placeholder="IR…" dir="ltr" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus:bg-white focus-visible:ring-2 focus-visible:ring-brand aria-invalid:border-rose-500" />
              {fieldError?.name === 'shaba' && <span id="supplier-payout-shaba-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            <label className="block text-sm font-bold text-slate-700">مبلغ برداشت (تومان)
              <input required type="text" inputMode="numeric" name="amount" value={amount} onChange={event => editField('amount', event.target.value)} aria-invalid={fieldError?.name === 'amount'} aria-describedby={fieldError?.name === 'amount' ? 'supplier-payout-amount-error' : undefined} placeholder="چقدر می‌خواهی برداشت کنی؟" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus:bg-white focus-visible:ring-2 focus-visible:ring-brand aria-invalid:border-rose-500" />
              {fieldError?.name === 'amount' && <span id="supplier-payout-amount-error" role="alert" className="mt-1 block text-xs text-rose-700">{fieldError.message}</span>}
            </label>
            </fieldset>
            <button disabled={saving || balance.loading} className="min-h-12 w-full rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 disabled:opacity-50">{saving ? 'در حال ثبت…' : 'ثبت درخواست برداشت'}</button>
            <p className="text-[10px] text-slate-500 leading-relaxed">واریز دستی است و با انتقال بانکی انجام می‌شود: بعد از تأیید درخواستت، تیم مالی پول را واریز می‌کند و شماره پیگیری همین‌جا ثبت می‌شود.</p>
          </form>
        </div>
      )}
      <AccountStatement role="supplier" refresh={statementRefresh} />
      {!fees.error && charges.length > 0 && (
        <>
          <h3 className="text-base font-extrabold text-[#261b41]">هزینه انبارداری</h3>
          <div className="space-y-2">
            {charges.map((charge) => (
              <div key={charge.id} className="rounded-[20px] border border-[#e6e2f0] bg-white p-4 text-sm leading-6 shadow-sm">
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
      {!history.error && (history.data || []).length > 0 && <h3 className="text-base font-extrabold text-[#261b41]">سوابق برداشت</h3>}
      {!history.error && !(history.data || []).length && !history.loading && <SupplierEmptyState icon={<Wallet />} title="هنوز برداشتی نداشته‌ای." description="وقتی موجودی قابل برداشت داشتی، از فرم بالا درخواست برداشت ثبت کن." />}
      {!history.error && (history.data || []).map((item) => (
        <section key={item.id} className="rounded-[20px] border border-[#e6e2f0] bg-white p-4 text-sm shadow-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <strong className="tabular-nums text-[#261b41]">{formatPrice(item.amount)} تومان</strong>
            <span className="rounded-lg bg-brand-soft px-2 py-1 text-xs font-bold text-brand-deep">{statusLabel(item.status)}</span>
          </div>
          <p className="mt-1 text-xs text-slate-600">{new Date(item.created_at).toLocaleDateString('fa-IR')}</p>
          {(item.transfer_reference || (item as any).transferReference) && <p className="text-slate-500">شماره پیگیری: <span dir="ltr">{item.transfer_reference || (item as any).transferReference}</span></p>}
        </section>
      ))}
    </div>
  );
};
