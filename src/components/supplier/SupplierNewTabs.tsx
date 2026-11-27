import { SupplierSectionHeading } from './SupplierSectionHeading';
import { SupplierEmptyState, SupplierResourceState } from './SupplierResourceState';
import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { supplierApi } from '../../services/supplierApi';
import { tokenStore } from '../../services/apiClient';
import { newUuid } from '../../services/uuid';
import { ProvinceCityPicker, toLatinDigits } from '../ProvinceCityPicker';
import { SupplierSLACard } from './SupplierSLACard';
import { ContextSupportButton } from '../support/ContextSupportButton';

import { useToast } from "../../context/ToastContext";import { authApi } from '../../services/authApi';
import { BarChart3, Banknote, Package, ShoppingBag, TrendingUp, RotateCcw, Warehouse, Truck, User, Info, Plus, Pencil, CheckCircle2, Clock, XCircle, Send, FileText, Loader2 } from 'lucide-react';

const detailSummaryClass = 'min-h-11 cursor-pointer rounded-lg py-2.5 text-sm font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand';
const serviceFieldClass = 'mt-1 min-h-11 w-full rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm focus-visible:ring-2 focus-visible:ring-brand';

// FIX 17: supplier tabs added to reach live's 17-tab structure.
// Analytics / customer-returns / my-warehouses / shipping-methods /
// profile are wired to real GET endpoints that exist in main.go.
// Demand-insight is wired to GET /supplier/search-insight (real tab in
// SupplierDemandInsightTab.tsx, re-exported below). Contract has no GET
// endpoint in this branch, so it stays an honest placeholder.

export const SupplierPlaceholderTab: React.FC<{ title: string; description?: string }> = ({ title, description }) => (
  <div className="space-y-3">
    <div className="bg-white rounded-2xl border border-slate-200 p-4">
      <h3 className="text-xs font-bold text-slate-800">{title}</h3>
      <p className="text-[11px] text-slate-500 mt-1">
        {description || 'این بخش هنوز پیاده‌سازی نشده است.'}
      </p>
    </div>
  </div>
);

