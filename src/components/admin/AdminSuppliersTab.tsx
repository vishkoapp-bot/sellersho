import React, { useCallback, useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest } from '../../services/apiClient';
import { formatPrice } from '../../utils/formatters';
import { Building2, Star, Truck, FileText, Eye, X } from 'lucide-react';

// Team 06 live-parity (D54/D15): this tab used to render mockSuppliers with
// local-only state. It now loads real suppliers from
// GET /api/v1/admin/suppliers/list and drives approve/reject and the
// direct-shipping decision through real admin endpoints. No supplier row
// is fabricated locally: the "add supplier" form that created local-only
// rows was removed, because no admin create-supplier endpoint is
// confirmed in the live build (suppliers are created by registration).
// The exact live approve/reject route is unverified (U06); the routes
// used here are the reconstruction choice under /admin/suppliers/.
//
// Item 6 (2026-11-13): cards are clickable and open a detail modal
// backed by GET /api/v1/admin/suppliers/:id (single-supplier detail
// endpoint). The card's stored totalProducts / rating / deliveryScore
// are STORED columns of unverified freshness — they are rendered as
// stored figures, never as live-computed ones; the live-computed
// counts (products / active / variants / proposals / orders) come
// from the detail endpoint and are shown inside the modal. The old
// card label «تعداد تنوع در کاتالوگ» was WRONG (totalProducts is a
// product count, not a variant count) and is fixed to «تعداد کالا».

export interface AdminSupplierRow {
  id: string;
  name: string;
  logoUrl?: string;
  rating: number;
  totalProducts: number;
  deliveryScore: number;
  warehouseCity: string;
  warehouseCityConfirmed?: boolean;
  warehouseCitySource?: string | null;
  approvalStatus?: string;
  signedContractVersion?: number;
  platformContractVersion?: number | null;
  defaultMarkupType?: string | null;
  defaultMarkupValue?: number | null;
  rejectionReason?: string | null;
  tapinShopId?: string | null;
  tapinConnected?: boolean;
  suspendedAt?: string | null;
  suspendedBy?: string | null;
  suspensionReason?: string | null;
  description?: string | null;
  badge?: string | null;
  approvedBy?: string | null;
  approvedAt?: string | null;
  createdAt?: string | null;
  supportsDirectShipping?: boolean;
  owner?: SupplierDetailOwner | null;
}

export interface SupplierDetailOwner {
  userId: string;
  fullName: string;
  mobile: string;
  email: string;
  isActive: boolean;
}

export interface SupplierDetailCounts {
  products: number;
  activeProducts: number;
  variants: number;
  proposals: { pending: number; approved: number; rejected: number };
  orders: { paid: number; directShipmentsByStatus: { [status: string]: number } };
}

export interface SupplierDetailWarehouse {
  id: string;
  name: string;
  provinceName: string;
  cityName: string;
  address?: string;
  isActive: boolean;
}

export interface SupplierDetailMember {
  id: string;
  fullName: string;
  username: string;
  mobile: string;
  roleKey: string;
  isActive: boolean;
}

export interface SupplierDetailShippingMethod {
  supplierId: string;
  method: string;
  status: string;
}

export interface SupplierDetailFinance {
  currencyUnit: string;
  balance: {
    earned: number;
    reserved: number;
    paid: number;
    returnCharges: number;
    returnClawbacks: number;
    adjustments: number;
    storageFees: number;
    available: number;
  } | null;
  settlements: { id: string; amount: number; status: string; created_at: string }[];
}

export interface SupplierDetailHistoryRow {
  id: string;
  timestamp: string;
  operator: string;
  action: string;
  details: string;
}

export interface SupplierDetail {
  supplier: AdminSupplierRow;
  counts: SupplierDetailCounts;
  warehouses: SupplierDetailWarehouse[];
  members: SupplierDetailMember[];
  shippingMethods: SupplierDetailShippingMethod[];
  tapin: { shopId: string; connected: boolean };
  finance: SupplierDetailFinance;
  history: SupplierDetailHistoryRow[];
  gaps: string[];
}

interface ShippingMethodRow {
  supplierId: string;
  method: string;
  status: string;
}

type StatusFilter = 'all' | 'pending_approval' | 'approved' | 'rejected' | 'suspended';

const statusLabel = (status?: string): string => {
  switch (status) {
    case 'pending_approval':
      return 'در انتظار تأیید';
    case 'approved':
      return 'تأییدشده';
    case 'rejected':
      return 'ردشده';
    case 'suspended':
      return 'تعلیق‌شده';
    default:
      return status || 'نامشخص';
  }
};

// The stored default-markup pair has no verified value vocabulary in
// this tree (type is a free string, value a number). Render the
// percent and fixed-amount shapes explicitly; anything else is shown
// raw instead of guessed.
export const formatMarkup = (
  type: string | null | undefined,
  value: number | null | undefined,
  toPersianDigits: (v: string | number) => string,
): string | null => {
  if (value == null) return null;
  if (type === 'percent' || type === 'percentage') return `٪${toPersianDigits(value)}`;
  if (type === 'fixed' || type === 'amount' || type === 'toman') return `${formatPrice(value)} تومان`;
  return type ? `${toPersianDigits(value)} (${type})` : toPersianDigits(value);
};

