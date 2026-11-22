import { financeOperation } from './financeOperation';
import { apiRequest, tokenStore, getApiBase } from './apiClient';
import type { ProductSchema } from '../types/productSchema';

export interface SupplierSKUIdentity { variantId?: string; variantName?: string; variantSpnc?: string; }
export interface SupplierProductVariantDto { id: string; variant_name: string; spnc: string; stock: number; selection?: Record<string,string>; attributes?: Record<string,string>; is_default?: boolean; is_active?: boolean; inventory_mode?: 'legacy'|'tracked'; combination_key?: string; min_cart_qty?: number|null; max_cart_qty?: number|null; supplier_base_cost?: number|null; images?: string[]; image_url?: string | null; weight_grams?: number | null; length_mm?: number | null; width_mm?: number | null; height_mm?: number | null; sale_price?: number | null; sale_from?: string | null; sale_to?: string | null; shipping_class?: string | null; tax_class?: string | null; }
export interface SupplierOrderItemDto extends SupplierSKUIdentity { orderItemId?: string; productId: string; productTitle: string; quantity: number; }

export interface TapinBoxDto { id: string; title: string; lengthCm: number; widthCm: number; heightCm: number; maxWeightGrams?: number | null; isActive: boolean; sort: number }

export interface SupplierProductDto {
  productType?: 'legacy'|'simple'|'variable';
  schemaVersion?: number;
  productSchema?: ProductSchema;
  id: string;
  title: string;
  slug: string;
  categorySlug: string;
  supplierId: string;
  originalPrice: number;
  supplierBaseCost: number;
  platformMargin: number;
  isOriginal: boolean;
  isActive?: boolean;
  stock: number;
  imageUrl: string;
  description: string;
  warehouseName: string;
  preparationDays: number;
  supplierWarranty: string;
  supplyOption: '3hour_local' | '1day' | '3day';
  warrantyPropertyId?: string;
  brand?: string;
  fulfillmentMethod?: string;
  originSupplierWarehouseId?: string;
  rating?: number;
  reviewsCount?: number;
}

// E1 — ONE shared definition of "active product": active = isActive !== false.
// The backend list carries is_active for every row (domain.Product.IsActive);
// a missing isActive stays active, only an explicit false is inactive.
export const isSupplierProductActive = (p: { isActive?: boolean | null }): boolean => p.isActive !== false;
export interface SupplierReturnRequestDto extends SupplierSKUIdentity { id:string; supplierId?:string; productId:string; productTitle:string; warehouseName:string; requestedQuantity:number; eligibleQuantity:number; collectionMethod:'supplier_collect'|'postal'; postalCharge:number; status:string; reason:string; createdAt:string; reviewedAt?:string; handoverAt?:string; receivedAt?:string; inspectedAt?:string; inspectionStatus?:string; acceptedQuantity?:number; failedQuantity?:number; }
export interface ReturnParcelInput { quantity:number; carrierCode:string; paymentMethod:'sender_pays'|'receiver_pays'; origin:string; destination:string; lengthMm:number; widthMm:number; heightMm:number; weightGrams:number; estimatedCharge?:number; estimateSource?:'tariff'|'api'|'manual'; estimateReference?:string; finalCharge?:number; finalChargeSource?:'tariff'|'api'|'manual'; finalChargeReference?:string; trackingNumber?:string; notes?:string; }
export interface ReturnParcelDto extends ReturnParcelInput { id:string; returnRequestId:string; parcelNumber:number; status:string; createdBy:string; confirmedBy?:string; }
export interface ReturnFinancialEventDto { id:string; returnRequestId:string; parcelId?:string; operation:string; amount:number; paymentMethod:string; ledgerEntryId?:string; reason:string; actorUserId:string; createdAt:string; }
export interface CarrierOptionDto { code:string; displayName:string; supportsSenderPays:boolean; supportsReceiverPays:boolean; supportsQuoteApi:boolean; tariffVersion:number; }
export interface SupplierInventoryLocationDto extends SupplierSKUIdentity { allocation?: 'sku'|'unallocated'; productId:string; productTitle:string; ownerType:string; locationType:string; locationKey:string; warehouseName?:string; binCode?:string; status:string; quantity:number; reconciled:boolean; }
export interface WarrantyPropertyDto { id:string; name:string; description:string; status:string; }

export interface SupplierInboundDto {
  id: string;
  supplierId: string;
  supplierName: string;
  warehouseName: string;
  itemsCount: number;
  cartonsCount: number;
  carrier: string;
  truckPlate: string;
  status: string;
  createdAt: string;
  receivedAt?: string;
}

