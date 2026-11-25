import { loadSupplierVariantWorkspace, saveSupplierVariantWork } from '../../services/variantWorkspaceApi';
import { VariantWorkspace, type WorkspaceRow } from '../VariantWorkspace';

async function load(signal: AbortSignal): Promise<WorkspaceRow[]> {
  const rows = await loadSupplierVariantWorkspace(signal);
  return rows.map(row => ({
    ...row,
    stock: row.sellable,
    attention: !row.active || row.sellable <= 5 || row.pendingCost != null,
  }));
}

export function SupplierVariantWorkspace({
  onChanged, onRetired,
}: {
  onChanged: () => void;
  onRetired: (codes: string[]) => void;
}) {
  return <VariantWorkspace role="supplier" title="موجودی و قیمت عمده تنوع‌ها" load={load} save={saveSupplierVariantWork}
    onChanged={onChanged} onRetired={onRetired} />;
}
