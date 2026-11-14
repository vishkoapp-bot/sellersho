import { financeOperation } from './financeOperation';
import { apiRequest } from './apiClient';

export interface LedgerEntryDto {
  id: string;
  date: string;
  targetAccount: string;
  targetType: 'seller' | 'supplier' | 'carrier' | 'platform';
  type: 'debit' | 'credit';
  amount: number;
  reason: string;
  operator: string;
  relatedOrderOrDispute?: string;
  createdAt: string;
}

export interface DisputeDto {
  id: string;
  orderNumber: string;
  type: 'return_7day' | 'delivery_undelivered';
  customerName: string;
  productTitle: string;
  claimAmount: number;
  reason: string;
  storeId: string;
  supplierName?: string;
  status: 'pending' | 'resolved';
  responsibleParty?: string;
  resolutionAction?: string;
  resolutionDate?: string;
  createdAt: string;
}

export interface CreateDisputePayload {
  orderNumber: string;
  type: string;
  customerName: string;
  productTitle: string;
  claimAmount: number;
  reason: string;
  storeId: string;
  supplierName?: string;
}

export interface CreateAdjustmentPayload {
  targetKind: 'supplier' | 'seller';
  targetId: string;
  direction: 'debit' | 'credit';
  amount: number; // Toman, positive integer
  reason: string;
}

export interface CreateAdjustmentResponse {
  success: boolean;
  docId: string; // e.g. ADJ-<uuid>
}

export interface ResolveDisputePayload {
  action: string;
  responsibleParty: string;
}

export const financeApi = {
  getLedger: async (): Promise<LedgerEntryDto[]> => {
    return apiRequest<LedgerEntryDto[]>('/finance/ledger', {
      method: 'GET',
      auth: true,
    });
  },

  getDisputes: async (): Promise<DisputeDto[]> => {
    return apiRequest<DisputeDto[]>('/disputes', {
      method: 'GET',
      auth: true,
    });
  },

  createDispute: async (payload: CreateDisputePayload): Promise<DisputeDto> => {
    return apiRequest<DisputeDto>('/disputes', {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  resolveDispute: async (id: string, payload: ResolveDisputePayload): Promise<any> => {
    return apiRequest<any>(`/disputes/${encodeURIComponent(id)}/resolve`, {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
  },

  // Durable keyed adjustment; ambiguous responses retain the operation identity.
  createAdjustment: async (payload: CreateAdjustmentPayload): Promise<CreateAdjustmentResponse> =>
    financeOperation<CreateAdjustmentResponse>('/finance/adjustments', payload,
      `${payload.targetKind}:${payload.targetId}:adjustment`,
      value => value.success === true && typeof value.docId === 'string' && !!value.docId),

  getCashPosition: async (days = 30): Promise<CashPositionDto> => {
    return apiRequest<CashPositionDto>(`/finance/treasury/cash-position?days=${days}`, {
      method: 'GET',
      auth: true,
    });
  },

  getCommission: async (days = 30): Promise<CommissionDto> => {
    return apiRequest<CommissionDto>(`/finance/treasury/commission?days=${days}`, {
      method: 'GET',
      auth: true,
    });
  },

  getProfitLocks: async (): Promise<ProfitLocksDto> => {
    return apiRequest<ProfitLocksDto>('/finance/treasury/profit-locks', {
      method: 'GET',
      auth: true,
    });
  },

  getPendingSettlements: async (): Promise<PendingSettlementDto[]> => {
    const res = await apiRequest<any>('/finance/settlements/pending', {
      method: 'GET',
      auth: true,
    });
    if (Array.isArray(res)) return res;
    if (Array.isArray(res?.settlements)) return res.settlements;
    return [];
  },
};

export interface TreasuryTrendPoint {
  date: string;
  commission?: number;
  in?: number;
  out?: number;
}

export interface CommissionDto {
  success: boolean;
  days: number;
  total: number;
  releasedTotal: number;
  unreleasedTotal: number;
  orderCount: number;
  trend: TreasuryTrendPoint[];
  note?: string;
}

export interface CashPositionDto {
  success: boolean;
  days: number;
  allTimeIn: number;
  allTimeOut: number;
  overObligated: boolean;
  safeToSpend: number;
  period: { in: number; out: number; trend: TreasuryTrendPoint[] };
  heldOrderCount: number;
  heldInOrders: number;
  owedToStores: number;
  owedToSuppliers: number;
  pendingRefundCount: number;
  pendingRefunds: number;
  pendingCount: number;
  pendingPayouts: number;
  platformEarned: number;
  note?: string;
  sellerInFlight: number;
  supplierInFlight: number;
}

export interface ProfitLocksDto {
  success: boolean;
  totals: { locked: number; lockedCount: number; releasable: number; releasableCount: number };
  rule?: string;
}

export interface PendingSettlementDto {
  amount: number;
  [key: string]: any;
}
