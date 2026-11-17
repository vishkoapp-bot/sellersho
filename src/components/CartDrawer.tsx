import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../context/StoreContext';
import { useOrders } from '../context/OrderContext';
import type { DirectBuy } from '../context/StoreContext';
import { useToast } from '../context/ToastContext';
import { CartItemList } from './CartItemList';
import { CartCheckoutForm } from './CartCheckoutForm';
import { CheckoutSummaryColumn } from './CheckoutSummaryColumn';
import { CartOrderSuccess } from './CartOrderSuccess';
import { AuthModal } from './auth/AuthModal';
import { Order } from '../types';
import type { CartItem } from '../types';
import { storeApi } from '../services/storeApi';
import { shippingApi, ShippingQuote } from '../services/shippingApi';
import { isValidIranianMobile, isValidIranianPostalCode } from '../utils/validators';
import { X, ShoppingBag, ArrowLeft, ArrowRight } from 'lucide-react';
import { getCartSupplierGroups } from '../utils/cartGrouping';
import { mapShaparakSimulatorError } from '../services/paymentApi';
import { motion, AnimatePresence } from 'motion/react';

export const CartDrawer: React.FC = () => {
  const { 
    isCartOpen, 
    setIsCartOpen, 
    cart, 
    cartTotal, 
    cartDiscount, 
    cartPayable, 
    placeOrder,
    directBuy,
    directBuyAddress,
    clearDirectBuy, 
    customerUser,
    setCustomerUser,
    formatPrice,
    currentStore,
    toPersianDigits 
  } = useStore();

  const {checkoutAttempt,recoverCheckout,startNewCheckout,isCreatingOrder}=useOrders();
  const {setActiveView}=useStore();
  const [checkoutStep, setCheckoutStep] = useState<'cart' | 'checkout' | 'success'>('cart');
  const [paymentMethod, setPaymentMethod] = useState<'cash'>('cash');
  const [isAuthOpen, setIsAuthOpen] = useState(false);
  const [customerInfo, setCustomerInfo] = useState({
    fullName: customerUser.fullName || '',
    phone: customerUser.mobile || '',
    province: '',
    city: '',
    address: '',
    postalCode: '',
  });
  const [placedOrder, setPlacedOrder] = useState<Order | null>(null);
  const [isSubmittingOrder, setIsSubmittingOrder] = useState(false);
  // FIX 5 (Team 03 G1/G2): live pre-checkout shipping quote states —
  // amount / free / warning / error — fetched with the live 450ms
  // debounce once the checkout step is active.
  const [shippingQuote, setShippingQuote] = useState<ShippingQuote | null>(null);
  const [shippingQuoteState, setShippingQuoteState] = useState<'idle' | 'loading' | 'quoted' | 'free' | 'warning' | 'error'>('idle');
  const [shippingQuoteMessage, setShippingQuoteMessage] = useState('');
  // P1-3: bumping this re-runs the quote effect (it is a dep) so a
  // failed quote can be retried without remounting or clearing the form.
  const [quoteNonce, setQuoteNonce] = useState(0);
  const retryShippingQuote = () => {
    setShippingQuoteMessage('');
    setShippingQuoteState('loading');
    setQuoteNonce((n) => n + 1);
  };
  const { error: toastError, success: toastSuccess } = useToast();
  const cartAddonsTotal = cart.reduce((sum, item) => sum + (item.product.addons && item.selectedAddonIds ? item.selectedAddonIds.reduce((s, id) => s + (item.product.addons!.find((a) => a.id === id)?.feePerUnit || 0), 0) : 0) * item.quantity, 0);
  const cartGrossDiscount = cartDiscount + cartAddonsTotal;
  // Worker A (buy-now): live drawer (wC) renders direct-buy lines
  // instead of the cart and forces the checkout step (X = _ ?
  // 'checkout' : V). Lines follow live eC(): the product carries
  // the addon snapshot only when addons are selected.
  const directLine: CartItem | null = directBuy
    ? {
        product: directBuy.addonIds.length > 0 ? { ...directBuy.product, addons: directBuy.addonSnapshot } : directBuy.product,
        quantity: directBuy.qty,
        selectedVariantId: directBuy.variantId,
        selectedAddonIds: directBuy.addonIds.length > 0 ? directBuy.addonIds : undefined,
        storeId: directBuy.store.id,
        storeName: directBuy.store.name,
      }
    : null;
  const isDirectBuy = directBuy !== null && directLine !== null;
  const displayItems: CartItem[] = isDirectBuy && directLine ? [directLine] : cart;
  const effectiveStep = isDirectBuy ? 'checkout' : checkoutStep;
  const directAddonTotal = directLine && directLine.selectedAddonIds && directLine.product.addons
    ? directLine.selectedAddonIds.reduce((s, id) => s + (directLine.product.addons!.find((a) => a.id === id)?.feePerUnit || 0), 0) * directLine.quantity
    : 0;
  // Live tC(): subtotal = original-based total + addons, payable =
  // price-based total + addons, discount = max(0, subtotal - payable).
  const directSubtotal = directLine ? (directLine.product.originalPrice || directLine.product.price) * directLine.quantity + directAddonTotal : 0;
  const directPayable = directLine ? directLine.product.price * directLine.quantity + directAddonTotal : 0;
  const directDiscount = Math.max(0, directSubtotal - directPayable);
  const displayGroups = getCartSupplierGroups(displayItems);
  const displayStoreName = isDirectBuy && directLine ? directLine.storeName : undefined;
  // P1-3: the gate keys on a valid quoteId for cart AND direct buy —
  // a direct order without a quote used to be created with shipping 0.
  const hasShippingQuote = shippingQuoteState === 'quoted' || shippingQuoteState === 'free';
  const shippingTotal = hasShippingQuote ? (shippingQuote?.totalShipping ?? 0) : 0;
  const finalPayable = (isDirectBuy ? directPayable : cartPayable) + shippingTotal;
  const cartSignature = cart.map((item) => `${item.product.id}::${item.selectedVariantId ?? ''}x${item.quantity}`).join(',');
  // Worker A: live shipping slot (drawer wC: wt = ev legs widget).
  // The per-leg detail line reuses this drawer's established leg
  // format; live's inner fb leg-detail internals are unproven beyond
  // the header/fare/items structure ported here.
  const shippingSlot = hasShippingQuote && shippingQuote ? (
    <div className="space-y-2">
      {shippingQuote.legs.map((leg) => {
        const legItems = (shippingQuote.allocations || [])
          .filter((allocation) => allocation.legIndex === leg.legIndex)
          .map((allocation) => {
            const line = displayItems.find((item) => item.product.id === allocation.productId && (!allocation.variantId || item.selectedVariantId === allocation.variantId));
            const variant = line && line.selectedVariantId ? (line.product.variants ?? []).find((v) => v.id === line.selectedVariantId) : undefined;
            return {
              name: line ? (variant ? `${line.product.title} (${variant.variantName || variant.name})` : line.product.title) : allocation.productId,
              quantity: allocation.quantity,
            };
          });
        return (
          <div key={leg.legIndex} className="space-y-1">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[12px] font-bold text-slate-700">
                محموله {toPersianDigits(leg.legIndex + 1)} · از {leg.originLabel}
                {leg.estimated && (
                  <span className="mr-1.5 rounded-full bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium text-amber-600 ring-1 ring-amber-200">برآوردی</span>
                )}
              </span>
              <span className="whitespace-nowrap font-medium text-slate-600">{leg.fare > 0 ? `${formatPrice(leg.fare)} تومان` : 'رایگان'}</span>
            </div>
            <p className="text-[12px] text-slate-500">{toPersianDigits(leg.weightGrams)} گرم، {leg.boxLabel}</p>
            <ul className="mt-1.5 space-y-1">
              {legItems.map((legItem, legItemIndex) => (
                <li key={`${leg.legIndex}-${legItem.name}-${legItemIndex}`} className="flex items-center justify-between gap-2 text-[12px] text-slate-500">
                  <span className="truncate">{legItem.name}</span>
                  <span className="shrink-0 font-medium">× {toPersianDigits(legItem.quantity)}</span>
                </li>
              ))}
            </ul>
          </div>
        );
      })}
    </div>
  ) : effectiveStep === 'checkout' && shippingQuoteState === 'error' ? (
    // P1-3: the slot must not fall back to the "next step" copy when the
    // quote FAILED — both the desktop side column and the mobile form
    // render the misleading fallback when shippingSlot is undefined.
    <div className="space-y-2">
      <div className="flex items-center gap-1.5 text-amber-700 font-bold text-xs">
        <span>هزینه ارسال محاسبه نشد — ثبت سفارش ممکن نیست</span>
      </div>
      <p className="text-[11px] text-slate-500 leading-5">برای ثبت سفارش، هزینه ارسال باید محاسبه شود. لطفاً دوباره تلاش کنید.</p>
      <button
        type="button"
        onClick={retryShippingQuote}
        className="rounded-full bg-amber-100 px-3 py-1.5 text-[11px] font-bold text-amber-800 hover:bg-amber-200 transition-colors"
      >
        تلاش دوباره
      </button>
    </div>
  ) : undefined;
  // Worker A: the footer breakdown (live tt()) shared by the footer
  // (mobile) and the desktop side column. The per-leg rows moved
  // into shippingSlot, like live — they are not part of tt().
  const pricingBreakdown = (
    <>
      {displayGroups.map((group, index) => (
        <div key={group.key} className="flex justify-between text-slate-500 gap-3">
          <span className="truncate">بسته {toPersianDigits(index + 1)}:</span>
          <span className="shrink-0">{formatPrice(group.subtotal)} تومان</span>
        </div>
      ))}
      <div className="flex justify-between text-slate-500 border-t border-slate-200 pt-1.5">
        <span>قیمت کالاها:</span>
        <span>{formatPrice(isDirectBuy ? directSubtotal : cartTotal)} تومان</span>
      </div>
      {(isDirectBuy ? directAddonTotal : cartAddonsTotal) > 0 && (
        <div className="flex justify-between text-slate-500">
          <span>محصولات مکمل:</span>
          <span>{formatPrice(isDirectBuy ? directAddonTotal : cartAddonsTotal)} تومان</span>
        </div>
      )}
      {(isDirectBuy ? directDiscount : cartGrossDiscount) > 0 && (
        <div className="flex justify-between text-rose-600 font-bold">
          <span>تخفیف ویژه:</span>
          <span><bdi>−{formatPrice(isDirectBuy ? directDiscount : cartGrossDiscount)}</bdi> تومان</span>
        </div>
      )}
      <div className="flex justify-between text-slate-500">
        <span>هزینه ارسال سفارش:</span>
        <span className="font-bold">
          {effectiveStep === 'cart' && 'محاسبه در مرحله بعد'}
          {effectiveStep === 'checkout' && shippingQuoteState === 'loading' && 'در حال محاسبه…'}
          {effectiveStep === 'checkout' && shippingQuoteState === 'quoted' && `${formatPrice(shippingQuote?.totalShipping ?? 0)} تومان`}
          {effectiveStep === 'checkout' && shippingQuoteState === 'free' && 'رایگان'}
          {effectiveStep === 'checkout' && (shippingQuoteState === 'warning' || shippingQuoteState === 'idle') && 'محاسبه در مرحله بعد'}
          {effectiveStep === 'checkout' && shippingQuoteState === 'error' && 'محاسبه نشد — ثبت سفارش ممکن نیست'}
        </span>
      </div>
      <div className="flex justify-between text-slate-900 font-black text-sm pt-2 border-t border-slate-200">
        <span>{hasShippingQuote ? 'مبلغ قابل پرداخت:' : 'مبلغ قابل پرداخت (هزینه ارسال محاسبه نشده):'}</span>
        <span className="text-brand-deep">{formatPrice(finalPayable)} تومان</span>
      </div>
    </>
  );

  // Live quote flow: with a complete destination, quote the cart (or
  // the single direct-buy line, from the same customerInfo the direct
  // prefill effect fills from directBuyAddress); without one, warn
  // instead of quoting a guessed zone. P1-3: direct buy quotes its own
  // line — a direct order must never be created without a quoteId.
  // The 30129 weight_formula quote IS valid: the gate below keys on
  // quoteId, never on the amount.
  const directQuoteSignature = isDirectBuy && directLine
    ? `direct:${directLine.product.id}::${directLine.selectedVariantId ?? ''}x${directLine.quantity}`
    : '';
  useEffect(() => {
    if (effectiveStep !== 'checkout') return;
    const quoteItems = isDirectBuy && directLine
      ? [{ productId: directLine.product.id, variantId: directLine.selectedVariantId, quantity: directLine.quantity }]
      : cart.map((item) => ({ productId: item.product.id, variantId: item.selectedVariantId, quantity: item.quantity }));
    if (quoteItems.length === 0) return;
    if (!customerInfo.province || !customerInfo.city || !customerInfo.address) {
      setShippingQuote(null);
      setShippingQuoteState('warning');
      setShippingQuoteMessage('برای محاسبه هزینه ارسال، نشانی کامل (استان، شهر و خیابان) را وارد کنید.');
      return;
    }
    setShippingQuoteState('loading');
    setShippingQuoteMessage('');
    const timer = setTimeout(async () => {
      try {
        const q = await shippingApi.quote(
          quoteItems,
          { province: customerInfo.province, city: customerInfo.city, address: customerInfo.address, postalCode: customerInfo.postalCode },
        );
        setShippingQuote(q);
        setShippingQuoteState(q.totalShipping > 0 ? 'quoted' : 'free');
      } catch (e: any) {
        const code = e?.body?.error || '';
        setShippingQuote(null);
        if (code === 'shipping_address_required') {
          setShippingQuoteState('warning');
          setShippingQuoteMessage('برای محاسبه هزینه ارسال، نشانی کامل (استان، شهر و خیابان) را وارد کنید.');
        } else if (code === 'shipping_quote_changed') {
          setShippingQuoteState('error');
          setShippingQuoteMessage('هزینه ارسال تغییر کرده است؛ لطفاً هزینه به‌روز را بررسی و دوباره تلاش کنید.');
        } else if (code === 'shipping_unavailable') {
          setShippingQuoteState('error');
          setShippingQuoteMessage('قیمت ارسال در دسترس نیست؛ لطفاً چند لحظه دیگر دوباره تلاش کنید.');
        } else {
          setShippingQuoteState('error');
          setShippingQuoteMessage('محاسبه هزینه ارسال انجام نشد. لطفاً دوباره تلاش کنید.');
        }
      }
    }, 450);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [checkoutStep, cartSignature, directQuoteSignature, customerInfo.province, customerInfo.city, customerInfo.address, customerInfo.postalCode, isDirectBuy, directBuyAddress?.id, quoteNonce, customerUser.isLoggedIn]);

  // Worker A: live drawer prefills the checkout form from the chosen
  // direct-buy address exactly once per direct-buy payload (sC).
  const directBuyRef = useRef<DirectBuy | null>(null);
  useEffect(() => {
    if (!directBuy || !directBuyAddress || directBuyRef.current === directBuy) return;
    directBuyRef.current = directBuy;
    setCustomerInfo((prev) => ({...prev,
      fullName: directBuyAddress.receiverFullName,
      phone: customerUser.mobile || directBuyAddress.receiverPhone || prev.phone,
      province: directBuyAddress.provinceName,
      city: directBuyAddress.cityName,
      address: directBuyAddress.addressDetail,
      postalCode: directBuyAddress.postalCode || prev.postalCode,
    }));
  }, [directBuy, directBuyAddress, customerUser.mobile]);
  // Worker A: a stale cart quote must never leak into a direct checkout.
  useEffect(() => {
    if (isDirectBuy) {
      setShippingQuote(null);
      setShippingQuoteState('idle');
      setShippingQuoteMessage('');
    }
  }, [isDirectBuy]);

  if (!isCartOpen) return null;

  const handleProceedToCheckout = () => {
    // Enforce Chapters 3 & 7: Check OTP authentication before checkout
    if (!customerUser.isLoggedIn) {
      setIsAuthOpen(true);
      return;
    }
    setCheckoutStep('checkout');
  };

  const handleCheckoutSubmit = async () => {
    if (!customerUser.isLoggedIn) {
      setIsAuthOpen(true);
      return;
    }
    if (!customerInfo.fullName || customerInfo.fullName.trim().length < 3) {
      toastError('لطفاً نام و نام خانوادگی تحویل‌گیرنده را وارد کنید.');
      return;
    }
    if (!isValidIranianMobile(customerInfo.phone)) {
      toastError('شماره موبایل خریدار نامعتبر است (مثال: ۰۹۱۲۳۴۵۶۷۸۹).');
      return;
    }
    if (!customerInfo.province || !customerInfo.city || !customerInfo.address) {
      toastError('لطفاً اطلاعات کامل آدرس پستی را تکمیل کنید.');
      return;
    }
    if (!isValidIranianPostalCode(customerInfo.postalCode)) {
      toastError('کد پستی ۱۰ رقمی وارد شده نامعتبر است.');
      return;
    }
    // P1-3: never submit without a valid shipping quote (quoted/free).
    // The gate keys on the quoteId, never on the amount — a 30129
    // weight_formula quote (or a free quote) passes. A blocked submit
    // creates no order, clears nothing, and keeps the form intact.
    if (shippingQuoteState !== 'quoted' && shippingQuoteState !== 'free') {
      toastError(
        shippingQuoteState === 'loading'
          ? 'هزینه ارسال در حال محاسبه است؛ لطفاً چند لحظه صبر کنید.'
          : shippingQuoteState === 'warning'
            ? 'برای محاسبه هزینه ارسال، نشانی کامل را وارد کنید.'
            : 'هزینه ارسال محاسبه نشد. لطفاً دوباره تلاش کنید.',
      );
      return;
    }

    setIsSubmittingOrder(true);
    try {
      if (isDirectBuy) {
        // Live direct submit (wC ve()): the chosen saved address is
        // required; without it the user must redo خرید فوری.
        if (!directBuy || !directBuyAddress || !directLine) {
          toastError('آدرس خرید فوری یافت نشد؛ لطفاً دوباره «خرید فوری» را بزنید.');
          setIsSubmittingOrder(false);
          return;
        }
        const order = await placeOrder(customerInfo, paymentMethod, undefined, shippingQuote?.quoteId, { lines: [directLine], store: directBuy.store }, { addressId: directBuyAddress.id });
        // Live clears only the direct-buy state here, never the cart.
        clearDirectBuy();
        setPlacedOrder(order);
        setCheckoutStep('success');
        toastSuccess('سفارش شما با موفقیت ثبت شد. پرداخت آنلاین به‌زودی فعال می‌شود؛ وضعیت سفارش: در انتظار پرداخت.');
      } else {
        const order = await placeOrder(customerInfo, paymentMethod, undefined, hasShippingQuote ? shippingQuote?.quoteId : undefined);
      setPlacedOrder(order);
      setCheckoutStep('success');
      toastSuccess('سفارش شما با موفقیت ثبت شد. پرداخت آنلاین به‌زودی فعال می‌شود؛ وضعیت سفارش: در انتظار پرداخت.');
      }
    } catch (err: any) {
      // Team 08 (D75): fail-closed simulator errors map to the live
      // pending-payment message; an order is never presented as paid.
      toastError(mapShaparakSimulatorError(err?.message || '') || 'ثبت سفارش انجام نشد. لطفاً دوباره تلاش کنید.');
    } finally {
      setIsSubmittingOrder(false);
    }
  };

  const handleClose = () => {
    if (isDirectBuy) clearDirectBuy();
    setIsCartOpen(false);
    setShippingQuote(null);
    setShippingQuoteState('idle');
    setShippingQuoteMessage('');
    setTimeout(() => setCheckoutStep('cart'), 300);
  };

  return (
    <div className="fixed inset-0 z-50 overflow-hidden bg-slate-900/60 backdrop-blur-xs flex justify-center">
      <motion.div
        initial={{ y: '100%' }}
        animate={{ y: 0 }}
        exit={{ y: '100%' }}
        transition={{ type: 'spring', damping: 28, stiffness: 240 }}
        className="w-full max-w-[504px] bg-white h-full shadow-2xl flex flex-col justify-between lg:max-w-[880px] lg:h-auto lg:max-h-[90vh] lg:rounded-3xl lg:my-auto"
      >

        {/* Header */}
        <div className="p-4 border-b border-slate-100 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <ShoppingBag className="w-5 h-5 text-brand-deep"  strokeWidth={1.5}/>
            <h3 className="font-extrabold text-sm text-slate-800">
              {effectiveStep === 'cart' && 'سبد خرید شما'}
              {effectiveStep === 'cart' && currentStore?.name && (<span className="block text-[10px] font-normal text-slate-400 mt-0.5">{currentStore.name}</span>)}
              {effectiveStep === 'checkout' && 'ثبت آدرس و تسویه'}
              {effectiveStep === 'checkout' && isDirectBuy && displayStoreName && (<span className="block text-[10px] font-normal text-slate-400 mt-0.5">{displayStoreName.startsWith('فروشگاه') ? displayStoreName : `فروشگاه ${displayStoreName}`}</span>)}
              {effectiveStep === 'success' && 'رسید سفارش'}
            </h3>
          </div>
          <button
            onClick={handleClose}
            aria-label="بستن سبد خرید"
            className="w-11 h-11 rounded-full hover:bg-slate-100 text-slate-500 transition-colors flex items-center justify-center"
          >
            <X className="w-5 h-5"  strokeWidth={1.5}/>
          </button>
        </div>

        {checkoutAttempt && (
          <div role="status" className="mx-4 mt-3 rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-950 space-y-2">
            <p>{checkoutAttempt.state==='completed' ? 'سفارش قبلی ثبت شده است. برای خرید دوباره، «شروع خرید تازه» را بزنید.' : 'نتیجه تلاش خرید قبلی قابل بازیابی است؛ برای جلوگیری از سفارش تکراری، ابتدا آن را بررسی کنید.'}</p>
            <div className="flex flex-wrap gap-3">
              <button disabled={isCreatingOrder} className="font-bold underline disabled:opacity-50" onClick={async()=>{try{const order=await recoverCheckout();if(order){toastSuccess(`سفارش ${order.orderNumber} بازیابی شد.`);setIsCartOpen(false);setActiveView('customer_orders');}else{toastError('سفارش ثبت‌شده‌ای برای این تلاش پیدا نشد؛ همان اطلاعات را دوباره ارسال کنید یا خرید تازه را صریحاً شروع کنید.');}}catch(err:any){toastError(err.message);}}}>بازیابی سفارش قبلی</button>
              <button disabled={isCreatingOrder} className="font-bold underline disabled:opacity-50" onClick={async()=>{try{await startNewCheckout();setPlacedOrder(null);setCheckoutStep('cart');toastSuccess('خرید تازه آماده است؛ سفارش ثبت‌شده قبلی در سفارش‌های من باقی می‌ماند.');}catch(err:any){toastError(err.message);}}}>شروع خرید تازه</button>
            </div>
          </div>
        )}

        {/* Body Content */}
        <div className="flex-1 overflow-y-auto no-scrollbar p-4 space-y-4">
          {effectiveStep === 'cart' && <CartItemList />}
          {effectiveStep === 'checkout' && (
            <div className="lg:grid lg:grid-cols-[minmax(0,1fr)_320px] lg:gap-6">
            <div className="rounded-2xl border border-slate-200 bg-white p-3 space-y-2">
            <CartCheckoutForm
              paymentMethod={paymentMethod}
              setPaymentMethod={setPaymentMethod}
              customerInfo={customerInfo}
              setCustomerInfo={setCustomerInfo}
              shippingSlot={shippingSlot}
              addonsAmount={isDirectBuy ? directAddonTotal : cartAddonsTotal}
              hideSavedAddressSelector={isDirectBuy}
            />
            </div>
            <CheckoutSummaryColumn shippingSlot={shippingSlot}>{pricingBreakdown}</CheckoutSummaryColumn>
            </div>
          )}
          {effectiveStep === 'success' && placedOrder && (
            <CartOrderSuccess order={placedOrder} onClose={handleClose} />
          )}
        </div>

        {/* Footer (Pricing Breakdown & Next Action) */}
        {effectiveStep !== 'success' && displayItems.length > 0 && (
          <div className="p-4 bg-slate-50 border-t border-slate-100 space-y-3">
            <div className={effectiveStep === 'checkout' ? 'space-y-1.5 text-xs lg:hidden' : 'space-y-1.5 text-xs'}>
              {pricingBreakdown}
            </div>
            {effectiveStep === 'checkout' && shippingQuoteState === 'warning' && shippingQuoteMessage && (
              <p className="text-[11px] text-amber-700">{shippingQuoteMessage}</p>
            )}
            {effectiveStep === 'checkout' && shippingQuoteState === 'error' && shippingQuoteMessage && (
              <div className="flex items-center justify-between gap-2">
                <p className="text-[11px] text-amber-700">{shippingQuoteMessage}</p>
                <button
                  type="button"
                  onClick={retryShippingQuote}
                  className="shrink-0 rounded-full bg-amber-100 px-3 py-1.5 text-[11px] font-bold text-amber-800 hover:bg-amber-200 transition-colors"
                >
                  تلاش دوباره
                </button>
              </div>
            )}
            {effectiveStep === 'cart' ? (
              <button
                onClick={handleProceedToCheckout}
                className="w-full flex items-center justify-center gap-2 bg-brand hover:bg-brand-deep text-white font-extrabold text-xs md:text-sm lg:text-base py-3 lg:py-3.5 rounded-2xl shadow-md shadow-brand/25 transition-all"
              >
                <span>ادامه فرآیند خرید</span>
                <ArrowLeft className="w-4 h-4"  strokeWidth={1.5}/>
              </button>
            ) : (
              <div className="space-y-2">
              <div className="flex gap-2">
                <button
                  onClick={() => { if (isDirectBuy) clearDirectBuy(); setCheckoutStep('cart'); }}
                  className="px-3 py-3 rounded-2xl bg-white border border-slate-200 text-slate-700 hover:bg-slate-100 text-xs font-bold transition-colors"
                >
                  <ArrowRight className="w-4 h-4"  strokeWidth={1.5}/>
                </button>
                <button
                  onClick={handleCheckoutSubmit}
                  disabled={isSubmittingOrder || isCreatingOrder || (shippingQuoteState !== 'quoted' && shippingQuoteState !== 'free')}
                  className="flex-1 flex items-center justify-center gap-2 bg-brand hover:bg-brand-deep text-white font-extrabold text-xs md:text-sm lg:text-base py-3 lg:py-3.5 rounded-2xl shadow-md shadow-brand/25 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {/* TODO(U44): exact live final-button text beyond the quoted
                      stub texts is unverified. This label says "register
                      order" only and must never imply a completed payment.
                      The source text "پرداخت نهایی و ثبت سفارش" is NOT restored. */}
                  <span>{isSubmittingOrder ? 'در حال ثبت سفارش…' : 'ثبت سفارش'}</span>
                </button>
              </div>
              <p className="text-center text-[11px] text-slate-500 leading-relaxed">پرداخت آنلاین به‌زودی فعال می‌شود؛ پس از ثبت سفارش، نحوه پرداخت به شما اعلام خواهد شد.</p>
              </div>
            )}
          </div>
        )}

        {/* OTP Authentication Modal for Checkout */}
        <AuthModal
          isOpen={isAuthOpen}
          onClose={() => setIsAuthOpen(false)}
          onSuccess={(user) => {
            setCustomerUser((prev: any) => ({ ...prev, fullName: user.fullName || prev.fullName, mobile: user.mobile, isLoggedIn: user.role === 'customer' }));
            setCustomerInfo((prev: any) => ({ ...prev, fullName: user.fullName || prev.fullName, phone: user.mobile }));
            setCheckoutStep('checkout');
          }}
          disableAutoNavigate
        />
      </motion.div>
    </div>
  );
};
