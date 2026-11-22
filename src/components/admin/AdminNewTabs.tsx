import React, { useState, useEffect } from 'react';
import {
  adminApi,
  HomepageSectionDto,
  AdminBannerDto,
  NewsletterSubscriberDto,
  AdminReviewRow,
  AdminStaffDto,
  CallbackRetryDto,
} from '../../services/adminApi';
import { useStore } from '../../context/StoreContext';
import type { Store } from '../../types';
import { RotateCcw, Store as StoreIcon, Home, Image, FileText, RefreshCw, Mail, Star, Users } from 'lucide-react';

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
  children: React.ReactNode;
}> = ({ title, icon, loading = false, loadError = null, children }) => (
  <div className="space-y-3">
    <div className="bg-white rounded-2xl border border-slate-200 p-4 flex items-center gap-2">
      {icon}
      <h3 className="text-xs font-bold text-slate-800">{title}</h3>
    </div>
    {loading && <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">در حال بارگذاری…</div>}
    {loadError && <div className="bg-white rounded-2xl border border-rose-200 p-4 text-xs text-rose-600">{loadError}</div>}
    {!loading && !loadError && children}
  </div>
);

// One-shot GET list fetch (AdminSupportReturnsTab pattern).
function useAdminList<T>(fetcher: () => Promise<T[]>, errorText: string) {
  const [items, setItems] = useState<T[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  useEffect(() => {
    let on = true;
    fetcher()
      .then((list) => { if (on) setItems(Array.isArray(list) ? list : []); })
      .catch((err: any) => { if (on) setLoadError(err?.message || errorText); })
      .finally(() => { if (on) setLoading(false); });
    return () => { on = false; };
  }, []);
  return { items, loading, loadError };
}

const iconCls = 'w-4 h-4 text-slate-500';
const rowCls = 'bg-white rounded-2xl border border-slate-200 p-4 space-y-1';
const badgeCls = 'text-[10px] font-bold bg-slate-100 text-slate-700 px-2 py-0.5 rounded-full';
const emptyCls = 'bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500';

// --- stores-list: reuses useStore().stores (same client data as
// AdminStoresTab); no new endpoint. Client Store objects carry no
// createdAt, so the column renders '—' unless the API provides one.
export const AdminStoresListView: React.FC<{ stores: Store[] }> = ({ stores }) => (
  <>
    {stores.length === 0 && <div className={emptyCls}>فروشگاهی ثبت نشده است.</div>}
    {stores.map((s) => (
      <div key={s.id} className={rowCls}>
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-slate-800">{s.name}</span>
          <span className={badgeCls}>{pick(s, ['status'])}</span>
        </div>
        <p className="text-[11px] text-slate-500">نامک: {s.slug} • مالک: {s.ownerName || '—'}</p>
        <p className="text-[11px] text-slate-500">تاریخ ثبت: {formatDate(pick(s, ['createdAt', 'created_at']))}</p>
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
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-slate-800">{s.title || s.sectionType}</span>
          <span className={badgeCls}>{s.isActive ? 'فعال' : 'غیرفعال'}</span>
        </div>
        <p className="text-[11px] text-slate-500">نوع: {s.sectionType} • ترتیب: {s.sortOrder}</p>
      </div>
    ))}
  </>
);

export const AdminHomepageTab: React.FC = () => {
  const { items, loading, loadError } = useAdminList<HomepageSectionDto>(
    adminApi.getAdminHomepageSections, 'خطا در دریافت بخش‌های صفحه اصلی');
  return (
    <AdminListShell title="صفحه اصلی" icon={<Home className={iconCls} strokeWidth={1.5} />} loading={loading} loadError={loadError}>
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
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-slate-800">{b.title}</span>
          <span className={badgeCls}>{b.isActive ? 'فعال' : 'غیرفعال'}</span>
        </div>
        <p className="text-[11px] text-slate-500">جایگاه: {b.position} • ترتیب: {b.sortOrder}</p>
        {(b.startsAt || b.endsAt) && (
          <p className="text-[11px] text-slate-500">از {formatDate(b.startsAt)} تا {formatDate(b.endsAt)}</p>
        )}
      </div>
    ))}
  </>
);

