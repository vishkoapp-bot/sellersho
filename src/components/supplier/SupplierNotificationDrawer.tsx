import React, { useEffect, useState } from 'react';
import { supplierApi, SupplierNotification } from '../../services/supplierApi';
import { Bell, Check, CheckCheck, X, XCircle } from 'lucide-react';
import { motion } from 'motion/react';

// Supplier rejection-notification drawer (DESIGN.md §4.3).
// Deliberately NOT a reuse of src/components/common/NotificationDrawer:
// that drawer is the customer inbox (GET /api/v1/notifications +
// referral bonuses, with a confirm-free read-only list), while this
// one is the supplier inbox with the resubmit CTA for rejections.
// This drawer reads the supplier-scoped outbox via
// supplierApi.getNotifications() and marks read via
// supplierApi.markNotificationsRead() — server-side, not local-only.
// v1 scope: rejection notifications only (kind `proposal_rejected`);
// any other kind renders read-only, without the resubmit CTA.

export const countUnreadNotifications = (items: SupplierNotification[]): number =>
  items.filter((n) => n.status !== 'read').length;

// Read/unread tabs (ITEM 3): counts and filtering are pure so the tab
// contract is unit-testable without mounting the drawer. Tabs count
// the FULL list; filtering never changes the bell count (the header
// subtitle and onUnreadCountChange always derive from `notifications`).
export type NotificationTab = 'all' | 'unread' | 'read';

export const NOTIFICATION_TAB_LABELS: Record<NotificationTab, string> = {
  all: 'همه',
  unread: 'خوانده‌نشده',
  read: 'خوانده‌شده',
};

export const countSupplierNotificationsByTab = (items: SupplierNotification[]): Record<NotificationTab, number> => ({
  all: items.length,
  unread: items.filter((n) => n.status !== 'read').length,
  read: items.filter((n) => n.status === 'read').length,
});

export const filterSupplierNotificationsByTab = (items: SupplierNotification[], tab: NotificationTab): SupplierNotification[] => {
  if (tab === 'unread') return items.filter((n) => n.status !== 'read');
  if (tab === 'read') return items.filter((n) => n.status === 'read');
  return items;
};

// Standalone per-item mark-read (ITEM 3) — deliberately NOT coupled
// to selectProposalFromNotification: that helper only marks read as a
// side effect of the resubmit CTA, so a non-rejection row (or a
// rejection without proposal_id) had no per-item read path at all.
// Same best-effort pattern as the role drawer: POSTs {ids: [id]} and
// returns whether a call was attempted; the caller flips the row
// locally regardless of the POST outcome.
export const markOneSupplierNotificationRead = (
  item: SupplierNotification,
  markRead: (ids: string[]) => Promise<unknown>,
): boolean => {
  if (item.status === 'read') return false;
  void markRead([item.id]).catch(() => undefined);
  return true;
};

export const rejectionProposalId = (item: SupplierNotification): string | null =>
  item.kind === 'proposal_rejected' ? (item.payload?.proposal_id || null) : null;

// CTA decision for one drawer item, extracted so the click handler's
// contract is unit-testable without a DOM: renderToStaticMarkup renders
// the button but can never dispatch its onClick. The component's onClick
// calls this function verbatim. Returns the navigated proposal id, or
// null when the item has no resubmit target. The mark-read is best-effort
// and never blocks (or fails) the navigation; onMarkedRead reports the
// server's unreadCount when present, undefined when absent.
export const selectProposalFromNotification = (
  item: SupplierNotification,
  onSelectProposal: (proposalId: string) => void,
  markRead: (ids: string[]) => Promise<{ unreadCount?: number } | void>,
  onMarkedRead?: (unreadCount: number | undefined) => void,
): string | null => {
  const proposalId = rejectionProposalId(item);
  if (!proposalId) return null;
  void markRead([item.id])
    .then((res) => { onMarkedRead?.(typeof res?.unreadCount === 'number' ? res.unreadCount : undefined); })
    .catch(() => undefined);
  onSelectProposal(proposalId);
  return proposalId;
};

const toPersianDigits = (n: number): string => String(n).replace(/\d/g, (d) => '۰۱۲۳۴۵۶۷۸۹'[Number(d)]);

const formatNotifDate = (iso: string): string => {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.toLocaleDateString('fa-IR')} ${d.toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' })}`;
};

interface Props {
  isOpen: boolean;
  onClose: () => void;
  onSelectProposal: (proposalId: string) => void;
  onUnreadCountChange?: (count: number) => void;
  // Test seam (and only that): when provided, the drawer renders this
  // list instead of fetching. Production never passes it.
  initialNotifications?: SupplierNotification[];
  // Test seam (and only that): the tab the drawer opens on.
  // Production never passes it; the default tab is 'all'.
  initialTab?: NotificationTab;
}

