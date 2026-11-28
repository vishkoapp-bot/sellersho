import React, { useState, useEffect } from 'react';
import {
  adminApi,
  HomepageSectionDto,
  AdminBannerDto,
  AdminReviewRow,
  AdminStaffDto,
  CallbackRetryDto,
} from '../../services/adminApi';
import { useStore } from '../../context/StoreContext';
import type { Store } from '../../types';
import { RotateCcw, Store as StoreIcon, Home, Image, FileText, RefreshCw, Star, Users } from 'lucide-react';

// FIX 17: admin tabs added to reach live's 30-item structure.
// support-returns wires to GET /admin/returns (read-only list).
// D1 (Batch 4a): the other eight former stubs are now read-only v1
// lists over the existing admin GET endpoints (plus the new
// GET /admin/callback-retries). No writes in v1 — no edit/approve/
// reject/suspend controls anywhere in this file.

export const AdminPlaceholderTab: React.FC<{ title: string; description?: string }> = ({ title, description }) => (
  <div className="space-y-3">
    <div className="bg-white rounded-2xl border border-slate-200 p-4">
      <h3 className="text-xs font-bold text-slate-800">{title}</h3>
      <p className="text-[11px] text-slate-500 mt-1">
        {description || 'این بخش هنوز پیاده‌سازی نشده است.'}
      </p>
    </div>
  </div>
);

const pick = (item: any, keys: string[]): string => {
  for (const k of keys) {
    const v = item?.[k];
    if (v !== undefined && v !== null && v !== '') return String(v);
  }
  return '—';
};

const formatDate = (value: any): string => {
  if (value === undefined || value === null || value === '') return '—';
  const d = new Date(String(value));
  if (isNaN(d.getTime())) return String(value);
  return d.toLocaleDateString('fa-IR');
};

// Shared read-only list shell: header + loading / error states.
// The list view passed as children owns its own empty state.
const AdminListShell: React.FC<{
  title: string;
  icon: React.ReactNode;
  loading?: boolean;
  loadError?: string | null;
  onRetry?: () => void;
  children: React.ReactNode;
}> = ({ title, icon, loading = false, loadError = null, onRetry, children }) => (
  <div className="space-y-3">
    <div className="bg-white rounded-2xl border border-slate-200 p-4 flex items-center gap-2">
      {icon}
      <h3 className="text-sm font-bold text-slate-800">{title}</h3>
    </div>
    {loading && <p role="status" className="rounded-2xl border border-slate-200 bg-white p-4 text-sm text-slate-600">در حال بارگذاری…</p>}
    {loadError && (
      <div className="rounded-2xl border border-rose-200 bg-rose-50 p-4 text-sm text-rose-900">
        <p role="alert" className="[overflow-wrap:anywhere]">{loadError}</p>
        {onRetry && <button type="button" onClick={onRetry} className="mt-2 min-h-11 rounded-lg border border-rose-300 px-3 font-bold focus-visible:outline-2 focus-visible:outline-rose-700">تلاش دوباره</button>}
      </div>
    )}
    {!loading && !loadError && children}
  </div>
);

function useAdminRead<T>(fetcher: () => Promise<T>, errorText: string) {
  const [data, setData] = useState<T>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let mounted = true;
    void fetcher()
      .then(result => { if (mounted) setData(result); })
      .catch((err: unknown) => { if (mounted) setLoadError(err instanceof Error ? err.message : errorText); })
      .finally(() => { if (mounted) setLoading(false); });
    return () => { mounted = false; };
  }, [attempt, errorText, fetcher]);
  return {
    data,
    loading,
    loadError,
    onRetry: () => {
      setLoadError(null);
      setLoading(true);
      setAttempt(value => value + 1);
    },
  };
}

function useAdminList<T>(fetcher: () => Promise<T[]>, errorText: string) {
  const { data, ...state } = useAdminRead(fetcher, errorText);
  return { items: Array.isArray(data) ? data : [], ...state };
}

const iconCls = 'w-4 h-4 text-slate-500';
const rowCls = 'space-y-1 rounded-2xl border border-slate-200 bg-white p-4 text-sm leading-6 text-slate-600 [overflow-wrap:anywhere]';
const rowHeaderCls = 'flex flex-wrap items-start justify-between gap-x-3 gap-y-1';
const rowTitleCls = 'min-w-0 font-bold text-slate-900';
const badgeCls = 'max-w-full rounded-full bg-slate-100 px-2 py-0.5 text-xs font-bold leading-5 text-slate-700';
const emptyCls = 'rounded-2xl border border-slate-200 bg-white p-4 text-sm text-slate-600';

// --- stores-list: reuses useStore().stores (same client data as
// AdminStoresTab); no new endpoint. Client Store objects carry no
// createdAt, so the column renders '—' unless the API provides one.
export const AdminStoresListView: React.FC<{ stores: Store[] }> = ({ stores }) => (
  <>
    {stores.length === 0 && <div className={emptyCls}>فروشگاهی ثبت نشده است.</div>}
    {stores.map((s) => (
      <div key={s.id} className={rowCls}>
        <div className={rowHeaderCls}>
          <span className={rowTitleCls}>{s.name}</span>
          <span className={badgeCls}>{pick(s, ['status'])}</span>
        </div>
        <p>نامک: <bdi>{s.slug}</bdi> • مالک: {s.ownerName || '—'}</p>
        <p>تاریخ ثبت: {formatDate(pick(s, ['createdAt', 'created_at']))}</p>
      </div>
    ))}
  </>
);

