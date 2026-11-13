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
export interface HistoricalReservationDetail { id:string; locationKey:string; quantity:number; referenceId:string; status:string; idempotencyKey:string; }
export interface HistoricalMovementDetail { id:string; quantity:number; fromStatus?:string; toStatus?:string; fromLocationKey?:string; toLocationKey?:string; referenceType:string; referenceId:string; idempotencyKey:string; createdAt:string; }
export interface HistoricalMeasurementEvidenceDetail { id:string; newLengthMm:number; newWidthMm:number; newHeightMm:number; newWeightGrams:number; reason:string; actorUserId:string; createdAt:string; }
export interface HistoricalInventoryReportRow { productId:string; productTitle:string; supplierId?:string; legacyStock:number; canonicalQuantity:number; canonicalSellableQuantity:number; physicalQuantity:number; canonicalRows:number; unreconciledRows:number; movementQuantity:number; measurementStatus:string; hasMeasurement:boolean; hasInboundEvidence:boolean; hasQCEvidence:boolean; hasPutawayEvidence:boolean; hasLedgerEvidence:boolean; classifications:string[]; classification:string; evidenceInsufficient:boolean; unresolved:boolean; suggestedAction:string; difference:number; physicalInventory:boolean; measurementClassification:string; locationBreakdown:HistoricalLocationDetail[]; activeReservations:HistoricalReservationDetail[]; movements:HistoricalMovementDetail[]; measurementEvidence:HistoricalMeasurementEvidenceDetail[]; evidenceIds:string[]; reviewStatus:HistoricalReviewStatus; reviewId?:string; }

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
  report: () => apiRequest<{ readOnly:boolean; rows:HistoricalInventoryReportRow[] }>('/warehouse/inventory/reconciliation-report', { method:'GET', auth:true }),
  review: (productId:string, payload:{status:HistoricalReviewStatus; reason:string; evidence:string[]}) => apiRequest(`/warehouse/inventory/reconciliation-reviews/${encodeURIComponent(productId)}`, { method:'POST', auth:true, body:JSON.stringify(payload) }),
};
