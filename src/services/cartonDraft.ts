import { supplierApi } from './supplierApi';
import { newUuid } from './productModel';
import { tokenStore } from './apiClient';

export type CartonDraft = { warehouse_id: string; source?: {source_kind: string; supplier_warehouse_id?: string; external_reference?: string; reason?: string}; items: { variant_id: string; expected_qty: number }[] };

// In-memory only: the operation survives ambiguous request failures while this
// draft is open. It stores no credentials and cannot carry an identity across an
// authentication generation. A successful UI submission explicitly resets it.
export const createCartonDraftOperation = () => {
  let operation: { canonical: string; key: string; generation: number } | null = null;
  return {
    async submit(payload: CartonDraft) {
      const items = payload.items.map(item => ({ ...item })).sort((a, b) => a.variant_id.localeCompare(b.variant_id));
      const total = items.reduce((sum, item) => sum + item.expected_qty, 0);
      if (!payload.warehouse_id || !items.length || items.some(item => !item.variant_id || !Number.isSafeInteger(item.expected_qty) || item.expected_qty <= 0 || item.expected_qty > 2147483647)
        || new Set(items.map(item => item.variant_id)).size !== items.length || total > 2147483647) throw new Error('تعداد همه مدل‌ها باید درست و حداکثر ۲۱۴۷۴۸۳۶۴۷ باشد.');
      const generation = tokenStore.getGeneration();
      const canonical = JSON.stringify({ warehouse_id: payload.warehouse_id, source: payload.source, items });
      if (!operation || operation.canonical !== canonical || operation.generation !== generation) {
        operation = { canonical, generation, key: newUuid() };
      }
      const result = await supplierApi.createShipment({ warehouse_id: payload.warehouse_id, source: payload.source, items, creation_key: operation.key });
      if (tokenStore.getGeneration() !== generation) throw new Error('Session changed during shipment creation');
      // A 200 response can be truncated or omit the committed identity. Treat
      // that as ambiguous so the UI retains this operation and its draft.
      if (!result || typeof result.id !== 'string' || !result.id.trim()
        || typeof result.shipment_code !== 'string' || !result.shipment_code.trim()) {
        throw new Error('پاسخ ثبت محموله کامل نبود؛ دوباره تلاش کن.');
      }
      return result;
    },
    reset() { operation = null; },
  };
};
