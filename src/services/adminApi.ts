import { apiRequest } from './apiClient';
import { PendingApprovalItem, AuditLogEntry } from '../types';

export interface PendingApprovalDto {
  id: string;
  entityType: string;
  targetId: string;
  title: string;
  requestedData: Record<string, any>;
  requestedBy: string;
  status: 'pending' | 'approved' | 'rejected';
  rejectionReason?: string;
  createdAt: string;
  reviewedAt?: string;
}

export interface ReviewApprovalPayload {
  status: 'approved' | 'rejected';
  rejectionReason?: string;
}

export interface AuditLogDto {
  id: string;
  timestamp: string;
  operator: string;
  action: string;
  category: string;
  details: string;
}

export type HistoricalReviewStatus = 'needs_review' | 'in_review' | 'awaiting_evidence' | 'approved_for_correction' | 'rejected' | 'resolved' | 'indeterminate';
export interface HistoricalLocationDetail { locationKey:string; locationType:string; warehouseName?:string; binCode?:string; status:string; quantity:number; reconciled:boolean; }
export interface HistoricalReservationDetail { id:string; locationKey:string; quantity:number; referenceId:string; status:string; }
export interface HistoricalMovementDetail { id:string; quantity:number; fromStatus?:string; toStatus?:string; fromLocationKey?:string; toLocationKey?:string; referenceType:string; referenceId:string; createdAt:string; }
export interface HistoricalMeasurementEvidenceDetail { id:string; newLengthMm:number; newWidthMm:number; newHeightMm:number; newWeightGrams:number; reason:string; actorUserId:string; createdAt:string; }
export interface HistoricalInventoryReportRow { productId:string; productTitle:string; supplierId?:string; legacyStock:number; canonicalQuantity:number; canonicalSellableQuantity:number; physicalQuantity:number; canonicalRows:number; unreconciledRows:number; movementQuantity:number; measurementStatus:string; hasMeasurement:boolean; hasInboundEvidence:boolean; hasQCEvidence:boolean; hasPutawayEvidence:boolean; hasLedgerEvidence:boolean; classifications:string[]; classification:string; evidenceInsufficient:boolean; unresolved:boolean; suggestedAction:string; difference:number; physicalInventory:boolean; measurementClassification:string; locationBreakdown:HistoricalLocationDetail[]; activeReservations:HistoricalReservationDetail[]; movements:HistoricalMovementDetail[]; measurementEvidence:HistoricalMeasurementEvidenceDetail[]; evidenceIds:string[]; evidenceTotal?:number; evidenceTruncated?:boolean; reviewStatus:HistoricalReviewStatus; reviewId?:string; }

export interface SettlementSettingsDto {
  settlementDay: number;
  earlyWithdrawalFeePct: number;
}

export interface SettlementSettingsPayload {
  settlementDay: number | null;
  earlyWithdrawalFeePct: number | null;
}

// Storage-fee settings (gap 7): GET/PUT /admin/settings/storage-fees.
// Amounts are integer Tomans. The flag ships OFF; activation is a
// separate owner decision. POST /admin/storage-fees/run executes one
// assessment pass and refuses while the flag is off.
export interface StorageFeeSettingsDto {
  storageFeeEnabled: boolean;
  storageFeeFreeDays: number;
  storageFeePerKgDay: number;
  storageFeePerM3Day: number;
}

export interface StorageFeeRunSummaryDto {
  moreWork?: boolean;
  verifiedReceiptHistoryOnly?: boolean;
  unresolvedSampleLimited?: boolean;
  positionsCharged: number;
  positionsSkippedNoMeasurement: number;
  chargesCreated: number;
  totalCharged: number;
}

