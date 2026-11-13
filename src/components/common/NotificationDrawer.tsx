import React, { useState, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useStore, useStoreManager } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi } from '../../services/storeApi';
import { customerNotificationsApi, type OutboxNotification } from '../../services/notificationsApi';
import { isMockMode } from '../../services/apiClient';
import { tokenStore } from '../../services/apiClient';
import { Bell, CheckCheck, X, ShieldAlert, Sparkles, Package, Truck, Wallet } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';

interface NotificationItem {
  id: string;
  role: string;
  title: string;
  message: string;
  time: string;
  read: boolean;
  type: 'order' | 'finance' | 'warehouse' | 'system';
}

const initialNotifications: NotificationItem[] = [];

// Outbox rows arrive as server state; the drawer maps them onto the
// local item shape. Pure + exported so the wiring is unit-testable
// without mounting the drawer (it needs StoreContext providers).
export const formatNotificationTime = (iso: string): string => {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return 'به‌تازگی';
  return `${d.toLocaleDateString('fa-IR')} ${d.toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' })}`;
};

export const mapOutboxNotificationToItem = (n: OutboxNotification): NotificationItem => {
  const kind = n.kind || '';
  const type: NotificationItem['type'] =
    kind === 'order_status' || kind.startsWith('order')
      ? 'order'
      : kind.includes('finance') || kind.includes('payout') || kind.includes('refund') || kind.includes('settlement') || kind.includes('adjustment')
        ? 'finance'
        : kind.includes('ship') || kind.includes('inbound') || kind.includes('return') || kind.includes('warehouse')
          ? 'warehouse'
          : 'system';
  return {
    id: n.id,
    role: 'customer',
    title: n.title,
    message: n.body,
    time: formatNotificationTime(n.createdAt),
    read: n.status === 'read',
    type,
  };
};

// P1-14: referral bonuses have no server-side read column
// (referral_bonuses carries no read state — a persistent server read
// state would need a migration), so read state for bonus items lives
// in localStorage, per store. Same device class as the existing toast
// dedupe, but it now survives reloads and mark-all-read instead of
// resetting to unread on every open.
export const bonusReadStorageKey = (storeSlug: string): string =>
  `seler-notifications-read-bonus:${storeSlug}`;

export const loadReadBonusIds = (storeSlug: string): Set<string> => {
  try {
    if (typeof localStorage === 'undefined' || !storeSlug) return new Set();
    const raw = localStorage.getItem(bonusReadStorageKey(storeSlug));
    if (!raw) return new Set();
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? new Set(parsed.filter((v): v is string => typeof v === 'string')) : new Set();
  } catch {
    return new Set();
  }
};

export const storeReadBonusIds = (storeSlug: string, ids: Set<string>): void => {
  try {
    if (typeof localStorage === 'undefined' || !storeSlug) return;
    localStorage.setItem(bonusReadStorageKey(storeSlug), JSON.stringify([...ids]));
  } catch {
    // Read state is best-effort; a full/blocked storage must never
    // break the drawer.
  }
};

