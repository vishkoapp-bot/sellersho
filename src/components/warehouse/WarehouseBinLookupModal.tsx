import { useId } from 'react';
import { SupplierDialog, SupplierDialogHeading } from '../supplier/SupplierDialog';
import { WarehouseBinInventory } from './WarehouseInventoryTab';

interface Props {
  isOpen: boolean;
  onClose: () => void;
  initialBinCode?: string;
}

export function WarehouseBinLookupModal({ isOpen, onClose, initialBinCode }: Props) {
  const titleId = useId();
  if (!isOpen) return null;

  return (
    <SupplierDialog titleId={titleId} onDismiss={onClose} wide>
      <SupplierDialogHeading id={titleId} title="استعلام قفسه" onClose={onClose} />
      <WarehouseBinInventory key={initialBinCode} initialBinCode={initialBinCode} />
    </SupplierDialog>
  );
}
