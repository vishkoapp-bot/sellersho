import { apiRequest } from './apiClient';
import type { UnitStatus } from '../utils/unitStatus';

export type { UnitStatus } from '../utils/unitStatus';

export interface UnitLabelRow {
  code: string;
  spnc: string;
  variant_id: string;
  variant_name: string;
  product_id: string;
  spn: string;
  title: string;
  status: UnitStatus;
  printed_at: string | null;
  print_count: number;
  last_printed_at: string | null;
}

export interface UnitLabelListResponse {
  spn: string;
  product_title: string;
  units: UnitLabelRow[];
  total_for_filter: number;
  next_cursor: string | null;
}

export interface PrintUnitsRequest {
  unit_codes?: string[];
  variant_id?: string;
  count?: number;
  unprinted_only?: boolean;
  reprint?: boolean;
  idempotency_key: string;
}

export interface PrintLabelPayload {
  code: string;
  spnc: string;
  variant_name: string;
  spn: string;
  title: string;
  barcode_value: string;
}

export interface UnitLookupResponse {
  code: string;
  status: UnitStatus;
  variant: { id: string; spnc: string; name: string };
  product: { id: string; spn: string; title: string };
  location: {
    balance_id: string;
    location_key: string;
    warehouse_name: string | null;
    bin_code: string | null;
    lot_status: string;
  } | null;
  printed: { printed_at: string | null; print_count: number; last_printed_at: string | null };
  order: { order_id: string; order_number: string; captured_at: string; restored_at: string | null } | null;
  history: UnitEvent[];
}

export interface UnitEvent {
  event_type: string;
  from_status: string | null;
  to_status: string | null;
  reference_type: string;
  reference_id: string | null;
  actor_user_id: string | null;
  created_at: string;
}

export interface ListUnitsParams {
  variant_id?: string;
  status?: UnitStatus;
  q?: string;
  order_id?: string;
  balance_id?: string;
  order_item_id?: string;
  limit?: number;
  cursor?: string;
}

export interface ListUnitsResponse {
  units: UnitLabelRow[];
  next_cursor: string | null;
}

export function getUnit(code: string): Promise<UnitLookupResponse> {
  return apiRequest(`/units/${encodeURIComponent(code)}`, { method: 'GET', auth: true });
}

export function listUnits({ limit, ...filters }: ListUnitsParams = {}): Promise<ListUnitsResponse> {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== '') query.set(key, value);
  }
  if (limit !== undefined && Number.isFinite(limit)) {
    query.set('limit', String(Math.max(1, Math.min(200, Math.trunc(limit)))));
  }
  const suffix = query.size ? `?${query}` : '';
  return apiRequest(`/units${suffix}`, { method: 'GET', auth: true });
}

export const unitsApi = { getUnit, listUnits };
