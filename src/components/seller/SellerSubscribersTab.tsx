import React, { useCallback, useEffect, useState } from 'react';
import { useStore } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { apiRequest, getApiBase, tokenStore } from '../../services/apiClient';
import { ConfirmDialog } from '../common/ConfirmDialog';
import { SupplierSectionHeading } from '../supplier/SupplierSectionHeading';
import { Users, Download, Trash2, Loader2, ChevronRight, ChevronLeft } from 'lucide-react';

// Live parity (SellerDashboardView-MERGED.js, subscribers tab yn, offsets
// ~175300-182000; API shapes in index-MERGED.js ~443837-444100):
// live fetches GET /stores/<slug>/subscribers?limit=20&offset=<page*20>
// returning {items, total, counts:{active, unsubscribed}}; export is an
// authenticated blob download named subscribers-<slug>.csv; delete is
// DELETE /stores/<slug>/subscribers/<id> behind a confirm dialog.
// Copy below is live's informal second-person copy, verbatim.
// The paginated fetch is done here (not via storeApi.getStoreSubscribers,
// which returns a bare array and drops total/counts) so the tab can render
// live's count badges and pagination. Unpaginated/array responses are
// still accepted as a fallback and paginated client-side at 20 per page.
interface LiveSubscriber {
  id?: string;
  mobile?: string;
  email?: string;
  status?: string;
  createdAt?: string;
  source?: string;
}

interface SubscribersPage {
  items?: LiveSubscriber[];
  subscribers?: LiveSubscriber[];
  total?: number;
  counts?: { active?: number; unsubscribed?: number };
}

const PAGE_SIZE = 20;

// C8 (Batch 4b): derive the header totals from the page actually
// received when the server omits total/counts — mirroring the
// bare-array branch exactly (active = status 'active', unsubscribed
// = the rest). Server values win whenever they are present. A full
// page with no server total is a LOWER BOUND on the total, never the
// total: totalIsMinimum flags the honest «حداقل» wording.
export interface DerivedSubscribersPage {
  items: LiveSubscriber[];
  total: number;
  counts: { active: number; unsubscribed: number };
  totalIsMinimum: boolean;
}

export function deriveSubscribersPage(data: LiveSubscriber[] | SubscribersPage): DerivedSubscribersPage {
  if (Array.isArray(data)) {
    return {
      items: data,
      total: data.length,
      counts: {
        active: data.filter((s) => s.status === 'active').length,
        unsubscribed: data.filter((s) => s.status !== 'active').length,
      },
      totalIsMinimum: false,
    };
  }
  const items = data.items || data.subscribers || [];
  const counts = data.counts
    ? { active: data.counts.active ?? 0, unsubscribed: data.counts.unsubscribed ?? 0 }
    : {
        active: items.filter((s) => s.status === 'active').length,
        unsubscribed: items.filter((s) => s.status !== 'active').length,
      };
  return {
    items,
    total: data.total ?? items.length,
    counts,
    totalIsMinimum: data.total == null && items.length === PAGE_SIZE,
  };
}

