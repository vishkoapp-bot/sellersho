import type { SupplierSession } from '../types';
import {
  canRunVariantEdit, VARIANT_BATCH_LIMIT, variantBatchRetiredCodes, type VariantEditBatch,
} from '../utils/variantWorkspace';

export interface VariantBatchRecovery {
  scope: string;
  load: () => VariantEditBatch | undefined;
  save: (batch: VariantEditBatch, replace?: boolean) => void;
  clear: (id: string) => void;
}

const memory = new Map<string, VariantEditBatch>();

export function supplierVariantBatchRecovery(
  session: Pick<SupplierSession, 'token' | 'supplierId'> | null | undefined,
): VariantBatchRecovery | undefined {
  if (!session?.supplierId || !session.token) return undefined;
  let scope: string;
  try {
    // Identity partition only. Each operation is still authorized by the server.
    const claims = JSON.parse(atob(session.token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')));
    if (typeof claims.userId !== 'string' || !claims.userId) return undefined;
    scope = 'sellersho.supplier-variant-batch.' + JSON.stringify([session.supplierId, claims.userId]);
  } catch {
    return undefined;
  }

  const load = () => {
    const cached = memory.get(scope);
    if (cached) return cached;
    try {
      const saved = JSON.parse(sessionStorage.getItem(scope) ?? 'null');
      if (typeof saved?.id === 'string' && Array.isArray(saved.rows) && saved.rows.length <= VARIANT_BATCH_LIMIT &&
        saved.rows.every((row: VariantEditBatch['rows'][number]) => row && typeof row.id === 'string' &&
          Number.isSafeInteger(row.line) && row.edit && typeof row.edit === 'object')) return saved as VariantEditBatch;
    } catch { /* storage may be denied */ }
    return undefined;
  };
  const clear = (id: string) => {
    if (load()?.id !== id) return;
    memory.delete(scope);
    try { sessionStorage.removeItem(scope); } catch { /* memory fallback */ }
  };
  return {
    scope,
    load,
    clear,
    save: (batch, replace = false) => {
      const previous = load();
      if (previous && previous.id !== batch.id && !replace) return;
      if (!batch.rows.some(canRunVariantEdit) && !variantBatchRetiredCodes(batch.rows).length) {
        clear(batch.id);
        return;
      }
      memory.set(scope, batch);
      try { sessionStorage.setItem(scope, JSON.stringify(batch)); } catch { /* memory fallback */ }
    },
  };
}
