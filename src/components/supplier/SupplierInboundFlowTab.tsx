import { useEffect, useRef, useState } from 'react';
import { Box, ListChecks, PackagePlus } from 'lucide-react';
import { SupplierLabeledInboundTab } from './SupplierLabeledInboundTab';
import { SupplierProductsTab } from './SupplierProductsTab';

export type ShipmentStage = 'products' | 'prepare' | 'track';

const stages = [
  { id: 'products', title: 'ثبت کالا', icon: Box },
  { id: 'prepare', title: 'آماده‌سازی محموله', icon: PackagePlus },
  { id: 'track', title: 'ارسال و برچسب‌ها', icon: ListChecks },
] as const;

interface Props {
  initialProductId: string;
  activeStage?: ShipmentStage;
  onStageChange?: (stage: ShipmentStage) => void;
  onProductConsumed: () => void;
  canManageProducts: boolean;
  canManageShipments: boolean;
  openOnly?: boolean;
}

export function SupplierInboundFlowTab({ initialProductId, activeStage, onStageChange, onProductConsumed, canManageProducts, canManageShipments, openOnly }: Props) {
  const [stage, setStage] = useState<ShipmentStage>(initialProductId || !canManageProducts ? 'prepare' : 'products');
  const requestedStage = activeStage ?? stage;
  const [selectedProductId, setSelectedProductId] = useState(initialProductId);
  const [shipmentsOpened, setShipmentsOpened] = useState(stage !== 'products');
  const stepNavigation = useRef<HTMLElement>(null);
  const availableStages = stages.filter(item => item.id === 'products' ? canManageProducts : canManageShipments);
  const currentStage = availableStages.some(item => item.id === requestedStage) ? requestedStage : availableStages[0]?.id;

  useEffect(() => {
    if (initialProductId) onProductConsumed();
  }, [initialProductId, onProductConsumed]);

  useEffect(() => {
    if (currentStage && currentStage !== 'products') setShipmentsOpened(true);
  }, [currentStage]);

  const navigate = (next: ShipmentStage) => {
    if (next !== 'products') setShipmentsOpened(true);
    if (onStageChange) onStageChange(next);
    else setStage(next);
    stepNavigation.current?.scrollIntoView({ block: 'nearest' });
  };

  const prepareProduct = (productId: string) => {
    setSelectedProductId(productId);
    navigate('prepare');
  };

  return (
    <div className="space-y-4 sm:space-y-5">
      {availableStages.length > 1 && (
        <nav ref={stepNavigation} aria-label="مراحل ثبت کالا و ارسال" className={`grid scroll-mt-24 gap-1 rounded-2xl border border-[#e6e2f0] bg-white p-1.5 sm:gap-2 sm:p-2 ${availableStages.length === 3 ? 'grid-cols-3' : 'grid-cols-2'}`}>
          {availableStages.map(({ id, title, icon: Icon }) => (
            <button key={id} type="button" aria-current={id === currentStage ? 'step' : undefined} onClick={() => navigate(id)}
              className={`flex min-h-14 min-w-0 flex-col items-center justify-center gap-1 rounded-xl px-1 py-2 text-center text-xs font-bold leading-5 focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 sm:min-h-12 sm:flex-row sm:gap-2 sm:px-3 sm:text-sm ${id === currentStage ? 'bg-brand-deep text-white' : 'text-brand-deep hover:bg-brand-soft'}`}>
              <Icon aria-hidden="true" className="size-4 shrink-0" strokeWidth={1.75} />
              {title}
            </button>
          ))}
        </nav>
      )}

      {canManageProducts && (
        <section hidden={currentStage !== 'products'} className="space-y-3">
          <SupplierProductsTab onPrepareShipment={canManageShipments ? prepareProduct : undefined} />
        </section>
      )}

      {canManageShipments && (shipmentsOpened || currentStage !== 'products') && (
        <section hidden={currentStage === 'products'} className="space-y-4">
          <SupplierLabeledInboundTab selectedProductId={selectedProductId} stage={currentStage === 'track' ? 'track' : 'prepare'}
            onShipmentCreated={() => navigate('track')} openOnly={openOnly} />
        </section>
      )}
    </div>
  );
}
