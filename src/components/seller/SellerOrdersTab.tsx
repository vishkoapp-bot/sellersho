import React, { useState, useEffect, useCallback } from 'react';
import { useStore } from '../../context/StoreContext';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { OrderDetailModal } from '../common/OrderDetailModal';
import { Package, Truck, CheckCircle2, Clock, Search, Eye, AlertCircle, Info, RefreshCw, XCircle, Loader2 } from 'lucide-react';

// W5 (live parity): seller orders tab rebuilt to the live component
// (SellerDashboardView-MERGED.js, orders component ~byte 170700-178300).
// Live fetches store-scoped orders via GET /stores/{slug}/orders and
// renders informal second-person copy verbatim; the candidate previously
// rendered context orders with formal copy and a single stub empty state.

// Live intro banner (Pe, text-only call): brand-soft box with an info icon.
const OrdersIntroBanner: React.FC = () => (
  <div className="rounded-2xl border border-brand-border bg-brand-soft/50 px-3.5 py-2.5">
    <div className="flex items-start gap-2">
      <Info className="w-4 h-4 text-brand-deep shrink-0 mt-0.5" strokeWidth={1.5} />
      <p className="flex-1 text-xs leading-6 text-slate-700">
        این‌جا سفارش‌های مشتری‌های فروشگاهت را می‌بینی. لازم نیست کاری بکنی؛ بسته را انبار پلتفرم می‌فرستد و سودت خودکار به کیف پولت می‌آید.
      </p>
    </div>
  </div>
);

// Live shared date helper shape (fa-IR, year + long month + day; '—' when
// missing/invalid), as used for Et(createdAt) in the live row header.
const formatOrderDate = (value: string): string => {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('fa-IR', { year: 'numeric', month: 'long', day: 'numeric' });
};

// Live order normalizer is a light camelCase pass-through (the live API
// already serves camelCase); accept snake_case defensively so a raw
// backend row still renders, without changing any field semantics.
const normalizeOrder = (raw: any): any => {
  const ci = raw.customerInfo || {};
  return {
    ...raw,
    orderNumber: String(raw.orderNumber ?? raw.order_number ?? ''),
    trackingCode: String(raw.trackingCode ?? raw.tracking_code ?? ''),
    createdAt: raw.createdAt ?? raw.created_at ?? '',
    payableAmount: raw.payableAmount ?? raw.payable_amount ?? raw.totalAmount ?? raw.total_amount ?? 0,
    dropshipperProfit: raw.dropshipperProfit ?? raw.dropshipper_profit ?? 0,
    paymentStatus: raw.paymentStatus ?? raw.payment_status ?? null,
    disputeReason: raw.disputeReason ?? raw.dispute_reason,
    customerInfo: {
      ...ci,
      province: ci.province ?? raw.customerProvince ?? raw.customer_province ?? '',
      city: ci.city ?? raw.customerCity ?? raw.customer_city ?? '',
    },
    items: (raw.items || []).map((it: any) => ({
      ...it,
      product: it.product || (it.product_title ? { title: it.product_title } : undefined),
      variantName: it.variant?.name ?? it.variant_name ?? it.variantName ?? '',
    })),
  };
};

