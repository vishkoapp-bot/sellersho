import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Box, ListChecks, PackagePlus } from 'lucide-react';
import { supplierApi } from '../../services/supplierApi';
import { SupplierLabeledInboundTab } from './SupplierLabeledInboundTab';
import { SupplierProductsTab } from './SupplierProductsTab';
import { SupplierResourceState } from './SupplierResourceState';
import { SupplierUnitPrint } from './SupplierUnitPrint';
import { useSupplierResource } from './useSupplierResource';

export type ShipmentStage = 'products' | 'prepare' | 'track';

const stages = [
  { id: 'products', title: 'ثبت کالا', detail: 'ثبت کالا، تنوع‌ها و پیگیری تأیید', icon: Box },
  { id: 'prepare', title: 'آماده‌سازی محموله', detail: 'انتخاب کالاها، تعداد و انبار مقصد', icon: PackagePlus },
  { id: 'track', title: 'ارسال و برچسب‌ها', detail: 'چاپ کارتن، ثبت ارسال و پیگیری دریافت', icon: ListChecks },
] as const;

function ProductUnitLabels({ initialProductId }: { initialProductId: string }) {
  const products = useSupplierResource(supplierApi.getProducts);
  const [productId, setProductId] = useState(initialProductId);
  const [hasLabels, setHasLabels] = useState<boolean | null>(null);
  const loadVariants = useCallback(() => productId ? supplierApi.getVariants(productId) : Promise.resolve([]), [productId]);
  const variants = useSupplierResource(loadVariants);
  return (
    <section id="supplier-unit-labels" className="space-y-3 rounded-2xl border border-slate-200 bg-[#f8f6fc] p-4">
      <h3 className="font-bold text-slate-900">چاپ برچسب واحد کالا</h3>
      <p className="text-xs leading-6 text-slate-600">کالا را انتخاب کن تا کدهای صادرشدهٔ آن را ببینی. برای دیدن کدهای قبلاً چاپ‌شده، تیک «فقط چاپ‌نشده» را بردار.</p>
      <SupplierResourceState loading={products.loading} error={products.error} retry={products.reload} />
      <label className="block text-sm font-medium">
        کالا
        <select value={productId} onChange={event => { setHasLabels(null); setProductId(event.target.value); }} className="mt-1 block min-h-11 w-full rounded-xl border border-slate-300 bg-white px-3">
          <option value="">انتخاب کالا</option>
          {(products.data ?? []).map(product => <option key={product.id} value={product.id}>{product.title}</option>)}
        </select>
      </label>
      {productId && <SupplierResourceState loading={variants.loading} error={variants.error} retry={variants.reload} />}
      {productId && hasLabels === false && (
        <p role="status" className="rounded-xl bg-slate-50 p-3 text-sm text-slate-700">برای این کالا در وضعیت‌های قابل چاپ، کد واحدی پیدا نشد. اگر کالا تازه به انبار می‌رود، ابتدا انبار باید دریافت آن را ثبت کند.</p>
      )}
      {productId && !variants.loading && !variants.error && variants.data && (
        <SupplierUnitPrint key={productId} productId={productId} variants={variants.data} onInitialAvailability={setHasLabels} />
      )}
    </section>
  );
}

interface Props {
  initialProductId: string;
  activeStage?: ShipmentStage;
  onStageChange?: (stage: ShipmentStage) => void;
  onProductConsumed: () => void;
  canManageProducts: boolean;
  canManageShipments: boolean;
}

export function SupplierInboundFlowTab({ initialProductId, activeStage, onStageChange, onProductConsumed, canManageProducts, canManageShipments }: Props) {
  const [stage, setStage] = useState<ShipmentStage>(initialProductId || !canManageProducts ? 'prepare' : 'products');
  const requestedStage = activeStage ?? stage;
  const [selectedProductId, setSelectedProductId] = useState(initialProductId);
  const [shipmentsOpened, setShipmentsOpened] = useState(stage !== 'products');
  const [labelsOpen, setLabelsOpen] = useState(false);
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
      <section ref={stepNavigation} aria-label="مراحل ثبت کالا و ارسال" className="scroll-mt-24 rounded-[24px] border border-[#e6e2f0] bg-white p-4 shadow-sm sm:p-5">
        <p className="mb-4 max-w-2xl text-sm leading-7 text-slate-700">{canManageProducts && canManageShipments ? 'برای کالای جدید از ثبت کالا شروع کن. اگر کالا قبلاً ثبت شده، «آماده‌سازی محموله» را انتخاب کن.' : canManageProducts ? 'کالا و تنوع‌هایش را ثبت کن و وضعیت تأیید را همین‌جا پیگیری کن.' : 'کالاهای ثبت‌شده را به محموله اضافه کن و ارسالشان را پیگیری کن.'}</p>
        <nav aria-label="مراحل" className={`grid gap-2 ${availableStages.length === 3 ? 'grid-cols-3' : availableStages.length === 2 ? 'grid-cols-2' : 'grid-cols-1'}`}>
          {availableStages.map(({ id, title, detail, icon: Icon }) => (
            <button key={id} type="button" aria-current={id === currentStage ? 'step' : undefined} onClick={() => navigate(id)}
              className={`group flex min-h-18 min-w-0 flex-col items-center justify-center gap-1.5 rounded-2xl border px-1.5 py-2 text-center focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-offset-2 sm:flex-row sm:justify-start sm:gap-3 sm:px-3 sm:text-start ${id === currentStage ? 'border-[#382660] bg-[#382660] text-white' : 'border-[#e6e2f0] bg-[#f8f6fc] text-[#302056] hover:border-brand-border hover:bg-brand-soft'}`}>
              <span className={`grid size-8 shrink-0 place-items-center rounded-xl sm:size-9 ${id === currentStage ? 'bg-white/15' : 'bg-[#ede6fa]'}`}>
                <Icon aria-hidden="true" className="size-4.5" strokeWidth={1.75} />
              </span>
              <span className="min-w-0">
                <strong className="block text-xs leading-5 sm:text-sm sm:leading-6">{title}</strong>
                <span className={`hidden text-xs leading-5 sm:block ${id === currentStage ? 'text-[#f1e9ff]' : 'text-slate-600'}`}>{detail}</span>
              </span>
            </button>
          ))}
        </nav>
      </section>

      {canManageProducts && (
        <section hidden={currentStage !== 'products'} className="space-y-3">
          <SupplierProductsTab onPrepareShipment={canManageShipments ? prepareProduct : undefined} />
        </section>
      )}

      {canManageShipments && (shipmentsOpened || currentStage !== 'products') && (
        <section hidden={currentStage === 'products'} className="space-y-4">
          <SupplierLabeledInboundTab selectedProductId={selectedProductId} stage={currentStage === 'track' ? 'track' : 'prepare'}
            onShipmentCreated={() => navigate('track')}
            onUnitLabels={canManageProducts ? () => setLabelsOpen(open => !open) : undefined}
            unitLabels={labelsOpen && currentStage === 'track' && canManageProducts ? <ProductUnitLabels initialProductId={selectedProductId} /> : undefined} />
        </section>
      )}
    </div>
  );
}