export const AdminStoresListTab: React.FC = () => {
  const { stores } = useStore();
  return (
    <AdminListShell title="لیست فروشگاه‌ها" icon={<StoreIcon className={iconCls} strokeWidth={1.5} />}>
      <AdminStoresListView stores={stores} />
    </AdminListShell>
  );
};

// --- homepage: GET /admin/homepage-sections -> {sections: [...]}
export const AdminHomepageSectionsListView: React.FC<{ sections: HomepageSectionDto[] }> = ({ sections }) => (
  <>
    {sections.length === 0 && <div className={emptyCls}>بخشی برای صفحه اصلی ثبت نشده است.</div>}
    {sections.map((s) => (
      <div key={s.id} className={rowCls}>
        <div className={rowHeaderCls}>
          <span className={rowTitleCls}>{s.title || s.sectionType}</span>
          <span className={badgeCls}>{s.isActive ? 'فعال' : 'غیرفعال'}</span>
        </div>
        <p>نوع: {s.sectionType} • ترتیب: {s.sortOrder}</p>
      </div>
    ))}
  </>
);

export const AdminHomepageTab: React.FC = () => {
  const { items, ...state } = useAdminList<HomepageSectionDto>(
    adminApi.getAdminHomepageSections, 'خطا در دریافت بخش‌های صفحه اصلی');
  return (
    <AdminListShell title="صفحه اصلی" icon={<Home className={iconCls} strokeWidth={1.5} />} {...state}>
      <AdminHomepageSectionsListView sections={items} />
    </AdminListShell>
  );
};

// --- banners: GET /admin/banners -> {banners: [...]} (sort_order)
export const AdminBannersListView: React.FC<{ banners: AdminBannerDto[] }> = ({ banners }) => (
  <>
    {banners.length === 0 && <div className={emptyCls}>بنری ثبت نشده است.</div>}
    {banners.map((b) => (
      <div key={b.id} className={rowCls}>
        <div className={rowHeaderCls}>
          <span className={rowTitleCls}>{b.title}</span>
          <span className={badgeCls}>{b.isActive ? 'فعال' : 'غیرفعال'}</span>
        </div>
        <p>جایگاه: {b.position} • ترتیب: {b.sortOrder}</p>
        {(b.startsAt || b.endsAt) && (
          <p>از {formatDate(b.startsAt)} تا {formatDate(b.endsAt)}</p>
        )}
      </div>
    ))}
  </>
);

export const AdminBannersTab: React.FC = () => {
  const { items, ...state } = useAdminList<AdminBannerDto>(
    adminApi.getAdminBanners, 'خطا در دریافت بنرها');
  return (
    <AdminListShell title="بنرها" icon={<Image className={iconCls} strokeWidth={1.5} />} {...state}>
      <AdminBannersListView banners={items} />
    </AdminListShell>
  );
};

// --- site-content: GET /admin/site-content -> {content: {...}} (nested
// map, rendered as a flattened key/value list)
export const flattenSiteContent = (content: Record<string, any>, prefix = ''): Array<{ key: string; value: string }> => {
  const rows: Array<{ key: string; value: string }> = [];
  for (const [k, v] of Object.entries(content || {})) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v !== null && typeof v === 'object') rows.push(...flattenSiteContent(v as Record<string, any>, key));
    else rows.push({ key, value: String(v) });
  }
  return rows;
};

export const AdminSiteContentListView: React.FC<{ content: Record<string, any> }> = ({ content }) => {
  const rows = flattenSiteContent(content);
  return (
    <>
      {rows.length === 0 && <div className={emptyCls}>محتوایی ثبت نشده است.</div>}
      {rows.map((r) => (
        <div key={r.key} className={rowCls}>
          <p className={rowTitleCls}><bdi>{r.key}</bdi></p>
          <p><bdi>{r.value}</bdi></p>
        </div>
      ))}
    </>
  );
};

export const AdminSiteContentTab: React.FC = () => {
  const { data, ...state } = useAdminRead(adminApi.getAdminSiteContent, 'خطا در دریافت محتوای سایت');
  return (
    <AdminListShell title="محتوای سایت و صفحات" icon={<FileText className={iconCls} strokeWidth={1.5} />} {...state}>
      <AdminSiteContentListView content={data ?? {}} />
    </AdminListShell>
  );
};