export interface SupplierDiscrepancyDto {
  response_due_at: string;
  responded_at?: string;
  escalated_at?: string;
  id: string;
  inbound_id: string;
  supplier_id: string;
  product_id: string;
  product_title?: string;
  expected_quantity: number;
  received_quantity: number;
  defect_count: number;
  reason: string;
  status: string;
  supplier_response?: string;
  resolution_offer?: string;
  created_at: string;
  resolved_at?: string;
}

export interface SupplierReportDto {
  total_revenue: number;
  items_sold: number;
  orders_count: number;
  average_order: number;
}

export interface SupplierCampaignStatsDto {
  campaign_id: string;
  impressions: number;
  clicks: number;
  spent_budget: number;
  ctr: number;
}

export interface SupplierOrderDto {
  items?: SupplierOrderItemDto[];
  id: string;
  orderId: string;
  orderNumber: string;
  customerName: string;
  customerPhone?: string;
  status: string;
  statusText: string;
  carrier?: string;
  trackingNumber?: string;
  shippedAt?: string;
  createdAt: string;
  itemCount: number;
}

export interface SupplierWarehouseOrderItemDto extends SupplierSKUIdentity {
  orderItemId?: string;
  productId: string;
  productTitle: string;
  quantity: number;
  wholesaleCost: number;
  lineTotal: number;
}

export interface SupplierWarehouseOrderDto {
  orderId: string;
  orderNumber: string;
  status: string;
  province: string;
  city: string;
  createdAt: string;
  items: SupplierWarehouseOrderItemDto[];
  itemsTotal: number;
}

export interface SupplierProposalDto {
  id: string;
  title: string;
  supplierBaseCost: number;
  barcode?: string;
  status: string;
  created_at: string;
  rejection_reason?: string;
}

export interface SupplierCampaignDto {
  id: string;
  campaign_name: string;
  budget: number;
  cpc_bid: number;
  spent_budget: number;
  impressions: number;
  clicks: number;
  status: string;
}

export interface SupplierPayoutDto {
  id: string;
  amount: number;
  status: string;
  transfer_reference?: string;
  shaba: string;
  bank_name: string;
  account_holder: string;
  created_at: string;
}

export interface SupplierSettlementBalance {
  earned: number;
  reserved: number;
  paid: number;
  returnCharges: number;
  returnClawbacks: number;
  adjustments: number;
  storageFees: number;
  available: number;
}

export interface SupplierStorageFeeCharge {
  id: string;
  productTitle?: string;
  warehouseName?: string;
  feeDate: string;
  qty: number;
  feeUnitDayToman: number;
  amountToman: number;
  goodsClass: string;
  estimated?: boolean;
}

export interface SupplierProfileFull {
  id: string;
  name: string;
  company_name: string;
  description: string;
  warehouse_city: string;
  warehouse_city_confirmed: boolean;
  logo_url: string;
}

export interface SupplierAddonDto {
  id: string;
  title: string;
  description: string;
  kind: string;
  priceMode: string;
  priceValue: number;
  capToman: number;
  approvalStatus: string;
  proposalStatus?: string;
  rejectionReason?: string;
  isActive: boolean;
  sortOrder: number;
  linkedProductId?: string;
  attachments?: { scopeType: string; productId: string }[];
}

export interface SupplierSearchInsightProduct { productId: string; title: string; imageUrl?: string }
export interface SupplierSearchInsightScopeMatch { type: 'category' | 'brand'; name: string }
export interface SupplierSearchDemandTerm { term: string; searches: number; zeroResultSearches: number; matchedProducts: SupplierSearchInsightProduct[] }
export interface SupplierSearchUnmetTerm { term: string; searches: number; zeroResultSearches: number; scopeMatches: SupplierSearchInsightScopeMatch[] }
export interface SupplierSearchTrendItem { productId: string; title: string; units: number; prevUnits: number; deltaPct: number | null }
export interface SupplierSearchInsight { windowDays: number; topDemand: SupplierSearchDemandTerm[]; unmetInScope: SupplierSearchUnmetTerm[]; ownProductTrend: SupplierSearchTrendItem[] }

export interface SupplierPriceIncreaseDto {
  id: string;
  productId: string;
  // Variant-targeted rows (governed variant pricing) carry the variant
  // they belong to; product-level rows leave both unset.
  variantId?: string;
  variantName?: string;
  oldReference: number;
  newReference: number;
  pct: number;
  status: 'pending' | 'approved' | 'rejected';
  isException: boolean;
  exceptionCapPct?: number;
  autoApproved: boolean;
  agreementReview: boolean;
  createdAt: string;
  reviewedAt?: string;
}