// Live payment badge (index-MERGED.js j3/N3, ~byte 683196-683528):
// paid "پرداخت شد", pending "در انتظار پرداخت", failed "پرداخت ناموفق",
// cancelled "پرداخت لغو شد", refund_pending "در انتظار بازپرداخت";
// unknown status renders its raw value; null renders nothing.
const PaymentStatusBadge: React.FC<{ status: string | null | undefined }> = ({ status }) => {
  if (!status) return null;
  const map: Record<string, { label: string; cls: string; Icon: any }> = {
    paid: { label: 'پرداخت شد', cls: 'text-emerald-700 bg-emerald-50 border-emerald-200', Icon: CheckCircle2 },
    pending: { label: 'در انتظار پرداخت', cls: 'text-amber-700 bg-amber-50 border-amber-200', Icon: Clock },
    failed: { label: 'پرداخت ناموفق', cls: 'text-rose-700 bg-rose-50 border-rose-200', Icon: XCircle },
    cancelled: { label: 'پرداخت لغو شد', cls: 'text-slate-500 bg-slate-100 border-slate-200', Icon: XCircle },
    refund_pending: { label: 'در انتظار بازپرداخت', cls: 'text-brand-deep bg-brand-soft border-brand-border', Icon: Clock },
  };
  const entry = map[status];
  if (!entry) {
    return (
      <span className="text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border text-slate-500 bg-slate-100 border-slate-200">
        <Clock className="w-3 h-3" />
        <span>{status}</span>
      </span>
    );
  }
  const Icon = entry.Icon;
  return (
    <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full flex items-center gap-1 border ${entry.cls}`}>
      <Icon className="w-3 h-3" />
      <span>{entry.label}</span>
    </span>
  );
};

interface SellerOrdersTabProps {
  onNavigateTab?: (tab: string) => void;
  onOpenStorefront?: () => void;
}

export const SellerOrdersTab: React.FC<SellerOrdersTabProps> = ({ onNavigateTab, onOpenStorefront }) => {
  const { orders, currentStore, formatPrice, toPersianDigits } = useStore();
  const [searchTerm, setSearchTerm] = useState('');
  const [selectedOrder, setSelectedOrder] = useState<any | null>(null);
  const [list, setList] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const reload = useCallback(async () => {
    // Mock mode has no backend: keep the previous context-orders source.
    if (isMockMode()) {
      setList(
        orders
          .filter((o) => o.storeId === currentStore.id || currentStore.ownerType === 'central')
          .map((o) => normalizeOrder(o))
      );
      return;
    }
    const slug = currentStore?.slug;
    if (!slug) {
      setList([]);
      return;
    }
    setLoading(true);
    setLoadError(null);
    try {
      const data = await storeApi.getStoreOrders(slug);
      setList((Array.isArray(data) ? data : []).map((o: any) => normalizeOrder(o)));
    } catch (err: any) {
      setLoadError(err?.message || 'دریافت سفارش‌های فروشگاه انجام نشد.');
    } finally {
      setLoading(false);
    }
  }, [currentStore?.slug, currentStore?.id, currentStore?.ownerType, orders]);

  useEffect(() => {
    reload();
  }, [reload]);

  const filteredOrders = list.filter(
    (o) =>
      (o.orderNumber || '').includes(searchTerm) ||
      (o.trackingCode || '').includes(searchTerm) ||
      (o.customerInfo?.city || '').includes(searchTerm)
  );

  // Live order status badges, verbatim (text-[10px], informal copy).
  const getStatusBadge = (status: string, disputeReason?: string) => {
    switch (status) {
      case 'cancelled':
        return (
          <span className="text-[10px] font-bold text-slate-500 bg-slate-100 border border-slate-200 px-2 py-0.5 rounded-full flex items-center gap-1">
            <XCircle className="w-3 h-3" />
            <span>لغو شده</span>
          </span>
        );
      case 'delivered_confirmed':
      case 'delivered':
        return (
          <span className="text-[10px] font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-2 py-0.5 rounded-full flex items-center gap-1">
            <CheckCircle2 className="w-3 h-3 text-emerald-600" strokeWidth={1.5} />
            <span>به دست مشتری رسید</span>
          </span>
        );
      case 'delivered_unconfirmed':
        return (
          <span className="text-[10px] font-bold text-brand-deep bg-brand-soft border border-brand-border px-2 py-0.5 rounded-full flex items-center gap-1">
            <Clock className="w-3 h-3 text-brand-deep" />
            <span>رسیده؛ منتظر تأیید مشتری</span>
          </span>
        );
      case 'delivery_disputed':
        return (
          <span className="inline-flex flex-col gap-0.5">
            <span className="text-[10px] font-bold text-rose-700 bg-rose-50 border border-rose-200 px-2 py-0.5 rounded-full flex items-center gap-1">
              <AlertCircle className="w-3 h-3" />
              <span>مشتری گفته به دستش نرسیده</span>
            </span>
            {disputeReason ? (
              <span className="text-[10px] font-normal text-rose-700 leading-relaxed">دلیل ثبت‌شده: {disputeReason}</span>
            ) : null}
          </span>
        );
      case 'shipped':
        return (
          <span className="text-[10px] font-bold text-brand-deep bg-brand-soft border border-brand-border px-2 py-0.5 rounded-full flex items-center gap-1">
            <Truck className="w-3 h-3" />
            <span>فرستاده شده؛ در راه است</span>
          </span>
        );
      default:
        return (
          <span className="text-[10px] font-bold text-amber-700 bg-amber-50 border border-amber-200 px-2 py-0.5 rounded-full flex items-center gap-1">
            <Clock className="w-3 h-3" />
            <span>در حال آماده‌سازی در انبار</span>
          </span>
        );
    }
  };

  return (
    <div className="space-y-3 pb-6">
      <OrdersIntroBanner />

      {/* Search Input */}
      <div className="relative flex items-center bg-white border border-slate-200 rounded-xl px-2.5 py-2 shadow-2xs">
        <Search className="w-3.5 h-3.5 text-slate-400 shrink-0 ml-1.5" strokeWidth={1.5} />
        <input
          type="text"
          placeholder="جستجو: شماره سفارش، شهر یا کد رهگیری…"
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
          className="w-full bg-transparent text-xs text-slate-800 placeholder-slate-400 focus:outline-hidden"
        />
      </div>

      {loading ? (
        <div className="bg-white rounded-2xl p-8 text-center border border-slate-100 flex flex-col items-center gap-2">
          <Loader2 className="w-6 h-6 text-brand-deep animate-spin" />
          <p className="text-xs text-slate-500">در حال دریافت سفارش‌های فروشگاه…</p>
        </div>
      ) : loadError ? (
        <div className="bg-white rounded-2xl p-8 text-center border border-rose-100 space-y-2">
          <AlertCircle className="w-8 h-8 text-rose-400 mx-auto" />
          <p className="text-xs font-bold text-slate-600">{loadError}</p>
          <button
            type="button"
            onClick={() => void reload()}
            className="inline-flex items-center gap-1 text-[11px] font-bold text-brand-deep bg-brand-soft hover:bg-brand-border px-4 py-2 rounded-xl transition-colors"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            <span>تلاش مجدد</span>
          </button>
        </div>
      ) : filteredOrders.length === 0 ? (
        <div className="bg-white rounded-2xl p-8 text-center border border-slate-100 space-y-2.5">
          <Package className="w-8 h-8 text-slate-300 mx-auto" strokeWidth={1.5} />
          {searchTerm ? (
            <>
              <p className="text-xs font-bold text-slate-600">با این جستجو سفارشی پیدا نشد.</p>
              <p className="text-[11px] text-slate-400 leading-6">کلمه دیگری امتحان کن یا جستجو را پاک کن تا همه سفارش‌ها را ببینی.</p>
              <button
                type="button"
                onClick={() => setSearchTerm('')}
                className="min-h-[44px] px-4 rounded-xl bg-brand-soft text-brand-deep text-xs font-bold"
              >
                پاک کردن جستجو
              </button>
            </>
          ) : (
            <>
              <p className="text-xs font-bold text-slate-600">هنوز سفارشی نداری</p>
              <p className="text-[11px] text-slate-400 leading-6">وقتی مشتری از فروشگاهت خرید کند، سفارشش این‌جا می‌آید. برای اولین فروش، لینک فروشگاهت را برای دوست‌ها و مشتری‌ها بفرست.</p>
              <div className="flex items-center justify-center gap-2 pt-1">
                {onNavigateTab && (
                  <button
                    type="button"
                    onClick={() => onNavigateTab('links')}
                    className="min-h-[44px] px-4 rounded-xl bg-brand-deep text-white text-xs font-bold"
                  >
                    گرفتن لینک فروشگاه
                  </button>
                )}
                {onOpenStorefront && (
                  <button
                    type="button"
                    onClick={onOpenStorefront}
                    className="min-h-[44px] px-4 rounded-xl bg-slate-100 text-slate-700 text-xs font-bold"
                  >
                    مشاهده فروشگاه من
                  </button>
                )}
              </div>
            </>
          )}
        </div>
      ) : (
        <div className="space-y-2.5">
          {filteredOrders.map((order) => (
            <div
              key={order.id}
              className="bg-white rounded-2xl p-3 border border-slate-200/90 shadow-2xs space-y-2.5"
            >
              {/* Row 1: Order ID, Date & Statuses */}
              <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                <div>
                  <span className="text-xs font-black text-slate-800 font-mono">
                    {order.orderNumber}
                  </span>
                  <span className="text-[10px] text-slate-400 block mt-0.5 font-mono">
                    {formatOrderDate(order.createdAt)}
                  </span>
                </div>
                <div className="flex items-center gap-1.5 flex-wrap justify-end">
                  {getStatusBadge(order.status, order.disputeReason)}
                  <PaymentStatusBadge status={order.paymentStatus} />
                </div>
              </div>

              {/* Row 2: Shipping Destination & Tracking */}
              <div className="text-[11px] text-slate-600 space-y-1">
                <div className="flex justify-between">
                  <span className="text-slate-400">می‌رود به شهر:</span>
                  <span className="font-bold text-slate-800">{order.customerInfo?.province}، {order.customerInfo?.city}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-slate-400">کد رهگیری پستی:</span>
                  <span className="font-mono text-brand-deep font-bold" dir="ltr">
                    {order.trackingCode}
                  </span>
                </div>
              </div>

              {/* Row 3: Items preview */}
              {order.items && order.items.length > 0 && (
                <div className="bg-slate-50 rounded-xl p-2 text-[10px] text-slate-600 space-y-1">
                  {order.items.map((item: any, idx: number) => (
                    <div key={idx} className="flex justify-between truncate">
                      <span className="truncate">
                        {item.product?.title || 'کالای سفارشی'}
                        {item.variantName ? <span className="text-slate-400"> — {item.variantName}</span> : null}
                      </span>
                      <span className="shrink-0 mr-2 font-bold">{toPersianDigits(item.quantity)} عدد</span>
                    </div>
                  ))}
                </div>
              )}

              {/* Row 4: Financial Summary */}
              <div className="flex items-center justify-between pt-1 border-t border-slate-100 text-xs">
                <div>
                  <span className="text-[10px] text-slate-400 block">مبلغ سفارش:</span>
                  <span className="font-bold text-slate-800">{formatPrice(order.payableAmount)} تومان</span>
                </div>
                <div className="text-left">
                  <span className="text-[10px] text-emerald-600 block font-medium">سود تو از این سفارش:</span>
                  <span className="font-black text-emerald-600 text-sm">
                    +{formatPrice(order.dropshipperProfit)} تومان
                  </span>
                </div>
              </div>

              {/* Action Buttons */}
              <div className="pt-1 grid grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setSelectedOrder(order)}
                  className="py-2.5 min-h-[44px] bg-brand-soft hover:bg-brand-border text-brand-deep font-bold text-xs rounded-lg transition-colors flex items-center justify-center gap-1"
                >
                  <Eye className="w-4 h-4" strokeWidth={1.5} />
                  <span>دیدن جزئیات</span>
                </button>
                {/* Live labels this "چاپ فاکتور" and opens its invoice print
                    modal; the candidate has no invoice print component, so
                    the label is ported and window.print() is retained
                    (see report: print-modal behavior left unported). */}
                <button
                  type="button"
                  onClick={() => window.print()}
                  className="py-2.5 min-h-[44px] bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold text-xs rounded-lg transition-colors flex items-center justify-center gap-1"
                >
                  <span>چاپ فاکتور</span>
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      <OrderDetailModal
        isOpen={!!selectedOrder}
        order={selectedOrder}
        hideSensitiveCustomerInfo={true}
        onClose={() => setSelectedOrder(null)}
      />
    </div>
  );
};
