import { apiRequest } from './apiClient';
import { Store, Order } from '../types';

export interface RegisterStorePayload {
  storeName: string;
  slug: string;
  ownerName: string;
  profitMarginPercent: number;
  referralCode?: string;
}

export interface StorePricingPayload {
  type?: 'product' | 'niche' | 'store_margin';
  targetId?: string;
  value?: number;
  mode?: 'percent';
  profitMarginPercent?: number;
  productOverrides?: Record<string, { value: number; mode: 'percent' }>;
  nicheOverrides?: Record<string, { value: number; mode: 'percent' }>;
}

export interface ReferralBonusNotice {
  id: string;
  referredStoreId: string;
  referredStoreName: string;
  amount: number;
  releasedAt: string;
  message: string;
}

export interface StoreReferralItem {
  id: string;
  name: string;
  slug: string;
  ownerName: string;
  totalSales: number;
  isActivated?: boolean;
  bonusStatus?: 'locked' | 'released' | 'none';
  bonusAmount?: number;
  earnedCommission?: number;
  totalPlatformFee?: number;
}

export interface StoreBalanceResponse {
  storeId: string;
  balance: number;
  unsettledAmount: number;
  availableAmount: number;
  bonusLocked: number;
  bonusAvailable: number;
  bonusTotal: number;
  referralCommissionEarned: number;
  lastReleasedAt?: string;
}

export interface WalletBreakdownResponse {
  storeId: string;
  pendingProfit: number;
  releasedProfit: number;
  availableAmount: number;
  pendingEarlyGross: number;
  settlementSettings?: { settlementDay?: number; earlyWithdrawalFeePct?: number };
}

export interface EarlyWithdrawalResponse {
  id: string;
  storeId: string;
  grossAmount: number;
  feePct: number;
  feeAmount: number;
  netAmount: number;
  shaba: string;
  bankName: string;
  status: string;
  rejectionReason?: string;
  payoutRequestId?: string;
  createdAt: string;
  processedAt?: string;
  deadlineAt?: string;
}

export interface SellerReportResponse { revenue: number; profit: number; orders: number; units: number; pendingProfit?: number; }
export interface SellerTicket { id: string; subject: string; status: string; createdAt: string; messages: string[]; }
export interface SellerCampaign { id: string; storeId: string; name: string; type: 'widget'|'banner'|'link'; status: 'draft'|'active'|'paused'|'archived'; productIds: string[]; configuration: Record<string, unknown>; }
export interface SellerCampaignAnalytics { campaignId: string; impressions: number; clicks: number; ctr: number; }

export interface StoreTemplate {
  id: string;
  name: string;
  description: string;
  thumbnailUrl: string;
  isActive: boolean;
}

export interface SellerStoreTemplate {
  storeId: string;
  templateId: string;
  templateConfig: Record<string, unknown>;
}

// Merge-phase backend: live shape the rebuilt SellerAnalyticsTab reads
// (its own LiveSellerAnalytics interface); the old shape is retired
// with the handler reshape.
export interface SellerAnalytics {
  totalRevenue: number;
  orderCount: number;
  avgOrderValue: number;
  sellerProfit: number;
  views?: number;
  conversionRate?: number;
  periodComparison?: { current: { revenue: number; orders: number; avgOrderValue: number; profit: number }; previous: { revenue: number; orders: number; avgOrderValue: number; profit: number } };
  statusBreakdown?: Array<{ status: string; count: number }>;
  daily?: Array<{ date: string; revenue: number; orders: number }>;
  topProducts?: Array<{ productId: string; name: string; quantity: number; revenue: number }>;
  productRows?: Array<{ productId: string; name: string; quantity: number; revenue: number; profit: number; returnRate: number; returnedQty: number; views?: number; conversionRate?: number; reviewsCount: number; rating: number; stock: number; daysLeft: number | null }>;
}

// Merge-phase backend: live bare shape the rebuilt
// SellerSearchInsightTab reads (no wrapper).
export interface SearchInsightTerm {
  term: string;
  searches: number;
  zeroResultSearches: number;
}

export interface SearchInsightCatalogMatch {
  productId: string;
  title: string;
  imageUrl?: string;
}

export interface SearchInsightUnmetTerm extends SearchInsightTerm {
  catalogMatches: SearchInsightCatalogMatch[];
}

