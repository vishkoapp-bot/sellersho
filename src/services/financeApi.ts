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

export interface DisputeOutcome {
 disputeId: string; orderId: string; action: string; responsibleParty: string;
 claimAmount: number; decisionTime?: string; replay: boolean; orderStatus: string;
 sellerReleaseState: 'awaiting_delivery'|'awaiting_window'|'held'|'eligible'|'released'|'needs_review';
 holdReasons: string[]; refundScope: 'order';
 refund: {status:'none'|'pending'|'succeeded'|'failed'|'unknown'; amountToman:number; pendingToman:number; succeededToman:number; failedToman:number; unknownToman:number; needsReview:boolean};
}

export interface DisputeDto {
  outcome?: DisputeOutcome;
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
  claimAmount?: number;
}

export const financeApi = {
  // PERF-02 Step 9: the ledger endpoint now pages (envelope with
  // `entries`); callers that need more than the first page pass a cursor.
  getLedger: async (params?: { limit?: number; cursor?: string; dateFrom?: string; dateTo?: string }): Promise<LedgerEntryDto[]> => {
    const query = new URLSearchParams();
    if (params?.limit) query.set('limit', String(params.limit));
    if (params?.cursor) query.set('cursor', params.cursor);
    if (params?.dateFrom) query.set('dateFrom', params.dateFrom);
    if (params?.dateTo) query.set('dateTo', params.dateTo);
    const suffix = query.toString() ? `?${query}` : '';
    const res = await apiRequest<any>(`/finance/ledger${suffix}`, {
      method: 'GET',
      auth: true,
    });
    if (Array.isArray(res)) return res;
    if (Array.isArray(res?.entries)) return res.entries;
    return [];
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

  resolveDispute: async (id: string, payload: ResolveDisputePayload): Promise<DisputeOutcome> => {
    const outcome = await apiRequest<DisputeOutcome>(`/disputes/${encodeURIComponent(id)}/resolve`, {
      method: 'POST',
      auth: true,
      body: JSON.stringify(payload),
    });
    if (!outcome || outcome.disputeId !== id || !outcome.orderId || !outcome.sellerReleaseState || outcome.refundScope !== 'order' || !outcome.refund) throw new Error('نتیجه حکم نامشخص است؛ فهرست را دوباره بارگذاری کنید.');
    return outcome;
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
  in?: number | null;
  out?: number | null;
  /**
   * false = this day could not be computed honestly. The UI renders it as
   * a gap ("\u2014"), never as 0 (FIN-008 D-18/D-15).
   */
  available?: boolean;
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

// FIN-008 (D-16/D-18): treasury cash position. All money is integer Toman.
// Unknown cash is null, never 0 and never a guess: liquidCash/safeToSpend
// are `number | null`, with cashStatus + reconciliationRequired saying so.
// Read path: GET /finance/treasury/cash-position?days=N (financeApi.getCashPosition).
export interface CashPositionDto {
  success: boolean;
  days: number;
  /** Gross paid-order value — diagnostic ONLY, never cash (D-01). */
  allTimeIn: number;
  allTimeOut: number;
  overObligated: boolean;
  /** Unclamped shortfall, negative when over-obligated; UI shows its magnitude. */
  overObligatedAmount: number;
  /** Verified liquid cash (Toman). null = unknown, never 0. */
  liquidCash: number | null;
  /** null = unknown cash, never a guessed number. */
  safeToSpend: number | null;
  cashStatus: 'verified' | 'unknown' | 'partial';
  reconciliationRequired: boolean;
  /** Paid-order value with no capture/settlement evidence + legacy simulator value. */
  unresolvedProviderCash: number;
  /** Itemisation of unresolvedProviderCash (G-13); optional for older responses. */
  unverifiedPaidOrders?: number;
  legacySimulator?: number;
  /** Total liability = sum of positive available + holds (Toman). */
  liability: number;
  /** Negative balances, reported separately, NEVER netted into liabilities (D-10). */
  recoveryReceivableStores: number;
  recoveryReceivableSuppliers: number;
  /** Sum of customer_wallets.balance (Toman). */
  walletLiability: number;
  /** Pending/unknown/failed refund obligations (Toman) — gateway +
      wallet destinations (F-01/F-07). The split is
      refundLiabilityGateway / refundLiabilityWallet. */
  refundLiability: number;
  /** Gateway-destination slice of refundLiability (optional: older responses omit it). */
  refundLiabilityGateway?: number;
  /** Wallet-destination slice of refundLiability. */
  refundLiabilityWallet?: number;
  /** Review-destination obligations (legacy_reservation, FIN-003):
      computed by the server but NOT part of refundLiability and NOT
      rendered yet — treatment is decided before FIN-014 (F-04). */
  refundLiabilityReview?: number;
  /** Unreleased seller-profit hold (same value as heldInOrders). */
  unreleasedProfitHold?: number;
  /** Unconfirmed transfer holds: payouts pending/approved/processing + pending early withdrawals. */
  holds: number;
  holdsByStatus?: { pending?: number; approved?: number; processing?: number; earlyWithdrawal?: number };
  period: { in: number; out: number; trend: TreasuryTrendPoint[]; trendAvailable?: boolean };
  heldOrderCount: number;
  /** Unreleased SELLER PROFIT only — never the whole order (D-11). */
  heldInOrders: number;
  owedToStores: number;
  owedToSuppliers: number;
  pendingRefundCount: number;
  /** Informational only: subset of walletLiability, never additive (D-08). */
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
  /** false = the locks cannot be computed honestly; UI shows an error, never zeros (D-15). */
  available?: boolean;
  totals: { locked: number; lockedCount: number; releasable: number; releasableCount: number };
  rule?: string;
}

export interface PendingSettlementDto {
  amount: number;
  [key: string]: any;
}
