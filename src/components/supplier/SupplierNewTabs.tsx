import { SupplierSectionHeading } from './SupplierSectionHeading';
import { SupplierResourceState } from './SupplierResourceState';
import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../../context/StoreContext';
import { supplierApi } from '../../services/supplierApi';
import { ProvinceCityPicker } from '../ProvinceCityPicker';
import { SupplierSLACard } from './SupplierSLACard';

import { useToast } from "../../context/ToastContext";import { authApi } from '../../services/authApi';
import { BarChart3, Banknote, Package, ShoppingBag, TrendingUp, RotateCcw, Warehouse, Truck, User, Info, Plus, Pencil, CheckCircle2, Clock, XCircle, Send, FileText, Loader2 } from 'lucide-react';

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
      <SupplierSectionHeading title="تحلیل فروش و مرجوعی کالاهای تو" description="روند فروش، کالاهای پرفروش و نتیجهٔ مرجوعی‌ها را بررسی کن." icon={<BarChart3 />} />
      <div className="bg-white rounded-2xl border border-slate-100 p-4 space-y-2">
        <p className="text-[11px] leading-6 text-slate-600">اینجا می‌بینی از کالاهای خودت چقدر فروش رفته، چقدرش برگشته و موجودی هر کالا با همین سرعت فروش چند روز دیگر تمام می‌شود. فقط کالاهای خودت را می‌بینی.</p>
        <p className="text-[11px] font-bold text-slate-700">مبلغ پایه یعنی چه؟</p>
        <p className="text-[11px] leading-6 text-slate-500">مبلغ پایه همان قیمت عمده توست که مبنای تسویه‌حساب تو با پلتفرم است؛ قیمت فروش به مشتری در این عددها نیست.</p>
      </div>
      <SupplierResourceState loading={loading} error={loadError ?? ''} retry={load} />
      {!loading && !loadError && data && !hasSales && (
        <div className="bg-white rounded-2xl border border-slate-100 p-10 text-center">
          <div className="w-14 h-14 rounded-2xl bg-brand-soft border border-brand-border flex items-center justify-center mx-auto mb-4"><BarChart3 className="w-7 h-7 text-brand-deep" strokeWidth={1.5} /></div>
          <p className="text-sm font-extrabold text-slate-800 mb-1.5">هنوز فروشی ثبت نشده</p>
          <p className="text-[11px] leading-6 text-slate-400 max-w-[260px] mx-auto">به محض ثبت اولین سفارش پرداخت‌شده برای کالاهایت، آمار فروش، نمودار ۳۰ روزه و وضعیت مرجوعی همین‌جا نمایش داده می‌شود.</p>
        </div>
      )}
      {!loading && !loadError && data && hasSales && (<>
        <div className="grid grid-cols-2 gap-2.5">
          {cards.map((u) => (
            <div key={u.label} className="bg-white rounded-2xl border border-slate-100 p-3.5">
              <div className="flex items-center gap-1.5 mb-2">
                <span className="w-7 h-7 rounded-xl bg-brand-soft border border-brand-border flex items-center justify-center shrink-0"><u.icon className="w-3.5 h-3.5 text-brand-deep" strokeWidth={1.5} /></span>
                <span className="text-[10px] font-bold text-slate-500">{u.label}</span>
              </div>
              <div className="text-base font-extrabold text-slate-900">{u.value}{u.unit && (<span className="text-[10px] font-normal text-slate-400 mr-1">{u.unit}</span>)}</div>
            </div>
          ))}
        </div>
        <section className="bg-white rounded-2xl border border-slate-100 p-4">
          <div className="flex items-center justify-between mb-3">
            <h3 className="text-xs font-extrabold text-slate-800 flex items-center gap-1.5"><TrendingUp className="w-3.5 h-3.5 text-slate-400" strokeWidth={1.5} />روند مبلغ پایه — ۳۰ روز گذشته</h3>
            <span className="text-[10px] text-slate-400">به تومان</span>
          </div>
          <AnalyticsChart daily={data.daily} formatPrice={formatPrice} toPersianDigits={toPersianDigits} />
          <p className="text-[10px] text-slate-400 mt-2">هر ستون، مبلغ پایه فروش یک روز است؛ روی هر ستون نگه داری، تعداد و سفارش‌های همان روز را می‌بینی.</p>
        </section>
        <section className="bg-white rounded-2xl border border-slate-100 p-4 space-y-3">
          <h3 className="text-xs font-extrabold text-slate-800 flex items-center gap-1.5"><RotateCcw className="w-3.5 h-3.5 text-slate-400" strokeWidth={1.5} />مرجوعی و نتیجه بررسی کیفیت</h3>
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
            <h3 className="text-xs font-extrabold text-slate-800 mb-3">کالاهای تو، یکی‌یکی</h3>
            <div className="overflow-x-auto">
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
            <p className="text-[10px] text-slate-400 mt-2">«روزهای باقی‌مانده» یعنی موجودی فعلی تقسیم بر میانگین فروش روزانه ۳۰ روز اخیر؛ کالای بدون فروش در ۳۰ روز اخیر با «—» مشخص است.</p>
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
// Live's In() idempotency helper, secure-context-safe (same shape as
// Fix 43's newUuid): crypto.randomUUID only exists on HTTPS/localhost.
const newBackIdempotencyKey = () => {
  const c = (globalThis as any).crypto;
  if (c && c.randomUUID) return c.randomUUID();
  return `rb-${Date.now()}-${Math.random().toString(36).slice(2)}`;
};
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
    const qty = Number(formQty);
    if (!Number.isSafeInteger(qty) || qty <= 0 || qty > maxQty || !formReason.trim() || submitting) {
      error(`تعداد را درست وارد کن (حداکثر ${toPersianDigits(maxQty)} عدد) و دلیل درخواست را بنویس.`);
      return;
    }
    setSubmitting(true);
    try {
      await supplierApi.requestCustomerReturnBack(item.returnItemId, qty, formReason.trim(), newBackIdempotencyKey());
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
      <SupplierSectionHeading title="مرجوعی‌های مشتریان" description="وضعیت مرجوعی‌ها را ببین و کالایی را که انبار تحویل گرفته برای بازگشت درخواست کن." icon={<RotateCcw />} />
      <p className="rounded-2xl border border-[#e6e2f0] bg-white p-3 text-sm leading-6 text-slate-700">بعد از تحویل کالا به انبار، درخواست بازگشت را ثبت کن. انبار درخواست را بررسی می‌کند و هزینهٔ ارسال به عهدهٔ توست. این هزینه از کیف پول کسر نمی‌شود.</p>
      <SupplierResourceState loading={loading} error={loadError ?? ''} retry={load} />
      {!loading && !loadError && returns.length === 0 && (<div className="rounded-2xl border bg-white p-6 text-center space-y-1.5"><p className="text-xs font-bold text-emerald-700">مرجوعی‌ای برای کالاهای تو ثبت نشده است.</p><p className="text-[11px] leading-5 text-slate-400">وقتی مشتری‌ای کالای تو را مرجوع کند، اینجا نمایش داده می‌شود.</p></div>)}
      {!loadError && returns.map((ret: any, ri: number) => (
        <section key={ret.returnId || ri} className="space-y-3 rounded-[22px] border border-[#e6e2f0] bg-white p-4 text-sm shadow-sm sm:p-5">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="break-words text-base font-extrabold text-[#261b41]">سفارش {toPersianDigits(ret.orderNumber || ret.orderId)}</h3>
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] text-slate-600">{RETURN_STATUS_LABELS[ret.status] || ret.status}</span>
            {ret.returnCode && (<span className="rounded-full bg-violet-50 px-2 py-0.5 text-[10px] text-violet-700">کد مرجوعی: {toPersianDigits(ret.returnCode)}</span>)}
            <span className="text-[10px] text-slate-400">{formatFaDate(ret.createdAt)}</span>
          </div>
          {ret.reason && (<p className="text-slate-600">دلیل مرجوعی مشتری: {ret.reason}</p>)}
          {(ret.destinationName || ret.warehouseName) && (<p className="text-slate-600">انبار مقصد: <b>{ret.destinationName || ret.warehouseName}</b>{ret.destinationType === "supplier" ? " (انبار تأمین‌کننده)" : ret.destinationType === "central" ? " (انبار مرکزی)" : ""}{ret.warehouseCity ? ` — ${ret.warehouseCity}` : ""}</p>)}
          {ret.returnShippingBearer ? (ret.returnShippingBearer === "supplier" ? (<p className="rounded-xl bg-amber-50 p-2 text-amber-800">هزینه مرجوعی به عهده شماست{ret.returnShippingAmount != null ? `: ${formatPrice(ret.returnShippingAmount)} تومان` : ""} — پس از تأیید انبار به کیف پول مشتری واریز می‌شود.</p>) : ret.returnShippingBearer === "mixed" ? (<p className="rounded-xl bg-violet-50 p-2 text-violet-800">بخشی از هزینه مرجوعی به عهده شماست و بخشی به عهده مشتری است{ret.returnShippingAmount != null ? ` (مجموع هزینه مرجوعی: ${formatPrice(ret.returnShippingAmount)} تومان)` : ""}.</p>) : (<p className="rounded-xl bg-slate-50 p-2 text-slate-600">هزینه مرجوعی به عهده مشتری است{ret.returnShippingAmount != null ? ` (${formatPrice(ret.returnShippingAmount)} تومان)` : ""}.</p>)) : (<p className="rounded-xl bg-slate-50 p-2 text-slate-500">هزینه مرجوعی هنوز توسط انبار تعیین نشده است.</p>)}
          {(chargesByReturn.get(ret.returnId) || []).map((ch: any) => (<p key={ch.id} className="rounded-xl border border-amber-100 bg-white p-2 text-[11px] leading-5 text-slate-600"><span className="font-bold text-slate-700">توضیح تسویه ({ch.bearer === "supplier" ? "به عهده شما" : "به عهده مشتری"}، {formatPrice(ch.amount)} تومان): </span>{ch.reason}</p>))}
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
                    <ol className="mt-2 space-y-1.5 border-r-2 border-slate-100 pr-3">
                      {steps.map((s: any, si: number) => (<li key={si} className={`flex items-center gap-2 text-[11px] ${s.done ? "text-slate-700" : "text-slate-400"}`}>{s.icon}<span>{s.label}</span>{s.at && (<span className="text-slate-400">· {formatFaDate(s.at)}</span>)}</li>))}
                      {back.trackingNumber && (<li className="text-[11px] text-slate-500">کد رهگیری مرسوله برگشتی: <b className="text-slate-700">{back.trackingNumber}</b></li>)}
                      {back.reviewReason && (<li className="text-[11px] text-slate-500">توضیح انبار: {back.reviewReason}</li>)}
                    </ol>
                    <div className="flex gap-2 pt-1">
                      {back.status === "requested" && (<button type="button" disabled={busyBackId === back.id} onClick={() => cancelBack(back)} className="rounded-xl border px-3 py-2 min-h-[40px] font-bold text-slate-600">{busyBackId === back.id ? "…" : "لغو درخواست"}</button>)}
                      {back.status === "shipped_back" && (<button type="button" disabled={busyBackId === back.id} onClick={() => completeBack(back)} className="rounded-xl bg-brand-deep px-3 py-2 min-h-[40px] text-white font-bold">{busyBackId === back.id ? "…" : "کالا را تحویل گرفتم"}</button>)}
                    </div>
                  </div>
                )}
                {!back && !item.receivedAt && (<p className="text-[11px] text-slate-400">این کالا هنوز به انبار نرسیده؛ پس از تحویل در انبار می‌توانید درخواست عودت دهید.</p>)}
                {openFormId === item.returnItemId ? (
                  <form onSubmit={(e) => submitBack(e, item, maxForForm)} className="space-y-2">
                    <label className="block">تعداد برای عودت (حداکثر {toPersianDigits(maxForForm)})<input required disabled={submitting} inputMode="numeric" value={formQty} onChange={(e) => setFormQty(e.target.value)} className="mt-1 w-full rounded-xl border p-2 min-h-[40px]" /></label>
                    <label className="block">دلیل درخواست عودت<input required disabled={submitting} value={formReason} onChange={(e) => setFormReason(e.target.value)} placeholder="مثلاً بررسی و تعیین تکلیف کالای مرجوعی" className="mt-1 w-full rounded-xl border p-2 min-h-[40px]" /></label>
                    <div className="flex gap-2">
                      <button disabled={submitting} className="rounded-xl bg-brand-deep px-3 py-2 min-h-[44px] text-white font-bold">{submitting ? "ثبت…" : "ثبت درخواست عودت"}</button>
                      <button type="button" disabled={submitting} onClick={() => { setOpenFormId(""); setFormQty(""); setFormReason(""); }} className="rounded-xl border px-3 py-2 min-h-[40px] font-bold text-slate-600">انصراف</button>
                    </div>
                  </form>
                ) : (
                  <>
                    {canRequest && (<button type="button" onClick={() => { setOpenFormId(item.returnItemId); setFormQty(String(item.quantity)); setFormReason(""); }} className="rounded-xl bg-brand-soft px-3 py-2 min-h-[40px] font-bold text-brand-deep flex items-center gap-1.5"><RotateCcw className="w-4 h-4" strokeWidth={1.5} />درخواست عودت کالاهای مرجوعی</button>)}
                    {!canRequest && back && item.receivedAt && (back.status === "rejected" || back.status === "cancelled" || (back.status === "completed" && remaining > 0)) && (<button type="button" onClick={() => { setOpenFormId(item.returnItemId); setFormQty(String(maxForForm)); setFormReason(""); }} className="rounded-xl bg-brand-soft px-3 py-2 min-h-[40px] font-bold text-brand-deep flex items-center gap-1.5"><RotateCcw className="w-4 h-4" strokeWidth={1.5} />درخواست دوباره عودت کالا</button>)}
                  </>
                )}
              </div>
            );
          })}
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
      <SupplierSectionHeading title="انبارهای من" description="انبار یا کارخانه‌ای را که مبدأ ارسال مستقیم کالاهایت است اینجا ثبت کن." icon={<Warehouse />}
        action={!formOpen && <button type="button" onClick={openCreate} className="inline-flex min-h-11 items-center gap-2 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white hover:bg-[#382660] focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2"><Plus aria-hidden="true" className="size-4" /> ثبت انبار جدید</button>} />
      <SupplierResourceState loading={loading} error={loadError} retry={load} />
      {items.length === 0 && !loading && !loadError && (
        <div className="rounded-2xl border bg-white p-8 text-center space-y-2">
          <Warehouse className="w-8 h-8 mx-auto text-slate-300"  strokeWidth={1.5}/>
          <p className="text-xs font-bold text-slate-600">هنوز انباری ثبت نکرده‌ای</p>
          <p className="text-[10px] text-slate-400 leading-relaxed">انبار لازم نیست؛ اگر انبار ثبت نکنی، کالاهای مستقیم از شهر انبار پروفایلت ارسال می‌شوند. ولی اگر کارخانه یا انبار جداگانه داری، اینجا ثبتش کن تا مبدأ هر کالا دقیق باشد.</p>
        </div>
      )}
      {formOpen && (
        <form onSubmit={submit} className="space-y-4 rounded-[22px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
          <h3 className="text-base font-extrabold text-[#261b41]">{editing ? "ویرایش انبار" : "ثبت انبار جدید"}</h3>
          <label className="block text-[11px] font-bold text-slate-600">نام انبار *<input autoFocus value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="مثلاً کارخانه کرج یا انبار شماره ۲" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand" /></label>
          <div className="grid grid-cols-2 gap-2">
            <div aria-hidden="true" className="block text-xs font-bold text-slate-700">استان *</div>
            <div aria-hidden="true" className="block text-xs font-bold text-slate-700">شهر *</div>
            <ProvinceCityPicker
              province={form.provinceName}
              city={form.cityName}
              onProvinceChange={(value, fromList) => setForm((f) => applyWarehouseProvinceChange(f, value, fromList))}
              onCityChange={(value) => setForm((f) => ({ ...f, cityName: value }))}
              inputClassName="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand"
              provinceId="supplier-warehouse-province"
              cityId="supplier-warehouse-city"
            />
          </div>
          <label className="block text-[11px] font-bold text-slate-600">نشانی (اختیاری)<input value={form.address} onChange={(e) => setForm({ ...form, address: e.target.value })} className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand" /></label>
          <div className="grid grid-cols-2 gap-2">
            <label className="block text-[11px] font-bold text-slate-600">کد پستی (اختیاری — ۱۰ رقم)<input value={form.postalCode} onChange={(e) => setForm({ ...form, postalCode: e.target.value })} inputMode="numeric" dir="ltr" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand" /></label>
            <label className="block text-[11px] font-bold text-slate-600">تلفن (اختیاری)<input value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} inputMode="tel" dir="ltr" className="mt-1 min-h-11 w-full rounded-xl border border-[#e6e2f0] bg-[#f8f6fc] px-3 text-sm font-normal focus-visible:ring-2 focus-visible:ring-brand" /></label>
          </div>
          <div className="flex gap-2">
            <button disabled={saving} className="flex-1 rounded-lg bg-brand-deep px-3 py-2 min-h-[44px] text-xs font-bold text-white">{saving ? "ذخیره…" : "ذخیره انبار"}</button>
            <button type="button" disabled={saving} onClick={() => { setFormOpen(false); setEditing(null); }} className="flex-1 rounded-lg border px-3 py-2 min-h-[44px] text-xs disabled:opacity-60">انصراف</button>
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
            {!w.isActive && <p className="text-[10px] text-amber-700 bg-amber-50 border-amber-100 rounded-lg px-2 py-1">این انبار را مدیر غیرفعال کرده است؛ کالاهایی که مبدأشان این انبار بود، فعلاً از شهر انبار پروفایلت ارسال می‌شوند.</p>}
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
    supplierApi.getTapinSettings()
      .then((res) => {
        setConnected(!!res.tapinConnected);
        setShopId(res.tapinShopId || "");
        setInputShopId(res.tapinShopId || "");
      })
      .catch(() => { /* the card degrades to the not-connected form */ })
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
      success("اتصال تاپین ذخیره شد.");
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
      success("اتصال تاپین قطع شد.");
    } catch (err: any) {
      error(err?.message || "قطع اتصال تاپین انجام نشد.");
    } finally {
      setDisconnecting(false);
    }
  };

  return (
    <section className="rounded-2xl border bg-white p-4 space-y-3">
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <span className="flex h-10 w-10 items-center justify-center rounded-2xl bg-brand-soft text-brand-deep">
            <Truck className="h-5 w-5" />
          </span>
          <div>
            <h3 className="text-xs font-extrabold text-slate-800">اتصال تاپین</h3>
            <p className="mt-0.5 text-[11px] leading-5 text-slate-500">
              با اتصال تاپین، هزینه ارسال سفارش‌های مستقیم تو از خود تاپین خوانده می‌شود و ثبت مرسوله و کد رهگیری خودکار انجام می‌شود.
            </p>
          </div>
        </div>
        {connected && (
          testResult?.kind === "inactive" ? (
            <span className="flex shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 text-[10px] font-bold bg-amber-50 text-amber-700 border-amber-200">
              <Info className="h-3 w-3" />
              سرویس تیپاکس غیرفعال
            </span>
          ) : testResult?.kind === "invalid" || testResult?.kind === "error" ? (
            <span className="flex shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 text-[10px] font-bold bg-rose-50 text-rose-600 border-rose-200">
              <Info className="h-3 w-3" />
              اتصال نامعتبر
            </span>
          ) : (
            <span className="flex shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 text-[10px] font-bold bg-emerald-50 text-emerald-700 border-emerald-200">
              <CheckCircle2 className="h-3 w-3" />
              متصل
            </span>
          )
        )}
      </div>

      {loading && <p className="text-[11px] text-slate-500">در حال بارگذاری…</p>}

      {!loading && !connected && (
        <>
          <p className="text-[11px] leading-6 text-slate-600">
            اول در تاپین حساب بساز؛ بعد از پنل تاپین (یکپارچه‌سازی ← مدیریت توکن — با آی‌پی سرور و رمز پنل) توکن و شناسه فروشگاه را بگیر و اینجا وارد کن.
          </p>
          <a
            href={TAPIN_SIGNUP_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="flex w-full items-center justify-center gap-1.5 rounded-xl bg-brand-deep py-2.5 min-h-[44px] text-xs font-bold text-white transition-colors hover:bg-brand"
          >
            <Send className="h-3.5 w-3.5" strokeWidth={1.5} />
            ثبت‌نام در تاپین
          </a>
        </>
      )}

      {!loading && (
        <div className="space-y-2.5">
          <div>
            <label className="text-[11px] font-bold text-slate-700 block mb-1">شناسه فروشگاه تاپین</label>
            <input
              type="text"
              value={inputShopId}
              onChange={(e) => setInputShopId(e.target.value)}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden font-bold"
              dir="ltr"
              placeholder="shop id"
            />
          </div>
          <div>
            <label className="text-[11px] font-bold text-slate-700 block mb-1">
              توکن تاپین {connected && <span className="font-normal text-slate-400">(برای تغییر توکن پر کن؛ خالی یعنی همان قبلی)</span>}
            </label>
            <input
              type="password"
              value={inputToken}
              onChange={(e) => setInputToken(e.target.value)}
              className="w-full text-xs p-2.5 rounded-xl bg-slate-50 border border-slate-200 outline-hidden font-bold"
              dir="ltr"
              placeholder="••••••••"
              autoComplete="off"
            />
          </div>
          <p className="flex items-start gap-1.5 text-[10px] leading-5 text-slate-400">
            <Info className="h-3.5 w-3.5 shrink-0 mt-0.5" strokeWidth={1.5} />
            مسیر دریافت توکن: پنل تاپین ← یکپارچه‌سازی ← مدیریت توکن (با آی‌پی سرور و رمز پنل). توکن فقط ذخیره می‌شود و هیچ‌جا نمایش داده نمی‌شود.
          </p>
          <div className="flex gap-2">
            <button type="button" disabled={saving} onClick={() => void save()} className="flex-1 rounded-xl bg-brand-deep py-2.5 min-h-[44px] text-xs font-bold text-white disabled:opacity-50">
              {saving ? "در حال ذخیره…" : "ذخیره اتصال"}
            </button>
            {connected && (
              <>
                <button type="button" onClick={() => void test()} title={testing ? "برای لغو تست، دوباره بزن" : undefined} className="flex-1 rounded-xl border border-slate-200 py-2.5 min-h-[44px] text-xs font-bold text-slate-700">
                  {testing ? (
                    <span className="inline-flex items-center justify-center gap-1.5">
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      در حال تست اتصال…
                    </span>
                  ) : "تست اتصال"}
                </button>
                <button type="button" disabled={disconnecting} onClick={() => void disconnect()} className="rounded-xl border border-rose-200 py-2.5 px-3 min-h-[44px] text-xs font-bold text-rose-600 disabled:opacity-50">
                  {disconnecting ? "…" : "قطع اتصال"}
                </button>
              </>
            )}
          </div>
          {connected && lastTestedAt && (
            <p className="text-[10px] text-slate-400">آخرین تست: {lastTestedAt.toLocaleString('fa-IR')}</p>
          )}
          {connected && (
            <p className="flex items-start gap-1.5 rounded-xl bg-amber-50 px-3 py-2 text-[11px] leading-5 font-bold text-amber-700">
              <Clock className="h-3.5 w-3.5 shrink-0 mt-0.5" strokeWidth={1.5} />
              سرویس فعال این اتصال تیپاکس است و برای هر فروشگاه جداگانه، بر اساس شهر فروشگاه فعال می‌شود؛ اگر وضعیت اتصال گفت سرویس فعال نیست، از پشتیبانی تاپین درخواست فعال‌سازی تیپاکس بده. سرویس پست به‌زودی اضافه می‌شود.
            </p>
          )}
          {testResult && (
            <p className={`flex items-start gap-1.5 rounded-xl px-3 py-2 text-[11px] leading-5 font-bold ${
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
        </div>
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
      <SupplierSectionHeading title="روش‌های ارسال" description="روش‌های فعال فروشگاه را ببین و درخواست فعال‌سازی روش‌های دیگر را ثبت کن." icon={<Truck />} />
      <SupplierSLACard />
      <div className="flex items-start gap-2.5 rounded-2xl border border-brand-border bg-brand-bg p-3.5">
        <Info className="h-4 w-4 shrink-0 mt-0.5 text-brand-deep"  strokeWidth={1.5}/>
        <p className="text-[11px] leading-6 text-slate-600">
          سفارش‌ها را دو جور می‌شود رساند: <strong>ارسال مستقیم</strong> (خودت از انبارت می‌فرستی) یا <strong>ارسال به انبار مرکزی</strong> (کالا را به انبار پلتفرم می‌دهی؛ کنترل کیفیت، بسته‌بندی و فرستادن با پلتفرم است). یکی یا هر دو روش را می‌توانی داشته باشی. فعال‌کردن هر روش با درخواست تو و تأیید مدیر است.
        </p>
      </div>
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
          <section key={method} className="rounded-2xl border bg-white p-4 space-y-3">
            <div className="flex items-start justify-between gap-2">
              <div className="flex items-center gap-2.5">
                <span className="flex h-10 w-10 items-center justify-center rounded-2xl bg-brand-soft text-brand-deep">
                  <MethodIcon className="h-5 w-5" />
                </span>
                <div>
                  <h3 className="text-xs font-extrabold text-slate-800">{meta.title}</h3>
                  <p className="mt-0.5 text-[11px] leading-5 text-slate-500">{meta.description}</p>
                </div>
              </div>
              <span className={`flex shrink-0 items-center gap-1 rounded-full border px-2.5 py-1 text-[10px] font-bold ${status.badgeClass}`}>
                <StatusIcon className="h-3 w-3" />
                {status.label}
              </span>
            </div>
            {row.status === "enabled" && (
              <p className="flex items-center gap-1.5 rounded-xl bg-emerald-50 px-3 py-2 text-[11px] font-bold text-emerald-700">
                <CheckCircle2 className="h-3.5 w-3.5 shrink-0"  strokeWidth={1.5}/>
                این روش فعال است.
              </p>
            )}
            {row.status === "requested" && (
              <p className="flex items-center gap-1.5 rounded-xl bg-amber-50 px-3 py-2 text-[11px] font-bold text-amber-700">
                <Clock className="h-3.5 w-3.5 shrink-0"  strokeWidth={1.5}/>
                درخواستت ثبت شد؛ مدیر باید تأیید کند.
              </p>
            )}
            {row.status === "disabled" && (
              <button type="button" disabled={pending} onClick={() => void request(method)} className="flex w-full items-center justify-center gap-1.5 rounded-xl bg-brand-deep py-2.5 min-h-[44px] text-xs font-bold text-white transition-colors hover:bg-brand disabled:opacity-50">
                <Send className="h-3.5 w-3.5"  strokeWidth={1.5}/>
                {pending ? "در حال ثبت درخواست…" : "می‌خواهم این روش را فعال کنم"}
              </button>
            )}
          </section>
        );
      })}
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
      <SupplierSectionHeading title="قرارداد همکاری" description="متن قرارداد تأمین و توزیع و وضعیت امضای آن را ببین." icon={<FileText />} />
      <div className="overflow-hidden rounded-[22px] border border-[#e6e2f0] bg-white shadow-sm">
        <div className="p-3.5 flex items-center gap-1.5">
          <FileText className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>
          <span className="text-xs font-bold text-slate-800">قرارداد تأمین و توزیع من</span>
          {version > 0 && (
            <span className="text-[10px] bg-brand-soft text-brand-deep px-2 py-0.5 rounded-full font-bold">نسخه فعلی: {toPersianDigits(version)}</span>
          )}
        </div>
        <div className="p-3.5 pt-0 space-y-3">
          <div className="p-3 bg-slate-50 border border-slate-200 rounded-2xl max-h-64 overflow-y-auto text-[11px] text-slate-700 leading-relaxed whitespace-pre-line">
            {text ? text : 'متن قرارداد الان از سرور نرسید؛ چند لحظه دیگر دوباره تلاش کن.'}
          </div>
          {typeof signed === 'number' && signed > 0 && (
            <p className="text-[11px] text-slate-500 leading-relaxed">
              {signed >= version
                ? <>نسخه‌ای که امضا کرده‌ای همین نسخه {toPersianDigits(signed)} است و قراردادت به‌روز است.</>
                : <>نسخه‌ای که امضا کرده‌ای {toPersianDigits(signed)} است؛ نسخه تازه‌تری منتشر شده و برای ادامه کار باید آن را قبول کنی.</>}
            </p>
          )}
        </div>
      </div>
    </div>
  );
};