export interface SellerSearchInsight {
  topSearches: SearchInsightTerm[];
  unmetInStore: SearchInsightUnmetTerm[];
  unmetCatalog: SearchInsightTerm[];
}

export interface DiscountCampaign {
  id: string;
  storeId?: string;
  name: string;
  scope: 'product' | 'niche' | 'store';
  targetId: string;
  mode: 'percent' | 'fixed';
  value: number;
  startsAt: string;
  endsAt: string;
  isActive: boolean;
}

export interface DiscountCampaignPayload {
  name: string;
  scope: 'product' | 'niche' | 'store';
  targetId: string;
  mode: 'percent' | 'fixed';
  value: number;
  startsAt: string;
  endsAt: string;
  isActive: boolean;
}

export interface DiscountCode {
  id: string;
  storeId: string;
  code: string;
  mode: 'percent' | 'fixed';
  value: number;
  startsAt: string;
  endsAt: string;
  isActive: boolean;
  maxUses?: number;
  usedCount: number;
  createdAt: string;
}

export interface DiscountCodePayload {
  code: string;
  mode: 'percent' | 'fixed';
  value: number;
  startsAt: string;
  endsAt: string;
  maxUses?: number;
}

export interface StoreSubscriber {
  id?: string;
  mobile?: string;
  email?: string;
  status?: string;
}

