import React, { useState, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useStore, useStoreManager } from '../../context/StoreContext';
import { useToast } from '../../context/ToastContext';
import { storeApi } from '../../services/storeApi';
import { isMockMode } from '../../services/apiClient';
import { tokenStore } from '../../services/apiClient';
import { Bell, CheckCheck, X, ShieldAlert, Sparkles, Package, Truck, Wallet, CheckCircle2 } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';

interface NotificationItem {
  id: string;
  role: string;
  title: string;
  message: string;
  time: string;
  read: boolean;
  type: 'order' | 'finance' | 'warehouse' | 'system';
  actionType?: 'confirm_delivery';
  orderId?: string;
  orderNumber?: string;
}

const initialNotifications: NotificationItem[] = [];

export const NotificationDrawer: React.FC<{ isOpen: boolean; onClose: () => void }> = ({
  isOpen,
  onClose,
}) => {
  const { currentStore, confirmOrderDelivery } = useStore();
  const { setUnreadNotificationCount } = useStoreManager();
  const { success } = useToast();
  const [notifications, setNotifications] = useState<NotificationItem[]>(initialNotifications);

  // Fetch persistent released bonus notifications for current seller store.
  // ISO-D Worker 3: bonus items are scoped to the CURRENT store only. The
  // previous append-only merge kept the previous store's bonus items (and
  // their unread count) after a store switch, leaking one store's
  // notifications into another store's drawer/header bell. The bonus slice
  // below is therefore replaced (not merged) on every fetch, and cleared
  // when the current store is not a dropshipper store or the fetch fails.
  useEffect(() => {
    if (!isMockMode() && tokenStore.getToken() && currentStore?.ownerType === 'dropshipper' && currentStore.slug) {
      storeApi.getReferralNotifications(currentStore.slug)
        .then((notifs) => {
          if (Array.isArray(notifs)) {
            const bonusNotifItems: NotificationItem[] = notifs.map((n) => ({
              id: `notif-bonus-${n.id}`,
              role: 'dropshipper',
              title: 'پاداش معرفی آزاد شد',
              message: n.message || `هدیه ۱۰۰٬۰۰۰ تومانی برای فروشگاه «${n.referredStoreName}» قابل برداشت شد.`,
              time: n.releasedAt ? new Date(n.releasedAt).toLocaleTimeString('fa-IR', { hour: '2-digit', minute: '2-digit' }) : 'به‌تازگی',
              read: false,
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

  const handleMarkAllRead = () => {
    setNotifications(prev => prev.map(n => ({ ...n, read: true })));
  };

  const handleConfirmDeliveryAction = (item: NotificationItem) => {
    if (item.orderId || item.orderNumber) {
      confirmOrderDelivery(item.orderId || item.orderNumber!);
      setNotifications(prev => prev.map(n => n.id === item.id ? { ...n, read: true, message: 'تحویل کالا با موفقیت توسط شما تأیید شد و سود به فروشنده آزاد گردید.' } : n));
      success('تحویل سفارش تأیید گردید و آزادسازی مالی تکمیل شد.');
    }
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
                onClick={handleMarkAllRead}
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
          {notifications.map((item) => (
            <div
              key={item.id}
              className={`p-3 rounded-2xl border transition-all text-xs space-y-1 ${
                item.read 
                  ? 'bg-white border-slate-200/80 text-slate-600' 
                  : 'bg-brand-soft/40 border-brand-border text-slate-900 ring-1 ring-brand-border'
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

              {/* Item 5: Direct Action for Confirm Delivery */}
              {item.actionType === 'confirm_delivery' && !item.read && (
                <div className="pt-2 pr-5">
                  <button
                    onClick={() => handleConfirmDeliveryAction(item)}
                    className="w-full py-1.5 px-3 bg-emerald-600 hover:bg-emerald-700 text-white rounded-xl font-bold text-xs flex items-center justify-center gap-1.5 shadow-2xs transition-colors"
                  >
                    <CheckCircle2 className="w-3.5 h-3.5"  strokeWidth={1.5}/>
                    <span>تأیید دریافت کالا (ترخیص نهایی)</span>
                  </button>
                </div>
              )}
            </div>
          ))}
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