export const NotificationDrawer: React.FC<{ isOpen: boolean; onClose: () => void }> = ({
  isOpen,
  onClose,
}) => {
  const { currentStore, formatPrice } = useStore();
  const { setUnreadNotificationCount } = useStoreManager();
  const { error } = useToast();
  const [notifications, setNotifications] = useState<NotificationItem[]>(initialNotifications);
  const [outboxError, setOutboxError] = useState('');

  // Fetch persistent released bonus notifications for current seller store.
  // ISO-D Worker 3: bonus items are scoped to the CURRENT store only. The
  // previous append-only merge kept the previous store's bonus items (and
  // their unread count) after a store switch, leaking one store's
  // notifications into another store's drawer/header bell. The bonus slice
  // below is therefore replaced (not merged) on every fetch, and cleared
  // when the current store is not a dropshipper store or the fetch fails.
  useEffect(() => {
    if (!isMockMode() && tokenStore.getToken() && currentStore?.ownerType === 'dropshipper' && currentStore.slug) {
      const readBonusIds = loadReadBonusIds(currentStore.slug);
      storeApi.getReferralNotifications(currentStore.slug)
        .then((notifs) => {
          if (Array.isArray(notifs)) {
            const bonusNotifItems: NotificationItem[] = notifs.map((n) => ({
              id: `notif-bonus-${n.id}`,
              role: 'dropshipper',
              title: 'پاداش معرفی آزاد شد',
              message: n.message || (n.amount
                ? `هدیه ${formatPrice(n.amount)} تومانی برای فروشگاه «${n.referredStoreName}» قابل برداشت شد.`
                : `پاداش معرفی برای فروشگاه «${n.referredStoreName}» قابل برداشت شد.`),
              time: n.releasedAt ? new Date(n.releasedAt).toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }) : 'به‌تازگی',
              read: readBonusIds.has(`notif-bonus-${n.id}`),
              type: 'finance',
            }));

            setNotifications((prev) => [...bonusNotifItems, ...prev.filter((p) => !p.id.startsWith('notif-bonus-'))]);
          }
        })
        .catch(() => { setNotifications((prev) => prev.filter((p) => !p.id.startsWith('notif-bonus-'))); });
    } else {
      setNotifications((prev) => prev.filter((p) => !p.id.startsWith('notif-bonus-')));
    }
  }, [currentStore?.ownerType, currentStore?.slug]);

  // P0-1: fetch the customer outbox on every open — packed/shipped
  // rows (and every future customer row) were orphaned because this
  // drawer never called GET /api/v1/notifications. The outbox slice
  // is replaced (not merged) on every fetch, mirroring the bonus
  // slice above; bonus items are left untouched. A 401/403 (e.g. a
  // seller viewing a storefront without the customer capability)
  // silently leaves bonuses only; other failures show a retryable
  // error instead of stale rows.
  useEffect(() => {
    if (!isOpen) return;
    if (isMockMode() || !tokenStore.getToken()) {
      setNotifications((prev) => prev.filter((p) => p.id.startsWith('notif-bonus-')));
      return;
    }
    let cancelled = false;
    setOutboxError('');
    customerNotificationsApi.getNotifications()
      .then((res) => {
        if (cancelled) return;
        const outboxItems = res.notifications.map(mapOutboxNotificationToItem);
        setNotifications((prev) => [...outboxItems, ...prev.filter((p) => p.id.startsWith('notif-bonus-'))]);
      })
      .catch((e: any) => {
        if (cancelled) return;
        if (e?.status === 401 || e?.status === 403) {
          setNotifications((prev) => prev.filter((p) => p.id.startsWith('notif-bonus-')));
          return;
        }
        setOutboxError('دریافت اعلان‌ها انجام نشد.');
        setNotifications((prev) => prev.filter((p) => p.id.startsWith('notif-bonus-')));
      });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  // Header bell dot (live parity): live's drawer syncs its unread
  // count to the header's counter on every list change (live
  // bundle: useEffect(() => { v5(y) }, [y]) with y = unread
  // count). This mirrors that from the same list this drawer
  // renders; no other drawer behavior is changed.
  useEffect(() => {
    setUnreadNotificationCount(notifications.filter((n) => !n.read).length);
  }, [notifications, setUnreadNotificationCount]);

  if (!isOpen) return null;

  const unreadCount = notifications.filter(n => !n.read).length;

  // Mark-all-read is server-side for outbox rows (POST
  // /api/v1/notifications/read, empty body = mark-all) and persisted
  // locally for bonus rows (P1-14 read-set). On a server failure the
  // list is left untouched so the user can retry — the old local-only
  // flip is gone: it used to report read rows the server still
  // counted as unread.
  const handleMarkAllRead = async () => {
    const unreadOutbox = notifications.filter((n) => !n.read && !n.id.startsWith('notif-bonus-'));
    if (unreadOutbox.length > 0) {
      try {
        await customerNotificationsApi.markNotificationsRead();
      } catch {
        error('خواندن همه اعلان‌ها انجام نشد؛ لطفاً دوباره تلاش کنید.');
        return;
      }
    }
    const slug = currentStore?.slug;
    if (slug) {
      const next = loadReadBonusIds(slug);
      notifications.filter((n) => n.id.startsWith('notif-bonus-')).forEach((n) => next.add(n.id));
      storeReadBonusIds(slug, next);
    }
    setNotifications(prev => prev.map(n => ({ ...n, read: true })));
  };

  // Tapping an unread row marks just that row read: server-side for
  // outbox rows (best-effort — the row flips locally even if the
  // POST fails; the next open re-fetches the server state), persisted
  // locally for bonus rows.
  const handleItemClick = (item: NotificationItem) => {
    if (item.read) return;
    if (item.id.startsWith('notif-bonus-')) {
      const slug = currentStore?.slug;
      if (slug) {
        const next = loadReadBonusIds(slug);
        next.add(item.id);
        storeReadBonusIds(slug, next);
      }
    } else {
      void customerNotificationsApi.markNotificationsRead([item.id]).catch(() => undefined);
    }
    setNotifications(prev => prev.map(n => n.id === item.id ? { ...n, read: true } : n));
  };

  const getIcon = (type: NotificationItem['type']) => {
    switch (type) {
      case 'order': return <Package className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>;
      case 'finance': return <Wallet className="w-4 h-4 text-emerald-500"  strokeWidth={1.5}/>;
      case 'warehouse': return <Truck className="w-4 h-4 text-amber-500"  strokeWidth={1.5}/>;
      default: return <Sparkles className="w-4 h-4 text-brand-deep"  strokeWidth={1.5}/>;
    }
  };

  return createPortal(
    <div className="fixed inset-0 z-[100] overflow-hidden bg-slate-900/60 backdrop-blur-xs flex justify-center">
      <motion.div
        initial={{ y: '100%' }}
        animate={{ y: 0 }}
        exit={{ y: '100%' }}
        transition={{ type: 'spring', damping: 28, stiffness: 240 }}
        className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between"
      >
        <div className="p-4 border-b border-slate-100 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center">
              <Bell className="w-4 h-4"  strokeWidth={1.5}/>
            </div>
            <div>
              <h3 className="text-xs font-bold text-slate-900">مرکز اعلان‌های سیستم</h3>
              <span className="text-[10px] text-slate-400 block">
                {unreadCount > 0 ? `${unreadCount} اعلان جدید خوانده‌نشده` : 'تمام اعلان‌ها خوانده شده است'}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {unreadCount > 0 && (
              <button
                onClick={() => void handleMarkAllRead()}
                className="text-[10px] text-brand-deep font-bold hover:underline flex items-center gap-1"
              >
                <CheckCheck className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                <span>خواندن همه</span>
              </button>
            )}
            <button
              onClick={onClose}
              className="p-1 rounded-lg text-slate-400 hover:bg-slate-100"
            >
              <X className="w-4 h-4"  strokeWidth={1.5}/>
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-3.5 space-y-2.5">
          {outboxError && (
            <div className="rounded-2xl border border-rose-200 bg-rose-50 p-3 text-center">
              <p className="text-xs font-bold text-rose-700">{outboxError}</p>
            </div>
          )}
          {notifications.length === 0 && !outboxError ? (
            <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center space-y-1.5">
              <p className="text-xs font-bold text-slate-600">اعلان جدیدی نیست.</p>
              <p className="text-[11px] leading-5 text-slate-400">بسته‌بندی و ارسال سفارش‌های شما اینجا نمایش داده می‌شود.</p>
            </div>
          ) : (
            notifications.map((item) => (
              <div
                key={item.id}
                onClick={() => handleItemClick(item)}
                className={`p-3 rounded-2xl border transition-all text-xs space-y-1 ${
                  item.read
                    ? 'bg-white border-slate-200/80 text-slate-600'
                    : 'bg-brand-soft/40 border-brand-border text-slate-900 ring-1 ring-brand-border cursor-pointer'
                }`}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-1.5 font-bold">
                    {getIcon(item.type)}
                    <span>{item.title}</span>
                  </div>
                  <span className="text-[10px] text-slate-400 font-mono">{item.time}</span>
                </div>
                <p className="text-[11px] leading-relaxed text-slate-600 pr-5">
                  {item.message}
                </p>
              </div>
            ))
          )}
        </div>

        <div className="p-3 border-t border-slate-100 bg-slate-50">
          <button
            onClick={onClose}
            className="w-full py-2.5 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl shadow-xs transition-colors"
          >
            بستن مرکز اعلان‌ها
          </button>
        </div>
      </motion.div>
    </div>,
    document.body,
  );
};