export const SupplierNotificationDrawer: React.FC<Props> = ({
  isOpen,
  onClose,
  onSelectProposal,
  onUnreadCountChange,
  initialNotifications,
  initialTab,
}) => {
  const [notifications, setNotifications] = useState<SupplierNotification[]>(initialNotifications ?? []);
  const [loading, setLoading] = useState(!initialNotifications);
  const [loadError, setLoadError] = useState('');
  const [tab, setTab] = useState<NotificationTab>(initialTab ?? 'all');

  useEffect(() => {
    if (!isOpen) return;
    if (initialNotifications) {
      setNotifications(initialNotifications);
      setLoading(false);
      onUnreadCountChange?.(countUnreadNotifications(initialNotifications));
      return;
    }
    let cancelled = false;
    setLoading(true);
    setLoadError('');
    supplierApi.getNotifications()
      .then((res) => {
        if (cancelled) return;
        setNotifications(res.notifications);
        onUnreadCountChange?.(res.unreadCount);
      })
      .catch(() => {
        if (cancelled) return;
        // Endpoint may not exist yet (backend builder in parallel) or
        // the network may be down: show a retryable error, never crash
        // the panel, and leave the bell count as-is.
        setLoadError('دریافت اعلان‌ها انجام نشد.');
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
    // Fetch on every open so a rejection that landed while the panel
    // was open shows up (no polling in v1, DESIGN.md §5.4).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  if (!isOpen) return null;

  const unreadCount = countUnreadNotifications(notifications);
  const tabCounts = countSupplierNotificationsByTab(notifications);
  const visibleNotifications = filterSupplierNotificationsByTab(notifications, tab);

  const handleMarkAllRead = async () => {
    if (unreadCount === 0) return;
    try {
      const res = await supplierApi.markNotificationsRead();
      // The read UPDATE is idempotent (updated may be 0 on a second
      // call from another tab/device) — treat any 2xx as success.
      void res;
      setNotifications((prev) => prev.map((n) => ({ ...n, status: 'read' })));
      onUnreadCountChange?.(0);
    } catch {
      // Leave the list untouched; the supplier can retry from the drawer.
    }
  };

  // Standalone per-item mark-read: works on every unread row,
  // including non-rejections and rejections without a proposal id
  // (rows the resubmit CTA never touches).
  const handleMarkOneRead = (item: SupplierNotification) => {
    if (!markOneSupplierNotificationRead(item, (ids) => supplierApi.markNotificationsRead(ids))) return;
    const next = notifications.map((n) => (n.id === item.id ? { ...n, status: 'read' } : n));
    setNotifications(next);
    onUnreadCountChange?.(countUnreadNotifications(next));
  };

  const handleSelect = (item: SupplierNotification) => {
    // Mark this one read (best-effort, never blocks the navigation),
    // then land the supplier in the existing resubmit flow.
    selectProposalFromNotification(
      item,
      onSelectProposal,
      (ids) => supplierApi.markNotificationsRead(ids),
      (unreadCount) => {
        const next = notifications.map((n) => (n.id === item.id ? { ...n, status: 'read' } : n));
        setNotifications(next);
        onUnreadCountChange?.(unreadCount ?? countUnreadNotifications(next));
      },
    );
  };

  return (
    <div className="fixed inset-0 z-[100] overflow-hidden bg-slate-900/60 backdrop-blur-xs flex justify-center" dir="rtl">
      <motion.div
        initial={{ y: '100%' }}
        animate={{ y: 0 }}
        exit={{ y: '100%' }}
        transition={{ type: 'spring', damping: 28, stiffness: 240 }}
        className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between"
      >
        <div className="p-4 border-b border-slate-100 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-rose-50 text-rose-600 flex items-center justify-center">
              <Bell className="w-4 h-4" strokeWidth={1.5} />
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">اعلان‌های تأمین‌کننده</h3>
              <span className="text-[10px] text-slate-400 block">
                {unreadCount > 0 ? `${toPersianDigits(unreadCount)} اعلان جدید خوانده‌نشده` : 'تمام اعلان‌ها خوانده شده است'}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {unreadCount > 0 && (
              <button
                onClick={() => void handleMarkAllRead()}
                className="text-[10px] text-brand-deep font-bold hover:underline flex items-center gap-1"
              >
                <CheckCheck className="w-3.5 h-3.5" strokeWidth={1.5} />
                <span>خواندن همه</span>
              </button>
            )}
            <button
              onClick={onClose}
              className="p-1 rounded-lg text-slate-400 hover:bg-slate-100"
              aria-label="بستن اعلان‌ها"
            >
              <X className="w-4 h-4" strokeWidth={1.5} />
            </button>
          </div>
        </div>

        <div className="px-4 pt-3 flex items-center gap-2 border-b border-slate-100" role="tablist" aria-label="دسته‌بندی اعلان‌ها">
          {(['all', 'unread', 'read'] as NotificationTab[]).map((t) => {
            const active = tab === t;
            return (
              <button
                key={t}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setTab(t)}
                className={`pb-2 text-[11px] font-bold border-b-2 transition-colors ${
                  active ? 'text-rose-600 border-rose-600' : 'text-slate-400 border-transparent hover:text-slate-600'
                }`}
              >
                {`${NOTIFICATION_TAB_LABELS[t]} ${toPersianDigits(tabCounts[t])}`}
              </button>
            );
          })}
        </div>

        <div className="flex-1 overflow-y-auto p-3.5 space-y-2.5">
          {loading ? (
            <p className="text-xs text-slate-400 text-center py-8">در حال دریافت اعلان‌ها…</p>
          ) : loadError ? (
            <div className="rounded-2xl border border-rose-200 bg-rose-50 p-4 text-center space-y-2">
              <p className="text-xs font-bold text-rose-700">{loadError}</p>
              <button
                type="button"
                onClick={() => {
                  setLoading(true);
                  setLoadError('');
                  supplierApi.getNotifications()
                    .then((res) => { setNotifications(res.notifications); onUnreadCountChange?.(res.unreadCount); })
                    .catch(() => setLoadError('دریافت اعلان‌ها انجام نشد.'))
                    .finally(() => setLoading(false));
                }}
                className="min-h-[40px] px-4 rounded-xl bg-rose-600 text-white text-xs font-bold"
              >
                تلاش دوباره
              </button>
            </div>
          ) : notifications.length === 0 ? (
            <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center space-y-1.5">
              <p className="text-xs font-bold text-slate-600">اعلان جدیدی نیست.</p>
              <p className="text-[11px] leading-5 text-slate-400">وقتی مدیر پیشنهادت را رد کند، علت ردشدن اینجا می‌آید تا ویرایش کنی و دوباره بفرستی.</p>
            </div>
          ) : visibleNotifications.length === 0 ? (
            <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center space-y-1.5">
              <p className="text-xs font-bold text-slate-600">
                {tab === 'unread' ? 'اعلان خوانده‌نشده‌ای نیست.' : 'اعلان خوانده‌شده‌ای نیست.'}
              </p>
            </div>
          ) : (
            visibleNotifications.map((item) => {
              const proposalId = rejectionProposalId(item);
              const isUnread = item.status !== 'read';
              return (
                <div
                  key={item.id}
                  className={`p-3 rounded-2xl border transition-all text-xs space-y-1 ${
                    isUnread
                      ? 'bg-rose-50/50 border-rose-200 text-slate-900 ring-1 ring-rose-200'
                      : 'bg-white border-slate-200/80 text-slate-600'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5 font-bold">
                      <XCircle className="w-4 h-4 text-rose-600 shrink-0" strokeWidth={1.5} />
                      <span>{item.title}</span>
                    </div>
                    <span className="text-[10px] text-slate-400 font-mono shrink-0">{formatNotifDate(item.createdAt)}</span>
                  </div>
                  <p className="text-[11px] leading-relaxed text-slate-600 pr-5">
                    {item.body}
                  </p>
                  {isUnread && (
                    <div className="pt-1 pr-5">
                      <button
                        type="button"
                        aria-label={`خوانده شد: ${item.title}`}
                        onClick={() => handleMarkOneRead(item)}
                        className="inline-flex items-center gap-1 px-2.5 py-1 min-h-[28px] rounded-lg bg-white border border-slate-200 text-[10px] font-bold text-slate-600 hover:bg-slate-50"
                      >
                        <Check className="w-3.5 h-3.5" strokeWidth={2} />
                        <span>خوانده شد</span>
                      </button>
                    </div>
                  )}
                  {proposalId && (
                    <div className="pt-2 pr-5">
                      <button
                        type="button"
                        onClick={() => handleSelect(item)}
                        className="w-full py-1.5 min-h-[40px] px-3 bg-rose-600 hover:bg-rose-700 text-white rounded-xl font-bold text-xs flex items-center justify-center gap-1.5 shadow-2xs transition-colors"
                      >
                        <span>ویرایش و ارسال مجدد</span>
                      </button>
                    </div>
                  )}
                </div>
              );
            })
          )}
        </div>

        <div className="p-3 border-t border-slate-100 bg-slate-50">
          <button
            onClick={onClose}
            className="w-full py-2.5 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl shadow-xs transition-colors"
          >
            بستن اعلان‌ها
          </button>
        </div>
      </motion.div>
    </div>
  );
};