export const SellerSubscribersTab: React.FC<{ onOpenStorefront?: () => void }> = ({ onOpenStorefront }) => {
  const { currentStore, toPersianDigits } = useStore();
  const { success, error } = useToast();
  const [subscribers, setSubscribers] = useState<LiveSubscriber[]>([]);
  const [total, setTotal] = useState(0);
  const [counts, setCounts] = useState<{ active?: number; unsubscribed?: number } | null>(null);
  const [totalIsMinimum, setTotalIsMinimum] = useState(false);
  const [page, setPage] = useState(0);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<LiveSubscriber | null>(null);
  const [deleting, setDeleting] = useState(false);
  // True when the backend returned a bare array (no server pagination):
  // then the whole list is in hand and pages are slices of it.
  const [allItems, setAllItems] = useState<LiveSubscriber[] | null>(null);

  const loadPage = useCallback(async (pageIndex: number) => {
    const slug = currentStore.slug;
    if (!slug) return;
    setLoading(true);
    try {
      const data = await apiRequest<LiveSubscriber[] | SubscribersPage>(
        `/stores/${encodeURIComponent(slug)}/subscribers?limit=${PAGE_SIZE}&offset=${pageIndex * PAGE_SIZE}`,
        { method: 'GET', auth: true },
      );
      const derived = deriveSubscribersPage(data);
      if (Array.isArray(data)) {
        setAllItems(data);
        setSubscribers(data.slice(pageIndex * PAGE_SIZE, (pageIndex + 1) * PAGE_SIZE));
      } else {
        setAllItems(null);
        setSubscribers(derived.items);
      }
      setTotal(derived.total);
      setCounts(derived.counts);
      setTotalIsMinimum(derived.totalIsMinimum);
      setFailed(false);
    } catch {
      setFailed(true);
    } finally {
      setLoading(false);
    }
  }, [currentStore.slug]);

  useEffect(() => { setPage(0); void loadPage(0); }, [loadPage]);

  const goToPage = (next: number) => {
    setPage(next);
    if (allItems) {
      setSubscribers(allItems.slice(next * PAGE_SIZE, (next + 1) * PAGE_SIZE));
    } else {
      void loadPage(next);
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const handleExport = async () => {
    if (exporting || !currentStore.slug) return;
    setExporting(true);
    try {
      const token = tokenStore.getToken();
      const response = await fetch(
        `${getApiBase()}/stores/${encodeURIComponent(currentStore.slug)}/subscribers/export`,
        { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: AbortSignal.timeout(60000) },
      );
      if (!response.ok) throw new Error('export failed');
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `subscribers-${currentStore.slug}.csv`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
      success('فهرست شماره‌های مشترک‌ها دانلود شد.');
    } catch {
      error('دانلود فهرست انجام نشد. دوباره تلاش کن.');
    } finally {
      setExporting(false);
    }
  };

  const handleDelete = async () => {
    if (!pendingDelete || deleting || !currentStore.slug || !pendingDelete.id) return;
    setDeleting(true);
    try {
      await apiRequest(
        `/stores/${encodeURIComponent(currentStore.slug)}/subscribers/${encodeURIComponent(pendingDelete.id)}`,
        { method: 'DELETE', auth: true },
      );
      success('مشترک حذف شد.');
      setPendingDelete(null);
      // Live steps back a page when the last row of a page is removed.
      const nextPage = subscribers.length === 1 && page > 0 ? page - 1 : page;
      setPage(nextPage);
      await loadPage(nextPage);
    } catch {
      error('حذف مشترک انجام نشد.');
    } finally {
      setDeleting(false);
    }
  };

  const formatDate = (value?: string) => {
    if (!value) return '';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '' : date.toLocaleDateString('fa-IR');
  };

  return (
    <div className="space-y-4 pb-6">
      <SupplierSectionHeading title="مشترکان فروشگاه" description="مشتری‌هایی را ببین که برای خبر گرفتن از موجودی و تخفیف‌ها شماره موبایلشان را ثبت کرده‌اند." icon={<Users />}
        action={
          <button
            type="button"
            onClick={handleExport}
            disabled={exporting || subscribers.length === 0}
            className="flex min-h-11 shrink-0 items-center gap-1.5 rounded-xl bg-brand-deep px-4 text-sm font-bold text-white focus-visible:ring-2 focus-visible:ring-brand disabled:opacity-50"
          >
            {exporting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Download className="w-3.5 h-3.5" />}
            <span>دانلود فهرست</span>
          </button>
        } />

      <div className="overflow-hidden rounded-[22px] border border-[#e6e2f0] bg-white shadow-sm">
        <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 p-4 text-xs sm:p-5">
          <span className="px-2.5 py-1 rounded-full bg-emerald-50 text-emerald-700 border border-emerald-200 font-bold">
            {counts ? toPersianDigits(counts.active ?? 0) : '…'} مشترک فعال
          </span>
          {counts && (counts.unsubscribed ?? 0) > 0 && (
            <span className="px-2.5 py-1 rounded-full bg-slate-100 text-slate-500 border border-slate-200 font-bold">
              {toPersianDigits(counts.unsubscribed ?? 0)} لغو عضویت‌کرده
            </span>
          )}
          <span className="min-w-0 text-slate-600">{totalIsMinimum ? `در مجموع حداقل ${toPersianDigits(total)} شماره` : `در مجموع ${toPersianDigits(total)} شماره`}</span>
        </div>
        {loading ? (
          <div role="status" aria-label="در حال دریافت مشترکان" className="flex items-center justify-center py-10 text-slate-500">
            <Loader2 className="w-6 h-6 animate-spin" />
          </div>
        ) : failed ? (
          <div className="p-6 text-center space-y-2">
            <p role="alert" className="text-sm font-bold text-slate-700">بارگذاری فهرست مشترکان انجام نشد.</p>
            <button
              type="button"
              onClick={() => void loadPage(page)}
              className="min-h-11 rounded-xl bg-brand-soft px-3 text-xs font-bold text-brand-deep focus-visible:ring-2 focus-visible:ring-brand"
            >
              تلاش دوباره
            </button>
          </div>
        ) : subscribers.length === 0 ? (
          <div className="p-8 text-center space-y-2.5">
            <Users className="w-8 h-8 mx-auto text-slate-300" strokeWidth={1.5} />
            <p className="text-sm font-bold text-slate-800">هنوز کسی مشترک نشده است</p>
            <p className="text-sm leading-6 text-slate-600">لینک فروشگاهت را برای مشتری‌ها بفرست تا بتوانند مشترک شوند.</p>
            {onOpenStorefront && (
              <button
                type="button"
                onClick={onOpenStorefront}
                className="min-h-[44px] px-4 rounded-xl bg-brand-soft text-brand-deep text-xs font-bold"
              >
                مشاهده فروشگاه من
              </button>
            )}
          </div>
        ) : (
          <div className="divide-y divide-slate-100">
            {subscribers.map((s) => {
              const isActive = s.status === 'active';
              return (
                <div key={s.id || s.mobile || s.email} className="flex items-center gap-2.5 px-3.5 py-2.5 text-xs">
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-bold text-slate-800 font-mono" dir="ltr">{s.mobile}</span>
                      <span className={`text-[9px] font-bold px-2 py-0.5 rounded-full border ${isActive ? 'bg-emerald-50 text-emerald-700 border-emerald-200' : 'bg-slate-100 text-slate-500 border-slate-200'}`}>
                        {isActive ? 'فعال' : 'لغو عضویت'}
                      </span>
                    </div>
                    <div className="flex items-center gap-2 text-[10px] text-slate-400 mt-0.5">
                      {s.email && <span className="truncate font-mono" dir="ltr">{s.email}</span>}
                      <span>عضویت: {formatDate(s.createdAt)}</span>
                      <span>{s.source === 'section' ? 'از بخش اطلاع‌رسانی' : 'از فوتر فروشگاه'}</span>
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => setPendingDelete(s)}
                    className="p-2.5 min-w-[40px] min-h-[40px] flex items-center justify-center rounded-xl bg-rose-50 text-rose-600 hover:bg-rose-100 shrink-0"
                    title="حذف این شماره"
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                  </button>
                </div>
              );
            })}
          </div>
        )}
        {total > PAGE_SIZE && (
          <div className="flex items-center justify-between px-3.5 py-2.5 border-t border-slate-100 text-[11px] font-bold">
            <button
              type="button"
              disabled={page <= 0 || loading}
              onClick={() => goToPage(page - 1)}
              className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-slate-100 text-slate-600 disabled:opacity-40"
            >
              <ChevronRight className="w-3.5 h-3.5" />قبلی
            </button>
            <span className="text-slate-500">صفحه {toPersianDigits(page + 1)} از {toPersianDigits(totalPages)}</span>
            <button
              type="button"
              disabled={page >= totalPages - 1 || loading}
              onClick={() => goToPage(page + 1)}
              className="flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-slate-100 text-slate-600 disabled:opacity-40"
            >
              بعدی<ChevronLeft className="w-3.5 h-3.5" />
            </button>
          </div>
        )}
      </div>

      <ConfirmDialog
        isOpen={!!pendingDelete}
        title="حذف مشترک"
        message={`شماره ${pendingDelete?.mobile || ''} از فهرست اطلاع‌رسانی فروشگاه شما حذف شود؟ این شماره دیگر در خروجی‌ها نخواهد بود.`}
        confirmText={deleting ? 'در حال حذف…' : 'حذف شود'}
        isDanger
        onConfirm={handleDelete}
        onCancel={() => { if (!deleting) setPendingDelete(null); }}
      />
    </div>
  );
};
