import { apiRequest } from './apiClient';

export interface CreateOrderItemPayload {
  productId: string;
  variantId?: string;
  quantity: number;
  addonIds?: string[];
}

export interface CustomerInfoPayload {
  fullName: string;
  phone: string;
  province: string;
  city: string;
  address: string;
  postalCode: string;
}

export interface CreateOrderPayload {
  storeId: string;
  customerInfo: CustomerInfoPayload;
  paymentMethod: string;
  items: CreateOrderItemPayload[];
  discountCode?: string;
  shippingQuoteId?: string;
  // Merge-phase backend (Team 1 gap): forwarded to the order handler,
  // which loads the saved address (addressId) and/or stores the order
  // address in the address book (saveAddress). A live direct order
  // uses the SAVED address even if form fields were edited.
  addressId?: string;
  saveAddress?: boolean;
  // Live parity (index-MERGED.js placeOrder): a fresh uuid per submit,
  // stored by the backend (order_handlers.go IdempotencyKey) so a
  // replayed request returns the original order. Deliberately NOT
  // derived from the cart: a deterministic key would permanently block
  // a legitimate repeat order of the same cart.
  idempotencyKey?: string;
}

export interface ConfirmDeliveryResponse {
  success: boolean;
  message: string;
  orderNumber?: string;
}

export interface DisputeDeliveryPayload {
  type: 'delivery_undelivered';
  claimAmount: number; // Toman, order payableAmount
  reason: string;
  productTitle?: string;
}

export interface DisputeDeliveryResponse {
  id: string; // e.g. DISP-… — show it in the success toast
  orderId: string;
  orderNumber: string;
  type: string;
  claimAmount: number;
  reason: string;
  status: 'pending' | 'resolved';
  createdAt: string;
  [key: string]: any;
}

export interface BackendOrderItemDto {
  id: string;
  orderId: string;
  productId: string;
  variantId?: string;
  productTitle?: string;
  quantity: number;
  wholesaleCost: number;
  platformFee: number;
  sellerMargin: number;
  finalUnitPrice: number;
  discountAmount?: number;
}

export interface OrderRefundSummary {
 status: 'none'|'pending'|'succeeded'|'failed'|'unknown';
 currency: 'TOMAN';
 amountToman: number;
 pendingToman: number;
 succeededToman: number;
 failedToman: number;
 unknownToman: number;
 destination: 'none'|'original_payment'|'wallet'|'review'|'mixed';
 needsReview: boolean;
}
export interface OrderCancellationResponse {success:boolean;orderId:string;status:'cancelled';refund:OrderRefundSummary;}

export interface BackendOrderDto {
 refund?: OrderRefundSummary;
  id: string;
  customerUserId?: string;
  orderNumber: string;
  storeId: string;
  storeName: string;
  totalAmount: number;
  discountAmount: number;
  sellerDiscountAmount?: number;
  discountCode?: string;
  payableAmount: number;
  paymentMethod: 'cash' | 'installments';
  paymentStatus: 'pending' | 'paid' | 'failed' | 'cancelled' | 'refunded';
  customerFullName: string;
  customerPhone: string;
  customerProvince: string;
  customerCity: string;
  customerAddress: string;
  customerPostalCode: string;
  status: 'pending' | 'processing' | 'shipped' | 'delivered_unconfirmed' | 'delivered_confirmed' | 'delivered' | 'delivery_disputed' | 'cancelled';
  trackingCode: string;
  shippingCost: number;
  shippingQuoteId?: string;
  returnWindowEndsAt?: string;
  dropshipperProfit: number;
  platformProfit: number;
  referralCommission?: number;
  referralCommissionReleased?: boolean;
  referrerSellerId?: string;
  profitReleased?: boolean;
  profitReleasedAt?: string;
  shippedAt?: string;
  confirmedAt?: string;
  disputedAt?: string;
  createdAt: string;
  updatedAt: string;
  items?: BackendOrderItemDto[];
}

export interface OrderShippingInfo {
  success: boolean;
  orderId: string;
  orderNumber: string;
  status: string;
  fulfillmentMethod: string;
  carrier?: string | null;
  trackingNumber?: string | null;
  trackingCode?: string | null;
  shippingLabel?: string | null;
  packedAt?: string | null;
  shippedAt?: string | null;
  boxSize?: string | null;
  weightGrams?: number | null;
  returnWindowEndsAt?: string | null;
  shipment?: {
    carrier?: string | null;
    trackingNumber?: string | null;
    shippedAt?: string | null;
    deliveredAt?: string | null;
    status?: string | null;
  };
}