export interface AdminPriceIncreaseDto {
  id: string;
  productId: string;
  supplierId: string;
  productTitle: string;
  supplierName: string;
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

export interface TapinBoxAdminDto { id: string; title: string; lengthCm: number; widthCm: number; heightCm: number; maxWeightGrams?: number | null; isActive: boolean; sort: number }

// D1 (Batch 4a): read-only list DTOs. Field names match the backend
// envelopes (camelCase, except reviews: the generic list handler returns
// raw product_reviews columns, snake_case, via t01ListRows).
export interface HomepageSectionDto {
  id: string;
  sectionType: string;
  title?: string | null;
  config?: Record<string, any> | null;
  sortOrder: number;
  isActive: boolean;
}

export interface AdminBannerDto {
  id: string;
  title: string;
  subtitle?: string;
  imageUrl: string;
  linkUrl?: string;
  position: string;
  sortOrder: number;
  isActive: boolean;
  startsAt?: string | null;
  endsAt?: string | null;
}

export interface NewsletterSubscriberDto {
  id: string;
  storeId: string;
  mobile: string;
  email?: string;
  status: string;
  source?: string;
  createdAt: string;
  unsubscribedAt?: string | null;
}

export type AdminReviewRow = Record<string, any>;

export interface AdminStaffDto {
  id: string;
  mobile: string;
  fullName: string;
  role: string;
  username?: string;
  isActive: boolean;
  createdAt: string;
}

export interface CallbackRetryDto {
  id: string;
  pspReference: string;
  orderId?: string;
  orderNumber?: string;
  attemptCount: number;
  maxAttempts: number;
  status: string;
  lastError?: string;
  nextRetryAt?: string;
  createdAt: string;
  updatedAt: string;
  resolvedAt?: string | null;
  eventStatus?: string;
  eventError?: string;
  eventRetryCount?: number;
  eventReceivedAt?: string | null;
}

export const adminApi = {
  getApprovals: async (status?: string): Promise<PendingApprovalDto[]> => {
    const qs = status ? `?status=${encodeURIComponent(status)}` : '';
    return apiRequest<PendingApprovalDto[]>(`/admin/approvals${qs}`, {
      method: 'GET',
      auth: true,
    });
  },

  createApproval: async (payload: Partial<PendingApprovalDto>): Promise<PendingApprovalDto> => {
    return apiRequest<PendingApprovalDto>('/admin/approvals', {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  reviewApproval: async (
    id: string,
    payload: ReviewApprovalPayload
  ): Promise<{ success: boolean; id: string; status: string }> => {
    return apiRequest<{ success: boolean; id: string; status: string }>(
      `/admin/approvals/${encodeURIComponent(id)}/review`,
      {
        method: 'POST',
        auth: true,
        body: JSON.stringify(payload),
      }
    );
  },

  // Gap 11 — price-increase governance: review queue + exception grants.
  getPriceIncreases: async (status: string = 'pending'): Promise<AdminPriceIncreaseDto[]> => {
    return apiRequest<AdminPriceIncreaseDto[]>(`/admin/price-increases?status=${encodeURIComponent(status)}`, {
      method: 'GET',
      auth: true,
    });
  },

  reviewPriceIncrease: async (id: string, approved: boolean): Promise<{ success: boolean; id: string; status: string }> => {
    return apiRequest<{ success: boolean; id: string; status: string }>(
      `/admin/price-increases/${encodeURIComponent(id)}/review`,
      { method: 'POST', auth: true, body: JSON.stringify({ approved }) }
    );
  },

  grantPriceException: async (productId: string, exceptionCapPct: number, ticketId?: string): Promise<{ success: boolean }> => {
    return apiRequest<{ success: boolean }>(
      `/admin/products/price-exception/${encodeURIComponent(productId)}`,
      { method: 'POST', auth: true, body: JSON.stringify({ exceptionCapPct, ticketId: ticketId || undefined }) }
    );
  },

  revokePriceException: async (productId: string): Promise<{ success: boolean }> => {
    return apiRequest<{ success: boolean }>(
      `/admin/products/price-exception/${encodeURIComponent(productId)}`,
      { method: 'DELETE', auth: true }
    );
  },

  // Gaps 1-3 — Tapin box catalogue (platform-managed; Tapin's API
  // has no boxes endpoint). PUT upserts the edited list; removal is
  // by deactivation so past shipments' box ids always resolve.
  getTapinBoxes: async (): Promise<{ success: boolean; boxes: TapinBoxAdminDto[]; defaultBoxId: string }> => {
    return apiRequest<{ success: boolean; boxes: TapinBoxAdminDto[]; defaultBoxId: string }>('/admin/settings/tapin-boxes', {
      method: 'GET',
      auth: true,
    });
  },

  updateTapinBoxes: async (boxes: TapinBoxAdminDto[]): Promise<{ success: boolean; boxes: TapinBoxAdminDto[]; defaultBoxId: string }> => {
    return apiRequest<{ success: boolean; boxes: TapinBoxAdminDto[]; defaultBoxId: string }>('/admin/settings/tapin-boxes', {
      method: 'PUT',
      auth: true,
      body: JSON.stringify({ boxes }),
    });
  },

  getAuditLogs: async (category?: string): Promise<AuditLogDto[]> => {
    const qs = category ? `?category=${encodeURIComponent(category)}` : '';
    return apiRequest<AuditLogDto[]>(`/admin/audit-logs${qs}`, {
      method: 'GET',
      auth: true,
    });
  },

  createAuditLog: async (payload: Partial<AuditLogDto>): Promise<AuditLogDto> => {
    return apiRequest<AuditLogDto>('/admin/audit-logs', {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  // Team Settlement (Phase 3): super-admin settlement settings.
  // Served by the live-parity route GET/PUT /admin/settings/settlement
  // (admin role; super_admin is accepted by RequireRoles). The GET
  // response collapses an unset (NULL) value to 0, so a
  // settlementDay of 0 in the UI means "not set yet".
  getSettlementSettings: async (): Promise<SettlementSettingsDto> => {
    return apiRequest<SettlementSettingsDto>('/admin/settings/settlement', {
      method: 'GET',
      auth: true,
    });
  },

  // FIX 17: admin support-returns tab. Backend route GET
  // /admin/returns lists customer_returns for admins.
  getAdminReturns: async (): Promise<any[]> => {
    const data = await apiRequest<any>('/admin/returns', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.returns ?? data?.data ?? []);
  },

  // D1 (Batch 4a): read-only v1 lists for the eight former stub tabs.
  // One method per endpoint, same { method: 'GET', auth: true } shape
  // as getAdminReturns; each normalises its envelope to a plain array
  // (or, for site-content, to the content map).
  getAdminHomepageSections: async (): Promise<HomepageSectionDto[]> => {
    const data = await apiRequest<any>('/admin/homepage-sections', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.sections ?? []);
  },

  getAdminBanners: async (): Promise<AdminBannerDto[]> => {
    const data = await apiRequest<any>('/admin/banners', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.banners ?? []);
  },

  getAdminSiteContent: async (): Promise<Record<string, any>> => {
    const data = await apiRequest<any>('/admin/site-content', { method: 'GET', auth: true });
    return data?.content ?? {};
  },

  getAdminNewsletterSubscribers: async (): Promise<NewsletterSubscriberDto[]> => {
    const data = await apiRequest<any>('/admin/newsletter/subscribers', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.subscribers ?? []);
  },

  getAdminReviews: async (): Promise<AdminReviewRow[]> => {
    const data = await apiRequest<any>('/admin/reviews', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.reviews ?? []);
  },

  getAdminStaff: async (): Promise<AdminStaffDto[]> => {
    const data = await apiRequest<any>('/admin/staff', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.staff ?? []);
  },

  getAdminCallbackRetries: async (): Promise<CallbackRetryDto[]> => {
    const data = await apiRequest<any>('/admin/callback-retries', { method: 'GET', auth: true });
    return Array.isArray(data) ? data : (data?.retries ?? []);
  },

  saveSettlementSettings: async (payload: SettlementSettingsPayload): Promise<SettlementSettingsDto> => {
    return apiRequest<SettlementSettingsDto>('/admin/settings/settlement', {
      method: 'PUT',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  getStorageFeeSettings: async (): Promise<StorageFeeSettingsDto> => {
    return apiRequest<StorageFeeSettingsDto>('/admin/settings/storage-fees', {
      method: 'GET',
      auth: true,
    });
  },

  saveStorageFeeSettings: async (payload: StorageFeeSettingsDto): Promise<StorageFeeSettingsDto> => {
    return apiRequest<StorageFeeSettingsDto>('/admin/settings/storage-fees', {
      method: 'PUT',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  runStorageFees: async (): Promise<StorageFeeRunSummaryDto> => {
    return apiRequest<StorageFeeRunSummaryDto>('/admin/storage-fees/run', {
      method: 'POST',
      auth: true,
    });
  },
};

export const historicalReviewApi = {
  // PERF-02 Step 9: cursor pages over the summary view by default
  // (counts/flags only, no nested evidence). Pass view:'full' only when
  // the caller needs capped evidence lists for the whole page.
  report: (params?: { limit?: number; cursor?: string; view?: 'summary' | 'full' }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.cursor) query.set('cursor', params.cursor);
    if (params?.view) query.set('view', params.view);
    const suffix = query.toString() ? `?${query}` : '';
    return apiRequest<{ readOnly:boolean; rows:HistoricalInventoryReportRow[]; total:number; limit:number; nextCursor:string; hasMore:boolean; view:string }>(`/warehouse/inventory/reconciliation-report${suffix}`, { method:'GET', auth:true });
  },
  // Single FULL row (bounded evidence lists) for the review modal.
  row: (productId:string) => apiRequest<HistoricalInventoryReportRow>(`/warehouse/inventory/reconciliation-report?productId=${encodeURIComponent(productId)}`, { method:'GET', auth:true }),
  review: (productId:string, payload:{status:HistoricalReviewStatus; reason:string; evidence:string[]}) => apiRequest(`/warehouse/inventory/reconciliation-reviews/${encodeURIComponent(productId)}`, { method:'POST', auth:true, body:JSON.stringify(payload) }),
};
