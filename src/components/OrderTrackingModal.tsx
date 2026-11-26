import React, { useEffect, useId, useRef, useState } from 'react';
import { AlertCircle, CheckCircle2, PackageSearch, Search, Truck, X } from 'lucide-react';
import { useAuth } from '../context/AuthContext';
import { followStoreLink } from '../utils/followStoreLink';
import { pathForView } from '../utils/routing';
import { activeStoreSlugOf, useStore } from '../context/StoreContext';
import { useOrders } from '../context/OrderContext';
import { maskName, type ViewerRole } from '../utils/privacyMasker';
import { orderApi, type OrderShippingInfo } from '../services/orderApi';
import { ReturnWindowBadge } from './ReturnWindowBadge';
import { TrackingTimeline } from './TrackingTimeline';

interface OrderTrackingModalProps {
  viewerRole?: ViewerRole;
  central?: boolean;
}

export const OrderTrackingModal: React.FC<OrderTrackingModalProps> = ({ viewerRole, central = false }) => {
  const {
    isTrackingModalOpen, setIsTrackingModalOpen, orders, trackingOrder, setTrackingOrder,
    confirmOrderDelivery, disputeOrderDelivery, busyOrderId, activeView, currentStore, routeStoreSlug, setActiveView,
    formatPrice, toPersianDigits,
  } = useStore();
  const { fetchMyOrders } = useOrders();
  const { isAuthenticated } = useAuth();
  const loadOrdersRef = useRef(fetchMyOrders);
  loadOrdersRef.current = fetchMyOrders;
  const dialog = useRef<HTMLDialogElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const titleId = useId();
  const helpId = useId();
  const inputId = useId();
  const [inputCode, setInputCode] = useState('');
  const [ordersLoading, setOrdersLoading] = useState(false);
  const [searchError, setSearchError] = useState(false);
  const [feedback, setFeedback] = useState<{ error: boolean; text: string } | null>(null);
  const [shipment, setShipment] = useState<{ orderId: string; info: OrderShippingInfo } | null>(null);
  const storeOrders = orders.filter(order => order.storeId === currentStore.id);
  const current = searchError ? null : trackingOrder?.storeId === currentStore.id ? trackingOrder : (storeOrders[0] ?? null);
  const currentOrderId = current?.id;
  const shippingInfo = shipment && shipment.orderId === currentOrderId ? shipment.info : null;
  const effectiveRole = viewerRole ?? (activeView === 'seller_dashboard' ? 'seller' : 'customer');
  const isMaskedForViewer = effectiveRole === 'seller' || effectiveRole === 'public';

  useEffect(() => {
    if (!isTrackingModalOpen) return;
    setInputCode('');
    setSearchError(false);
    setFeedback(null);
    const panel = dialog.current;
    returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    panel?.showModal();
    return () => { if (panel?.open) panel.close(); };
  }, [isTrackingModalOpen, currentStore.id]);

  useEffect(() => {
    if (!isTrackingModalOpen || !isAuthenticated) {
      setOrdersLoading(false);
      return;
    }
    let active = true;
    setOrdersLoading(true);
    void loadOrdersRef.current().finally(() => { if (active) setOrdersLoading(false); });
    return () => { active = false; };
  }, [isTrackingModalOpen, isAuthenticated]);

  useEffect(() => {
    if (!isTrackingModalOpen || !currentOrderId) return;
    let active = true;
    setShipment(null);
    orderApi.getOrderShipping(currentOrderId).then(info => {
      if (active && info) setShipment({ orderId: currentOrderId, info });
    }).catch(() => {});
    return () => { active = false; };
  }, [isTrackingModalOpen, currentOrderId]);

  if (!isTrackingModalOpen) return null;

  const close = () => {
    if (dialog.current?.open) dialog.current.close();
    returnFocus.current?.focus({ preventScroll: true });
    setIsTrackingModalOpen(false);
  };
  const search = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const normalized = inputCode.trim().toLowerCase();
    if (!normalized || ordersLoading) return;
    const found = storeOrders.find(order => order.trackingCode?.toLowerCase() === normalized);
    setSearchError(!found);
    setFeedback(null);
    if (found) setTrackingOrder(found);
  };
  const recipient = current
    ? [isMaskedForViewer ? maskName(current.customerInfo.fullName) : current.customerInfo.fullName,
       isMaskedForViewer ? '' : current.customerInfo.province, current.customerInfo.city].filter(Boolean).join('، ')
    : '';
  const canReportDelivery = effectiveRole === 'customer' && (current?.status === 'shipped' || current?.status === 'delivered_unconfirmed');

  return (
    <dialog
      ref={dialog}
      dir="rtl"
      className={`tracking-dialog ${central ? 'central-portal' : ''}`}
      aria-labelledby={titleId}
      onCancel={event => { event.preventDefault(); close(); }}
      onClick={event => { if (event.target === event.currentTarget) close(); }}
    >
      <div className="tracking-panel">
        <header className="tracking-header">
          <span className="tracking-icon"><Truck size={23} aria-hidden="true" /></span>
          <div>
            <h2 id={titleId}>پیگیری مرسوله</h2>
            <p>وضعیت ارسال و تحویل سفارش</p>
          </div>
          <button type="button" className="tracking-close" aria-label="بستن پنجرهٔ رهگیری" onClick={close}>
            <X size={19} aria-hidden="true" />
          </button>
        </header>

        <div className="tracking-content">
          <form className="tracking-search" onSubmit={search}>
            <label htmlFor={inputId}>کد رهگیری</label>
            <div className="tracking-search-row">
              <input id={inputId} name="tracking-code" type="text" dir="ltr" autoComplete="off" spellCheck={false}
                enterKeyHint="search" placeholder="TRK-123456" value={inputCode}
                aria-describedby={helpId} aria-invalid={searchError || undefined}
                onChange={event => { setInputCode(event.target.value); setSearchError(false); }} />
              <button type="submit" className="tracking-submit" disabled={!inputCode.trim() || ordersLoading}>
                <Search size={17} aria-hidden="true" /> رهگیری
              </button>
            </div>
            <p id={helpId}>کد درج‌شده در رسید سفارش این فروشگاه را وارد کنید.</p>
          </form>

          {searchError ? (
            <div className="tracking-notice tracking-notice--error" role="alert">
              <AlertCircle size={19} aria-hidden="true" />
              <p>سفارشی با این کد پیدا نشد. کد روی رسید را بررسی کنید.</p>
            </div>
          ) : current ? (
            <>
              <section className="tracking-card" aria-label="خلاصه سفارش">
                <dl className="tracking-facts">
                  <Fact label="کد رهگیری"><bdi>{current.trackingCode || 'هنوز صادر نشده'}</bdi></Fact>
                  <Fact label="فروشگاه">{current.storeName}</Fact>
                  <Fact label="گیرنده">{recipient}</Fact>
                  <Fact label="مبلغ سفارش">{formatPrice(current.payableAmount)} تومان</Fact>
                </dl>
              </section>

              <TrackingTimeline status={current.status} />

              {shippingInfo && (
                <section className="tracking-card" aria-label="مشخصات ارسال">
                  <h3>مشخصات ارسال</h3>
                  <dl className="tracking-facts">
                    {shippingInfo.carrier && <Fact label="شرکت حمل">{shippingInfo.carrier}</Fact>}
                    {shippingInfo.trackingNumber && <Fact label="شماره مرسوله"><bdi>{shippingInfo.trackingNumber}</bdi></Fact>}
                    {shippingInfo.boxSize && <Fact label="اندازه بسته">{shippingInfo.boxSize}</Fact>}
                    {shippingInfo.weightGrams != null && <Fact label="وزن">{toPersianDigits(shippingInfo.weightGrams)} گرم</Fact>}
                    {shippingInfo.packedAt && <Fact label="زمان بسته‌بندی">{shippingInfo.packedAt}</Fact>}
                    {shippingInfo.shippedAt && <Fact label="زمان ارسال">{shippingInfo.shippedAt}</Fact>}
                    <Fact label="مهلت مرجوعی"><ReturnWindowBadge returnWindowEndsAt={shippingInfo.returnWindowEndsAt} /></Fact>
                  </dl>
                </section>
              )}

              {feedback && <p className={`tracking-notice ${feedback.error ? 'tracking-notice--error' : ''}`}
                role={feedback.error ? 'alert' : 'status'}>{feedback.text}</p>}
              {canReportDelivery && (
                <section className="tracking-card">
                  <h3>آیا بسته را دریافت کرده‌اید؟</h3>
                  <div className="tracking-actions">
                    <button type="button" className="tracking-submit" disabled={busyOrderId === current.id} onClick={async () => {
                      try {
                        await confirmOrderDelivery(current.id);
                        setFeedback({ error: false, text: 'دریافت مرسوله ثبت شد.' });
                      } catch (error) { setFeedback({ error: true, text: error instanceof Error ? error.message : 'تأیید تحویل انجام نشد.' }); }
                    }}>
                      <CheckCircle2 size={17} aria-hidden="true" /> دریافت کردم
                    </button>
                    <button type="button" className="tracking-secondary" disabled={busyOrderId === current.id} onClick={async () => {
                      try {
                        await disputeOrderDelivery(current.id, 'خریدار اعلام کرد بسته پستی هنوز به دست ایشان نرسیده است.');
                        setFeedback({ error: false, text: 'گزارش عدم دریافت ثبت شد و در صف پیگیری پشتیبانی قرار گرفت.' });
                      } catch (error) { setFeedback({ error: true, text: error instanceof Error ? error.message : 'ثبت گزارش عدم دریافت انجام نشد.' }); }
                    }}>هنوز دریافت نکرده‌ام</button>
                  </div>
                </section>
              )}
            </>
          ) : ordersLoading ? (
            <p className="tracking-empty" role="status">در حال بارگذاری سفارش‌ها…</p>
          ) : (
            <div className="tracking-empty">
              <span className="tracking-icon"><PackageSearch size={23} aria-hidden="true" /></span>
              <strong>سفارشی برای نمایش در دسترس نیست</strong>
              <p>سفارش‌های قبلی را در حساب خریدتان ببینید.</p>
              <a className="tracking-secondary" href={pathForView('customer_orders', { storeSlug: activeStoreSlugOf({ routeStoreSlug, currentStore }) ?? undefined })}
                onClick={event => followStoreLink(event, () => { close(); setActiveView('customer_orders'); })}>سفارش‌های من</a>
            </div>
          )}
        </div>
      </div>
    </dialog>
  );
};

function Fact({ label, children }: React.PropsWithChildren<{ label: string }>) {
  return <div><dt>{label}</dt><dd>{children}</dd></div>;
}