// PUT /supplier/products/{id}/pricing response. A governed increase that
// cannot auto-apply comes back HTTP 202 with status 'pending_review' and
// the Persian explanation in `message`; an applied increase carries
// applied/autoApproved (and requestId when an audit row was written).
export interface UpdatePricingResponse {
  success: boolean;
  supplier_base_cost?: number;
  applied?: boolean;
  autoApproved?: boolean;
  requestId?: string;
  status?: string;
  pct?: number;
  capPct?: number;
  reason?: string;
  message?: string;
}

// PUT /supplier/products/{id}/variants/{vid}/pricing response: the
// same governed shapes as UpdatePricingResponse (applied carries
// applied/autoApproved/requestId + supplier_base_cost; a queued
// increase is HTTP 202 with status 'pending_review' and the Persian
// explanation in `message`).
export type UpdateVariantPricingResponse = UpdatePricingResponse;

export interface SupplierNotification {
  id: string;
  kind: string;
  title: string;
  body: string;
  payload?: { proposal_id?: string; event?: string; entity_type?: string; product_title?: string };
  status: string;
  createdAt: string;
  sentAt?: string;
}

export interface SupplierNotificationsResponse {
  success?: boolean;
  notifications: SupplierNotification[];
  unreadCount: number;
}
export const supplierApi = {
  getProducts: () => apiRequest<SupplierProductDto[]>('/supplier/products', { method: 'GET', auth: true }),
  getProposals: () => apiRequest<SupplierProposalDto[]>('/supplier/products/proposals', { method: 'GET', auth: true }),
  // Reject-notification (DESIGN.md 4.1-4.2): supplier-scoped outbox
  // read endpoints. Contract mirrors the customer GET/POST
  // /api/v1/notifications[/read] envelope: {success, notifications:
  // [{id, kind, title, body, payload, status, createdAt, sentAt?}],
  // unreadCount}; read accepts {id}, {ids}, or empty body = mark-all
  // and answers {success, updated}. The backend builder is implementing
  // these routes in parallel - this client codes against that contract.
  // Defensive normalization: the server now always returns the full
  // envelope (success + unreadCount), including when the outbox table
  // is missing (P1-15), and a bare array is tolerated too -- the
  // fallback below is a safety net, not a live branch.
  getNotifications: async (): Promise<SupplierNotificationsResponse> => {
    const data = await apiRequest<any>('/supplier/notifications', { method: 'GET', auth: true });
    const notifications: SupplierNotification[] = Array.isArray(data) ? data : (Array.isArray(data?.notifications) ? data.notifications : []);
    const unreadCount: number = typeof data?.unreadCount === 'number'
      ? data.unreadCount
      : notifications.filter((n) => n.status !== 'read').length;
    return { success: data?.success, notifications, unreadCount };
  },
  markNotificationsRead: (ids?: string[]) => apiRequest<{ success?: boolean; updated?: number; unreadCount?: number }>('/supplier/notifications/read', { method: 'POST', auth: true, body: JSON.stringify(ids?.length ? { ids } : {}) }),
  proposeProduct: (payload: Record<string, unknown>) => apiRequest<{ id: string; status: string }>('/supplier/products/propose', { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  updateStock: (productId: string, newStock: number, reason: string, variantId?: string) => apiRequest<{ success: boolean; stock: number; productStock: number }>(`/supplier/products/${encodeURIComponent(productId)}/stock`, { method: 'PUT', auth: true, body: JSON.stringify({ new_stock: newStock, reason, variantId }) }),
  updatePricing: (productId: string, newSupplierBaseCost: number, reason?: string) => apiRequest<UpdatePricingResponse>(`/supplier/products/${encodeURIComponent(productId)}/pricing`, { method: 'PUT', auth: true, body: JSON.stringify({ new_supplier_base_cost: newSupplierBaseCost, reason }) }),
  // Gap 11 — price-increase governance: own request history + exception tickets.
  getPriceIncreases: () => apiRequest<SupplierPriceIncreaseDto[]>('/supplier/price-increases', { method: 'GET', auth: true }),
  createPriceIncreaseExceptionTicket: (productId: string, newReference: number, message?: string) => apiRequest<{ success: boolean; ticketId: string }>('/supplier/price-increases/exception-ticket', { method: 'POST', auth: true, body: JSON.stringify({ productId, newReference, message }) }),
  updateSupplyOption: (productId: string, supplyOption: SupplierProductDto['supplyOption']) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/supply-option`, { method: 'PUT', auth: true, body: JSON.stringify({ supply_option: supplyOption }) }),
  getInbounds: () => apiRequest<SupplierInboundDto[]>('/supplier/inbounds', { method: 'GET', auth: true }),
  createInbound: (payload: { sourceKind?: 'transfer'; supplierWarehouseId?: string; items: { product_id: string; quantity: number; carton_count: number }[]; carrier: string; vehicle_plate: string; notes?: string; warehouseId?: string; dispatchAt?: string }) => apiRequest<{ inbound_id: string }>('/supplier/inbounds/create', { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  getShipments: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/supplier/shipments', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.shipments ?? []);
  },
  getVariants: async (productId: string): Promise<SupplierProductVariantDto[]> => {
    const data = await apiRequest<any>(`/supplier/products/${encodeURIComponent(productId)}/variants`, { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.variants ?? []);
  },
  // FIX 47 — live gallery modal (Ll) + variants modal (Fl) clients.
  getProductImages: async (productId: string): Promise<any[]> => {
    const data = await apiRequest<any>(`/supplier/products/${encodeURIComponent(productId)}/images`, { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.images ?? []);
  },
  // P0: single variant-image upload (POST /upload/product-image, the
  // same endpoint the propose form uses). Returns the stored URL.
  uploadVariantImage: async (file: File, sessionToken?: string): Promise<{ imageUrl: string }> => {
    const form = new FormData();
    form.append('image', file);
    const token = sessionToken ?? tokenStore.getToken();
    const res = await fetch(`${getApiBase()}/upload/product-image`, {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
      signal: AbortSignal.timeout(75000),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as any)?.error || 'خطا در آپلود تصویر');
    if (!(data as any)?.success || !(data as any)?.imageUrl) throw new Error((data as any)?.error || 'خطا در آپلود تصویر');
    return data as { imageUrl: string };
  },
  // P0: variant gallery (product_variant_images, cap 4/variant).
  getVariantImages: async (productId: string, variantId: string): Promise<any[]> => {
    const data = await apiRequest<any>(`/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/images`, { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.images ?? []);
  },
  uploadVariantImages: async (productId: string, variantId: string, files: File[]): Promise<any> => {
    const form = new FormData();
    files.forEach((f) => form.append('images', f));
    const token = tokenStore.getToken();
    const res = await fetch(`${getApiBase()}/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/images`, {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
      signal: AbortSignal.timeout(75000),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as any)?.error || 'خطا در آپلود تصاویر تنوع');
    return data;
  },
  deleteVariantImage: (productId: string, variantId: string, imageId: string) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/images/${encodeURIComponent(imageId)}`, { method: 'DELETE', auth: true }),
  reorderVariantImages: (productId: string, variantId: string, orderedIds: string[]) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/images/reorder`, { method: 'PUT', auth: true, body: JSON.stringify({ orderedIds }) }),
  // P0: post-publish physicals edit (null clears back to inherit).
  updateVariantPhysical: (productId: string, variantId: string, payload: { weightGrams?: number | null; lengthMm?: number | null; widthMm?: number | null; heightMm?: number | null }) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/physical`, { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  // P1(c): post-publish sale edit (salePrice null clears the whole sale).
  updateVariantSale: (productId: string, variantId: string, payload: { salePrice?: number | null; saleFrom?: string | null; saleTo?: string | null }) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/sale`, { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  uploadProductImages: async (productId: string, files: File[]): Promise<any> => {
    const form = new FormData();
    files.forEach((f) => form.append('images', f));
    const token = tokenStore.getToken();
    const res = await fetch(`${getApiBase()}/supplier/products/${encodeURIComponent(productId)}/images`, {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
      signal: AbortSignal.timeout(75000),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as any)?.error || 'خطا در آپلود تصاویر');
    return data;
  },
  deleteProductImage: (productId: string, imageId: string) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/images/${encodeURIComponent(imageId)}`, { method: 'DELETE', auth: true }),
  setProductCoverImage: (productId: string, imageId: string) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/images/${encodeURIComponent(imageId)}/cover`, { method: 'PUT', auth: true }),
  reorderProductImages: (productId: string, orderedIds: string[]) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/images/reorder`, { method: 'PUT', auth: true, body: JSON.stringify({ orderedIds }) }),
  getLabels: async (productId: string): Promise<any> => {
    const data = await apiRequest<any>(`/supplier/products/${encodeURIComponent(productId)}/labels`, { method: 'GET', auth: true });
    return data;
  },
  createVariant: (productId: string, payload: { variant_name: string; attributes: Record<string, string> }) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/variants`, { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  createVariantsBulk: (productId: string, variants: { variant_name: string; attributes: Record<string, string> }[]) => apiRequest<{ count: number }>(`/supplier/products/${encodeURIComponent(productId)}/variants/bulk`, { method: 'POST', auth: true, body: JSON.stringify({ variants }) }),
  updateVariantCartLimits: (productId: string, variantId: string, minQty: number, maxQty: number | null) => apiRequest(`/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/cart-limits`, { method: 'PUT', auth: true, body: JSON.stringify({ min_cart_qty: minQty, max_cart_qty: maxQty }) }),
  // Gap 10 — variant's own supplier cost (null on the row = inherit the product cost); clear restores inheritance.
  updateVariantPricing: (productId: string, variantId: string, payload: { supplierBaseCost?: number; clear?: boolean; reason?: string }) => apiRequest<UpdateVariantPricingResponse>(`/supplier/products/${encodeURIComponent(productId)}/variants/${encodeURIComponent(variantId)}/pricing`, { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  // FIX 48 — live reviews modal (jl/rv) + feedback modal (zl) clients.
  getProductReviews: async (productId: string, limit = 10, offset = 0): Promise<any> => {
    const data = await apiRequest<any>(`/products/${encodeURIComponent(productId)}/reviews?limit=${limit}&offset=${offset}`, { method: 'GET' });
    return data;
  },
  getCatalogReview: (proposalId: string) => apiRequest<any>(`/supplier/catalog/review/${encodeURIComponent(proposalId)}`, { method: 'GET', auth: true }),
  resubmitCatalogReview: (proposalId: string, data: Record<string, unknown>) => apiRequest(`/supplier/catalog/review/${encodeURIComponent(proposalId)}/resubmit`, { method: 'POST', auth: true, body: JSON.stringify({ data }) }),
  createShipment: async (payload: { creation_key: string; warehouse_id: string; source?: {source_kind: string; supplier_warehouse_id?: string; external_reference?: string; reason?: string}; items: { variant_id: string; expected_qty: number }[] }): Promise<any> => {
    if (!payload.creation_key?.trim() || payload.creation_key.length > 128) throw new Error('Shipment creation key is required');
    const data = await apiRequest<any>('/supplier/shipments', { method: 'POST', auth: true, body: JSON.stringify(payload) });
    return data?.shipment ?? data;
  },
  changeShipmentDispatch: (shipment_id: string, action: 'dispatch' | 'cancel') => apiRequest<{success: boolean}>('/supplier/shipments', {method: 'POST', auth: true, body: JSON.stringify({shipment_id, action})}),

  getCentralWarehouses: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/supplier/warehouses/central', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : [];
  },
  dispatchInbound: (inboundId: string) => apiRequest(`/supplier/inbounds/${encodeURIComponent(inboundId)}/dispatch`, { method: 'POST', auth: true, body: JSON.stringify({}) }),
  cancelInbound: (inboundId: string) => apiRequest(`/supplier/inbounds/${encodeURIComponent(inboundId)}/cancel`, { method: 'POST', auth: true, body: JSON.stringify({}) }),
  getDiscrepancies: () => apiRequest<SupplierDiscrepancyDto[]>('/supplier/discrepancies', { method: 'GET', auth: true }),
  respondDiscrepancy: (id: string, response: string, resolutionOffer: string) => apiRequest(`/supplier/discrepancies/${encodeURIComponent(id)}/respond`, { method: 'POST', auth: true, body: JSON.stringify({ response, resolution_offer: resolutionOffer }) }),
  getSalesReport: (startDate?: string, endDate?: string) => apiRequest<SupplierReportDto>(`/supplier/sales-report${startDate || endDate ? `?start_date=${encodeURIComponent(startDate || '')}&end_date=${encodeURIComponent(endDate || '')}` : ''}`, { method: 'GET', auth: true }),
  requestSettlement: (amount: number, shaba: string, bankName: string, accountHolder: string) => financeOperation('/supplier/settlement/request', { amount, shaba, bank_name: bankName, account_holder: accountHolder }, 'supplier:settlement', value => value.success === true && typeof value.id === 'string' && !!value.id && value.payout_request_id === value.id),
  getSettlementHistory: () => apiRequest<SupplierPayoutDto[]>('/supplier/settlement/history', { method: 'GET', auth: true }),
  getStorageFeeCharges: () => apiRequest<{ charges: SupplierStorageFeeCharge[] }>('/supplier/settlement/storage-fees', { method: 'GET', auth: true }),
  getSupplierProfileFull: () => apiRequest<SupplierProfileFull>('/supplier/profile', { method: 'GET', auth: true }),
  updateSupplierProfile: (payload: { description?: string; warehouse_city?: string; logo_url?: string }) => apiRequest<SupplierProfileFull>('/supplier/profile', { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  uploadLogoImage: async (file: File): Promise<{ imageUrl: string }> => {
    const form = new FormData();
    form.append('image', file);
    const token = tokenStore.getToken();
    const res = await fetch(`${getApiBase()}/upload/product-image`, {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
      signal: AbortSignal.timeout(75000),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error((data as any)?.error || 'خطا در آپلود لوگو');
    if (!(data as any)?.success || !(data as any)?.imageUrl) throw new Error((data as any)?.error || 'خطا در آپلود لوگو');
    return data as { imageUrl: string };
  },
  getSettlementBalance: () => apiRequest<SupplierSettlementBalance>('/supplier/settlement/balance', { method: 'GET', auth: true }),
  getCampaigns: () => apiRequest<SupplierCampaignDto[]>('/supplier/campaigns', { method: 'GET', auth: true }),
  setCampaignStatus: (id: string, status: 'active' | 'paused') => apiRequest(`/supplier/campaigns/${encodeURIComponent(id)}/status`, { method: 'PUT', auth: true, body: JSON.stringify({ status }) }),
  createCampaign: (payload: Record<string, unknown>) => apiRequest<{ id: string; campaign_id: string }>('/supplier/campaigns/create', { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  getCampaignStats: (id: string) => apiRequest<SupplierCampaignStatsDto>(`/supplier/campaigns/${encodeURIComponent(id)}/stats`, { method: 'GET', auth: true }),
  // PERF-02 Step 9: cursor pages (default 50, max 100). The handler
  // still returns `success: true`; orders live under `orders`.
  getOrders: (params?: { limit?: number; cursor?: string }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.cursor) query.set('cursor', params.cursor);
    const suffix = query.toString() ? `?${query}` : '';
    return apiRequest<{ success: boolean; orders: SupplierOrderDto[]; nextCursor: string; hasMore: boolean; limit: number }>(`/supplier/orders${suffix}`, { method: 'GET', auth: true });
  },
  getOrderAddons: async (orderId: string) => { const r = await apiRequest<any>(`/supplier/orders/${encodeURIComponent(orderId)}/addons`, { method: "GET", auth: true }); return { addons: r.addons || [], total: r.total || 0 }; },
  // FIX 50 — live addons widget (Ql) clients.
  getAddons: async (): Promise<SupplierAddonDto[]> => { const r = await apiRequest<any>('/supplier/addons', { method: 'GET', auth: true }); return r.addons || []; },
  createAddon: (payload: Record<string, unknown>) => apiRequest<SupplierAddonDto>('/supplier/addons', { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  updateAddon: (id: string, payload: Record<string, unknown>) => apiRequest<{ staged: boolean }>(`/supplier/addons/${encodeURIComponent(id)}`, { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  setAddonActive: (id: string, isActive: boolean) => apiRequest(`/supplier/addons/${encodeURIComponent(id)}/active`, { method: 'PATCH', auth: true, body: JSON.stringify({ isActive }) }),
  setAddonAttachments: (id: string, productIds: string[]) => apiRequest(`/supplier/addons/${encodeURIComponent(id)}/attachments`, { method: 'PUT', auth: true, body: JSON.stringify({ attachments: productIds.map((productId) => ({ scopeType: 'product', productId })) }) }),
  submitShipping: (orderId: string, payload: { carrier?: string; trackingNumber?: string; shippingDate?: string; viaTapin?: boolean; tapinService?: 'post' | 'tipax'; tapinBoxId?: string }) => apiRequest<{ success: boolean; carrier?: string; trackingNumber?: string; tapin?: { service: string; boxId: string; trackingNumber: string } }>(`/supplier/orders/${encodeURIComponent(orderId)}/shipping`, { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  // Tapin connection (gaps 1-3). The token is write-only: it is sent
  // to the backend on save and NEVER returned by any endpoint.
  getTapinSettings: () => apiRequest<{ supplierId: string; tapinShopId: string | null; tapinConnected: boolean }>('/supplier/tapin/settings', { method: 'GET', auth: true }),
  updateTapinSettings: (payload: { shopId: string; token?: string }) => apiRequest<{ supplierId: string; tapinShopId: string | null; tapinConnected: boolean }>('/supplier/tapin/settings', { method: 'PUT', auth: true, body: JSON.stringify(payload) }),
  deleteTapinSettings: () => apiRequest<{ supplierId: string; tapinShopId: null; tapinConnected: boolean }>('/supplier/tapin/settings', { method: 'DELETE', auth: true }),
  // E6: optional signal lets the Tapin card cancel/time out the manual test; apiRequest still applies its own 15 s bound.
  testTapinConnection: (signal?: AbortSignal) => apiRequest<{ ok: boolean; serviceActive?: boolean; message: string; error?: string }>('/supplier/tapin/test', { method: 'POST', auth: true, body: JSON.stringify({}), signal }),
  getTapinBoxes: () => apiRequest<{ success: boolean; boxes: TapinBoxDto[]; defaultBoxId: string }>('/supplier/tapin/boxes', { method: 'GET', auth: true }),
  getTapinLabel: async (shipmentId: string): Promise<Blob> => {
    const token = tokenStore.getToken();
    const res = await fetch(`${getApiBase()}/supplier/shipments/${encodeURIComponent(shipmentId)}/tapin-label`, {
      method: 'GET',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) {
      let message = 'دریافت برچسب از تاپین انجام نشد.';
      try {
        const body = await res.json();
        if (body?.error) message = body.error;
      } catch { /* non-JSON error body */ }
      throw new Error(message);
    }
    return res.blob();
  },
  acceptOrder: (orderId: string) => apiRequest<{ success: boolean; status: string }>(`/supplier/orders/${encodeURIComponent(orderId)}/accept`, { method: 'POST', auth: true, body: JSON.stringify({}) }),
  getWarehouseOrders: (page = 1, pageSize = 20) => apiRequest<{ success: boolean; orders: SupplierWarehouseOrderDto[]; total: number; page: number; pageSize: number }>(`/supplier/warehouse-orders?page=${page}&pageSize=${pageSize}`, { method: 'GET', auth: true }),
  getReturns: () => apiRequest<SupplierReturnRequestDto[]>('/supplier/returns', { method:'GET', auth:true }),
  requestReturn: (productId:string, quantity:number, collectionMethod:'supplier_collect'|'postal', reason:string, idempotencyKey:string, variantId?:string) => apiRequest<SupplierReturnRequestDto>('/supplier/returns', { method:'POST', auth:true, headers:{'Idempotency-Key':idempotencyKey}, body:JSON.stringify({productId,quantity,collectionMethod,reason,variantId}) }),
  cancelReturn: (returnId:string, reason:string) => apiRequest(`/supplier/returns/${encodeURIComponent(returnId)}/cancel`, { method:'POST', auth:true, headers:{'Idempotency-Key':`cancel-${returnId}`}, body:JSON.stringify({reason}) }),
  getWarranties: () => apiRequest<WarrantyPropertyDto[]>('/supplier/warranties', { method:'GET', auth:true }),
  requestWarranty: (payload:{productId?:string;name:string;description:string;images:string[]}) => apiRequest<{success:boolean;id:string;status:string}>('/supplier/warranties', { method:'POST', auth:true, body:JSON.stringify(payload) }),
  getInventoryLocations: () => apiRequest<{success:boolean;inventory:SupplierInventoryLocationDto[]}>('/supplier/inventory', { method:'GET', auth:true }),
  getStockShare: () => apiRequest<any>('/supplier/stock-share', { method: 'GET', auth: true }),
  getCustomerReturns: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/supplier/customer-returns', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.returns ?? data?.data ?? []);
  },
  // FIX 44: live's An reads a.data.charges (snake_case return_id grouping).
  getReturnShippingCharges: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/supplier/return-shipping-charges', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.charges ?? []);
  },
  // FIX 44: live's An back-request actions (S / R / $ handlers).
  requestCustomerReturnBack: (returnItemId: string, quantity: number, reason: string, idempotencyKey: string) => apiRequest(`/supplier/customer-returns/${encodeURIComponent(returnItemId)}/request-back`, { method: 'POST', auth: true, headers: { 'Idempotency-Key': idempotencyKey }, body: JSON.stringify({ quantity, reason }) }),
  cancelReturnBack: (backId: string, reason: string) => apiRequest(`/supplier/customer-returns/back-requests/${encodeURIComponent(backId)}/cancel`, { method: 'POST', auth: true, headers: { 'Idempotency-Key': `cancel-back-${backId}` }, body: JSON.stringify({ reason }) }),
  completeReturnBack: (backId: string) => apiRequest(`/supplier/customer-returns/back-requests/${encodeURIComponent(backId)}/complete`, { method: 'POST', auth: true, headers: { 'Idempotency-Key': `complete-back-${backId}` }, body: JSON.stringify({}) }),
  getShippingMethods: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/supplier/shipping-methods', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.methods ?? data?.data ?? []);
  },
  requestShippingMethod: (method: string) => apiRequest("/supplier/shipping-methods/request", { method: "POST", auth: true, body: JSON.stringify({ method }) }),
  getMyWarehouses: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/supplier/warehouses', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.warehouses ?? data?.data ?? []);
  },
  getProfile: () => apiRequest<any>('/supplier/profile', { method: 'GET', auth: true }),
  acceptContract: (version: number) => apiRequest('/supplier/contract/accept', { method: 'POST', auth: true, body: JSON.stringify({ version }) }),
  updateProductOrigin: (id: string, warehouseId: string) => apiRequest(`/supplier/products/${encodeURIComponent(id)}/origin`, { method: 'PUT', auth: true, body: JSON.stringify({ supplierWarehouseId: warehouseId }) }),
  // G4 — bulk origin assign: omitted productIds assigns ALL NULL-origin
  // supplier_direct products of the caller; JSON drops undefined, so an
  // omitted productIds stays omitted on the wire.
  bulkAssignProductOrigin: (warehouseId: string, productIds?: string[]) => apiRequest<{ assigned: number; warehouseId: string }>('/supplier/products/origin/bulk', { method: 'POST', auth: true, body: JSON.stringify({ warehouseId, productIds }) }),
  getMyExclusivityChallenges: async (): Promise<any[]> => { const data = await apiRequest<any>('/supplier/exclusivity-challenges', { method: 'GET', auth: true }); return Array.isArray(data) ? data : (data?.challenges ?? []); },
  // FIX 52 — live challenge-create (Jl challenge box).
  createExclusivityChallenge: (payload: Record<string, unknown>) => apiRequest<{ id: string; status: string }>('/supplier/exclusivity-challenges', { method: 'POST', auth: true, body: JSON.stringify(payload) }),
  createMyWarehouse: (input: { name: string; provinceName: string; cityName: string; address?: string; postalCode?: string; phone?: string }) => apiRequest("/supplier/warehouses", { method: "POST", auth: true, body: JSON.stringify(input) }),
  updateMyWarehouse: (id: string, input: { name: string; provinceName: string; cityName: string; address?: string; postalCode?: string; phone?: string }) => apiRequest(`/supplier/warehouses/${encodeURIComponent(id)}`, { method: "PUT", auth: true, body: JSON.stringify(input) }),
  getAnalytics: async (): Promise<any> => apiRequest<any>('/supplier/analytics', { method: 'GET', auth: true }),
  getSearchInsight: async (days: number = 30): Promise<SupplierSearchInsight> => apiRequest<SupplierSearchInsight>(`/supplier/search-insight?days=${days}`, { method: 'GET', auth: true }),
};

export const warehouseReturnsApi = {
  list: () => apiRequest<{success:boolean;returns:SupplierReturnRequestDto[]}>('/warehouse/returns', { method:'GET', auth:true }),
  review: (returnId:string, status:'approved'|'rejected', reason:string) => apiRequest(`/warehouse/returns/${encodeURIComponent(returnId)}/review`, { method:'POST', auth:true, headers:{'Idempotency-Key':`review-${returnId}-${status}`}, body:JSON.stringify({status,reason}) }),
  createParcel: (returnId:string, payload:ReturnParcelInput) => apiRequest<ReturnParcelDto>(`/warehouse/returns/${encodeURIComponent(returnId)}/parcels`, { method:'POST', auth:true, headers:{'Idempotency-Key':`parcel-${returnId}-${payload.quantity}-${payload.carrierCode}`}, body:JSON.stringify(payload) }),
  confirmCharge: (returnId:string, parcelId:string) => apiRequest(`/warehouse/returns/${encodeURIComponent(returnId)}/parcels/${encodeURIComponent(parcelId)}/confirm-charge`, { method:'POST', auth:true, headers:{'Idempotency-Key':`charge-${parcelId}`} }),
  handover: (returnId:string) => apiRequest(`/warehouse/returns/${encodeURIComponent(returnId)}/handover`, { method:'POST', auth:true, headers:{'Idempotency-Key':`handover-${returnId}`} }),
  receive: (returnId:string) => apiRequest(`/warehouse/returns/${encodeURIComponent(returnId)}/receive`, { method:'POST', auth:true, headers:{'Idempotency-Key':`receive-${returnId}`} }),
  inspect: (returnId:string, acceptedQuantity:number, failedQuantity:number, reason:string) => apiRequest(`/warehouse/returns/${encodeURIComponent(returnId)}/inspect`, { method:'POST', auth:true, headers:{'Idempotency-Key':`inspect-${returnId}-${acceptedQuantity}-${failedQuantity}`}, body:JSON.stringify({acceptedQuantity,failedQuantity,reason}) }),
  getParcels: (returnId:string) => apiRequest<{success:boolean;parcels:ReturnParcelDto[]}>(`/warehouse/returns/${encodeURIComponent(returnId)}/parcels`, { method:'GET', auth:true }),
  getFinancialEvents: (returnId:string) => apiRequest<{success:boolean;events:ReturnFinancialEventDto[]}>(`/warehouse/returns/${encodeURIComponent(returnId)}/financial-events`, { method:'GET', auth:true }),
  getCarrierOptions: () => apiRequest<{success:boolean;carriers:CarrierOptionDto[]}>('/warehouse/carriers', { method:'GET', auth:true }),
  quote: (payload:{carrierCode:string;originRegion:string;destinationRegion:string;sizeClass:string;weightGrams:number}) => apiRequest<{carrierCode:string;source:string;tariffVersion:number;baseWeightGrams:number;charge:number;roundingRule:string}>('/warehouse/carriers/quote', { method:'POST', auth:true, body:JSON.stringify(payload) }),
};