export const storeApi = {
  saveAdWidget: (slug: string, config: NonNullable<Store['sellerDealsWidgetConfig']>) => apiRequest<NonNullable<Store['sellerDealsWidgetConfig']>>(`/stores/${encodeURIComponent(slug)}/ad-widget`, { method: 'PUT', auth: true, body: JSON.stringify(config) }),
  getSellerCampaigns: async (slug: string): Promise<SellerCampaign[]> => apiRequest<SellerCampaign[]>(`/stores/${encodeURIComponent(slug)}/campaigns`, { auth: true }),
  getSellerCampaignAnalytics: async (slug: string, id: string): Promise<SellerCampaignAnalytics> => apiRequest<SellerCampaignAnalytics>(`/stores/${encodeURIComponent(slug)}/campaigns/${id}/analytics`, { auth: true }),
  setSellerCampaignStatus: async (slug: string, id: string, status: SellerCampaign['status']): Promise<void> => apiRequest<void>(`/stores/${encodeURIComponent(slug)}/campaigns/${id}`, { method: 'PATCH', auth: true, body: JSON.stringify({ status }) }),
  updateSellerCampaign: async (slug: string, id: string, payload: { name: string; productIds: string[] }): Promise<void> => apiRequest<void>(`/stores/${encodeURIComponent(slug)}/campaigns/${id}`, { method: 'PATCH', auth: true, body: JSON.stringify(payload) }),
  getSellerCampaignHistory: async (slug: string, id: string): Promise<Array<{ action: string; createdAt: string }>> => apiRequest(`/stores/${encodeURIComponent(slug)}/campaigns/${id}/history`, { auth: true }),
  getPublicSellerCampaigns: async (slug: string): Promise<SellerCampaign[]> => apiRequest<SellerCampaign[]>(`/stores/${encodeURIComponent(slug)}/public-campaigns`),
  recordSellerAdEvent: async (campaignId: string, type: 'impression'|'click', token: string, productId?: string): Promise<{ accepted: boolean }> => apiRequest<{ accepted: boolean }>('/ads/events', { method: 'POST', body: JSON.stringify({ campaignId, type, token, productId }) }),
  getAllStores: async (): Promise<Store[]> => {
    return apiRequest<Store[]>('/stores', { method: 'GET' });
  },

  getOwnedStores: async (): Promise<Store[]> => {
    return apiRequest<Store[]>('/stores/mine', { method: 'GET', auth: true });
  },

  getStoreBySlug: async (slug: string): Promise<Store> => {
    return apiRequest<Store>(`/stores/${encodeURIComponent(slug)}`, { method: 'GET' });
  },

  registerStore: async (payload: RegisterStorePayload): Promise<Store> => {
    return apiRequest<Store>('/stores/register', {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  updateStorePricing: async (slug: string, payload: StorePricingPayload): Promise<any> => {
    return apiRequest<any>(`/stores/${encodeURIComponent(slug)}/pricing`, {
      method: 'PUT',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  // Seller variant margins (percent-only, D28).
  getSellerVariantOverrides: async (storeId: string): Promise<Array<{ storeId: string; productId: string; variantId: string; value: number; mode: string }>> => {
    // The backend wraps the list ({ overrides: [...] }) — unwrap it here so
    // callers always receive the bare array. A wrapped object is not
    // iterable, which silently restored nothing in the pricing modal.
    const data: any = await apiRequest('/seller/variant-overrides?storeId=' + encodeURIComponent(storeId), {
      method: 'GET',
      auth: true,
    });
    if (Array.isArray(data)) return data;
    if (data && Array.isArray(data.overrides)) return data.overrides;
    return [];
  },

  setSellerVariantOverride: async (payload: { storeId: string; productId: string; variantId: string; value: number; mode: 'percent' }): Promise<any> => {
    return apiRequest<any>('/seller/variant-overrides', {
      method: 'PUT',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  deleteSellerVariantOverride: async (storeId: string, productId: string, variantId: string): Promise<any> => {
    return apiRequest<any>('/seller/variant-overrides?storeId=' + encodeURIComponent(storeId) + '&productId=' + encodeURIComponent(productId) + '&variantId=' + encodeURIComponent(variantId), {
      method: 'DELETE',
      auth: true,
    });
  },

  getStoreOrders: async (slug: string): Promise<Order[]> => {
    return apiRequest<Order[]>(`/stores/${encodeURIComponent(slug)}/orders`, {
      method: 'GET',
      auth: true,
    });
  },

  saveStoreSettings: async (slug: string, config: Record<string, unknown>): Promise<Store> => apiRequest<Store>(`/stores/${encodeURIComponent(slug)}/settings`, { method: 'PUT', auth: true, body: JSON.stringify(config) }),
  saveStoreProducts: async (slug: string, productIds: string[]): Promise<Store> => apiRequest<Store>(`/stores/${encodeURIComponent(slug)}/products`, { method: 'PUT', auth: true, body: JSON.stringify({ productIds }) }),
  purchaseService: async (slug: string, service: string): Promise<Store> => apiRequest<Store>(`/stores/${encodeURIComponent(slug)}/services`, { method: 'POST', auth: true, body: JSON.stringify({ service }) }),
  getOwnedBalance: async (slug: string): Promise<StoreBalanceResponse> => apiRequest<StoreBalanceResponse>(`/stores/${encodeURIComponent(slug)}/balance`, { auth: true }),
  getPayouts: async (slug: string): Promise<import('../types').PayoutRequest[]> => apiRequest<import('../types').PayoutRequest[]>(`/stores/${encodeURIComponent(slug)}/payouts`, { auth: true }),
  requestPayout: async (slug: string, amount: number, shaba: string): Promise<unknown> => apiRequest(`/stores/${encodeURIComponent(slug)}/payouts`, { method: 'POST', auth: true, body: JSON.stringify({ amount, shaba }) }),
  // Team Settlement (Phase 3): seller wallet breakdown (pending vs
  // released profit) and early-withdrawal request/list. The seller
  // wallet tab UI for early withdrawal is still to be wired to
  // these calls; the API itself is live in this branch.
  getWalletBreakdown: async (slug: string): Promise<WalletBreakdownResponse> => apiRequest<WalletBreakdownResponse>(`/stores/${encodeURIComponent(slug)}/wallet`, { auth: true }),
  getEarlyWithdrawals: async (slug: string): Promise<EarlyWithdrawalResponse[]> => apiRequest<EarlyWithdrawalResponse[]>(`/stores/${encodeURIComponent(slug)}/early-withdrawals`, { auth: true }),
  requestEarlyWithdrawal: async (slug: string, amount: number, shaba: string): Promise<EarlyWithdrawalResponse> => apiRequest<EarlyWithdrawalResponse>(`/stores/${encodeURIComponent(slug)}/early-withdrawals`, { method: 'POST', auth: true, body: JSON.stringify({ amount, shaba }) }),
  // FIX 17: seller returns tab. Backend serves GET
  // /stores/{slug}/returns for authorised store reads; no client
  // function existed yet. Read-only list for the tab.
  getStoreReturns: async (slug: string): Promise<any[]> => {
    const data = await apiRequest<any>(`/stores/${encodeURIComponent(slug)}/returns`, { auth: true });
    return Array.isArray(data) ? data : (data?.returns ?? data?.data ?? []);
  },
  getSellerReport: async (slug: string, start?: string, end?: string): Promise<SellerReportResponse> => {
    const params = new URLSearchParams(); if (start) params.set('start', start); if (end) params.set('end', end);
    return apiRequest<SellerReportResponse>(`/stores/${encodeURIComponent(slug)}/report?${params}`, { method: 'GET', auth: true });
  },
  getSellerTickets: async (): Promise<SellerTicket[]> => apiRequest<SellerTicket[]>('/seller/tickets', { method: 'GET', auth: true }),
  createSellerTicket: async (subject: string, message: string): Promise<SellerTicket[]> => apiRequest<SellerTicket[]>('/seller/tickets', { method: 'POST', auth: true, body: JSON.stringify({ subject, message }) }),

  getStoreReferrals: async (slug: string): Promise<StoreReferralItem[]> => {
    return apiRequest<StoreReferralItem[]>(`/stores/${encodeURIComponent(slug)}/referrals`, {
      method: 'GET',
      auth: true,
    });
  },

  // Team 05 (Phase 2, D49): live requestStoreChange. Files a
  // name/slug/custom-domain change request on the server
  // (POST /stores/:slug/change-requests {kind, value}); the
  // backend stores it as a pending approval. TODO U40: the
  // admin approval flow itself is unverified in live, so this
  // only files the request.
  requestStoreChange: async (slug: string, payload: { kind: 'store_name' | 'store_slug' | 'custom_domain'; value: string }): Promise<Store> => apiRequest<Store>(`/stores/${encodeURIComponent(slug)}/change-requests`, { method: 'POST', auth: true, body: JSON.stringify(payload) }),

  updateSupplyOption: async (productId: string, supplyOption: '3hour_local' | '1day' | '3day'): Promise<{ success: boolean; supply_option: string }> => {
    return apiRequest<{ success: boolean; supply_option: string }>(`/supplier/products/${productId}/supply-option`, {
      method: 'PUT',
      auth: true,
      body: JSON.stringify({ supply_option: supplyOption }),
    });
  },

  getStoreBalance: async (storeId: string): Promise<StoreBalanceResponse> => {
    return apiRequest<StoreBalanceResponse>(`/finance/stores/${encodeURIComponent(storeId)}/balance`, {
      method: 'GET',
      auth: true,
    });
  },

  getReferralNotifications: async (slug: string): Promise<ReferralBonusNotice[]> => {
    return apiRequest<ReferralBonusNotice[]>(`/stores/${encodeURIComponent(slug)}/referral-notifications`, {
      method: 'GET',
      auth: true,
    });
  },
  // ============ BEGIN TEAM 08 (live parity) ============
  // Team 08 owns the discount-campaigns + template calls in this file.
  // Team 05 adds requestStoreChange to the same file; the supervisor
  // resolves that file at integration, so all Team 08 additions stay
  // inside this one clearly-delimited block. Nothing outside it is
  // Team 08's.
  // Template API (D04, Team 01 backend): GET /store-templates,
  // GET/PUT /seller/store/template, POST /seller/store/template/publish.
  getStoreTemplates: async (): Promise<{ templates: StoreTemplate[] }> => {
    const data = await apiRequest<{ templates: StoreTemplate[] } | StoreTemplate[]>('/store-templates', { method: 'GET' });
    return Array.isArray(data) ? { templates: data } : data;
  },
  getSellerStoreTemplate: async (slug?: string): Promise<SellerStoreTemplate> => {
    const qs = slug ? `?slug=${encodeURIComponent(slug)}` : '';
    return apiRequest<SellerStoreTemplate>(`/seller/store/template${qs}`, { method: 'GET', auth: true });
  },
  updateSellerStoreTemplate: async (payload: { templateId: string; templateConfig?: Record<string, unknown> }): Promise<SellerStoreTemplate> => {
    return apiRequest<SellerStoreTemplate>('/seller/store/template', { method: 'PUT', auth: true, body: JSON.stringify(payload) });
  },
  publishSellerStoreTemplate: async (payload: { slug?: string; templateId?: string; templateConfig?: Record<string, unknown> }): Promise<SellerStoreTemplate> => {
    return apiRequest<SellerStoreTemplate>('/seller/store/template/publish', { method: 'POST', auth: true, body: JSON.stringify(payload) });
  },
  // Seller analytics + search insight (D47, Team 01 backend).
  getSellerAnalytics: async (storeId?: string): Promise<SellerAnalytics> => {
    const qs = storeId ? `?storeId=${encodeURIComponent(storeId)}` : '';
    return apiRequest<SellerAnalytics>(`/seller/analytics${qs}`, { method: 'GET', auth: true });
  },
  getSellerSearchInsight: async (): Promise<SellerSearchInsight> => {
    return apiRequest<SellerSearchInsight>('/seller/search-insight', { method: 'GET', auth: true });
  },
  // Discount campaigns (D48, MANAGEMENT ONLY): GET/POST/PATCH/DELETE
  // /stores/<slug>/discount-campaigns. No price application, no
  // discount-code calls, no checkout input. NOTE: this backend CRUD
  // route is not present in the base tree (Team 05 owns it); until it
  // lands these calls fail and the tab shows the error, not fake data.
  getDiscountCampaigns: async (slug: string): Promise<DiscountCampaign[]> => {
    const data = await apiRequest<DiscountCampaign[] | { campaigns: DiscountCampaign[] }>(`/stores/${encodeURIComponent(slug)}/discount-campaigns`, { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data.campaigns || []);
  },
  createDiscountCampaign: async (slug: string, payload: DiscountCampaignPayload): Promise<DiscountCampaign> => {
    return apiRequest<DiscountCampaign>(`/stores/${encodeURIComponent(slug)}/discount-campaigns`, { method: 'POST', auth: true, body: JSON.stringify(payload) });
  },
  updateDiscountCampaign: async (slug: string, id: string, payload: DiscountCampaignPayload): Promise<DiscountCampaign> => {
    return apiRequest<DiscountCampaign>(`/stores/${encodeURIComponent(slug)}/discount-campaigns/${encodeURIComponent(id)}`, { method: 'PATCH', auth: true, body: JSON.stringify(payload) });
  },
  deleteDiscountCampaign: async (slug: string, id: string): Promise<void> => {
    return apiRequest<void>(`/stores/${encodeURIComponent(slug)}/discount-campaigns/${encodeURIComponent(id)}`, { method: 'DELETE', auth: true });
  },
  // Store subscribers (D47, live frontend path). NOTE: this backend
  // route is not present in the base tree (Team 05 owns it); until it
  // lands this call fails and the tab shows the error, not fake data.
  getStoreSubscribers: async (slug: string): Promise<StoreSubscriber[]> => {
    const data = await apiRequest<StoreSubscriber[] | { subscribers: StoreSubscriber[] }>(`/stores/${encodeURIComponent(slug)}/subscribers`, { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data.subscribers || []);
  },
  // ============ TEAM DISCOUNTS (Phase 3) ============
  // Discount codes (new) + discount quote. Campaign CRUD above
  // (Team 08) is reused unchanged; no duplicate campaign routes.
  // Codes toggle uses PUT on the collection {id, isActive}
  // (old-branch shape); campaigns use PATCH /:id.
  getDiscountCodes: async (slug: string): Promise<DiscountCode[]> => {
    const data = await apiRequest<DiscountCode[] | { codes: DiscountCode[] }>(`/stores/${encodeURIComponent(slug)}/discount-codes`, { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data.codes || []);
  },
  createDiscountCode: async (slug: string, payload: DiscountCodePayload): Promise<DiscountCode> => {
    return apiRequest<DiscountCode>(`/stores/${encodeURIComponent(slug)}/discount-codes`, { method: 'POST', auth: true, body: JSON.stringify(payload) });
  },
  setDiscountCodeActive: async (slug: string, id: string, isActive: boolean): Promise<unknown> => {
    return apiRequest(`/stores/${encodeURIComponent(slug)}/discount-codes`, { method: 'PUT', auth: true, body: JSON.stringify({ id, isActive }) });
  },
  quoteDiscount: async (slug: string, payload: { items: { productId: string; variantId?: string; quantity: number }[]; discountCode?: string }): Promise<{ payableAmount: number; originalPayable: number; sellerDiscount: number; discountCode?: string; discountCodeValid: boolean }> => {
    return apiRequest(`/stores/${encodeURIComponent(slug)}/discount-quote`, { method: 'POST', body: JSON.stringify(payload) });
  },
  // ============ END TEAM 08 (live parity) ============
};
