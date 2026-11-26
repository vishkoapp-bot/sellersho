import { useMemo } from 'react';
import type { SupplierSession } from '../../types';
import { loadSupplierVariantWorkspace, saveSupplierVariantWork } from '../../services/variantWorkspaceApi';
import { supplierVariantBatchRecovery } from '../../services/supplierVariantBatch';
import { VariantWorkspace, type WorkspaceRow } from '../VariantWorkspace';
import { RetiredPrintedCodesPanel } from './RetiredPrintedCodesPanel';

async function load(signal: AbortSignal): Promise<WorkspaceRow[]> {
  const rows = await loadSupplierVariantWorkspace(signal);
  return rows.map(row => ({
    ...row,
    stock: row.sellable,
    attention: !row.active || row.sellable <= 5 || row.pendingCost != null,
  }));
}

export function SupplierVariantWorkspace({
  session, onChanged,
}: {
  session?: Pick<SupplierSession, 'supplierId' | 'token'> | null;
  onChanged: () => void;
}) {
  const recovery = useMemo(() => supplierVariantBatchRecovery(session), [session]);
  return <VariantWorkspace key={recovery?.scope} role="supplier" title="موجودی و قیمت عمده تنوع‌ها"
    load={load} save={saveSupplierVariantWork} recovery={recovery} onChanged={onChanged}
    renderRetired={(codes, acknowledge) => <RetiredPrintedCodesPanel codes={codes} onAcknowledge={acknowledge} />} />;
}
