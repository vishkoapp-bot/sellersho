import { ApiError } from '../services/apiClient';
import type { UnitStatus } from './unitStatus';

export interface ScannedUnit {
  code: string;
  status?: UnitStatus;
}

export function normalizeUnitScan(input: string) {
  return input.trim().replace(/[a-z]/g, character => character.toUpperCase()).replace(/\s+/g, ' ');
}

export function completeScan(scans: ScannedUnit[], quantity: number) {
  return scans.length === 0 || scans.length === quantity;
}

export function unitMutationError(cause: unknown, fallback: string) {
  if (cause instanceof ApiError && cause.body?.error === 'unit_mismatch' && Array.isArray(cause.body.errors)) {
    const details = cause.body.errors.map((item: { code: string; error: string; expected?: string; actual?: string }) =>
      [item.code, item.error, item.expected, item.actual].filter(Boolean).join(' / '));
    return [cause.body.error, cause.body.line_id, 'کدهای اسکن‌شده با این ردیف مطابقت ندارند.', ...details].filter(Boolean).join(' — ');
  }
  if (cause instanceof ApiError && typeof cause.body?.error === 'string') return cause.body.error;
  return cause instanceof Error ? cause.message : fallback;
}
