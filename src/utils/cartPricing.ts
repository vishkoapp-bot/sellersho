// CHECKOUT-03: one canonical cart/checkout price breakdown, shared by the cart,
// direct checkout, quote review, success screen and order detail.
//
// Definitions (all amounts integer Toman):
//   merchandiseOriginal = SUM (originalPrice || price) x qty   (list price, no addons)
//   merchandiseNet      = SUM price x qty                     (sale price, no addons)
//   merchandiseDiscount = max(0, merchandiseOriginal - merchandiseNet)
//                         — the discount is merchandise-ONLY. Addon fees are never
//                         netted against it (the old CartContext formula subtracted
//                         a payable that INCLUDED addons, swallowing the discount
//                         whenever addons were present) and never added to it (the
//                         old drawer `cartGrossDiscount = cartDiscount + addons`
//                         invented a discount equal to the addons).
//   addonsTotal         = SUM (SUM selected addon feePerUnit) x qty
//   goodsPayable        = merchandiseNet + addonsTotal
//   total               = goodsPayable + shipping
// Row identity: original - discount + addons + shipping = total, whenever every
// line's price <= its original (the sale/campaign pipeline only ever lowers
// prices, so this holds for live data; see the anomaly test in
// CartContext.pricing.test.ts).
//
// The discount is clamped at the AGGREGATE level, mirroring the server:
// backend order creation stores DiscountAmount = totalAmount - merchandisePayable
// (order_handlers.go), i.e. an aggregate over the whole order, not a per-line
// clamp. A per-line clamp would diverge from the server snapshot if any line's
// price ever exceeded its original.
//
// For a stored order, the same five fields are DERIVED FROM THE SNAPSHOT ONLY
// (computeOrderBreakdown): totalAmount / discountAmount / payableAmount /
// shippingCost. Nothing is ever recomputed from current catalog prices, so a
// historical order keeps its paid amounts exactly as the server recorded them.
import type { CartItem, Order } from '../types';

export interface CartPricing {
  /** Merchandise at list price, addons excluded. */
  merchandiseOriginal: number;
  /** Merchandise at sale price, addons excluded. */
  merchandiseNet: number;
  /** Merchandise-only discount, max(0, original - net). */
  merchandiseDiscount: number;
  /** Selected addon fees, SUM feePerUnit x qty. */
  addonsTotal: number;
  /** What the goods cost: merchandiseNet + addonsTotal (before shipping). */
  goodsPayable: number;
}

export interface OrderPricing extends CartPricing {
  shipping: number;
  /** Server-accepted payable: goodsPayable + shipping. */
  total: number;
}

const addonFeePerUnit = (item: CartItem): number => {
  const ids = item.selectedAddonIds;
  if (!ids || ids.length === 0 || !item.product.addons) return 0;
  const byId = new Map(item.product.addons.map((a) => [a.id, a.feePerUnit || 0]));
  return ids.reduce((sum, id) => sum + (byId.get(id) ?? 0), 0);
};

export const computeCartPricing = (items: CartItem[]): CartPricing => {
  let merchandiseOriginal = 0;
  let merchandiseNet = 0;
  let addonsTotal = 0;
  for (const item of items) {
    merchandiseOriginal += (item.product.originalPrice || item.product.price) * item.quantity;
    merchandiseNet += item.product.price * item.quantity;
    addonsTotal += addonFeePerUnit(item) * item.quantity;
  }
  return {
    merchandiseOriginal,
    merchandiseNet,
    merchandiseDiscount: Math.max(0, merchandiseOriginal - merchandiseNet),
    addonsTotal,
    goodsPayable: merchandiseNet + addonsTotal,
  };
};

/**
 * The same breakdown for a stored order, derived from the server snapshot
 * fields only. The server identity is:
 *   payableAmount = (totalAmount - discountAmount) + addons + shippingCost
 * so addons = payableAmount - shippingCost - (totalAmount - discountAmount).
 * Returns null when the snapshot is incoherent (legacy / partial rows), so
 * callers fall back to the plain single-total display instead of inventing
 * numbers. A stored discountAmount is used AS STORED (never re-derived);
 * only its displayed row is hidden when it is not positive.
 */
export const computeOrderBreakdown = (
  order: Pick<Order, 'totalAmount' | 'discountAmount' | 'payableAmount' | 'shippingCost'>,
): OrderPricing | null => {
  const merchandiseOriginal = order.totalAmount;
  const merchandiseDiscount = order.discountAmount;
  const shipping = order.shippingCost || 0;
  if (
    !Number.isFinite(merchandiseOriginal) || merchandiseOriginal <= 0 ||
    !Number.isFinite(merchandiseDiscount) || merchandiseDiscount < 0 ||
    !Number.isFinite(order.payableAmount) || order.payableAmount < 0 ||
    !Number.isFinite(shipping) || shipping < 0
  ) {
    return null;
  }
  const merchandiseNet = merchandiseOriginal - merchandiseDiscount;
  const addonsTotal = order.payableAmount - shipping - merchandiseNet;
  if (merchandiseNet < 0 || addonsTotal < 0) return null;
  // Note: no separate "identity" check is possible here — addonsTotal is
  // derived FROM the identity, so net + addons + shipping === payable by
  // construction. The negative guards above are the coherence check: a
  // snapshot whose payable cannot cover its net + shipping (or whose
  // discount exceeds its original) is not displayable as a breakdown.
  return {
    merchandiseOriginal,
    merchandiseNet,
    merchandiseDiscount,
    addonsTotal,
    goodsPayable: merchandiseNet + addonsTotal,
    shipping,
    total: order.payableAmount,
  };
};
