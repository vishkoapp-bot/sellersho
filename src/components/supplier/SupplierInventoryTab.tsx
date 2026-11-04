import React, { useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { supplierApi, SupplierProductDto, SupplierReturnRequestDto, SupplierInventoryLocationDto } from '../../services/supplierApi';
import { useSupplierResource } from './useSupplierResource';
import { SupplierResourceState } from './SupplierResourceState';

// FIX 42: rebuilt to the live bundle SupplierDashboardView-MERGED.js (Zl component).
// Divergences fixed vs the previous candidate tab: wrong h2 (it even said
// "canonical"), missing help box, missing central stock-share box, wrong
// search placeholder, missing empty states, wrong grid labels (live:
// آماده فروش / کنار گذاشته برای سفارش / درخواست برگشت / در حال بررسی
// کیفیت / در راه انبار / هنوز شمرده نشده), raw English statuses in location
// lines (live maps them through Xl) and wrong unreconciled note, a dead
// "adjustment legacy" button (it set state but rendered no form at all),
// wrong returns section (live: «برگرداندن‌هایی که خواسته‌ای» with Persian
// quantities and Yl status labels, no id), and a wrong return modal
// (live: «برگرداندن کالا: …» with labeled fields, «خودم می‌آیم تحویل
// می‌گیرم» / «با پست برمی‌گردد» options, and its own validation/toast
// strings). The stock-share box renders only on a successful
// /supplier/stock-share response, exactly like live's conditional box;
// the candidate backend still returns the U07 stub error for that route,
// so the box stays hidden until a real stock-share source exists —
// matching live's error branch, not a UI divergence.
// NOTE: live's number inputs use its __sepF/__sepW Persian formatting;
// the small helpers below reproduce it (Persian digits + ٬ separators).
const STATUS_LABELS: Record<string, string> = {
  sellable: 'آماده فروش',
  sales_reserved: 'کنار گذاشته برای سفارش',
  return_requested: 'درخواست برگشت',
  quarantine: 'در حال بررسی کیفیت',
  in_transit: 'در راه انبار',
};

const RETURN_STATUS_LABELS: Record<string, string> = {
  requested: 'ثبت شده',
  pending: 'ثبت شده',
  approved: 'قبول شد',
  rejected: 'رد شد',
  completed: 'انجام شد',
};

export const SupplierInventoryTab: React.FC = () => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const resource = useSupplierResource(supplierApi.getProducts);
  const [query, setQuery] = useState('');
  const [editing, setEditing] = useState<SupplierProductDto | null>(null);
  const [stock, setStock] = useState('');
  const [saving, setSaving] = useState(false);
  const [returns, setReturns] = useState<SupplierReturnRequestDto[]>([]);
  const [locations, setLocations] = useState<SupplierInventoryLocationDto[]>([]);
  const [returning, setReturning] = useState<SupplierProductDto | null>(null);
  const [returnQuantity, setReturnQuantity] = useState('');
  const [returnMethod, setReturnMethod] = useState<'supplier_collect' | 'postal'>('supplier_collect');
  const [returnReason, setReturnReason] = useState('');
  const [returnIdempotencyKeys, setReturnIdempotencyKeys] = useState<Record<string, string>>({});
  const [shareData, setShareData] = useState<{ centralStockShare: number | null; centralUnits: number; supplierUnits: number } | null>(null);
  const [shareLoaded, setShareLoaded] = useState(false);

  useEffect(() => {
    Promise.all([supplierApi.getReturns(), supplierApi.getInventoryLocations()]).then(([returnItems, inventory]) => {
      setReturns(returnItems);
      setLocations(inventory.inventory || []);
    }).catch(() => undefined);
    supplierApi.getStockShare().then((data: any) => {
      setShareData(data);
      setShareLoaded(true);
    }).catch(() => {
      setShareLoaded(true);
    });
  }, []);

  const formatNumber = (digits: string): string => {
    if (digits === '') return '';
    const grouped = digits.replace(/\B(?=(\d{3})+(?!\d))/g, '٬');
    return toPersianDigits(grouped);
  };

  const parseDigits = (value: string): string => {
    const persian = '۰۱۲۳۴۵۶۷۸۹';
    let out = '';
    for (const ch of value) {
      const pi = persian.indexOf(ch);
      if (pi >= 0) out += String(pi);
      else if (ch >= '0' && ch <= '9') out += ch;
    }
    return out;
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!editing || saving) return;
    const value = Number(stock);
    if (stock.trim() === '' || !Number.isSafeInteger(value) || value < 0) {
      error('موجودی را با عدد وارد کن (نمی‌تواند منفی باشد).');
      return;
    }
    setSaving(true);
    try {
      await supplierApi.updateStock(editing.id, value);
      setEditing(null);
      success('موجودی ذخیره شد.');
      await resource.reload();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : 'ذخیره انجام نشد.');
    } finally {
      setSaving(false);
    }
  };

  const items = (resource.data || []).filter(item => `${item.title} ${item.id}`.toLowerCase().includes(query.toLowerCase()));
  const grouped = locations.reduce<Record<string, SupplierInventoryLocationDto[]>>((groups, location) => {
    (groups[location.productId] ||= []).push(location);
    return groups;
  }, {});
  const canonicalSellable = (productId: string) => (grouped[productId] || []).filter((location) => location.status === 'sellable').reduce((sum, location) => sum + location.quantity, 0);