// Audit details are a JSON string ({"supplier_id": ..., "reason": ...}).
// Pull the human reason out when it parses; otherwise show nothing
// rather than raw JSON.
export const auditHistoryReason = (details: string): string | null => {
  try {
    const parsed = JSON.parse(details) as { reason?: unknown };
    return typeof parsed.reason === 'string' && parsed.reason.trim() !== '' ? parsed.reason : null;
  } catch {
    return null;
  }
};

const datePart = (iso: string | null | undefined, toPersianDigits: (v: string | number) => string): string =>
  iso ? toPersianDigits(iso.slice(0, 10)) : '—';

export interface AdminSupplierCardViewProps {
  supplier: AdminSupplierRow;
  hasDirectShip: boolean;
  busyId: string | null;
  toPersianDigits: (v: string | number) => string;
  onOpenDetail: (supplier: AdminSupplierRow) => void;
  onContract: (supplier: AdminSupplierRow) => void;
  onApprove: (id: string) => void;
  onReject: (supplier: AdminSupplierRow) => void;
  onSuspend: (supplier: AdminSupplierRow) => void;
  onReactivate: (id: string) => void;
  onToggleDirectShip: (id: string) => void;
}

// Presentational card (no providers): the container passes the store
// helpers and the action callbacks. The whole card opens the detail
// modal; every action button stops propagation so it does not.
export const AdminSupplierCardView: React.FC<AdminSupplierCardViewProps> = ({
  supplier,
  hasDirectShip,
  busyId,
  toPersianDigits,
  onOpenDetail,
  onContract,
  onApprove,
  onReject,
  onSuspend,
  onReactivate,
  onToggleDirectShip,
}) => {
  const isPending = supplier.approvalStatus === 'pending_approval';
  const isApproved = supplier.approvalStatus === 'approved';
  const isSuspended = supplier.approvalStatus === 'suspended';
  const markup = formatMarkup(supplier.defaultMarkupType, supplier.defaultMarkupValue, toPersianDigits);
  const stop = (e: React.MouseEvent) => e.stopPropagation();
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`مشاهده جزئیات ${supplier.name}`}
      onClick={() => onOpenDetail(supplier)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onOpenDetail(supplier);
        }
      }}
      className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2.5 cursor-pointer hover:border-blue-300"
    >
      <div className="flex items-center justify-between border-b border-slate-100 pb-2">
        <div className="flex items-center gap-2">
          {supplier.logoUrl ? (
            <img
              src={supplier.logoUrl}
              alt={supplier.name}
              className="w-8 h-8 rounded-full object-contain border border-slate-100 p-0.5"
            />
          ) : (
            <Building2 className="w-8 h-8 text-slate-300" strokeWidth={1.5} />
          )}
          <div>
            <h4 className="text-xs font-bold text-slate-900">{supplier.name}</h4>
            <span className="text-[10px] text-slate-400">{supplier.warehouseCity}</span>
          </div>
        </div>

        <div className="flex items-center gap-1.5">
          <span className="text-[10px] font-bold text-slate-600">{statusLabel(supplier.approvalStatus)}</span>
          <div className="flex items-center gap-1 text-xs text-amber-500 font-bold">
            <Star className="w-3.5 h-3.5 fill-current" strokeWidth={1.5} />
            <span>{toPersianDigits(supplier.rating)}</span>
          </div>
          <button
            type="button"
            onClick={(e) => {
              stop(e);
              onOpenDetail(supplier);
            }}
            aria-label={`مشاهده جزئیات ${supplier.name}`}
            title="مشاهده جزئیات"
            className="p-1.5 bg-slate-100 hover:bg-slate-200 text-slate-600 rounded-lg"
          >
            <Eye className="w-3.5 h-3.5" strokeWidth={1.5} />
          </button>
        </div>
      </div>

      {supplier.badge && (
        <span className="inline-block text-[10px] font-bold text-blue-700 bg-blue-50 border border-blue-100 rounded-lg px-2 py-0.5">
          {supplier.badge}
        </span>
      )}
      {supplier.description && (
        <p className="text-[10px] text-slate-500 leading-relaxed">{supplier.description}</p>
      )}

      <div className="grid grid-cols-2 gap-2 text-[10px] text-slate-600">
        <div>تعداد کالا در کاتالوگ: <strong className="text-slate-800">{toPersianDigits(supplier.totalProducts)} قلم</strong></div>
        <div>امتیاز تحویل بموقع: <strong className="text-emerald-600">٪{toPersianDigits(supplier.deliveryScore)}</strong></div>
        <div>نسخه قرارداد امضاشده: <strong className="text-slate-800">{supplier.signedContractVersion != null ? toPersianDigits(supplier.signedContractVersion) : '—'}</strong></div>
        <div>شهر انبار تأییدشده: <strong className="text-slate-800">{supplier.warehouseCityConfirmed ? 'بله' : 'خیر'}</strong></div>
        {markup && (
          <div>سود پیش‌فرض توافق: <strong className="text-slate-800">{markup}</strong></div>
        )}
        <div>اتصال تاپین: <strong className="text-slate-800">{supplier.tapinConnected ? 'متصل' : 'متصل نیست'}</strong></div>
        {supplier.approvedBy && (
          <div>تأییدکننده: <strong className="text-slate-800">{supplier.approvedBy}</strong></div>
        )}
        {supplier.createdAt && (
          <div>تاریخ ثبت: <strong className="text-slate-800">{datePart(supplier.createdAt, toPersianDigits)}</strong></div>
        )}
      </div>

      {supplier.rejectionReason && (
        <div className="text-[10px] text-rose-700 bg-rose-50 border border-rose-100 rounded-xl p-2">
          دلیل رد: {supplier.rejectionReason}
        </div>
      )}

      {isSuspended && supplier.suspensionReason && (
        <div className="text-[10px] text-amber-700 bg-amber-50 border border-amber-100 rounded-xl p-2">
          دلیل تعلیق: {supplier.suspensionReason}
        </div>
      )}

      <div className="pt-2 border-t border-slate-100 flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <Truck className="w-3.5 h-3.5 text-slate-400" strokeWidth={1.5} />
          <span className="text-[10px] text-slate-600">مجوز استثنای ارسال مستقیم:</span>
        </div>

        <div className="flex items-center gap-1.5" onClick={stop}>
          <button
            onClick={() => onContract(supplier)}
            className="px-2 py-1 text-[10px] font-bold rounded-lg bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center gap-1"
          >
            <FileText className="w-3 h-3" strokeWidth={1.5} />
            <span>قرارداد</span>
          </button>
          {isPending && (
            <>
              <button
                onClick={() => onApprove(supplier.id)}
                disabled={busyId === supplier.id}
                className="px-2.5 py-1 text-[10px] font-bold rounded-lg bg-emerald-600 text-white disabled:opacity-50"
              >
                تأیید
              </button>
              <button
                onClick={() => onReject(supplier)}
                disabled={busyId === supplier.id}
                className="px-2.5 py-1 text-[10px] font-bold rounded-lg bg-rose-100 text-rose-700 disabled:opacity-50"
              >
                رد
              </button>
            </>
          )}
          {isApproved && (
            <button
              onClick={() => onSuspend(supplier)}
              disabled={busyId === supplier.id}
              className="px-2.5 py-1 text-[10px] font-bold rounded-lg bg-amber-100 text-amber-700 disabled:opacity-50"
            >
              تعلیق
            </button>
          )}
          {isSuspended && (
            <button
              onClick={() => onReactivate(supplier.id)}
              disabled={busyId === supplier.id}
              className="px-2.5 py-1 text-[10px] font-bold rounded-lg bg-emerald-600 text-white disabled:opacity-50"
            >
              فعال‌سازی مجدد
            </button>
          )}
          <button
            onClick={() => onToggleDirectShip(supplier.id)}
            disabled={busyId === supplier.id}
            className={`px-2.5 py-1 text-[10px] font-bold rounded-lg transition-colors disabled:opacity-50 ${
              hasDirectShip
                ? 'bg-blue-600 text-white'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
            }`}
          >
            {hasDirectShip ? 'فعال (با مجوز)' : 'غیرفعال (ارسال انبار)'}
          </button>
        </div>
      </div>
    </div>
  );
};

