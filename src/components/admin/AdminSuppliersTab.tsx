import React, { useCallback, useEffect, useId, useRef, useState } from 'react';
import { ArrowLeft, Building2, ChevronDown, FileText, RefreshCw, Search, Star } from 'lucide-react';
import { useStore } from '../../context/StoreContext';
import { apiRequest } from '../../services/apiClient';
import { formatPrice } from '../../utils/formatters';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';

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

const statusFilters: StatusFilter[] = ['all', 'pending_approval', 'approved', 'rejected', 'suspended'];

const buttonClass = 'inline-flex min-h-11 items-center gap-2 rounded-xl px-3 py-2 text-sm font-bold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand disabled:opacity-50';
const plainButton = buttonClass + ' justify-center border border-slate-200 bg-white hover:bg-brand-bg';
const secondaryButton = plainButton + ' text-slate-700';
const primaryButton = buttonClass + ' justify-center bg-brand-deep text-white hover:bg-brand';
const dangerButton = buttonClass + ' justify-center bg-rose-700 text-white hover:bg-rose-800';
const inputClass = 'min-h-11 min-w-0 rounded-xl border border-slate-300 bg-white px-3 py-2 text-base text-slate-900 focus-visible:outline-2 focus-visible:outline-brand';
const surfaceClass = 'rounded-2xl border border-slate-200 bg-white';

const statusTones = new Map([
  ['pending_approval', 'bg-amber-100 text-amber-900'],
  ['approved', 'bg-emerald-50 text-emerald-800'],
  ['rejected', 'bg-rose-50 text-rose-800'],
]);

function SupplierStatus({ status }: { status?: string }) {
  const tone = statusTones.get(status ?? '') ?? 'bg-slate-100 text-slate-700';
  return <span className={tone + ' inline-flex w-fit rounded-lg px-2 py-1 text-xs font-bold'}>{statusLabel(status)}</span>;
}

export interface AdminSupplierCardViewProps {
  supplier: AdminSupplierRow;
  toPersianDigits: (v: string | number) => string;
  onOpenDetail: (supplier: AdminSupplierRow) => void;
}

export function AdminSupplierCardView({ supplier, toPersianDigits, onOpenDetail }: AdminSupplierCardViewProps) {
  return (
    <article className={surfaceClass + ' flex min-w-0 flex-col justify-between gap-4 p-4'}>
      <div className="flex min-w-0 items-start gap-3">
        <div className="grid size-11 shrink-0 place-items-center overflow-hidden rounded-xl bg-brand-soft text-brand-deep">
          {supplier.logoUrl ? <img src={supplier.logoUrl} alt="" className="size-full object-contain" /> : <Building2 aria-hidden="true" className="size-5" />}
        </div>
        <div className="min-w-0 flex-1">
          <h3 className="break-words text-base font-extrabold leading-6 text-slate-900">{supplier.name}</h3>
          {supplier.warehouseCity && <p className="break-words text-sm text-slate-600">{supplier.warehouseCity}</p>}
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <SupplierStatus status={supplier.approvalStatus} />
            {supplier.badge && <span className="break-words rounded-lg bg-brand-bg px-2 py-1 text-xs font-bold text-brand-deep">{supplier.badge}</span>}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-600">
        <span>تعداد کالا در کاتالوگ <strong className="text-slate-900">{toPersianDigits(supplier.totalProducts)}</strong></span>
        <span className="inline-flex items-center gap-1"><Star aria-hidden="true" className="size-3.5 text-amber-700" /> امتیاز <strong className="text-slate-900">{toPersianDigits(supplier.rating)}</strong></span>
      </div>

      <button
        type="button" aria-haspopup="dialog" aria-label={'مشاهده جزئیات ' + supplier.name}
        onClick={() => onOpenDetail(supplier)}
        className={buttonClass + ' w-full justify-between border border-brand-border bg-brand-bg text-brand-deep hover:bg-brand-soft'}
      >
        {supplier.approvalStatus === 'pending_approval' ? 'بررسی درخواست' : 'مشاهده پرونده'}
        <ArrowLeft aria-hidden="true" className="size-4" />
      </button>
    </article>
  );
}

