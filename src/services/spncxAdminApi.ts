import { ApiError, apiRequest } from './apiClient';

export interface BackfillVariantAlloc {
  variant_id: string;
  spnc: string;
  variant_stock: number;
  alloc: number;
  already_minted: number;
  is_default: boolean;
}

export interface BackfillVariantOrders {
  variant_id: string;
  spnc: string;
  orders: number;
}

export interface BackfillOrderHistory {
  default_ever_ordered: boolean;
  named_ever_ordered: boolean;
  orders: number;
  named_variant_orders: BackfillVariantOrders[];
}

export type ExcessDecision = 'write_off_accepted' | 'physical_count_ordered';
export type BackfillLotCreation = { variant_id: string; quantity: number; location_key: string };
export type BackfillLotReduction = { balance_id: string; quantity_delta: number };
export type BackfillVariantStockCorrection = { variant_id: string; from: number; to: number };
export type BackfillNewVariant = { variant_id: string; stock: number };

export interface BackfillProductPlan {
  product_id: string;
  title: string;
  P: number;
  alloc: BackfillVariantAlloc[];
  lots_to_create: BackfillLotCreation[];
  lots_to_reduce: BackfillLotReduction[];
  top_up: number;
  excess: number;
  excess_decision: ExcessDecision | null;
  variant_stock_corrections: BackfillVariantStockCorrection[];
  new_variants: BackfillNewVariant[];
  order_history: BackfillOrderHistory;
  trim_target: string | null;
  excluded: boolean;
  excluded_reason: string | null;
  flipped_variants: string[];
}

export interface BackfillProductDiff {
  product_id: string;
  alloc_changed: boolean;
  top_up_changed: boolean;
  excess_changed: boolean;
}

export interface BackfillPlanDiff {
  previous_plan_hash: string | null;
  changed_products: BackfillProductDiff[];
}

export interface BackfillGroupTotals {
  doubled_non_default: number;
  equal: number;
  other: number;
  variant_less: number;
}

export type BackfillAssertion = { name: string; passed: boolean; details: string };
export type BackfillPreCutoverOrders = { count: number; items: number; open_reservations: number };

export interface BackfillPlan {
  dry_run: true;
  plan_hash: string;
  basis: string;
  total_units: number;
  product_count: number;
  group_totals: BackfillGroupTotals;
  products: BackfillProductPlan[];
  global_assertions: BackfillAssertion[];
  demo_test_products: string[];
  inactive_products: string[];
  pre_cutover_orders: BackfillPreCutoverOrders;
  diff_vs_previous_dry_run: BackfillPlanDiff | null;
}

export interface BackfillExcessDecision {
  product_id: string;
  excess_amount: number;
  excess_decision: ExcessDecision;
  excess_decided_by: string;
  excess_decided_at: string;
  review_by: string | null;
}

export interface BackfillStatusCounts {
  pending: number;
  completed: number;
  failed: number;
  excluded: number;
}

export interface BackfillStatusSummary extends BackfillStatusCounts {
  total_units_minted: number;
}

export interface BackfillStatusProduct {
  product_id: string;
  status: string;
  units_minted: number;
  units_target: number;
  started_at: string | null;
  completed_at: string | null;
  error: string | null;
  excluded_reason: string | null;
  excluded_by: string | null;
  excluded_at: string | null;
  flipped_variants: string[];
  excess_held: number;
  excess_review_by: string | null;
  excess_amount: number;
  excess_decision: ExcessDecision | null;
  excess_decided_by: string | null;
  excess_decided_at: string | null;
}

export interface BackfillStatus {
  running: boolean;
  products: BackfillStatusProduct[];
  summary: BackfillStatusSummary;
}

export type BackfillRunProduct = Pick<BackfillStatusProduct,
  'product_id' | 'status' | 'units_minted' | 'units_target' | 'flipped_variants'> & { excess: number };

export interface BackfillRunResult {
  dry_run: false;
  plan_hash: string;
  completed: number;
  skipped_completed: number;
  failed: number;
  excluded_skipped: number;
  total_minted: number;
  products: BackfillRunProduct[];
}

export interface BackfillRollbackProduct {
  product_id: string;
  rolled_back: boolean;
  printed_codes_to_destroy: string[];
  restored_snapshot: boolean;
  error?: string | null;
}

export type BackfillRollbackResult = BackfillRollbackProduct | { all: true; results: BackfillRollbackProduct[] };
export type RetryCaptureResult = { order_id: string; captured: boolean; unit_codes: string[] };
export type ReincludeReasons = Record<string, string>;

