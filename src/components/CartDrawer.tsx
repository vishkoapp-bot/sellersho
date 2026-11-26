import React, { useState, useEffect, useRef } from 'react';
import { useStore } from '../context/StoreContext';
import type { CustomerUser } from '../context/AuthContext';
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
import { shippingApi, QuoteSession, buildQuoteSignature, isQuoteChangedError } from '../services/shippingApi';
import type { QuoteSignatureInput } from '../services/shippingApi';
import { isValidIranianMobile, isValidIranianPostalCode } from '../utils/validators';
import { X, ShoppingBag, ArrowLeft, ArrowRight, Check, ChevronDown, Package } from 'lucide-react';
import { computeCartPricing } from '../utils/cartPricing';
import { isCheckoutFingerprintConflictError } from '../services/checkoutAttempt';
import { stageAddressSelection } from '../services/addressApi';
import { mapShaparakSimulatorError } from '../services/paymentApi';
import { motion, useReducedMotion } from 'motion/react';

// CHECKOUT-02 Q2 (abandon-and-reclaim): after a mismatch refresh the
// resubmit abandons the old attempt — which created no order, because
// the server rejected it with shipping_quote_changed — and submits
// with the new quoteId, so a fresh attempt is claimed and exactly one
// order is created. The CHECKOUT-01 fingerprint stays inclusive of
// shippingQuoteId, so a bare retry under the old attempt would throw;
// this must run as part of recovery, never as a plain resubmit.
//
// CHECKOUT-02 fix B2 (recover-before-abandon): if the reclaim submit
// creates the order but its response is lost, a retry must NOT abandon
// first — abandoning discards the completed attempt (the server
// preserves it; startNewCheckout drops the recovered order and clears
// the local record) and the fresh claim then creates a SECOND order.
// So the reclaim checks recovery BEFORE abandoning: a recovered order
// is returned and nothing is abandoned or submitted. Only a null
// recovery abandons + submits.
//
// CHECKOUT-02 fix S1: the same guarded reclaim is the route for a
// CHECKOUT-01 fingerprint-conflict submit — after an input change or
// a mode switch clears an acknowledged mismatch, the next submit
// claims under the old attempt's fingerprint and throws instead of
// going through abandon-and-reclaim. The drawer's catch routes that
// error here (recover-check first), so the guided flow survives the
// input change. It deliberately does NOT run when the attempt is
// already completed (checkoutAttempt.state === 'completed'): that is
// a known completed purchase, and the banner's explicit
// "شروع خرید تازه" stays the way to start a genuinely new one
// (G-13(c)).
// CHECKOUT-02 fix S2: the reclaim result carries whether the order
// came from recovery. A recovered order's payload necessarily differs
// from the just-attempted submit in the S1 (fingerprint-conflict)
// case — the conflict proves it — so the success copy must say the
// earlier order was recovered, not that the new submit succeeded.
// In the B2 (mismatch-resubmit) case the payload is identical, so
// the generic success copy stays accurate there.
export interface ReclaimResult {
  order: Order;
  recovered: boolean;
}

export async function guardedReclaimSubmit(args: {
  quoteId: string;
  abandonCheckout: () => Promise<void>;
  submitOrder: (quoteId: string) => Promise<Order>;
  recoverCheckout?: () => Promise<Order | null>;
}): Promise<ReclaimResult> {
  if (args.recoverCheckout) {
    const recovered = await args.recoverCheckout();
    if (recovered) return { order: recovered, recovered: true };
  }
  await args.abandonCheckout();
  const order = await args.submitOrder(args.quoteId);
  return { order, recovered: false };
}

export async function resubmitAfterMismatch(args: {
  session: QuoteSession;
  abandonCheckout: () => Promise<void>;
  submitOrder: (quoteId: string) => Promise<Order>;
  recoverCheckout?: () => Promise<Order | null>;
}): Promise<ReclaimResult> {
  const quoteId = args.session.getSubmittableQuoteId();
  if (!args.session.mismatch?.acknowledged || !quoteId) {
    throw new Error('هزینه ارسال به‌روز هنوز تأیید نشده است.');
  }
  return guardedReclaimSubmit({
    quoteId,
    abandonCheckout: args.abandonCheckout,
    submitOrder: args.submitOrder,
    recoverCheckout: args.recoverCheckout,
  });
}

// CHECKOUT-02 fix S2/S3: what happens after a guarded reclaim
// returns depends on WHERE it ran, so the two call sites say so
// explicitly instead of the success path guessing.
//  - B2 (mismatch resubmit, payload-identical): a recovered order
//    means the cart's items ARE purchased. The recover-short-circuit
//    bypasses the StoreContext placeOrder wrapper's cart clear, so
//    the drawer clears the cart here, like a normal success.
//  - S1 (fingerprint conflict): the recovered order was created
//    under the OLD payload, so the just-typed payload is still
//    unpurchased — the cart MUST stay, and the success copy (via
//    `recovered` passed to onPlaced) says the earlier order was
//    recovered instead of presenting it as a fresh-submit success.
export function applyReclaimOutcome(
  outcome: ReclaimResult,
  opts: {
    clearCartOnRecovery: boolean;
    clearCart: () => void;
    onPlaced: (order: Order, recovered: boolean) => void;
  },
): void {
  if (outcome.recovered && opts.clearCartOnRecovery) opts.clearCart();
  opts.onPlaced(outcome.order, outcome.recovered);
}