export const AdminBannersTab: React.FC = () => {
  const { items, loading, loadError } = useAdminList<AdminBannerDto>(
    adminApi.getAdminBanners, 'خطا در دریافت بنرها');
  return (
    <AdminListShell title="بنرها" icon={<Image className={iconCls} strokeWidth={1.5} />} loading={loading} loadError={loadError}>
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
          <p className="text-[11px] font-bold text-slate-700">{r.key}</p>
          <p className="text-[11px] text-slate-500 break-words">{r.value}</p>
        </div>
      ))}
    </>
  );
};

export const AdminSiteContentTab: React.FC = () => {
  const [content, setContent] = useState<Record<string, any>>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  useEffect(() => {
    let on = true;
    adminApi.getAdminSiteContent()
      .then((c) => { if (on) setContent(c || {}); })
      .catch((err: any) => { if (on) setLoadError(err?.message || 'خطا در دریافت محتوای سایت'); })
      .finally(() => { if (on) setLoading(false); });
    return () => { on = false; };
  }, []);
  return (
    <AdminListShell title="محتوای سایت و صفحات" icon={<FileText className={iconCls} strokeWidth={1.5} />} loading={loading} loadError={loadError}>
      <AdminSiteContentListView content={content} />
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
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-slate-800">{r.orderNumber || r.pspReference}</span>
          <span className={badgeCls}>{r.status}</span>
        </div>
        <p className="text-[11px] text-slate-500">تلاش‌ها: {r.attemptCount} از {r.maxAttempts} • رویداد: {r.eventStatus || '—'}</p>
        <p className="text-[11px] text-slate-600">آخرین خطا: {r.lastError || '—'}</p>
        <p className="text-[11px] text-slate-500">تاریخ: {formatDate(r.createdAt)}</p>
      </div>
    ))}
  </>
);

export const AdminCallbackRetriesTab: React.FC = () => {
  const { items, loading, loadError } = useAdminList<CallbackRetryDto>(
    adminApi.getAdminCallbackRetries, 'خطا در دریافت پرداخت‌های ناموفق');
  return (
    <AdminListShell title="پرداخت‌های ناموفق" icon={<RefreshCw className={iconCls} strokeWidth={1.5} />} loading={loading} loadError={loadError}>
      <AdminCallbackRetriesListView retries={items} />
    </AdminListShell>
  );
};

// --- newsletter: GET /admin/newsletter/subscribers -> {subscribers}
export const AdminNewsletterSubscribersListView: React.FC<{ subscribers: NewsletterSubscriberDto[] }> = ({ subscribers }) => (
  <>
    {subscribers.length === 0 && <div className={emptyCls}>مشترکی ثبت نشده است.</div>}
    {subscribers.map((s) => (
      <div key={s.id} className={rowCls}>
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-slate-800" dir="ltr">{s.mobile}</span>
          <span className={badgeCls}>{s.status}</span>
        </div>
        <p className="text-[11px] text-slate-500">ایمیل: {s.email || '—'} • فروشگاه: {s.storeId}</p>
        <p className="text-[11px] text-slate-500">تاریخ عضویت: {formatDate(s.createdAt)}</p>
      </div>
    ))}
  </>
);