// --- finance-callback-retries: GET /admin/callback-retries -> {retries}
// Read-only on purpose: no manual "retry now" button — replaying a
// gateway callback can double-post a payment outcome.
export const AdminCallbackRetriesListView: React.FC<{ retries: CallbackRetryDto[] }> = ({ retries }) => (
  <>
    {retries.length === 0 && <div className={emptyCls}>پرداخت ناموفقی ثبت نشده است.</div>}
    {retries.map((r) => (
      <div key={r.id} className={rowCls}>
        <div className={rowHeaderCls}>
          <bdi className={rowTitleCls}>{r.orderNumber || r.pspReference}</bdi>
          <span className={badgeCls}>{r.status}</span>
        </div>
        <p>تلاش‌ها: {r.attemptCount} از {r.maxAttempts} • رویداد: {r.eventStatus || '—'}</p>
        <p>آخرین خطا: <bdi>{r.lastError || '—'}</bdi></p>
        <p>تاریخ: {formatDate(r.createdAt)}</p>
      </div>
    ))}
  </>
);

export const AdminCallbackRetriesTab: React.FC = () => {
  const { items, ...state } = useAdminList<CallbackRetryDto>(
    adminApi.getAdminCallbackRetries, 'خطا در دریافت پرداخت‌های ناموفق');
  return (
    <AdminListShell title="پرداخت‌های ناموفق" icon={<RefreshCw className={iconCls} strokeWidth={1.5} />} {...state}>
      <AdminCallbackRetriesListView retries={items} />
    </AdminListShell>
  );
};

// --- reviews: GET /admin/reviews -> {reviews} over product_reviews.
// The generic list handler returns raw table columns (snake_case).
export const AdminReviewsListView: React.FC<{ reviews: AdminReviewRow[] }> = ({ reviews }) => (
  <>
    {reviews.length === 0 && <div className={emptyCls}>نظری ثبت نشده است.</div>}
    {reviews.map((r: any, i: number) => (
      <div key={pick(r, ['id']) + '-' + i} className={rowCls}>
        <div className={rowHeaderCls}>
          <span className={rowTitleCls}>{pick(r, ['customer_display_name', 'customer_name', 'customer_id'])}</span>
          <span className={badgeCls}>{pick(r, ['status'])}</span>
        </div>
        <p>کالا: {pick(r, ['product_id', 'productId', 'product_title'])} • امتیاز: {pick(r, ['rating'])} از ۵</p>
        {r.title && <p className="font-bold text-slate-700">{r.title}</p>}
        {r.body && <p>{r.body}</p>}
        <p>تاریخ: {formatDate(pick(r, ['created_at', 'createdAt']))}</p>
      </div>
    ))}
  </>
);

export const AdminReviewsTab: React.FC = () => {
  const { items, ...state } = useAdminList<AdminReviewRow>(
    adminApi.getAdminReviews, 'خطا در دریافت نظرات مشتریان');
  return (
    <AdminListShell title="نظرات مشتریان" icon={<Star className={iconCls} strokeWidth={1.5} />} {...state}>
      <AdminReviewsListView reviews={items} />
    </AdminListShell>
  );
};

// --- staff: GET /admin/staff -> {success, staff}. The staff query
// scopes to staff roles and does NOT include super_admin itself
// (super_admin is not "staff") — kept as-is by design.
export const AdminStaffListView: React.FC<{ staff: AdminStaffDto[] }> = ({ staff }) => (
  <>
    {staff.length === 0 && <div className={emptyCls}>کارکنی ثبت نشده است.</div>}
    {staff.map((u) => (
      <div key={u.id} className={rowCls}>
        <div className={rowHeaderCls}>
          <span className={rowTitleCls}>{u.fullName}</span>
          <span className={badgeCls}>{u.isActive ? 'فعال' : 'غیرفعال'}</span>
        </div>
        <p><bdi dir="ltr">{u.mobile}</bdi></p>
        <p>نقش: {u.role} • تاریخ ثبت: {formatDate(u.createdAt)}</p>
      </div>
    ))}
  </>
);

export const AdminStaffTab: React.FC = () => {
  const { items, ...state } = useAdminList<AdminStaffDto>(
    adminApi.getAdminStaff, 'خطا در دریافت کارکنان');
  return (
    <AdminListShell title="کارکنان انبار" icon={<Users className={iconCls} strokeWidth={1.5} />} {...state}>
      <AdminStaffListView staff={items} />
    </AdminListShell>
  );
};

export const AdminSupportReturnsTab: React.FC = () => {
  const { items, ...state } = useAdminList(adminApi.getAdminReturns, 'خطا در دریافت مرجوعی‌ها');
  return (
    <AdminListShell title="مرجوعی‌ها" icon={<RotateCcw className={iconCls} strokeWidth={1.5} />} {...state}>
      {items.length === 0 && <div className={emptyCls}>مرجوعی ثبت‌شده‌ای وجود ندارد.</div>}
      {items.map((r, i) => (
        <div key={r.id || i} className={rowCls}>
          <div className={rowHeaderCls}>
            <span className={rowTitleCls}>سفارش <bdi>{pick(r, ['orderId', 'order_id', 'orderNumber'])}</bdi></span>
            <span className={badgeCls}>{pick(r, ['status'])}</span>
          </div>
          {r.reason && <p>دلیل: {r.reason}</p>}
          {r.trackingNumber && <p>کد رهگیری: <bdi>{r.trackingNumber}</bdi></p>}
        </div>
      ))}
    </AdminListShell>
  );
};