export const CartDrawer: React.FC = () => {
  const {
    isCartOpen,
    openCatalogCategory,
    setActiveView,
    setIsCartOpen,
    cart,
    clearCart,
    cartTotal,
    cartDiscount,
    cartPayable,
    cartAddonsTotal,
    placeOrder,
    directBuy,
    directBuyAddress,
    clearDirectBuy,
    customerUser,
    setCustomerUser,
    formatPrice,
    currentStore,
    routeStoreSlug,
    activeView,
    toPersianDigits
  } = useStore();

  const {checkoutAttempt,recoverCheckout,startNewCheckout,isCreatingOrder}=useOrders();
  const [checkoutStep, setCheckoutStep] = useState<'cart' | 'checkout' | 'success'>('cart');
  const paymentMethod = 'cash';
  const [isAuthOpen, setIsAuthOpen] = useState(false);
  const titleRef = useRef<HTMLHeadingElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const reducedMotion = useReducedMotion();
  useEffect(() => {
    if (!isCartOpen) return;
    const previousFocus = document.activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = previousOverflow;
      if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus({ preventScroll: true });
    };
  }, [isCartOpen]);
  const [customerInfo, setCustomerInfo] = useState({
    fullName: customerUser.fullName || '',
    phone: customerUser.mobile || '',
    province: '',
    city: '',
    address: '',
    postalCode: '',
  });
  const [receipt, setReceipt] = useState<{ order: Order; direct: boolean; recovered: boolean } | null>(null);
  const [isSubmittingOrder, setIsSubmittingOrder] = useState(false);
  // CHECKOUT-02: all quote sequencing lives in QuoteSession
  // (src/services/shippingApi.ts) — generation + AbortController,
  // signature, stale retention, mismatch review. The drawer only
  // mirrors its state (via quoteVersion) and binds UI to it, so the
  // gates hold in exactly one place.
  const quoteSessionRef = useRef<QuoteSession | null>(null);
  if (quoteSessionRef.current === null) {
    quoteSessionRef.current = new QuoteSession((items, destination, opts) => shippingApi.quote(items, destination, opts));
  }
  const quoteSession = quoteSessionRef.current;
  const [, setQuoteVersion] = useState(0);
  useEffect(() => quoteSession.subscribe(() => setQuoteVersion((v) => v + 1)), [quoteSession]);
  const shippingQuote = quoteSession.quote;
  const shippingQuoteState = quoteSession.status;
  const shippingQuoteMessage = quoteSession.message;
  const retryShippingQuote = () => {
    quoteSession.retry();
  };
  const { error: toastError, success: toastSuccess } = useToast();
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
  const isEmpty = displayItems.length === 0;
  // CHECKOUT-03: the direct-buy line uses the SAME canonical breakdown as
  // the cart (src/utils/cartPricing.ts). The old directSubtotal folded the
  // addons into the "قیمت کالاها" row while the breakdown rendered a second
  // addons row, so the rows double-counted the addons (150 + 50 = 200
  // against a 150 payable); the old cartGrossDiscount did the opposite,
  // inventing a discount equal to the addons. Both modes now render:
  // original - discount + addons + shipping = total.
  const directPricing = directLine ? computeCartPricing([directLine]) : null;
  const directAddonTotal = directPricing ? directPricing.addonsTotal : 0;
  const directOriginal = directPricing ? directPricing.merchandiseOriginal : 0;
  const directPayable = directPricing ? directPricing.goodsPayable : 0;
  const directDiscount = directPricing ? directPricing.merchandiseDiscount : 0;
  const displayStoreName = isDirectBuy ? directLine?.storeName : (cart[0]?.storeName ?? currentStore?.name);
  const centralCheckout = !routeStoreSlug && activeView !== 'seller_storefront' && currentStore?.ownerType !== 'dropshipper';
  const checkoutStyle = {
    '--checkout-accent': centralCheckout ? 'var(--shop-forest, #164638)' : 'var(--color-brand-deep)',
    '--checkout-soft': centralCheckout ? 'var(--shop-soft, #edf2e9)' : 'var(--color-brand-soft)',
    '--checkout-ink': centralCheckout ? 'var(--shop-ink, #172d26)' : '#1e293b',
    '--checkout-bg': centralCheckout ? 'var(--shop-bg, #f8f6ef)' : '#f8fafc',
    '--checkout-line': centralCheckout ? 'var(--shop-line, #dde2d7)' : 'var(--color-slate-200)',
    '--checkout-muted': centralCheckout ? 'var(--shop-muted, #4f6359)' : 'var(--color-slate-600)',
    '--checkout-radius': centralCheckout ? 'var(--shop-radius, 16px)' : '1rem',
  } as React.CSSProperties;
  useEffect(() => {
    if (!isCartOpen || isAuthOpen) return;
    bodyRef.current?.scrollTo({ top: 0 });
    titleRef.current?.focus({ preventScroll: true });
  }, [effectiveStep, isCartOpen, isAuthOpen, isEmpty]);
  // CHECKOUT-02 (Q5): the payable keeps the retained quote's charge
  // while a refetch is in flight or a refetch failed — marked stale
  // in the UI, never silently 0 and never silently current. Submit is
  // gated separately by quoteSession.canSubmit().
  const shippingTotal = shippingQuote ? shippingQuote.totalShipping : 0;
  const finalPayable = (isDirectBuy ? directPayable : cartPayable) + shippingTotal;
  // CHECKOUT-02 (G-09/Q4): one signature over destination, store,
  // lines (product/variant/quantity/store) and addon selection.
  // Addons are fee-only (Q3), so they bump the signature and force a
  // revalidation request without adding quote lines. Recipient name
  // and phone are not inputs: changing them issues no new request.
  const quoteStoreId = isDirectBuy && directLine
    ? directLine.storeId
    : (cart.length > 0 && cart.every((item) => item.storeId === cart[0].storeId) ? cart[0].storeId : currentStore?.id ?? undefined);
  const quoteInput: QuoteSignatureInput = {
    storeId: quoteStoreId,
    items: (isDirectBuy && directLine
      ? [directLine]
      : cart).map((item) => ({
        productId: item.product.id,
        variantId: item.selectedVariantId,
        quantity: item.quantity,
        storeId: item.storeId,
        addonIds: item.selectedAddonIds,
      })),
    destination: {
      province: customerInfo.province,
      city: customerInfo.city,
      address: customerInfo.address,
      postalCode: customerInfo.postalCode,
    },
  };
  const quoteSignature = buildQuoteSignature(quoteInput);
  const quoteMatchesForm = quoteSession.quoteSignature === quoteSignature;
  const isQuoteRefreshing = effectiveStep === 'checkout' && quoteSession.currentSignature !== quoteSignature;
  const isQuoteStale = effectiveStep === 'checkout' && !!shippingQuote && (quoteSession.displayStale || !quoteMatchesForm);
  const hasShippingQuote = effectiveStep === 'checkout' && (shippingQuoteState === 'quoted' || shippingQuoteState === 'free') && quoteMatchesForm;
  const canSubmitQuote = quoteMatchesForm && quoteSession.canSubmit();
  const canConfirmOrder = canSubmitQuote && !isEmpty;
  const orderBusy = isSubmittingOrder || isCreatingOrder;
  const goodsPayable = isDirectBuy ? directPayable : cartPayable;
  // CHECKOUT-02 (G-12/Q7): old-vs-new charge review block, shared by
  // the shipping slot and the footer. Submit stays blocked until the
  // acknowledgement control is pressed.
  const mismatch = isQuoteRefreshing ? null : quoteSession.mismatch;
  const mismatchBlock = mismatch ? (
    <div className="space-y-2 rounded-xl border border-amber-300 bg-amber-50 p-3">
      <p className="text-xs font-bold text-amber-800">هزینه ارسال تغییر کرده است؛ لطفاً هزینه به‌روز را بررسی کنید.</p>
      <div className="space-y-1 text-[12px] text-slate-600">
        <div className="flex justify-between"><span>هزینه ارسال قبلی:</span><span className="font-bold line-through">{formatPrice(mismatch.oldTotalShipping)} تومان</span></div>
        <div className="flex justify-between"><span>هزینه ارسال جدید:</span><span className="font-bold">{mismatch.newTotalShipping !== null ? `${formatPrice(mismatch.newTotalShipping)} تومان` : 'در حال محاسبه…'}</span></div>
        <div className="flex justify-between"><span>مبلغ قابل پرداخت قبلی:</span><span className="line-through">{formatPrice(mismatch.oldPayable)} تومان</span></div>
        <div className="flex justify-between"><span>مبلغ قابل پرداخت جدید:</span><span className="font-bold">{mismatch.newPayable !== null ? `${formatPrice(mismatch.newPayable)} تومان` : 'در حال محاسبه…'}</span></div>
      </div>
      {mismatch.newTotalShipping !== null ? (
        mismatch.acknowledged ? (
          <p className="text-[11px] font-bold text-emerald-700">هزینه جدید تأیید شد؛ می‌توانید سفارش را ثبت کنید.</p>
        ) : (
          <button
            type="button"
            onClick={() => quoteSession.acknowledgeMismatch()}
            className="min-h-11 rounded-full bg-amber-800 px-4 text-sm font-bold text-white hover:bg-amber-900"
          >
            هزینه جدید را تأیید می‌کنم
          </button>
        )
      ) : (
        <button
          type="button"
          onClick={retryShippingQuote}
          className="min-h-11 rounded-full bg-amber-100 px-4 text-sm font-bold text-amber-900 hover:bg-amber-200"
        >
          محاسبه دوباره هزینه ارسال
        </button>
      )}
    </div>
  ) : null;
  // Worker A: live shipping slot (drawer wC: wt = ev legs widget).
  // The per-leg detail line reuses this drawer's established leg
  // format; live's inner fb leg-detail internals are unproven beyond
  // the header/fare/items structure ported here.
  // CHECKOUT-02 (G-07/G-13): the slot is always rendered during
  // checkout — loading / warning / error never fall back to the
  // "محاسبه در مرحله بعد" copy — and a retained quote is shown with
  // an explicit stale marking.
  const shippingSlot = effectiveStep === 'checkout' ? (
    <div className="space-y-3" aria-live="polite">
      {shippingQuote && (
        <div className="space-y-2">
          {isQuoteStale && (
            <p className="text-xs font-semibold text-amber-800">هزینهٔ قبلی نمایش داده شده است. قبل از ثبت، مبلغ به‌روز را بررسی کنید.</p>
          )}
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
                  <span className="whitespace-nowrap font-medium text-slate-600">{`${formatPrice(leg.fare)} تومان`}</span>
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
      )}
      {(isQuoteRefreshing || shippingQuoteState === 'loading') && !shippingQuote && (
        <p className="text-[12px] font-bold text-slate-600">در حال محاسبه هزینه ارسال…</p>
      )}
      {!isQuoteRefreshing && shippingQuoteState === 'warning' && shippingQuoteMessage && (
        <p className="text-[12px] font-bold text-amber-700">{shippingQuoteMessage}</p>
      )}
      {!isQuoteRefreshing && shippingQuoteState === 'error' && (
        <div className="space-y-2">
          <div className="flex items-center gap-1.5 text-amber-700 font-bold text-xs">
            <span>هزینه ارسال محاسبه نشد — ثبت سفارش ممکن نیست</span>
          </div>
          <p className="text-[11px] text-slate-500 leading-5">{shippingQuoteMessage || 'برای ثبت سفارش، هزینه ارسال باید محاسبه شود. لطفاً دوباره تلاش کنید.'}</p>
          <button
            type="button"
            onClick={retryShippingQuote}
            className="min-h-11 rounded-full bg-amber-100 px-4 text-sm font-bold text-amber-900 hover:bg-amber-200"
          >
            تلاش دوباره
          </button>
        </div>
      )}
      {mismatchBlock}
    </div>
  ) : undefined;
  // Worker A: the footer breakdown (live tt()) shared by the footer
  // (mobile) and the desktop side column. The per-leg rows moved
  // into shippingSlot, like live — they are not part of tt().
  const pricingBreakdown = (
    <>
      <div className="flex flex-wrap justify-between gap-x-3 gap-y-1 text-[var(--checkout-muted)] tabular-nums">
        <span>قیمت کالاها:</span>
        <span>{formatPrice(isDirectBuy ? directOriginal : cartTotal)} تومان</span>
      </div>
      {(isDirectBuy ? directAddonTotal : cartAddonsTotal) > 0 && (
        <div className="flex flex-wrap justify-between gap-x-3 gap-y-1 text-[var(--checkout-muted)] tabular-nums">
          <span>محصولات مکمل:</span>
          <span>{formatPrice(isDirectBuy ? directAddonTotal : cartAddonsTotal)} تومان</span>
        </div>
      )}
      {(isDirectBuy ? directDiscount : cartDiscount) > 0 && (
        <div className="flex flex-wrap justify-between gap-x-3 gap-y-1 font-bold tabular-nums text-rose-800">
          <span>تخفیف ویژه:</span>
          <span><bdi>−{formatPrice(isDirectBuy ? directDiscount : cartDiscount)}</bdi> تومان</span>
        </div>
      )}
      <div className="flex flex-wrap justify-between gap-x-3 gap-y-1 text-[var(--checkout-muted)] tabular-nums">
        <span>هزینه ارسال سفارش:</span>
        <span className="font-bold">
          {effectiveStep === 'cart' ? 'با وارد کردن نشانی' :
            hasShippingQuote ? `${formatPrice(shippingTotal)} تومان` :
            isQuoteStale ? `${formatPrice(shippingTotal)} تومان (مبلغ قبلی)` :
            !isQuoteRefreshing && shippingQuoteState === 'warning' ? 'پس از تکمیل نشانی' :
            !isQuoteRefreshing && shippingQuoteState === 'error' ? 'محاسبه نشد' : 'در حال محاسبه…'}
        </span>
      </div>
      <div className="flex flex-wrap justify-between gap-x-4 gap-y-1 border-t border-[var(--checkout-line)] pt-3 text-sm font-black tabular-nums text-[var(--checkout-ink)]">
        <span>{hasShippingQuote ? 'مبلغ قابل پرداخت' : isQuoteStale ? 'جمع با هزینهٔ ارسال قبلی' : 'جمع کالاها'}</span>
        <span className="whitespace-nowrap text-[var(--checkout-accent)]">{formatPrice(effectiveStep === 'checkout' ? finalPayable : goodsPayable)} تومان</span>
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
  // CHECKOUT-02: the 450 ms debounce still applies to fetches; an
  // incomplete destination or an emptied cart is applied immediately
  // (warning / no submittable quote) without a fetch. Closing the
  // drawer aborts the pending request and discards its display state
  // (G-06); reopening refetches because isCartOpen is a dep.
  useEffect(() => {
    if (!isCartOpen) {
      quoteSession.cancelAndClear();
      return;
    }
    if (effectiveStep !== 'checkout') return;
    const destComplete = Boolean(customerInfo.province && customerInfo.city && customerInfo.address);
    if (quoteInput.items.length === 0 || !destComplete) {
      quoteSession.request(quoteInput, goodsPayable);
      return;
    }
    const timer = setTimeout(() => {
      quoteSession.request(quoteInput, goodsPayable);
    }, 450);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [checkoutStep, isDirectBuy, isCartOpen, quoteSignature, customerUser.isLoggedIn]);

  // Worker A: live drawer prefills the checkout form from the chosen
  // direct-buy address exactly once per direct-buy payload (sC).
  const directBuyRef = useRef<DirectBuy | null>(null);
  // CHECKOUT-02 (Q8, direct-buy limb): an address edit in the form
  // clears the saved direct-buy address override, so the order is
  // computed from the typed fields. The flag is reset on each new
  // direct-buy prefill.
  const customerInfoRef = useRef(customerInfo);
  customerInfoRef.current = customerInfo;
  const [directAddressEdited, setDirectAddressEdited] = useState(false);
  const handleFormCustomerInfoChange: React.Dispatch<React.SetStateAction<any>> = (updater: any) => {
    const prev = customerInfoRef.current;
    const next = typeof updater === 'function' ? updater(prev) : { ...prev, ...updater };
    if (isDirectBuy && (['province', 'city', 'address', 'postalCode'] as const).some((key) => next?.[key] !== prev?.[key])) {
      setDirectAddressEdited(true);
    }
    setCustomerInfo(next);
  };
  useEffect(() => {
    if (!directBuy || !directBuyAddress || directBuyRef.current === directBuy) return;
    directBuyRef.current = directBuy;
    // CHECKOUT-02 fix B3: a direct-buy submit never legitimately
    // wants a cart leftover. The staged selection is a module-level
    // singleton that survives drawer close / mode switch, and the
    // cart form never re-stages in direct mode — so an edited
    // direct-buy submit (address=undefined) would fall back to the
    // stale cart addressId in placeOrder and deterministically fail
    // with shipping_quote_changed. Clear it on entering direct-buy
    // mode. This never touches the cart retry path: placeOrder's
    // catch re-stage stays as-is, and this clear runs only on mode
    // entry/close, never on submit.
    stageAddressSelection(null);
    setDirectAddressEdited(false);
    setCustomerInfo((prev) => ({...prev,
      fullName: directBuyAddress.receiverFullName,
      phone: customerUser.mobile || directBuyAddress.receiverPhone || prev.phone,
      province: directBuyAddress.provinceName,
      city: directBuyAddress.cityName,
      address: directBuyAddress.addressDetail,
      postalCode: directBuyAddress.postalCode || prev.postalCode,
    }));
  }, [directBuy, directBuyAddress, customerUser.mobile]);
  // CHECKOUT-02 (G-05): a stale quote must never leak across a
  // direct↔cart switch, in either direction — abort the in-flight
  // request and clear the display immediately.
  const prevModeRef = useRef(isDirectBuy);
  useEffect(() => {
    if (prevModeRef.current !== isDirectBuy) {
      prevModeRef.current = isDirectBuy;
      quoteSession.cancelAndClear();
      // CHECKOUT-02 fix B3: same staged-selection clear as the
      // prefill effect — covers a cart→direct switch inside an open
      // drawer even before any direct-buy address is set.
      if (isDirectBuy) stageAddressSelection(null);
    }
  }, [isDirectBuy, quoteSession]);

  if (!isCartOpen) return null;

  const handleProceedToCheckout = () => {
    // Enforce Chapters 3 & 7: Check OTP authentication before checkout
    if (!customerUser.isLoggedIn) {
      setIsAuthOpen(true);
      return;
    }
    setCheckoutStep('checkout');
  };

  // Shared success path for the normal submit and the S1
  // fingerprint-conflict reclaim below: a recovered or reclaimed
  // order lands on the same success screen as a first submit.
  // CHECKOUT-02 fix S2: when `recovered` is true the order came
  // from recovery under the OLD payload — the just-attempted submit
  // differs from it by construction (the conflict proves it) — so
  // the copy says the earlier order was recovered and the cart is
  // deliberately kept (the new payload is still unpurchased).
  const handleOrderPlaced = (order: Order, recovered = false) => {
    quoteSession.clearMismatch();
    // Live clears only the direct-buy state here, never the cart.
    if (isDirectBuy) clearDirectBuy();
    setReceipt({ order, direct: isDirectBuy, recovered });
    setCheckoutStep('success');
    toastSuccess(recovered
      ? 'سفارش قبلی شما بازیابی شد؛ این سفارش با اطلاعات قبلی ثبت شده است. اطلاعات جدیدی که وارد کرده‌اید هنوز ثبت نشده؛ برای ثبت آن‌ها «شروع خرید تازه» را بزنید.'
      : 'سفارش شما ثبت شد. جزئیات پرداخت در سفارش‌های من قابل پیگیری است.');
  };

  const handleCheckoutSubmit = async () => {
    if (orderBusy) return;
    if (!customerUser.isLoggedIn) {
      setIsAuthOpen(true);
      return;
    }
    if (!customerInfo.fullName || customerInfo.fullName.trim().length < 3) {
      document.getElementById('cart-checkout-full-name')?.focus();
      toastError('لطفاً نام و نام خانوادگی تحویل‌گیرنده را وارد کنید.');
      return;
    }
    if (!isValidIranianMobile(customerInfo.phone)) {
      document.getElementById('cart-checkout-phone')?.focus();
      toastError('شماره موبایل خریدار نامعتبر است (مثال: ۰۹۱۲۳۴۵۶۷۸۹).');
      return;
    }
    if (!customerInfo.province || !customerInfo.city || !customerInfo.address) {
      toastError('لطفاً اطلاعات کامل آدرس پستی را تکمیل کنید.');
      return;
    }
    if (!isValidIranianPostalCode(customerInfo.postalCode)) {
      document.getElementById('cart-checkout-postal-code')?.focus();
      toastError('کد پستی ۱۰ رقمی وارد شده نامعتبر است.');
      return;
    }
    // CHECKOUT-02 (G-08): never submit without the latest matching
    // fresh quote — the gate keys on the quoteId, never on the amount.
    // An emptied cart mid-checkout has no submittable quote either.
    // A blocked submit creates no order, clears nothing, and keeps
    // the form intact.
    if (displayItems.length === 0) {
      toastError('سبد خرید خالی است؛ ثبت سفارش ممکن نیست.');
      return;
    }
    if (!canSubmitQuote) {
      toastError(
        isQuoteRefreshing || shippingQuoteState === 'loading'
          ? 'هزینه ارسال در حال محاسبه است؛ لطفاً چند لحظه صبر کنید.'
          : mismatch && !mismatch.acknowledged
            ? 'هزینه ارسال تغییر کرده است؛ لطفاً هزینه به‌روز را بررسی و تأیید کنید.'
            : shippingQuoteState === 'warning'
              ? 'برای محاسبه هزینه ارسال، نشانی کامل را وارد کنید.'
              : 'هزینه ارسال محاسبه نشد. لطفاً دوباره تلاش کنید.',
      );
      return;
    }
    const quoteId = quoteSession.getSubmittableQuoteId();
    if (!quoteId) {
      toastError('هزینه ارسال محاسبه نشد. لطفاً دوباره تلاش کنید.');
      return;
    }

    setIsSubmittingOrder(true);
    const submitWithQuote = async (qid: string): Promise<Order> => {
      if (isDirectBuy) {
        // Live direct submit (wC ve()): the chosen saved address is
        // required — unless the buyer edited the address after the
        // prefill (Q8), in which case the typed fields win and no
        // saved addressId is forced on submit.
        if (!directBuy || !directLine) {
          throw new Error('آدرس خرید فوری یافت نشد؛ لطفاً دوباره «خرید فوری» را بزنید.');
        }
        if (!directAddressEdited && !directBuyAddress) {
          throw new Error('آدرس خرید فوری یافت نشد؛ لطفاً دوباره «خرید فوری» را بزنید.');
        }
        return placeOrder(customerInfo, paymentMethod, undefined, qid, { lines: [directLine], store: directBuy.store }, directAddressEdited ? undefined : { addressId: directBuyAddress!.id });
      }
      return placeOrder(customerInfo, paymentMethod, undefined, qid);
    };
    try {
      // CHECKOUT-02 (G-12/Q2): a resubmit after an acknowledged
      // mismatch goes through abandon-and-reclaim, never a bare retry
      // under the old attempt.
      // CHECKOUT-02 fix S3: a B2 recovery short-circuits the
      // StoreContext placeOrder wrapper, so its cart clear never
      // runs — applyReclaimOutcome clears the cart for the
      // payload-identical recovery case, like a normal success.
      // The generic success copy stays: the payload is identical.
      // CHECKOUT-02 fix B4: in direct-buy mode the cart is NOT
      // part of this purchase — a direct order must NOT clear the
      // cart (StoreContext.tsx:1562-1565, wrapper `if (!direct)`)
      // — so the recovery clear is gated on !isDirectBuy.
      if (mismatch?.acknowledged) {
        const outcome = await resubmitAfterMismatch({ session: quoteSession, abandonCheckout: startNewCheckout, submitOrder: submitWithQuote, recoverCheckout });
        applyReclaimOutcome(outcome, {
          clearCartOnRecovery: !isDirectBuy,
          clearCart,
          onPlaced: (order) => handleOrderPlaced(order),
        });
      } else {
        const order = await submitWithQuote(quoteId);
        handleOrderPlaced(order);
      }
    } catch (err: any) {
      // CHECKOUT-02 (G-12/Q1): a server mismatch auto-refetches the
      // quote and shows old vs new for explicit review — no address
      // edit, no drawer close, no cart loss. The dead
      // shipping_quote_changed branch in the old quote catch is gone:
      // that code is only ever returned by order creation.
      if (isQuoteChangedError(err)) {
        quoteSession.beginMismatch();
        toastError('هزینه ارسال تغییر کرده است؛ هزینه به‌روز را بررسی و تأیید کنید.');
        return;
      }
      // CHECKOUT-02 fix S1: a fingerprint conflict means the submit's
      // payload no longer matches the open attempt — the usual cause
      // is an input change or mode switch after a mismatch
      // acknowledgement cleared the mismatch, so this submit never
      // went through abandon-and-reclaim. Route it through the same
      // guarded reclaim as B2 (recover-check first) instead of
      // leaving the user with the error + banner. A completed
      // attempt is NOT reclaimed here: that is a known purchase and
      // a genuinely new one starts via the banner's "شروع خرید تازه"
      // (G-13(c)); the error toast below is the correct outcome there.
      // A stale (non-submittable) quote also fails closed here.
      if (isCheckoutFingerprintConflictError(err)) {
        if (checkoutAttempt?.state === 'completed') {
          toastError(err?.message || 'خرید قبلی هنوز بسته نشده است؛ ابتدا نتیجه آن را بازیابی کنید یا «شروع خرید تازه» را بزنید.');
          return;
        }
        const reclaimQuoteId = quoteSession.quoteSignature === quoteSignature ? quoteSession.getSubmittableQuoteId() : null;
        if (!reclaimQuoteId) {
          toastError('هزینه ارسال محاسبه نشد. لطفاً دوباره تلاش کنید.');
          return;
        }
        try {
          const reclaimed = await guardedReclaimSubmit({ quoteId: reclaimQuoteId, abandonCheckout: startNewCheckout, submitOrder: submitWithQuote, recoverCheckout });
          // CHECKOUT-02 fix S2/S3: a recovered order here is the
          // EARLIER order (different payload by construction) — the
          // distinct copy applies and the cart is KEPT (the new
          // payload is still unpurchased), so clearCartOnRecovery
          // is false. A fresh reclaimed submit clears the cart via
          // the StoreContext wrapper as usual.
          applyReclaimOutcome(reclaimed, {
            clearCartOnRecovery: false,
            clearCart,
            onPlaced: (order, recovered) => handleOrderPlaced(order, recovered),
          });
        } catch (reclaimErr: any) {
          // CHECKOUT-02 fix N1: a shipping_quote_changed thrown by
          // the reclaim submit is a fresh mismatch, not a generic
          // failure — route it through beginMismatch like the outer
          // catch, so the user gets the guided review instead of a
          // dead-end toast (it used to self-heal only on the next
          // submit press).
          if (isQuoteChangedError(reclaimErr)) {
            quoteSession.beginMismatch();
            toastError('هزینه ارسال تغییر کرده است؛ هزینه به‌روز را بررسی و تأیید کنید.');
            return;
          }
          toastError(mapShaparakSimulatorError(reclaimErr?.message || '') || 'ثبت سفارش انجام نشد. لطفاً دوباره تلاش کنید.');
        }
        return;
      }
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
    // CHECKOUT-02 (G-06): abort any pending quote and discard its
    // display state; the cart and the typed address survive.
    quoteSession.cancelAndClear();
    // CHECKOUT-02 fix B3 (defence in depth): a staged cart address
    // must not survive the drawer close into a later direct-buy
    // submit. The cart form re-stages from its fields on reopen, so
    // nothing is lost; placeOrder's catch re-stage (cart retry path)
    // is untouched.
    stageAddressSelection(null);
    setDirectAddressEdited(false);
    setIsAuthOpen(false);
    setReceipt(null);
    setCheckoutStep('cart');
  };

  const handleDialogKeys = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.defaultPrevented) return;
    if (event.key === 'Escape') {
      if (!isAuthOpen && !event.currentTarget.contains(event.target as Node)) return;
      event.preventDefault();
      if (isAuthOpen) setIsAuthOpen(false);
      else handleClose();
      return;
    }
    if (event.key !== 'Tab' || isAuthOpen) return;
    const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>(
      'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, [tabindex="0"]',
    )).filter((control) => control.tabIndex >= 0 && control.getClientRects().length > 0);
    const first = controls[0];
    const last = controls[controls.length - 1];
    if (event.shiftKey && (document.activeElement === first || document.activeElement === titleRef.current)) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  };

  const steps = isDirectBuy || (effectiveStep === 'success' && receipt?.direct) ? [
    { id: 'checkout', label: 'نشانی و ثبت' },
    { id: 'success', label: 'رسید سفارش' },
  ] : [
    { id: 'cart', label: 'سبد خرید' },
    { id: 'checkout', label: 'نشانی و ثبت' },
    { id: 'success', label: 'رسید سفارش' },
  ];
  const stepIndex = steps.findIndex((step) => step.id === effectiveStep);
  const title = effectiveStep === 'cart' ? 'سبد خرید شما' : effectiveStep === 'checkout' ? 'تکمیل سفارش' : 'رسید سفارش';
  const priceText = formatPrice(effectiveStep === 'checkout' ? finalPayable : goodsPayable);
  const primaryButton = 'min-h-12 min-w-0 w-full rounded-xl bg-[var(--checkout-accent)] px-3 text-sm font-extrabold text-white transition-colors hover:brightness-90 disabled:cursor-not-allowed disabled:opacity-55';

  return (
    <div dir="rtl" className="fixed inset-0 z-50 flex justify-center overflow-hidden bg-slate-950/55 lg:p-7">
      <motion.div
        role="dialog"
        aria-modal={!isAuthOpen || undefined}
        aria-labelledby="cart-drawer-title"
        inert={isAuthOpen || undefined}
        onKeyDown={handleDialogKeys}
        style={checkoutStyle}
        data-checkout-scope={centralCheckout ? 'central' : 'storefront'}
        initial={reducedMotion ? false : { y: 24, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        transition={{ duration: reducedMotion ? 0 : 0.18 }}
        className={`flex h-dvh w-full min-w-0 max-w-[640px] flex-col overflow-hidden bg-[var(--checkout-bg)] text-[var(--checkout-ink)] shadow-2xl lg:my-auto lg:h-auto lg:max-h-[calc(100dvh-3.5rem)] lg:max-w-5xl lg:rounded-3xl ${centralCheckout ? 'central-portal' : ''}`}
      >
        <header className="shrink-0 border-b border-[var(--checkout-line)] bg-white px-4 pt-3.5 sm:px-6 sm:pt-5">
          <div className="flex items-center justify-between gap-3 pb-3">
            <div className="flex min-w-0 items-center gap-3">
              <span className="grid size-11 shrink-0 place-items-center rounded-xl bg-[var(--checkout-soft)] text-[var(--checkout-accent)]">
                <ShoppingBag className="size-5" aria-hidden="true" />
              </span>
              <div className="min-w-0">
                <h2 id="cart-drawer-title" tabIndex={-1} ref={titleRef} className="text-lg font-black leading-7 text-balance focus:outline-none sm:text-2xl">{title}</h2>
                {displayStoreName && effectiveStep !== 'success' && (
                  <p className="truncate text-sm text-[var(--checkout-muted)]">{displayStoreName}</p>
                )}
              </div>
            </div>
            <button type="button" onClick={handleClose} aria-label="بستن سبد خرید" className="grid size-11 shrink-0 place-items-center rounded-xl border border-[var(--checkout-line)] bg-white text-[var(--checkout-ink)] hover:bg-[var(--checkout-soft)]">
              <X className="size-5" aria-hidden="true" />
            </button>
          </div>
          {(displayItems.length > 0 || effectiveStep === 'success') && (
            <ol aria-label="مراحل سفارش" className={`grid gap-1.5 border-t border-[var(--checkout-line)] py-2.5 text-xs sm:gap-3 sm:text-sm ${steps.length === 2 ? 'grid-cols-2' : 'grid-cols-3'}`}>
              {steps.map((step, index) => (
                <li key={step.id} aria-current={index === stepIndex ? 'step' : undefined}
                  className={`flex min-w-0 items-center gap-1.5 rounded-xl px-1.5 py-2 font-semibold sm:gap-2 sm:px-3 ${index === stepIndex ? 'bg-[var(--checkout-soft)] text-[var(--checkout-accent)]' : 'text-[var(--checkout-muted)]'}`}>
                  <span aria-hidden="true" className={`grid size-5 shrink-0 place-items-center rounded-full text-[11px] tabular-nums sm:size-6 ${index <= stepIndex ? 'bg-[var(--checkout-accent)] text-white' : 'border border-[var(--checkout-line)] text-[var(--checkout-muted)]'}`}>
                    {index < stepIndex ? <Check className="size-3" /> : toPersianDigits(index + 1)}
                  </span>
                  <span className="min-w-0 leading-5">{step.label}</span>
                </li>
              ))}
            </ol>
          )}
        </header>

        <div ref={bodyRef} className="min-h-0 flex-1 space-y-4 overflow-y-auto overscroll-contain p-4 [scrollbar-gutter:stable] sm:p-6">
          {checkoutAttempt && effectiveStep !== 'success' && (
            <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
              <p role="status">{checkoutAttempt.state === 'completed' ? 'سفارش قبلی ثبت شده است. برای خرید دوباره، «شروع خرید تازه» را بزنید.' : 'سفارش قبلی را بازیابی کنید تا از ثبت تکراری جلوگیری شود.'}</p>
              <div className="flex flex-wrap gap-x-4">
                <button type="button" disabled={orderBusy} className="min-h-11 font-bold underline disabled:opacity-50" onClick={async () => {
                  try {
                    const order = await recoverCheckout();
                    if (order) {
                      toastSuccess(`سفارش ${order.orderNumber} بازیابی شد.`);
                      handleClose();
                      setActiveView('customer_orders');
                    } else toastError('سفارشی برای این تلاش پیدا نشد. همین اطلاعات را دوباره بفرستید یا خرید تازه را شروع کنید.');
                  } catch (err: any) { toastError(err.message); }
                }}>بازیابی سفارش قبلی</button>
                <button type="button" disabled={orderBusy} className="min-h-11 font-bold underline disabled:opacity-50" onClick={async () => {
                  try {
                    await startNewCheckout();
                    setReceipt(null);
                    if (!isDirectBuy) setCheckoutStep('cart');
                    toastSuccess('می‌توانید سفارش تازه‌ای ثبت کنید.');
                  } catch (err: any) { toastError(err.message); }
                }}>شروع خرید تازه</button>
              </div>
            </div>
          )}

          {effectiveStep === 'cart' && <CartItemList onBrowse={() => { handleClose(); openCatalogCategory('all'); }} />}
          {effectiveStep === 'checkout' && (
            <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(270px,340px)] lg:gap-6">
              <form id="checkout-details" onSubmit={(event) => { event.preventDefault(); void handleCheckoutSubmit(); }}>
                <fieldset disabled={orderBusy} className="min-w-0 rounded-[var(--checkout-radius)] border border-[var(--checkout-line)] bg-white p-4 sm:p-5">
                  {directLine && (
                    <div className="mb-5 flex items-start gap-3 rounded-xl bg-[var(--checkout-soft)] p-3 text-sm">
                      <Package className="mt-0.5 size-4 shrink-0 text-[var(--checkout-accent)]" aria-hidden="true" />
                      <div className="min-w-0">
                        <p className="font-bold">{directLine.product.title}</p>
                        <p className="mt-1 text-xs text-slate-600">{toPersianDigits(directLine.quantity)} عدد · خرید فوری</p>
                      </div>
                    </div>
                  )}
                  <CartCheckoutForm
                    customerInfo={customerInfo}
                    setCustomerInfo={handleFormCustomerInfoChange}
                    hideSavedAddressSelector={isDirectBuy}
                  />
                </fieldset>
              </form>
              <CheckoutSummaryColumn shippingSlot={shippingSlot}>
                {pricingBreakdown}
              </CheckoutSummaryColumn>
            </div>
          )}
          {effectiveStep === 'success' && receipt && <CartOrderSuccess order={receipt.order} recovered={receipt.recovered} onClose={handleClose} />}
        </div>

        {effectiveStep !== 'success' && displayItems.length > 0 && (
          <footer className="shrink-0 border-t border-[var(--checkout-line)] bg-white px-4 pt-3 pb-[max(.75rem,env(safe-area-inset-bottom))] sm:px-6">
            {effectiveStep === 'cart' ? (
              <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_250px] sm:items-end sm:gap-5">
                <details className="group">
                  <summary className="flex min-h-11 cursor-pointer list-none flex-wrap items-center justify-between gap-x-3 gap-y-1 py-2 text-sm [&::-webkit-details-marker]:hidden">
                    <span className="flex items-center gap-1 text-slate-700">جمع کالاها <ChevronDown className="size-4 transition-transform group-open:rotate-180" aria-hidden="true" /></span>
                    <strong className="font-extrabold tabular-nums text-[var(--checkout-accent)]">{priceText} تومان</strong>
                  </summary>
                  <div className="space-y-2 border-t border-slate-100 py-3 text-xs">{pricingBreakdown}</div>
                </details>
                <button type="button" onClick={handleProceedToCheckout} className={`${primaryButton} flex items-center justify-center gap-2`}>
                  ادامه به نشانی و ارسال
                  <ArrowLeft className="size-4" aria-hidden="true" />
                </button>
              </div>
            ) : (
              <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(260px,360px)] sm:items-center sm:gap-5">
                <div className="flex flex-wrap items-center justify-between gap-x-2 text-sm sm:justify-start">
                  <span className="text-slate-700">{hasShippingQuote ? 'مبلغ قابل پرداخت' : isQuoteStale ? 'جمع با ارسال قبلی' : 'جمع کالاها'}</span>
                  <strong className="whitespace-nowrap text-base font-extrabold tabular-nums text-[var(--checkout-accent)]">{priceText} تومان</strong>
                  {!canConfirmOrder && (
                    <p id="checkout-submit-help" className="w-full text-xs leading-5 text-slate-600" aria-live="polite">
                      {mismatch && !mismatch.acknowledged ? 'هزینهٔ جدید را در بخش ارسال بررسی و تأیید کنید.' :
                        shippingQuoteState === 'warning' && !isQuoteRefreshing ? 'برای ادامه، نشانی را کامل کنید.' :
                        shippingQuoteState === 'error' && !isQuoteRefreshing ? 'در بخش ارسال، محاسبهٔ هزینه را دوباره انجام دهید.' :
                        'هزینهٔ ارسال در حال محاسبه است.'}
                    </p>
                  )}
                </div>
                <div className="flex items-stretch gap-2">
                  <button type="button" disabled={orderBusy} onClick={() => { if (isDirectBuy) handleClose(); else setCheckoutStep('cart'); }} className="flex min-h-12 shrink-0 items-center gap-1.5 rounded-xl border border-[var(--checkout-line)] px-2.5 text-xs font-bold text-[var(--checkout-ink)] hover:bg-[var(--checkout-soft)] disabled:opacity-55">
                    <ArrowRight className="size-4" aria-hidden="true" />
                    {isDirectBuy ? 'بازگشت به کالا' : 'بازگشت به سبد'}
                  </button>
                  <button
                    type="submit"
                    form="checkout-details"
                    disabled={orderBusy || !canConfirmOrder}
                    aria-describedby={!canConfirmOrder ? 'checkout-submit-help' : undefined}
                    className={primaryButton}
                  >
                    {orderBusy ? 'در حال ثبت سفارش…' : 'ثبت سفارش'}
                  </button>
                </div>
              </div>
            )}
          </footer>
        )}

        <AuthModal central={centralCheckout}
          isOpen={isAuthOpen}
          onClose={() => setIsAuthOpen(false)}
          onSuccess={(user) => {
            const canShop = user.capabilities?.includes('customer') ?? user.role === 'customer';
            if (!canShop) {
              toastError('این حساب مجوز ثبت سفارش مشتری را ندارد.');
              return;
            }
            setCustomerUser((prev: CustomerUser) => ({ ...prev, fullName: user.fullName || prev.fullName, mobile: user.mobile, isLoggedIn: true }));
            setCustomerInfo((prev) => ({ ...prev, fullName: user.fullName || prev.fullName, phone: user.mobile }));
            setCheckoutStep('checkout');
          }}
          disableAutoNavigate
        />
      </motion.div>
    </div>
  );
};
