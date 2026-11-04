import React, { useState } from 'react';
import { supplierApi } from '../../services/supplierApi';
import { useToast } from '../../context/ToastContext';
import { useStore } from '../../context/StoreContext';
import { isValidIranianSheba } from '../../utils/validators';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';
import { Clock } from 'lucide-react';

// Live settlement tab (on): supplier balance, return-window lock, manual
// bank-transfer withdrawal requests, storage-fee charges, payout history.
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
    if (!Number.isSafeInteger(value) || value <= 0 || value > ((balance.data as any)?.available || 0)) {
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

  const b: any = balance.data || {};
  const locked = b.locked_in_return_window ?? b.lockedInReturnWindow ?? b.pendingRelease ?? b.pending_release ?? b.pendingReserve ?? b.pending_reserve ?? null;
  const showLocked = locked !== null;
  const returnCharges = b.returnCharges ?? b.return_charges ?? null;
  const net = b.net_balance ?? b.netBalance ?? null;
  const storageFees = b.storageFees ?? b.storage_fees ?? null;
  const charges = (fees.data as any)?.charges || [];

  return (
    <div className="space-y-3 pb-6">
      <h2 className="text-sm font-bold">تسویه‌حساب و برداشت پول</h2>
      <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">
        <p>پول فروش کالاهایت اینجاست. وقتی مهلت مرجوعی سفارش تمام شود، پولش آزاد می‌شود و می‌توانی برداشتش کنی.</p>
        <p className="mt-2"><b>چرا بعضی پول‌ها هنوز آزاد نشده‌اند؟</b> مشتری تا چند روز حق برگرداندن کالا را دارد. تا آن مهلت تمام نشود، پول آن سفارش نگه داشته می‌شود؛ بعد خودش کامل آزاد می‌شود.</p>
      </div>
      <SupplierResourceState loading={balance.loading || history.loading} error={balance.error || history.error} retry={() => { void balance.reload(); void history.reload(); }} />
      {!balance.error && (
        <>
          <section className="rounded-2xl bg-slate-900 p-4 text-white">
            <span className="text-xs text-slate-300">پولی که می‌توانی برداشت کنی</span>
            <strong className="mt-1 block text-xl text-emerald-300">{formatPrice(b.available || 0)} تومان</strong>
            <p className="mt-2 text-[10px] text-slate-400">کل فروخته‌شده: {formatPrice(b.earned || 0)} · در راه (مهلت مرجوعی): {formatPrice(b.reserved || 0)} · تا حالا واریزشده: {formatPrice(b.paid || 0)}</p>
            {(returnCharges !== null || net !== null) && (
              <div className="mt-3 border-t border-white/10 pt-2 space-y-1 text-[10px] leading-relaxed">
                {returnCharges !== null && returnCharges > 0 && <p className="text-slate-300">هزینه ارسال مرجوعی معیوب (از حساب شما پرداخت شده): <span className="font-bold text-amber-300">{formatPrice(returnCharges)} تومان</span></p>}
                {storageFees !== null && storageFees > 0 && <p className="text-slate-300">هزینه انبارداری کالاهای دپوشده در انبار مرکزی (از حساب شما پرداخت شده): <span className="font-bold text-amber-300">{formatPrice(storageFees)} تومان</span></p>}
                {net !== null && <p className="text-slate-300">تراز خالص حساب: <span className={`font-bold ${net < 0 ? 'text-rose-300' : 'text-emerald-300'}`}>{formatPrice(net)} تومان</span>{net < 0 && ' — حساب شما منفی است؛ هزینه ارسال مرجوعی‌های معیوب از آن کسر شده و با فروش‌های بعدی جبران می‌شود.'}</p>}
              </div>
            )}
          </section>
          {showLocked && (
            <section className="rounded-2xl border border-amber-200/80 bg-amber-50 p-4">
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <div className="w-8 h-8 rounded-xl bg-amber-100 text-amber-700 flex items-center justify-center shrink-0"><Clock className="w-4 h-4" strokeWidth={1.5} /></div>
                  <span className="text-xs font-bold text-slate-800">در انتظار پایان مهلت مرجوعی</span>
                </div>
                {locked !== null && locked > 0 ? <strong className="text-base text-amber-700">{formatPrice(locked)} تومان</strong> : <span className="text-[11px] font-bold text-slate-400">موردی نیست</span>}
              </div>
              <p className="mt-2 text-[10px] text-slate-500 leading-relaxed">سود سفارش تا پایان مهلت مرجوعی مشتری نگه داشته می‌شود و سپس کامل آزاد می‌شود.</p>
            </section>
          )}
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
            {charges.map((charge: any) => (
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
      {!history.error && !(history.data || []).length && !history.loading && <p className="rounded-2xl border bg-white p-4 text-[11px] leading-5 text-slate-500">هنوز برداشتی نداشته‌ای. وقتی پول آزادشده داشتی، از فرم بالا اولین برداشتت را ثبت کن.</p>}
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
