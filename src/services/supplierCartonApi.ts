import { ApiError, apiRequest } from './apiClient';
import type { PrintLabelPayload } from './unitsApi';

export interface SupplierCartonUnit {
  code: string;
  print_count: number;
}

export interface SupplierCartonLine {
  id?: string;
  variant_id: string;
  variant_name: string;
  product_title: string;
  spnc: string;
  source_kind?: 'transfer' | 'external_replenishment';
  expected_qty: number;
  received_qty: number;
  putaway_qty: number | null;
  cancelled_qty: number;
  qc_status: string;
  qc_passed_qty: number | null;
  qc_failed_qty: number | null;
  qc_shortage: number | null;
  qc_reason?: string;
  units: SupplierCartonUnit[];
}

interface SupplierCartonDetails {
  id: string;
  shipment_code: string;
  status: string;
  warehouse_name?: string;
  received_at: string | null;
  lines: SupplierCartonLine[];
}

export const supplierCartonApi = {
  async details(shipmentId: string) {
    const result = await apiRequest<SupplierCartonDetails>(`/supplier/cartons/${encodeURIComponent(shipmentId)}`, { auth: true });
    if (result?.id !== shipmentId || !Array.isArray(result.lines)) throw new Error('جزئیات کارتن کامل دریافت نشد.');
    return result;
  },
  async printUnits(shipmentId: string, data: { line_id: string; unit_codes: string[]; reprint: boolean; idempotency_key: string }) {
    const result = await apiRequest<{ units: PrintLabelPayload[] }>(`/supplier/cartons/${encodeURIComponent(shipmentId)}/print-units`, {
      method: 'POST', auth: true, body: JSON.stringify(data),
    });
    if (!Array.isArray(result?.units) || result.units.length !== data.unit_codes.length ||
        result.units.some((unit, index) => unit.code !== data.unit_codes[index] || unit.barcode_value !== unit.code)) {
      throw new ApiError(0, 'برچسب‌های دریافت‌شده با کدهای انتخابی مطابقت ندارد. برای همین درخواست دوباره تلاش کن.');
    }
    return result;
  },
};