export interface AdminSupplierDetailViewProps {
  supplier: AdminSupplierRow;
  detail: SupplierDetail | null;
  detailLoading: boolean;
  detailError: string | null;
  hasDirectShip: boolean;
  busyId: string | null;
  toPersianDigits: (v: string | number) => string;
  onClose: () => void;
  onRetryDetail: () => void;
  onContract: (supplier: AdminSupplierRow) => void;
  onApprove: (id: string) => void;
  onReject: (supplier: AdminSupplierRow) => void;
  onSuspend: (supplier: AdminSupplierRow) => void;
  onReactivate: (id: string) => void;
  onToggleDirectShip: (id: string) => void;
}

// Presentational detail modal (no providers). Hosts the same actions
// the card offers — approve / reject / suspend / reactivate /
// direct-ship toggle / contract — by calling the container's existing
// functions unchanged; the container reloads list + detail afterwards.
export const AdminSupplierDetailView: React.FC<AdminSupplierDetailViewProps> = ({
  supplier,
  detail,
  detailLoading,
  detailError,
  hasDirectShip,
  busyId,
  toPersianDigits,
  onClose,
  onRetryDetail,
  onContract,
  onApprove,
  onReject,
  onSuspend,
  onReactivate,
  onToggleDirectShip,
}) => {
  const isPending = supplier.approvalStatus === 'pending_approval';
  const isApproved = supplier.approvalStatus === 'approved';
  const isSuspended = supplier.approvalStatus === 'suspended';
  const owner = detail?.supplier.owner ?? null;
  const counts = detail?.counts ?? null;
  const directShipCount = counts
    ? Object.values(counts.orders.directShipmentsByStatus).reduce((a, b) => a + b, 0)
    : 0;
  const sectionTitle = 'text-[11px] font-extrabold text-slate-800 border-b border-slate-100 pb-1';
  return (
    <div className="fixed inset-0 z-50 bg-slate-900/60 backdrop-blur-xs flex items-center justify-center p-4">
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`جزئیات تأمین‌کننده ${supplier.name}`}
        className="bg-white rounded-2xl max-w-lg w-full max-h-[90vh] overflow-y-auto p-4 border border-slate-200 shadow-2xl space-y-3 text-xs"
      >
        <div className="flex items-center justify-between border-b border-slate-100 pb-2">
          <div>
            <h4 className="font-extrabold text-slate-900 text-sm">{supplier.name}</h4>
            <span className="text-[10px] text-slate-400">
              {statusLabel(supplier.approvalStatus)} · {supplier.warehouseCity}
            </span>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="بستن جزئیات"
            className="px-2 py-1 bg-slate-100 rounded-lg font-bold text-slate-600"
          >
            <X className="w-4 h-4" strokeWidth={1.5} />
          </button>
        </div>

        {detailLoading && (
          <p className="text-[11px] text-slate-500">در حال دریافت جزئیات تأمین‌کننده…</p>
        )}
        {!detailLoading && detailError && (
          <div className="text-[11px] text-rose-700 bg-rose-50 border border-rose-100 rounded-xl p-2 space-y-2">
            <p>{detailError}</p>
            <button onClick={onRetryDetail} className="px-2.5 py-1 bg-rose-600 text-white font-bold rounded-lg">
              تلاش دوباره
            </button>
          </div>
        )}
        {!detailLoading && !detailError && detail && (
          <>
            <div className="grid grid-cols-3 gap-2 text-center">
              <div className="bg-slate-50 rounded-xl p-2">
                <div className="text-sm font-black text-slate-800">{toPersianDigits(counts?.products ?? 0)}</div>
                <div className="text-[10px] text-slate-500">کالا (محاسبه‌شده)</div>
              </div>
              <div className="bg-slate-50 rounded-xl p-2">
                <div className="text-sm font-black text-slate-800">{toPersianDigits(counts?.activeProducts ?? 0)}</div>
                <div className="text-[10px] text-slate-500">کالای فعال</div>
              </div>
              <div className="bg-slate-50 rounded-xl p-2">
                <div className="text-sm font-black text-slate-800">{toPersianDigits(counts?.variants ?? 0)}</div>
                <div className="text-[10px] text-slate-500">تنوع کالا</div>
              </div>
              <div className="bg-slate-50 rounded-xl p-2">
                <div className="text-sm font-black text-slate-800">{toPersianDigits(counts?.proposals.pending ?? 0)}</div>
                <div className="text-[10px] text-slate-500">پیشنهاد در انتظار</div>
              </div>
              <div className="bg-slate-50 rounded-xl p-2">
                <div className="text-sm font-black text-slate-800">{toPersianDigits(counts?.orders.paid ?? 0)}</div>
                <div className="text-[10px] text-slate-500">سفارش پرداخت‌شده</div>
              </div>
              <div className="bg-slate-50 rounded-xl p-2">
                <div className="text-sm font-black text-slate-800">{toPersianDigits(directShipCount)}</div>
                <div className="text-[10px] text-slate-500">ارسال مستقیم</div>
              </div>
            </div>
            <p className="text-[10px] text-slate-400 leading-relaxed">
              امتیاز، امتیاز تحویل و تعداد کالای روی کارت، مقادیر ثبت‌شده در پرونده‌اند، نه محاسبهٔ زنده؛ شمارش‌های بالا از داده‌های واقعی محاسبه شده‌اند.
            </p>

            <div className="space-y-1.5">
              <h5 className={sectionTitle}>مالک و تماس</h5>
              {owner ? (
                <div className="space-y-1 text-slate-600">
                  <div className="flex justify-between"><span>نام:</span><strong className="text-slate-900">{owner.fullName}</strong></div>
                  <div className="flex justify-between"><span>موبایل:</span><strong className="text-slate-900" dir="ltr">{owner.mobile}</strong></div>
                  <div className="flex justify-between"><span>ایمیل:</span><strong className="text-slate-900" dir="ltr">{owner.email || '—'}</strong></div>
                  <div className="flex justify-between"><span>حساب کاربری:</span><strong className="text-slate-900">{owner.isActive ? 'فعال' : 'غیرفعال'}</strong></div>
                </div>
              ) : (
                <p className="text-[11px] text-slate-500">مالکی برای این رکورد ثبت نشده است (رکورد مرکزی پلتفرم).</p>
              )}
              {supplier.approvedBy && (
                <div className="flex justify-between text-slate-600"><span>تأییدکننده:</span><strong className="text-slate-900">{supplier.approvedBy}</strong></div>
              )}
              {supplier.approvedAt && (
                <div className="flex justify-between text-slate-600"><span>تاریخ تأیید:</span><strong className="text-slate-900">{datePart(supplier.approvedAt, toPersianDigits)}</strong></div>
              )}
            </div>

            <div className="space-y-1.5">
              <h5 className={sectionTitle}>انبارها</h5>
              {detail.warehouses.length === 0 ? (
                <p className="text-[11px] text-slate-500">انباری ثبت نشده است.</p>
              ) : (
                detail.warehouses.map((w) => (
                  <div key={w.id} className="flex justify-between text-slate-600">
                    <span>{w.name}</span>
                    <strong className="text-slate-900">{w.cityName}، {w.provinceName}{w.isActive ? '' : ' (غیرفعال)'}</strong>
                  </div>
                ))
              )}
            </div>

            <div className="space-y-1.5">
              <h5 className={sectionTitle}>اعضای تیم</h5>
              {detail.members.length === 0 ? (
                <p className="text-[11px] text-slate-500">عضو دیگری ثبت نشده است.</p>
              ) : (
                detail.members.map((m) => (
                  <div key={m.id} className="flex justify-between text-slate-600">
                    <span>{m.fullName} ({m.roleKey})</span>
                    <strong className="text-slate-900" dir="ltr">{m.mobile}{m.isActive ? '' : ' — غیرفعال'}</strong>
                  </div>
                ))
              )}
            </div>

            <div className="space-y-1.5">
              <h5 className={sectionTitle}>تاپین و ارسال</h5>
              <div className="flex justify-between text-slate-600"><span>فروشگاه تاپین:</span><strong className="text-slate-900" dir="ltr">{detail.tapin.shopId || '—'}</strong></div>
              <div className="flex justify-between text-slate-600"><span>اتصال تاپین:</span><strong className="text-slate-900">{detail.tapin.connected ? 'متصل' : 'متصل نیست'}</strong></div>
              <div className="flex justify-between text-slate-600"><span>ارسال مستقیم:</span><strong className="text-slate-900">{hasDirectShip ? 'فعال (با مجوز)' : 'غیرفعال (ارسال انبار)'}</strong></div>
              {detail.shippingMethods.length === 0 ? (
                <p className="text-[11px] text-slate-500">روش ارسالی ثبت نشده است.</p>
              ) : (
                detail.shippingMethods.map((m) => (
                  <div key={`${m.supplierId}-${m.method}`} className="flex justify-between text-slate-600">
                    <span>روش {m.method}</span>
                    <strong className="text-slate-900">{m.status === 'enabled' ? 'فعال' : 'غیرفعال'}</strong>
                  </div>
                ))
              )}
            </div>

            <div className="space-y-1.5">
              <h5 className={sectionTitle}>مالی (تومان)</h5>
              {detail.finance.balance ? (
                <div className="space-y-1 text-slate-600">
                  <div className="flex justify-between"><span>درآمد:</span><strong className="text-slate-900">{formatPrice(detail.finance.balance.earned)} تومان</strong></div>
                  <div className="flex justify-between"><span>رزرو شده:</span><strong className="text-slate-900">{formatPrice(detail.finance.balance.reserved)} تومان</strong></div>
                  <div className="flex justify-between"><span>پرداخت‌شده:</span><strong className="text-slate-900">{formatPrice(detail.finance.balance.paid)} تومان</strong></div>
                  <div className="flex justify-between"><span>قابل برداشت:</span><strong className="text-emerald-700">{formatPrice(detail.finance.balance.available)} تومان</strong></div>
                </div>
              ) : (
                <p className="text-[11px] text-slate-500">موجودی در دسترس نیست.</p>
              )}
              {detail.finance.settlements.length === 0 ? (
                <p className="text-[11px] text-slate-500">تسویه‌ای ثبت نشده است.</p>
              ) : (
                detail.finance.settlements.map((s) => (
                  <div key={s.id} className="flex justify-between text-slate-600">
                    <span>{s.status} · {datePart(s.created_at, toPersianDigits)}</span>
                    <strong className="text-slate-900">{formatPrice(s.amount)} تومان</strong>
                  </div>
                ))
              )}
            </div>

            <div className="space-y-1.5">
              <h5 className={sectionTitle}>قرارداد</h5>
              <div className="flex justify-between text-slate-600"><span>نسخهٔ امضاشده:</span><strong className="text-slate-900">{supplier.signedContractVersion != null ? toPersianDigits(supplier.signedContractVersion) : '—'}</strong></div>
              <div className="flex justify-between text-slate-600"><span>نسخهٔ جاری پلتفرم:</span><strong className="text-slate-900">{supplier.platformContractVersion != null ? toPersianDigits(supplier.platformContractVersion) : 'نامشخص'}</strong></div>
            </div>

            <div className="space-y-1.5">
              <h5 className={sectionTitle}>تاریخچهٔ مدیریتی</h5>
              {detail.history.length === 0 ? (
                <p className="text-[11px] text-slate-500">رویداد مدیریتی ثبت‌شده‌ای برای این تأمین‌کننده وجود ندارد.</p>
              ) : (
                detail.history.map((hrow) => (
                  <div key={hrow.id} className="text-slate-600 space-y-0.5">
                    <div className="flex justify-between">
                      <span>{hrow.action} · {hrow.operator}</span>
                      <strong className="text-slate-900">{datePart(hrow.timestamp, toPersianDigits)}</strong>
                    </div>
                    {auditHistoryReason(hrow.details) && (
                      <p className="text-[10px] text-slate-500">دلیل: {auditHistoryReason(hrow.details)}</p>
                    )}
                  </div>
                ))
              )}
              {detail.gaps.filter((g) => g.includes('تأیید/رد')).map((g) => (
                <p key={g} className="text-[10px] text-amber-700 bg-amber-50 border border-amber-100 rounded-xl p-2 leading-relaxed">
                  {g}
                </p>
              ))}
            </div>

            <div className="pt-2 border-t border-slate-100 flex flex-wrap items-center gap-1.5">
              <button
                onClick={() => onContract(supplier)}
                className="px-2.5 py-1.5 text-[10px] font-bold rounded-lg bg-slate-100 hover:bg-slate-200 text-slate-700 flex items-center gap-1"
              >
                <FileText className="w-3 h-3" strokeWidth={1.5} />
                <span>قرارداد</span>
              </button>
              {isPending && (
                <>
                  <button
                    onClick={() => onApprove(supplier.id)}
                    disabled={busyId === supplier.id}
                    className="px-2.5 py-1.5 text-[10px] font-bold rounded-lg bg-emerald-600 text-white disabled:opacity-50"
                  >
                    تأیید
                  </button>
                  <button
                    onClick={() => onReject(supplier)}
                    disabled={busyId === supplier.id}
                    className="px-2.5 py-1.5 text-[10px] font-bold rounded-lg bg-rose-100 text-rose-700 disabled:opacity-50"
                  >
                    رد
                  </button>
                </>
              )}
              {isApproved && (
                <button
                  onClick={() => onSuspend(supplier)}
                  disabled={busyId === supplier.id}
                  className="px-2.5 py-1.5 text-[10px] font-bold rounded-lg bg-amber-100 text-amber-700 disabled:opacity-50"
                >
                  تعلیق
                </button>
              )}
              {isSuspended && (
                <button
                  onClick={() => onReactivate(supplier.id)}
                  disabled={busyId === supplier.id}
                  className="px-2.5 py-1.5 text-[10px] font-bold rounded-lg bg-emerald-600 text-white disabled:opacity-50"
                >
                  فعال‌سازی مجدد
                </button>
              )}
              <button
                onClick={() => onToggleDirectShip(supplier.id)}
                disabled={busyId === supplier.id}
                className={`px-2.5 py-1.5 text-[10px] font-bold rounded-lg transition-colors disabled:opacity-50 ${
                  hasDirectShip ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
                }`}
              >
                {hasDirectShip ? 'ارسال مستقیم: فعال' : 'ارسال مستقیم: غیرفعال'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
};

export const AdminSuppliersTab: React.FC = () => {
  const { toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [suppliersList, setSuppliersList] = useState<AdminSupplierRow[]>([]);
  const [directShip, setDirectShip] = useState<{ [key: string]: boolean }>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [selectedContractSupplier, setSelectedContractSupplier] = useState<AdminSupplierRow | null>(null);
  const [rejectSupplier, setRejectSupplier] = useState<AdminSupplierRow | null>(null);
  const [rejectReason, setRejectReason] = useState('');
  const [suspendSupplier, setSuspendSupplier] = useState<AdminSupplierRow | null>(null);
  const [suspendReason, setSuspendReason] = useState('');
  const [busyId, setBusyId] = useState<string | null>(null);
  const [detailSupplier, setDetailSupplier] = useState<AdminSupplierRow | null>(null);
  const [detail, setDetail] = useState<SupplierDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);

  const loadSuppliers = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      // /records (not /list) so the contract modal also has rejection
      // reason and Tapin connection state, which /list omits.
      // PERF-02 Step 9: /records now pages (envelope with `suppliers`).
      const records = await apiRequest<any>('/admin/suppliers/records', { method: 'GET', auth: true });
      const suppliers = Array.isArray(records) ? records : (records?.suppliers ?? []);
      setSuppliersList(Array.isArray(suppliers) ? suppliers : []);
      try {
        const methods = await apiRequest<ShippingMethodRow[]>('/admin/suppliers/shipping-methods', { method: 'GET', auth: true });
        const toggles: { [key: string]: boolean } = {};
        (Array.isArray(methods) ? methods : []).forEach((m) => {
          if (m.method === 'direct') toggles[m.supplierId] = m.status === 'enabled';
        });
        setDirectShip(toggles);
      } catch {
        // Shipping-method rows are supplementary; the supplier list is the
        // primary data and must still render if this call fails.
        setDirectShip({});
      }
    } catch (err: any) {
      setLoadError(err?.message || 'خطا در دریافت فهرست تأمین‌کنندگان');
      setSuppliersList([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSuppliers();
  }, [loadSuppliers]);

  const loadDetail = useCallback(async (id: string) => {
    setDetailLoading(true);
    setDetailError(null);
    try {
      const d = await apiRequest<SupplierDetail>(`/admin/suppliers/${encodeURIComponent(id)}`, { method: 'GET', auth: true });
      setDetail(d);
    } catch (err: any) {
      setDetailError(err?.message || 'خطا در دریافت جزئیات تأمین‌کننده');
      setDetail(null);
    } finally {
      setDetailLoading(false);
    }
  }, []);

  const openDetail = useCallback(
    (supplier: AdminSupplierRow) => {
      setDetailSupplier(supplier);
      setDetail(null);
      void loadDetail(supplier.id);
    },
    [loadDetail],
  );

  const closeDetail = useCallback(() => {
    setDetailSupplier(null);
    setDetail(null);
    setDetailError(null);
  }, []);

  // After any action, reload the list AND the open detail (when one is
  // open) so the modal never shows a stale status.
  const refreshAfterAction = useCallback(async () => {
    await loadSuppliers();
    if (detailSupplier) await loadDetail(detailSupplier.id);
  }, [loadSuppliers, loadDetail, detailSupplier]);

  const approveSupplier = async (id: string) => {
    setBusyId(id);
    try {
      await apiRequest(`/admin/suppliers/${encodeURIComponent(id)}/approve`, { method: 'POST', auth: true, body: JSON.stringify({}) });
      success('تأمین‌کننده تأیید شد.');
      await refreshAfterAction();
    } catch (err: any) {
      error(err?.message || 'تأیید تأمین‌کننده ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const rejectSupplierConfirm = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!rejectSupplier) return;
    setBusyId(rejectSupplier.id);
    try {
      await apiRequest(`/admin/suppliers/${encodeURIComponent(rejectSupplier.id)}/reject`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({ reason: rejectReason }),
      });
      success('تأمین‌کننده رد شد.');
      setRejectSupplier(null);
      setRejectReason('');
      await refreshAfterAction();
    } catch (err: any) {
      error(err?.message || 'رد تأمین‌کننده ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const suspendSupplierConfirm = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!suspendSupplier) return;
    setBusyId(suspendSupplier.id);
    try {
      await apiRequest(`/admin/suppliers/${encodeURIComponent(suspendSupplier.id)}/suspend`, {
        method: 'POST',
        auth: true,
        body: JSON.stringify({ reason: suspendReason }),
      });
      success('تأمین‌کننده تعلیق شد.');
      setSuspendSupplier(null);
      setSuspendReason('');
      await refreshAfterAction();
    } catch (err: any) {
      error(err?.message || 'تعلیق تأمین‌کننده ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const reactivateSupplier = async (id: string) => {
    setBusyId(id);
    try {
      await apiRequest(`/admin/suppliers/${encodeURIComponent(id)}/reactivate`, { method: 'POST', auth: true, body: JSON.stringify({}) });
      success('تأمین‌کننده دوباره فعال شد.');
      await refreshAfterAction();
    } catch (err: any) {
      error(err?.message || 'فعال‌سازی مجدد تأمین‌کننده ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const toggleDirectShip = async (id: string) => {
    const nextEnabled = !directShip[id];
    setBusyId(id);
    try {
      await apiRequest('/admin/suppliers/shipping-methods', {
        method: 'POST',
        auth: true,
        body: JSON.stringify({ supplierId: id, method: 'direct', status: nextEnabled ? 'enabled' : 'disabled' }),
      });
      setDirectShip((prev) => ({ ...prev, [id]: nextEnabled }));
      success('وضعیت مجوز ارسال مستقیم تأمین‌کننده به‌روزرسانی شد.');
      if (detailSupplier) await loadDetail(detailSupplier.id);
    } catch (err: any) {
      error(err?.message || 'به‌روزرسانی مجوز ارسال مستقیم ناموفق بود.');
    } finally {
      setBusyId(null);
    }
  };

  const pendingCount = suppliersList.filter((s) => s.approvalStatus === 'pending_approval').length;

  const visibleSuppliers = statusFilter === 'all'
    ? suppliersList
    : suppliersList.filter((s) => s.approvalStatus === statusFilter);

  return (
    <div className="space-y-3.5 pb-6">
      <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-bold text-slate-800">
            تأمین‌کنندگان رسمی ({toPersianDigits(visibleSuppliers.length)} برند همکار)
          </h3>
          <button
            onClick={loadSuppliers}
            className="px-2.5 py-1 text-[10px] font-bold rounded-xl bg-slate-100 hover:bg-slate-200 text-slate-700"
          >
            به‌روزرسانی فهرست
          </button>
        </div>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          پیش‌فرض پلتفرم ارسال از انبار مرکزی است. مجوز ارسال مستقیم فقط با تأیید مدیر برای تأمین‌کنندگان واجد شرایط فعال می‌شود (بخش ۱ و ۹ پروپوزال).
        </p>
        <p className="text-[10px] text-slate-500 leading-relaxed">
          تفاوت «مسدودکردن کاربر» و «تعلیق تأمین‌کننده»: مسدودکردن در سطح شخص است — فقط ورود همان کاربر بسته می‌شود و کالاهای تأمین‌کننده همچنان فروخته می‌شوند. تعلیق در سطح کسب‌وکار است — پنل تأمین‌کننده و تیمش بسته می‌شود و کالاهایش از فروش می‌ایستند تا فعال‌سازی مجدد.
        </p>
        <div className="flex gap-1.5 pt-1">
          {([
            ['all', 'همه'],
            ['pending_approval', 'در انتظار تأیید'],
            ['approved', 'تأییدشده'],
            ['rejected', 'ردشده'],
            ['suspended', 'تعلیق‌شده'],
          ] as [StatusFilter, string][]).map(([value, label]) => (
            <button
              key={value}
              onClick={() => setStatusFilter(value)}
              className={`px-2.5 py-1 text-[10px] font-bold rounded-lg transition-colors ${
                statusFilter === value ? 'bg-blue-600 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200'
              }`}
            >
              {label}{value === 'pending_approval' && pendingCount > 0 ? ` (${toPersianDigits(pendingCount)})` : ''}
            </button>
          ))}
        </div>
      </div>

      {loading && (
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 text-[11px] text-slate-500">
          در حال دریافت فهرست تأمین‌کنندگان…
        </div>
      )}

      {!loading && loadError && (
        <div className="bg-white rounded-2xl p-3.5 border border-rose-200 text-[11px] text-rose-700">
          {loadError}
        </div>
      )}

      {!loading && !loadError && visibleSuppliers.length === 0 && (
        <div className="bg-white rounded-2xl p-3.5 border border-slate-200/90 text-[11px] text-slate-500">
          تأمین‌کننده‌ای با این وضعیت یافت نشد.
        </div>
      )}

      <div className="space-y-2.5">
        {visibleSuppliers.map((supplier) => (
          <AdminSupplierCardView
            key={supplier.id}
            supplier={supplier}
            hasDirectShip={!!directShip[supplier.id]}
            busyId={busyId}
            toPersianDigits={toPersianDigits}
            onOpenDetail={openDetail}
            onContract={setSelectedContractSupplier}
            onApprove={approveSupplier}
            onReject={setRejectSupplier}
            onSuspend={setSuspendSupplier}
            onReactivate={reactivateSupplier}
            onToggleDirectShip={toggleDirectShip}
          />
        ))}
      </div>

      {detailSupplier && (
        <AdminSupplierDetailView
          supplier={detail?.supplier ?? detailSupplier}
          detail={detail}
          detailLoading={detailLoading}
          detailError={detailError}
          hasDirectShip={!!directShip[detailSupplier.id]}
          busyId={busyId}
          toPersianDigits={toPersianDigits}
          onClose={closeDetail}
          onRetryDetail={() => void loadDetail(detailSupplier.id)}
          onContract={setSelectedContractSupplier}
          onApprove={approveSupplier}
          onReject={setRejectSupplier}
          onSuspend={setSuspendSupplier}
          onReactivate={reactivateSupplier}
          onToggleDirectShip={toggleDirectShip}
        />
      )}

      {rejectSupplier && (
        <div className="fixed inset-0 z-50 bg-slate-900/60 backdrop-blur-xs flex items-center justify-center p-4">
          <form onSubmit={rejectSupplierConfirm} className="bg-white rounded-2xl max-w-sm w-full p-4 border border-slate-200 shadow-2xl space-y-3 text-xs">
            <h4 className="font-extrabold text-slate-900 text-sm">رد تأمین‌کننده: {rejectSupplier.name}</h4>
            <div>
              <label className="block text-[11px] font-bold text-slate-700 mb-1">دلیل رد (اختیاری)</label>
              <input
                value={rejectReason}
                onChange={(e) => setRejectReason(e.target.value)}
                placeholder="دلیل رد درخواست"
                className="w-full p-2.5 rounded-xl bg-slate-50 border border-slate-200"
              />
            </div>
            <div className="flex gap-2 pt-1">
              <button type="submit" disabled={busyId === rejectSupplier.id} className="flex-1 py-2 bg-rose-600 text-white font-bold rounded-xl disabled:opacity-50">
                تأیید رد
              </button>
              <button type="button" onClick={() => { setRejectSupplier(null); setRejectReason(''); }} className="px-3 py-2 bg-slate-100 text-slate-600 font-bold rounded-xl">
                انصراف
              </button>
            </div>
          </form>
        </div>
      )}

      {suspendSupplier && (
        <div className="fixed inset-0 z-50 bg-slate-900/60 backdrop-blur-xs flex items-center justify-center p-4">
          <form onSubmit={suspendSupplierConfirm} className="bg-white rounded-2xl max-w-sm w-full p-4 border border-slate-200 shadow-2xl space-y-3 text-xs">
            <h4 className="font-extrabold text-slate-900 text-sm">تعلیق تأمین‌کننده: {suspendSupplier.name}</h4>
            <p className="text-[11px] text-slate-600 leading-relaxed">
              با تعلیق، پنل تأمین‌کننده و تیمش بسته می‌شود، کالاهایش از فروش می‌ایستند، سفارش‌های مستقیمِ در جریان متوقف می‌مانند و درخواست تسویهٔ جدید ممکن نیست؛ سفارش‌های انباری طبق روال ادامه پیدا می‌کنند. موجودی و تسویه‌های در انتظار بررسی دست‌نخورده می‌مانند.
            </p>
            <div>
              <label className="block text-[11px] font-bold text-slate-700 mb-1">دلیل تعلیق (اختیاری)</label>
              <input
                value={suspendReason}
                onChange={(e) => setSuspendReason(e.target.value)}
                placeholder="دلیل تعلیق کسب‌وکار"
                className="w-full p-2.5 rounded-xl bg-slate-50 border border-slate-200"
              />
            </div>
            <div className="flex gap-2 pt-1">
              <button type="submit" disabled={busyId === suspendSupplier.id} className="flex-1 py-2 bg-amber-600 text-white font-bold rounded-xl disabled:opacity-50">
                تأیید تعلیق
              </button>
              <button type="button" onClick={() => { setSuspendSupplier(null); setSuspendReason(''); }} className="px-3 py-2 bg-slate-100 text-slate-600 font-bold rounded-xl">
                انصراف
              </button>
            </div>
          </form>
        </div>
      )}

      {selectedContractSupplier && (
        <div className="fixed inset-0 z-50 bg-slate-900/60 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl max-w-md w-full p-4 border border-slate-200 shadow-2xl space-y-3 text-xs">
            <div className="flex items-center justify-between border-b border-slate-100 pb-2">
              <h4 className="font-extrabold text-slate-900">خلاصه قرارداد تجاری: {selectedContractSupplier.name}</h4>
              <button onClick={() => setSelectedContractSupplier(null)} className="px-2 py-1 bg-slate-100 rounded-lg font-bold">
                بستن
              </button>
            </div>
            <div className="space-y-1.5 text-slate-600">
              <div className="flex justify-between"><span>مدل همکاری:</span><strong className="text-slate-900">امانی در انبار مرکزی (Consignment)</strong></div>
              <div className="flex justify-between"><span>دوره تسویه حساب:</span><strong className="text-emerald-700">پس از انقضای ۷ روز مهلت تست خریدار</strong></div>
              <div className="flex justify-between"><span>وضعیت تأیید:</span><strong className="text-slate-900">{statusLabel(selectedContractSupplier.approvalStatus)}</strong></div>
              <div className="flex justify-between"><span>نسخه قرارداد امضاشده:</span><strong className="text-slate-900">{selectedContractSupplier.signedContractVersion != null ? toPersianDigits(selectedContractSupplier.signedContractVersion) : '—'}</strong></div>
              <div className="flex justify-between"><span>اتصال تاپین:</span><strong className="text-slate-900">{selectedContractSupplier.tapinConnected ? 'متصل' : 'متصل نیست'}</strong></div>
              <div className="flex justify-between"><span>امتیاز SLA تحویل:</span><strong className="text-blue-600">٪{toPersianDigits(selectedContractSupplier.deliveryScore)}</strong></div>
              {selectedContractSupplier.rejectionReason && (
                <p className="p-2.5 bg-rose-50 rounded-xl border border-rose-100 text-[11px] leading-relaxed">
                  دلیل رد: {selectedContractSupplier.rejectionReason}
                </p>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