function Facts({ entries }: { entries: ReadonlyArray<readonly [string, React.ReactNode]> }) {
  return (
    <dl className="divide-y divide-slate-100 text-sm">
      {entries.map(([label, value]) => (
        <div key={label} className="grid grid-cols-2 gap-2 py-2">
          <dt className="min-w-0 break-words text-slate-600">{label}</dt>
          <dd className="min-w-0 break-words text-end font-semibold text-slate-900">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

function DetailSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <details className="group rounded-xl border border-slate-200">
      <summary className="flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 rounded-xl px-3 py-2 text-sm font-bold hover:bg-brand-bg focus-visible:outline-2 focus-visible:outline-brand [&::-webkit-details-marker]:hidden">
        {title}<ChevronDown aria-hidden="true" className="size-4 shrink-0 text-slate-600 group-open:rotate-180" />
      </summary>
      <div className="space-y-2 border-t border-slate-100 px-3 py-2 text-sm text-slate-700">{children}</div>
    </details>
  );
}

function ErrorMessage({ message }: { message: string }) {
  return <p role="alert" className="break-words rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm leading-6 text-rose-800">{message}</p>;
}

export interface AdminSupplierDetailViewProps {
  supplier: AdminSupplierRow;
  detail: SupplierDetail | null;
  detailLoading: boolean;
  detailError: string | null;
  hasDirectShip: boolean;
  busyId: string | null;
  actionError?: string | null;
  actionMessage?: string | null;
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

export function AdminSupplierDetailView({
  supplier, detail, detailLoading, detailError, hasDirectShip, busyId, actionError, actionMessage,
  toPersianDigits, onClose, onRetryDetail, onContract, onApprove, onReject, onSuspend, onReactivate, onToggleDirectShip,
}: AdminSupplierDetailViewProps) {
  const id = useId();
  const busy = busyId === supplier.id;
  const owner = detail?.supplier.owner;
  const counts = detail?.counts;
  const directShipCount = Object.values(counts?.orders.directShipmentsByStatus ?? {}).reduce((a, b) => a + b, 0);
  const markup = formatMarkup(supplier.defaultMarkupType, supplier.defaultMarkupValue, toPersianDigits);
  const status = supplier.approvalStatus;
  const decisionReason = status === 'rejected' ? supplier.rejectionReason : status === 'suspended' ? supplier.suspensionReason : null;

  return (
    <SupplierDialog titleId={id} onDismiss={onClose} wide>
      <SupplierDialogHeading id={id} title={supplier.name} detail={supplier.warehouseCity} onClose={onClose} busy={busy} />
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <SupplierStatus status={status} />
          {supplier.createdAt && <p className="text-sm text-slate-600">ثبت‌شده در <bdi>{datePart(supplier.createdAt, toPersianDigits)}</bdi></p>}
        </div>
        {decisionReason && <p className="break-words rounded-xl bg-slate-100 p-3 text-sm text-slate-700">{status === 'rejected' ? 'دلیل رد' : 'دلیل تعلیق'} — {decisionReason}</p>}
        {supplier.description && <p className="break-words text-sm leading-6 text-slate-700">{supplier.description}</p>}
        {actionMessage && <p role="status" className="rounded-xl bg-emerald-50 p-3 text-sm font-medium text-emerald-800">{actionMessage}</p>}
        {actionError && <ErrorMessage message={actionError} />}
        {detailLoading && !detail && <p role="status" className="text-sm text-slate-600">در حال دریافت جزئیات تأمین‌کننده…</p>}
        {detailError && (
          <div className="space-y-2">
            <ErrorMessage message={detailError} />
            <button type="button" onClick={onRetryDetail} className={secondaryButton}>تلاش دوباره</button>
          </div>
        )}

        {detail && !detailError && (
          <>
            <section className="rounded-xl border border-slate-200 px-3 pt-3">
              <h3 className="text-sm font-extrabold text-slate-900">مالک و تماس</h3>
              {owner ? (
                <Facts entries={[
                  ['نام', owner.fullName],
                  ['موبایل', <bdi dir="ltr">{owner.mobile || '—'}</bdi>],
                  ['ایمیل', <bdi dir="ltr">{owner.email || '—'}</bdi>],
                  ['حساب کاربری', owner.isActive ? 'فعال' : 'غیرفعال'],
                ]} />
              ) : <p className="py-2 text-sm text-slate-600">مالکی برای این رکورد ثبت نشده است (رکورد مرکزی پلتفرم).</p>}
            </section>

            <section className="space-y-3 rounded-xl border border-brand-border bg-brand-bg p-3">
              <h3 className="text-sm font-extrabold text-brand-deep">عملیات مدیریتی</h3>
              {status === 'pending_approval' && (
                <div className="grid grid-cols-1 gap-2 min-[360px]:grid-cols-2">
                  <button type="button" disabled={busy} onClick={() => onApprove(supplier.id)} className={primaryButton}>تأیید تأمین‌کننده</button>
                  <button type="button" aria-haspopup="dialog" disabled={busy} onClick={() => onReject(supplier)} className={plainButton + ' text-rose-800'}>رد درخواست</button>
                </div>
              )}
              {status === 'approved' && (
                <button type="button" aria-haspopup="dialog" disabled={busy} onClick={() => onSuspend(supplier)} className={plainButton + ' w-full text-amber-900 sm:w-auto'}>تعلیق تأمین‌کننده</button>
              )}
              {status === 'suspended' && (
                <button type="button" disabled={busy} onClick={() => onReactivate(supplier.id)} className={primaryButton + ' w-full sm:w-auto'}>فعال‌سازی مجدد</button>
              )}
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                <button type="button" aria-haspopup="dialog" disabled={busy} onClick={() => onContract(supplier)} className={secondaryButton}>
                  <FileText aria-hidden="true" className="size-4" /> خلاصه قرارداد
                </button>
                <button type="button" disabled={busy} onClick={() => onToggleDirectShip(supplier.id)} className={secondaryButton}>
                  {hasDirectShip ? 'لغو مجوز ارسال مستقیم' : 'فعال‌کردن ارسال مستقیم'}
                </button>
              </div>
              <p className="text-xs leading-5 text-slate-700">مجوز ارسال مستقیم {hasDirectShip ? 'فعال است.' : 'غیرفعال است و ارسال از انبار انجام می‌شود.'}</p>
            </section>

            <div className="space-y-2">
              <DetailSection title="کالا و سفارش">
                <Facts entries={[
                  ['کالا (محاسبه‌شده)', toPersianDigits(counts?.products ?? 0)],
                  ['کالای فعال', toPersianDigits(counts?.activeProducts ?? 0)],
                  ['تنوع کالا', toPersianDigits(counts?.variants ?? 0)],
                  ['پیشنهاد در انتظار', toPersianDigits(counts?.proposals.pending ?? 0)],
                  ['سفارش پرداخت‌شده', toPersianDigits(counts?.orders.paid ?? 0)],
                  ['ارسال مستقیم', toPersianDigits(directShipCount)],
                  ['امتیاز تحویل به‌موقع', '٪' + toPersianDigits(supplier.deliveryScore)],
                ]} />
                <p className="text-xs leading-5 text-slate-600">امتیاز، امتیاز تحویل و تعداد کالا در فهرست، مقادیر ثبت‌شده در پرونده‌اند. شمارش‌های این بخش از داده‌های عملیاتی محاسبه شده‌اند.</p>
              </DetailSection>

              <DetailSection title="انبارها و تیم">
                <Facts entries={[['شهر انبار تأییدشده', supplier.warehouseCityConfirmed ? 'بله' : 'خیر']]} />
                {detail.warehouses.length ? (
                  <ul className="space-y-2">
                    {detail.warehouses.map(w => (
                      <li key={w.id} className="break-words border-b border-slate-100 pb-2">
                        <strong>{w.name}</strong><p>{w.cityName}، {w.provinceName}{w.isActive ? '' : ' (غیرفعال)'}</p>
                      </li>
                    ))}
                  </ul>
                ) : <p>انباری ثبت نشده است.</p>}
                {detail.members.length ? (
                  <ul className="space-y-2">
                    {detail.members.map(m => (
                      <li key={m.id} className="break-words border-b border-slate-100 pb-2">
                        <strong>{m.fullName}</strong> <bdi>({m.roleKey})</bdi>
                        <p><bdi dir="ltr">{m.mobile}</bdi>{m.isActive ? '' : ' · غیرفعال'}</p>
                      </li>
                    ))}
                  </ul>
                ) : <p>عضو دیگری ثبت نشده است.</p>}
              </DetailSection>

              <DetailSection title="تاپین و ارسال">
                <Facts entries={[
                  ['فروشگاه تاپین', <bdi dir="ltr">{detail.tapin.shopId || '—'}</bdi>],
                  ['اتصال تاپین', detail.tapin.connected ? 'متصل' : 'متصل نیست'],
                  ['ارسال مستقیم', hasDirectShip ? 'فعال (با مجوز)' : 'غیرفعال (ارسال انبار)'],
                  ...detail.shippingMethods.map(m => ['روش ' + m.method, m.status === 'enabled' ? 'فعال' : 'غیرفعال'] as const),
                ]} />
                {detail.shippingMethods.length === 0 && <p>روش ارسالی ثبت نشده است.</p>}
              </DetailSection>

              <DetailSection title="مالی (تومان)">
                {detail.finance.balance ? (
                  <Facts entries={[
                    ['درآمد', formatPrice(detail.finance.balance.earned) + ' تومان'],
                    ['رزرو شده', formatPrice(detail.finance.balance.reserved) + ' تومان'],
                    ['پرداخت‌شده', formatPrice(detail.finance.balance.paid) + ' تومان'],
                    ['قابل برداشت', formatPrice(detail.finance.balance.available) + ' تومان'],
                  ]} />
                ) : <p>موجودی در دسترس نیست.</p>}
                {detail.finance.settlements.length ? (
                  <ul className="divide-y divide-slate-100">
                    {detail.finance.settlements.map(s => (
                      <li key={s.id} className="break-words py-2">
                        <bdi>{s.status}</bdi> · <bdi>{datePart(s.created_at, toPersianDigits)}</bdi> · <strong>{formatPrice(s.amount)} تومان</strong>
                      </li>
                    ))}
                  </ul>
                ) : <p>تسویه‌ای ثبت نشده است.</p>}
              </DetailSection>

              <DetailSection title="قرارداد و سابقه بررسی">
                <Facts entries={[
                  ['نسخهٔ امضاشده', supplier.signedContractVersion != null ? toPersianDigits(supplier.signedContractVersion) : '—'],
                  ['نسخهٔ جاری پلتفرم', supplier.platformContractVersion != null ? toPersianDigits(supplier.platformContractVersion) : 'نامشخص'],
                  ...(markup ? [['سود پیش‌فرض توافق', markup] as const] : []),
                  ...(supplier.approvedBy ? [['تأییدکننده', <bdi>{supplier.approvedBy}</bdi>] as const] : []),
                  ...(supplier.approvedAt ? [['تاریخ تأیید', <bdi>{datePart(supplier.approvedAt, toPersianDigits)}</bdi>] as const] : []),
                ]} />
                {detail.history.length ? (
                  <ul className="divide-y divide-slate-100">
                    {detail.history.map(row => {
                      const reason = auditHistoryReason(row.details);
                      return (
                        <li key={row.id} className="break-words py-2">
                          <bdi>{row.action}</bdi> · <bdi>{row.operator}</bdi> · <bdi>{datePart(row.timestamp, toPersianDigits)}</bdi>
                          {reason && <p>دلیل — {reason}</p>}
                        </li>
                      );
                    })}
                  </ul>
                ) : <p>رویداد مدیریتی ثبت‌شده‌ای برای این تأمین‌کننده وجود ندارد.</p>}
                {detail.gaps.filter(gap => gap.includes('تأیید/رد')).map(gap => (
                  <p key={gap} className="rounded-xl bg-amber-50 p-3 text-xs leading-5 text-amber-900">{gap}</p>
                ))}
              </DetailSection>
            </div>
          </>
        )}
      </div>
    </SupplierDialog>
  );
}

type ReasonAction = 'reject' | 'suspend';

const reviewActions = {
  approve: { status: 'approved', success: 'تأمین‌کننده تأیید شد.', error: 'تأیید تأمین‌کننده ناموفق بود.' },
  reject: { status: 'rejected', success: 'درخواست تأمین‌کننده رد شد.', error: 'رد تأمین‌کننده ناموفق بود.' },
  suspend: { status: 'suspended', success: 'تأمین‌کننده تعلیق شد.', error: 'تعلیق تأمین‌کننده ناموفق بود.' },
  reactivate: { status: 'approved', success: 'تأمین‌کننده دوباره فعال شد.', error: 'فعال‌سازی مجدد تأمین‌کننده ناموفق بود.' },
} as const;

function SupplierReasonDialog({ action, supplier, busy, error, onConfirm, onClose }: {
  action: ReasonAction;
  supplier: AdminSupplierRow;
  busy: boolean;
  error: string | null;
  onConfirm: (reason: string) => void;
  onClose: () => void;
}) {
  const id = useId();
  const [reason, setReason] = useState('');
  const suspend = action === 'suspend';
  return (
    <SupplierDialog titleId={id} descriptionId={suspend ? id + '-description' : undefined} onDismiss={onClose}>
      <SupplierDialogHeading id={id} title={suspend ? 'تعلیق تأمین‌کننده' : 'رد درخواست تأمین‌کننده'} detail={supplier.name} onClose={onClose} busy={busy} />
      <form className="space-y-3" onSubmit={event => { event.preventDefault(); onConfirm(reason); }}>
        {suspend && <p id={id + '-description'} className="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-900">تعلیق، دسترسی تأمین‌کننده و تیمش به پنل را می‌بندد. فروش کالاها، سفارش‌های مستقیمِ در جریان و درخواست تسویهٔ جدید متوقف می‌شوند. سفارش‌های انباری طبق روال ادامه دارند.</p>}
        <label className="block text-sm font-bold text-slate-800" htmlFor={id + '-reason'}>{suspend ? 'دلیل تعلیق (اختیاری)' : 'دلیل رد (اختیاری)'}</label>
        <textarea id={id + '-reason'} value={reason} disabled={busy} onChange={event => setReason(event.target.value)} rows={3} className={inputClass + ' block w-full resize-y'} />
        {error && <ErrorMessage message={error} />}
        <div className="grid grid-cols-2 gap-2">
          <button type="button" onClick={onClose} disabled={busy} className={secondaryButton}>انصراف</button>
          <button type="submit" disabled={busy} className={dangerButton}>{busy ? 'در حال ثبت…' : suspend ? 'تأیید تعلیق' : 'تأیید رد'}</button>
        </div>
      </form>
    </SupplierDialog>
  );
}

function SupplierContractDialog({ supplier, toPersianDigits, onClose }: {
  supplier: AdminSupplierRow;
  toPersianDigits: AdminSupplierCardViewProps['toPersianDigits'];
  onClose: () => void;
}) {
  const id = useId();
  return (
    <SupplierDialog titleId={id} onDismiss={onClose}>
      <SupplierDialogHeading id={id} title="خلاصه قرارداد تجاری" detail={supplier.name} onClose={onClose} />
      <Facts entries={[
        ['مدل همکاری', 'امانی در انبار مرکزی (Consignment)'],
        ['دوره تسویه حساب', 'پس از انقضای ۷ روز مهلت تست خریدار'],
        ['وضعیت تأیید', statusLabel(supplier.approvalStatus)],
        ['نسخه قرارداد امضاشده', supplier.signedContractVersion != null ? toPersianDigits(supplier.signedContractVersion) : '—'],
        ['اتصال تاپین', supplier.tapinConnected ? 'متصل' : 'متصل نیست'],
        ['امتیاز SLA تحویل', '٪' + toPersianDigits(supplier.deliveryScore)],
      ]} />
      {supplier.rejectionReason && <p className="mt-3 break-words rounded-xl bg-rose-50 p-3 text-sm text-rose-800">دلیل رد — {supplier.rejectionReason}</p>}
    </SupplierDialog>
  );
}

interface SupplierPage {
  suppliers: AdminSupplierRow[];
  hasMore: boolean;
  nextCursor: string;
}

interface SupplierListState {
  filter: StatusFilter;
  items: AdminSupplierRow[];
  nextCursor: string;
  pending: 'first' | 'more' | null;
  error: string | null;
}

function normalizeSearch(value: string) {
  return value.normalize('NFKD').toLowerCase().replace(/\p{M}/gu, '').replace(/[يى]/g, 'ی')
    .replace(/ك/g, 'ک').replace(/[\s\u200c\u200d]+/g, ' ').trim();
}

const matchesStatus = (supplier: AdminSupplierRow, filter: StatusFilter) => filter === 'all' || supplier.approvalStatus === filter;
const messageFrom = (cause: unknown, fallback: string) => cause instanceof Error && cause.message ? cause.message : fallback;

export function AdminSuppliersTab() {
  const { toPersianDigits } = useStore();
  const listTitle = useId();
  const searchRef = useRef<HTMLInputElement>(null);
  const listRequest = useRef<{ controller: AbortController; filter: StatusFilter; cursor: string } | null>(null);
  const detailRequest = useRef<AbortController | null>(null);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
  const [search, setSearch] = useState('');
  const [list, setList] = useState<SupplierListState>({ filter: 'all', items: [], nextCursor: '', pending: 'first', error: null });
  const [detailSupplier, setDetailSupplier] = useState<AdminSupplierRow | null>(null);
  const [detail, setDetail] = useState<SupplierDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [reasonAction, setReasonAction] = useState<ReasonAction | null>(null);
  const [contractOpen, setContractOpen] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<string | null>(null);

  const loadSuppliers = useCallback(async (filter: StatusFilter, cursor = '') => {
    listRequest.current?.controller.abort();
    const controller = new AbortController();
    const request = { controller, filter, cursor };
    listRequest.current = request;
    setList(current => ({
      filter,
      items: current.filter === filter ? current.items : [],
      nextCursor: cursor,
      pending: cursor ? 'more' : 'first',
      error: null,
    }));
    const params = new URLSearchParams();
    if (filter !== 'all') params.set('status', filter);
    if (cursor) params.set('cursor', cursor);
    const query = params.toString();
    try {
      const result = await apiRequest<SupplierPage | AdminSupplierRow[]>(
        '/admin/suppliers/records' + (query ? '?' + query : ''),
        { auth: true, signal: controller.signal },
      );
      if (controller.signal.aborted) return;
      const page = Array.isArray(result) ? null : result;
      const rows = Array.isArray(result) ? result : page?.suppliers;
      setList(current => {
        const items = new Map((cursor ? current.items : []).map(supplier => [supplier.id, supplier]));
        if (Array.isArray(rows)) {
          rows.filter(supplier => matchesStatus(supplier, filter)).forEach(supplier => items.set(supplier.id, supplier));
        }
        return {
          filter,
          items: [...items.values()],
          nextCursor: page?.hasMore && page.nextCursor && page.nextCursor !== cursor ? page.nextCursor : '',
          pending: null,
          error: null,
        };
      });
    } catch (cause) {
      if (!controller.signal.aborted) {
        setList(current => ({ ...current, pending: null, error: messageFrom(cause, 'خطا در دریافت فهرست تأمین‌کنندگان') }));
      }
    } finally {
      if (listRequest.current === request) listRequest.current = null;
    }
  }, []);

  useEffect(() => {
    void loadSuppliers(statusFilter);
    return () => listRequest.current?.controller.abort();
  }, [loadSuppliers, statusFilter]);

  const loadDetail = useCallback(async (supplierId: string) => {
    detailRequest.current?.abort();
    const controller = new AbortController();
    detailRequest.current = controller;
    setDetailLoading(true);
    setDetailError(null);
    try {
      const result = await apiRequest<SupplierDetail>('/admin/suppliers/' + encodeURIComponent(supplierId), { auth: true, signal: controller.signal });
      if (!controller.signal.aborted) setDetail(result);
    } catch (cause) {
      if (!controller.signal.aborted) setDetailError(messageFrom(cause, 'خطا در دریافت جزئیات تأمین‌کننده'));
    } finally {
      if (!controller.signal.aborted) setDetailLoading(false);
    }
  }, []);

  useEffect(() => () => detailRequest.current?.abort(), []);

  const openDetail = (supplier: AdminSupplierRow) => {
    setDetailSupplier(supplier);
    setDetail(null);
    setActionError(null);
    setActionMessage(null);
    void loadDetail(supplier.id);
  };

  const closeDetail = () => {
    if (busyId) return;
    detailRequest.current?.abort();
    setDetailSupplier(null);
    setDetail(null);
    requestAnimationFrame(() => {
      if (document.activeElement === document.body) searchRef.current?.focus({ preventScroll: true });
    });
  };

  const closeReason = () => {
    if (busyId) return;
    setReasonAction(null);
    setActionError(null);
  };

  const runAction = async (action: {
    supplierId: string;
    endpoint: string;
    body: Record<string, string>;
    patch: Partial<AdminSupplierRow>;
    success: string;
    error: string;
  }) => {
    if (busyId) return;
    const { supplierId, patch } = action;
    setBusyId(supplierId);
    setActionError(null);
    setActionMessage(null);
    try {
      await apiRequest(action.endpoint, { method: 'POST', auth: true, body: JSON.stringify(action.body) });
      // Keep already loaded pages and the current search position after a decision.
      setList(current => ({
        ...current,
        items: current.items
          .map(supplier => supplier.id === supplierId ? { ...supplier, ...patch } : supplier)
          .filter(supplier => matchesStatus(supplier, current.filter)),
      }));
      setDetailSupplier(current => current?.id === supplierId ? { ...current, ...patch } : current);
      setDetail(current => current?.supplier.id === supplierId ? {
        ...current,
        supplier: { ...current.supplier, ...patch },
        shippingMethods: current.shippingMethods.map(method => method.method === 'direct' && patch.supportsDirectShipping !== undefined
          ? { ...method, status: patch.supportsDirectShipping ? 'enabled' : 'disabled' }
          : method),
      } : current);
      setReasonAction(null);
      setActionMessage(action.success);
      const pendingList = listRequest.current;
      if (pendingList && !pendingList.controller.signal.aborted) {
        // A response started before the decision can contain the old status.
        void loadSuppliers(pendingList.filter, pendingList.cursor);
      }
      await loadDetail(supplierId);
    } catch (cause) {
      setActionError(messageFrom(cause, action.error));
    } finally {
      setBusyId(null);
    }
  };

  const review = (supplierId: string, action: keyof typeof reviewActions, reason = '') => {
    const labels = reviewActions[action];
    const patch: Partial<AdminSupplierRow> = { approvalStatus: labels.status };
    if (action === 'reject') patch.rejectionReason = reason.trim();
    if (action === 'suspend') patch.suspensionReason = reason.trim();
    return runAction({
      supplierId,
      endpoint: '/admin/suppliers/' + encodeURIComponent(supplierId) + '/' + action,
      body: action === 'reject' || action === 'suspend' ? { reason } : {},
      patch,
      success: labels.success,
      error: labels.error,
    });
  };

  const selectedSupplier = detail?.supplier ?? detailSupplier;
  const directMethod = detail?.shippingMethods.find(method => method.method === 'direct');
  const hasDirectShip = directMethod ? directMethod.status === 'enabled' : detailSupplier?.supportsDirectShipping ?? false;
  const toggleDirectShip = (supplierId: string) => runAction({
    supplierId,
    endpoint: '/admin/suppliers/shipping-methods',
    body: { supplierId, method: 'direct', status: hasDirectShip ? 'disabled' : 'enabled' },
    patch: { supportsDirectShipping: !hasDirectShip },
    success: 'وضعیت مجوز ارسال مستقیم به‌روزرسانی شد.',
    error: 'به‌روزرسانی مجوز ارسال مستقیم ناموفق بود.',
  });

  const searchTerm = normalizeSearch(search);
  const visibleSuppliers = list.filter === statusFilter
    ? list.items.filter(supplier => !searchTerm || [supplier.name, supplier.warehouseCity, supplier.id].some(value => normalizeSearch(value || '').includes(searchTerm)))
    : [];
  const pending = list.filter === statusFilter ? list.pending : 'first';
  const canLoadMore = list.filter === statusFilter && Boolean(list.nextCursor);

  return (
    <section aria-labelledby={listTitle} dir="rtl" className="space-y-4 pb-6">
      <div className={surfaceClass + ' space-y-4 p-4 sm:p-5'}>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 id={listTitle} className="text-base font-extrabold text-slate-900">فهرست تأمین‌کنندگان</h2>
          <button type="button" onClick={() => void loadSuppliers(statusFilter)} disabled={pending !== null} className={secondaryButton}>
            <RefreshCw aria-hidden="true" className="size-4" /> به‌روزرسانی
          </button>
        </div>

        <label className="block space-y-1">
          <span className="block text-sm font-bold text-slate-800">جست‌وجوی تأمین‌کننده</span>
          <span className="relative block">
            <Search aria-hidden="true" className="pointer-events-none absolute start-3 top-3.5 size-4 text-slate-500" />
            <input ref={searchRef} type="search" value={search} onChange={event => setSearch(event.target.value)} placeholder="نام، شهر انبار یا شناسه" className={inputClass + ' w-full ps-9'} />
          </span>
        </label>

        <div role="group" aria-label="وضعیت تأمین‌کننده" className="flex flex-wrap gap-2">
          {statusFilters.map(value => (
            <button
              key={value} type="button" onClick={() => setStatusFilter(value)} aria-pressed={statusFilter === value}
              className={buttonClass + (statusFilter === value ? ' bg-brand-deep text-white hover:bg-brand' : ' bg-slate-100 text-slate-700 hover:bg-brand-bg')}
            >
              {value === 'all' ? 'همه' : statusLabel(value)}
            </button>
          ))}
        </div>
      </div>

      {pending === 'first' && <p role="status" className={surfaceClass + ' p-4 text-sm text-slate-700'}>در حال دریافت فهرست تأمین‌کنندگان…</p>}
      {list.filter === statusFilter && list.error && (
        <div className="space-y-2">
          <ErrorMessage message={list.error} />
          {!canLoadMore && <button type="button" onClick={() => void loadSuppliers(statusFilter)} className={secondaryButton}>تلاش دوباره</button>}
        </div>
      )}
      {pending === null && !list.error && visibleSuppliers.length === 0 && (
        <p role="status" className={surfaceClass + ' p-4 text-sm leading-6 text-slate-700'}>
          {searchTerm
            ? canLoadMore ? 'در موارد بارگیری‌شده نتیجه‌ای یافت نشد. برای ادامهٔ جست‌وجو موارد بیشتری نمایش دهید.' : 'تأمین‌کننده‌ای مطابق جست‌وجو یافت نشد.'
            : statusFilter === 'all' ? 'تأمین‌کننده‌ای ثبت نشده است.' : 'تأمین‌کننده‌ای با این وضعیت یافت نشد.'}
        </p>
      )}

      {visibleSuppliers.length > 0 && (
        <>
          <p role="status" className="text-sm text-slate-600">{toPersianDigits(visibleSuppliers.length)} تأمین‌کننده نمایش داده شده</p>
          <div className="grid gap-3 sm:grid-cols-2">
            {visibleSuppliers.map(supplier => (
              <AdminSupplierCardView key={supplier.id} supplier={supplier} toPersianDigits={toPersianDigits} onOpenDetail={openDetail} />
            ))}
          </div>
        </>
      )}

      {canLoadMore && (
        <button type="button" onClick={() => void loadSuppliers(statusFilter, list.nextCursor)} disabled={pending !== null} className={secondaryButton + ' w-full sm:w-auto'}>
          {pending === 'more' ? 'در حال دریافت…' : 'نمایش تأمین‌کنندگان بیشتر'}
        </button>
      )}

      {selectedSupplier && (
        <AdminSupplierDetailView
          supplier={selectedSupplier} detail={detail} detailLoading={detailLoading} detailError={detailError}
          hasDirectShip={hasDirectShip} busyId={busyId} actionError={reasonAction ? null : actionError} actionMessage={actionMessage}
          toPersianDigits={toPersianDigits} onClose={closeDetail} onRetryDetail={() => void loadDetail(selectedSupplier.id)}
          onContract={() => setContractOpen(true)} onApprove={id => void review(id, 'approve')}
          onReject={() => { setActionError(null); setReasonAction('reject'); }}
          onSuspend={() => { setActionError(null); setReasonAction('suspend'); }}
          onReactivate={id => void review(id, 'reactivate')} onToggleDirectShip={id => void toggleDirectShip(id)}
        />
      )}

      {selectedSupplier && reasonAction && (
        <SupplierReasonDialog
          action={reasonAction} supplier={selectedSupplier} busy={busyId !== null} error={actionError}
          onConfirm={reason => void review(selectedSupplier.id, reasonAction, reason)} onClose={closeReason}
        />
      )}

      {selectedSupplier && contractOpen && (
        <SupplierContractDialog supplier={selectedSupplier} toPersianDigits={toPersianDigits} onClose={() => setContractOpen(false)} />
      )}
    </section>
  );
}