// crypto.randomUUID exists only in secure contexts (HTTPS/localhost); the live and preview origins are plain HTTP, so fall back instead of throwing. Uniqueness is all the call sites need.
const newUuid = (): string => {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch { /* fall through to the fallback */ }
  return 'id-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2);
};

  const openReturnForm = (item: SupplierProductDto) => {
    setReturning(item);
    setReturnQuantity('1');
    setReturnIdempotencyKeys((current) => current[item.id] ? current : { ...current, [item.id]: `return-${item.id}-${newUuid()}` });
  };

  const submitReturn = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!returning) return;
    const quantity = Number(returnQuantity);
    const max = canonicalSellable(returning.id);
    if (!Number.isSafeInteger(quantity) || quantity <= 0 || quantity > max || !returnReason.trim()) {
      error('تعداد را درست وارد کن (از موجودی آماده فروش بیشتر نباشد) و علت برگرداندن را بنویس.');
      return;
    }
    const idempotencyKey = returnIdempotencyKeys[returning.id];
    if (!idempotencyKey) {
      error('فرم را ببند و دوباره باز کن.');
      return;
    }
    try {
      const created = await supplierApi.requestReturn(returning.id, quantity, returnMethod, returnReason, idempotencyKey);
      setReturns([created, ...returns.filter((item) => item.id !== created.id)]);
      setReturning(null);
      setReturnQuantity('');
      setReturnReason('');
      success('درخواست برگرداندن کالا ثبت شد.');
    } catch (cause) {
      error(cause instanceof Error ? cause.message : 'ثبت درخواست انجام نشد.');
    }
  };

  return (
    <div className="space-y-3 pb-6">
      <h2 className="text-sm font-bold">موجودی کالاهای تو</h2>
      <div className="rounded-2xl border border-slate-100 bg-white p-4 text-[11px] leading-relaxed text-slate-600">اینجا برای هر کالا می‌بینی چند تا از آن کجاست: آماده فروش، کنار گذاشته برای سفارش، یا در راه انبار. موجودی را هم از همین‌جا عوض می‌کنی.</div>
      {shareLoaded && shareData && (
        <div className="rounded-2xl border bg-white p-3 text-[11px] leading-5">
          <div className="flex items-center justify-between gap-2">
            <span className="font-bold text-slate-800">سهم انبار مرکزی</span>
            {shareData.centralStockShare !== null ? (
              <span className="font-extrabold text-slate-900 font-mono">{toPersianDigits(Math.round(shareData.centralStockShare * 100))}٪</span>
            ) : (
              <span className="text-slate-400">بدون موجودی قابل فروش</span>
            )}
          </div>
          <p className="text-slate-500">{toPersianDigits(shareData.centralUnits)} واحد در انبار مرکزی • {toPersianDigits(shareData.supplierUnits)} واحد نزد شما</p>
          <p className="text-slate-400 text-[10px]">کالایی که در انبار مرکزی دپو شده باشد، با «ارسال سریع» فروش می‌رود و امتیازت بالاتر می‌رود.</p>
        </div>
      )}
      <input
        aria-label="جستجوی موجودی"
        value={query}
        onChange={event => setQuery(event.target.value)}
        placeholder="اسم کالا را بنویس"
        className="w-full rounded-xl border p-2.5 min-h-[44px] text-xs"
      />
      <SupplierResourceState loading={resource.loading} error={resource.error} retry={resource.reload} />
      {items.length === 0 && !resource.loading && !resource.error && (
        <div className="rounded-2xl border bg-white p-8 text-center space-y-2">
          <p className="text-xs font-bold text-slate-600">{query.trim() ? 'کالایی با این اسم پیدا نشد' : 'هنوز کالای تأییدشده‌ای نداری'}</p>
          <p className="text-[10px] text-slate-400 leading-relaxed">{query.trim() ? 'اسم دیگری را امتحان کن.' : 'وقتی مدیر کالایت را تأیید کند، موجودی‌اش اینجا می‌آید.'}</p>
        </div>
      )}
      {items.map(item => {
        const itemLocations = grouped[item.id] || [];
        const sums = itemLocations.reduce<Record<string, number>>((result, location) => {
          result[location.status] = (result[location.status] || 0) + location.quantity;
          return result;
        }, {});
        return (
          <section key={item.id} className="space-y-2 rounded-2xl border bg-white p-3">
            <h3 className="text-xs font-bold">{item.title}</h3>
            <div className="grid grid-cols-2 gap-1 text-[10px] text-slate-600">
              <span>آماده فروش: <b>{sums.sellable || 0}</b></span>
              <span>کنار گذاشته برای سفارش: <b>{sums.sales_reserved || 0}</b></span>
              <span>درخواست برگشت: <b>{sums.return_requested || 0}</b></span>
              <span>در حال بررسی کیفیت: <b>{sums.quarantine || 0}</b></span>
              <span>در راه انبار: <b>{sums.in_transit || 0}</b></span>
              <span>هنوز شمرده نشده: <b>{itemLocations.filter(location => !location.reconciled).reduce((sum, location) => sum + location.quantity, 0)}</b></span>
            </div>
            {itemLocations.map(location => (
              <p key={`${location.locationKey}-${location.status}`} className="text-[10px] text-slate-500">
                {location.locationKey} · {STATUS_LABELS[location.status] || location.status} · {toPersianDigits(location.quantity)} عدد {location.reconciled ? '' : '· انبار هنوز ثبت نکرده'}
              </p>
            ))}
            <div className="flex gap-2">
              <button
                type="button"
                disabled={saving}
                onClick={() => { setEditing(item); setStock(String(item.stock)); }}
                className="rounded-lg bg-brand-soft px-3 py-2 min-h-[40px] text-xs font-bold text-brand-deep"
              >
                عوض‌کردن موجودی
              </button>
              <button
                type="button"
                onClick={() => openReturnForm(item)}
                className="rounded-lg bg-amber-50 px-3 py-2 min-h-[40px] text-xs font-bold text-amber-700"
              >
                برگرداندن کالا
              </button>
            </div>
            {editing?.id === item.id && (
              <form onSubmit={save} className="flex items-end gap-2 rounded-xl bg-brand-soft/60 border border-brand-border p-2">
                <label className="flex-1 text-[10px] font-bold text-slate-600">
                  موجودی جدید (عدد)
                  <input
                    autoFocus
                    type="text"
                    inputMode="numeric"
                    min="0"
                    value={formatNumber(stock)}
                    onChange={event => setStock(parseDigits(event.target.value))}
                    className="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs bg-white"
                  />
                </label>
                <button
                  disabled={saving}
                  className="rounded-lg bg-brand-deep px-3 py-2 min-h-[40px] text-xs font-bold text-white shrink-0"
                >
                  {saving ? 'ذخیره…' : 'ذخیره موجودی'}
                </button>
                <button
                  type="button"
                  onClick={() => setEditing(null)}
                  className="rounded-lg bg-white border px-3 py-2 min-h-[40px] text-xs shrink-0"
                >
                  انصراف
                </button>
              </form>
            )}
          </section>
        );
      })}
      {returns.length > 0 && (
        <section className="rounded-xl border bg-white p-3 space-y-2">
          <h3 className="text-xs font-bold">برگرداندن‌هایی که خواسته‌ای</h3>
          {returns.map(item => (
            <p key={item.id} className="text-[10px] text-slate-600">
              {toPersianDigits(item.requestedQuantity)} عدد · {RETURN_STATUS_LABELS[item.status] || item.status}
            </p>
          ))}
        </section>
      )}
      {returning && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 p-3">
          <form onSubmit={submitReturn} className="w-full max-w-sm space-y-3 rounded-2xl bg-white p-4">
            <h3 className="text-sm font-bold">برگرداندن کالا: {returning.title}</h3>
            <label className="block text-[11px] font-bold text-slate-600">
              چند عدد برمی‌گردد؟
              <input
                aria-label="تعداد برگشتی"
                type="text"
                inputMode="numeric"
                min="1"
                max={canonicalSellable(returning.id)}
                value={formatNumber(returnQuantity)}
                onChange={event => setReturnQuantity(parseDigits(event.target.value))}
                className="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs font-normal"
              />
            </label>
            <label className="block text-[11px] font-bold text-slate-600">
              چطور برمی‌گردد؟
              <select
                value={returnMethod}
                onChange={event => setReturnMethod(event.target.value as 'supplier_collect' | 'postal')}
                className="mt-1 w-full rounded-lg border p-2 min-h-[40px] text-xs font-normal"
              >
                <option value="supplier_collect">خودم می‌آیم تحویل می‌گیرم</option>
                <option value="postal">با پست برمی‌گردد</option>
              </select>
            </label>
            <label className="block text-[11px] font-bold text-slate-600">
              چرا برمی‌گردد؟
              <textarea
                aria-label="علت برگرداندن"
                value={returnReason}
                onChange={event => setReturnReason(event.target.value)}
                className="mt-1 w-full rounded-lg border p-2 text-xs font-normal"
                placeholder="علت برگرداندن را کوتاه بنویس"
              />
            </label>
            <div className="flex gap-2">
              <button className="flex-1 rounded-lg bg-amber-600 p-2 min-h-[44px] text-xs font-bold text-white">ثبت برگرداندن</button>
              <button type="button" onClick={() => setReturning(null)} className="flex-1 rounded-lg border p-2 min-h-[44px] text-xs">انصراف</button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
};