function useList(fetch: () => Promise<any[]>) {
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  useEffect(() => {
    let on = true;
    fetch()
      .then((list) => { if (on) setItems(list); })
      .catch((err: any) => { if (on) setLoadError(err?.message || 'خطا در دریافت داده'); })
      .finally(() => { if (on) setLoading(false); });
    return () => { on = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return { items, loading, loadError };
}

function ListShell({ title, icon, loading, loadError, empty, children }: {
  title: string; icon: React.ReactNode; loading: boolean; loadError: string | null; empty: boolean; children: React.ReactNode;
}) {
  return (
    <div className="space-y-3">
      <div className="bg-white rounded-2xl border border-slate-200 p-4 flex items-center gap-2">
        {icon}
        <h3 className="text-xs font-bold text-slate-800">{title}</h3>
      </div>
      {loading && <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">در حال بارگذاری…</div>}
      {loadError && <div className="bg-white rounded-2xl border border-rose-200 p-4 text-xs text-rose-600">{loadError}</div>}
      {!loading && !loadError && empty && (
        <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">موردی یافت نشد.</div>
      )}
      {!loading && !loadError && !empty && children}
    </div>
  );
}

const pick = (item: any, keys: string[]): string => {
  for (const k of keys) {
    const v = item?.[k];
    if (v !== undefined && v !== null && v !== '') return String(v);
  }
  return '—';
};

// FIX 34: supplier analytics tab rebuilt to the live tab spec
// (title, help text, empty state, four stat cards, 30 day base
// amount chart, returns and quality section, per product table).
// The help box renders intro plus help title and text statically;
// live help box expand behavior is unproven.
const formatFaDay = (t: string) => {
  const d = new Date((t || "").slice(0, 10) + "T12:00:00");
  return isNaN(d.getTime()) ? "" : new Intl.DateTimeFormat("fa-IR", { day: "numeric", month: "short" }).format(d);
};

const AnalyticsChart: React.FC<{ daily: any[]; formatPrice: (n: number) => string; toPersianDigits: (n: number | string) => string }> = ({ daily, formatPrice, toPersianDigits }) => {
  const max = Math.max(1, ...daily.map((x) => x.revenue));
  const k = daily.length || 1;
  const T = 306;
  const f = 108;
  const v = T / k;
  const E = Math.max(2, v * 0.62);
  return (
    <div dir="ltr" className="w-full">
      <svg viewBox="0 0 320 140" className="w-full h-auto" role="img" aria-label="نمودار مبلغ پایه فروش ۳۰ روز گذشته">
        {[0.25, 0.5, 0.75, 1].map((x) => (
          <line key={x} x1={6} x2={312} y1={10 + f * (1 - x)} y2={10 + f * (1 - x)} stroke="#e2e8f0" strokeWidth={1} strokeDasharray="3 3" />
        ))}
        {daily.map((x, m) => {
          const y = (x.revenue / max) * f;
          const S = 6 + m * v + (v - E) / 2;
          const R = 10 + f - y;
          return (
            <g key={x.date}>
              <rect x={S} y={R} width={E} height={Math.max(y, x.revenue > 0 ? 2 : 0)} rx={E / 2} fill={x.revenue > 0 ? "#94a3b8" : "#f1f5f9"}>
                <title>{`${formatFaDay(x.date)} — ${formatPrice(x.revenue)} تومان • ${toPersianDigits(x.orders)} سفارش`}</title>
              </rect>
              {m % 6 === 0 && (<text x={S + E / 2} y={134} textAnchor="middle" fontSize={8} fill="#94a3b8">{formatFaDay(x.date)}</text>)}
            </g>
          );
        })}
        <text x={312} y={8} textAnchor="end" fontSize={8} fill="#94a3b8">{formatPrice(max)}</text>
      </svg>
    </div>
  );
};

export const SupplierAnalyticsTab: React.FC = () => {
  const { toPersianDigits, formatPrice } = useStore();
  const [data, setData] = useState<any | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const load = () => {
    setLoading(true);
    setLoadError(null);
    supplierApi.getAnalytics()
      .then((d: any) => { setData(d); })
      .catch((err: any) => { setLoadError(err?.message || "خطا در دریافت تحلیل فروش"); })
      .finally(() => { setLoading(false); });
  };
  useEffect(() => { load(); }, []);
  const hasSales = !!data && data.orderCount > 0;
  const cards = data ? [
    { icon: Banknote, label: "مبلغ پایه فروش کل", value: formatPrice(data.totalBaseAmount), unit: "تومان" },
    { icon: Package, label: "تعداد فروخته‌شده", value: toPersianDigits(data.totalQuantity), unit: "عدد" },
    { icon: ShoppingBag, label: "تعداد سفارش‌ها", value: toPersianDigits(data.orderCount), unit: "" },
    { icon: RotateCcw, label: "مرجوعی نهایی‌شده", value: toPersianDigits(data.returns.totalReturned), unit: "عدد" },
  ] : [];
  const percent = (r: number) => toPersianDigits(`${Math.round(r * 1e3) / 10}٪`);
  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="تحلیل فروش" description="روند فروش، کالاهای پرفروش و نتیجهٔ مرجوعی‌ها را بررسی کن." icon={<BarChart3 />} />
      <SupplierResourceState loading={loading} error={loadError ?? ''} retry={load} />
      {!loading && !loadError && data && !hasSales && (
        <SupplierEmptyState icon={<BarChart3 />} title="هنوز فروشی ثبت نشده است." description="پس از اولین سفارش پرداخت‌شده، آمار فروش و روند مرجوعی‌ها اینجا نمایش داده می‌شود." />
      )}
      {!loading && !loadError && data && hasSales && (<>
        <div className="grid grid-cols-2 gap-2.5 lg:grid-cols-4">
          {cards.map((u) => (
            <div key={u.label} className="bg-white rounded-2xl border border-slate-100 p-3.5">
              <div className="flex items-center gap-1.5 mb-2">
                <span className="w-7 h-7 rounded-xl bg-brand-soft border border-brand-border flex items-center justify-center shrink-0"><u.icon className="w-3.5 h-3.5 text-brand-deep" strokeWidth={1.5} /></span>
                <span className="text-xs font-bold text-slate-700">{u.label}</span>
              </div>
              <div className="break-words text-base font-extrabold text-slate-900">{u.value}{u.unit && (<span className="ms-1 text-xs font-normal text-slate-600">{u.unit}</span>)}</div>
            </div>
          ))}
        </div>
        <section className="bg-white rounded-2xl border border-slate-100 p-4">
          <div className="flex items-center justify-between mb-3">
            <h3 className="flex items-center gap-1.5 text-sm font-extrabold text-slate-800"><TrendingUp aria-hidden="true" className="size-4 text-brand-deep" strokeWidth={1.5} />مبلغ پایه در ۳۰ روز گذشته</h3>
            <span className="text-xs text-slate-600">تومان</span>
          </div>
          <AnalyticsChart daily={data.daily} formatPrice={formatPrice} toPersianDigits={toPersianDigits} />
          <details className="mt-2 border-t border-slate-100">
            <summary className={detailSummaryClass}>فروش روزانه</summary>
            <ul className="divide-y divide-slate-100 text-sm text-slate-700">
              {data.daily.map((day: { date: string; revenue: number; orders: number }) => (
                <li key={day.date} className="flex flex-wrap justify-between gap-x-3 py-2">
                  <time dateTime={day.date}>{formatFaDay(day.date)}</time>
                  <span>{formatPrice(day.revenue)} تومان · {toPersianDigits(day.orders)} سفارش</span>
                </li>
              ))}
            </ul>
            <p className="mt-2 text-xs leading-6 text-slate-600">مبلغ پایه بر اساس قیمت عمده، یعنی مبنای تسویه‌حساب تو با پلتفرم، محاسبه می‌شود.</p>
          </details>
        </section>
        <section className="bg-white rounded-2xl border border-slate-100 p-4 space-y-3">
          <h3 className="flex items-center gap-1.5 text-sm font-extrabold text-slate-800"><RotateCcw aria-hidden="true" className="size-4 text-brand-deep" strokeWidth={1.5} />مرجوعی و نتیجهٔ بررسی کیفیت</h3>
          <div className="grid grid-cols-2 gap-2 text-center">
            <div className="rounded-xl bg-emerald-50 border border-emerald-100 p-2.5">
              <CheckCircle2 className="w-4 h-4 text-emerald-600 mx-auto mb-1" strokeWidth={1.5} />
              <p className="text-sm font-extrabold text-emerald-800">{toPersianDigits(data.returns.qcOk)}</p>
              <p className="text-[10px] text-emerald-700">سالم برگشته</p>
            </div>
            <div className="rounded-xl bg-rose-50 border border-rose-100 p-2.5">
              <XCircle className="w-4 h-4 text-rose-500 mx-auto mb-1" strokeWidth={1.5} />
              <p className="text-sm font-extrabold text-rose-800">{toPersianDigits(data.returns.qcDefective)}</p>
              <p className="text-[10px] text-rose-700">معیوب</p>
            </div>
          </div>
          <div>
            <p className="text-[11px] font-bold text-slate-700 mb-1.5">علت‌های پرتکرار مرجوعی</p>
            {(data.returns.topReasons || []).length === 0 ? (<p className="text-[11px] text-slate-400">هنوز علت مرجوعی نهایی‌شده‌ای ثبت نشده است.</p>) : (
              <ul className="space-y-1">
                {(data.returns.topReasons || []).map((u: any) => (
                  <li key={u.reason} className="flex items-center justify-between rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px]">
                    <span className="text-slate-700">{u.reason}</span>
                    <span className="font-bold text-slate-500">{toPersianDigits(u.count)} بار</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>
        {(data.products || []).length > 0 && (
          <section className="bg-white rounded-2xl border border-slate-100 p-4">
            <h3 id="supplier-analytics-products" className="mb-3 text-sm font-extrabold text-slate-800">فروش و موجودی به تفکیک کالا</h3>
            <div role="region" aria-labelledby="supplier-analytics-products" tabIndex={0} className="overflow-x-auto rounded-md focus-visible:ring-2 focus-visible:ring-brand">
              <table className="w-full text-[11px] min-w-[560px]">
                <thead><tr className="text-slate-400 border-b border-slate-100">
                  <th className="text-right font-bold py-2 pl-2">کالا</th>
                  <th className="font-bold py-2 px-2">فروش (عدد)</th>
                  <th className="font-bold py-2 px-2">مبلغ پایه (تومان)</th>
                  <th className="font-bold py-2 px-2">مرجوعی</th>
                  <th className="font-bold py-2 px-2">موجودی</th>
                  <th className="font-bold py-2 pr-2">روزهای باقی‌مانده</th>
                </tr></thead>
                <tbody className="divide-y divide-slate-50">
                  {(data.products || []).map((u: any) => {
                    const high = u.returnRate > 0.1;
                    const low = u.daysLeft !== null && u.daysLeft < 7;
                    return (
                      <tr key={u.productId}>
                        <td className="py-2.5 pl-2 font-bold text-slate-800">{u.name}</td>
                        <td className="py-2.5 px-2 text-center text-slate-600">{toPersianDigits(u.quantity)}</td>
                        <td className="py-2.5 px-2 text-center text-slate-600">{formatPrice(u.baseAmount)}</td>
                        <td className={`py-2.5 px-2 text-center ${high ? "text-rose-600 font-extrabold" : "text-slate-600"}`}>{percent(u.returnRate)}{high && (<span className="block text-[9px] font-bold">بالا است</span>)}</td>
                        <td className="py-2.5 px-2 text-center text-slate-600">{toPersianDigits(u.stock)}</td>
                        <td className={`py-2.5 pr-2 text-center ${low ? "text-rose-600 font-extrabold" : "text-slate-600"}`}>{u.daysLeft === null ? "—" : toPersianDigits(u.daysLeft)}{low && (<span className="block text-[9px] font-bold">رو به اتمام</span>)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <p className="mt-2 text-xs leading-6 text-slate-600">«روزهای باقی‌مانده» برآورد مدت ماندگاری موجودی با سرعت فروش ۳۰ روز اخیر است. برای کالاهایی که در این مدت فروش نداشته‌اند، «—» نمایش داده می‌شود.</p>
          </section>
        )}
      </>)}
    </div>
  );
};

// FIX 44: rebuilt from the live bundle SupplierDashboardView-MERGED.js (An component).
// Live's tab: h2 + long help text, xe loading/error, emerald empty state,
// one section per return (orderNumber + Sn status badge + violet returnCode
// badge + fa-IR date, reason, destination line, return-shipping bearer
// boxes, per-return settlement notes from /supplier/return-shipping-charges
// grouped by return_id), and per item: product row + la QC badge,
// back-request block (Cn status badge + count + Pn timeline + tracking +
// review reason + cancel/confirm buttons), not-received note, and the
// back-request form (max + reason, live's validation/success/error
// strings) wired to live's POST paths. The previous candidate tab was a
// thin generic list (order/status/reason/tracking only, no bearer boxes,
// no QC badges, no back-request flow at all).
// NOTES: Sn's completed label reads «تکمیل شد» + mojibake in the bundle
// dump, so «تکمیل شده» is used. Destination fields render only when the
// backend join finds a warehouse, exactly like live's conditional. The
// help box is rendered statically (live's ue expand behaviour unproven),
// same treatment as Fixes 34/35/37/42.
const RETURN_STATUS_LABELS: Record<string, string> = { requested: "در انتظار بررسی", approved: "تأیید شده", rejected: "رد شده", received: "تحویل گرفته شده", refunded: "وجه برگشت خورده", completed: "تکمیل شده", cancelled: "لغو شده" };
const QC_BADGES: Record<string, { text: string; cls: string }> = { pending: { text: "در انتظار تحویل", cls: "bg-slate-100 text-slate-600" }, ok: { text: "سالم — قابل فروش مجدد", cls: "bg-emerald-50 text-emerald-700" }, defective: { text: "معیوب", cls: "bg-red-50 text-red-700" } };
const BACK_STATUS_LABELS: Record<string, string> = { requested: "در انتظار بررسی انبار", approved: "تأیید شد — در انتظار ارسال", rejected: "رد شد", shipped_back: "ارسال شد به سمت شما", completed: "تحویل گرفتید", cancelled: "لغو شد" };
const BACK_STATUS_CLASSES: Record<string, string> = { requested: "bg-amber-50 text-amber-700", approved: "bg-sky-50 text-sky-700", rejected: "bg-red-50 text-red-700", shipped_back: "bg-violet-50 text-violet-700", completed: "bg-emerald-50 text-emerald-700", cancelled: "bg-slate-100 text-slate-500" };
export function createSupplierReturnBackSubmitter() {
  let pending: { payload: string; generation: number; key: string } | undefined;
  return async (returnItemId: string, quantity: number, reason: string) => {
    const generation = tokenStore.getGeneration();
    const payload = JSON.stringify([returnItemId, quantity, reason]);
    if (!pending || pending.payload !== payload || pending.generation !== generation) {
      pending = { payload, generation, key: newUuid() };
    }
    const operation = pending;
    const result = await supplierApi.requestCustomerReturnBack(returnItemId, quantity, reason, operation.key);
    if (tokenStore.getGeneration() !== generation) throw new Error("حساب کاربری در زمان ثبت درخواست تغییر کرد.");
    if (!result || typeof result !== 'object' || !('id' in result)
      || typeof result.id !== 'string' || !result.id
      || !('returnItemId' in result) || result.returnItemId !== returnItemId
      || !('quantity' in result) || result.quantity !== quantity
      || !('reason' in result) || result.reason !== reason) {
      throw new Error("پاسخ ثبت درخواست عودت کامل نبود. دوباره تلاش کن.");
    }
    if (pending === operation) pending = undefined;
  };
}
export const SupplierCustomerReturnsTab: React.FC = () => {
  const { toPersianDigits, formatPrice } = useStore();
  const { success, error } = useToast();
  const [returns, setReturns] = useState<any[]>([]);
  const [charges, setCharges] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [openFormId, setOpenFormId] = useState("");
  const [formQty, setFormQty] = useState("");
  const [formReason, setFormReason] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [requestReturnBack] = useState(createSupplierReturnBackSubmitter);
  const [busyBackId, setBusyBackId] = useState("");
  const load = () => {
    setLoading(true);
    setLoadError(null);
    Promise.all([supplierApi.getCustomerReturns(), supplierApi.getReturnShippingCharges()])
      .then(([rs, ch]) => { setReturns(rs); setCharges(ch); })
      .catch((err: any) => { setLoadError(err?.message || "خطا در دریافت مرجوعی‌ها"); })
      .finally(() => { setLoading(false); });
  };
  useEffect(() => { load(); }, []);
  const chargesByReturn = new Map<string, any[]>();
  for (const c of charges) {
    const rid = c.return_id || c.returnId;
    if (!rid) continue;
    const list = chargesByReturn.get(rid) || [];
    list.push(c);
    chargesByReturn.set(rid, list);
  }
  const formatFaDate = (t: string) => {
    if (!t) return "";
    const d = new Date(t);
    return Number.isNaN(d.getTime()) ? t : d.toLocaleDateString("fa-IR");
  };
  const submitBack = async (e: React.FormEvent, item: any, maxQty: number) => {
    e.preventDefault();
    const qty = Number(toLatinDigits(formQty));
    if (!Number.isSafeInteger(qty) || qty <= 0 || qty > maxQty || !formReason.trim() || submitting) {
      error(`تعداد را درست وارد کن (حداکثر ${toPersianDigits(maxQty)} عدد) و دلیل درخواست را بنویس.`);
      return;
    }
    setSubmitting(true);
    try {
      await requestReturnBack(item.returnItemId, qty, formReason.trim());
      setOpenFormId("");
      setFormQty("");
      setFormReason("");
      success("درخواست عودت کالا ثبت شد؛ انبار بررسی می‌کند.");
      load();
    } catch (err: any) { error(err?.message || "ثبت درخواست انجام نشد."); }
    finally { setSubmitting(false); }
  };
  const cancelBack = async (back: any) => {
    if (busyBackId) return;
    setBusyBackId(back.id);
    try {
      await supplierApi.cancelReturnBack(back.id, "لغو توسط تأمین‌کننده");
      success("درخواست عودت لغو شد.");
      load();
    } catch (err: any) { error(err?.message || "لغو درخواست انجام نشد."); }
    finally { setBusyBackId(""); }
  };
  const completeBack = async (back: any) => {
    if (busyBackId) return;
    setBusyBackId(back.id);
    try {
      await supplierApi.completeReturnBack(back.id);
      success("دریافت کالا ثبت شد.");
      load();
    } catch (err: any) { error(err?.message || "ثبت دریافت انجام نشد."); }
    finally { setBusyBackId(""); }
  };
  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="مرجوعی‌های مشتریان" description="وضعیت مرجوعی‌ها را پیگیری کن و پس از رسیدن کالا به انبار، درخواست بازپس‌گیری بده." icon={<RotateCcw />} />
      <SupplierResourceState loading={loading} error={loadError ?? ''} retry={load} />
      {!loading && !loadError && returns.length === 0 && <SupplierEmptyState icon={<RotateCcw />} title="مرجوعی‌ای برای کالاهای تو ثبت نشده است." description="وقتی مشتری کالایی را مرجوع کند، وضعیت آن را همین‌جا می‌بینی." />}
      {!loadError && returns.map((ret: any, ri: number) => (
        <section key={ret.returnId || ri} className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-sm shadow-sm sm:p-5">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="break-words text-base font-extrabold text-[#261b41]">سفارش {toPersianDigits(ret.orderNumber || ret.orderId)}</h3>
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] text-slate-600">{RETURN_STATUS_LABELS[ret.status] || ret.status}</span>
            {ret.returnCode && (<span className="rounded-full bg-violet-50 px-2 py-0.5 text-[10px] text-violet-700">کد مرجوعی: {toPersianDigits(ret.returnCode)}</span>)}
            <span className="text-[10px] text-slate-400">{formatFaDate(ret.createdAt)}</span>
            <ContextSupportButton role="supplier" kind="return" referenceId={ret.returnId} className="ms-auto inline-flex min-h-11 items-center gap-1.5 rounded-lg border border-brand-border px-2.5 text-xs font-bold text-brand-deep hover:bg-brand-soft focus-visible:ring-2 focus-visible:ring-brand" />
          </div>
          {(ret.items || []).map((item: any) => {
            const back = (item.backRequests || [])[0];
            const heldQty = (item.backRequests || []).reduce((sum: number, b: any) => (b.status === "rejected" || b.status === "cancelled" ? sum : sum + b.quantity), 0);
            const remaining = item.quantity - heldQty;
            const canRequest = !!item.receivedAt && !back;
            const maxForForm = back && back.status === "completed" ? remaining : item.quantity;
            const qc = QC_BADGES[item.qcStatus] || QC_BADGES.pending;
            const steps = back ? [
              { label: "درخواست ثبت شد", at: back.createdAt, done: true, icon: <CheckCircle2 className="w-3.5 h-3.5" strokeWidth={1.5} /> },
              { label: back.status === "rejected" ? "انبار رد کرد" : "انبار بررسی کرد", at: back.reviewedAt, done: !!back.reviewedAt, icon: back.status === "rejected" ? <XCircle className="w-3.5 h-3.5" strokeWidth={1.5} /> : <CheckCircle2 className="w-3.5 h-3.5" strokeWidth={1.5} /> },
              { label: "ارسال شد به سمت شما", at: back.shippedAt, done: !!back.shippedAt, icon: <Truck className="w-3.5 h-3.5" strokeWidth={1.5} /> },
              { label: "تحویل گرفتید", at: back.completedAt, done: !!back.completedAt, icon: <Package className="w-3.5 h-3.5" strokeWidth={1.5} /> },
            ] : [];
            return (
              <div key={item.returnItemId} className="space-y-2 rounded-2xl border border-[#e6e2f0] bg-[#f8f6fc] p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Package className="w-4 h-4 text-slate-400" strokeWidth={1.5} />
                  <span className="font-bold">{item.productTitle}</span>
                  {item.variantName && (<span className="text-slate-500">({item.variantName})</span>)}
                  <span className="text-slate-500">تعداد مرجوعی: <b>{toPersianDigits(item.quantity)}</b></span>
                  <span className={`rounded-full px-2 py-0.5 text-[10px] ${qc.cls}`}>{qc.text}</span>
                </div>
                {back && (
                  <div className="space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className={`rounded-full px-2 py-0.5 text-[10px] ${BACK_STATUS_CLASSES[back.status] || "bg-slate-100 text-slate-600"}`}>{BACK_STATUS_LABELS[back.status] || back.status}</span>
                      <span className="text-slate-500">تعداد درخواستی: {toPersianDigits(back.quantity)}</span>
                    </div>
                    {back.trackingNumber && <p className="text-sm text-slate-700">کد رهگیری مرسوله برگشتی <bdi dir="ltr" className="font-bold">{back.trackingNumber}</bdi></p>}
                    {back.reviewReason && <p className="text-sm text-slate-700">توضیح انبار {back.reviewReason}</p>}
                    <div className="flex gap-2 pt-1">
                      {back.status === "requested" && (<button type="button" disabled={!!busyBackId} onClick={() => cancelBack(back)} className="min-h-11 rounded-xl border border-slate-300 px-3 py-2 font-bold text-slate-700 focus-visible:ring-2 focus-visible:ring-brand">{busyBackId === back.id ? "در حال لغو…" : "لغو درخواست"}</button>)}
                      {back.status === "shipped_back" && (<button type="button" disabled={!!busyBackId} onClick={() => completeBack(back)} className="min-h-11 rounded-xl bg-brand-deep px-3 py-2 font-bold text-white focus-visible:ring-2 focus-visible:ring-brand">{busyBackId === back.id ? "در حال ثبت…" : "کالا را تحویل گرفتم"}</button>)}
                    </div>
                    <details>
                      <summary className={detailSummaryClass}>مراحل بازپس‌گیری</summary>
                      <ol className="space-y-1.5 border-s-2 border-slate-200 ps-3">
                        {steps.map(step => <li key={step.label} className="flex flex-wrap items-center gap-x-2 text-sm text-slate-700"><span aria-hidden="true">{step.icon}</span><span>{step.label}</span><span className="text-slate-600">{step.done ? formatFaDate(step.at) : 'در انتظار'}</span></li>)}
                      </ol>
                    </details>
                  </div>
                )}
                {!back && !item.receivedAt && (<p className="text-sm text-slate-600">پس از رسیدن کالا به انبار می‌توانی درخواست بازپس‌گیری بدهی.</p>)}
                {openFormId === item.returnItemId ? (
                  <form onSubmit={(e) => submitBack(e, item, maxForForm)} className="space-y-2">
                    <label className="block">تعداد (حداکثر {toPersianDigits(maxForForm)})<input autoFocus required disabled={submitting} inputMode="numeric" value={formQty} onChange={(e) => setFormQty(e.target.value)} className={serviceFieldClass} /></label>
                    <label className="block">دلیل بازپس‌گیری<input required disabled={submitting} value={formReason} onChange={(e) => setFormReason(e.target.value)} placeholder="مثلاً بررسی کیفیت کالا" className={serviceFieldClass} /></label>
                    <p className="text-xs leading-6 text-slate-600">انبار درخواست را بررسی می‌کند. هزینهٔ ارسال کالا به تو بر عهدهٔ خودت است و جدا از کیف پول پرداخت می‌شود.</p>
                    <div className="flex gap-2">
                      <button disabled={submitting} className="min-h-11 rounded-xl bg-brand-deep px-3 py-2 font-bold text-white focus-visible:ring-2 focus-visible:ring-brand">{submitting ? "در حال ثبت…" : "ثبت درخواست عودت"}</button>
                      <button type="button" disabled={submitting} onClick={() => { setOpenFormId(""); setFormQty(""); setFormReason(""); }} className="min-h-11 rounded-xl border border-slate-300 px-3 py-2 font-bold text-slate-700 focus-visible:ring-2 focus-visible:ring-brand">انصراف</button>
                    </div>
                  </form>
                ) : (
                  <>
                    {(canRequest || (back && item.receivedAt && (back.status === "rejected" || back.status === "cancelled" || (back.status === "completed" && remaining > 0)))) && (
                      <button type="button" onClick={() => { setOpenFormId(item.returnItemId); setFormQty(String(maxForForm)); setFormReason(""); }} className="flex min-h-11 items-center gap-1.5 rounded-xl bg-brand-deep px-3 py-2 font-bold text-white focus-visible:ring-2 focus-visible:ring-brand">
                        <RotateCcw aria-hidden="true" className="size-4" strokeWidth={1.5} />
                        {canRequest ? "درخواست بازپس‌گیری کالا" : "درخواست دوباره بازپس‌گیری"}
                      </button>
                    )}
                  </>
                )}
              </div>
            );
          })}
          <details className="border-t border-slate-100">
            <summary className={detailSummaryClass}>دلیل، مقصد و هزینهٔ مرجوعی</summary>
            <div className="space-y-2 pb-2">
              {ret.reason && (<p className="text-slate-600">دلیل مرجوعی مشتری: {ret.reason}</p>)}
              {(ret.destinationName || ret.warehouseName) && (<p className="text-slate-600">انبار مقصد: <b>{ret.destinationName || ret.warehouseName}</b>{ret.destinationType === "supplier" ? " (انبار تأمین‌کننده)" : ret.destinationType === "central" ? " (انبار مرکزی)" : ""}{ret.warehouseCity ? ` — ${ret.warehouseCity}` : ""}</p>)}
              {ret.returnShippingBearer ? (ret.returnShippingBearer === "supplier" ? (<p className="rounded-xl bg-amber-50 p-2 text-amber-800">هزینه مرجوعی به عهده شماست{ret.returnShippingAmount != null ? `: ${formatPrice(ret.returnShippingAmount)} تومان` : ""} — پس از تأیید انبار به کیف پول مشتری واریز می‌شود.</p>) : ret.returnShippingBearer === "mixed" ? (<p className="rounded-xl bg-violet-50 p-2 text-violet-800">بخشی از هزینه مرجوعی به عهده شماست و بخشی به عهده مشتری است{ret.returnShippingAmount != null ? ` (مجموع هزینه مرجوعی: ${formatPrice(ret.returnShippingAmount)} تومان)` : ""}.</p>) : (<p className="rounded-xl bg-slate-50 p-2 text-slate-600">هزینه مرجوعی به عهده مشتری است{ret.returnShippingAmount != null ? ` (${formatPrice(ret.returnShippingAmount)} تومان)` : ""}.</p>)) : (<p className="rounded-xl bg-slate-50 p-2 text-slate-600">انبار هنوز هزینهٔ مرجوعی را تعیین نکرده است.</p>)}
              {(chargesByReturn.get(ret.returnId) || []).map((ch: any) => (<p key={ch.id} className="rounded-xl border border-amber-100 bg-white p-2 text-xs leading-6 text-slate-700"><span className="font-bold">توضیح تسویه ({ch.bearer === "supplier" ? "به عهده شما" : "به عهده مشتری"}، {formatPrice(ch.amount)} تومان): </span>{ch.reason}</p>))}
            </div>
          </details>
        </section>
      ))}
    </div>
  );
};

// FIX 37: rebuilt from the live bundle SupplierDashboardView-MERGED.js (Nn component).
// Live's warehouses tab is a full CRUD tab: help text, live empty state, cards
// with فعال / غیرفعال (مدیر) badges and an admin-deactivation note, an edit
// button per card, a "ثبت انبار جدید" button and a create/edit form with
// live's exact validation messages and toasts. The previous candidate tab
// was a read-only generic list (no form, no badges, wrong empty text).
// NOTE: live's duplicate-name server message is unproven; the backend maps
// the UNIQUE(supplier_id, name) violation to a readable 400 (see
// HandleCreateSupplierWarehouse). The empty state is expected on staging:
// the test supplier has no registered warehouses (its header "انبار: تهران"
// comes from the supplier profile city, which live's help text says is the
// origin when no warehouse is registered — not a contradiction).
const WAREHOUSE_EMPTY_FORM = { name: "", provinceName: "", cityName: "", address: "", postalCode: "", phone: "" };
export type WarehouseForm = typeof WAREHOUSE_EMPTY_FORM;
// Pure helpers for the ProvinceCityPicker wiring, so the rules are
// unit-testable in the tsx --test + renderToStaticMarkup harness (no DOM):
// a province picked from the dataset list clears a stale city, a free-text
// province keeps it; edit mode copies the saved Persian name strings into
// the form and the picker resolves them by name (matchedProvince/matchedCity).
export const warehouseFormFromItem = (w: any): WarehouseForm => ({ name: w.name, provinceName: w.provinceName, cityName: w.cityName, address: w.address || "", postalCode: w.postalCode || "", phone: w.phone || "" });
export const applyWarehouseProvinceChange = (form: WarehouseForm, value: string, fromList: boolean): WarehouseForm => fromList ? { ...form, provinceName: value, cityName: "" } : { ...form, provinceName: value };
export const SupplierMyWarehousesTab: React.FC = () => {
  const { success, error } = useToast();
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<any | null>(null);
  const [form, setForm] = useState(WAREHOUSE_EMPTY_FORM);
  const [saving, setSaving] = useState(false);
  const load = async () => {
    setLoading(true);
    setLoadError("");
    try {
      setItems(await supplierApi.getMyWarehouses());
    } catch (cause: unknown) {
      setLoadError(cause instanceof Error ? cause.message : "دریافت اطلاعات انجام نشد.");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const openCreate = () => {
    setEditing(null);
    setForm(WAREHOUSE_EMPTY_FORM);
    setFormOpen(true);
  };
  const openEdit = (w: any) => {
    setEditing(w);
    setForm(warehouseFormFromItem(w));
    setFormOpen(true);
  };
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    if (!form.name.trim() || !form.provinceName.trim() || !form.cityName.trim()) {
      error("نام، استان و شهر انبار الزامی است.");
      return;
    }
    if (form.postalCode.trim() !== "" && !/^[0-9]{10}$/.test(form.postalCode.trim())) {
      error("کد پستی باید دقیقاً ۱۰ رقم باشد.");
      return;
    }
    setSaving(true);
    try {
      if (editing) {
        await supplierApi.updateMyWarehouse(editing.id, form);
        success("انبار ویرایش شد.");
      } else {
        await supplierApi.createMyWarehouse(form);
        success("انبار ثبت شد؛ از این به بعد می‌توانی آن را مبدأ کالاهای مستقیم قرار بدهی.");
      }
      setFormOpen(false);
      setEditing(null);
      setForm(WAREHOUSE_EMPTY_FORM);
      await load();
    } catch (cause: unknown) {
      error(cause instanceof Error ? cause.message : "ذخیره انجام نشد.");
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="انبارهای من" description="مبدأ ارسال مستقیم کالاهایت را ثبت یا ویرایش کن." icon={<Warehouse />}
        action={!formOpen && <button type="button" onClick={openCreate} className="inline-flex min-h-11 items-center gap-2 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2"><Plus aria-hidden="true" className="size-4" /> ثبت انبار جدید</button>} />
      <SupplierResourceState loading={loading} error={loadError} retry={load} />
      {items.length === 0 && !loading && !loadError && !formOpen && <SupplierEmptyState icon={<Warehouse />} title="هنوز انباری ثبت نکرده‌ای." description="برای تعیین مبدأ ارسال یا انتقال موجودی، از دکمهٔ «ثبت انبار جدید» استفاده کن." />}
      {formOpen && (
        <form onSubmit={submit} className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
          <h3 className="text-base font-extrabold text-[#261b41]">{editing ? "ویرایش انبار" : "ثبت انبار جدید"}</h3>
          <label className="block text-sm font-bold text-slate-700">نام انبار<input autoFocus required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="مثلاً کارخانه کرج" className={serviceFieldClass} /></label>
          <div className="grid grid-cols-2 gap-2">
            <label htmlFor="supplier-warehouse-province" className="block text-sm font-bold text-slate-700">استان</label>
            <label htmlFor="supplier-warehouse-city" className="block text-sm font-bold text-slate-700">شهر</label>
            <ProvinceCityPicker
              province={form.provinceName}
              city={form.cityName}
              onProvinceChange={(value, fromList) => setForm((f) => applyWarehouseProvinceChange(f, value, fromList))}
              onCityChange={(value) => setForm((f) => ({ ...f, cityName: value }))}
              inputClassName={serviceFieldClass}
              provinceId="supplier-warehouse-province"
              cityId="supplier-warehouse-city"
            />
          </div>
          <details key={editing?.id || 'new'} open={Boolean(editing?.address || editing?.postalCode || editing?.phone)} className="border-y border-slate-100">
            <summary className={detailSummaryClass}>نشانی و تماس (اختیاری)</summary>
            <div className="space-y-3 pb-3">
              <label className="block text-sm font-bold text-slate-700">نشانی<input value={form.address} onChange={(e) => setForm({ ...form, address: e.target.value })} className={serviceFieldClass} /></label>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                <label className="block text-sm font-bold text-slate-700">کد پستی (۱۰ رقم)<input value={form.postalCode} onChange={(e) => setForm({ ...form, postalCode: toLatinDigits(e.target.value) })} inputMode="numeric" dir="ltr" className={serviceFieldClass} /></label>
                <label className="block text-sm font-bold text-slate-700">تلفن<input value={form.phone} onChange={(e) => setForm({ ...form, phone: toLatinDigits(e.target.value) })} type="tel" dir="ltr" className={serviceFieldClass} /></label>
              </div>
            </div>
          </details>
          <div className="flex gap-2">
            <button disabled={saving} className="min-h-11 rounded-xl bg-brand-deep px-4 py-2 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">{saving ? "در حال ذخیره…" : "ذخیره انبار"}</button>
            <button type="button" disabled={saving} onClick={() => { setFormOpen(false); setEditing(null); }} className="min-h-11 rounded-xl border border-slate-300 px-4 py-2 text-sm focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-60">انصراف</button>
          </div>
        </form>
      )}
      <div className="grid items-start gap-3 xl:grid-cols-2">
        {items.map((w: any, i: number) => (
          <section key={w.id || i} className="min-w-0 space-y-2 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm">
            <div className="flex items-start justify-between gap-2">
              <h3 className="break-words text-base font-extrabold text-[#261b41]">{w.name}</h3>
              {w.isActive ? (
                <span className="rounded-full bg-emerald-50 border border-emerald-200 px-2 py-0.5 text-[10px] font-bold text-emerald-700">فعال</span>
              ) : (
                <span className="rounded-full bg-slate-100 border border-slate-200 px-2 py-0.5 text-[10px] font-bold text-slate-500">غیرفعال (مدیر)</span>
              )}
            </div>
            <p className="text-sm text-slate-700">{w.provinceName} · {w.cityName}</p>
            {w.address && <p className="break-words text-xs text-slate-600">{w.address}</p>}
            {(w.postalCode || w.phone) && <p className="break-words text-xs text-slate-600" dir="ltr">{[w.postalCode, w.phone].filter(Boolean).join(" · ")}</p>}
            {!w.isActive && <p className="rounded-lg bg-amber-50 px-2 py-1 text-xs leading-6 text-amber-900">کالاهایی که از این انبار ارسال می‌شدند، فعلاً از شهر ثبت‌شده در پروفایلت ارسال می‌شوند.</p>}
            <div className="flex gap-2 pt-1">
              <button type="button" onClick={() => openEdit(w)} className="inline-flex min-h-11 items-center gap-1 rounded-xl bg-brand-soft px-3 text-xs font-bold text-brand-deep hover:bg-brand-border focus-visible:ring-2 focus-visible:ring-brand"><Pencil className="w-3.5 h-3.5"  strokeWidth={1.5}/> ویرایش</button>
            </div>
          </section>
        ))}
      </div>
    </div>
  );
};

// FIX 26: rebuilt from the live bundle SupplierDashboardView-MERGED.js (kn component).
// Live renders one row per known method (direct, warehouse), matched by method key,
// with live titles, descriptions and status labels. The previous generic list rendered
// pick(name/title/carrier/code), which the backend never sends, so every row was "—".
// NOTE: live also renders a Tapin settings block (wn) at the bottom of this tab;
// that block is SupplierTapinCard below (gaps 1-3).
const SHIPPING_METHOD_META: Record<string, { title: string; description: string; icon: React.ElementType }> = {
  direct: { title: "ارسال مستقیم", description: "سفارش را خودت از انبارت بسته‌بندی می‌کنی و مستقیم به دست مشتری می‌رسانی.", icon: Truck },
  warehouse: { title: "ارسال به انبار مرکزی", description: "کالا را اول به انبار مرکزی می‌فرستی؛ کنترل کیفیت، بسته‌بندی و فرستادن به مشتری با پلتفرم است.", icon: Warehouse },
};
const SHIPPING_METHOD_ORDER = ["direct", "warehouse"];
const SHIPPING_STATUS_META: Record<string, { label: string; badgeClass: string; icon: React.ElementType }> = {
  enabled: { label: "فعال", badgeClass: "bg-emerald-50 text-emerald-700 border-emerald-200", icon: CheckCircle2 },
  requested: { label: "در انتظار تأیید", badgeClass: "bg-amber-50 text-amber-700 border-amber-200", icon: Clock },
  disabled: { label: "غیرفعال", badgeClass: "bg-slate-100 text-slate-500 border-slate-200", icon: XCircle },
};


// Tapin connection card (gaps 1-3) — rendered at the bottom of the
// shipping-methods tab, where live renders its Tapin block (wn).
// The supplier creates their own Tapin account, then pastes the shop
// id + API token from the Tapin panel (یکپارچه‌سازی ← مدیریت توکن).
// The token is write-only: it is never displayed, and the backend
// never returns it.
const TAPIN_SIGNUP_URL = "https://my.tapin.ir/authentication/login?utm_source=selersho&utm_medium=supplier_panel&utm_campaign=tapin_signup";

type TapinTestResult = { kind: "active" | "inactive" | "invalid" | "error"; message: string };

export const SupplierTapinCard: React.FC = () => {
  const { success, error } = useToast();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [connected, setConnected] = useState(false);
  const [shopId, setShopId] = useState("");
  const [inputShopId, setInputShopId] = useState("");
  const [inputToken, setInputToken] = useState("");
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [disconnecting, setDisconnecting] = useState(false);
  const [testResult, setTestResult] = useState<TapinTestResult | null>(null);
  // E6: passive freshness signal — the last manual test, not an auto-test.
  const [lastTestedAt, setLastTestedAt] = useState<Date | null>(null);
  const testAbortRef = useRef<AbortController | null>(null);
  const testTimedOutRef = useRef(false);

  const load = () => {
    setLoading(true);
    setLoadError('');
    supplierApi.getTapinSettings()
      .then((res) => {
        setConnected(!!res.tapinConnected);
        setShopId(res.tapinShopId || "");
        setInputShopId(res.tapinShopId || "");
      })
      .catch((cause: unknown) => { setLoadError(cause instanceof Error ? cause.message : 'تنظیمات تاپین دریافت نشد.'); })
      .finally(() => { setLoading(false); });
  };
  useEffect(() => { load(); }, []);

  const save = async () => {
    if (!inputShopId.trim()) { error("شناسه فروشگاه تاپین را وارد کن."); return; }
    if (!connected && !inputToken.trim()) { error("توکن تاپین را وارد کن."); return; }
    setSaving(true);
    try {
      const res = await supplierApi.updateTapinSettings({
        shopId: inputShopId.trim(),
        token: inputToken.trim() ? inputToken.trim() : undefined,
      });
      setConnected(!!res.tapinConnected);
      setShopId(res.tapinShopId || "");
      setInputToken("");
      setTestResult(null);
      setLastTestedAt(null);
      success("تنظیمات ذخیره شد. برای بررسی دسترسی تیپاکس، تست اتصال را بزن.");
    } catch (err: any) {
      error(err?.message || "ذخیره اتصال تاپین انجام نشد.");
    } finally {
      setSaving(false);
    }
  };

  // E6: testing is an explicit user action, bounded and cancellable.
  // The button stays enabled while testing — clicking it again aborts
  // the in-flight call. The 15 s client timer aborts the request itself
  // (not just the spinner); the server-side Tapin client is already
  // bounded (dial 1 s, header 3 s, total 6 s — backend/internal/tapin/
  // client.go), so no backend change was needed.
  const test = async () => {
    if (testing) { testAbortRef.current?.abort(); return; }
    const controller = new AbortController();
    testAbortRef.current = controller;
    testTimedOutRef.current = false;
    setTesting(true);
    setTestResult(null);
    const timer = setTimeout(() => { testTimedOutRef.current = true; controller.abort(); }, 15000);
    try {
      const res = await supplierApi.testTapinConnection(controller.signal);
      if (res.ok && res.serviceActive) {
        setTestResult({ kind: "active", message: res.message });
      } else if (res.ok) {
        setTestResult({ kind: "inactive", message: res.message });
      } else {
        setTestResult({ kind: "invalid", message: res.message || res.error || "اتصال برقرار نیست." });
      }
    } catch (err: any) {
      if (testTimedOutRef.current) {
        setTestResult({ kind: "error", message: "تست طول کشید، دوباره تلاش کن." });
      } else if (controller.signal.aborted) {
        setTestResult({ kind: "error", message: "تست لغو شد." });
      } else {
        setTestResult({ kind: "error", message: err?.message || "برقراری ارتباط با تاپین ممکن نشد." });
      }
    } finally {
      clearTimeout(timer);
      testAbortRef.current = null;
      setTesting(false);
      setLastTestedAt(new Date());
    }
  };

  // E6: no auto-test on mount — the connection status comes from the
  // stored tapinConnected flag; testing is an explicit user action.

  const disconnect = async () => {
    setDisconnecting(true);
    try {
      await supplierApi.deleteTapinSettings();
      setConnected(false);
      setShopId("");
      setInputShopId("");
      setInputToken("");
      setTestResult(null);
      setLastTestedAt(null);
      success("اتصال تاپین قطع شد.");
    } catch (err: any) {
      error(err?.message || "قطع اتصال تاپین انجام نشد.");
    } finally {
      setDisconnecting(false);
    }
  };

  return (
    <section className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <span aria-hidden="true" className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand-soft text-brand-deep">
            <Truck className="h-5 w-5" />
          </span>
          <div>
            <h3 className="text-base font-extrabold text-slate-800">اتصال تاپین</h3>
            <p className="mt-0.5 text-sm leading-6 text-slate-600">
              با تاپین، هزینهٔ ارسال مستقیم محاسبه می‌شود و می‌توانی مرسوله ثبت کنی و کد رهگیری بگیری.
            </p>
          </div>
        </div>
        {connected && (
          testResult?.kind === "active" ? (
            <span className="flex shrink-0 items-center gap-1 rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-[10px] font-bold text-emerald-700">
              <CheckCircle2 className="h-3 w-3" />
              تیپاکس فعال
            </span>
          ) : testResult?.kind === "inactive" ? (
            <span className="flex shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 text-[10px] font-bold bg-amber-50 text-amber-700 border-amber-200">
              <Info className="h-3 w-3" />
              سرویس تیپاکس غیرفعال
            </span>
          ) : testResult?.kind === "invalid" ? (
            <span className="flex shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 text-[10px] font-bold bg-rose-50 text-rose-600 border-rose-200">
              <Info className="h-3 w-3" />
              اتصال نامعتبر
            </span>
          ) : (
            <span className="flex shrink-0 items-center gap-1 rounded-full border border-slate-200 bg-slate-50 px-2.5 py-1 text-[10px] font-bold text-slate-700">
              <Info className="h-3 w-3" />
              {testing ? "در حال بررسی اتصال" : "تنظیم‌شده، نیاز به تست"}
            </span>
          )
        )}
      </div>

      {loading && <p role="status" className="text-sm text-slate-600">در حال بارگذاری…</p>}
      {!loading && loadError && <SupplierResourceState loading={false} error={loadError} retry={load} />}

      {!loading && !loadError && (
        <form onSubmit={event => { event.preventDefault(); if (!saving) void save(); }} className="space-y-3">
          {!connected && (
            <details className="rounded-xl bg-slate-50 px-3">
              <summary className={detailSummaryClass}>شناسه و توکن تاپین را از کجا بگیرم؟</summary>
              <p className="pb-2 text-sm leading-6 text-slate-700">در پنل تاپین به «یکپارچه‌سازی» و سپس «مدیریت توکن» برو. برای دریافت توکن به آی‌پی سرور و رمز پنل نیاز داری.</p>
              <a href="https://docs.tapin.ir/GetToken/" target="_blank" rel="noopener noreferrer" className="mb-2 inline-flex min-h-11 items-center rounded-lg px-2 text-sm font-bold text-brand-deep underline focus-visible:ring-2 focus-visible:ring-brand">راهنمای دریافت توکن</a>
              <a href={TAPIN_SIGNUP_URL} target="_blank" rel="noopener noreferrer" className="mb-2 inline-flex min-h-11 items-center rounded-lg px-2 text-sm font-bold text-brand-deep underline focus-visible:ring-2 focus-visible:ring-brand">ثبت‌نام در تاپین (زبانهٔ جدید)</a>
            </details>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label htmlFor="supplier-tapin-shop" className="block text-sm font-bold text-slate-700">شناسهٔ فروشگاه تاپین</label>
              <input
                id="supplier-tapin-shop"
                type="text"
                required
                value={inputShopId}
                onChange={(e) => setInputShopId(e.target.value)}
                className={serviceFieldClass}
                dir="ltr"
              />
            </div>
            <div>
              <label htmlFor="supplier-tapin-token" className="block text-sm font-bold text-slate-700">توکن تاپین</label>
              <input
                id="supplier-tapin-token"
                type="password"
                required={!connected}
                aria-describedby={connected ? 'supplier-tapin-token-help' : undefined}
                value={inputToken}
                onChange={(e) => setInputToken(e.target.value)}
                className={serviceFieldClass}
                dir="ltr"
                autoComplete="off"
              />
              {connected && <p id="supplier-tapin-token-help" className="mt-1 text-xs leading-6 text-slate-600">برای نگه‌داشتن توکن فعلی، این بخش را خالی بگذار.</p>}
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <button type="submit" disabled={saving || disconnecting || testing} className="min-h-11 rounded-xl bg-brand-deep px-4 py-2.5 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
              {saving ? "در حال ذخیره…" : "ذخیره اتصال"}
            </button>
            {connected && (
              <>
                <button type="button" disabled={saving || disconnecting} onClick={() => void test()} className="min-h-11 rounded-xl border border-slate-300 px-3 py-2.5 text-sm font-bold text-slate-700 focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
                  {testing ? (
                    <span className="inline-flex items-center justify-center gap-1.5">
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      لغو تست اتصال
                    </span>
                  ) : "تست اتصال"}
                </button>
                <button type="button" disabled={disconnecting || saving || testing} onClick={() => void disconnect()} className="min-h-11 rounded-xl px-3 py-2.5 text-sm font-bold text-rose-700 focus-visible:ring-2 focus-visible:ring-rose-700 disabled:opacity-50">
                  {disconnecting ? "در حال قطع اتصال…" : "قطع اتصال"}
                </button>
              </>
            )}
          </div>
          {connected && lastTestedAt && (
            <p className="text-xs text-slate-600">آخرین تست {lastTestedAt.toLocaleString('fa-IR')}</p>
          )}
          {connected && !testResult && (
            <p className="text-xs leading-6 text-slate-600">
              برای ثبت مرسوله، تیپاکس باید برای فروشگاهت در تاپین فعال باشد.
            </p>
          )}
          {testResult && (
            <p role="status" className={`flex items-start gap-1.5 rounded-xl px-3 py-2 text-sm leading-6 font-bold ${
              testResult.kind === "active" ? "bg-emerald-50 text-emerald-700" :
              testResult.kind === "inactive" ? "bg-amber-50 text-amber-700" :
              "bg-rose-50 text-rose-600"
            }`}>
              {testResult.kind === "active" ? <CheckCircle2 className="h-3.5 w-3.5 shrink-0 mt-0.5" strokeWidth={1.5} /> : <Info className="h-3.5 w-3.5 shrink-0 mt-0.5" strokeWidth={1.5} />}
              {testResult.kind === "inactive"
                ? "سرویس تیپاکس برای فروشگاهت هنوز فعال نیست؛ فعال‌سازی از طریق پشتیبانی تاپین انجام می‌شود. تا آن زمان هزینه ارسال سفارش‌هایت با تعرفه داخلی محاسبه می‌شود."
                : testResult.message}
            </p>
          )}
        </form>
      )}
    </section>
  );
};

export const SupplierShippingMethodsTab: React.FC = () => {
  const { supplierSession } = useStore();
  const { success, error } = useToast();
  const [methods, setMethods] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pendingMethod, setPendingMethod] = useState<string | null>(null);
  const load = () => {
    setLoading(true);
    setLoadError(null);
    supplierApi.getShippingMethods()
      .then((list) => { setMethods(list); })
      .catch((err: any) => { setLoadError(err?.message || "خطا در دریافت داده"); })
      .finally(() => { setLoading(false); });
  };
  useEffect(() => { load(); }, []);
  const request = async (method: string) => {
    if (pendingMethod) return;
    setPendingMethod(method);
    try {
      await supplierApi.requestShippingMethod(method);
      load();
      const title = SHIPPING_METHOD_META[method]?.title || method;
      success(`درخواست فعال‌کردن «${title}» ثبت شد؛ مدیر باید تأیید کند.`);
    } catch (err: any) {
      error(err?.message || "ثبت درخواست انجام نشد. دوباره تلاش کن.");
    } finally {
      setPendingMethod(null);
    }
  };
  return (
    <div className="space-y-3 pb-6">
      <SupplierSectionHeading title="روش‌های ارسال" description="درخواست فعال‌سازی یک یا هر دو روش را برای بررسی مدیر ثبت کن." icon={<Truck />} />
      <SupplierResourceState loading={loading} error={loadError ?? ''} retry={load} />
      {!loading && !loadError && SHIPPING_METHOD_ORDER.map((method) => {
        const row = methods.find((m: any) => m.method === method);
        if (!row) return null;
        const meta = SHIPPING_METHOD_META[method];
        const status = SHIPPING_STATUS_META[row.status] || SHIPPING_STATUS_META.disabled;
        const MethodIcon = meta.icon;
        const StatusIcon = status.icon;
        const pending = pendingMethod === method;
        return (
          <section key={method} className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="flex items-center gap-2.5">
                <span aria-hidden="true" className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand-soft text-brand-deep">
                  <MethodIcon className="h-5 w-5" />
                </span>
                <div>
                  <h3 className="text-base font-extrabold text-slate-800">{meta.title}</h3>
                  <p className="mt-0.5 text-sm leading-6 text-slate-600">{meta.description}</p>
                </div>
              </div>
              <span className={`flex shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 text-xs font-bold ${status.badgeClass}`}>
                <StatusIcon aria-hidden="true" className="size-3" />
                {status.label}
              </span>
            </div>
            {row.status === "disabled" && (
              <button type="button" disabled={!!pendingMethod} aria-label={pending ? "در حال ثبت درخواست…" : `درخواست فعال‌سازی ${meta.title}`} onClick={() => void request(method)} className="inline-flex min-h-11 items-center justify-center gap-1.5 rounded-xl bg-brand-deep px-3 py-2.5 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50">
                <Send aria-hidden="true" className="size-4" strokeWidth={1.5} />
                {pending ? "در حال ثبت درخواست…" : "درخواست فعال‌سازی"}
              </button>
            )}
          </section>
        );
      })}
      <SupplierSLACard />
      {supplierSession?.permissions?.includes('supplier.manage') && <SupplierTapinCard />}
    </div>
  );
};

// The demand-insight tab lives in its own file; re-exported here so
// SupplierDashboardView keeps importing it from this module unchanged.
export { SupplierDemandInsightTab } from './SupplierDemandInsightTab';

export const SupplierContractTab: React.FC = () => {
  const { toPersianDigits, supplierSession, platformSettings } = useStore();
  const text = platformSettings.contracts?.supplierContractText?.trim() || '';
  const version = platformSettings.contracts?.supplierContractVersion || 0;
  const signed = supplierSession?.signedContractVersion;
  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="قرارداد من" description="نسخهٔ فعلی قرارداد تأمین و توزیع و وضعیت امضای آن." icon={<FileText />} />
      <section aria-labelledby="supplier-contract-text" className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 id="supplier-contract-text" className="text-base font-extrabold text-[#261b41]">متن قرارداد</h3>
          {version > 0 && (
            <span className="rounded-lg bg-brand-soft px-2.5 py-1 text-xs font-bold text-brand-deep">نسخهٔ {toPersianDigits(version)}</span>
          )}
        </div>
        {typeof signed === 'number' && signed > 0 && (
          <p className={`rounded-xl p-3 text-sm leading-6 ${signed >= version ? 'bg-emerald-50 text-emerald-800' : 'bg-amber-50 text-amber-900'}`}>
            {signed >= version
              ? <>قرارداد به‌روز است. نسخهٔ {toPersianDigits(signed)} را امضا کرده‌ای.</>
              : <>نسخهٔ {toPersianDigits(signed)} را امضا کرده‌ای. برای ادامهٔ کار باید نسخهٔ جدید را تأیید کنی.</>}
          </p>
        )}
        <div className="whitespace-pre-line break-words rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm leading-8 text-slate-800">
          {text || 'متن قرارداد دریافت نشد. صفحه را دوباره بارگذاری کن.'}
        </div>
      </section>
    </div>
  );
};