export type ReconciliationRow = Record<string, unknown>;
export type ReconciliationV0Row = { product_id: string; source: 'V0a' | 'V0b' };
export type ReconciliationCheckBase = { id: string; name: string; passed: boolean };
export type ReconciliationRowsCheck = ReconciliationCheckBase & {
  rows: ReconciliationRow[];
  row_count: number;
  details?: string;
};
export interface ReconciliationTotals {
  total_units: number;
  in_stock: number;
  removed: number;
  distinct_codes: number;
  counters: number;
}
export type ReconciliationTotalsCheck = ReconciliationCheckBase & {
  id: 'V4'; totals: ReconciliationTotals; expected: ReconciliationTotals;
};
export type ReconciliationCompletionCheck = ReconciliationCheckBase & { id: 'V5'; summary: BackfillStatusCounts };
export type ReconciliationPhysicalCheck = ReconciliationCheckBase & {
  id: 'V10'; sampled_products: string[]; mismatches: ReconciliationRow[]; details: string;
};
export type ReconciliationCheck = ReconciliationRowsCheck | ReconciliationTotalsCheck
  | ReconciliationCompletionCheck | ReconciliationPhysicalCheck;

export interface ReconciliationHeldLot {
  product_id: string;
  location_key: string;
  quantity: number;
  review_by: string | null;
  excess_decision: ExcessDecision | null;
}
export interface ReconciliationCoverageGap {
  variant_id: string | null;
  requested: number;
  allocated: number;
  shortfall: number;
  operation: string;
}
export interface ReconciliationFailedCapture {
  order_id: string;
  failure: string;
  mode: string;
  at: string;
}
export interface ReconciliationReport {
  generated_at: string;
  checks: ReconciliationCheck[];
  held_lots: ReconciliationHeldLot[];
  coverage_gaps: ReconciliationCoverageGap[];
  failed_captures: ReconciliationFailedCapture[];
}

export interface BackfillErrorBody {
  error?: string;
  diff?: BackfillPlanDiff;
  global_assertions?: BackfillAssertion[];
  printed_codes_to_destroy?: string[];
}

export function extractSpncxErrorCode(error: unknown): string | null {
  return error instanceof ApiError && error.body && typeof error.body === 'object' && typeof error.body.error === 'string'
    ? error.body.error
    : null;
}

export const SPNCX_UNAVAILABLE = 'قابلیت سریال واحد هنوز فعال نشده است.';

export function isSpncxUnavailable(error: unknown, code: string | null): boolean {
  // A JSON 404 for an unknown product or order comes from a deployed endpoint.
  return error instanceof ApiError && (error.status === 501 || (error.status === 404 && code === null));
}

const backfillPath = '/admin/spncx/backfill';
// The run and rollback handlers respond after the server finishes processing.
const backfillTimeout = 15 * 60_000;

function post<T>(path: string, body: object, timeout?: number): Promise<T> {
  return apiRequest<T>(path, { auth: true, method: 'POST', body: JSON.stringify(body), timeout });
}

export function getReconciliation(): Promise<ReconciliationReport> {
  return apiRequest('/admin/inventory/reconciliation', { auth: true });
}

export function postBackfillDryRun(productIds?: string[], batchSize?: number): Promise<BackfillPlan> {
  return post(backfillPath, { dry_run: true, product_ids: productIds, batch_size: batchSize });
}

export function postBackfillRun(productIds?: string[], batchSize?: number, confirmPlanDiff?: boolean, reinclude?: ReincludeReasons): Promise<BackfillRunResult> {
  return post(backfillPath, { dry_run: false, product_ids: productIds, batch_size: batchSize,
    confirm_plan_diff: confirmPlanDiff, reinclude }, backfillTimeout);
}

export function getBackfillStatus(): Promise<BackfillStatus> {
  return apiRequest(backfillPath + '/status', { auth: true });
}

export function postBackfillRollback(productId?: string, all?: boolean, confirmPrintedDestruction?: boolean): Promise<BackfillRollbackResult> {
  return post(backfillPath + '/rollback', {
    ...(all ? { all: true } : { product_id: productId }),
    confirm_printed_destruction: confirmPrintedDestruction || undefined,
  }, backfillTimeout);
}

export function postExcessDecision(productId: string, decision: ExcessDecision): Promise<BackfillExcessDecision> {
  return post(backfillPath + '/excess-decision', { product_id: productId, excess_decision: decision });
}

export function retryCapture(orderId: string): Promise<RetryCaptureResult> {
  return apiRequest('/admin/orders/' + encodeURIComponent(orderId) + '/retry-capture', { auth: true, method: 'POST' });
}

export const spncxAdminApi = {
  getReconciliation, postBackfillDryRun, postBackfillRun, getBackfillStatus,
  postBackfillRollback, postExcessDecision, retryCapture,
};
