import React, { useEffect, useState } from 'react';
import type { NotificationsResponse, OutboxNotification, RoleNotificationsApi } from '../../services/notificationsApi';
import { Bell, Check, CheckCheck, Package, Sparkles, Truck, Wallet, X } from 'lucide-react';
import { motion } from 'motion/react';

// Generic role-scoped outbox drawer (Phase 2, W2) — the seller, admin
// and warehouse counterpart of SupplierNotificationDrawer, which it
// deliberately mirrors: fetch on open (no polling), server-side
// mark-all-read, click an unread row to mark just that row read
// (best-effort, never blocks), retryable load error, empty state.
// The only per-role differences are the copy, the accent and the api
// object passed in; there is no per-role drawer fork to drift.
// v1 scope: read-only rows. Any row kind renders without an action
// button — a kind that later needs a deep link gets its own CTA then,
// it does not get one guessed here.

export const countUnreadOutboxNotifications = (items: OutboxNotification[]): number =>
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

export const countOutboxNotificationsByTab = (items: OutboxNotification[]): Record<NotificationTab, number> => ({
  all: items.length,
  unread: items.filter((n) => n.status !== 'read').length,
  read: items.filter((n) => n.status === 'read').length,
});

export const filterOutboxNotificationsByTab = (items: OutboxNotification[], tab: NotificationTab): OutboxNotification[] => {
  if (tab === 'unread') return items.filter((n) => n.status !== 'read');
  if (tab === 'read') return items.filter((n) => n.status === 'read');
  return items;
};

// Per-item mark-read, extracted so the button's wire call is
// unit-testable without a DOM (this harness has no jsdom): POSTs
// {ids: [id]} best-effort and returns whether a call was attempted.
// The caller flips the row locally regardless of the POST outcome —
// the next open re-fetches the server state.
export const markOneOutboxNotificationRead = (
  item: OutboxNotification,
  markRead: (ids: string[]) => Promise<unknown>,
): boolean => {
  if (item.status === 'read') return false;
  void markRead([item.id]).catch(() => undefined);
  return true;
};

const toPersianDigits = (n: number): string => String(n).replace(/\d/g, (d) => '۰۱۲۳۴۵۶۷۸۹'[Number(d)]);

