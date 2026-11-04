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
export interface HistoricalInventoryReportRow { productId:string; productTitle:string; supplierId?:string; legacyStock:number; canonicalQuantity:number; canonicalSellableQuantity:number; physicalQuantity:number; canonicalRows:number; unreconciledRows:number; movementQuantity:number; measurementStatus:string; hasMeasurement:boolean; classifications:string[]; classification:string; evidenceInsufficient:boolean; unresolved:boolean; suggestedAction:string; difference:number; physicalInventory:boolean; measurementClassification:string; locationBreakdown:HistoricalLocationDetail[]; activeReservations:Array<Record<string, unknown>>; movements:Array<Record<string, unknown>>; measurementEvidence:Array<Record<string, unknown>>; evidenceIds:string[]; reviewStatus:HistoricalReviewStatus; reviewId?:string; }

export interface SettlementSettingsDto {
  settlementDay: number;
  earlyWithdrawalFeePct: number;
}

export interface SettlementSettingsPayload {
  settlementDay: number | null;
  earlyWithdrawalFeePct: number | null;
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
};

export const historicalReviewApi = {
  report: () => apiRequest<{ readOnly:boolean; rows:HistoricalInventoryReportRow[] }>('/warehouse/inventory/reconciliation-report', { method:'GET', auth:true }),
  review: (productId:string, payload:{status:HistoricalReviewStatus; reason:string; evidence:string[]}) => apiRequest(`/warehouse/inventory/reconciliation-reviews/${encodeURIComponent(productId)}`, { method:'POST', auth:true, body:JSON.stringify(payload) }),
};
