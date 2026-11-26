import React, { useState, useEffect, useId, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useStore, useStoreManager } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi } from '../../services/storeApi';
import { customerNotificationsApi, type OutboxNotification } from '../../services/notificationsApi';
import { isMockMode } from '../../services/apiClient';
import { tokenStore } from '../../services/apiClient';
import { Bell, BellOff, Check, CheckCheck, X, Sparkles, Package, Truck, Wallet } from 'lucide-react';
import { motion } from 'motion/react';

export interface NotificationItem {
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

// Read/unread tabs (ITEM 3): counts and filtering are pure so the tab
// contract is unit-testable without mounting the drawer (it needs
// StoreContext providers). Tabs count the FULL merged list — server
// outbox rows PLUS referral-bonus rows — and filtering never changes
// the bell count (the header bell syncs from `notifications`).
export type NotificationTab = 'all' | 'unread' | 'read';

export const NOTIFICATION_TAB_LABELS: Record<NotificationTab, string> = {
  all: 'همه',
  unread: 'خوانده نشده',
  read: 'خوانده شده',
};

export const countNotificationsByTab = (items: NotificationItem[]): Record<NotificationTab, number> => ({
  all: items.length,
  unread: items.filter((n) => !n.read).length,
  read: items.filter((n) => n.read).length,
});

export const filterNotificationsByTab = (items: NotificationItem[], tab: NotificationTab): NotificationItem[] => {
  if (tab === 'unread') return items.filter((n) => !n.read);
  if (tab === 'read') return items.filter((n) => n.read);
  return items;
};

export const isBonusNotification = (item: NotificationItem): boolean =>
  item.id.startsWith('notif-bonus-');

// Per-item mark-read decision, extracted so the bonus-local vs
// server split is unit-testable without mounting the drawer. Bonus
// rows have no server id, so marking one read stays local-only
// (localStorage read-set, never a POST); outbox rows POST
// {ids: [id]} best-effort. Returns what happened; the caller flips
// the row locally in all cases except 'already-read'.
export type MarkOneCustomerResult = 'local' | 'server' | 'already-read';

export const markOneCustomerNotificationRead = (
  item: NotificationItem,
  storeSlug: string | undefined,
  markRead: (ids: string[]) => Promise<unknown>,
): MarkOneCustomerResult => {
  if (item.read) return 'already-read';
  if (isBonusNotification(item)) {
    if (storeSlug) {
      const next = loadReadBonusIds(storeSlug);
      next.add(item.id);
      storeReadBonusIds(storeSlug, next);
    }
    return 'local';
  }
  void markRead([item.id]).catch(() => undefined);
  return 'server';
};

const toPersianDigits = (n: number): string => String(n).replace(/\d/g, (d) => '۰۱۲۳۴۵۶۷۸۹'[Number(d)]);

export const NotificationDrawer: React.FC<{ isOpen: boolean; onClose: () => void; central?: boolean }> = ({
  isOpen,
  onClose,
  central = false,
}) => {
  const { currentStore, formatPrice } = useStore();
  const { setUnreadNotificationCount } = useStoreManager();
  const { error } = useToast();
  const [notifications, setNotifications] = useState<NotificationItem[]>(initialNotifications);
  const [outboxError, setOutboxError] = useState('');
  const [tab, setTab] = useState<NotificationTab>('all');
  const [outboxLoading, setOutboxLoading] = useState(false);
  const [retry, setRetry] = useState(0);
  const [markReadError, setMarkReadError] = useState('');
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const tabsId = useId();

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!central || !isOpen || !dialog || dialog.open) return;
    const previousFocus = document.activeElement;
    const { overflow } = document.body.style;
    dialog.showModal();
    document.body.style.overflow = 'hidden';
    return () => {
      dialog.close();
      document.body.style.overflow = overflow;
      if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus({ preventScroll: true });
    };
  }, [central, isOpen]);

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
    setMarkReadError('');
    if (isMockMode() || !tokenStore.getToken()) {
      setOutboxLoading(false);
      setOutboxError('');
      setNotifications((prev) => prev.filter((p) => p.id.startsWith('notif-bonus-')));
      return;
    }
    let cancelled = false;
    setOutboxError('');
    setOutboxLoading(true);
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
      })
      .finally(() => { if (!cancelled) setOutboxLoading(false); });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen, retry]);

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
  const tabCounts = countNotificationsByTab(notifications);
  const visibleNotifications = filterNotificationsByTab(notifications, tab);

  // Mark-all-read is server-side for outbox rows (POST
  // /api/v1/notifications/read, empty body = mark-all) and persisted
  // locally for bonus rows (P1-14 read-set). On a server failure the
  // list is left untouched so the user can retry — the old local-only
  // flip is gone: it used to report read rows the server still
  // counted as unread.
  const handleMarkAllRead = async () => {
    setMarkReadError('');
    const unreadOutbox = notifications.filter((n) => !n.read && !n.id.startsWith('notif-bonus-'));
    if (unreadOutbox.length > 0) {
      try {
        await customerNotificationsApi.markNotificationsRead();
      } catch {
        const message = 'خواندن همه اعلان‌ها انجام نشد؛ لطفاً دوباره تلاش کنید.';
        if (central) setMarkReadError(message);
        else error(message);
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

  // Tapping an unread row (or its "خوانده شد" button) marks just that
  // row read: server-side for outbox rows (best-effort — the row flips
  // locally even if the POST fails; the next open re-fetches the
  // server state), persisted locally for bonus rows.
  const handleItemClick = (item: NotificationItem) => {
    if (markOneCustomerNotificationRead(item, currentStore?.slug, (ids) => customerNotificationsApi.markNotificationsRead(ids)) === 'already-read') return;
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

  const content = (
    <>
      <div className={central ? 'shrink-0 flex items-center justify-between gap-3 border-b border-[var(--shop-line,#dde2d7)] bg-white px-4 py-3 sm:px-5' : 'p-4 border-b border-slate-100 flex items-center justify-between'}>
        <div className="flex items-center gap-2">
          <div className={central ? 'grid size-11 shrink-0 place-items-center rounded-xl bg-brand-soft text-brand-deep' : 'w-8 h-8 rounded-xl bg-brand-soft text-brand-deep flex items-center justify-center'}>
            <Bell aria-hidden="true" className={central ? "size-5" : "w-4 h-4"}  strokeWidth={1.5}/>
          </div>
          <div>
            <h3 id={titleId} className={central ? 'text-lg font-black text-[var(--shop-ink,#172d26)]' : 'text-xs font-bold text-slate-900'}>
              {central ? 'اعلان‌ها' : 'مرکز اعلان‌های سیستم'}
            </h3>
            <span className={central ? "block text-xs text-[var(--shop-muted,#4f6359)]" : "text-[10px] text-slate-400 block"}>
              {unreadCount > 0 ? `${central ? toPersianDigits(unreadCount) : unreadCount} اعلان خوانده‌نشده` : central ? 'وضعیت سفارش‌ها و پیام‌های فروشگاه' : 'تمام اعلان‌ها خوانده شده است'}
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {unreadCount > 0 && (
            <button
              type="button"
              onClick={() => void handleMarkAllRead()}
              className={central ? 'inline-flex min-h-11 items-center gap-1 rounded-lg px-2 text-xs font-bold text-brand-deep hover:bg-brand-soft' : 'text-[10px] text-brand-deep font-bold hover:underline flex items-center gap-1'}
            >
              <CheckCheck className="w-3.5 h-3.5"  strokeWidth={1.5}/>
              <span>خواندن همه</span>
            </button>
          )}
          <button
            type="button"
            onClick={onClose}
            aria-label="بستن اعلان‌ها"
            className={central ? 'grid size-11 shrink-0 place-items-center rounded-xl text-[var(--shop-muted,#4f6359)] hover:bg-brand-soft' : 'p-1 rounded-lg text-slate-400 hover:bg-slate-100'}
          >
            <X className="w-4 h-4"  strokeWidth={1.5}/>
          </button>
        </div>
      </div>

      <div className={central ? 'flex shrink-0 gap-1 overflow-x-auto border-b border-[var(--shop-line,#dde2d7)] bg-white px-3 py-2 sm:px-4' : 'px-4 pt-3 flex items-center gap-2 border-b border-slate-100'} role="group" aria-label="دسته‌بندی اعلان‌ها">
        {(['all', 'unread', 'read'] as NotificationTab[]).map((t) => {
          const active = tab === t;
          return (
            <button
              key={t}
              type="button"
              aria-pressed={active}
              aria-controls={tabsId}
              onClick={() => setTab(t)}
              className={central
                ? `min-h-11 shrink-0 rounded-xl px-3 text-xs font-bold transition-colors ${active ? 'bg-brand-soft text-brand-deep' : 'text-[var(--shop-muted,#4f6359)] hover:bg-brand-soft/60'}`
                : `pb-2 text-[11px] font-bold border-b-2 transition-colors ${active ? 'text-brand-deep border-brand-deep' : 'text-slate-400 border-transparent hover:text-slate-600'}`}
            >
              {`${NOTIFICATION_TAB_LABELS[t]} ${toPersianDigits(tabCounts[t])}`}
            </button>
          );
        })}
      </div>

      <div id={tabsId} aria-busy={outboxLoading} className={central ? 'min-h-0 flex-1 overflow-y-auto overscroll-contain space-y-2.5 px-4 py-4 sm:px-5' : 'flex-1 overflow-y-auto p-3.5 space-y-2.5'}>
        {markReadError && <p role="alert" className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-sm font-bold text-rose-800">{markReadError}</p>}
        {outboxError && (
          <div className="rounded-2xl border border-rose-200 bg-rose-50 p-3 text-center">
            <p role="alert" className="text-sm font-bold text-rose-700">{outboxError}</p>
            {central && <button type="button" onClick={() => setRetry(current => current + 1)} className="mt-2 min-h-11 rounded-xl border border-rose-200 bg-white px-3 text-xs font-bold text-rose-800 hover:bg-rose-100">تلاش مجدد</button>}
          </div>
        )}
        {central && outboxLoading && !notifications.length ? (
          <p role="status" className="rounded-2xl border border-[var(--shop-line,#dde2d7)] bg-white p-6 text-center text-sm text-[var(--shop-muted,#4f6359)]">در حال دریافت اعلان‌ها…</p>
        ) : notifications.length === 0 && !outboxError ? (
          <div className={central ? 'space-y-2 rounded-2xl border border-dashed border-[var(--shop-line,#dde2d7)] bg-white px-4 py-7 text-center' : 'rounded-2xl border border-slate-200 bg-white p-6 text-center space-y-1.5'}>
            {central && <span className="mx-auto grid size-12 place-items-center rounded-2xl bg-brand-soft text-brand-deep"><BellOff size={22} aria-hidden="true" /></span>}
            <p className={central ? 'text-sm font-bold text-[var(--shop-ink,#172d26)]' : 'text-xs font-bold text-slate-600'}>اعلان جدیدی نیست.</p>
            <p className={central ? 'text-xs leading-6 text-[var(--shop-muted,#4f6359)]' : 'text-[11px] leading-5 text-slate-400'}>بسته‌بندی و ارسال سفارش‌های شما اینجا نمایش داده می‌شود.</p>
          </div>
        ) : visibleNotifications.length === 0 && !outboxError ? (
          <div className="rounded-2xl border border-slate-200 bg-white p-6 text-center space-y-1.5">
            <p className="text-xs font-bold text-slate-600">
              {tab === 'unread' ? 'اعلان خوانده‌نشده‌ای نیست.' : 'اعلان خوانده‌شده‌ای نیست.'}
            </p>
          </div>
        ) : (
          visibleNotifications.map((item) => (
            <div
              key={item.id}
              onClick={() => handleItemClick(item)}
              className={`p-3 rounded-2xl border transition-colors text-xs space-y-1 ${
                item.read
                  ? 'bg-white border-slate-200/80 text-slate-600'
                  : 'bg-brand-soft/40 border-brand-border text-slate-900 ring-1 ring-brand-border cursor-pointer'
              }`}
            >
              <div className="flex flex-wrap items-center justify-between gap-1">
                <div className="flex min-w-0 items-center gap-1.5 font-bold">
                  {getIcon(item.type)}
                  <span>{item.title}</span>
                </div>
                <span className="text-[10px] text-slate-400 font-mono">{item.time}</span>
              </div>
              <p className="text-[11px] leading-relaxed text-slate-600 pr-5">
                {item.message}
              </p>
              {!item.read && (
                <div className="pt-1 pr-5">
                  <button
                    type="button"
                    aria-label={`خوانده شد: ${item.title}`}
                    onClick={(e) => { e.stopPropagation(); handleItemClick(item); }}
                    className={central ? 'inline-flex min-h-11 items-center gap-1 rounded-lg border border-[var(--shop-line,#dde2d7)] bg-white px-3 text-xs font-bold text-brand-deep hover:bg-brand-soft' : 'inline-flex items-center gap-1 px-2.5 py-1 min-h-[28px] rounded-lg bg-white border border-slate-200 text-[10px] font-bold text-slate-600 hover:bg-slate-50'}
                  >
                    <Check className="w-3.5 h-3.5" strokeWidth={2} />
                    <span>خوانده شد</span>
                  </button>
                </div>
              )}
            </div>
          ))
        )}
      </div>

      {!central && <div className="p-3 border-t border-slate-100 bg-slate-50">
        <button
          onClick={onClose}
          className="w-full py-2.5 bg-slate-900 hover:bg-slate-800 text-white font-bold text-xs rounded-xl shadow-xs transition-colors"
        >
          بستن مرکز اعلان‌ها
        </button>
      </div>}
    </>
  );

  if (central) {
    return createPortal(
      <dialog
        ref={dialogRef}
        dir="rtl"
        aria-labelledby={titleId}
        onCancel={event => { event.preventDefault(); onClose(); }}
        onClick={event => {
          if (event.target !== event.currentTarget) return;
          const rect = event.currentTarget.getBoundingClientRect();
          if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) onClose();
        }}
        className="central-portal fixed inset-0 mx-auto mb-0 mt-auto w-full max-w-full overflow-hidden rounded-t-[var(--shop-radius-lg,24px)] border border-[var(--shop-line,#dde2d7)] bg-[var(--shop-bg,#f8f6ef)] p-0 text-[var(--shop-ink,#172d26)] shadow-2xl backdrop:bg-[#10271f]/55 sm:m-auto sm:w-[calc(100%-2rem)] sm:max-w-[35rem] sm:rounded-[var(--shop-radius-lg,24px)] [--color-brand:#164638] [--color-brand-deep:var(--shop-forest,#164638)] [--color-brand-soft:var(--shop-soft,#edf2e9)] [--color-brand-border:var(--shop-line,#dde2d7)] [&_:is(button,a):focus-visible]:outline-2 [&_:is(button,a):focus-visible]:outline-offset-2 [&_:is(button,a):focus-visible]:outline-[var(--shop-focus,#98601c)]"
      >
        <div className="flex max-h-[min(86dvh,43rem)] min-h-[min(22rem,86dvh)] flex-col pb-[env(safe-area-inset-bottom)]">
          {content}
        </div>
      </dialog>,
      document.body,
    );
  }

  return createPortal(
    <div className="fixed inset-0 z-[100] overflow-hidden bg-slate-900/60 backdrop-blur-xs flex justify-center">
      <motion.div
        initial={{ y: '100%' }}
        animate={{ y: 0 }}
        exit={{ y: '100%' }}
        transition={{ type: 'spring', damping: 28, stiffness: 240 }}
        className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between"
      >
        {content}
      </motion.div>
    </div>,
    document.body,
  );
};