export const formatOutboxDate = (iso: string): string => {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${d.toLocaleDateString('fa-IR')} ${d.toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' })}`;
};

const kindIcon = (kind: string): React.ReactNode => {
  if (kind === 'order_status' || kind.startsWith('order')) {
    return <Package className="w-4 h-4 text-brand-deep shrink-0" strokeWidth={1.5} />;
  }
  if (kind.includes('finance') || kind.includes('payout') || kind.includes('refund') || kind.includes('settlement') || kind.includes('adjustment')) {
    return <Wallet className="w-4 h-4 text-emerald-500 shrink-0" strokeWidth={1.5} />;
  }
  if (kind.includes('ship') || kind.includes('inbound') || kind.includes('return') || kind.includes('warehouse')) {
    return <Truck className="w-4 h-4 text-amber-500 shrink-0" strokeWidth={1.5} />;
  }
  return <Sparkles className="w-4 h-4 text-brand-deep shrink-0" strokeWidth={1.5} />;
};

type Accent = 'brand' | 'blue' | 'amber';

const ACCENT_CLASSES: Record<Accent, { iconWrap: string; unreadRing: string }> = {
  brand: { iconWrap: 'bg-brand-soft text-brand-deep', unreadRing: 'bg-brand-soft/40 border-brand-border ring-brand-border' },
  blue: { iconWrap: 'bg-blue-50 text-blue-700', unreadRing: 'bg-blue-50/60 border-blue-200 ring-blue-200' },
  amber: { iconWrap: 'bg-amber-50 text-amber-700', unreadRing: 'bg-amber-50/60 border-amber-200 ring-amber-200' },
};

interface Props {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  emptyTitle: string;
  emptyHint: string;
  accent?: Accent;
  api: RoleNotificationsApi;
  onUnreadCountChange?: (count: number) => void;
  // Optional section rendered above the outbox list (the admin
  // drawer uses it for the overview alert counts, which stay one
  // tap away from the bell). Rendered only when provided.
  extraContent?: React.ReactNode;
  // Test seam (and only that): when provided, the drawer renders this
  // list instead of fetching. Production never passes it.
  initialNotifications?: OutboxNotification[];
  // Test seam (and only that): the tab the drawer opens on.
  // Production never passes it; the default tab is 'all'.
  initialTab?: NotificationTab;
}

export const RoleNotificationDrawer: React.FC<Props> = ({
  isOpen,
  onClose,
  title,
  emptyTitle,
  emptyHint,
  accent = 'brand',
  api,
  onUnreadCountChange,
  extraContent,
  initialNotifications,
  initialTab,
}) => {
  const [notifications, setNotifications] = useState<OutboxNotification[]>(initialNotifications ?? []);
  const [loading, setLoading] = useState(!initialNotifications);
  const [loadError, setLoadError] = useState('');
  const [tab, setTab] = useState<NotificationTab>(initialTab ?? 'all');

  useEffect(() => {
    if (!isOpen) return;
    if (initialNotifications) {
      setNotifications(initialNotifications);
      setLoading(false);
      onUnreadCountChange?.(countUnreadOutboxNotifications(initialNotifications));
      return;
    }
    let cancelled = false;
    setLoading(true);
    setLoadError('');
    api.getNotifications()
      .then((res: NotificationsResponse) => {
        if (cancelled) return;
        setNotifications(res.notifications);
        onUnreadCountChange?.(res.unreadCount);
      })
      .catch(() => {
        if (cancelled) return;
        // Endpoint may be unreachable or the network down: show a
        // retryable error, never crash the panel, and leave the bell
        // count as-is.
        setLoadError('دریافت اعلان‌ها انجام نشد.');
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
    // Fetch on every open so a notice that landed while the panel
    // was open shows up (no polling in v1, same as the supplier drawer).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  if (!isOpen) return null;

  const unreadCount = countUnreadOutboxNotifications(notifications);
  const accentClasses = ACCENT_CLASSES[accent];
  const tabCounts = countOutboxNotificationsByTab(notifications);
  const visibleNotifications = filterOutboxNotificationsByTab(notifications, tab);

  const handleMarkAllRead = async () => {
    if (unreadCount === 0) return;
    try {
      // The read UPDATE is idempotent (updated may be 0 on a second
      // call from another tab/device) — treat any 2xx as success.
      await api.markNotificationsRead();
      setNotifications((prev) => prev.map((n) => ({ ...n, status: 'read' })));
      onUnreadCountChange?.(0);
    } catch {
      // Leave the list untouched; the user can retry from the drawer.
    }
  };

  const handleMarkOneRead = (item: OutboxNotification) => {
    if (!markOneOutboxNotificationRead(item, (ids) => api.markNotificationsRead(ids))) return;
    // Best-effort: the row flips locally even if the POST fails, the
    // next open re-fetches the server state.
    const next = notifications.map((n) => (n.id === item.id ? { ...n, status: 'read' } : n));
    setNotifications(next);
    onUnreadCountChange?.(countUnreadOutboxNotifications(next));
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
            <div className={`w-8 h-8 rounded-xl ${accentClasses.iconWrap} flex items-center justify-center`}>
              <Bell className="w-4 h-4" strokeWidth={1.5} />
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">{title}</h3>
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
                  active ? 'text-brand-deep border-brand-deep' : 'text-slate-400 border-transparent hover:text-slate-600'
                }`}
              >
                {`${NOTIFICATION_TAB_LABELS[t]} ${toPersianDigits(tabCounts[t])}`}
              </button>
            );
          })}
        </div>

        <div className="flex-1 overflow-y-auto p-3.5 space-y-2.5">
          {extraContent}
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
                  api.getNotifications()
                    .then((res: NotificationsResponse) => { setNotifications(res.notifications); onUnreadCountChange?.(res.unreadCount); })
                    .catch(() => setLoadError('دریافت اعلان‌ها انجام نشد.'))
                    .finally(() => setLoading(false));
                }}
                className="min-h-[40px] px-4 rounded-xl bg-rose-600 text-white text-xs font-bold"
              >
                تلاش دوباره
              </button>
            </div>
          ) : notifications.length === 0 ? (
            extraContent ? null : (
              <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center space-y-1.5">
                <p className="text-xs font-bold text-slate-600">{emptyTitle}</p>
                <p className="text-[11px] leading-5 text-slate-400">{emptyHint}</p>
              </div>
            )
          ) : visibleNotifications.length === 0 ? (
            <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center space-y-1.5">
              <p className="text-xs font-bold text-slate-600">
                {tab === 'unread' ? 'اعلان خوانده‌نشده‌ای نیست.' : 'اعلان خوانده‌شده‌ای نیست.'}
              </p>
            </div>
          ) : (
            visibleNotifications.map((item) => {
              const isUnread = item.status !== 'read';
              return (
                <div
                  key={item.id}
                  onClick={() => handleMarkOneRead(item)}
                  className={`p-3 rounded-2xl border transition-all text-xs space-y-1 ${
                    isUnread
                      ? `${accentClasses.unreadRing} text-slate-900 ring-1 cursor-pointer`
                      : 'bg-white border-slate-200/80 text-slate-600'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5 font-bold">
                      {kindIcon(item.kind)}
                      <span>{item.title}</span>
                    </div>
                    <span className="text-[10px] text-slate-400 font-mono shrink-0">{formatOutboxDate(item.createdAt)}</span>
                  </div>
                  <p className="text-[11px] leading-relaxed text-slate-600 pr-5">
                    {item.body}
                  </p>
                  {isUnread && (
                    <div className="pt-1 pr-5">
                      <button
                        type="button"
                        aria-label={`خوانده شد: ${item.title}`}
                        onClick={(e) => { e.stopPropagation(); handleMarkOneRead(item); }}
                        className="inline-flex items-center gap-1 px-2.5 py-1 min-h-[28px] rounded-lg bg-white border border-slate-200 text-[10px] font-bold text-slate-600 hover:bg-slate-50"
                      >
                        <Check className="w-3.5 h-3.5" strokeWidth={2} />
                        <span>خوانده شد</span>
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