export const AdminNewsletterTab: React.FC = () => {
  const { items, loading, loadError } = useAdminList<NewsletterSubscriberDto>(
    adminApi.getAdminNewsletterSubscribers, 'خطا در دریافت مشترکان خبرنامه');
  return (
    <AdminListShell title="مشترکان خبرنامه" icon={<Mail className={iconCls} strokeWidth={1.5} />} loading={loading} loadError={loadError}>
      <AdminNewsletterSubscribersListView subscribers={items} />
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
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-slate-800">{pick(r, ['customer_display_name', 'customer_name', 'customer_id'])}</span>
          <span className={badgeCls}>{pick(r, ['status'])}</span>
        </div>
        <p className="text-[11px] text-slate-500">کالا: {pick(r, ['product_id', 'productId', 'product_title'])} • امتیاز: {pick(r, ['rating'])} از ۵</p>
        {r.title && <p className="text-[11px] font-bold text-slate-700">{r.title}</p>}
        {r.body && <p className="text-[11px] text-slate-600">{r.body}</p>}
        <p className="text-[11px] text-slate-500">تاریخ: {formatDate(pick(r, ['created_at', 'createdAt']))}</p>
      </div>
    ))}
  </>
);

export const AdminReviewsTab: React.FC = () => {
  const { items, loading, loadError } = useAdminList<AdminReviewRow>(
    adminApi.getAdminReviews, 'خطا در دریافت نظرات مشتریان');
  return (
    <AdminListShell title="نظرات مشتریان" icon={<Star className={iconCls} strokeWidth={1.5} />} loading={loading} loadError={loadError}>
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
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-slate-800">{u.fullName}</span>
          <span className={badgeCls}>{u.isActive ? 'فعال' : 'غیرفعال'}</span>
        </div>
        <p className="text-[11px] text-slate-500" dir="ltr">{u.mobile}</p>
        <p className="text-[11px] text-slate-500">نقش: {u.role} • تاریخ ثبت: {formatDate(u.createdAt)}</p>
      </div>
    ))}
  </>
);

export const AdminStaffTab: React.FC = () => {
  const { items, loading, loadError } = useAdminList<AdminStaffDto>(
    adminApi.getAdminStaff, 'خطا در دریافت کارکنان');
  return (
    <AdminListShell title="کارکنان انبار" icon={<Users className={iconCls} strokeWidth={1.5} />} loading={loading} loadError={loadError}>
      <AdminStaffListView staff={items} />
    </AdminListShell>
  );
};

export const AdminSupportReturnsTab: React.FC = () => {
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  useEffect(() => {
    let on = true;
    adminApi.getAdminReturns()
      .then((list) => { if (on) setItems(list); })
      .catch((err: any) => { if (on) setLoadError(err?.message || 'خطا در دریافت مرجوعی‌ها'); })
      .finally(() => { if (on) setLoading(false); });
    return () => { on = false; };
  }, []);
  return (
    <div className="space-y-3">
      <div className="bg-white rounded-2xl border border-slate-200 p-4 flex items-center gap-2">
        <RotateCcw className="w-4 h-4 text-slate-500"  strokeWidth={1.5}/>
        <h3 className="text-xs font-bold text-slate-800">مرجوعی‌ها</h3>
      </div>
      {loading && <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">در حال بارگذاری…</div>}
      {loadError && <div className="bg-white rounded-2xl border border-rose-200 p-4 text-xs text-rose-600">{loadError}</div>}
      {!loading && !loadError && items.length === 0 && (
        <div className="bg-white rounded-2xl border border-slate-200 p-4 text-xs text-slate-500">مرجوعی ثبت‌شده‌ای وجود ندارد.</div>
      )}
      {!loading && !loadError && items.map((r: any, i: number) => (
        <div key={r.id || i} className="bg-white rounded-2xl border border-slate-200 p-4 space-y-1">
          <div className="flex items-center justify-between gap-2">
            <span className="text-xs font-bold text-slate-800">سفارش {pick(r, ['orderId', 'order_id', 'orderNumber'])}</span>
            <span className="text-[10px] font-bold bg-slate-100 text-slate-700 px-2 py-0.5 rounded-full">{pick(r, ['status'])}</span>
          </div>
          {r.reason && <p className="text-[11px] text-slate-600">دلیل: {r.reason}</p>}
          {r.trackingNumber && <p className="text-[11px] text-slate-500">کد رهگیری: {r.trackingNumber}</p>}
        </div>
      ))}
    </div>
  );
};