// The return destination as derived by the backend from the order's
// shipment origins (owner rule, Oct 28 2026): the parcel goes back to
// the warehouse it shipped from. Carried by the return-quote payload.
export interface ReturnDestinationInfo {
  type: 'central' | 'supplier';
  warehouseId?: string;
  supplierWarehouseId?: string;
  name: string;
  city?: string;
  province?: string;
  address?: string;
}

export interface ReturnQuoteResponse {
  groups: { originId?: string; originLabel?: string; fare: number }[];
  totalFareToman: number;
  orderId?: string;
  orderNumber?: string;
  shippingQuoteUnavailable?: boolean;
  destination?: ReturnDestinationInfo;
}

export interface CustomerReturnDto {
  id: string;
  orderId: string;
  customerId?: string;
  status: string;
  reason: string;
  warehouseId?: string;
  destinationSupplierWarehouseId?: string;
  destinationName?: string;
  destinationCity?: string;
  destinationType?: string;
  prepaid?: boolean;
  trackingNumber?: string;
  createdAt?: string;
  items?: { id: string; orderItemId: string; qty: number; qcStatus?: string }[];
}

export const orderApi = {
  createOrder: async (payload: CreateOrderPayload): Promise<BackendOrderDto> => {
    return apiRequest<BackendOrderDto>('/orders', {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  trackOrder: async (trackingCode: string): Promise<BackendOrderDto> => {
    return apiRequest<BackendOrderDto>(`/orders/track/${encodeURIComponent(trackingCode.trim())}`, {
      method: 'GET',
    });
  },

  confirmDelivery: async (idOrNumber: string): Promise<ConfirmDeliveryResponse> => {
    return apiRequest<ConfirmDeliveryResponse>(`/orders/confirm-delivery/${encodeURIComponent(idOrNumber)}`, {
      method: 'POST',
      auth: true,
    });
  },

  // P0-1: POST /orders/{id}/dispute. The body is mandatory — an empty
  // body 400s server-side — so always send type + claimAmount + reason.
  disputeDelivery: async (idOrNumber: string, payload: DisputeDeliveryPayload): Promise<DisputeDeliveryResponse> => {
    return apiRequest<DisputeDeliveryResponse>(`/orders/${encodeURIComponent(idOrNumber)}/dispute`, {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  // Team 08 live parity (D64): GET /orders/<id>/shipping (Team 03
  // backend). Display only; order creation is untouched.
  getOrderShipping: async (idOrNumber: string): Promise<OrderShippingInfo> => {
    return apiRequest<OrderShippingInfo>(`/orders/${encodeURIComponent(idOrNumber)}/shipping`, {
      method: 'GET',
      auth: true,
    });
  },

  // FIX 5 (Team 03 G4/G5): customer cancel / return / return-quote /
  // my-returns. The return body is the live client shape
  // (order_item_id + warehouse_id + prepaid), not just orderItemId.
  cancelOrder: async (idOrNumber: string): Promise<OrderCancellationResponse> => {
    return apiRequest<OrderCancellationResponse>(`/orders/${encodeURIComponent(idOrNumber)}/cancel`, {
      method: 'POST',
      auth: true,
    });
  },

  // FIX 14: return-quote means RETURN-SHIPPING COST on live
  // ({groups, totalFareToman}), not a refund estimate.
  requestReturnQuote: async (
    idOrNumber: string,
    items: { order_item_id: string }[],
  ): Promise<ReturnQuoteResponse> => {
    return apiRequest<ReturnQuoteResponse>(`/orders/${encodeURIComponent(idOrNumber)}/return-quote`, {
      method: 'POST',
      auth: true,
      body: JSON.stringify({ items }),
    });
  },

  requestReturn: async (
    idOrNumber: string,
    body: { items: { order_item_id: string; qty: number }[]; reason: string; warehouse_id?: string; prepaid?: boolean },
  ): Promise<CustomerReturnDto> => {
    // FIX 14: live wraps the single return as {return: ...}; unwrap
    // tolerantly so a bare DTO still works.
    const data = await apiRequest<any>(`/orders/${encodeURIComponent(idOrNumber)}/return`, {
      method: 'POST',
      auth: true,
      body: JSON.stringify(body),
    });
    return (data?.return ?? data) as CustomerReturnDto;
  },

  getMyReturns: async (): Promise<CustomerReturnDto[]> => {
    // FIX 14: live wraps lists as {returns: [...]}; unwrap tolerantly.
    const data = await apiRequest<any>('/returns', {
      method: 'GET',
      auth: true,
    });
    return (Array.isArray(data) ? data : (data?.returns ?? [])) as CustomerReturnDto[];
  },

  getCustomerOrders: async (phone?: string): Promise<BackendOrderDto[]> => {
    const qs = phone ? `?phone=${encodeURIComponent(phone)}` : '';
    return apiRequest<BackendOrderDto[]>(`/orders${qs}`, {
      method: 'GET',
      auth: true,
    });
  },
};
